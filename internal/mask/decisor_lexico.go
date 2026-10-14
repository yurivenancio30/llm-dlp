package mask

import "strings"

// Rule léxica para código e configuração (ver
// docs/pt-BR/estruturas.md). A maior parte já é feita pelos leitores de código (leitor_codigo.go) e
// de chave-valor (leitor_chave_valor.go), que ficam como estão:
//
//   - candidata: só o literal ou o valor INTEIRO, sem espaço (resourceValue recusa espaço, então
//     "payments failed" é frase e fica); identificador de código nunca (o valor sem aspas de
//     "x = y" só vale em linha de configuração); comentário nunca (não há chave nele).
//   - pista da chave: o nome da chave contém um tipo de ObjectPrefixes em qualquer grafia (entKey):
//     namespace = "payments", bucket: billing, TABLE_NAME=pedidos.
//
// Este arquivo acrescenta:
//
//   - seção de INI: "chave = valor" com espaços em volta do "=" é atribuição de código em
//     Python, Ruby, shell..., salvo quando a linha está num bloco que começa num cabeçalho de
//     seção "[nome]" (iniSection, usado pelo leitor de chave-valor).
//   - âncora (decisor "léxica-âncora"): numa lista de literais ([..], {..} de Go/C#, lista YAML
//     "- x" debaixo de "chave:"), todos candidatos, em que um item já é nome aprendido (neste
//     texto, pelos leitores, ou antes), os outros itens são nomes do mesmo tipo. A lista é
//     homogênea; a tupla e os argumentos de uma chamada ("(...)") não contam, porque misturam
//     tipos; __all__ = [...] (Python) é lista de identificadores de código.
//
// O eco e a palavra que aparece como dado numa saída de comando vêm da memória da conversa e da
// proveniência (seções A e B), não daqui.

// iniSection: a linha de s[i] está num bloco de INI: subindo, só linhas "chave = valor",
// comentários (";", "#") e linhas em branco até um cabeçalho "[seção]". Olha no máximo 64 linhas.
func iniSection(s string, i int) bool {
	l := lineStartJ(s, i)
	for n := 0; n < 64 && l > 0; n++ {
		f := l - 1 // o '\n' da linha de cima
		a := lineStartJ(s, f)
		lineV := strings.TrimSpace(s[a:f])
		switch {
		case lineV == "" || lineV[0] == ';' || lineV[0] == '#':
		case lineV[0] == '[':
			return iniHeader(lineV)
		case keyEqualsLine(lineV):
		default:
			return false
		}
		l = a
	}
	return false
}

// iniHeader: "[seção]", "[tool:pytest]", "[remote \"origin\"]" (não "[1]", "[x for x in y]").
func iniHeader(lineV string) bool {
	if len(lineV) < 3 || len(lineV) > 80 || lineV[len(lineV)-1] != ']' || !letterD(lineV[1]) {
		return false
	}
	quotes := false
	for i := 1; i < len(lineV)-1; i++ {
		switch c := lineV[i]; {
		case c == '"':
			quotes = !quotes
		case quotes:
		case c == ' ':
			if lineV[i+1] != '"' {
				return false
			}
		case !(isAlnum(c) || c == '_' || c == '.' || c == '-' || c == ':'):
			return false
		}
	}
	return !quotes
}

// keyEqualsLine: "chave = valor" ou "chave: valor" (a linha de um bloco de INI).
func keyEqualsLine(lineV string) bool {
	if !letterD(lineV[0]) {
		return false
	}
	i := 0
	for i < len(lineV) && (isAlnum(lineV[i]) || lineV[i] == '_' || lineV[i] == '.' || lineV[i] == '-') {
		i++
	}
	for i < len(lineV) && (lineV[i] == ' ' || lineV[i] == '\t') {
		i++
	}
	return i < len(lineV) && (lineV[i] == '=' || lineV[i] == ':') && (i+1 == len(lineV) || lineV[i+1] != '=')
}

// ---------------------------------------------------------------------------------------
// Âncora

func init() {
	registerDecider(Decider{Name: "léxica-âncora", Decide: decideAnchor})
}

func decideAnchor(c *TextCtx) {
	s := c.S
	// a âncora é um nome que o proxy já aprendeu (neste texto, pelos leitores, ou antes): o
	// aprendizado já passou pelos freios de evidência forte. Um achado fraco do texto não
	// ancora (a lista multiplicaria o engano).
	knownOne := c.Known
	list := func(els [][2]int) {
		ent := ""
		anchors := 0
		for _, e := range els {
			v := s[e[0]:e[1]]
			if !lexicalCandidate(v) {
				return // lista que não é só de nomes: frase, número, opção, caminho...
			}
			if t, ok := knownOne(v); ok {
				if ent != "" && t != ent {
					return // âncoras de tipos diferentes: não é uma lista de uma coisa só
				}
				ent = t
				anchors++
			}
		}
		if anchors == 0 || anchors == len(els) {
			return
		}
		for _, e := range els {
			v := s[e[0]:e[1]]
			if _, ok := knownOne(v); ok || !resourceValue(v, ent) {
				continue
			}
			c.Decide(e[0], e[1], ent, "léxica-âncora")
			if hookFound != nil {
				hookFound("léxica", ObjFinding{e[0], e[1], ent, "âncora", false}, v)
			}
		}
	}
	literalLists(s, list)
	if (strings.Contains(s, ":\n") || strings.Contains(s, ":\r\n")) && strings.Contains(s, "- ") {
		yamlLists(s, list)
	}
}

// lexicalCandidate: o valor inteiro pode ser nome: sem espaço, com forma de nome de recurso, que
// não é palavra de tipo nem pseudônimo.
func lexicalCandidate(v string) bool {
	return resourceValue(v, "") && !typeWords[strings.ToLower(v)] && !isPseudoObj(v)
}

// literalLists: cada lista ("[...]" ou "{...}") de 2 ou mais literais entre aspas, só
// separados por vírgula (pode ocupar várias linhas). Uma passada: o que não é lista desiste no
// primeiro caractere que não é aspas.
func literalLists(s string, fn func(els [][2]int)) {
	var buf [16][2]int
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '[' && c != '{' {
			continue
		}
		closes := byte(']')
		if c == '{' {
			closes = '}'
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
			if s[j] == closes && len(els) > 0 { // vírgula no fim
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
			ok = j < len(s) && s[j] == closes
			break
		}
		if ok && len(els) >= 2 {
			if !dunderList(s, i) {
				fn(els)
			}
			i = j
		}
	}
}

// dunderList: a lista em s[i] é atribuída a um nome "__x__" (__all__ = [...], __slots__ += (...)
// do Python): são identificadores de código, nunca nomes de recurso.
func dunderList(s string, i int) bool {
	k := beforeBlank(s, i)
	if k < 0 || s[k] != '=' {
		return false
	}
	if k > 0 && s[k-1] == '+' {
		k--
	}
	k = beforeBlank(s, k)
	if k < 3 || s[k] != '_' || s[k-1] != '_' {
		return false
	}
	a, _ := identBefore(s, k)
	return k-a >= 4 && s[a] == '_' && s[a+1] == '_'
}

// yamlLists: os itens "- x" consecutivos, com o mesmo recuo, logo abaixo de uma linha "chave:"
// (a chave sem tipo; com tipo, o leitor de chave-valor já fez). Lista de marcadores em prosa
// (markdown) não tem a linha "chave:" em cima.
func yamlLists(s string, fn func(els [][2]int)) {
	var buf [16][2]int
	ls := wrapLines(s)
	for k := 0; k+2 < len(ls); k++ {
		lineV := strings.TrimSpace(s[ls[k][0]:ls[k][1]])
		if len(lineV) < 2 || lineV[len(lineV)-1] != ':' || !keyEqualsLine(lineV) || strings.HasPrefix(lineV, "- ") {
			continue
		}
		els := buf[:0]
		indent := -1
		x := k + 1
		for ; x < len(ls); x++ {
			a, b := ls[x][0], ls[x][1]
			r := a
			for r < b && (s[r] == ' ' || s[r] == '\t') {
				r++
			}
			if r+1 >= b || s[r] != '-' || s[r+1] != ' ' || indent >= 0 && r-a != indent {
				break
			}
			indent = r - a
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
