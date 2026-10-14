package mask

import (
	"regexp"
	"strings"
	"unicode"
)

// Detection by field name, part 3: tables (CSV, markdown, terminal, spreadsheet).

// findTables looks for tables with a header and masks the cells of the sensitive columns. It only
// applies to the lines right below the header, with the same shape. Accepted forms:
//   - with a separator: CSV, TSV, ";", "|", markdown, terminal table (mysql, psql);
//   - lines in Python format: ('NOME', 'CPF') / ['NOME', 'CPF'] (openpyxl, csv.reader);
//   - columns aligned by spaces: the printout of a spreadsheet or DataFrame (pandas),
//     "column -t", terminal output without borders.
//
// This is how an Excel spreadsheet is covered: the .xlsx never goes to the API as a file; what goes
// is the text a tool extracted from it (CSV, markdown, printed DataFrame, tuples).
func (m *Masker) findTables(s string, add func(ini, end int, typ string)) {
	line := func(ini int) (string, int) { // the line that starts at start, and the start of the next one
		f := strings.IndexByte(s[ini:], '\n')
		if f < 0 {
			return s[ini:], len(s)
		}
		return s[ini : ini+f], ini + f + 1
	}
	for ini := 0; ini < len(s); {
		l, next := line(ini)
		// Claude Code's read tool puts the line number before each line ("12<tab>content"). Without
		// removing it, the first column of every table would have the number glued to the value and
		// would not be recognized.
		num := lineNumber(l)
		lineStart := ini
		l, ini = l[num:], ini+num
		if classes, sep := m.header(l); classes != nil && !continuesTable(s, lineStart, sep, len(classes)) {
			ini = next
			for ini < len(s) {
				l, next = line(ini)
				if num > 0 { // if the header had a line number, the lines have it too
					k := lineNumber(l)
					l, ini = l[k:], ini+k
				}
				if !isMarkdownSeparator(l) {
					cells := splitText(l, sep)
					if sep == ',' && notTableLine(l) {
						cells = nil
					}
					if len(cells) != len(classes) {
						break // end of the table; this line becomes a header candidate again
					}
					for i, c := range cells {
						if classes[i] != "" {
							a, b := trim(l, c[0], c[1])
							markIt(s, ini+a, ini+b, classes[i], add)
						}
					}
				}
				ini = next
			}
			continue
		}
		if m.alignedHeader(l) != nil && !continuesTable(s, lineStart, ' ', 0) {
			if end := m.alignedTable(s, ini, num > 0, add); end > ini {
				ini = end
				continue
			}
		}
		ini = next
	}
}

// continuesTable: is the line that starts at start the continuation of a table that was already
// coming from the lines above? A header can only be the FIRST line of the table: in a documentation
// table ("| Column | Type | Description |"), the data line "| USER_ID | NUMBER | ... |" is not a
// header, however much "USER_ID" is a field name.
func continuesTable(s string, ini int, sep byte, n int) bool {
	if ini == 0 {
		return false
	}
	a := strings.LastIndexByte(s[:ini-1], '\n') + 1
	prev := s[a : ini-1]
	prev = prev[lineNumber(prev):]
	if strings.TrimSpace(prev) == "" {
		return false
	}
	if isMarkdownSeparator(prev) {
		// separator right below a header ("|---|"): continuation. Top border of a terminal table
		// ("+----+", with nothing above): it is not.
		if a == 0 {
			return false
		}
		b := strings.LastIndexByte(s[:a-1], '\n') + 1
		above := s[b : a-1]
		above = above[lineNumber(above):]
		return strings.TrimSpace(above) != "" && !isMarkdownSeparator(above)
	}
	if sep == ' ' { // table aligned by spaces: the line above also has columns separated by 2+ spaces
		return strings.Contains(strings.TrimSpace(prev), "  ")
	}
	return len(splitText(prev, sep)) == n
}

// notTableLine: the line has commas but is a list of SQL columns or an enumeration in text, not a
// CSV line. Signs: it starts indented, ends with a comma, has backticks or parentheses, or starts
// with an SQL word.
//
//	select start_time, user_name, role_name     (SQL)
//	    user_name,                              (SQL, one column per line)
//	`user_id`, `user_name`, `role_name`         (text)
var reSQLStart = regexp.MustCompile(`(?i)^(?:select|from|where|group|order|having|insert|update|delete|values|set|join|left|right|inner|on|and|or|with|union|create|alter|partition|by|case|when)\b`)

func notTableLine(l string) bool {
	if strings.IndexByte(l, ',') < 0 || strings.IndexByte(l, ';') >= 0 || strings.IndexByte(l, '\t') >= 0 || strings.IndexByte(l, '|') >= 0 {
		return false // only applies to comma-separated lines
	}
	t := strings.TrimRight(l, " \r")
	if t == "" {
		return true
	}
	if l[0] == ' ' || t[len(t)-1] == ',' || strings.ContainsAny(t, "`()") {
		return true
	}
	return reSQLStart.MatchString(t)
}

// lineNumber returns the length of the line number prefix of l (" 12<tab>" or " 12→"), or 0 if
// there is none.
func lineNumber(l string) int {
	i := 0
	for i < len(l) && l[i] == ' ' {
		i++
	}
	j := i
	for j < len(l) && j-i < 8 && l[j] >= '0' && l[j] <= '9' {
		j++
	}
	if j == i || j >= len(l) {
		return 0
	}
	if l[j] == '\t' {
		return j + 1
	}
	if strings.HasPrefix(l[j:], "→") {
		return j + len("→")
	}
	return 0
}

// splitText: the cells of a table line. sep 'P' is the Python format: (a, b) or [a, b].
func splitText(l string, sep byte) [][2]int {
	if sep != 'P' {
		return cellsOf(l, sep)
	}
	a, b := 0, len(l)
	for a < b && l[a] == ' ' {
		a++
	}
	for b > a && (l[b-1] == ' ' || l[b-1] == ',' || l[b-1] == '\r') {
		b--
	}
	if b-a < 2 || !((l[a] == '(' && l[b-1] == ')') || (l[a] == '[' && l[b-1] == ']')) {
		return nil
	}
	a, b = a+1, b-1
	var out [][2]int
	ini, quote := a, byte(0)
	for i := a; i < b; i++ {
		switch c := l[i]; {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == ',':
			out = append(out, [2]int{ini, i})
			ini = i + 1
		}
	}
	return append(out, [2]int{ini, b})
}

type alignedColumn struct {
	ini, end int // position of the column name in the header line, in characters
	class    string
}

// alignedHeader: is the line the header of a table aligned by spaces (column names separated by
// spaces), with some sensitive column?
func (m *Masker) alignedHeader(l string) []alignedColumn {
	if len(l) < 5 || len(l) > 2000 || strings.ContainsAny(l, "=(){}@:;|,\t\"'.!?") {
		return nil
	}
	rs := []rune(l)
	splitAt := func(minSpace int) []alignedColumn {
		var cols []alignedColumn
		for i := 0; i < len(rs); {
			for i < len(rs) && rs[i] == ' ' {
				i++
			}
			if i >= len(rs) {
				break
			}
			ini := i
			for i < len(rs) && !(rs[i] == ' ' && (minSpace == 1 || i+1 >= len(rs) || rs[i+1] == ' ')) {
				i++
			}
			cols = append(cols, alignedColumn{ini, i, m.class(string(rs[ini:i]))})
		}
		return cols
	}
	// how many sensitive columns the split recognizes (0 = it does not work as a header)
	sensitives := func(cols []alignedColumn) int {
		if len(cols) < 2 {
			return 0
		}
		sens := 0
		for _, c := range cols {
			name := string(rs[c.ini:c.end])
			// a loose sentence word ("o", "de", "foi"): not a column name
			if len(name) > 48 || (len(name) <= 3 && name == strings.ToLower(name) && c.class == "") {
				return 0
			}
			if c.class != "" && c.class != "name_loose" {
				sens++
			}
		}
		return sens
	}
	// The names may be separated by 2+ spaces (and have spaces inside) or by a single space
	// (pandas, without an index, leaves one space between columns). The split that recognizes more
	// sensitive columns wins.
	cols, c1 := splitAt(2), splitAt(1)
	if sensitives(c1) > sensitives(cols) {
		cols = c1
	}
	if sensitives(cols) == 0 {
		return nil
	}
	for i := range cols {
		if cols[i].class == "name_loose" {
			cols[i].class = "name_full"
		}
	}
	return cols
}

// alignedTable handles the table aligned by spaces whose header starts at s[start]. Returns where
// the table ends (start if the line was not a table header).
//
// The limits of each column are looked for BETWEEN the header names: between one name and the next
// there has to be a position that is a space on every line (the "lane"). It works with
// right-aligned values (pandas), left-aligned ones ("column -t"), with an index column and with a
// single space between columns.
func (m *Masker) alignedTable(s string, ini int, numbered bool, add func(ini, end int, typ string)) int {
	type lineV struct {
		ini int // where the line starts in s
		rs  []rune
		pos []int // character -> byte inside the line
	}
	var ls []lineV
	wid := 0
	for p := ini; p < len(s) && len(ls) < 5000; {
		f := strings.IndexByte(s[p:], '\n')
		txt := s[p:]
		if f >= 0 {
			txt = s[p : p+f]
		}
		q := p // where the line content starts (after the line number, if any)
		if numbered && len(ls) > 0 {
			k := lineNumber(txt)
			txt, q = txt[k:], p+k
		}
		if (len(ls) > 0 && strings.TrimSpace(txt) == "") || len(txt) > 4000 {
			break
		}
		rs := []rune(strings.TrimRight(txt, "\r"))
		pos := make([]int, len(rs)+1)
		b := 0
		for i, r := range rs {
			pos[i] = b
			b += len(string(r))
		}
		pos[len(rs)] = b
		ls = append(ls, lineV{q, rs, pos})
		if len(rs) > wid {
			wid = len(rs)
		}
		if f < 0 {
			break
		}
		p += f + 1
	}
	cols := m.alignedHeader(string(ls[0].rs))
	if cols == nil || len(ls) < 2 {
		return ini
	}
	// Which lines belong to the table: a line enters while there is still, between each header name
	// and the next, some position that is a space on all accepted lines. The first one that closes
	// one of these lanes (text that came right after, without a blank line) ends the table.
	free := make([]bool, wid+2) // the position is a space on all accepted lines
	for c := range free {
		free[c] = c >= len(ls[0].rs) || ls[0].rs[c] == ' '
	}
	n, hasIndex := 1, false
	for i := 1; i < len(ls); i++ {
		tmp := append([]bool(nil), free...)
		for c, r := range ls[i].rs {
			if r != ' ' {
				tmp[c] = false
			}
		}
		fits := true
		if hasIndex { // the line has to keep the lane between the index and the first column
			fits = false
			for c := 1; c < cols[0].ini; c++ {
				if tmp[c] {
					fits = true
					break
				}
			}
		}
		for j := 0; j+1 < len(cols) && fits; j++ {
			fits = false
			for c := cols[j].end; c < cols[j+1].ini; c++ {
				if tmp[c] {
					fits = true
					break
				}
			}
		}
		if !fits {
			break
		}
		free, n = tmp, i+1
		if i == 1 {
			// the first data line says whether there is an index column (pandas): something written
			// and, after it, a lane, all before the first header name
			for c := 1; c < cols[0].ini && c < len(ls[1].rs); c++ {
				// two spaces in a row: a single space can be the one inside a name
				if free[c] && free[c+1] && c+1 < cols[0].ini && strings.TrimSpace(string(ls[1].rs[:c])) != "" {
					hasIndex = true
					break
				}
			}
		}
	}
	if n < 2 {
		return ini
	}
	if n == 2 {
		// A single data line: there is no way to know where one column ends and the other starts
		// (every space in the line looks like a lane). When in doubt, mask: every long number if
		// the table has a document column, and every sequence of words if it has a name column. It
		// may catch a value from another column; it does not let the one of the sensitive column
		// through.
		hasName := false
		var others []string // sensitive classes that are not names
		for _, c := range cols {
			switch c.class {
			case "name", "name_full":
				hasName = true
			case "":
			default:
				others = append(others, c.class)
			}
		}
		classNum := ""
		if len(others) > 0 {
			classNum = others[0]
		}
		// a value that is not a name is marked with the first sensitive class that accepts it
		markOther := func(a, b int) {
			for _, c := range others {
				if typ, ok := fieldValue(c, s[a:b]); ok {
					add(a, b, typ)
					return
				}
			}
		}
		l := ls[1]
		letter := func(r rune) bool { return unicode.IsLetter(r) || r == '\'' || r == '-' || r == '.' }
		for i := 0; i < len(l.rs); {
			if l.rs[i] == ' ' {
				i++
				continue
			}
			j := i
			lettersOnly := true
			for j < len(l.rs) && l.rs[j] != ' ' {
				lettersOnly = lettersOnly && letter(l.rs[j])
				j++
			}
			if lettersOnly && hasName {
				for j+1 < len(l.rs) && l.rs[j] == ' ' && letter(l.rs[j+1]) { // joins the following words
					k := j + 1
					for k < len(l.rs) && letter(l.rs[k]) {
						k++
					}
					if k < len(l.rs) && l.rs[k] != ' ' {
						break
					}
					j = k
				}
				// only what looks like a person's name (first and last name capitalized)
				markIt(s, l.ini+l.pos[i], l.ini+l.pos[j], "name_full", add)
			} else if !lettersOnly && classNum != "" && (i > 0 || cols[0].ini == 0) { // i > 0: not the line index
				markOther(l.ini+l.pos[i], l.ini+l.pos[j])
			}
			i = j
		}
		return min(len(s), l.ini+l.pos[len(l.rs)]+1)
	}
	{
		lane := func(c int) (int, int) { // the lane that contains position c: [a, b)
			a, b := c, c+1
			for a > 0 && free[a-1] {
				a--
			}
			for b < wid && free[b] {
				b++
			}
			return a, b
		}
		left := make([]int, len(cols)) // limits of each column, in characters: [left, right)
		dir := make([]int, len(cols))
		ok := true
		// before the first column there may be an index (pandas): the column starts after the first
		// lane that comes after some character
		left[0] = 0
		for c := 0; c < cols[0].ini; c++ {
			if free[c] {
				_, b := lane(c)
				if c == 0 || b <= cols[0].ini {
					left[0] = b
				}
				if c > 0 {
					break
				}
				c = b
			}
		}
		if left[0] > cols[0].ini {
			left[0] = cols[0].ini
		}
		dir[len(cols)-1] = wid
		for j := 0; j+1 < len(cols) && ok; j++ {
			e, f := cols[j].end, cols[j+1].ini-1 // right after a name, right before the next
			switch {
			case free[e] && free[f]:
				a1, b1 := lane(e)
				a2, b2 := lane(f)
				if a1 == a2 { // a single lane between the two names
					dir[j], left[j+1] = a1, b1
				} else if cols[j].class != "" { // there are values in between: when in doubt, they go with the sensitive column
					dir[j], left[j+1] = a2, b2
				} else {
					dir[j], left[j+1] = a1, b1
				}
			case free[e]: // values of the next column pass to the left of its name (right-aligned)
				a, b := lane(e)
				dir[j], left[j+1] = a, b
			case free[f]: // values of this column pass to the right of its name (left-aligned)
				a, b := lane(f)
				dir[j], left[j+1] = a, b
			default: // looks for a lane in the middle
				ok = false
				for c := e; c <= f; c++ {
					if free[c] {
						a, b := lane(c)
						dir[j], left[j+1], ok = a, b, true
						break
					}
				}
			}
		}
		if !ok {
			return ini
		}
		for _, l := range ls[1:n] {
			if isMarkdownSeparator(string(l.rs)) {
				continue
			}
			for j, c := range cols {
				if c.class == "" {
					continue
				}
				a, b := min(left[j], len(l.rs)), min(dir[j], len(l.rs))
				for a < b && l.rs[a] == ' ' {
					a++
				}
				for b > a && l.rs[b-1] == ' ' {
					b--
				}
				if b > a {
					markIt(s, l.ini+l.pos[a], l.ini+l.pos[b], c.class, add)
				}
			}
		}
		lastPos := ls[n-1]
		return min(len(s), lastPos.ini+lastPos.pos[len(lastPos.rs)]+1)
	}
}

// header: is the line the header of a table with some sensitive column? Returns the class of each
// column and the separator. Rejects early what does not look like a header.
func (m *Masker) header(line string) ([]string, byte) {
	if len(line) < 5 || len(line) > 4000 {
		return nil, 0
	}
	if cells := splitText(line, 'P'); len(cells) >= 2 { // ('NOME', 'CPF') or ['NOME', 'CPF']
		cs := make([]string, len(cells))
		sens := 0
		for i, c := range cells {
			a, b := trim(line, c[0], c[1])
			if b-a > 48 {
				return nil, 0
			}
			if cs[i] = m.class(line[a:b]); cs[i] != "" && cs[i] != "name_loose" {
				sens++
			}
		}
		if sens == 0 {
			return nil, 0
		}
		for i := range cs {
			if cs[i] == "name_loose" {
				cs[i] = "name_full"
			}
		}
		return cs, 'P'
	}
	if notTableLine(line) {
		return nil, 0
	}
	// the separator is the most frequent among tab, "|", ";" and ","
	var n [4]int
	seps := [4]byte{'\t', '|', ';', ','}
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\t':
			n[0]++
		case '|':
			n[1]++
		case ';':
			n[2]++
		case ',':
			n[3]++
		case '=', '(', ')', '{', '}', '@':
			return nil, 0 // code or data, not column names
		}
	}
	best := -1
	for i := range n {
		if n[i] > 0 && (best < 0 || n[i] > n[best]) {
			best = i
		}
	}
	if best < 0 {
		return nil, 0
	}
	cells := cellsOf(line, seps[best])
	if len(cells) < 2 {
		return nil, 0
	}
	cs := make([]string, len(cells))
	sens := 0
	for i, c := range cells {
		a, b := trim(line, c[0], c[1])
		cellText := line[a:b]
		if len(cellText) > 48 || strings.Contains(cellText, ":") || strings.Contains(cellText, "//") {
			return nil, 0
		}
		if cs[i] = m.class(cellText); cs[i] != "" && cs[i] != "name_loose" {
			sens++
		}
	}
	if sens == 0 {
		return nil, 0
	}
	for i := range cs { // "nome" alone, in a table with another person column, is a person's name
		if cs[i] == "name_loose" {
			cs[i] = "name_full"
		}
	}
	return cs, seps[best]
}

// cellsOf splits the line by the separator, respecting double quotes; returns [start,end) of each
// cell.
func cellsOf(line string, sep byte) [][2]int {
	if strings.IndexByte(line, sep) < 0 {
		return nil
	}
	var out [][2]int
	ini, quotes := 0, false
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '"':
			quotes = !quotes
		case line[i] == sep && !quotes:
			out = append(out, [2]int{ini, i})
			ini = i + 1
		}
	}
	out = append(out, [2]int{ini, len(line)})
	if sep == '|' && len(out) >= 3 { // "| a | b |": the empty ends are not columns
		if strings.TrimSpace(line[out[0][0]:out[0][1]]) == "" {
			out = out[1:]
		}
		if n := len(out); n > 0 && strings.TrimSpace(line[out[n-1][0]:out[n-1][1]]) == "" {
			out = out[:n-1]
		}
	}
	return out
}

func trim(line string, a, b int) (int, int) {
	for a < b && (line[a] == ' ' || line[a] == '\t' || line[a] == '"' || line[a] == '\'' || line[a] == '\r') {
		a++
	}
	for b > a && (line[b-1] == ' ' || line[b-1] == '\t' || line[b-1] == '"' || line[b-1] == '\'' || line[b-1] == '\r') {
		b--
	}
	return a, b
}

// isMarkdownSeparator: a line of dashes only, like the one that separates the header in a markdown
// table ("|---|:-:|") or a terminal one ("+----+----+", "-----+-----", "=====").
func isMarkdownSeparator(line string) bool {
	t := strings.TrimSpace(line)
	return len(t) >= 3 && strings.Trim(t, "|-:+= ") == ""
}

// identifiesPerson: finding types that, in a table line, show the line belongs to a person.
var identifiesPerson = map[string]bool{"cpf": true, "email": true, "phone": true, "rg": true, "cnh": true, "pis": true, "birthdate": true}

// namesInLine: in a table line without a header (pasted in the chat, a piece of CSV) that already
// has a detected CPF, e-mail or telephone, the cell that looks like a full name is the person's
// name. "Maria Lopes | 529.982.247-25 | maria@x.com" -> "Maria Lopes" is also masked.
func namesInLine(s string, findings []Finding) []Finding {
	lines := map[int]bool{} // start of the lines that have a person identifier
	for _, a := range findings {
		if identifiesPerson[a.Type] {
			// a line of more than 2000 characters is not a table line: the search stops there
			a0 := max(0, a.Start-2001)
			k := strings.LastIndexByte(s[a0:a.Start], '\n')
			if k < 0 && a0 > 0 {
				continue
			}
			lines[a0+k+1] = true
		}
	}
	var out []Finding
	for ini := range lines {
		z := min(len(s), ini+2001)
		end := strings.IndexByte(s[ini:z], '\n')
		if end < 0 {
			end = z - ini
		}
		l := s[ini : ini+end]
		if len(l) > 2000 {
			continue
		}
		var sep byte
		for _, c := range []byte{'|', '\t', ';', ','} {
			if strings.Count(l, string(c)) >= 1 {
				sep = c
				break
			}
		}
		if sep == 0 {
			continue
		}
		for _, c := range cellsOf(l, sep) {
			a, b := trim(l, c[0], c[1])
			if _, ok := fieldValue("name_full", l[a:b]); ok && len(strings.Fields(l[a:b])) >= 2 {
				out = append(out, Finding{ini + a, ini + b, "name", l[a:b]})
			}
		}
	}
	return out
}
