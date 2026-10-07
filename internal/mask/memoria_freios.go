package mask

import (
	"sort"
	"strings"
)

// Freios e âncora da memória da conversa (ver docs/estruturas.md, seção Conhecer o nome).
//
//   - Programa: na saída de um comando, a palavra que está no texto do programa (o comando ou o
//     script que ele executa) não é trocada pela memória; a exceção é a palavra que o proxy
//     traduziu de um pseudônimo nesse comando (essa é nome, não programa). Uma regex
//     '.*cpf.*' que o script escreveu sai como o script a escreveu.
//   - Comentário e texto corrido: a memória não troca palavra comum (sem cara de identificador)
//     dentro de comentário (--, #, //, /* */ na linha). Em texto corrido (três ou mais palavras
//     de prosa seguidas), não troca a palavra comum que é nome de coluna ou de índice: é o
//     vocabulário do modelo de dados (a coluna "nome" de um catálogo não troca o "nome" de uma
//     frase), o mesmo motivo por que o vistos não propaga coluna. Nome de recurso (namespace,
//     tabela, bucket...) decidido num inventário continua valendo na prosa.
//   - Âncora por posição: numa saída de comando com registros paralelos (3 ou mais linhas com o
//     mesmo número de palavras, ±1), uma posição em que dois ou mais valores distintos já são
//     nomes de um mesmo tipo é uma coluna desse tipo: os outros valores dela que podem ser nome
//     (palavraDecidivel, fora do programa) ganham o tipo, e os que estavam com o tipo genérico
//     são corrigidos. É o que pega as colunas do catálogo que o head não mostrou.

// filtroMem: o que a memória não pode trocar num texto.
type filtroMem struct {
	prog map[uint32]bool // palavras do programa (a dica da saída)
}

func filtroDe(dica string) filtroMem {
	_, ext := partesDica(dica)
	e, ok := lerExtChamada(ext)
	if !ok || !e.saida {
		return filtroMem{}
	}
	return filtroMem{prog: e.prog}
}

// pula: a memória não troca s[a:b] (o nome nm) aqui.
func (f filtroMem) pula(s string, a, b int, nm *nomeMem) bool {
	v := s[a:b]
	if caraDeIdentificador(v) {
		// nome do cliente que o programa cita (o modelo o copiou de um dado) sai mascarado
		// também na saída dele; o que o modelo escreveu por conta própria já é público
		// (anterioridade)
		return false
	}
	if f.prog != nil && f.prog[hpal(v)] {
		return true
	}
	if emComentario(s, a) {
		return true
	}
	return (nm.ent == "coluna" || nm.ent == "indice") && emProsa(s, a, b)
}

// emComentario: s[a] está depois de um marcador de comentário na mesma linha (--, #, //, /*),
// que começa a linha ou vem depois de um espaço.
func emComentario(s string, a int) bool {
	ini := inicioLinhaJ(s, a)
	l := s[ini:a]
	for i := 0; i < len(l); i++ {
		if i > 0 && l[i-1] != ' ' && l[i-1] != '\t' {
			continue
		}
		switch {
		case l[i] == '#':
			return true
		case i+1 < len(l) && (l[i:i+2] == "--" || l[i:i+2] == "//" || l[i:i+2] == "/*"):
			return true
		}
	}
	return false
}

// emProsa: s[a:b] está numa sequência de 3 ou mais palavras de letras separadas por um espaço
// (texto corrido), contando a própria.
func emProsa(s string, a, b int) bool {
	letras := func(x, y int) bool {
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
		if !letras(x, j) {
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
		if !letras(x, y) {
			break
		}
		n++
		i = y
	}
	return n >= 3
}

// ancorarPosicoes: a âncora por posição (ver acima), sobre os trechos já decididos de s (em
// ordem). Devolve os trechos com os novos e os corrigidos (sem pseudônimo nos novos).
func (l *Lote) ancorarPosicoes(s string, ts []trecho, f filtroMem) []trecho {
	if f.prog == nil || len(ts) < 2 || strings.Count(s, "\n") < 2 || len(s) > maxTextoChamada {
		return ts
	}
	// o trecho que cobre cada posição
	cobre := func(a, b int) int {
		i, j := 0, len(ts)
		for i < j {
			h := (i + j) / 2
			if ts[h].Fim <= a {
				i = h + 1
			} else {
				j = h
			}
		}
		if i < len(ts) && ts[i].Ini < b {
			return i
		}
		return -1
	}
	type pal = [2]int
	var novos []trecho
	refazer := map[int]string{} // trecho -> tipo novo
	ls := quebraLinhas(s)
	var bloco [][]pal
	n0 := -1
	fechar := func() {
		if len(bloco) >= minLista {
			novos = append(novos, l.ancorarBloco(s, bloco, ts, cobre, f, refazer)...)
		}
		bloco, n0 = nil, -1
	}
	for k, ln := range ls {
		if k > maxRegistros {
			break
		}
		var ps []pal
		if ln[1]-ln[0] <= 4096 {
			varrerPalavras(s[ln[0]:ln[1]], func(a, b int) {
				if len(ps) <= maxPalavrasReg {
					ps = append(ps, pal{ln[0] + a, ln[0] + b})
				}
			})
		}
		if len(ps) == 0 || len(ps) > maxPalavrasReg {
			fechar()
			continue
		}
		if n0 >= 0 && (len(ps) < n0-1 || len(ps) > n0+1) {
			fechar()
		}
		if n0 < 0 {
			n0 = len(ps)
		}
		r := make([]pal, len(ps))
		copy(r, ps)
		bloco = append(bloco, r)
	}
	fechar()
	// por campo: linhas seguidas com o mesmo separador na mesma quantidade (a contagem de
	// palavras pode variar dentro de um campo: "NUMBER(12,2)", uma lista de regex no fim)
	for _, b := range blocosPorCampo(s, ls) {
		novos = append(novos, l.ancorarBloco(s, b, ts, cobre, f, refazer)...)
	}
	if len(novos) == 0 && len(refazer) == 0 {
		return ts
	}
	novos = semSobrepor(novos)
	out := make([]trecho, 0, len(ts)+len(novos))
	for i, t := range ts {
		if tp, ok := refazer[i]; ok {
			t.Tipo, t.Pseudo = tp, ""
		}
		out = append(out, t)
	}
	out = append(out, novos...)
	ordenarTrechos(out)
	return out
}

func (l *Lote) ancorarBloco(s string, bloco [][][2]int, ts []trecho, cobre func(a, b int) int, f filtroMem, refazer map[int]string) []trecho {
	var novos []trecho
	maxN := 0
	for _, r := range bloco {
		maxN = max(maxN, len(r))
	}
	usado := map[int]bool{} // palavra (início) já decidida neste bloco
	for lado := 0; lado < 2; lado++ {
		for pos := 0; pos < maxN; pos++ {
			at := func(r [][2]int) ([2]int, bool) {
				k := pos
				if lado == 1 {
					k = len(r) - 1 - pos
				}
				if k < 0 || k >= len(r) {
					return [2]int{}, false
				}
				return r[k], true
			}
			// os tipos dos valores já decididos nesta posição (por valor distinto)
			porTipo := map[string]map[string]bool{}
			n := 0
			for _, r := range bloco {
				p, ok := at(r)
				if !ok || p[0] == p[1] {
					continue
				}
				n++
				// semente: o valor inteiro do campo (num nome qualificado, a última parte), com
				// prova direta na mesma fonte; dedução não é semente (ver rastreamento.go)
				if i := ultimoQueCobre(ts, p[0], p[1]); i >= 0 && ts[i].Fim == p[1] && ehObjeto(ts[i].Tipo) && ts[i].Tipo != prefTipoObj+entGenerica &&
					!l.deduzido(s[ts[i].Ini:ts[i].Fim]) && l.provadoNaFonte(s[ts[i].Ini:ts[i].Fim]) {
					if porTipo[ts[i].Tipo] == nil {
						porTipo[ts[i].Tipo] = map[string]bool{}
					}
					porTipo[ts[i].Tipo][s[p[0]:p[1]]] = true
				}
			}
			if n < minLista {
				continue
			}
			tipo, maxV, totV := "", 0, 0
			for tp, vs := range porTipo {
				totV += len(vs)
				if len(vs) > maxV || len(vs) == maxV && tp < tipo {
					tipo, maxV = tp, len(vs)
				}
			}
			if maxV < 2 || maxV*3 < totV*2 {
				continue // menos de dois valores conhecidos, ou tipos misturados
			}
			for _, r := range bloco {
				p, ok := at(r)
				if !ok || p[0] == p[1] || usado[p[0]] {
					continue
				}
				if i := cobre(p[0], p[1]); i >= 0 {
					if ts[i].Tipo == prefTipoObj+entGenerica && ts[i].Ini == p[0] && ts[i].Fim == p[1] {
						refazer[i] = tipo
					}
					if ts[i].Ini <= p[0] && ts[i].Fim >= p[1] || !strings.Contains(s[p[0]:p[1]], ".") {
						continue // coberto inteiro (de um nome qualificado, só as partes que faltam)
					}
				}
				v := s[p[0]:p[1]]
				if f.prog[hpal(v)] {
					continue
				}
				// nome qualificado (a.b): cada parte, com o tipo da posição no último pedaço
				ps := strings.Split(v, ".")
				ents := entQual(len(ps), strings.TrimPrefix(tipo, prefTipoObj))
				a := p[0]
				for k, x := range ps {
					if palavraDecidivel(x) && !publicoSistema(x) && !f.prog[hpal(x)] && cobre(a, a+len(x)) < 0 {
						novos = append(novos, trecho{Ini: a, Fim: a + len(x), Tipo: prefTipoObj + ents[k]})
					}
					a += len(x) + 1
				}
				usado[p[0]] = true
			}
		}
	}
	return novos
}

// ordenarTrechos: em ordem de início.
func ordenarTrechos(ts []trecho) {
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].Ini < ts[j].Ini })
}

// blocosPorCampo: os blocos de 3 ou mais linhas seguidas em que um separador (| TAB ; ,) aparece
// o mesmo número de vezes (1 ou mais); cada registro são os campos de uma palavra só (o campo
// com espaço no meio, ou vazio, fica como posição sem valor).
func blocosPorCampo(s string, ls [][2]int) [][][][2]int {
	var out [][][][2]int
	for _, sep := range []byte{'|', '\t', ';', ','} {
		var bloco [][][2]int
		n0 := -1
		fechar := func() {
			if len(bloco) >= minLista {
				out = append(out, bloco)
			}
			bloco, n0 = nil, -1
		}
		for k, ln := range ls {
			if k > maxRegistros {
				break
			}
			l := s[ln[0]:ln[1]]
			n := contaForaAspas(l, sep)
			if n == 0 || len(l) > 4096 || n > maxPalavrasReg {
				fechar()
				continue
			}
			if n0 >= 0 && n != n0 {
				fechar()
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
			bloco = append(bloco, r)
		}
		fechar()
	}
	return out
}

// semSobrepor: os trechos novos em ordem, sem repetir nem sobrepor (fica o primeiro).
func semSobrepor(ts []trecho) []trecho {
	ordenarTrechos(ts)
	out := ts[:0]
	for _, t := range ts {
		if len(out) > 0 && t.Ini < out[len(out)-1].Fim {
			continue
		}
		out = append(out, t)
	}
	return out
}
