package mask

import (
	"sort"
	"strings"
)

// Freios e âncora da memória da conversa (ver docs/pt-BR/estruturas.md, seção Conhecer o nome).
//
//   - Programa: na saída de um comando, a palavra que está no texto do programa (o comando ou o
//     script que ele executa) não é trocada pela memória; a exceção é a palavra que o proxy
//     traduziu de um pseudônimo nesse comando (essa é nome, não programa). Uma regex
//     '.*cpf.*' que o script escreveu sai como o script a escreveu.
//   - Comentário e texto corrido: a memória não troca palavra comum (sem cara de identificador)
//     dentro de comentário (--, #, //, /* */ na linha, fora de aspas) na frase do usuário ou do
//     modelo; num dado (saída de ferramenta, arquivo lido), só a coluna e o índice ficam de
//     fora: o nome de recurso que a requisição provou continua nome (procedência). Em texto corrido (três ou mais palavras
//     de prosa seguidas), não troca a palavra comum que é nome de coluna ou de índice: é o
//     vocabulário do modelo de dados (a coluna "nome" de um catálogo não troca o "nome" de uma
//     frase), o mesmo motivo por que o vistos não propaga coluna. Name de recurso (namespace,
//     tabela, bucket...) decidido num inventário continua valendo na prosa.
//   - Âncora por posição: numa saída de comando com registros paralelos (3 ou mais linhas com o
//     mesmo número de palavras, ±1), uma posição em que dois ou mais valores distintos já são
//     nomes de um mesmo tipo é uma coluna desse tipo: os outros valores dela que podem ser nome
//     (decidableWord, fora do programa) ganham o tipo, e os que estavam com o tipo genérico
//     são corrigidos. É o que pega as colunas do catálogo que o head não mostrou.

// memFilter: o que a memória não pode trocar num texto.
type memFilter struct {
	prog map[uint32]bool // palavras do programa (a dica da saída)
	// saida: o texto é dado (saída de ferramenta, arquivo lido), não frase do usuário nem do
	// modelo. Aí vale a procedência (ver pula).
	output bool
}

// columnOrIndex: o vocabulário do modelo de dados, que a memória não leva para a prosa nem
// para outro arquivo (o mesmo motivo por que o vistos não propaga coluna).
func columnOrIndex(ent string) bool { return ent == "coluna" || ent == "indice" }

func filterOf(hint string) memFilter {
	_, ext := hintParts(hint)
	e, ok := readCallExt(ext)
	if !ok || !e.output {
		return memFilter{}
	}
	return memFilter{prog: e.prog}
}

// pula: a memória não troca s[a:b] (o nome nm) aqui.
func (f memFilter) skips(s string, a, b int, nm *memName) bool {
	v := s[a:b]
	if identifierShape(v) {
		// nome do cliente que o programa cita (o modelo o copiou de um dado) sai mascarado
		// também na saída dele; o que o modelo escreveu por conta própria já é público
		// (anterioridade)
		return false
	}
	if f.prog != nil && f.prog[hpal(v)] {
		return true
	}
	// procedência: num dado (saída de ferramenta, arquivo), o nome de recurso que esta
	// requisição já provou (é o que está na memória) continua nome dentro de um comentário; a
	// palavra comum só vale como palavra na frase do usuário ou do modelo
	if inComment(s, a) && !(f.output && !columnOrIndex(nm.ent)) {
		return true
	}
	return columnOrIndex(nm.ent) && inProse(s, a, b)
}

// inComment: s[a] está depois de um marcador de comentário na mesma linha (--, #, //, /*),
// que começa a linha ou vem depois de um espaço, fora de aspas. Não são comentário: "--x"
// colado numa letra ou dígito (opção de comando: --schema=x) e "#" seguido de dígito (#3).
func inComment(s string, a int) bool {
	ini := lineStartJ(s, a)
	l := s[ini:a]
	var quote byte
	for i := 0; i < len(l); i++ {
		c := l[i]
		if quote != 0 { // dentro de uma string: o marcador é texto
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' || c == '`' {
			quote = c
			continue
		}
		if i > 0 && l[i-1] != ' ' && l[i-1] != '\t' {
			continue
		}
		after := byte(' ') // o que vem depois do marcador (pode ser o próprio nome, em s[a])
		switch {
		case c == '#':
			if ini+i+1 < len(s) {
				after = s[ini+i+1]
			}
			if after < '0' || after > '9' {
				return true
			}
		case i+1 < len(l) && (l[i:i+2] == "//" || l[i:i+2] == "/*"):
			return true
		case i+1 < len(l) && l[i:i+2] == "--":
			if ini+i+2 < len(s) {
				after = s[ini+i+2]
			}
			if !isAlnum(after) {
				return true
			}
		}
	}
	return false
}

// inProse: s[a:b] está numa sequência de 3 ou mais palavras de letras separadas por um espaço
// (texto corrido), contando a própria.
func inProse(s string, a, b int) bool {
	letters := func(x, y int) bool {
		if y-x < 1 {
			return false
		}
		for i := x; i < y; i++ {
			if c := s[i]; !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80) {
				return false
			}
		}
		return true
	}
	n := 1
	// para trás
	for i, k := a, 0; k < 2; k++ {
		if i < 2 || s[i-1] != ' ' {
			break
		}
		j := i - 1
		x := j
		for x > 0 && s[x-1] != ' ' && s[x-1] != '\n' && j-x < 40 {
			x--
		}
		if !letters(x, j) {
			break
		}
		n++
		i = x
	}
	// para a frente (pontuação de frase depois da palavra ainda conta)
	for i, k := b, 0; k < 2; k++ {
		for i < len(s) && strings.IndexByte(",.;:!?", s[i]) >= 0 {
			i++
		}
		if i >= len(s) || s[i] != ' ' {
			break
		}
		x := i + 1
		y := x
		for y < len(s) && s[y] != ' ' && s[y] != '\n' && strings.IndexByte(",.;:!?", s[y]) < 0 && y-x < 40 {
			y++
		}
		if !letters(x, y) {
			break
		}
		n++
		i = y
	}
	return n >= 3
}

// anchorPositions: a âncora por posição (ver acima), sobre os trechos já decididos de s (em
// ordem). Devolve os trechos com os novos e os corrigidos (sem pseudônimo nos novos).
func (l *Batch) anchorPositions(s string, ts []snippet, f memFilter) []snippet {
	if f.prog == nil || len(ts) < 2 || strings.Count(s, "\n") < 2 || len(s) > maxCallText {
		return ts
	}
	// o trecho que cobre cada posição
	covers := func(a, b int) int {
		i, j := 0, len(ts)
		for i < j {
			h := (i + j) / 2
			if ts[h].End <= a {
				i = h + 1
			} else {
				j = h
			}
		}
		if i < len(ts) && ts[i].Start < b {
			return i
		}
		return -1
	}
	type wd = [2]int
	var newSet []snippet
	redo := map[int]string{} // trecho -> tipo novo
	ls := wrapLines(s)
	var block [][]wd
	n0 := -1
	closeIt := func() {
		if len(block) >= minList {
			newSet = append(newSet, l.anchorBlock(s, block, ts, covers, f, redo)...)
		}
		block, n0 = nil, -1
	}
	for k, ln := range ls {
		if k > maxRecords {
			break
		}
		var ps []wd
		if ln[1]-ln[0] <= 4096 {
			scanWords(s[ln[0]:ln[1]], func(a, b int) {
				if len(ps) <= maxRegWords {
					ps = append(ps, wd{ln[0] + a, ln[0] + b})
				}
			})
		}
		if len(ps) == 0 || len(ps) > maxRegWords {
			closeIt()
			continue
		}
		if n0 >= 0 && (len(ps) < n0-1 || len(ps) > n0+1) {
			closeIt()
		}
		if n0 < 0 {
			n0 = len(ps)
		}
		r := make([]wd, len(ps))
		copy(r, ps)
		block = append(block, r)
	}
	closeIt()
	// por campo: linhas seguidas com o mesmo separador na mesma quantidade (a contagem de
	// palavras pode variar dentro de um campo: "NUMBER(12,2)", uma lista de regex no fim)
	for _, b := range blocksByField(s, ls) {
		newSet = append(newSet, l.anchorBlock(s, b, ts, covers, f, redo)...)
	}
	if len(newSet) == 0 && len(redo) == 0 {
		return ts
	}
	newSet = noOverlap(newSet)
	out := make([]snippet, 0, len(ts)+len(newSet))
	for i, t := range ts {
		if tp, ok := redo[i]; ok {
			t.Type, t.Pseudo = tp, ""
		}
		out = append(out, t)
	}
	out = append(out, newSet...)
	sortSnippets(out)
	return out
}

func (l *Batch) anchorBlock(s string, block [][][2]int, ts []snippet, covers func(a, b int) int, f memFilter, redo map[int]string) []snippet {
	var newSet []snippet
	maxN := 0
	for _, r := range block {
		maxN = max(maxN, len(r))
	}
	used := map[int]bool{} // palavra (início) já decidida neste bloco
	for side := 0; side < 2; side++ {
		for pos := 0; pos < maxN; pos++ {
			at := func(r [][2]int) ([2]int, bool) {
				k := pos
				if side == 1 {
					k = len(r) - 1 - pos
				}
				if k < 0 || k >= len(r) {
					return [2]int{}, false
				}
				return r[k], true
			}
			// os tipos dos valores já decididos nesta posição (por valor distinto)
			byType := map[string]map[string]bool{}
			n := 0
			for _, r := range block {
				p, ok := at(r)
				if !ok || p[0] == p[1] {
					continue
				}
				n++
				// semente: o valor inteiro do campo (num nome qualificado, a última parte), com
				// prova direta na mesma fonte; dedução não é semente (ver memoria_rastreamento.go)
				if i := lastCovering(ts, p[0], p[1]); i >= 0 && ts[i].End == p[1] && isObject(ts[i].Type) && ts[i].Type != objTypePrefix+genericEnt &&
					!l.deduced(s[ts[i].Start:ts[i].End]) && l.provenAtSource(s[ts[i].Start:ts[i].End]) {
					if byType[ts[i].Type] == nil {
						byType[ts[i].Type] = map[string]bool{}
					}
					byType[ts[i].Type][s[p[0]:p[1]]] = true
				}
			}
			if n < minList {
				continue
			}
			typ, maxV, totV := "", 0, 0
			for tp, vs := range byType {
				totV += len(vs)
				if len(vs) > maxV || len(vs) == maxV && tp < typ {
					typ, maxV = tp, len(vs)
				}
			}
			if maxV < 2 || maxV*3 < totV*2 {
				continue // menos de dois valores conhecidos, ou tipos misturados
			}
			for _, r := range block {
				p, ok := at(r)
				if !ok || p[0] == p[1] || used[p[0]] {
					continue
				}
				if i := covers(p[0], p[1]); i >= 0 {
					if ts[i].Type == objTypePrefix+genericEnt && ts[i].Start == p[0] && ts[i].End == p[1] {
						redo[i] = typ
					}
					if ts[i].Start <= p[0] && ts[i].End >= p[1] || !strings.Contains(s[p[0]:p[1]], ".") {
						continue // coberto inteiro (de um nome qualificado, só as partes que faltam)
					}
				}
				v := s[p[0]:p[1]]
				if f.prog[hpal(v)] {
					continue
				}
				// nome qualificado (a.b): cada parte, com o tipo da posição no último pedaço
				ps := strings.Split(v, ".")
				ents := entQual(len(ps), strings.TrimPrefix(typ, objTypePrefix))
				a := p[0]
				for k, x := range ps {
					if decidableWord(x) && !publicSystem(x) && !f.prog[hpal(x)] && covers(a, a+len(x)) < 0 {
						newSet = append(newSet, snippet{Start: a, End: a + len(x), Type: objTypePrefix + ents[k]})
					}
					a += len(x) + 1
				}
				used[p[0]] = true
			}
		}
	}
	return newSet
}

// sortSnippets: em ordem de início.
func sortSnippets(ts []snippet) {
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].Start < ts[j].Start })
}

// blocksByField: os blocos de 3 ou mais linhas seguidas em que um separador (| TAB ; ,) aparece
// o mesmo número de vezes (1 ou mais); cada registro são os campos de uma palavra só (o campo
// com espaço no meio, ou vazio, fica como posição sem valor).
func blocksByField(s string, ls [][2]int) [][][][2]int {
	var out [][][][2]int
	for _, sep := range []byte{'|', '\t', ';', ','} {
		var block [][][2]int
		n0 := -1
		closeIt := func() {
			if len(block) >= minList {
				out = append(out, block)
			}
			block, n0 = nil, -1
		}
		for k, ln := range ls {
			if k > maxRecords {
				break
			}
			l := s[ln[0]:ln[1]]
			n := countOutsideQuotes(l, sep)
			if n == 0 || len(l) > 4096 || n > maxRegWords {
				closeIt()
				continue
			}
			if n0 >= 0 && n != n0 {
				closeIt()
			}
			n0 = n
			var r [][2]int
			a := ln[0]
			for i := ln[0]; i <= ln[1]; i++ {
				if i < ln[1] && (s[i] == '"' || s[i] == '\'') {
					if f := strings.IndexByte(s[i+1:ln[1]], s[i]); f >= 0 {
						i += f + 1 // separador dentro de aspas não conta
						continue
					}
				}
				if i < ln[1] && s[i] != sep {
					continue
				}
				x, y := a, i
				for x < y && (s[x] == ' ' || s[x] == '\t') {
					x++
				}
				for y > x && (s[y-1] == ' ' || s[y-1] == '\t' || s[y-1] == '\r') {
					y--
				}
				// o valor sem o que o embrulha: aspas, parênteses, colchetes
				for x < y && strings.IndexByte("'\"`([{", s[x]) >= 0 {
					x++
				}
				for y > x && strings.IndexByte("'\"`)]};", s[y-1]) >= 0 {
					y--
				}
				if x == y || strings.ContainsAny(s[x:y], " \t") {
					r = append(r, [2]int{x, x}) // campo vazio ou com frase: posição sem valor
				} else {
					r = append(r, [2]int{x, y})
				}
				a = i + 1
			}
			block = append(block, r)
		}
		closeIt()
	}
	return out
}

// noOverlap: os trechos novos em ordem, sem repetir nem sobrepor (fica o primeiro).
func noOverlap(ts []snippet) []snippet {
	sortSnippets(ts)
	out := ts[:0]
	for _, t := range ts {
		if len(out) > 0 && t.Start < out[len(out)-1].End {
			continue
		}
		out = append(out, t)
	}
	return out
}
