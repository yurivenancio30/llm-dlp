package mask

import (
	"regexp"
	"sort"
	"strings"
)

// Normalização de transporte: o conteúdo quase sempre chega embrulhado por um comando ou por
// uma ferramenta (número de linha da ferramenta de leitura, cat -n, grep -n, diff, citação de
// markdown, carimbo de log, cerca de código, cores ANSI, bordas de caixa, texto escapado de
// JSON, CRLF). Nenhum leitor trata isso, e cada um teria de tratar tudo: por isso é uma camada
// antes deles. Ela devolve o texto limpo e, para cada posição dele, a posição no original; os
// leitores rodam também no texto limpo e os achados voltam para o original (só os que
// correspondem byte a byte, para a troca e a volta ficarem exatas).

// textoNorm: o texto limpo e o mapa posição limpa -> posição original (len(t)+1 posições).
type textoNorm struct {
	t  string
	mp []int
	// o original também precisa ser lido: a normalização tirou algo que traz nome (o arquivo
	// do grep, o cabeçalho do diff) ou mudou a estrutura (JSON decodificado). Sem isso (número
	// de linha, carimbo de log, cores, bordas, CRLF), o texto limpo basta e o original não é lido.
	tambemCru bool
}

// original: o trecho [a, b) do texto limpo no original, se corresponde byte a byte.
func (n *textoNorm) original(s string, a, b int) (int, int, bool) {
	if a < 0 || b > len(n.t) || a >= b {
		return 0, 0, false
	}
	oa := n.mp[a]
	ob := oa + (b - a)
	if ob > len(s) || n.mp[b-1] != ob-1 || s[oa:ob] != n.t[a:b] {
		return 0, 0, false
	}
	return oa, ob, true
}

// limpo: o trecho [oa, ob) do original no texto limpo, se corresponde byte a byte.
func (n *textoNorm) limpo(s string, oa, ob int) (int, int, bool) {
	a := sort.SearchInts(n.mp[:len(n.t)], oa)
	if a >= len(n.t) || n.mp[a] != oa {
		return 0, 0, false
	}
	b := a + (ob - oa)
	if b > len(n.t) || n.t[a:b] != s[oa:ob] || n.mp[b-1] != ob-1 {
		return 0, 0, false
	}
	return a, b, true
}

// construtor de texto com mapa
type montador struct {
	b  []byte
	mp []int
}

func (m *montador) byte(c byte, de int) {
	m.b = append(m.b, c)
	m.mp = append(m.mp, de)
}

func (m *montador) trecho(s string, a, b int) {
	for i := a; i < b; i++ {
		m.byte(s[i], i)
	}
}

func (m *montador) fim(n int) (string, []int) { return string(m.b), append(m.mp, n) }

// compor: o mapa da etapa (posição na etapa -> posição na entrada da etapa) depois do mapa anterior.
func compor(ant, novo []int) []int {
	if ant == nil {
		return novo
	}
	for i, p := range novo {
		novo[i] = ant[p]
	}
	return novo
}

// normalizar: o texto sem o transporte. ok=false quando não há nada para tirar (o caso comum).
func normalizar(s string) (*textoNorm, bool) {
	if len(s) < 8 || !talvezTransporte(s) {
		return nil, false
	}
	t, mp := s, []int(nil)
	etapa := func(f func(string) (string, []int, bool)) {
		if u, m, ok := f(t); ok {
			t, mp = u, compor(mp, m)
		}
	}
	cru := false
	for k := 0; k < 3; k++ { // texto escapado dentro de texto escapado
		antes := t
		etapa(desescaparJSON)
		if t == antes {
			break
		}
		cru = true
	}
	etapa(tirarANSIeCaixa)
	antes := t
	etapa(tirarPrefixos)
	if t != antes && (reDiffCab.MatchString(antes) || strings.Contains(antes, "\n@@ ") || reGrepArq.MatchString(antes)) {
		cru = true
	}
	if mp == nil || t == s {
		return nil, false
	}
	return &textoNorm{t, mp, cru}, true
}

// talvezTransporte: teste barato antes do trabalho (o texto comum não paga nada).
func talvezTransporte(s string) bool {
	if strings.Contains(s, `\n`) || strings.Contains(s, `\"`) || strings.IndexByte(s, 0x1b) >= 0 || strings.IndexByte(s, '\r') >= 0 ||
		strings.Contains(s, "\xe2\x94") || strings.Contains(s, "\xe2\x95") || strings.HasPrefix(s, "\ufeff") ||
		strings.Contains(s, "→") || strings.Contains(s, "```") || strings.Contains(s, "\n@@ ") || strings.HasPrefix(s, "@@ ") ||
		strings.Contains(s, "\n+++ ") || strings.Contains(s, "\n> ") || strings.HasPrefix(s, "> ") {
		return true
	}
	// prefixo numérico ou de arquivo nas primeiras linhas (cat -n, grep -n, blame, log)
	n, com := 0, 0
	for i := 0; i < len(s) && n < 6; n++ {
		j := strings.IndexByte(s[i:], '\n')
		f := len(s)
		if j >= 0 {
			f = i + j
		}
		if _, ok := prefixoLinha(s[i:f]); ok {
			com++
		}
		if j < 0 {
			break
		}
		i = f + 1
	}
	return com > 0
}

// desescaparJSON: string de JSON com \n, \t, \" (saída de comando dentro de JSON, resultado de
// ferramenta serializado, célula de notebook) vira o texto decodificado. Cada string com
// quebra de linha escapada vira um bloco próprio (o resto, a estrutura do JSON, os leitores já
// leem no original); sem aspas em volta, o texto inteiro é decodificado.
func desescaparJSON(s string) (string, []int, bool) {
	// num objeto JSON, uma string de uma linha com aspas escapadas ("relation \"x\"") já basta
	pj := pareceJSON(s)
	if strings.Count(s, `\n`) < 2 && strings.Count(s, `\"`) < 4 && !(pj && (strings.Contains(s, `\n`) || strings.Count(s, `\"`) >= 2)) {
		return s, nil, false
	}
	var m montador
	m.b = make([]byte, 0, len(s))
	m.mp = make([]int, 0, len(s)+1)
	decodificar := func(a, b int) {
		for i := a; i < b; i++ {
			c := s[i]
			if c != '\\' || i+1 >= b {
				m.byte(c, i)
				continue
			}
			switch s[i+1] {
			case 'n':
				m.byte('\n', i)
			case 't':
				m.byte('\t', i)
			case 'r':
			case '"', '\\', '/', '\'':
				m.byte(s[i+1], i+1)
			case 'u': // \u003c: só os de ASCII (o resto fica como está)
				if i+5 < b && s[i+2] == '0' && s[i+3] == '0' && s[i+4] <= '7' && ehHex(s[i+4]) && ehHex(s[i+5]) {
					m.byte(byte(hexVal(s[i+4])<<4|hexVal(s[i+5])), i)
					i += 4
				} else {
					m.byte(c, i)
					continue
				}
			default:
				m.byte(c, i)
				continue
			}
			i++
		}
	}
	achou := false
	for i := 0; i < len(s); i++ {
		if s[i] != '"' {
			continue
		}
		j, nl, aspas := i+1, false, 0
		for ; j < len(s) && s[j] != '"' && s[j] != '\n'; j++ {
			if s[j] == '\\' && j+1 < len(s) {
				nl = nl || s[j+1] == 'n'
				if s[j+1] == '"' {
					aspas++
				}
				j++
			}
		}
		nl = nl || aspas >= 4 || pj && aspas >= 2
		if j >= len(s) || s[j] != '"' {
			break
		}
		if nl {
			decodificar(i+1, j)
			m.byte('\n', j)
			achou = true
		}
		i = j
	}
	if !achou {
		decodificar(0, len(s))
	}
	t, mp := m.fim(len(s))
	return t, mp, true
}

// pareceJSON: o texto é um objeto ou lista de JSON (uma só quebra de linha escapada já basta).
func pareceJSON(s string) bool {
	t := strings.TrimSpace(s)
	return len(t) > 2 && (t[0] == '{' || t[0] == '[') && strings.Contains(t, `":`)
}

var reANSI = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[@-Z\\-_])`)

// tirarANSIeCaixa: cores e movimentos do terminal saem; CR antes de LF sai; BOM sai; as bordas
// de caixa (desenho de tabela) viram "|" e "-", que os leitores de tabela já entendem.
func tirarANSIeCaixa(s string) (string, []int, bool) {
	if strings.IndexByte(s, 0x1b) < 0 && strings.IndexByte(s, '\r') < 0 && !strings.Contains(s, "\xe2\x94") &&
		!strings.Contains(s, "\xe2\x95") && !strings.HasPrefix(s, "\ufeff") {
		return s, nil, false
	}
	var ansi [][]int
	if strings.IndexByte(s, 0x1b) >= 0 {
		ansi = reANSI.FindAllStringIndex(s, -1)
	}
	var m montador
	m.b = make([]byte, 0, len(s))
	m.mp = make([]int, 0, len(s)+1)
	i := 0
	if strings.HasPrefix(s, "\ufeff") {
		i = 3
	}
	for i < len(s) {
		if len(ansi) > 0 && ansi[0][0] == i {
			i = ansi[0][1]
			ansi = ansi[1:]
			continue
		}
		for len(ansi) > 0 && ansi[0][0] < i {
			ansi = ansi[1:]
		}
		c := s[i]
		if c == '\r' && (i+1 == len(s) || s[i+1] == '\n') {
			i++
			continue
		}
		// U+2500..U+257F (desenho de caixa): E2 94 80..E2 95 BF
		if c == 0xe2 && i+2 < len(s) && (s[i+1] == 0x94 || s[i+1] == 0x95) {
			r := rune(s[i+1]-0x94)<<6 | rune(s[i+2]&0x3f) // 0..127 dentro do bloco
			switch r {
			case 0x00, 0x01, 0x04, 0x05, 0x08, 0x09, 0x4c, 0x4d, 0x50: // ─ ━ ┄ ┅ ┈ ┉ ╌ ╍ ═
				m.byte('-', i)
			default:
				m.byte('|', i)
			}
			i += 3
			continue
		}
		m.byte(c, i)
		i++
	}
	t, mp := m.fim(len(s))
	return t, mp, t != s
}

var (
	// número de linha da ferramenta de leitura ("   12→") e do cat -n / nl ("    12\t")
	reNumLinha = regexp.MustCompile(`^ *\d{1,7}(?:→|\t)`)
	// grep -n com e sem arquivo ("a/b.py:12:", "12:"), linha de contexto ("a/b.py-12-")
	reGrepN = regexp.MustCompile(`^(?:[^\s:]*[^\s:\d][^\s:]*(?::\d+:|-\d+-)|\d+[:-](?:\D|$))`)
	// git blame: "1a2b3c4d (Fulano 2024-01-02 10:11:12 -0300  12) "
	reBlame = regexp.MustCompile(`^\^?[0-9a-f]{7,40}(?: [^\s(]+)? \([^)]*\d{4}-\d\d-\d\d[^)]*?\d+\) `)
	// carimbo de log no começo da linha, com o nível depois
	reCarimbo = regexp.MustCompile(`^\[?\d{4}-\d\d-\d\d[T ]\d\d:\d\d:\d\d(?:[.,]\d+)?(?:Z|[+-]\d\d:?\d\d)?\]?(?:\s+\[?(?:TRACE|DEBUG|INFO|NOTICE|WARN|WARNING|ERROR|FATAL|CRITICAL)\]?)?[ \t]`)
	reGrepArq = regexp.MustCompile(`(?m)^[^\s:]*[^\s:\d][^\s:]*(?::\d+:|-\d+-)`)
	reDiffCab = regexp.MustCompile(`^(?:@@ .*@@|diff --git |index [0-9a-f]+\.\.|\+\+\+ |--- (?:a/|/dev/null|\S+\t)|new file mode|deleted file mode)`)
)

// prefixoLinha: o tamanho do prefixo de transporte da linha (sem diff, que depende do bloco).
func prefixoLinha(l string) (int, bool) {
	if l == "" {
		return 0, false
	}
	if c := l[0]; c == ' ' || c >= '0' && c <= '9' {
		if m := reNumLinha.FindStringIndex(l); m != nil {
			return m[1], true
		}
	}
	if m := reBlame.FindStringIndex(l); m != nil {
		return m[1], true
	}
	if l[0] >= '0' && l[0] <= '9' || l[0] == '[' {
		if m := reCarimbo.FindStringIndex(l); m != nil {
			return m[1], true
		}
	}
	if m := reGrepN.FindStringIndex(l); m != nil {
		return m[1], true
	}
	return 0, false
}

// tirarPrefixos: prefixos de linha (número, grep, blame, log, citação, diff) e cercas de código.
// Um prefixo de grep ou numérico só vale quando a maioria das linhas o tem (senão "12: x" de um
// YAML viraria prefixo); o de diff, quando há cabeçalho de diff no texto.
func tirarPrefixos(s string) (string, []int, bool) {
	ls := quebraLinhas(s)
	pref := make([]int, len(ls))
	n, com := 0, 0
	diff := false
	for k, l := range ls {
		linha := s[l[0]:l[1]]
		if strings.TrimSpace(linha) == "" {
			continue
		}
		n++
		if p, ok := prefixoLinha(linha); ok {
			pref[k] = p
			com++
		}
		if !diff && reDiffCab.MatchString(linha) {
			diff = true
		}
	}
	maioria := com > 0 && com*2 >= n
	mudou := false
	var m montador
	m.b = make([]byte, 0, len(s))
	m.mp = make([]int, 0, len(s)+1)
	for k, l := range ls {
		a, b := l[0], l[1]
		linha := s[a:b]
		if maioria && pref[k] > 0 {
			a += pref[k]
			mudou = true
		} else if maioria && linha == "--" { // separador de grupos do grep
			a = b
			mudou = true
		}
		linha = s[a:b]
		switch {
		case diff && reDiffCab.MatchString(linha):
			a, mudou = b, true
		case diff && linha != "" && (linha[0] == '+' || linha[0] == '-' || linha[0] == ' '):
			a, mudou = a+1, true
		case strings.HasPrefix(linha, "```") || strings.HasPrefix(strings.TrimLeft(linha, " "), "```"):
			a, mudou = b, true
		}
		for a < b && s[a] == '>' { // citação de markdown, inclusive aninhada
			a++
			if a < b && s[a] == ' ' {
				a++
			}
			mudou = true
		}
		e := b
		for e > a && (s[e-1] == ' ' || s[e-1] == '\t') {
			e--
		}
		if e != b {
			mudou = true
		}
		m.trecho(s, a, e)
		nl := l[1]
		if nl < len(s) && s[nl] == '\r' {
			nl++
			mudou = true
		}
		if nl < len(s) {
			m.byte('\n', nl)
		}
	}
	if !mudou {
		return s, nil, false
	}
	t, mp := m.fim(len(s))
	return t, mp, true
}

func ehHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func hexVal(c byte) byte {
	switch {
	case c >= 'a':
		return c - 'a' + 10
	case c >= 'A':
		return c - 'A' + 10
	}
	return c - '0'
}
