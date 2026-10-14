package mask

import (
	"regexp"
	"strings"
)

// Listas homogêneas (ver docs/pt-BR/structures.md, seção Saídas soltas de script e de shell): numa
// lista de nomes, se pelo menos metade dos itens já são nomes de um mesmo tipo (aprendidos
// antes ou achados pelos leitores neste texto), os outros itens com cara de identificador são
// do mesmo tipo. Não é um leitor: depende do que já se sabe, e por isso roda depois dos
// leitores, como a propagação. As formas de lista são só de formato:
//   - coluna solta: um item por linha (com marcador "- ", "* " e aspas/vírgula no fim);
//   - contagem + valor ("   12 t_x" de sort | uniq -c, "t_x    12" de value_counts);
//   - linha decorada de laço ("== t_x ==", "--- t_x", "## t_x"), agrupada pela decoração;
//   - lista entre vírgulas numa linha, com ou sem aspas e colchetes (json.dumps, print).
// E o nome qualificado em que uma parte já é conhecida: "fin.<x>" com fin aprendido como
// schema faz de <x> uma tabela.

const minList = 3

var (
	// item de lista: identificador, talvez qualificado (sem espaço)
	reListItem      = `["'\x60]?[A-Za-z_][\w$#\-]*(?:\.[A-Za-z_][\w$#\-]*){0,2}["'\x60]?`
	reItemLine      = regexp.MustCompile(`^[ \t]*(?:[-*•][ \t]+)?(` + reListItem + `)[ \t]*[,;]?[ \t]*$`)
	reLeftContLine  = regexp.MustCompile(`^[ \t]*\d+[ \t]+(` + reListItem + `)[ \t]*$`)
	reRightContLine = regexp.MustCompile(`^[ \t]*(` + reListItem + `)[ \t]{2,}\d+[ \t]*$`)
	reDecorLine     = regexp.MustCompile(`^[ \t]*(={2,}|-{2,}|#{1,6}|\*{2,}|>{2,}|\[)[ \t]*(` + reListItem + `)[ \t]*(?:={2,}|-{2,}|\*{2,}|\])?[ \t]*:?[ \t]*$`)
	reWholeItem     = regexp.MustCompile(`^` + reListItem + `$`)
)

// listItem: o nome de um item (sem aspas), nas posições de s.
func listItem(s string, a, b int) cell {
	for a < b && (s[a] == '"' || s[a] == '\'' || s[a] == '`') {
		a++
	}
	for b > a && (s[b-1] == '"' || s[b-1] == '\'' || s[b-1] == '`') {
		b--
	}
	return cell{a, b}
}

// listsIn: os grupos de itens de s nas formas acima.
func listsIn(s string) [][]cell {
	var groups [][]cell
	var race []cell
	shape := 0
	closeIt := func() {
		if len(race) >= minList {
			groups = append(groups, race)
		}
		race, shape = nil, 0
	}
	decor := map[string][]cell{}
	var decorOrder []string
	for _, l := range wrapLines(s) {
		line := s[l[0]:l[1]]
		if len(line) > 260 || words(line) > 3 { // nenhuma das formas tem mais de 3 palavras
			closeIt()
			continue
		}
		if m := reDecorLine.FindStringSubmatchIndex(line); m != nil {
			k := line[m[2]:m[3]]
			if _, ok := decor[k]; !ok {
				decorOrder = append(decorOrder, k)
			}
			decor[k] = append(decor[k], listItem(s, l[0]+m[4], l[0]+m[5]))
			closeIt()
			continue
		}
		f, a, b := 0, -1, -1
		if m := reItemLine.FindStringSubmatchIndex(line); m != nil {
			f, a, b = 1, m[2], m[3]
		} else if m := reLeftContLine.FindStringSubmatchIndex(line); m != nil {
			f, a, b = 2, m[2], m[3]
		} else if m := reRightContLine.FindStringSubmatchIndex(line); m != nil {
			f, a, b = 3, m[2], m[3]
		}
		if f == 0 || shape != 0 && f != shape {
			closeIt()
		}
		if f == 0 {
			continue
		}
		shape = f
		race = append(race, listItem(s, l[0]+a, l[0]+b))
	}
	closeIt()
	for _, k := range decorOrder {
		if len(decor[k]) >= minList {
			groups = append(groups, decor[k])
		}
	}
	if strings.IndexByte(s, ',') >= 0 {
		for _, l := range wrapLines(s) {
			if strings.Count(s[l[0]:l[1]], ",") >= minList-1 {
				groups = append(groups, commaLists(s, l[0], l[1])...)
			}
		}
	}
	return groups
}

// commaLists: as listas entre vírgulas da linha [a, b): itens inteiros entre as vírgulas; o
// primeiro pode vir depois de um rótulo ("carregadas: a, b, c") e o último antes de um fecho.
func commaLists(s string, a, b int) [][]cell {
	var sgms []cell
	for i := a; i <= b; i++ {
		if i == b || s[i] == ',' {
			c := cell{a, i}
			for c.a < c.b && (s[c.a] == ' ' || s[c.a] == '\t') {
				c.a++
			}
			for c.b > c.a && (s[c.b-1] == ' ' || s[c.b-1] == '\t') {
				c.b--
			}
			sgms = append(sgms, c)
			a = i + 1
		}
	}
	whole := func(c cell) bool { return c.b > c.a && c.b-c.a <= 200 && reWholeItem.MatchString(s[c.a:c.b]) }
	edge := func(c cell, tail bool) (cell, bool) { // a palavra do fim (ou do começo) do pedaço
		if tail {
			k := c.b
			for k > c.a && !strings.ContainsRune(" \t[(:={", rune(s[k-1])) {
				k--
			}
			c.a = k
		} else {
			k := c.a
			for k < c.b && !strings.ContainsRune(" \t])};", rune(s[k])) {
				k++
			}
			c.b = k
		}
		return c, whole(c)
	}
	var out [][]cell
	for i := 0; i+1 < len(sgms); {
		ini, ok := edge(sgms[i], true)
		if !ok || callBefore(s, ini.a) {
			i++
			continue
		}
		g := []cell{listItem(s, ini.a, ini.b)}
		j := i + 1
		for j < len(sgms) && whole(sgms[j]) {
			g = append(g, listItem(s, sgms[j].a, sgms[j].b))
			j++
		}
		if j < len(sgms) {
			if end, ok := edge(sgms[j], false); ok {
				g = append(g, listItem(s, end.a, end.b))
			}
		}
		if len(g) >= minList {
			out = append(out, g)
		}
		i = j
	}
	return out
}

// palavras: quantas palavras separadas por espaço há em l (para de contar em 4).
func words(l string) int {
	n, inside := 0, false
	for i := 0; i < len(l) && n < 4; i++ {
		b := l[i] == ' ' || l[i] == '\t'
		if !b && !inside {
			n++
		}
		inside = !b
	}
	return n
}

// callBefore: a lista que começa em i está entre os parênteses de uma chamada ("f(a, b").
func callBefore(s string, i int) bool {
	k := i - 1
	for k >= 0 && (s[k] == ' ' || s[k] == '\t') {
		k--
	}
	return k >= 1 && s[k] == '(' && (isAlnum(s[k-1]) || s[k-1] == '_')
}

// knownType: o tipo de v se é um nome aprendido (em RAM ou, só o hash, no vistos.json).
func (m *Masker) knownType(v string) (string, bool) {
	c := m.known
	c.mu.RLock()
	tp, ok := c.canon["O:"+v]
	if !ok {
		tp, ok = c.canon["o:"+strings.ToLower(v)]
		ok = ok && entSQL[strings.TrimPrefix(tp, objTypePrefix)]
	}
	c.mu.RUnlock()
	if ok {
		ent := strings.TrimPrefix(tp, objTypePrefix)
		return ent, isObject(tp) && m.objPropagates(ent)
	}
	if m.seenSet == nil || !m.seenSet.HasObj() || !identifierShape(v) {
		return "", false
	}
	ent, seen, ok := m.seenSet.Obj(m.objID("O:" + v))
	if !ok {
		ent, seen, ok = m.seenSet.Obj(m.objID("o:" + strings.ToLower(v)))
		ok = ok && entSQL[ent]
	}
	if !ok || today()-seen > objValidity || !m.objPropagates(ent) {
		return "", false
	}
	return ent, true
}

// publicList: o vocabulário que nunca vira item de lista.
func publicList(v string) bool { return publicSQL(v) || publicDev(v) || publicDevops(v) }

// findLists: as listas homogêneas e os nomes qualificados com uma parte conhecida (aprendida).
func (m *Masker) findLists(s string, learn bool, add func(ini, end int, typ string)) {
	if !m.cfg.Objects.Enabled {
		return
	}
	c := m.known
	c.mu.RLock()
	nRAM := c.nObj
	c.mu.RUnlock()
	if nRAM == 0 && (m.seenSet == nil || !m.seenSet.HasObj()) {
		return
	}
	// só o que já foi APRENDIDO (com os freios: evidência forte ou duas regras), e não qualquer
	// achado deste texto: um achado fraco errado puxaria a lista inteira junto
	knownOne := m.knownType
	m.runReader(Reader{Name: "lista", Public: publicList, Find: func(s string, add func(ObjFinding)) {
		homogeneousLists(s, knownOne, add)
		knownQualified(s, knownOne, add)
	}}, s, learn, add)
}

// homogeneousLists: em cada lista, o tipo da maioria conhecida vale para os itens que faltam.
func homogeneousLists(s string, knownOne func(string) (string, bool), add func(ObjFinding)) {
	for _, g := range listsIn(s) {
		n := map[string]int{}
		knows := make([]bool, len(g))
		for i, c := range g {
			v := s[c.a:c.b]
			if p := strings.LastIndexByte(v, '.'); p >= 0 {
				v = v[p+1:]
			}
			if e, ok := knownOne(v); ok {
				n[e]++
				knows[i] = true
			}
		}
		typ, max := "", 0
		for e, k := range n {
			if k > max || k == max && e < typ {
				typ, max = e, k
			}
		}
		if max*2 < len(g) {
			continue
		}
		for i, c := range g {
			if knows[i] {
				continue
			}
			v := s[c.a:c.b]
			if !identifierShape(v) || isDataType(v) || isPseudoObj(v) {
				continue
			}
			ps := strings.Split(v, ".")
			if len(ps) > 1 && fileExtensions[strings.ToLower(ps[len(ps)-1])] {
				continue // arquivo.csv
			}
			ents := entQual(len(ps), typ)
			a := c.a
			for k, p := range ps {
				if _, ok := knownOne(p); !ok && reSimpleIdent.MatchString(p) {
					add(ObjFinding{a, a + len(p), ents[k], "lista", ents[k] != "coluna" && identifierShape(p)})
				}
				a += len(p) + 1
			}
		}
	}
}

// assignsAttribute: o nome s[a:b] abre a linha e recebe uma atribuição ("mock.scalars.return_value
// = x", "self.cfg.schema = y"): é atributo de objeto no código, não nome de tabela.
func assignsAttribute(s string, a, b int) bool {
	for k := a - 1; k >= 0 && s[k] != '\n'; k-- {
		if s[k] != ' ' && s[k] != '\t' {
			return false
		}
	}
	for b < len(s) && (s[b] == ' ' || s[b] == '\t') {
		b++
	}
	return b+1 < len(s) && s[b] == '=' && s[b+1] != '='
}

// knownQualified: "a.b" ou "a.b.c" em que uma parte é um schema ou banco conhecido na
// posição em que fica num nome de tabela: as outras partes ganham o tipo da posição.
func knownQualified(s string, knownOne func(string) (string, bool), add func(ObjFinding)) {
	if strings.IndexByte(s, '.') < 0 {
		return
	}
	ident := func(c byte) bool { return isAlnum(c) || c == '_' || c == '$' }
	for i := 0; i < len(s); {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_') || i > 0 && (ident(s[i-1]) || s[i-1] == '.' || s[i-1] == '/' || s[i-1] == '-') {
			i++
			continue
		}
		var ps [][2]int
		j := i
		for {
			k := j
			for k < len(s) && ident(s[k]) {
				k++
			}
			if k == j {
				break
			}
			ps = append(ps, [2]int{j, k})
			j = k
			if j+1 < len(s) && s[j] == '.' && (letterD(s[j+1]) || s[j+1] == '_') && len(ps) < 4 {
				j++
				continue
			}
			break
		}
		i = max(j, i+1)
		if len(ps) < 2 || len(ps) > 3 || j < len(s) && (s[j] == '(' || s[j] == '-' || s[j] == '/' || s[j] == '.' && j+1 < len(s) && isAlnum(s[j+1])) {
			continue
		}
		if lastPos := strings.ToLower(s[ps[len(ps)-1][0]:ps[len(ps)-1][1]]); fileExtensions[lastPos] || publicTLDs[lastPos] {
			continue
		}
		ents := entQual(len(ps), "tabela")
		matched := false
		var knows [3]bool
		for k, p := range ps {
			if e, ok := knownOne(s[p[0]:p[1]]); ok {
				knows[k] = true
				if (e == "schema" || e == "database") && e == ents[k] {
					matched = true
				}
			}
		}
		if !matched || assignsAttribute(s, ps[0][0], j) {
			continue
		}
		for k, p := range ps {
			if v := s[p[0]:p[1]]; !knows[k] && !isPseudoObj(v) && len(v) >= 2 {
				add(ObjFinding{p[0], p[1], ents[k], "qualificado", identifierShape(v)})
			}
		}
	}
}
