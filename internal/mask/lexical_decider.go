package mask

import "strings"

// Lexical rule for code and configuration (see docs/structures.md). Most of it is already done by
// the code readers (reader_code.go) and the key-value reader (reader_key_value.go), which stay as
// they are:
//
//   - candidate: only the WHOLE literal or value, without spaces (resourceValue refuses spaces,
//     so "payments failed" is a sentence and stays); a code identifier never (the unquoted value
//     of "x = y" only counts in a configuration line); a comment never (there is no key in it).
//   - key clue: the key name contains an ObjectPrefixes type in any spelling (entKey):
//     namespace = "payments", bucket: billing, TABLE_NAME=pedidos.
//
// This file adds:
//
//   - INI section: "key = value" with spaces around the "=" is a code assignment in Python,
//     Ruby, shell..., except when the line is in a block that starts with a section header
//     "[name]" (iniSection, used by the key-value reader).
//   - anchor (decider "lexical-anchor"): in a list of literals ([..], {..} of Go/C#, YAML list
//     "- x" under "key:"), all of them candidates, in which one item is already a learned name
//     (in this text, by the readers, or before), the other items are names of the same type.
//     The list is homogeneous; the tuple and the arguments of a call ("(...)") do not count,
//     because they mix types; __all__ = [...] (Python) is a list of code identifiers.
//
// The echo and the word that appears as data in a command output come from the conversation memory
// and from provenance (sections A and B), not from here.

// iniSection: the line of s[i] is in an INI block: going up, only "key = value" lines, comments
// (";", "#") and blank lines until a "[section]" header. Looks at most 64 lines.
func iniSection(s string, i int) bool {
	l := lineStartJ(s, i)
	for n := 0; n < 64 && l > 0; n++ {
		f := l - 1 // the '\n' of the line above
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

// iniHeader: "[section]", "[tool:pytest]", "[remote \"origin\"]" (not "[1]", "[x for x in y]").
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

// keyEqualsLine: "key = value" or "key: value" (the line of an INI block).
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

// --------------------------------------------------------------------------------------- Anchor

func init() {
	registerDecider(Decider{Name: "lexical-anchor", Decide: decideAnchor})
}

func decideAnchor(c *TextCtx) {
	s := c.S
	// the anchor is a name the proxy already learned (in this text, by the readers, or before):
	// learning already went through the strong-evidence brakes. A weak finding of the text does not
	// anchor (the list would multiply the mistake).
	knownOne := c.Known
	list := func(els [][2]int) {
		ent := ""
		anchors := 0
		for _, e := range els {
			v := s[e[0]:e[1]]
			if !lexicalCandidate(v) {
				return // a list that is not only names: sentence, number, option, path...
			}
			if t, ok := knownOne(v); ok {
				if ent != "" && t != ent {
					return // anchors of different types: not a list of one thing
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
			c.Decide(e[0], e[1], ent, "lexical-anchor")
			if hookFound != nil {
				hookFound("lexical", ObjFinding{e[0], e[1], ent, "anchor", false}, v)
			}
		}
	}
	literalLists(s, list)
	if (strings.Contains(s, ":\n") || strings.Contains(s, ":\r\n")) && strings.Contains(s, "- ") {
		yamlLists(s, list)
	}
}

// lexicalCandidate: the whole value can be a name: no spaces, with the shape of a resource name,
// and it is neither a type word nor a pseudonym.
func lexicalCandidate(v string) bool {
	return resourceValue(v, "") && !typeWords[strings.ToLower(v)] && !isPseudoObj(v)
}

// literalLists: each list ("[...]" or "{...}") of 2 or more quoted literals, separated only by
// commas (it may span several lines). One pass: what is not a list gives up at the first character
// that is not a quote.
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
			if s[j] == closes && len(els) > 0 { // comma at the end
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

// dunderList: the list at s[i] is assigned to a "__x__" name (Python's __all__ = [...], __slots__
// += (...)): they are code identifiers, never resource names.
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

// yamlLists: the consecutive "- x" items, with the same indentation, right below a "key:" line (the
// key without a type; with a type, the key-value reader already did it). A bullet list in prose
// (markdown) does not have the "key:" line above.
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
				break // an item that is a map or a sentence
			}
			els = append(els, [2]int{va, vb})
		}
		if len(els) >= 2 {
			fn(els)
		}
		k = x - 1
	}
}
