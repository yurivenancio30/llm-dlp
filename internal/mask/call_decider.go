package mask

import (
	"sort"
	"strconv"
	"strings"
)

// Decider of command outputs and tool_use inputs (see call.go and docs/structures.md):
//
//   - B1 provenance: a word of the output that is in the program (the command or the script) did
//     not come from the data and is never decided here;
//   - B2 identity: in blocks of 3 or more parallel records (lines with the same number of
//     words, ±1), the position where all values are distinct, next to positions with repeated
//     values, is the identity of the record; a list (one word per line, all distinct) also is;
//   - B3: the identity word that came from the data is a name when the output is an inventory
//     (the request has an enumeration verb and a noun, in the command or in the user message) or
//     when another position of the block already holds known names (anchor);
//   - B4 echo: a tool_use word that echoes a previous output in argument position is a name;
//     and so is a word that is a segment of a name already masked in the same text;
//   - B1 exception: a word the proxy translated from a pseudonym is a name (in the tool_use and
//     in its output).
//
// Brakes: numeric forms, dates, durations (and the unit after a number: "2 days"), versions,
// hashes, data types, the public vocabularies that already exist and common words with fewer than 4
// letters. Everything in one pass per line and lookups in a hash table.

func init() {
	registerDecider(Decider{Name: "chamada", Decide: decideCall})
}

// callExt: the extension of the hint read (call.go).
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

// maxCallText: above this, the rules of this section do not run (the whole output of a command
// rarely goes past it; the rest of the rules still apply).
const maxCallText = 8 << 20

func decideCall(c *TextCtx) {
	e, ok := readCallExt(c.Ext)
	if !ok || len(c.S) > maxCallText {
		return
	}
	s := c.S
	cov := coverage(c.Findings)
	// B1 exception and B4 echo: the name words of the call, where they appear whole
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

// knownEnt: the type with which v is already a name, if it is; otherwise ent.
func knownEnt(c *TextCtx, v, ent string) string {
	if k, ok := c.Known(v); ok {
		return k
	}
	return ent
}

// ---- coverage: the pieces already found -------------------------------------------------

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

// covers: does [a, b) cross some piece already found?
func (fs ranges) covers(a, b int) bool {
	i := sort.Search(len(fs), func(i int) bool { return fs[i][1] > a })
	return i < len(fs) && fs[i][0] < b
}

// ---- freios ------------------------------------------------------------------------------

// decidableWord: v can be a name (the brakes common to echo, identity and segment).
func decidableWord(v string) bool {
	if len(v) < 3 || len(v) > 128 || v[0] >= '0' && v[0] <= '9' {
		return false // number, date, time, duration (12d, 3h4m), size (10Mi)
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

// fieldLabel: the word that ends at s[b] is the label of a "field: value" or "field=value" (JSON,
// YAML, .env, log, Python dict): the name of the field, never the name of the record. Not labels:
// "x::y", "x://", "x==y" nor "host:5432" and "12:30" (port and time).
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

// ---- segments of an already masked name (B4) ---------------------------------------------

// segments: a word that is a segment of an object name already found in the text ("payments" in
// payments-api-7d9f, registry/acme/payments:1.2) and that also appears loose in the same text is a
// name, of the type of the name it is in. Outside comments and running text, and never from a
// column name.
func segments(c *TextCtx, cov ranges) {
	var sgms map[string]string
	for _, a := range c.Findings {
		if !isObject(a.Type) || !strings.ContainsAny(a.Real, "-_./:") {
			continue
		}
		ent := strings.TrimPrefix(a.Type, objTypePrefix)
		if ent == "coluna" || ent == "indice" {
			continue // the pieces of a column name (cd_cliente) are data model vocabulary, not a resource
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

// record: the words of a line.
type record struct {
	ps  []regWord
	hdr bool // comes before a dash line (header)
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
			continue // dash line, empty: does not interrupt
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

// dashLine: line k only has dashes, "=", "+", "|" and spaces (and at least 3 dashes).
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

// uppercase: all words uppercase only (NAME STATUS AGE).
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

// blockIdentity: the identity positions of a block and the decision.
func blockIdentity(c *TextCtx, e callExt, cov ranges, block []record) {
	s := c.S
	// the header (the first line, before dashes or uppercase only when the rest is not) is not a
	// record
	if b0 := block[0]; b0.hdr || uppercase(s, b0.ps) && !uppercase(s, block[1].ps) {
		block = block[1:]
	}
	if len(block) < minList {
		return
	}
	anc := e.typ == "" // the anchor only matters without a requested type
	// decidable[r][k]: word k of record r can be a name (brakes + provenance)
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
			// unit: the word right after a number, separated by a single space ("2 days")
			if k > 0 && numeric(s[r.ps[k-1].a:r.ps[k-1].b]) && p.a == r.ps[k-1].b+1 && s[p.a-1] == ' ' {
				continue
			}
			dec[i][k] = true
		}
	}
	// two readings of the positions: from the left (k) and from the right (n-1-k); with the same
	// number of words on all lines, they are the same
	ident := [2][]bool{make([]bool, maxN), make([]bool, maxN)}
	// anchorPos: a position with repeated values in which half or more are already names (a catalog
	// column: the same name repeats across tables)
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
	// identifier shape coming from the data, in an anchored position: two clues agreeing, without
	// requiring the values to be unique
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
		// real identity: few distinct positions (a table of records has one or two keys; a loose
		// sentence has all words distinct)
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
				dec[i][k] = false // only once
				c.Decide(p.a, p.b, knownEnt(c, s[p.a:p.b], typ), "identidade")
			}
		}
	}
}

// entOfPosition: the most common type among the already known values of a block position (generic
// if none).
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
