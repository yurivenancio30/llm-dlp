package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// llm-dlp audit [PERÍODO]: o resumo do llm-dlp.log (e do .1) num período. Só lê o log, que só tem
// contagens e pseudônimos: nada do dado real. Responde o que antes exigia vigiar a sessão:
// o que foi mascarado, por tipo; se algum pseudônimo voltou ao cliente sem virar o valor real
// (e pode ter ido parar num arquivo); se algo saiu sem máscara; o que foi recusado.

var (
	reLogReq       = regexp.MustCompile(` -> (\d{3}) \| (\d+) substituições \| (\d+) colisões`)
	reLogTypes     = regexp.MustCompile(` \| tipos: ([^|]+)`)
	reLogNotUndone = regexp.MustCompile(` \| NÃO DESFEITOS \d+: ([^|]+)`)
	reLogCol       = regexp.MustCompile(` \| colididos: ([^|]+)`)
	reLogAPI       = regexp.MustCompile(` \| API: ([\w.-]+)`)
)

type logSummary struct {
	since                    time.Time
	req, substs, collisions  int
	collided                 []string
	status                   map[string]int // status HTTP diferente de 2xx
	apiErrors                map[string]int
	types                    map[string]int
	ndResponses              int
	pseudosNotUndone         []string
	unmasked, errs, refusedN int
	refusalReasons           map[string]int
}

func newSummary(since time.Time) *logSummary {
	return &logSummary{since: since, status: map[string]int{}, apiErrors: map[string]int{}, types: map[string]int{},
		refusalReasons: map[string]int{}}
}

// ler junta ao resumo as linhas do período (as linhas começam com "2006/01/02 15:04:05 ", na
// hora local).
func (r *logSummary) readIt(in io.Reader) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		l := sc.Text()
		if len(l) < 20 {
			continue
		}
		t, err := time.ParseInLocation("2006/01/02 15:04:05", l[:19], time.Local)
		if err != nil || t.Before(r.since) {
			continue
		}
		msg := l[20:]
		switch {
		case strings.HasPrefix(msg, "RECUSADO "):
			r.refusedN++
			if i := strings.Index(msg, ": "); i >= 0 {
				m := msg[i+2:]
				if len(m) > 70 {
					m = m[:70] + "…"
				}
				r.refusalReasons[m]++
			}
		case strings.HasPrefix(msg, "AVISO ") && strings.Contains(msg, "sem máscara"):
			r.unmasked++
		case strings.HasPrefix(msg, "ERRO "):
			r.errs++
		}
		m := reLogReq.FindStringSubmatch(msg)
		if m == nil {
			continue
		}
		r.req++
		if m[1][0] != '2' {
			r.status[m[1]]++
		}
		n, _ := strconv.Atoi(m[2])
		c, _ := strconv.Atoi(m[3])
		r.substs += n
		r.collisions += c
		if a := reLogAPI.FindStringSubmatch(msg); a != nil {
			r.apiErrors[strings.TrimSuffix(a[1], ":")]++
		}
		if tp := reLogTypes.FindStringSubmatch(msg); tp != nil {
			for _, pair := range strings.Split(tp[1], ",") {
				f := strings.Fields(pair)
				if len(f) == 2 {
					k, _ := strconv.Atoi(f[1])
					r.types[f[0]] += k
				}
			}
		}
		if nd := reLogNotUndone.FindStringSubmatch(msg); nd != nil {
			r.ndResponses++
			r.pseudosNotUndone = joinList(r.pseudosNotUndone, nd[1])
		}
		if c := reLogCol.FindStringSubmatch(msg); c != nil {
			r.collided = joinList(r.collided, c[1])
		}
	}
}

// joinList acrescenta os itens de "a, b, c" que ainda não estão em xs (até 40).
func joinList(xs []string, list string) []string {
	for _, p := range strings.Split(list, ",") {
		if p = strings.TrimSpace(p); p != "" && len(xs) < 40 && !containsStr(xs, p) {
			xs = append(xs, p)
		}
	}
	return xs
}

func containsStr(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// contagens: "a 3, b 1", do maior ao menor.
func counts(m map[string]int) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool { return m[ks[i]] > m[ks[j]] || m[ks[i]] == m[ks[j]] && ks[i] < ks[j] })
	ps := make([]string, len(ks))
	for i, k := range ks {
		ps[i] = fmt.Sprintf("%s %d", k, m[k])
	}
	return strings.Join(ps, ", ")
}

func (r *logSummary) text(period string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "llm-dlp: log das últimas %s (desde %s)\n", period, r.since.Format("02/01 15:04"))
	fmt.Fprintf(&b, "  requisições: %d", r.req)
	if len(r.status) > 0 {
		fmt.Fprintf(&b, " (status HTTP: %s)", counts(r.status))
	}
	b.WriteString("\n")
	if len(r.apiErrors) > 0 {
		fmt.Fprintf(&b, "  erros da API: %s\n", counts(r.apiErrors))
	}
	fmt.Fprintf(&b, "  substituições: %d; colisões (pseudônimo que ficou sem desmascarar): %d\n", r.substs, r.collisions)
	if len(r.collided) > 0 {
		fmt.Fprintf(&b, "    pseudônimos colididos: %s\n", strings.Join(r.collided, ", "))
	}
	if len(r.types) > 0 {
		fmt.Fprintf(&b, "  mascarados por tipo (valores distintos de cada requisição, somados): %s\n", counts(r.types))
	}
	if r.ndResponses == 0 {
		b.WriteString("  pseudônimos que voltaram sem virar o valor real: nenhum\n")
	} else {
		fmt.Fprintf(&b, "  ATENÇÃO: %d resposta(s) chegaram ao cliente com pseudônimo no lugar do valor real: %s\n",
			r.ndResponses, strings.Join(r.pseudosNotUndone, ", "))
		b.WriteString("    (procure-os nos arquivos gravados nesse período: eles ficaram com o pseudônimo)\n")
	}
	if r.unmasked > 0 {
		fmt.Fprintf(&b, "  ATENÇÃO: %d requisição(ões) enviada(s) SEM máscara (fail_closed desligado e o mascaramento falhou)\n", r.unmasked)
	} else {
		b.WriteString("  enviadas sem máscara: nenhuma\n")
	}
	if r.refusedN > 0 {
		fmt.Fprintf(&b, "  recusadas: %d (%s)\n", r.refusedN, counts(r.refusalReasons))
	}
	if r.errs > 0 {
		fmt.Fprintf(&b, "  falhas ao falar com a API: %d\n", r.errs)
	}
	return b.String()
}

// periodo: "24h", "90m", "7d" (dias) ou só o número de horas.
func period(s string) (time.Duration, error) {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return time.Duration(n) * time.Hour, nil
	}
	if strings.HasSuffix(s, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour, nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("período inválido: %q (use, por exemplo, 24h, 90m ou 7d)", s)
	}
	return d, nil
}

func audit(args []string) error {
	p := "24h"
	if len(args) > 0 {
		p = args[0]
	}
	d, err := period(p)
	if err != nil {
		return err
	}
	r := newSummary(time.Now().Add(-d))
	file := config.Path("llm-dlp.log")
	readOnes := 0
	for _, a := range []string{file + ".1", file} { // o antigo primeiro: a ordem do tempo
		f, err := os.Open(a)
		if err != nil {
			continue
		}
		r.readIt(f)
		f.Close()
		readOnes++
	}
	if readOnes == 0 {
		return fmt.Errorf("sem log em %s", file)
	}
	fmt.Print(r.text(p))
	return nil
}
