package mask

import (
	"sort"
	"strconv"
	"strings"
)

// Decider das saídas de comando e das entradas de tool_use (ver chamada.go e docs/pt-BR/estruturas.md):
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
	registerDecider(Decider{Name: "chamada", Decide: decideCall})
}

// callExt: a extensão da dica lida (chamada.go).
type callExt struct {
	output bool
	typ    string
	prog   map[uint32]bool
	names  map[uint32]string // x e e: hash -> tipo
}

func readCallExt(s string) (callExt, bool) {
	var e callExt
	if s == "" || s[0] != 's' && s[0] != 'u' {
		return e, false
	}
	e.output = s[0] == 's'
	for _, f := range strings.Split(s[1:], ";") {
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			e.typ = v
		case "p":
			e.prog = make(map[uint32]bool, strings.Count(v, ",")+1)
			for _, h := range strings.Split(v, ",") {
				if n, err := strconv.ParseUint(h, 16, 32); err == nil {
					e.prog[uint32(n)] = true
				}
			}
		case "x", "e":
			if e.names == nil {
				e.names = map[uint32]string{}
			}
			for _, p := range strings.Split(v, ",") {
				ent, h, ok := strings.Cut(p, ":")
				if n, err := strconv.ParseUint(h, 16, 32); ok && err == nil {
					e.names[uint32(n)] = ent
				}
			}
		}
	}
	return e, true
}

// maxCallText: acima disso, as regras desta seção não rodam (a saída inteira de um
// comando raramente passa disso; o resto das regras continua valendo).
const maxCallText = 8 << 20

func decideCall(c *TextCtx) {
	e, ok := readCallExt(c.Ext)
	if !ok || len(c.S) > maxCallText {
		return
	}
	s := c.S
	cov := coverage(c.Findings)
	// B1 exceção e B4 eco: as palavras de nome da chamada, onde aparecem inteiras
	if len(e.names) > 0 {
		scanWords(s, func(a, b int) {
			if ent, ok := e.names[hpal(s[a:b])]; ok && !cov.covers(a, b) {
				c.Decide(a, b, knownEnt(c, s[a:b], ent), "eco")
			}
		})
	}
	segments(c, cov)
	if e.output {
		identity(c, e, cov)
	}
}

// knownEnt: o tipo com que v já é nome, se é; senão ent.
func knownEnt(c *TextCtx, v, ent string) string {
	if k, ok := c.Known(v); ok {
		return k
	}
	return ent
}

// ---- cobertura: os trechos já achados ----------------------------------------------------

type ranges [][2]int

func coverage(as []Finding) ranges {
	if len(as) == 0 {
		return nil
	}
	fs := make(ranges, 0, len(as))
	for _, a := range as {
		fs = append(fs, [2]int{a.Start, a.End})
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
func (fs ranges) covers(a, b int) bool {
	i := sort.Search(len(fs), func(i int) bool { return fs[i][1] > a })
	return i < len(fs) && fs[i][0] < b
}

// ---- freios ------------------------------------------------------------------------------

// decidableWord: v pode ser um nome (os freios comuns a eco, identidade e segmento).
func decidableWord(v string) bool {
	if len(v) < 3 || len(v) > 128 || v[0] >= '0' && v[0] <= '9' {
		return false // número, data, hora, duração (12d, 3h4m), tamanho (10Mi)
	}
	letter := false
	for i := 0; i < len(v); i++ {
		if c := v[i]; c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80 {
			letter = true
			break
		}
	}
	if !letter {
		return false
	}
	ident := identifierShape(v)
	if !ident && len(v) < 4 {
		return false
	}
	if hasDigit(v) && reVersionOrHash.MatchString(v) || isDataType(v) || publicList(v) || isPseudoObj(v) {
		return false
	}
	l := strings.ToLower(v)
	if typeWords[l] || l == "null" || l == "none" || l == "true" || l == "false" || l == "nil" {
		return false
	}
	if k := strings.LastIndexByte(v, '.'); k > 0 && fileExtensions[strings.ToLower(v[k+1:])] {
		return false
	}
	return true
}

func numeric(v string) bool { return v != "" && v[0] >= '0' && v[0] <= '9' }

// fieldLabel: a palavra que termina em s[b] é o rótulo de um "campo: valor" ou
// "campo=valor" (JSON, YAML, .env, log, dict do Python): o nome do campo, nunca o nome do
// registro. Não são rótulo "x::y", "x://", "x==y" nem "host:5432" e "12:30" (porta e hora).
func fieldLabel(s string, b int) bool {
	if b < len(s) && (s[b] == '"' || s[b] == '\'') {
		b++
	}
	for b < len(s) && (s[b] == ' ' || s[b] == '\t') {
		b++
	}
	if b >= len(s) {
		return false
	}
	var next byte
	if b+1 < len(s) {
		next = s[b+1]
	}
	switch s[b] {
	case ':':
		return next != ':' && next != '/' && !(next >= '0' && next <= '9')
	case '=':
		return next != '='
	}
	return false
}

// ---- segmentos de um nome já mascarado (B4) ---------------------------------------------

// segmentos: a palavra que é segmento de um nome de objeto já achado no texto ("payments" em
// payments-api-7d9f, registry/acme/payments:1.2) e que aparece também solta no mesmo texto é
// nome, do tipo do nome em que está. Fora de comentário e de texto corrido, e nunca a partir de
// um nome de coluna.
func segments(c *TextCtx, cov ranges) {
	var sgms map[string]string
	for _, a := range c.Findings {
		if !isObject(a.Type) || !strings.ContainsAny(a.Real, "-_./:") {
			continue
		}
		ent := strings.TrimPrefix(a.Type, objTypePrefix)
		if ent == "coluna" || ent == "indice" {
			continue // os pedaços de um nome de coluna (cd_cliente) são vocabulário do modelo de dados, não recurso
		}
		for _, p := range strings.FieldsFunc(a.Real, func(r rune) bool { return r < 0x80 && !letterD(byte(r)) }) {
			if len(p) >= 4 && p != a.Real && decidableWord(p) {
				if sgms == nil {
					sgms = map[string]string{}
				}
				if _, ok := sgms[p]; !ok {
					sgms[p] = ent
				}
			}
		}
	}
	if len(sgms) == 0 {
		return
	}
	s := c.S
	scanWords(s, func(a, b int) {
		if ent, ok := sgms[s[a:b]]; ok && !cov.covers(a, b) && !inComment(s, a) && !inProse(s, a, b) {
			c.Decide(a, b, knownEnt(c, s[a:b], ent), "segmento")
		}
	})
}

// ---- identidade (B2, B3) ----------------------------------------------------------------

type regWord struct{ a, b int }

// registro: as palavras de uma linha.
type record struct {
	ps  []regWord
	hdr bool // vem antes de uma linha de traços (cabeçalho)
}

const (
	maxRegWords = 40
	maxRecords  = 50_000
)

func identity(c *TextCtx, e callExt, cov ranges) {
	s := c.S
	var block []record
	n0 := -1
	nReg := 0
	closeIt := func() {
		if len(block) >= minList {
			blockIdentity(c, e, cov, block)
		}
		block, n0 = nil, -1
	}
	ls := wrapLines(s)
	for k, l := range ls {
		if nReg++; nReg > maxRecords {
			break
		}
		if l[1]-l[0] > 4096 {
			closeIt()
			continue
		}
		var r record
		scanWords(s[l[0]:l[1]], func(a, b int) {
			if len(r.ps) <= maxRegWords {
				r.ps = append(r.ps, regWord{l[0] + a, l[0] + b})
			}
		})
		if len(r.ps) == 0 {
			continue // linha de traços, vazia: não interrompe
		}
		if len(r.ps) > maxRegWords {
			closeIt()
			continue
		}
		r.hdr = dashLine(s, ls, k+1)
		if n0 >= 0 && (len(r.ps) < n0-1 || len(r.ps) > n0+1) {
			closeIt()
		}
		if n0 < 0 {
			n0 = len(r.ps)
		}
		block = append(block, r)
	}
	closeIt()
}

// dashLine: a linha k só tem traços, "=", "+", "|" e espaços (e pelo menos 3 traços).
func dashLine(s string, ls [][2]int, k int) bool {
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
func uppercase(s string, ps []regWord) bool {
	for _, p := range ps {
		for i := p.a; i < p.b; i++ {
			if c := s[i]; c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
				return false
			}
		}
	}
	return true
}

// blockIdentity: as posições de identidade de um bloco e a decisão.
func blockIdentity(c *TextCtx, e callExt, cov ranges, block []record) {
	s := c.S
	// o cabeçalho (a primeira linha, antes de traços ou só em maiúsculas quando o resto não é)
	// não é registro
	if b0 := block[0]; b0.hdr || uppercase(s, b0.ps) && !uppercase(s, block[1].ps) {
		block = block[1:]
	}
	if len(block) < minList {
		return
	}
	anc := e.typ == "" // a âncora só importa sem tipo pedido
	// decidivel[r][k]: a palavra k do registro r pode ser nome (freios + proveniência)
	dec := make([][]bool, len(block))
	maxN := 0
	for i, r := range block {
		maxN = max(maxN, len(r.ps))
		dec[i] = make([]bool, len(r.ps))
		for k, p := range r.ps {
			v := s[p.a:p.b]
			if !decidableWord(v) || e.prog[hpal(v)] || fieldLabel(s, p.b) {
				continue
			}
			// unidade: a palavra logo depois de um número, separada por um espaço só ("2 days")
			if k > 0 && numeric(s[r.ps[k-1].a:r.ps[k-1].b]) && p.a == r.ps[k-1].b+1 && s[p.a-1] == ' ' {
				continue
			}
			dec[i][k] = true
		}
	}
	// duas leituras das posições: da esquerda (k) e da direita (n-1-k); com o mesmo número de
	// palavras em todas as linhas, são a mesma
	ident := [2][]bool{make([]bool, maxN), make([]bool, maxN)}
	// anchorPos: posição com valores repetidos em que metade ou mais já são nomes (coluna de um
	// catálogo: o mesmo nome se repete entre tabelas)
	anchorPos := [2][]bool{make([]bool, maxN), make([]bool, maxN)}
	nIdent, nRepeat, anchor := [2]int{}, [2]int{}, [2]bool{}
	vals := map[string]int{}
	for side := 0; side < 2; side++ {
		for pos := 0; pos < maxN; pos++ {
			clear(vals)
			n, nDec, nKnown, repeats := 0, 0, 0, false
			for i, r := range block {
				k := pos
				if side == 1 {
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
					repeats = true
				}
				if dec[i][k] {
					nDec++
				}
				if anc {
					if cov.covers(p.a, p.b) {
						nKnown++
					} else if _, ok := c.Known(v); ok {
						nKnown++
					}
				}
			}
			if n < minList {
				continue
			}
			switch {
			case repeats:
				nRepeat[side]++
				if nKnown*2 >= n {
					anchor[side] = true
					anchorPos[side][pos] = true
				}
			case nDec*2 >= n:
				ident[side][pos] = true
				nIdent[side]++
			default:
				if nKnown*2 >= n {
					anchor[side] = true
				}
			}
		}
	}
	// cara de identificador vinda dos dados, numa posição ancorada: duas pistas concordando,
	// sem exigir que os valores sejam únicos
	if anc {
		for side := 0; side < 2; side++ {
			for i, r := range block {
				for k, p := range r.ps {
					pos := k
					if side == 1 {
						pos = len(r.ps) - 1 - k
					}
					v := s[p.a:p.b]
					if pos >= maxN || !anchorPos[side][pos] || !dec[i][k] || !identifierShape(v) || cov.covers(p.a, p.b) {
						continue
					}
					dec[i][k] = false
					c.Decide(p.a, p.b, knownEnt(c, v, entOfPosition(c, block, side, pos)), "identidade")
				}
			}
		}
	}
	for side := 0; side < 2; side++ {
		// identidade de verdade: poucas posições distintas (uma tabela de registros tem uma ou
		// duas chaves; uma frase solta tem todas as palavras distintas)
		if nIdent[side] == 0 || nIdent[side] > max(1, nRepeat[side]) {
			continue
		}
		if e.typ == "" && !anchor[side] {
			continue
		}
		typ := e.typ
		if typ == "" {
			typ = genericEnt
		}
		for i, r := range block {
			for k, p := range r.ps {
				pos := k
				if side == 1 {
					pos = len(r.ps) - 1 - k
				}
				if !ident[side][pos] || !dec[i][k] || cov.covers(p.a, p.b) {
					continue
				}
				dec[i][k] = false // uma vez só
				c.Decide(p.a, p.b, knownEnt(c, s[p.a:p.b], typ), "identidade")
			}
		}
	}
}

// entOfPosition: o tipo mais comum entre os valores já conhecidos de uma posição do bloco (genérico
// se nenhum).
func entOfPosition(c *TextCtx, block []record, side, pos int) string {
	n := map[string]int{}
	for _, r := range block {
		k := pos
		if side == 1 {
			k = len(r.ps) - 1 - pos
		}
		if k < 0 || k >= len(r.ps) {
			continue
		}
		if e, ok := c.Known(c.S[r.ps[k].a:r.ps[k].b]); ok {
			n[e]++
		}
	}
	best, max := genericEnt, 0
	for e, k := range n {
		if k > max || k == max && e < best {
			best, max = e, k
		}
	}
	return best
}
