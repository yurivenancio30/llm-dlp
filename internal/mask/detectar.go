package mask

import (
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// Detect: junta os detectores. Text comum é examinado inteiro; texto grande, em pedaços
// paralelos.

// Detect: texto comum é examinado inteiro; texto grande, em pedaços paralelos.

// Detect devolve os trechos sensíveis de s (sem sobreposição resolvida).
func (m *Masker) Detect(s string) []Finding { return m.detect(s, true) }

// detectar com aprende=false não lembra nada do que achar (conteúdo da internet): só
// mascara onde aparece.
func (m *Masker) detect(s string, learn bool) []Finding { return m.detectD(s, learn, nil) }

// detectD: com a dica do comando que produziu o texto (nil = sem dica).
func (m *Masker) detectD(s string, learn bool, d *outputHint) []Finding {
	out, _ := m.detectDE(s, learn, d, "")
	return out
}

// detectDE: também com a extensão da dica (para os decisores); devolve os nomes decididos.
func (m *Masker) detectDE(s string, learn bool, d *outputHint, ext string) ([]Finding, []Decision) {
	if len(s) > largeMin && !longBlock(s, largeMargin) {
		return m.detectLargeE(s, learn, d, ext)
	}
	return m.detectWholeE(s, learn, d, ext)
}

// Text grande é examinado em pedaços, em paralelo. Cada pedaço é examinado junto com uma
// margem do texto vizinho dos dois lados, e só valem os achados que COMEÇAM dentro do
// pedaço: assim, o que fica em cima de um corte (uma chave privada de várias linhas, um
// número com a palavra-chave logo antes) é visto inteiro por um dos dois lados.
//
// Os cortes e as bordas das janelas caem sempre num espaço em branco: uma "palavra" (uma
// senha, um token, um bloco de base64, por maior que seja) nunca é dividida. Assim, só um
// dado com espaços no meio E maior que a margem poderia ficar em cima de um corte; o único
// desse tipo é o bloco "-----BEGIN ... -----END", e texto com um bloco maior que a margem é
// examinado inteiro, de uma vez.
//
// São variáveis só para os testes poderem usar tamanhos pequenos.
var (
	largeMin    = 96 << 10 // acima disso, em pedaços
	largePiece  = 48 << 10
	largeMargin = 8 << 10 // maior que os dados sensíveis de várias linhas (chave RSA 4096 ≈ 3 KB)
)

// longBlock: há um bloco "-----BEGIN" cujo "-----END" está a mais de limite bytes (ou falta)?
func longBlock(s string, limitN int) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], "-----BEGIN")
		if j < 0 {
			return false
		}
		i += j + 10
		k := strings.Index(s[i:], "-----END")
		if k < 0 || k+40 > limitN {
			return true
		}
	}
}

// detectWhole examina s de uma vez só.
func (m *Masker) detectWhole(s string) []Finding { return m.detectWholeA(s, true) }

func (m *Masker) detectWholeA(s string, learn bool) []Finding {
	return m.detectWholeD(s, learn, nil)
}

func (m *Masker) detectWholeD(s string, learn bool, d *outputHint) []Finding {
	out, _ := m.detectWholeE(s, learn, d, "")
	return out
}

func (m *Masker) detectWholeE(s string, learn bool, d *outputHint, ext string) ([]Finding, []Decision) {
	out := m.detectBase(s)
	out = append(out, m.findStructure(s, learn, out, d)...)
	newSet, dec := m.runDeciders(s, out, learn, ext)
	dec = append(findingDecisions(out), dec...) // as dos leitores (antes dos achados dos decisores)
	out = append(out, newSet...)
	// Values que dependem de contexto são lembrados e reconhecidos depois em qualquer
	// lugar (ver memoria_conhecidos.go): sem isto, vazariam quando o modelo os repete sem a
	// palavra-chave por perto e o histórico é reenviado.
	for _, a := range out {
		if learn {
			m.learnIt(a.Type, a.Real)
		}
	}
	longNum, _ := numericProfile(s)
	m.findKnown(s, longNum, func(ini, end int, typ string) {
		out = append(out, Finding{ini, end, typ, s[ini:end]})
	})
	m.unifyObjects(out)
	return out, dec
}

func blank(b byte) bool { return b == ' ' || b == '\n' || b == '\t' || b == '\r' }

// detectLarge examina s em pedaços paralelos (ver o comentário acima).
func (m *Masker) detectLarge(s string, learn bool, d *outputHint) []Finding {
	out, _ := m.detectLargeE(s, learn, d, "")
	return out
}

func (m *Masker) detectLargeE(s string, learn bool, d *outputHint, ext string) ([]Finding, []Decision) {
	type piece struct{ ini, end, jStart, jEnd int } // miolo [ini,fim) e janela [jStart,jEnd)
	// depois: primeira posição >= i que vem logo depois de um espaço em branco (ou o fim)
	after := func(i int) int {
		for i < len(s) && !(i > 0 && blank(s[i-1])) {
			i++
		}
		return i
	}
	// antes: última posição <= i que vem logo depois de um espaço em branco (ou o começo)
	before := func(i int) int {
		for i > 0 && !blank(s[i-1]) {
			i--
		}
		return i
	}
	var ps []piece
	for ini := 0; ini < len(s); {
		end := ini + largePiece
		if end >= len(s) {
			end = len(s)
		} else if k := strings.LastIndexByte(s[ini:end], '\n'); k > largePiece/2 {
			end = ini + k + 1 // de preferência, numa quebra de linha
		} else {
			end = after(end) // senão, no próximo espaço: uma "palavra" nunca é cortada
		}
		ps = append(ps, piece{ini, end, before(max(0, ini-largeMargin)), after(min(len(s), end+largeMargin))})
		ini = end
	}
	if len(ps) < 2 {
		return m.detectWholeE(s, learn, d, ext)
	}
	runIt := func(f func(window string, add func(ini, end int, typ string))) []Finding {
		res := make([][]Finding, len(ps))
		var next atomic.Int64
		var wg sync.WaitGroup
		for w := 0; w < min(runtime.GOMAXPROCS(0), len(ps)); w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					i := int(next.Add(1)) - 1
					if i >= len(ps) {
						return
					}
					p := ps[i]
					f(s[p.jStart:p.jEnd], func(ini, end int, typ string) {
						if a := p.jStart + ini; a >= p.ini && a < p.end {
							res[i] = append(res[i], Finding{a, p.jStart + end, typ, s[a : p.jStart+end]})
						}
					})
				}
			}()
		}
		wg.Wait()
		var out []Finding
		for _, r := range res {
			out = append(out, r...)
		}
		return out
	}
	out := runIt(func(window string, add func(ini, end int, typ string)) {
		for _, a := range m.detectBase(window) {
			add(a.Start, a.End, a.Type)
		}
	})
	// estrutura (objetos) e tabelas: no texto inteiro (uma estrutura pode passar de um pedaço)
	out = append(out, m.findStructure(s, learn, out, d)...)
	newSet, dec := m.runDeciders(s, out, learn, ext)
	dec = append(findingDecisions(out), dec...) // as dos leitores (antes dos achados dos decisores)
	out = append(out, newSet...)
	// primeiro aprende TUDO, depois procura os valores conhecidos no texto inteiro: um valor
	// ensinado no fim do texto é reconhecido também no começo
	for _, a := range out {
		if learn {
			m.learnIt(a.Type, a.Real)
		}
	}
	out = append(out, runIt(func(window string, add func(ini, end int, typ string)) {
		longNum, _ := numericProfile(window)
		m.findKnown(window, longNum, add)
	})...)
	m.unifyObjects(out)
	return out, dec
}

// findStructure: os leitores de estrutura e de tabela, no texto sem o transporte (ver
// normalizacao.go), com os achados de volta nas posições do original, e no original quando a
// normalização tirou algo que traz nome. base: os achados dos detectores (para namesInLine).
// d: a dica do comando que produziu o texto (chamada_comando.go), ou nil. Depois dos leitores, as
// listas homogêneas (objetos_listas.go) usam o que já se sabe para tipar os itens que faltam.
func (m *Masker) findStructure(s string, learn bool, base []Finding, d *outputHint) []Finding {
	readIt := func(s string, base []Finding) []Finding {
		var out []Finding
		add := func(ini, end int, typ string) { out = append(out, Finding{ini, end, typ, s[ini:end]}) }
		m.findObjects(s, learn, add)
		if d != nil && m.cfg.Objects.Enabled {
			m.runReader(Reader{Name: "comando", Public: publicHint,
				Find: func(s string, add func(ObjFinding)) { findWithHint(s, d, add) }}, s, learn, add)
		}
		m.findLists(s, learn, add)
		if !m.cfg.Disabled("campo") {
			m.findTables(s, add)
			out = append(out, namesInLine(s, append(base[:len(base):len(base)], out...))...)
		}
		return out
	}
	n, ok := normalize(s)
	if !ok {
		return readIt(s, base)
	}
	var out []Finding
	if n.alsoRaw {
		out = readIt(s, base)
	}
	var bt []Finding // os achados dos detectores, nas posições do texto limpo
	for _, a := range base {
		if la, lb, ok := n.cleaned(s, a.Start, a.End); ok {
			bt = append(bt, Finding{la, lb, a.Type, a.Real})
		}
	}
	for _, a := range readIt(n.t, bt) {
		if oa, ob, ok := n.original(s, a.Start, a.End); ok {
			out = append(out, Finding{oa, ob, a.Type, s[oa:ob]})
		}
	}
	return out
}
