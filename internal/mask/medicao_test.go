package mask

import (
	"bufio"
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

// Medições de falso positivo (seção I, item 39). Só rodam com as variáveis de ambiente e só
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
	for _, a := range arqs {
		var c contRegra
		c.ligar()
		m := novoTeste(t)
		m.cfg.DominiosInternos, m.cfg.Termos = nil, nil
		textos += mascararSessao(m, a)
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
}

// mascararSessao: os textos de um transcript na ordem, com a dica do comando de cada
// resultado de ferramenta (como o proxy faz). Devolve quantos textos.
func mascararSessao(m *Masker, arq string) int {
	f, err := os.Open(arq)
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 256<<20)
	cs := NovosComandos()
	cmds := map[string]string{}
	n := 0
	for sc.Scan() {
		var e map[string]any
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		msg, _ := e["message"].(map[string]any)
		switch c := msg["content"].(type) {
		case string:
			m.Mascarar(c)
			n++
		case []any:
			for _, b := range c {
				bl, _ := b.(map[string]any)
				switch bl["type"] {
				case "text":
					if s, ok := bl["text"].(string); ok {
						m.Mascarar(s)
						n++
					}
				case "tool_use":
					id, _ := bl["id"].(string)
					if in, ok := bl["input"].(map[string]any); ok {
						for _, k := range []string{"command", "query", "sql", "file_path", "path", "pattern"} {
							if v, ok := in[k].(string); ok {
								cmds[id] = v
								break
							}
						}
					}
				case "tool_result":
					id, _ := bl["tool_use_id"].(string)
					for _, s := range textosResultado(bl["content"]) {
						d := ""
						if cmd := cmds[id]; cmd != "" {
							d = cs.Dica(cmd, s)
						}
						m.mascararD(s, true, d)
						n++
					}
				}
			}
		}
	}
	return n
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
