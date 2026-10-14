package mask

import (
	"regexp"
	"strings"
)

// Homogeneous lists (see docs/structures.md, section Loose script and shell outputs): in a list of
// names, if at least half of the items are already names of the same type (learned before or found
// by the readers in this text), the other identifier-shaped items are of the same type. It is not a
// reader: it depends on what is already known, and so it runs after the readers, like propagation.
// The list forms are format only:
//   - loose column: one item per line (with a "- ", "* " marker and quotes/comma at the end);
//   - count + value ("   12 t_x" from sort | uniq -c, "t_x    12" from value_counts);
//   - decorated loop line ("== t_x ==", "--- t_x", "## t_x"), grouped by the decoration;
//   - comma-separated list on a line, with or without quotes and brackets (json.dumps, print).
// And the qualified name in which one part is already known: "fin.<x>" with fin learned as a schema
// makes <x> a table.

const minList = 3

var (
	// list item: an identifier, maybe qualified (without spaces)
	reListItem      = `["'\x60]?[A-Za-z_][\w$#\-]*(?:\.[A-Za-z_][\w$#\-]*){0,2}["'\x60]?`
	reItemLine      = regexp.MustCompile(`^[ \t]*(?:[-*•][ \t]+)?(` + reListItem + `)[ \t]*[,;]?[ \t]*$`)
	reLeftContLine  = regexp.MustCompile(`^[ \t]*\d+[ \t]+(` + reListItem + `)[ \t]*$`)
	reRightContLine = regexp.MustCompile(`^[ \t]*(` + reListItem + `)[ \t]{2,}\d+[ \t]*$`)
	reDecorLine     = regexp.MustCompile(`^[ \t]*(={2,}|-{2,}|#{1,6}|\*{2,}|>{2,}|\[)[ \t]*(` + reListItem + `)[ \t]*(?:={2,}|-{2,}|\*{2,}|\])?[ \t]*:?[ \t]*$`)
	reWholeItem     = regexp.MustCompile(`^` + reListItem + `$`)
)

// listItem: the name of an item (without quotes), at positions of s.
func listItem(s string, a, b int) cell {
	for a < b && (s[a] == '"' || s[a] == '\'' || s[a] == '`') {
		a++
	}
	for b > a && (s[b-1] == '"' || s[b-1] == '\'' || s[b-1] == '`') {
		b--
	}
	return cell{a, b}
}

// listsIn: the groups of items of s in the forms above.
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
		if len(line) > 260 || words(line) > 3 { // none of the forms has more than 3 words
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

// commaLists: the comma-separated lists of line [a, b): whole items between the commas; the first
// may come after a label ("carregadas: a, b, c") and the last before a closing.
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
	edge := func(c cell, tail bool) (cell, bool) { // the word at the end (or start) of the piece
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

// words: how many space-separated words there are in l (stops counting at 4).
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

// callBefore: the list that starts at i is between the parentheses of a call ("f(a, b").
func callBefore(s string, i int) bool {
	k := i - 1
	for k >= 0 && (s[k] == ' ' || s[k] == '\t') {
		k--
	}
	return k >= 1 && s[k] == '(' && (isAlnum(s[k-1]) || s[k-1] == '_')
}

// knownType: the type of v if it is a learned name (in RAM or, hash only, in vistos.json).
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

// publicList: the vocabulary that never becomes a list item.
func publicList(v string) bool { return publicSQL(v) || publicDev(v) || publicDevops(v) }

// findLists: the homogeneous lists and the qualified names with a known (learned) part.
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
	// only what was already LEARNED (with the brakes: strong evidence or two rules), and not any
	// finding of this text: a wrong weak finding would pull the whole list along
	knownOne := m.knownType
	m.runReader(Reader{Name: "list", Public: publicList, Find: func(s string, add func(ObjFinding)) {
		homogeneousLists(s, knownOne, add)
		knownQualified(s, knownOne, add)
	}}, s, learn, add)
}

// homogeneousLists: in each list, the type of the known majority applies to the missing items.
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
					add(ObjFinding{a, a + len(p), ents[k], "list", ents[k] != "column" && identifierShape(p)})
				}
				a += len(p) + 1
			}
		}
	}
}

// assignsAttribute: the name s[a:b] opens the line and receives an assignment
// ("mock.scalars.return_value = x", "self.cfg.schema = y"): it is an object attribute in code, not
// a table name.
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

// knownQualified: "a.b" or "a.b.c" in which one part is a known schema or database at the position
// it has in a table name: the other parts get the position's type.
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
		ents := entQual(len(ps), "table")
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
