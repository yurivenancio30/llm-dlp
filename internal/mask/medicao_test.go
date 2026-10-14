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

type ruleCount struct {
	mu       sync.Mutex
	findings map[string]int
	values   map[string]map[string]bool // regra -> valores achados
}

func (c *ruleCount) enable() {
	c.findings, c.values = map[string]int{}, map[string]map[string]bool{}
	hookFound = func(reader string, o ObjFinding, v string) {
		k := reader + "/" + o.Rule
		c.mu.Lock()
		c.findings[k]++
		if c.values[k] == nil {
			c.values[k] = map[string]bool{}
		}
		c.values[k][strings.Trim(v, "[]\"`")] = true
		c.mu.Unlock()
	}
}

func disableHook() { hookFound = nil }

// corpusFiles: os arquivos de texto do corpus, em ordem, até max bytes.
func corpusFiles(dirs []string, max int64) []string {
	exts := setOf(".go", ".py", ".md", ".rst", ".txt", ".yaml", ".yml", ".json", ".toml", ".sql", ".sh", ".cfg", ".ini")
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

func TestMeasurePublicCorpus(t *testing.T) {
	dirs := os.Getenv("LLM_DLP_CORPUS")
	if dirs == "" {
		t.Skip("LLM_DLP_CORPUS não definido")
	}
	lim := int64(40 << 20)
	if os.Getenv("LLM_DLP_CORPUS_MB") != "" {
		fmt.Sscan(os.Getenv("LLM_DLP_CORPUS_MB"), &lim)
		lim <<= 20
	}
	files := corpusFiles(strings.Split(dirs, ":"), lim)
	var c ruleCount
	c.enable()
	defer disableHook()
	var bytes int64
	for i, a := range files {
		b, err := os.ReadFile(a)
		if err != nil {
			continue
		}
		bytes += int64(len(b))
		if i%200 == 0 { // um Masker novo de tempos em tempos: o que se aprende num projeto não vaza para o outro
			t.Logf("... %d/%d arquivos", i, len(files))
		}
		m := newTest(t)
		m.cfg.InternalDomains, m.cfg.Terms = nil, nil
		m.Mask(string(b))
	}
	mb := float64(bytes) / (1 << 20)
	var ls []string
	tot := 0
	for k, n := range c.findings {
		ls = append(ls, fmt.Sprintf("  %-40s %6d achados %6d valores distintos  %7.2f/MB", k, n, len(c.values[k]), float64(n)/mb))
		tot += n
	}
	sort.Strings(ls)
	if os.Getenv("LLM_DLP_AMOSTRA") != "" { // só no corpus público (nunca nas sessões): exemplos por regra
		for k, vs := range c.values {
			var xs []string
			for v := range vs {
				if xs = append(xs, v); len(xs) == 12 {
					break
				}
			}
			t.Logf("amostra %s: %s", k, strings.Join(xs, " "))
		}
	}
	t.Logf("corpus público: %d arquivos, %.1f MB, %d achados (%.2f/MB)\n%s", len(files), mb, tot, float64(tot)/mb, strings.Join(ls, "\n"))
}

// publicIdentifiers: os identificadores (com cara de nome) que aparecem no corpus público.
func publicIdentifiers(dirs []string) map[string]bool {
	out := map[string]bool{}
	for _, a := range corpusFiles(dirs, 200<<20) {
		b, err := os.ReadFile(a)
		if err != nil {
			continue
		}
		s := string(b)
		objTokens(s, func(i, j int) {
			if v := s[i:j]; identifierShape(v) {
				out[strings.ToLower(v)] = true
			}
		})
	}
	return out
}

func TestMeasureRealSessions(t *testing.T) {
	dir, corpus := os.Getenv("LLM_DLP_SESSOES"), os.Getenv("LLM_DLP_CORPUS")
	if dir == "" || corpus == "" {
		t.Skip("LLM_DLP_SESSOES e LLM_DLP_CORPUS não definidos")
	}
	pub := publicIdentifiers(strings.Split(corpus, ":"))
	var files []string
	filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() && strings.HasSuffix(p, ".jsonl") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	learnedOnes := map[string]map[string]bool{} // regra -> valores aprendidos
	texts := 0
	var commons []int
	byRule := map[string]int{}
	swaps := map[string]int{}
	var pu, tu, pa, ta int
	for _, a := range files {
		var c ruleCount
		c.enable()
		m := newTest(t)
		m.cfg.InternalDomains, m.cfg.Terms = nil, nil
		st := maskSession(m, a)
		texts += st.texts
		commons = append(commons, st.commonsInMemory)
		for k, n := range st.byRule {
			byRule[k] += n
		}
		for k, n := range st.swapsByType {
			swaps[k] += n
		}
		pu, tu, pa, ta = pu+st.userWords, tu+st.userSwaps, pa+st.assistWords, ta+st.assistSwaps
		for k, vs := range c.values {
			for v := range vs {
				if _, ok := m.learnedEnt(v); ok {
					if learnedOnes[k] == nil {
						learnedOnes[k] = map[string]bool{}
					}
					learnedOnes[k][strings.ToLower(v)] = true
				}
			}
		}
		disableHook()
	}
	var ls []string
	bad := 0
	for k, vs := range learnedOnes {
		p := 0
		for v := range vs {
			if pub[v] {
				p++
			}
		}
		rate := 100 * float64(p) / float64(len(vs))
		mark := ""
		if rate > 2 && p > 1 {
			mark = "  <-- acima de 2%" + shapes(vs, pub)
			bad++
		}
		ls = append(ls, fmt.Sprintf("  %-40s %6d aprendidos %5d no corpus público %6.1f%%%s", k, len(vs), p, rate, mark))
	}
	sort.Strings(ls)
	t.Logf("sessões: %d arquivos, %d textos; identificadores públicos de referência: %d\n%s", len(files), texts, len(pub), strings.Join(ls, "\n"))
	if bad > 0 {
		t.Logf("%d regras acima de 2%%", bad)
	}
	sort.Ints(commons)
	q := func(p float64) int {
		if len(commons) == 0 {
			return 0
		}
		return commons[int(float64(len(commons)-1)*p)]
	}
	var rs []string
	for k, n := range byRule {
		rs = append(rs, fmt.Sprintf("%s=%d", k, n))
	}
	sort.Strings(rs)
	var tr []string
	for k, n := range swaps {
		tr = append(tr, fmt.Sprintf("%s=%d", k, n))
	}
	sort.Strings(tr)
	t.Logf("trocas no texto do usuário, por tipo/forma: %s", strings.Join(tr, " "))
	pct := func(a, b int) float64 { return 100 * float64(a) / float64(max(1, b)) }
	t.Logf("memória da conversa: palavras comuns por conversa p50=%d p90=%d máx=%d (por regra, somando as conversas: %s)\n"+
		"texto corrido trocado pela memória: usuário %d de %d palavras (%.3f%%); assistente, pior caso sem registro, %d de %d (%.3f%%)",
		q(.5), q(.9), q(1), strings.Join(rs, " "), tu, pu, pct(tu, pu), ta, pa, pct(ta, pa))
}

// sessionStats: as duas métricas da memória da conversa numa sessão (só contagens).
type sessionStats struct {
	texts           int
	commonsInMemory int            // palavras comuns (sem cara de identificador) que entram na memória
	byRule          map[string]int // as mesmas, pela regra que decidiu
	userWords       int            // palavras de texto corrido nas mensagens do usuário
	userSwaps       int            // quantas a memória trocaria
	assistWords     int            // o mesmo nos textos do assistente (pior caso: sem registro)
	assistSwaps     int
	swapsByType     map[string]int // trocas no texto do usuário, pelo tipo do nome (forma, sem valor)
}

// maskSession: os textos de um transcript na ordem, com a dica de cada resultado de
// ferramenta como o proxy monta (comando, pedido do usuário, extensão), e no fim a memória da
// conversa da sessão inteira (o último pedido de uma conversa carrega o histórico todo).
func maskSession(m *Masker, file string) sessionStats {
	var st sessionStats
	b, err := os.ReadFile(file)
	if err != nil {
		return st
	}
	type item struct {
		s, hint  string
		hostRole string // "user", "assistant" ou "tool"
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
	cs := NewCommands()
	cmds := map[string]string{}
	chs := map[string]*Call{}
	blocks := func(msg map[string]any) []map[string]any {
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
		for _, bl := range blocks(msg) {
			if bl["type"] == "tool_use" {
				id, _ := bl["id"].(string)
				cmds[id] = sessionCommand(bl["input"])
				cs.ArgumentText(cmds[id])
			}
		}
	}
	var items []item
	for _, msg := range msgs {
		hostRole, _ := msg["role"].(string)
		for _, bl := range blocks(msg) {
			switch bl["type"] {
			case "text":
				if t, ok := bl["text"].(string); ok {
					if hostRole == "user" {
						cs.User(t)
					}
					items = append(items, item{t, "", hostRole})
				}
			case "tool_use":
				id, _ := bl["id"].(string)
				chs[id] = cs.Usage(cmds[id], nil)
			case "tool_result":
				id, _ := bl["tool_use_id"].(string)
				output := strings.Join(resultTexts(bl["content"]), "\n")
				d := ""
				if cmd := cmds[id]; cmd != "" {
					d = cs.Hint(cmd, output)
				}
				d = WithExtension(d, cs.Result(chs[id], output))
				for _, x := range resultTexts(bl["content"]) {
					items = append(items, item{x, d, "tool"})
				}
			}
		}
		if hostRole == "assistant" {
			cs.EndTurn()
		}
	}
	var decs []Decision
	res := make([]result, len(items))
	for i, it := range items {
		res[i], _ = m.maskD(it.s, true, it.hint)
		decs = append(decs, res[i].decidedSet...)
	}
	st.texts = len(items)
	mm := m.newMemory(decs)
	st.byRule = map[string]int{}
	if mm == nil {
		return st
	}
	seen := map[string]bool{}
	for _, d := range decs {
		if d.Generic || len(d.Name) < memMin || identifierShape(d.Name) || seen[d.Key()] {
			continue
		}
		seen[d.Key()] = true
		st.commonsInMemory++
		st.byRule[d.Rule]++
	}
	for i, it := range items {
		if it.hostRole == "tool" {
			continue
		}
		n := 0
		scanWords(it.s, func(a, b int) { n++ })
		k := 0
		for _, t := range mm.scanF(it.s, res[i].snippets, filterOf(it.hint)) {
			k++
			if it.hostRole == "user" {
				if st.swapsByType == nil {
					st.swapsByType = map[string]int{}
				}
				shape := "comum"
				if identifierShape(it.s[t.Start:t.End]) {
					shape = "identificador"
				}
				st.swapsByType[strings.TrimPrefix(t.Type, objTypePrefix)+"/"+shape]++
			}
		}
		if it.hostRole == "user" {
			st.userWords += n
			st.userSwaps += k
		} else {
			st.assistWords += n
			st.assistSwaps += k
		}
	}
	return st
}

func asSlice(v any) []any { x, _ := v.([]any); return x }

// sessionCommand: como o proxy (commandOf): o comando do shell, ou as strings da entrada.
func sessionCommand(v any) string {
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

func resultTexts(c any) []string {
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
func shapes(vs, pub map[string]bool) string {
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
