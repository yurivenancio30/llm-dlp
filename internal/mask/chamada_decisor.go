package mask

import (
	"sort"
	"strconv"
	"strings"
)

// Decisor das saídas de comando e das entradas de tool_use (ver chamada.go e docs/estruturas.md):
//
//   - B1 proveniência: palavra da saída que está no programa (o comando ou o script) não veio
//     dos dados e nunca é decidida aqui;
//   - B2 identidade: nos blocos de 3 ou mais registros paralelos (linhas com o mesmo número de
//     palavras, ±1), a posição em que todos os valores são distintos, ao lado de posições com
//     valores repetidos, é a identidade do registro; uma lista (uma palavra por linha, todas
//     distintas) também é;
//   - B3: a palavra de identidade que veio dos dados é nome quando a saída é um inventário (o
//     pedido tem verbo de enumeração e substantivo, no comando ou na mensagem do usuário) ou
//     quando outra posição do bloco já é de nomes conhecidos (âncora);
//   - B4 eco: a palavra do tool_use que ecoa uma saída anterior em posição de argumento é nome;
//     e a palavra que é segmento de um nome já mascarado no mesmo texto também;
//   - B1 exceção: a palavra que o proxy traduziu de um pseudônimo é nome (no tool_use e na saída
//     dele).
//
// Freios: formas numéricas, datas, durações (e a unidade depois de um número: "2 days"),
// versões, hashes, tipos de dado, os vocabulários públicos que já existem e a palavra comum com
// menos de 4 letras. Tudo em uma passada por linha e consulta em tabela hash.

func init() {
	registrarDecisor(Decisor{Nome: "chamada", Decidir: decidirChamada})
}

// extChamada: a extensão da dica lida (chamada.go).
type extChamada struct {
	saida bool
	tipo  string
	prog  map[uint32]bool
	nomes map[uint32]string // x e e: hash -> tipo
}

func lerExtChamada(s string) (extChamada, bool) {
	var e extChamada
	if s == "" || s[0] != 's' && s[0] != 'u' {
		return e, false
	}
	e.saida = s[0] == 's'
	for _, f := range strings.Split(s[1:], ";") {
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			e.tipo = v
		case "p":
			e.prog = make(map[uint32]bool, strings.Count(v, ",")+1)
			for _, h := range strings.Split(v, ",") {
				if n, err := strconv.ParseUint(h, 16, 32); err == nil {
					e.prog[uint32(n)] = true
				}
			}
		case "x", "e":
			if e.nomes == nil {
				e.nomes = map[uint32]string{}
			}
			for _, p := range strings.Split(v, ",") {
				ent, h, ok := strings.Cut(p, ":")
				if n, err := strconv.ParseUint(h, 16, 32); ok && err == nil {
					e.nomes[uint32(n)] = ent
				}
			}
		}
	}
	return e, true
}

// maxTextoChamada: acima disso, as regras desta seção não rodam (a saída inteira de um
// comando raramente passa disso; o resto das regras continua valendo).
const maxTextoChamada = 8 << 20

func decidirChamada(c *TextoCtx) {
	e, ok := lerExtChamada(c.Ext)
	if !ok || len(c.S) > maxTextoChamada {
		return
	}
	s := c.S
	cob := cobertura(c.Achados)
	// B1 exceção e B4 eco: as palavras de nome da chamada, onde aparecem inteiras
	if len(e.nomes) > 0 {
		varrerPalavras(s, func(a, b int) {
			if ent, ok := e.nomes[hpal(s[a:b])]; ok && !cob.cobre(a, b) {
				c.Decidir(a, b, entConhecida(c, s[a:b], ent), "eco")
			}
		})
	}
	segmentos(c, cob)
	if e.saida {
		identidade(c, e, cob)
	}
}

// entConhecida: o tipo com que v já é nome, se é; senão ent.
func entConhecida(c *TextoCtx, v, ent string) string {
	if k, ok := c.Conhecido(v); ok {
		return k
	}
	return ent
}

// ---- cobertura: os trechos já achados ----------------------------------------------------

type faixas [][2]int

func cobertura(as []Achado) faixas {
	if len(as) == 0 {
		return nil
	}
	fs := make(faixas, 0, len(as))
	for _, a := range as {
		fs = append(fs, [2]int{a.Ini, a.Fim})
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i][0] < fs[j][0] })
	out := fs[:1]
	for _, f := range fs[1:] {
		u := &out[len(out)-1]
		if f[0] <= u[1] {
			if f[1] > u[1] {
				u[1] = f[1]
			}
			continue
		}
		out = append(out, f)
	}
	return out
}

// cobre: [a, b) cruza algum trecho já achado?
func (fs faixas) cobre(a, b int) bool {
	i := sort.Search(len(fs), func(i int) bool { return fs[i][1] > a })
	return i < len(fs) && fs[i][0] < b
}

// ---- freios ------------------------------------------------------------------------------

// palavraDecidivel: v pode ser um nome (os freios comuns a eco, identidade e segmento).
func palavraDecidivel(v string) bool {
	if len(v) < 3 || len(v) > 128 || v[0] >= '0' && v[0] <= '9' {
		return false // número, data, hora, duração (12d, 3h4m), tamanho (10Mi)
	}
	letra := false
	for i := 0; i < len(v); i++ {
		if c := v[i]; c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80 {
			letra = true
			break
		}
	}
	if !letra {
		return false
	}
	ident := caraDeIdentificador(v)
	if !ident && len(v) < 4 {
		return false
	}
	if temDigito(v) && reVersaoOuHash.MatchString(v) || ehTipoDado(v) || publicoLista(v) || ehPseudoObj(v) {
		return false
	}
	l := strings.ToLower(v)
	if palavrasTipo[l] || l == "null" || l == "none" || l == "true" || l == "false" || l == "nil" {
		return false
	}
	if k := strings.LastIndexByte(v, '.'); k > 0 && extensoesArquivo[strings.ToLower(v[k+1:])] {
		return false
	}
	return true
}

func numerico(v string) bool { return v != "" && v[0] >= '0' && v[0] <= '9' }

// ---- segmentos de um nome já mascarado (B4) ---------------------------------------------

// segmentos: a palavra que é segmento de um nome de objeto já achado no texto ("payments" em
// payments-api-7d9f, registry/acme/payments:1.2) e que aparece também solta no mesmo texto é
// nome, do tipo do nome em que está. Fora de comentário e de texto corrido, e nunca a partir de
// um nome de coluna.
func segmentos(c *TextoCtx, cob faixas) {
	var segs map[string]string
	for _, a := range c.Achados {
		if !ehObjeto(a.Tipo) || !strings.ContainsAny(a.Real, "-_./:") {
			continue
		}
		ent := strings.TrimPrefix(a.Tipo, prefTipoObj)
		if ent == "coluna" || ent == "indice" {
			continue // os pedaços de um nome de coluna (cd_cliente) são vocabulário do modelo de dados, não recurso
		}
		for _, p := range strings.FieldsFunc(a.Real, func(r rune) bool { return r < 0x80 && !letraD(byte(r)) }) {
			if len(p) >= 4 && p != a.Real && palavraDecidivel(p) {
				if segs == nil {
					segs = map[string]string{}
				}
				if _, ok := segs[p]; !ok {
					segs[p] = ent
				}
			}
		}
	}
	if len(segs) == 0 {
		return
	}
	s := c.S
	varrerPalavras(s, func(a, b int) {
		if ent, ok := segs[s[a:b]]; ok && !cob.cobre(a, b) && !emComentario(s, a) && !emProsa(s, a, b) {
			c.Decidir(a, b, entConhecida(c, s[a:b], ent), "segmento")
		}
	})
}

// ---- identidade (B2, B3) ----------------------------------------------------------------

type palavraReg struct{ a, b int }

// registro: as palavras de uma linha.
type registro struct {
	ps  []palavraReg
	cab bool // vem antes de uma linha de traços (cabeçalho)
}

const (
	maxPalavrasReg = 40
	maxRegistros   = 50_000
)

func identidade(c *TextoCtx, e extChamada, cob faixas) {
	s := c.S
	var bloco []registro
	n0 := -1
	nReg := 0
	fechar := func() {
		if len(bloco) >= minLista {
			identidadeBloco(c, e, cob, bloco)
		}
		bloco, n0 = nil, -1
	}
	ls := quebraLinhas(s)
	for k, l := range ls {
		if nReg++; nReg > maxRegistros {
			break
		}
		if l[1]-l[0] > 4096 {
			fechar()
			continue
		}
		var r registro
		varrerPalavras(s[l[0]:l[1]], func(a, b int) {
			if len(r.ps) <= maxPalavrasReg {
				r.ps = append(r.ps, palavraReg{l[0] + a, l[0] + b})
			}
		})
		if len(r.ps) == 0 {
			continue // linha de traços, vazia: não interrompe
		}
		if len(r.ps) > maxPalavrasReg {
			fechar()
			continue
		}
		r.cab = linhaDeTracos(s, ls, k+1)
		if n0 >= 0 && (len(r.ps) < n0-1 || len(r.ps) > n0+1) {
			fechar()
		}
		if n0 < 0 {
			n0 = len(r.ps)
		}
		bloco = append(bloco, r)
	}
	fechar()
}

// linhaDeTracos: a linha k só tem traços, "=", "+", "|" e espaços (e pelo menos 3 traços).
func linhaDeTracos(s string, ls [][2]int, k int) bool {
	if k >= len(ls) {
		return false
	}
	n := 0
	for i := ls[k][0]; i < ls[k][1]; i++ {
		switch s[i] {
		case '-', '=':
			n++
		case ' ', '\t', '+', '|', ':':
		default:
			return false
		}
	}
	return n >= 3
}

// maiusculas: todas as palavras só com maiúsculas (NAME STATUS AGE).
func maiusculas(s string, ps []palavraReg) bool {
	for _, p := range ps {
		for i := p.a; i < p.b; i++ {
			if c := s[i]; c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
				return false
			}
		}
	}
	return true
}

// identidadeBloco: as posições de identidade de um bloco e a decisão.
func identidadeBloco(c *TextoCtx, e extChamada, cob faixas, bloco []registro) {
	s := c.S
	// o cabeçalho (a primeira linha, antes de traços ou só em maiúsculas quando o resto não é)
	// não é registro
	if b0 := bloco[0]; b0.cab || maiusculas(s, b0.ps) && !maiusculas(s, bloco[1].ps) {
		bloco = bloco[1:]
	}
	if len(bloco) < minLista {
		return
	}
	anc := e.tipo == "" // a âncora só importa sem tipo pedido
	// decidivel[r][k]: a palavra k do registro r pode ser nome (freios + proveniência)
	dec := make([][]bool, len(bloco))
	maxN := 0
	for i, r := range bloco {
		maxN = max(maxN, len(r.ps))
		dec[i] = make([]bool, len(r.ps))
		for k, p := range r.ps {
			v := s[p.a:p.b]
			if !palavraDecidivel(v) || e.prog[hpal(v)] {
				continue
			}
			// unidade: a palavra logo depois de um número, separada por um espaço só ("2 days")
			if k > 0 && numerico(s[r.ps[k-1].a:r.ps[k-1].b]) && p.a == r.ps[k-1].b+1 && s[p.a-1] == ' ' {
				continue
			}
			dec[i][k] = true
		}
	}
	// duas leituras das posições: da esquerda (k) e da direita (n-1-k); com o mesmo número de
	// palavras em todas as linhas, são a mesma
	ident := [2][]bool{make([]bool, maxN), make([]bool, maxN)}
	// posAnc: posição com valores repetidos em que metade ou mais já são nomes (coluna de um
	// catálogo: o mesmo nome se repete entre tabelas)
	posAnc := [2][]bool{make([]bool, maxN), make([]bool, maxN)}
	nIdent, nRep, ancora := [2]int{}, [2]int{}, [2]bool{}
	vals := map[string]int{}
	for lado := 0; lado < 2; lado++ {
		for pos := 0; pos < maxN; pos++ {
			clear(vals)
			n, nDec, nConh, repete := 0, 0, 0, false
			for i, r := range bloco {
				k := pos
				if lado == 1 {
					k = len(r.ps) - 1 - pos
				}
				if k < 0 || k >= len(r.ps) {
					continue
				}
				p := r.ps[k]
				v := s[p.a:p.b]
				n++
				vals[v]++
				if vals[v] > 1 {
					repete = true
				}
				if dec[i][k] {
					nDec++
				}
				if anc {
					if cob.cobre(p.a, p.b) {
						nConh++
					} else if _, ok := c.Conhecido(v); ok {
						nConh++
					}
				}
			}
			if n < minLista {
				continue
			}
			switch {
			case repete:
				nRep[lado]++
				if nConh*2 >= n {
					ancora[lado] = true
					posAnc[lado][pos] = true
				}
			case nDec*2 >= n:
				ident[lado][pos] = true
				nIdent[lado]++
			default:
				if nConh*2 >= n {
					ancora[lado] = true
				}
			}
		}
	}
	// cara de identificador vinda dos dados, numa posição ancorada: duas pistas concordando,
	// sem exigir que os valores sejam únicos
	if anc {
		for lado := 0; lado < 2; lado++ {
			for i, r := range bloco {
				for k, p := range r.ps {
					pos := k
					if lado == 1 {
						pos = len(r.ps) - 1 - k
					}
					v := s[p.a:p.b]
					if pos >= maxN || !posAnc[lado][pos] || !dec[i][k] || !caraDeIdentificador(v) || cob.cobre(p.a, p.b) {
						continue
					}
					dec[i][k] = false
					c.Decidir(p.a, p.b, entConhecida(c, v, entDaPosicao(c, bloco, lado, pos)), "identidade")
				}
			}
		}
	}
	for lado := 0; lado < 2; lado++ {
		// identidade de verdade: poucas posições distintas (uma tabela de registros tem uma ou
		// duas chaves; uma frase solta tem todas as palavras distintas)
		if nIdent[lado] == 0 || nIdent[lado] > max(1, nRep[lado]) {
			continue
		}
		if e.tipo == "" && !ancora[lado] {
			continue
		}
		tipo := e.tipo
		if tipo == "" {
			tipo = entGenerica
		}
		for i, r := range bloco {
			for k, p := range r.ps {
				pos := k
				if lado == 1 {
					pos = len(r.ps) - 1 - k
				}
				if !ident[lado][pos] || !dec[i][k] || cob.cobre(p.a, p.b) {
					continue
				}
				dec[i][k] = false // uma vez só
				c.Decidir(p.a, p.b, entConhecida(c, s[p.a:p.b], tipo), "identidade")
			}
		}
	}
}

// entDaPosicao: o tipo mais comum entre os valores já conhecidos de uma posição do bloco (genérico
// se nenhum).
func entDaPosicao(c *TextoCtx, bloco []registro, lado, pos int) string {
	n := map[string]int{}
	for _, r := range bloco {
		k := pos
		if lado == 1 {
			k = len(r.ps) - 1 - pos
		}
		if k < 0 || k >= len(r.ps) {
			continue
		}
		if e, ok := c.Conhecido(c.S[r.ps[k].a:r.ps[k].b]); ok {
			n[e]++
		}
	}
	melhor, max := entGenerica, 0
	for e, k := range n {
		if k > max || k == max && e < melhor {
			melhor, max = e, k
		}
	}
	return melhor
}
