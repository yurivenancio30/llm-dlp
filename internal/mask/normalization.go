package mask

import (
	"regexp"
	"sort"
	"strings"
)

// Transport normalization: content almost always arrives wrapped by a command or a tool (line
// number of the read tool, cat -n, grep -n, diff, markdown quote, log stamp, code fence, ANSI
// colors, box borders, JSON-escaped text, CRLF). No reader handles this, and each one would have to
// handle all of it: so it is a layer before them. It returns the clean text and, for each position
// of it, the position in the original; the readers also run on the clean text and the findings go
// back to the original (only those that match byte for byte, so the swap and the return are exact).

// normText: the clean text and the map clean position -> original position (len(t)+1 positions).
type normText struct {
	t  string
	mp []int
	// the original also needs to be read: normalization removed something that carries a name (the
	// grep file, the diff header) or changed the structure (decoded JSON). Without that (line
	// number, log stamp, colors, borders, CRLF), the clean text is enough and the original is not
	// read.
	alsoRaw bool
}

// original: the piece [a, b) of the clean text in the original, if it matches byte for byte.
func (n *normText) original(s string, a, b int) (int, int, bool) {
	if a < 0 || b > len(n.t) || a >= b {
		return 0, 0, false
	}
	oa := n.mp[a]
	ob := oa + (b - a)
	if ob > len(s) || n.mp[b-1] != ob-1 || s[oa:ob] != n.t[a:b] {
		return 0, 0, false
	}
	return oa, ob, true
}

// cleaned: the piece [oa, ob) of the original in the clean text, if it matches byte for byte.
func (n *normText) cleaned(s string, oa, ob int) (int, int, bool) {
	a := sort.SearchInts(n.mp[:len(n.t)], oa)
	if a >= len(n.t) || n.mp[a] != oa {
		return 0, 0, false
	}
	b := a + (ob - oa)
	if b > len(n.t) || n.t[a:b] != s[oa:ob] || n.mp[b-1] != ob-1 {
		return 0, 0, false
	}
	return a, b, true
}

// text builder with a map
type builder struct {
	b  []byte
	mp []int
}

func (m *builder) byte(c byte, de int) {
	m.b = append(m.b, c)
	m.mp = append(m.mp, de)
}

func (m *builder) snippet(s string, a, b int) {
	for i := a; i < b; i++ {
		m.byte(s[i], i)
	}
}

func (m *builder) end(n int) (string, []int) { return string(m.b), append(m.mp, n) }

// composeAll: the stage's map (position in the stage -> position in the stage input) after the
// previous map.
func composeAll(prev, newOne []int) []int {
	if prev == nil {
		return newOne
	}
	for i, p := range newOne {
		newOne[i] = prev[p]
	}
	return newOne
}

// normalize: the text without the transport. ok=false when there is nothing to remove (the common
// case).
func normalize(s string) (*normText, bool) {
	if len(s) < 8 || !maybeTransport(s) {
		return nil, false
	}
	t, mp := s, []int(nil)
	stage := func(f func(string) (string, []int, bool)) {
		if u, m, ok := f(t); ok {
			t, mp = u, composeAll(mp, m)
		}
	}
	rawValue := false
	for k := 0; k < 3; k++ { // escaped text inside escaped text
		before := t
		stage(unescapeJSON)
		if t == before {
			break
		}
		rawValue = true
	}
	stage(stripANSIAndCase)
	before := t
	stage(stripPrefixes)
	if t != before && (reDiffCab.MatchString(before) || strings.Contains(before, "\n@@ ") || reGrepFile.MatchString(before)) {
		rawValue = true
	}
	stage(cutStrings)
	if mp == nil || t == s {
		return nil, false
	}
	return &normText{t, mp, rawValue}, true
}

// maybeTransport: a cheap test before the work (common text pays nothing).
func maybeTransport(s string) bool {
	if strings.Contains(s, `\n`) || strings.Contains(s, `\"`) || strings.IndexByte(s, 0x1b) >= 0 || strings.IndexByte(s, '\r') >= 0 ||
		strings.Contains(s, "\xe2\x94") || strings.Contains(s, "\xe2\x95") || strings.HasPrefix(s, "\ufeff") ||
		strings.Contains(s, "→") || strings.Contains(s, "```") || strings.Contains(s, "\n@@ ") || strings.HasPrefix(s, "@@ ") ||
		strings.Contains(s, "\n+++ ") || strings.Contains(s, "\n> ") || strings.HasPrefix(s, "> ") ||
		strings.Contains(s, `"""`) || strings.Contains(s, "'''") || strings.IndexByte(s, '`') >= 0 {
		return true
	}
	// numeric or file prefix in the first lines (cat -n, grep -n, blame, log)
	n, with := 0, 0
	for i := 0; i < len(s) && n < 6; n++ {
		j := strings.IndexByte(s[i:], '\n')
		f := len(s)
		if j >= 0 {
			f = i + j
		}
		if _, ok := linePrefix(s[i:f]); ok {
			with++
		}
		if j < 0 {
			break
		}
		i = f + 1
	}
	return with > 0
}

// unescapeJSON: a JSON string with \n, \t, \" (command output inside JSON, serialized tool result,
// notebook cell) becomes the decoded text. Each string with an escaped line break becomes a block
// of its own (the rest, the JSON structure, the readers already read in the original); without
// quotes around it, the whole text is decoded.
func unescapeJSON(s string) (string, []int, bool) {
	// in a JSON object, a one-line string with escaped quotes ("relation \"x\"") is already enough
	pj := looksLikeJSON(s)
	if strings.Count(s, `\n`) < 2 && strings.Count(s, `\"`) < 4 && !(pj && (strings.Contains(s, `\n`) || strings.Count(s, `\"`) >= 2)) {
		return s, nil, false
	}
	var m builder
	m.b = make([]byte, 0, len(s))
	m.mp = make([]int, 0, len(s)+1)
	decode := func(a, b int) {
		for i := a; i < b; i++ {
			c := s[i]
			if c != '\\' || i+1 >= b {
				m.byte(c, i)
				continue
			}
			switch s[i+1] {
			case 'n':
				m.byte('\n', i)
			case 't':
				m.byte('\t', i)
			case 'r':
			case '"', '\\', '/', '\'':
				m.byte(s[i+1], i+1)
			case 'u': // <: only the ASCII ones (the rest stays as it is)
				if i+5 < b && s[i+2] == '0' && s[i+3] == '0' && s[i+4] <= '7' && isHex(s[i+4]) && isHex(s[i+5]) {
					m.byte(byte(hexVal(s[i+4])<<4|hexVal(s[i+5])), i)
					i += 4
				} else {
					m.byte(c, i)
					continue
				}
			default:
				m.byte(c, i)
				continue
			}
			i++
		}
	}
	foundIt := false
	for i := 0; i < len(s); i++ {
		if s[i] != '"' {
			continue
		}
		j, nl, quotes := i+1, false, 0
		for ; j < len(s) && s[j] != '"' && s[j] != '\n'; j++ {
			if s[j] == '\\' && j+1 < len(s) {
				nl = nl || s[j+1] == 'n'
				if s[j+1] == '"' {
					quotes++
				}
				j++
			}
		}
		nl = nl || quotes >= 4 || pj && quotes >= 2
		if j >= len(s) || s[j] != '"' {
			break
		}
		if nl {
			decode(i+1, j)
			m.byte('\n', j)
			foundIt = true
		}
		i = j
	}
	if !foundIt {
		decode(0, len(s))
	}
	t, mp := m.end(len(s))
	return t, mp, true
}

// looksLikeJSON: the text is a JSON object or list (a single escaped line break is enough).
func looksLikeJSON(s string) bool {
	t := strings.TrimSpace(s)
	return len(t) > 2 && (t[0] == '{' || t[0] == '[') && strings.Contains(t, `":`)
}

var reANSI = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[@-Z\\-_])`)

// stripANSIAndCase: terminal colors and movements go; CR before LF goes; BOM goes; box borders
// (table drawing) become "|" and "-", which the table readers already understand.
func stripANSIAndCase(s string) (string, []int, bool) {
	if strings.IndexByte(s, 0x1b) < 0 && strings.IndexByte(s, '\r') < 0 && !strings.Contains(s, "\xe2\x94") &&
		!strings.Contains(s, "\xe2\x95") && !strings.HasPrefix(s, "\ufeff") {
		return s, nil, false
	}
	var ansi [][]int
	if strings.IndexByte(s, 0x1b) >= 0 {
		ansi = reANSI.FindAllStringIndex(s, -1)
	}
	var m builder
	m.b = make([]byte, 0, len(s))
	m.mp = make([]int, 0, len(s)+1)
	i := 0
	if strings.HasPrefix(s, "\ufeff") {
		i = 3
	}
	for i < len(s) {
		if len(ansi) > 0 && ansi[0][0] == i {
			i = ansi[0][1]
			ansi = ansi[1:]
			continue
		}
		for len(ansi) > 0 && ansi[0][0] < i {
			ansi = ansi[1:]
		}
		c := s[i]
		if c == '\r' && (i+1 == len(s) || s[i+1] == '\n') {
			i++
			continue
		}
		// U+2500..U+257F (box drawing): E2 94 80..E2 95 BF
		if c == 0xe2 && i+2 < len(s) && (s[i+1] == 0x94 || s[i+1] == 0x95) {
			r := rune(s[i+1]-0x94)<<6 | rune(s[i+2]&0x3f) // 0..127 inside the block
			switch r {
			case 0x00, 0x01, 0x04, 0x05, 0x08, 0x09, 0x4c, 0x4d, 0x50: // ─ ━ ┄ ┅ ┈ ┉ ╌ ╍ ═
				m.byte('-', i)
			default:
				m.byte('|', i)
			}
			i += 3
			continue
		}
		m.byte(c, i)
		i++
	}
	t, mp := m.end(len(s))
	return t, mp, t != s
}

var (
	// line number of the read tool (" 12→") and of cat -n / nl (" 12\t")
	reLineNum = regexp.MustCompile(`^ *\d{1,7}(?:→|\t)`)
	// grep -n with and without a file ("a/b.py:12:", "12:"), context line ("a/b.py-12-", only with
	// a file path: "x-2024-01-" of a CSV is not context); comma, ";" and "=" are not part of a file
	reGrepN = regexp.MustCompile(`^(?:[^\s:,;=]*[^\s:\d,;=][^\s:,;=]*:\d+:|[^\s:,;=]*[./][^\s:,;=]*?-\d+-|\d+[:-](?:\D|$))`)
	// git blame: "1a2b3c4d (Fulano 2024-01-02 10:11:12 -0300  12) "
	reBlame = regexp.MustCompile(`^\^?[0-9a-f]{7,40}(?: [^\s(]+)? \([^)]*\d{4}-\d\d-\d\d[^)]*?\d+\) `)
	// log stamp at the start of the line, with the level after it
	reStamp    = regexp.MustCompile(`^\[?\d{4}-\d\d-\d\d[T ]\d\d:\d\d:\d\d(?:[.,]\d+)?(?:Z|[+-]\d\d:?\d\d)?\]?(?:\s+\[?(?:TRACE|DEBUG|INFO|NOTICE|WARN|WARNING|ERROR|FATAL|CRITICAL)\]?)?[ \t]`)
	reGrepFile = regexp.MustCompile(`(?m)^(?:[^\s:,;=]*[^\s:\d,;=][^\s:,;=]*:\d+:|[^\s:,;=]*[./][^\s:,;=]*?-\d+-)`)
	reDiffCab  = regexp.MustCompile(`^(?:@@ .*@@|diff --git |index [0-9a-f]+\.\.|\+\+\+ |--- (?:a/|/dev/null|\S+\t)|new file mode|deleted file mode)`)
)

// linePrefix: the length of the transport prefix of the line (without diff, which depends on the
// block).
func linePrefix(l string) (int, bool) {
	if l == "" {
		return 0, false
	}
	if c := l[0]; c == ' ' || c >= '0' && c <= '9' {
		if m := reLineNum.FindStringIndex(l); m != nil {
			return m[1], true
		}
	}
	if m := reBlame.FindStringIndex(l); m != nil {
		return m[1], true
	}
	if l[0] >= '0' && l[0] <= '9' || l[0] == '[' {
		if m := reStamp.FindStringIndex(l); m != nil {
			return m[1], true
		}
	}
	if m := reGrepN.FindStringIndex(l); m != nil {
		return m[1], true
	}
	return 0, false
}

// stripPrefixes: line prefixes (number, grep, blame, log, quote, diff) and code fences. A grep or
// numeric prefix only counts when most lines have it (otherwise "12: x" of a YAML would become a
// prefix); the diff one, when there is a diff header in the text.
func stripPrefixes(s string) (string, []int, bool) {
	ls := wrapLines(s)
	pfx := make([]int, len(ls))
	n, with := 0, 0
	diff := false
	for k, l := range ls {
		line := s[l[0]:l[1]]
		if strings.TrimSpace(line) == "" {
			continue
		}
		n++
		if p, ok := linePrefix(line); ok {
			pfx[k] = p
			with++
		}
		if !diff && reDiffCab.MatchString(line) {
			diff = true
		}
	}
	majority := with > 0 && with*2 >= n
	changed := false
	var m builder
	m.b = make([]byte, 0, len(s))
	m.mp = make([]int, 0, len(s)+1)
	for k, l := range ls {
		a, b := l[0], l[1]
		line := s[a:b]
		if majority && pfx[k] > 0 {
			a += pfx[k]
			changed = true
		} else if majority && line == "--" { // grep group separator
			a = b
			changed = true
		}
		line = s[a:b]
		switch {
		case diff && reDiffCab.MatchString(line):
			a, changed = b, true
		case diff && line != "" && (line[0] == '+' || line[0] == '-' || line[0] == ' '):
			a, changed = a+1, true
		case strings.HasPrefix(line, "```") || strings.HasPrefix(strings.TrimLeft(line, " "), "```"):
			a, changed = b, true
		}
		for a < b && s[a] == '>' { // markdown quote, including nested
			a++
			if a < b && s[a] == ' ' {
				a++
			}
			changed = true
		}
		e := b
		for e > a && (s[e-1] == ' ' || s[e-1] == '\t') {
			e--
		}
		if e != b {
			changed = true
		}
		m.snippet(s, a, e)
		nl := l[1]
		if nl < len(s) && s[nl] == '\r' {
			nl++
			changed = true
		}
		if nl < len(s) {
			m.byte('\n', nl)
		}
	}
	if !changed {
		return s, nil, false
	}
	t, mp := m.end(len(s))
	return t, mp, true
}

func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func hexVal(c byte) byte {
	switch {
	case c >= 'a':
		return c - 'a' + 10
	case c >= 'A':
		return c - 'A' + 10
	}
	return c - '0'
}

// cutStrings: the content of a multi-line string in code (triple quotes of Python, Java, Kotlin,
// Scala and C#; backtick of JS, TS and Go) starts and ends on a line of its own. The first line of
// the content shares the line with the code that opens the string (dados = """tabela,coluna,tipo)
// and the last one with what closes it (col_x VARCHAR(14)`); the structure readers (table, schema,
// YAML) read whole lines. Only the line is broken: the code and the delimiter stay in the text (the
// SQL reader uses the delimiter to know where the statement ends). The delimiter only opens when it
// is odd on the line (opened and closed on the same line changes nothing).
func cutStrings(s string) (string, []int, bool) {
	ls := wrapLines(s)
	isOpen := ""
	changed := false
	var m builder
	m.b = make([]byte, 0, len(s))
	m.mp = make([]int, 0, len(s)+1)
	for _, l := range ls {
		a, b := l[0], l[1]
		line := s[a:b]
		cut := -1 // where the line is broken (position in the line)
		if isOpen == "" {
			for _, q := range []string{`"""`, "'''", "`"} {
				if n := countQuotes(line, q); n%2 == 1 {
					isOpen = q
					if p := lastQuote(line, q) + len(q); strings.TrimSpace(line[p:]) != "" {
						cut = p
					}
					break
				}
			}
		} else if p := lastQuote(line, isOpen); p >= 0 && countQuotes(line, isOpen)%2 == 1 {
			if strings.TrimSpace(line[:p]) != "" {
				cut = p
			}
			isOpen = ""
		}
		if cut > 0 {
			m.snippet(s, a, a+cut)
			m.byte('\n', a+cut-1) // the new break takes the place of the last byte before it
			a, changed = a+cut, true
		}
		m.snippet(s, a, b)
		if l[1] < len(s) {
			m.byte('\n', l[1])
		}
	}
	if !changed {
		return s, nil, false
	}
	t, mp := m.end(len(s))
	return t, mp, true
}

// lastQuote: the position of the last unescaped occurrence of q in s (-1 if none).
func lastQuote(s, q string) int {
	u := -1
	for p := 0; p+len(q) <= len(s); {
		switch {
		case s[p] == '\\':
			p += 2
		case s[p:p+len(q)] == q:
			u = p
			p += len(q)
		default:
			p++
		}
	}
	return u
}
