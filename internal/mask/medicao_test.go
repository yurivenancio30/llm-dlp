package mask

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Medições de falso positivo. Só rodam com as variáveis de ambiente e só
// imprimem contagens (nenhum valor):
//
//	LLM_DLP_CORPUS=dir1:dir2   corpus público (código e documentação de terceiros): todo nome de
//	                           objeto achado ali é falso positivo; conta por regra e por MB
//	LLM_DLP_SESSOES=dir        transcripts (.jsonl) de sessões reais: por regra, quantos nomes
//	                           foram aprendidos e quantos deles existem no corpus público (sem
//	                           cara de nome interno). Limite: 2% por regra. Precisa do corpus.
//
// O Masker usa chave e configuração de teste (nada da instalação real é lido nem gravado).

type contRegra struct {
	mu      sync.Mutex
	achados map[string]int
	valores map[string]map[string]bool // regra -> valores achados
}

func (c *contRegra) ligar() {
	c.achados, c.valores = map[string]int{}, map[string]map[string]bool{}
	ganchoAchado = func(leitor string, o ObjAchado, v string) {
		k := leitor + "/" + o.Regra
		c.mu.Lock()
		c.achados[k]++
		if c.valores[k] == nil {
			c.valores[k] = map[string]bool{}
		}
		c.valores[k][strings.Trim(v, "[]\"`")] = true
		c.mu.Unlock()
	}
}

func desligarGancho() { ganchoAchado = nil }

// arquivosCorpus: os arquivos de texto do corpus, em ordem, até max bytes.
func arquivosCorpus(dirs []string, max int64) []string {
	exts := conj(".go", ".py", ".md", ".rst", ".txt", ".yaml", ".yml", ".json", ".toml", ".sql", ".sh", ".cfg", ".ini")
	var out []string
	var tot int64
	for _, d := range dirs {
		filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil || tot >= max {
				return nil
			}
			if e.IsDir() {
				if n := e.Name(); n == "testdata" || n == ".git" || n == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			if !exts[strings.ToLower(filepath.Ext(p))] {
				return nil
			}
			if i, err := e.Info(); err == nil && i.Size() < 512<<10 {
				out = append(out, p)
				tot += i.Size()
			}
			return nil
		})
	}
	return out
}

func TestMedirCorpusPublico(t *testing.T) {
	dirs := os.Getenv("LLM_DLP_CORPUS")
	if dirs == "" {
		t.Skip("LLM_DLP_CORPUS não definido")
	}
	lim := int64(40 << 20)
	if os.Getenv("LLM_DLP_CORPUS_MB") != "" {
		fmt.Sscan(os.Getenv("LLM_DLP_CORPUS_MB"), &lim)
		lim <<= 20
	}
	arqs := arquivosCorpus(strings.Split(dirs, ":"), lim)
	var c contRegra
	c.ligar()
	defer desligarGancho()
	var bytes int64
	for i, a := range arqs {
		b, err := os.ReadFile(a)
		if err != nil {
			continue
		}
		bytes += int64(len(b))
		if i%200 == 0 { // um Masker novo de tempos em tempos: o que se aprende num projeto não vaza para o outro
			t.Logf("... %d/%d arquivos", i, len(arqs))
		}
		m := novoTeste(t)
		m.cfg.DominiosInternos, m.cfg.Termos = nil, nil
		m.Mascarar(string(b))
	}
	mb := float64(bytes) / (1 << 20)
	var ls []string
	tot := 0
	for k, n := range c.achados {
		ls = append(ls, fmt.Sprintf("  %-40s %6d achados %6d valores distintos  %7.2f/MB", k, n, len(c.valores[k]), float64(n)/mb))
		tot += n
	}
	sort.Strings(ls)
	if os.Getenv("LLM_DLP_AMOSTRA") != "" { // só no corpus público (nunca nas sessões): exemplos por regra
		for k, vs := range c.valores {
			var xs []string
			for v := range vs {
				if xs = append(xs, v); len(xs) == 12 {
					break
				}
			}
			t.Logf("amostra %s: %s", k, strings.Join(xs, " "))
		}
	}
	t.Logf("corpus público: %d arquivos, %.1f MB, %d achados (%.2f/MB)\n%s", len(arqs), mb, tot, float64(tot)/mb, strings.Join(ls, "\n"))
}

// identificadoresPublicos: os identificadores (com cara de nome) que aparecem no corpus público.
func identificadoresPublicos(dirs []string) map[string]bool {
	out := map[string]bool{}
	for _, a := range arquivosCorpus(dirs, 200<<20) {
		b, err := os.ReadFile(a)
		if err != nil {
			continue
		}
		s := string(b)
		tokensObj(s, func(i, j int) {
			if v := s[i:j]; caraDeIdentificador(v) {
				out[strings.ToLower(v)] = true
			}
		})
	}
	return out
}

func TestMedirSessoesReais(t *testing.T) {
	dir, corpus := os.Getenv("LLM_DLP_SESSOES"), os.Getenv("LLM_DLP_CORPUS")
	if dir == "" || corpus == "" {
		t.Skip("LLM_DLP_SESSOES e LLM_DLP_CORPUS não definidos")
	}
	pub := identificadoresPublicos(strings.Split(corpus, ":"))
	var arqs []string
	filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() && strings.HasSuffix(p, ".jsonl") {
			arqs = append(arqs, p)
		}
		return nil
	})
	sort.Strings(arqs)
	aprendidos := map[string]map[string]bool{} // regra -> valores aprendidos
	textos := 0
	var comuns []int
	porRegra := map[string]int{}
	trocas := map[string]int{}
	var pu, tu, pa, ta int
	for _, a := range arqs {
		var c contRegra
		c.ligar()
		m := novoTeste(t)
		m.cfg.DominiosInternos, m.cfg.Termos = nil, nil
		st := mascararSessao(m, a)
		textos += st.textos
		comuns = append(comuns, st.comunsNaMemoria)
		for k, n := range st.porRegra {
			porRegra[k] += n
		}
		for k, n := range st.trocasPorTipo {
			trocas[k] += n
		}
		pu, tu, pa, ta = pu+st.palavrasUsuario, tu+st.trocadasUsuario, pa+st.palavrasAssist, ta+st.trocadasAssist
		for k, vs := range c.valores {
			for v := range vs {
				if _, ok := m.entAprendido(v); ok {
					if aprendidos[k] == nil {
						aprendidos[k] = map[string]bool{}
					}
					aprendidos[k][strings.ToLower(v)] = true
				}
			}
		}
		desligarGancho()
	}
	var ls []string
	ruins := 0
	for k, vs := range aprendidos {
		p := 0
		for v := range vs {
			if pub[v] {
				p++
			}
		}
		taxa := 100 * float64(p) / float64(len(vs))
		marca := ""
		if taxa > 2 && p > 1 {
			marca = "  <-- acima de 2%" + formas(vs, pub)
			ruins++
		}
		ls = append(ls, fmt.Sprintf("  %-40s %6d aprendidos %5d no corpus público %6.1f%%%s", k, len(vs), p, taxa, marca))
	}
	sort.Strings(ls)
	t.Logf("sessões: %d arquivos, %d textos; identificadores públicos de referência: %d\n%s", len(arqs), textos, len(pub), strings.Join(ls, "\n"))
	if ruins > 0 {
		t.Logf("%d regras acima de 2%%", ruins)
	}
	sort.Ints(comuns)
	q := func(p float64) int {
		if len(comuns) == 0 {
			return 0
		}
		return comuns[int(float64(len(comuns)-1)*p)]
	}
	var rs []string
	for k, n := range porRegra {
		rs = append(rs, fmt.Sprintf("%s=%d", k, n))
	}
	sort.Strings(rs)
	var tr []string
	for k, n := range trocas {
		tr = append(tr, fmt.Sprintf("%s=%d", k, n))
	}
	sort.Strings(tr)
	t.Logf("trocas no texto do usuário, por tipo/forma: %s", strings.Join(tr, " "))
	pct := func(a, b int) float64 { return 100 * float64(a) / float64(max(1, b)) }
	t.Logf("memória da conversa: palavras comuns por conversa p50=%d p90=%d máx=%d (por regra, somando as conversas: %s)\n"+
		"texto corrido trocado pela memória: usuário %d de %d palavras (%.3f%%); assistente, pior caso sem registro, %d de %d (%.3f%%)",
		q(.5), q(.9), q(1), strings.Join(rs, " "), tu, pu, pct(tu, pu), ta, pa, pct(ta, pa))
}

// statsSessao: as duas métricas da memória da conversa numa sessão (só contagens).
type statsSessao struct {
	textos          int
	comunsNaMemoria int            // palavras comuns (sem cara de identificador) que entram na memória
	porRegra        map[string]int // as mesmas, pela regra que decidiu
	palavrasUsuario int            // palavras de texto corrido nas mensagens do usuário
	trocadasUsuario int            // quantas a memória trocaria
	palavrasAssist  int            // o mesmo nos textos do assistente (pior caso: sem registro)
	trocadasAssist  int
	trocasPorTipo   map[string]int // trocas no texto do usuário, pelo tipo do nome (forma, sem valor)
}

// mascararSessao: os textos de um transcript na ordem, com a dica de cada resultado de
// ferramenta como o proxy monta (comando, pedido do usuário, extensão), e no fim a memória da
// conversa da sessão inteira (o último pedido de uma conversa carrega o histórico todo).
func mascararSessao(m *Masker, arq string) statsSessao {
	var st statsSessao
	b, err := os.ReadFile(arq)
	if err != nil {
		return st
	}
	type item struct {
		s, dica string
		papel   string // "user", "assistant" ou "tool"
	}
	var msgs []map[string]any
	for _, l := range strings.Split(string(b), "\n") {
		var e map[string]any
		if json.Unmarshal([]byte(l), &e) != nil {
			continue
		}
		if msg, ok := e["message"].(map[string]any); ok {
			msgs = append(msgs, msg)
		}
	}
	cs := NovosComandos()
	cmds := map[string]string{}
	chs := map[string]*Chamada{}
	blocos := func(msg map[string]any) []map[string]any {
		if t, ok := msg["content"].(string); ok {
			return []map[string]any{{"type": "text", "text": t}}
		}
		var out []map[string]any
		for _, x := range asSlice(msg["content"]) {
			if bl, ok := x.(map[string]any); ok {
				out = append(out, bl)
			}
		}
		return out
	}
	for _, msg := range msgs {
		for _, bl := range blocos(msg) {
			if bl["type"] == "tool_use" {
				id, _ := bl["id"].(string)
				cmds[id] = comandoSessao(bl["input"])
				cs.Argumentos(cmds[id])
			}
		}
	}
	var itens []item
	for _, msg := range msgs {
		papel, _ := msg["role"].(string)
		for _, bl := range blocos(msg) {
			switch bl["type"] {
			case "text":
				if t, ok := bl["text"].(string); ok {
					if papel == "user" {
						cs.Usuario(t)
					}
					itens = append(itens, item{t, "", papel})
				}
			case "tool_use":
				id, _ := bl["id"].(string)
				chs[id] = cs.Uso(cmds[id], nil)
			case "tool_result":
				id, _ := bl["tool_use_id"].(string)
				saida := strings.Join(textosResultado(bl["content"]), "\n")
				d := ""
				if cmd := cmds[id]; cmd != "" {
					d = cs.Dica(cmd, saida)
				}
				d = ComExtensao(d, cs.Resultado(chs[id], saida))
				for _, x := range textosResultado(bl["content"]) {
					itens = append(itens, item{x, d, "tool"})
				}
			}
		}
		if papel == "assistant" {
			cs.FimTurno()
		}
	}
	var decs []Decisao
	res := make([]resultado, len(itens))
	for i, it := range itens {
		res[i], _ = m.mascararD(it.s, true, it.dica)
		decs = append(decs, res[i].decididos...)
	}
	st.textos = len(itens)
	mm := m.novaMemoria(decs)
	st.porRegra = map[string]int{}
	if mm == nil {
		return st
	}
	visto := map[string]bool{}
	for _, d := range decs {
		if d.Generica || len(d.Nome) < memMin || caraDeIdentificador(d.Nome) || visto[d.Chave()] {
			continue
		}
		visto[d.Chave()] = true
		st.comunsNaMemoria++
		st.porRegra[d.Regra]++
	}
	for i, it := range itens {
		if it.papel == "tool" {
			continue
		}
		n := 0
		varrerPalavras(it.s, func(a, b int) { n++ })
		k := 0
		for _, t := range mm.varrerF(it.s, res[i].trechos, filtroDe(it.dica)) {
			k++
			if it.papel == "user" {
				if st.trocasPorTipo == nil {
					st.trocasPorTipo = map[string]int{}
				}
				forma := "comum"
				if caraDeIdentificador(it.s[t.Ini:t.Fim]) {
					forma = "identificador"
				}
				st.trocasPorTipo[strings.TrimPrefix(t.Tipo, prefTipoObj)+"/"+forma]++
			}
		}
		if it.papel == "user" {
			st.palavrasUsuario += n
			st.trocadasUsuario += k
		} else {
			st.palavrasAssist += n
			st.trocadasAssist += k
		}
	}
	return st
}

func asSlice(v any) []any { x, _ := v.([]any); return x }

// comandoSessao: como o proxy (comandoDe): o comando do shell, ou as strings da entrada.
func comandoSessao(v any) string {
	in, _ := v.(map[string]any)
	if c, ok := in["command"].(string); ok {
		return c
	}
	ks := make([]string, 0, len(in))
	for k := range in {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	var b strings.Builder
	for _, k := range ks {
		if x, ok := in[k].(string); ok && k != "description" && len(x) <= 4<<10 {
			b.WriteString(x + "\n")
		}
	}
	return b.String()
}

func textosResultado(c any) []string {
	switch x := c.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, b := range x {
			if bl, ok := b.(map[string]any); ok && bl["type"] == "text" {
				if s, ok := bl["text"].(string); ok {
					out = append(out, s)
				}
			}
		}
		return out
	}
	return nil
}

// formas: só a FORMA dos aprendidos que existem no corpus público (nenhum valor), para achar a
// regra que precisa de freio.
func formas(vs, pub map[string]bool) string {
	n := map[string]int{}
	for v := range vs {
		if !pub[v] {
			continue
		}
		switch {
		case strings.Contains(v, "."):
			n["ponto"]++
		case strings.Contains(v, "-"):
			n["hífen"]++
		case strings.ContainsAny(v, "0123456789"):
			n["dígito"]++
		case strings.Contains(v, "_"):
			n["snake"]++
		default:
			n["outro"]++
		}
		n[fmt.Sprintf("tam%d", min(len(v)/5*5, 20))]++
	}
	var ks []string
	for k, c := range n {
		ks = append(ks, fmt.Sprintf("%s=%d", k, c))
	}
	sort.Strings(ks)
	return " [" + strings.Join(ks, " ") + "]"
}
