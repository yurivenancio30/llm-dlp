package mask

import "strings"

// Regra léxica para código e configuração (ver
// docs/pt-BR/estruturas.md). A maior parte já é feita pelos leitores de código (leitor_codigo.go) e
// de chave-valor (leitor_chave_valor.go), que ficam como estão:
//
//   - candidata: só o literal ou o valor INTEIRO, sem espaço (valorRecurso recusa espaço, então
//     "payments failed" é frase e fica); identificador de código nunca (o valor sem aspas de
//     "x = y" só vale em linha de configuração); comentário nunca (não há chave nele).
//   - pista da chave: o nome da chave contém um tipo de EntObjeto em qualquer grafia (entChave):
//     namespace = "payments", bucket: billing, TABLE_NAME=pedidos.
//
// Este arquivo acrescenta:
//
//   - seção de INI: "chave = valor" com espaços em volta do "=" é atribuição de código em
//     Python, Ruby, shell..., salvo quando a linha está num bloco que começa num cabeçalho de
//     seção "[nome]" (secaoINI, usado pelo leitor de chave-valor).
//   - âncora (decisor "léxica-âncora"): numa lista de literais ([..], {..} de Go/C#, lista YAML
//     "- x" debaixo de "chave:"), todos candidatos, em que um item já é nome aprendido (neste
//     texto, pelos leitores, ou antes), os outros itens são nomes do mesmo tipo. A lista é
//     homogênea; a tupla e os argumentos de uma chamada ("(...)") não contam, porque misturam
//     tipos; __all__ = [...] (Python) é lista de identificadores de código.
//
// O eco e a palavra que aparece como dado numa saída de comando vêm da memória da conversa e da
// proveniência (seções A e B), não daqui.

// secaoINI: a linha de s[i] está num bloco de INI: subindo, só linhas "chave = valor",
// comentários (";", "#") e linhas em branco até um cabeçalho "[seção]". Olha no máximo 64 linhas.
func secaoINI(s string, i int) bool {
	l := inicioLinhaJ(s, i)
	for n := 0; n < 64 && l > 0; n++ {
		f := l - 1 // o '\n' da linha de cima
		a := inicioLinhaJ(s, f)
		lin := strings.TrimSpace(s[a:f])
		switch {
		case lin == "" || lin[0] == ';' || lin[0] == '#':
		case lin[0] == '[':
			return cabecalhoINI(lin)
		case linhaChaveIgual(lin):
		default:
			return false
		}
		l = a
	}
	return false
}

// cabecalhoINI: "[seção]", "[tool:pytest]", "[remote \"origin\"]" (não "[1]", "[x for x in y]").
func cabecalhoINI(lin string) bool {
	if len(lin) < 3 || len(lin) > 80 || lin[len(lin)-1] != ']' || !letraD(lin[1]) {
		return false
	}
	aspas := false
	for i := 1; i < len(lin)-1; i++ {
		switch c := lin[i]; {
		case c == '"':
			aspas = !aspas
		case aspas:
		case c == ' ':
			if lin[i+1] != '"' {
				return false
			}
		case !(ehAlnum(c) || c == '_' || c == '.' || c == '-' || c == ':'):
			return false
		}
	}
	return !aspas
}

// linhaChaveIgual: "chave = valor" ou "chave: valor" (a linha de um bloco de INI).
func linhaChaveIgual(lin string) bool {
	if !letraD(lin[0]) {
		return false
	}
	i := 0
	for i < len(lin) && (ehAlnum(lin[i]) || lin[i] == '_' || lin[i] == '.' || lin[i] == '-') {
		i++
	}
	for i < len(lin) && (lin[i] == ' ' || lin[i] == '\t') {
		i++
	}
	return i < len(lin) && (lin[i] == '=' || lin[i] == ':') && (i+1 == len(lin) || lin[i+1] != '=')
}

// ---------------------------------------------------------------------------------------
// Âncora

func init() {
	registrarDecisor(Decisor{Nome: "léxica-âncora", Decidir: decidirAncora})
}

func decidirAncora(c *TextoCtx) {
	s := c.S
	// a âncora é um nome que o proxy já aprendeu (neste texto, pelos leitores, ou antes): o
	// aprendizado já passou pelos freios de evidência forte. Um achado fraco do texto não
	// ancora (a lista multiplicaria o engano).
	conhecido := c.Conhecido
	lista := func(els [][2]int) {
		ent := ""
		ancoras := 0
		for _, e := range els {
			v := s[e[0]:e[1]]
			if !candidataLexica(v) {
				return // lista que não é só de nomes: frase, número, opção, caminho...
			}
			if t, ok := conhecido(v); ok {
				if ent != "" && t != ent {
					return // âncoras de tipos diferentes: não é uma lista de uma coisa só
				}
				ent = t
				ancoras++
			}
		}
		if ancoras == 0 || ancoras == len(els) {
			return
		}
		for _, e := range els {
			v := s[e[0]:e[1]]
			if _, ok := conhecido(v); ok || !valorRecurso(v, ent) {
				continue
			}
			c.Decidir(e[0], e[1], ent, "léxica-âncora")
			if ganchoAchado != nil {
				ganchoAchado("léxica", ObjAchado{e[0], e[1], ent, "âncora", false}, v)
			}
		}
	}
	listasLiterais(s, lista)
	if (strings.Contains(s, ":\n") || strings.Contains(s, ":\r\n")) && strings.Contains(s, "- ") {
		listasYAML(s, lista)
	}
}

// candidataLexica: o valor inteiro pode ser nome: sem espaço, com forma de nome de recurso, que
// não é palavra de tipo nem pseudônimo.
func candidataLexica(v string) bool {
	return valorRecurso(v, "") && !palavrasTipo[strings.ToLower(v)] && !ehPseudoObj(v)
}

// listasLiterais: cada lista ("[...]" ou "{...}") de 2 ou mais literais entre aspas, só
// separados por vírgula (pode ocupar várias linhas). Uma passada: o que não é lista desiste no
// primeiro caractere que não é aspas.
func listasLiterais(s string, fn func(els [][2]int)) {
	var buf [16][2]int
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '[' && c != '{' {
			continue
		}
		fecha := byte(']')
		if c == '{' {
			fecha = '}'
		}
		els := buf[:0]
		j, ok := i+1, false
		for j < len(s) && j-i < 8000 && len(els) < 500 {
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j >= len(s) {
				break
			}
			if s[j] == fecha && len(els) > 0 { // vírgula no fim
				ok = true
				break
			}
			if s[j] != '"' && s[j] != '\'' {
				break
			}
			e := literal(s, j)
			if e < 0 {
				break
			}
			els = append(els, [2]int{j + 1, e})
			j = e + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && s[j] == ',' {
				j++
				continue
			}
			ok = j < len(s) && s[j] == fecha
			break
		}
		if ok && len(els) >= 2 {
			if !listaDunder(s, i) {
				fn(els)
			}
			i = j
		}
	}
}

// listaDunder: a lista em s[i] é atribuída a um nome "__x__" (__all__ = [...], __slots__ += (...)
// do Python): são identificadores de código, nunca nomes de recurso.
func listaDunder(s string, i int) bool {
	k := antesBranco(s, i)
	if k < 0 || s[k] != '=' {
		return false
	}
	if k > 0 && s[k-1] == '+' {
		k--
	}
	k = antesBranco(s, k)
	if k < 3 || s[k] != '_' || s[k-1] != '_' {
		return false
	}
	a, _ := identAntes(s, k)
	return k-a >= 4 && s[a] == '_' && s[a+1] == '_'
}

// listasYAML: os itens "- x" consecutivos, com o mesmo recuo, logo abaixo de uma linha "chave:"
// (a chave sem tipo; com tipo, o leitor de chave-valor já fez). Lista de marcadores em prosa
// (markdown) não tem a linha "chave:" em cima.
func listasYAML(s string, fn func(els [][2]int)) {
	var buf [16][2]int
	ls := quebraLinhas(s)
	for k := 0; k+2 < len(ls); k++ {
		lin := strings.TrimSpace(s[ls[k][0]:ls[k][1]])
		if len(lin) < 2 || lin[len(lin)-1] != ':' || !linhaChaveIgual(lin) || strings.HasPrefix(lin, "- ") {
			continue
		}
		els := buf[:0]
		recuo := -1
		x := k + 1
		for ; x < len(ls); x++ {
			a, b := ls[x][0], ls[x][1]
			r := a
			for r < b && (s[r] == ' ' || s[r] == '\t') {
				r++
			}
			if r+1 >= b || s[r] != '-' || s[r+1] != ' ' || recuo >= 0 && r-a != recuo {
				break
			}
			recuo = r - a
			va, vb := r+2, b
			for vb > va && (s[vb-1] == ' ' || s[vb-1] == '\t' || s[vb-1] == '\r') {
				vb--
			}
			if vb-va >= 2 && (s[va] == '"' || s[va] == '\'') && s[vb-1] == s[va] {
				va, vb = va+1, vb-1
			}
			if vb <= va || strings.ContainsAny(s[va:vb], ":{}[]# ") {
				els = els[:0]
				break // item que é mapa ou frase
			}
			els = append(els, [2]int{va, vb})
		}
		if len(els) >= 2 {
			fn(els)
		}
		k = x - 1
	}
}
