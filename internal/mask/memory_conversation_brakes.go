package mask

import (
	"sort"
	"strings"
)

// Brakes and anchor of the conversation memory (see docs/structures.md, section Knowing the name).
//
//   - Program: in the output of a command, the word that is in the program text (the command
//     or the script it runs) is not swapped by the memory; the exception is the word the proxy
//     translated from a pseudonym in that command (that one is a name, not program). A regex
//     '.*cpf.*' the script wrote goes out as the script wrote it.
//   - Comment and running text: the memory does not swap a common word (without identifier
//     shape) inside a comment (--, #, //, /* */ on the line, outside quotes) in the user's or
//     the model's sentence; in data (tool output, file read), only column and index stay out:
//     the resource name the request proved stays a name (provenance). In running text (three
//     or more prose words in a row), it does not swap the common word that is a column or
//     index name: it is the data model vocabulary (the "nome" column of a catalog does not
//     swap the "nome" of a sentence), the same reason why vistos does not propagate columns.
//     A resource name (namespace, table, bucket...) decided in an inventory still applies in
//     prose.
//   - Anchor by position: in a command output with parallel records (3 or more lines with the
//     same number of words, ±1), a position in which two or more distinct values are already
//     names of the same type is a column of that type: its other values that can be names
//     (decidableWord, outside the program) get the type, and those with the generic type are
//     corrected. It is what catches the catalog columns head did not show.

// memFilter: what the memory cannot swap in a text.
type memFilter struct {
	prog map[uint32]bool // program words (the output hint)
	// output: the text is data (tool output, file read), not a sentence of the user or the model.
	// There provenance applies (see skips).
	output bool
}

// columnOrIndex: the data model vocabulary, which the memory does not carry into prose nor into
// another file (the same reason why vistos does not propagate columns).
func columnOrIndex(ent string) bool { return ent == "column" || ent == "index" }

func filterOf(hint string) memFilter {
	_, ext := hintParts(hint)
	e, ok := readCallExt(ext)
	if !ok || !e.output {
		return memFilter{}
	}
	return memFilter{prog: e.prog}
}

// skips: the memory does not swap s[a:b] (the name nm) here.
func (f memFilter) skips(s string, a, b int, nm *memName) bool {
	v := s[a:b]
	if identifierShape(v) {
		// a client name the program cites (the model copied it from data) also goes out masked in
		// its output; what the model wrote on its own is already public (priority)
		return false
	}
	if f.prog != nil && f.prog[hpal(v)] {
		return true
	}
	// provenance: in data (tool output, file), the resource name this request already proved (it is
	// what is in the memory) stays a name inside a comment; the common word only counts as a word
	// in the user's or the model's sentence
	if inComment(s, a) && !(f.output && !columnOrIndex(nm.ent)) {
		return true
	}
	return columnOrIndex(nm.ent) && inProse(s, a, b)
}

// inComment: s[a] is after a comment marker on the same line (--, #, //, /*), which starts the line
// or comes after a space, outside quotes. These are not comments: "--x" glued to a letter or digit
// (command option: --schema=x) and "#" followed by a digit (#3).
func inComment(s string, a int) bool {
	ini := lineStartJ(s, a)
	l := s[ini:a]
	var quote byte
	for i := 0; i < len(l); i++ {
		c := l[i]
		if quote != 0 { // inside a string: the marker is text
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
		after := byte(' ') // what comes after the marker (may be the name itself, at s[a])
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

// inProse: s[a:b] is in a sequence of 3 or more words of letters separated by one space (running
// text), counting itself.
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
	// backwards
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
	// forwards (sentence punctuation after the word still counts)
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

// anchorPositions: the anchor by position (see above), over the already decided pieces of s (in
// order). Returns the pieces with the new and the corrected ones (without pseudonyms in the new
// ones).
func (l *Batch) anchorPositions(s string, ts []snippet, f memFilter) []snippet {
	if f.prog == nil || len(ts) < 2 || strings.Count(s, "\n") < 2 || len(s) > maxCallText {
		return ts
	}
	// the piece that covers each position
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
	// by field: consecutive lines with the same separator the same number of times (the word count
	// may vary inside a field: "NUMBER(12,2)", a list of regexes at the end)
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
	used := map[int]bool{} // word (start) already decided in this block
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
			// the types of the values already decided in this position (per distinct value)
			byType := map[string]map[string]bool{}
			n := 0
			for _, r := range block {
				p, ok := at(r)
				if !ok || p[0] == p[1] {
					continue
				}
				n++
				// seed: the whole value of the field (in a qualified name, the last part), with
				// direct proof in the same source; a deduction is not a seed (see
				// memory_tracing.go)
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
				continue // fewer than two known values, or mixed types
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
						continue // covered whole (of a qualified name, only the missing parts)
					}
				}
				v := s[p[0]:p[1]]
				if f.prog[hpal(v)] {
					continue
				}
				// qualified name (a.b): each part, with the position's type in the last piece
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

// sortSnippets: in start order.
func sortSnippets(ts []snippet) {
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].Start < ts[j].Start })
}

// blocksByField: the blocks of 3 or more consecutive lines in which a separator (| TAB ; ,) appears
// the same number of times (1 or more); each record is the single-word fields (a field with a space
// inside, or empty, stays as a position without a value).
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
						i += f + 1 // a separator inside quotes does not count
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
				// the value without what wraps it: quotes, parentheses, brackets
				for x < y && strings.IndexByte("'\"`([{", s[x]) >= 0 {
					x++
				}
				for y > x && strings.IndexByte("'\"`)]};", s[y-1]) >= 0 {
					y--
				}
				if x == y || strings.ContainsAny(s[x:y], " \t") {
					r = append(r, [2]int{x, x}) // empty field or one with a sentence: a position without a value
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

// noOverlap: the new pieces in order, without repeating or overlapping (the first one stays).
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
