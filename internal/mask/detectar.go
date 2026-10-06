package mask

import (
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// Detectar: junta os detectores. Texto comum é examinado inteiro; texto grande, em pedaços
// paralelos.

// Detectar: texto comum é examinado inteiro; texto grande, em pedaços paralelos.

// Detectar devolve os trechos sensíveis de s (sem sobreposição resolvida).
func (m *Masker) Detectar(s string) []Achado { return m.detectar(s, true) }

// detectar com aprende=false não lembra nada do que achar (conteúdo da internet): só
// mascara onde aparece.
func (m *Masker) detectar(s string, aprende bool) []Achado { return m.detectarD(s, aprende, nil) }

// detectarD: com a dica do comando que produziu o texto (nil = sem dica).
func (m *Masker) detectarD(s string, aprende bool, d *dicaSaida) []Achado {
	out, _ := m.detectarDE(s, aprende, d, "")
	return out
}

// detectarDE: também com a extensão da dica (para os decisores); devolve os nomes decididos.
func (m *Masker) detectarDE(s string, aprende bool, d *dicaSaida, ext string) ([]Achado, []Decisao) {
	if len(s) > grandeMin && !blocoLongo(s, margemGrande) {
		return m.detectarGrandeE(s, aprende, d, ext)
	}
	return m.detectarInteiroE(s, aprende, d, ext)
}

// Texto grande é examinado em pedaços, em paralelo. Cada pedaço é examinado junto com uma
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
	grandeMin    = 96 << 10 // acima disso, em pedaços
	pedacoGrande = 48 << 10
	margemGrande = 8 << 10 // maior que os dados sensíveis de várias linhas (chave RSA 4096 ≈ 3 KB)
)

// blocoLongo: há um bloco "-----BEGIN" cujo "-----END" está a mais de limite bytes (ou falta)?
func blocoLongo(s string, limite int) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], "-----BEGIN")
		if j < 0 {
			return false
		}
		i += j + 10
		k := strings.Index(s[i:], "-----END")
		if k < 0 || k+40 > limite {
			return true
		}
	}
}

// detectarInteiro examina s de uma vez só.
func (m *Masker) detectarInteiro(s string) []Achado { return m.detectarInteiroA(s, true) }

func (m *Masker) detectarInteiroA(s string, aprende bool) []Achado {
	return m.detectarInteiroD(s, aprende, nil)
}

func (m *Masker) detectarInteiroD(s string, aprende bool, d *dicaSaida) []Achado {
	out, _ := m.detectarInteiroE(s, aprende, d, "")
	return out
}

func (m *Masker) detectarInteiroE(s string, aprende bool, d *dicaSaida, ext string) ([]Achado, []Decisao) {
	out := m.detectarBase(s)
	out = append(out, m.acharEstrutura(s, aprende, out, d)...)
	novos, dec := m.rodarDecisores(s, out, aprende, ext)
	dec = append(decisoesDosAchados(out), dec...) // as dos leitores (antes dos achados dos decisores)
	out = append(out, novos...)
	// Valores que dependem de contexto são lembrados e reconhecidos depois em qualquer
	// lugar (ver conhecidos.go): sem isto, vazariam quando o modelo os repete sem a
	// palavra-chave por perto e o histórico é reenviado.
	for _, a := range out {
		if aprende {
			m.aprender(a.Tipo, a.Real)
		}
	}
	numLongo, _ := perfilNumerico(s)
	m.acharConhecidos(s, numLongo, func(ini, fim int, tipo string) {
		out = append(out, Achado{ini, fim, tipo, s[ini:fim]})
	})
	m.unificarObjetos(out)
	return out, dec
}

func branco(b byte) bool { return b == ' ' || b == '\n' || b == '\t' || b == '\r' }

// detectarGrande examina s em pedaços paralelos (ver o comentário acima).
func (m *Masker) detectarGrande(s string, aprende bool, d *dicaSaida) []Achado {
	out, _ := m.detectarGrandeE(s, aprende, d, "")
	return out
}

func (m *Masker) detectarGrandeE(s string, aprende bool, d *dicaSaida, ext string) ([]Achado, []Decisao) {
	type pedaco struct{ ini, fim, jIni, jFim int } // miolo [ini,fim) e janela [jIni,jFim)
	// depois: primeira posição >= i que vem logo depois de um espaço em branco (ou o fim)
	depois := func(i int) int {
		for i < len(s) && !(i > 0 && branco(s[i-1])) {
			i++
		}
		return i
	}
	// antes: última posição <= i que vem logo depois de um espaço em branco (ou o começo)
	antes := func(i int) int {
		for i > 0 && !branco(s[i-1]) {
			i--
		}
		return i
	}
	var ps []pedaco
	for ini := 0; ini < len(s); {
		fim := ini + pedacoGrande
		if fim >= len(s) {
			fim = len(s)
		} else if k := strings.LastIndexByte(s[ini:fim], '\n'); k > pedacoGrande/2 {
			fim = ini + k + 1 // de preferência, numa quebra de linha
		} else {
			fim = depois(fim) // senão, no próximo espaço: uma "palavra" nunca é cortada
		}
		ps = append(ps, pedaco{ini, fim, antes(max(0, ini-margemGrande)), depois(min(len(s), fim+margemGrande))})
		ini = fim
	}
	if len(ps) < 2 {
		return m.detectarInteiroE(s, aprende, d, ext)
	}
	rodar := func(f func(janela string, add func(ini, fim int, tipo string))) []Achado {
		res := make([][]Achado, len(ps))
		var prox atomic.Int64
		var wg sync.WaitGroup
		for w := 0; w < min(runtime.GOMAXPROCS(0), len(ps)); w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					i := int(prox.Add(1)) - 1
					if i >= len(ps) {
						return
					}
					p := ps[i]
					f(s[p.jIni:p.jFim], func(ini, fim int, tipo string) {
						if a := p.jIni + ini; a >= p.ini && a < p.fim {
							res[i] = append(res[i], Achado{a, p.jIni + fim, tipo, s[a : p.jIni+fim]})
						}
					})
				}
			}()
		}
		wg.Wait()
		var out []Achado
		for _, r := range res {
			out = append(out, r...)
		}
		return out
	}
	out := rodar(func(janela string, add func(ini, fim int, tipo string)) {
		for _, a := range m.detectarBase(janela) {
			add(a.Ini, a.Fim, a.Tipo)
		}
	})
	// estrutura (objetos) e tabelas: no texto inteiro (uma estrutura pode passar de um pedaço)
	out = append(out, m.acharEstrutura(s, aprende, out, d)...)
	novos, dec := m.rodarDecisores(s, out, aprende, ext)
	dec = append(decisoesDosAchados(out), dec...) // as dos leitores (antes dos achados dos decisores)
	out = append(out, novos...)
	// primeiro aprende TUDO, depois procura os valores conhecidos no texto inteiro: um valor
	// ensinado no fim do texto é reconhecido também no começo
	for _, a := range out {
		if aprende {
			m.aprender(a.Tipo, a.Real)
		}
	}
	out = append(out, rodar(func(janela string, add func(ini, fim int, tipo string)) {
		numLongo, _ := perfilNumerico(janela)
		m.acharConhecidos(janela, numLongo, add)
	})...)
	m.unificarObjetos(out)
	return out, dec
}

// acharEstrutura: os leitores de estrutura e de tabela, no texto sem o transporte (ver
// normalizacao.go), com os achados de volta nas posições do original, e no original quando a
// normalização tirou algo que traz nome. base: os achados dos detectores (para nomesNaLinha).
// d: a dica do comando que produziu o texto (comando.go), ou nil. Depois dos leitores, as
// listas homogêneas (listas.go) usam o que já se sabe para tipar os itens que faltam.
func (m *Masker) acharEstrutura(s string, aprende bool, base []Achado, d *dicaSaida) []Achado {
	ler := func(s string, base []Achado) []Achado {
		var out []Achado
		add := func(ini, fim int, tipo string) { out = append(out, Achado{ini, fim, tipo, s[ini:fim]}) }
		m.acharObjetos(s, aprende, add)
		if d != nil && m.cfg.Objetos.Ligado {
			m.rodarLeitor(Leitor{Nome: "comando", Publico: publicoDica,
				Achar: func(s string, add func(ObjAchado)) { acharComDica(s, d, add) }}, s, aprende, add)
		}
		m.acharListas(s, aprende, add)
		if !m.cfg.Desligado("campo") {
			m.acharTabelas(s, add)
			out = append(out, nomesNaLinha(s, append(base[:len(base):len(base)], out...))...)
		}
		return out
	}
	n, ok := normalizar(s)
	if !ok {
		return ler(s, base)
	}
	var out []Achado
	if n.tambemCru {
		out = ler(s, base)
	}
	var bt []Achado // os achados dos detectores, nas posições do texto limpo
	for _, a := range base {
		if la, lb, ok := n.limpo(s, a.Ini, a.Fim); ok {
			bt = append(bt, Achado{la, lb, a.Tipo, a.Real})
		}
	}
	for _, a := range ler(n.t, bt) {
		if oa, ob, ok := n.original(s, a.Ini, a.Fim); ok {
			out = append(out, Achado{oa, ob, a.Tipo, s[oa:ob]})
		}
	}
	return out
}
