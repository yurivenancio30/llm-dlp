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
	reLogReq   = regexp.MustCompile(` -> (\d{3}) \| (\d+) substituições \| (\d+) colisões`)
	reLogTipos = regexp.MustCompile(` \| tipos: ([^|]+)`)
	reLogND    = regexp.MustCompile(` \| NÃO DESFEITOS \d+: ([^|]+)`)
	reLogAPI   = regexp.MustCompile(` \| API: ([\w.-]+)`)
)

type resumoLog struct {
	desde                    time.Time
	req, subst, colisoes     int
	status                   map[string]int // status HTTP diferente de 2xx
	errosAPI                 map[string]int
	tipos                    map[string]int
	respostasND              int
	pseudosND                []string
	semMascara, erros, recus int
	motivosRecusa            map[string]int
}

func novoResumo(desde time.Time) *resumoLog {
	return &resumoLog{desde: desde, status: map[string]int{}, errosAPI: map[string]int{}, tipos: map[string]int{},
		motivosRecusa: map[string]int{}}
}

// ler junta ao resumo as linhas do período (as linhas começam com "2006/01/02 15:04:05 ", na
// hora local).
func (r *resumoLog) ler(in io.Reader) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		l := sc.Text()
		if len(l) < 20 {
			continue
		}
		t, err := time.ParseInLocation("2006/01/02 15:04:05", l[:19], time.Local)
		if err != nil || t.Before(r.desde) {
			continue
		}
		msg := l[20:]
		switch {
		case strings.HasPrefix(msg, "RECUSADO "):
			r.recus++
			if i := strings.Index(msg, ": "); i >= 0 {
				m := msg[i+2:]
				if len(m) > 70 {
					m = m[:70] + "…"
				}
				r.motivosRecusa[m]++
			}
		case strings.HasPrefix(msg, "AVISO ") && strings.Contains(msg, "sem máscara"):
			r.semMascara++
		case strings.HasPrefix(msg, "ERRO "):
			r.erros++
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
		r.subst += n
		r.colisoes += c
		if a := reLogAPI.FindStringSubmatch(msg); a != nil {
			r.errosAPI[strings.TrimSuffix(a[1], ":")]++
		}
		if tp := reLogTipos.FindStringSubmatch(msg); tp != nil {
			for _, par := range strings.Split(tp[1], ",") {
				f := strings.Fields(par)
				if len(f) == 2 {
					k, _ := strconv.Atoi(f[1])
					r.tipos[f[0]] += k
				}
			}
		}
		if nd := reLogND.FindStringSubmatch(msg); nd != nil {
			r.respostasND++
			for _, p := range strings.Split(nd[1], ",") {
				if p = strings.TrimSpace(p); p != "" && len(r.pseudosND) < 40 && !contem(r.pseudosND, p) {
					r.pseudosND = append(r.pseudosND, p)
				}
			}
		}
	}
}

func contem(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// contagens: "a 3, b 1", do maior ao menor.
func contagens(m map[string]int) string {
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

func (r *resumoLog) texto(periodo string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "llm-dlp: log das últimas %s (desde %s)\n", periodo, r.desde.Format("02/01 15:04"))
	fmt.Fprintf(&b, "  requisições: %d", r.req)
	if len(r.status) > 0 {
		fmt.Fprintf(&b, " (status HTTP: %s)", contagens(r.status))
	}
	b.WriteString("\n")
	if len(r.errosAPI) > 0 {
		fmt.Fprintf(&b, "  erros da API: %s\n", contagens(r.errosAPI))
	}
	fmt.Fprintf(&b, "  substituições: %d; colisões (pseudônimo que ficou sem desmascarar): %d\n", r.subst, r.colisoes)
	if len(r.tipos) > 0 {
		fmt.Fprintf(&b, "  mascarados por tipo (valores distintos de cada requisição, somados): %s\n", contagens(r.tipos))
	}
	if r.respostasND == 0 {
		b.WriteString("  pseudônimos que voltaram sem virar o valor real: nenhum\n")
	} else {
		fmt.Fprintf(&b, "  ATENÇÃO: %d resposta(s) chegaram ao cliente com pseudônimo no lugar do valor real: %s\n",
			r.respostasND, strings.Join(r.pseudosND, ", "))
		b.WriteString("    (procure-os nos arquivos gravados nesse período: eles ficaram com o pseudônimo)\n")
	}
	if r.semMascara > 0 {
		fmt.Fprintf(&b, "  ATENÇÃO: %d requisição(ões) enviada(s) SEM máscara (fail_closed desligado e o mascaramento falhou)\n", r.semMascara)
	} else {
		b.WriteString("  enviadas sem máscara: nenhuma\n")
	}
	if r.recus > 0 {
		fmt.Fprintf(&b, "  recusadas: %d (%s)\n", r.recus, contagens(r.motivosRecusa))
	}
	if r.erros > 0 {
		fmt.Fprintf(&b, "  falhas ao falar com a API: %d\n", r.erros)
	}
	return b.String()
}

// periodo: "24h", "90m", "7d" (dias) ou só o número de horas.
func periodo(s string) (time.Duration, error) {
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

func auditar(args []string) error {
	p := "24h"
	if len(args) > 0 {
		p = args[0]
	}
	d, err := periodo(p)
	if err != nil {
		return err
	}
	r := novoResumo(time.Now().Add(-d))
	arq := config.Caminho("llm-dlp.log")
	lidos := 0
	for _, a := range []string{arq + ".1", arq} { // o antigo primeiro: a ordem do tempo
		f, err := os.Open(a)
		if err != nil {
			continue
		}
		r.ler(f)
		f.Close()
		lidos++
	}
	if lidos == 0 {
		return fmt.Errorf("sem log em %s", arq)
	}
	fmt.Print(r.texto(p))
	return nil
}
