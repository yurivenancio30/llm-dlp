package mask

import (
	"regexp"
	"strings"
)

// Table reader (see docs/structures.md, sections Database client output and Tabular data). The
// table is recognized by its shape, not by the tool: header followed by a line of dashes (psql,
// sqlcmd, sqlplus, mysql, snowsql, bq, db2, markdown), lines with the same number of separators
// outside quotes (CSV per RFC 4180, TSV, ";", "|"; box borders arrive as "|" through
// normalization), columns aligned by spaces without a dash line (DataFrame print, R, command-line
// listings), lines that are tuples or lists, HTML table and vertical record ("-[ RECORD 1 ]-" +
// "key | value"). Where the header says the column holds an object name (the standard names of
// information_schema and of the catalogs, or a type word in any spelling: see entKey), the values
// are objects. The other header cells are column names.

// catalog header -> entity of the values in that column
var catalogHdr = map[string]string{
	"table_name": "table", "tablename": "table", "tabname": "table", "table": "table", "tabela": "table",
	"view_name": "table", "object_name": "table", "objeto": "table", "object": "table", "tableid": "table",
	"table_schema": "schema", "schema_name": "schema", "schemaname": "schema", "tabschema": "schema", "schema": "schema",
	"routine_schema": "schema", "specific_schema": "schema", "constraint_schema": "schema", "owner": "user",
	"table_catalog": "database", "catalog_name": "database", "database_name": "database", "database": "database",
	"datname": "database", "banco": "database", "routine_catalog": "database",
	"column_name": "column", "colname": "column", "coluna": "column", "column": "column", "field": "column",
	"routine_name": "procedure", "specific_name": "procedure", "procedure_name": "procedure", "function_name": "procedure",
	"index_name": "index", "indname": "index", "key_name": "index", "constraint_name": "index",
	"server": "server", "servidor": "server", "server_name": "server", "host_name": "server", "hostname": "server",
	"usename": "user", "rolname": "user", "grantee": "user", "user_name": "user", "username": "user",
}

// the tools' own labels: never a user's column name
var outputLabels = map[string]bool{"name": true, "type": true, "null": true, "key": true, "default": true, "extra": true,
	"rows": true, "count": true, "mean": true, "std": true, "min": true, "max": true, "dtype": true, "non-null": true,
	"labels": true, "id": true, "value": true, "description": true, "status": true, "size": true, "kind": true,
	"comment": true, "collation": true, "nullable": true, "length": true, "precision": true, "scale": true}

var (
	reSeparator = regexp.MustCompile(`^\s*[|+]?[\s|+:]*[-=─━]{3,}[-=─━+|:\s┼┬┴╋]*$`)
	reIdentCell = regexp.MustCompile(`^[\p{L}_][\p{L}0-9_$#.\-]*$`)
	// header cell: identifier, up to 3 words ("primary key", "APP VERSION"), "null?"
	reHdrCell  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$#.\-]*(?: [A-Za-z_][A-Za-z0-9_$#.\-]*){0,2}\??$`)
	reTablesIn = regexp.MustCompile(`(?i)^tables_in_([A-Za-z_][\w$]*)$`)
)

type cell struct{ a, b int } // position in the text

// headerCell: the cell has the shape of a catalog header: an identifier ("null?" accepted) or, in a
// "|", TAB or dash table (not in CSV, where the comma also separates SQL lists), up to 3 words all
// lowercase or all uppercase ("primary key", "APP VERSION"), which is how the tools write them. A
// document title ("Data Type") does not. Under a dash line, the band already delimits the cell: any
// short text counts ("Non-Null Count").
func headerCell(v string, sep byte, dashes bool) bool {
	if !reHdrCell.MatchString(v) {
		return false
	}
	if !strings.Contains(v, " ") || dashes { // under the dash line, the band already delimits the cell
		return true
	}
	return (sep == '|' || sep == '\t' || sep == 0) && (strings.ToLower(v) == v || strings.ToUpper(v) == v)
}

// entHeader: the entity of a column's values by the header name.
func entHeader(v string) (string, bool) {
	if e, ok := catalogHdr[strings.ToLower(v)]; ok {
		return e, true
	}
	if e, strong := entKey(v); strong && e != "" {
		return e, true
	}
	return "", false
}

// countOutside: how many c there are in l outside double quotes (CSV: "a,b" is a single cell).
func countOutside(l string, c byte) int {
	n, quotes := 0, false
	for i := 0; i < len(l); i++ {
		switch l[i] {
		case '"':
			quotes = !quotes
		case c:
			if !quotes {
				n++
			}
		}
	}
	return n
}

// objCells splits the line [ini, end) of s into cells: by the separator sep (outside quotes), or by
// the dash bands of the separator line (table aligned by spaces).
func objCells(s string, ini, end int, sep byte, ranges []cell) []cell {
	var out []cell
	add := func(a, b int) {
		for a < b && (s[a] == ' ' || s[a] == '\t' || s[a] == '"') {
			a++
		}
		for b > a && (s[b-1] == ' ' || s[b-1] == '\t' || s[b-1] == '\r' || s[b-1] == '"') {
			b--
		}
		out = append(out, cell{a, b})
	}
	if sep != 0 {
		a, quotes := ini, false
		for i := ini; i <= end; i++ {
			if i < end && s[i] == '"' && sep != '|' {
				quotes = !quotes
			}
			if i == end || s[i] == sep && !quotes {
				add(a, i)
				a = i + 1
			}
		}
		if sep == '|' { // "| a | b |", "|| a | b ||": bordas vazias
			for len(out) > 0 && out[0].a == out[0].b {
				out = out[1:]
			}
			for n := len(out); n > 0 && out[n-1].a == out[n-1].b; n = len(out) {
				out = out[:n-1]
			}
		}
		return out
	}
	for _, f := range ranges {
		a, b := ini+f.a, ini+f.b
		if a >= end {
			out = append(out, cell{end, end})
			continue
		}
		add(a, min(b, end))
	}
	return out
}

// lineTokens: the words (separated by spaces) of s[ini:end).
func lineTokens(s string, ini, end int) []cell {
	var out []cell
	for i := ini; i < end; {
		for i < end && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		j := i
		for j < end && s[j] != ' ' && s[j] != '\t' {
			j++
		}
		if j > i {
			out = append(out, cell{i, j})
		}
		i = j
	}
	return out
}

// hasGap: the line has a gap of 2+ spaces between words, a TAB, or starts indented.
func hasGap(l string) bool {
	t := strings.TrimRight(l, " \t")
	if strings.HasPrefix(t, " ") {
		return true
	}
	return strings.Contains(strings.TrimLeft(t, " "), "  ") || strings.Contains(t, "\t")
}

// looksLikeCode: the line has code punctuation (block, assignment, end of statement, comment,
// operator, lowercase call): indented code with aligned spaces is not a table.
func looksLikeCode(l string) bool {
	if strings.ContainsAny(l, "{};") || strings.Contains(l, ":=") || strings.Contains(l, "//") || strings.Contains(l, "==") ||
		strings.Contains(l, "!=") || strings.Contains(l, "&&") || strings.Contains(l, "||") || strings.Contains(l, " = ") {
		return true
	}
	for i := 1; i < len(l); i++ {
		if l[i] == '(' && (l[i-1] >= 'a' && l[i-1] <= 'z' || l[i-1] >= '0' && l[i-1] <= '9') {
			return true // f(x), int64(v)
		}
	}
	return false
}

var reFixedHdr = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$#.\-()/?]*$`)

// fixedColumns: the line s[l] as a header of space-aligned columns (each word is a column); nil if
// it does not have the shape (fewer than two words, a number, sentence punctuation).
func fixedColumns(s string, l cell) []cell {
	cs := lineTokens(s, l.a, l.b)
	if len(cs) < 2 || len(cs) > 40 {
		return nil
	}
	// prose has no columns: the header has a gap of 2+ spaces or a TAB between the words
	// (indentation alone is an indented RST/Markdown paragraph; a DataFrame separates columns by 2+
	// spaces); the data lines may just start indented (see alignFixed)
	if h := strings.TrimSpace(s[l.a:l.b]); !strings.Contains(h, "  ") && !strings.Contains(h, "\t") || looksLikeCode(s[l.a:l.b]) {
		return nil
	}
	for _, c := range cs {
		if !reFixedHdr.MatchString(s[c.a:c.b]) {
			return nil
		}
	}
	return cs
}

// alignFixed: the cells of line l under the columns hdr (positions relative to the start of the
// line). Each word goes to the column it overlaps the most (value aligned right or left) or, with
// no overlap, to the column where it starts; a word before the first column is an index. ok=false
// if the line does not align (less than half of the columns filled).
func alignFixed(s string, l cell, hdr []cell, hIni int) ([]cell, bool) {
	if !hasGap(s[l.a:l.b]) || looksLikeCode(s[l.a:l.b]) || len(lineTokens(s, l.a, l.b)) > 2*len(hdr)+4 {
		return nil, false
	}
	out := make([]cell, len(hdr))
	for k := range out {
		out[k] = cell{-1, -1}
	}
	n := 0
	for _, t := range lineTokens(s, l.a, l.b) {
		a, b := t.a-l.a, t.b-l.a
		best, under := -1, 0
		for k, c := range hdr {
			ca, cb := c.a-hIni, c.b-hIni
			if o := min(b, cb) - max(a, ca); o > under {
				best, under = k, o
			}
		}
		if best < 0 {
			for k := len(hdr) - 1; k >= 0; k-- {
				if a >= hdr[k].a-hIni {
					best = k
					break
				}
			}
		}
		if best < 0 {
			continue // index on the left
		}
		if out[best].a < 0 {
			out[best] = t
			n++
		} else {
			out[best].b = t.b // value with spaces ("2 days ago")
		}
	}
	return out, n >= 2 && n*2 >= len(hdr)
}

// typeTitle: is the line right above the header (up to 3 lines, no borders) just a type word
// ("Buckets", "TABLES")? Then the "name" column holds names of that type.
func typeTitle(s string, lines []cell, i int) string {
	for k := i - 1; k >= 0 && k >= i-3; k-- {
		l := strings.Trim(s[lines[k].a:lines[k].b], " \t|+-=")
		if l == "" {
			continue
		}
		if strings.ContainsAny(l, " \t") {
			return ""
		}
		e, _ := entHeader(strings.ToLower(l))
		return e
	}
	return ""
}

func findTablesObj(s string, add func(ObjFinding)) { findTablesD(s, nil, add) }

// findTablesD: with d != nil (the command hint, see call_command.go), only the columns the header
// does not type and the hint does (the rest already came out without the hint).
func findTablesD(s string, d *outputHint, add func(ObjFinding)) {
	addWithoutHint := add
	if d != nil {
		addWithoutHint = func(ObjFinding) {}
	}
	findTuples(s, d, add)
	findHTMLTable(s, d, add)
	if strings.Count(s, "\n") < 2 {
		return
	}
	var lines []cell // [start, end) of each line
	for i := 0; i < len(s); {
		j := strings.IndexByte(s[i:], '\n')
		if j < 0 {
			lines = append(lines, cell{i, len(s)})
			break
		}
		lines = append(lines, cell{i, i + j})
		i += j + 1
	}
	label := make([]bool, len(lines))
	for i, l := range lines {
		label[i] = strings.IndexByte(s[l.a:l.b], '\t') > 0 && labelAndValues(s, l, addWithoutHint)
	}
	for i := 0; i+1 < len(lines); i++ {
		if label[i] {
			continue
		}
		if k := looseColumn(s, lines, i, add); k > i {
			i = k - 1
			continue
		}
		h, next := lines[i], lines[i+1]
		hdr := s[h.a:h.b]
		if strings.TrimSpace(hdr) == "" || reSeparator.MatchString(hdr) {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(hdr), "-[ RECORD ") {
			i = verticalRecord(s, lines, i+1, addWithoutHint) - 1
			continue
		}
		var sep byte
		var ranges, fixedOnes []cell
		data := i + 1
		if reSeparator.MatchString(s[next.a:next.b]) {
			data = i + 2
			if strings.Contains(hdr, "|") {
				sep = '|'
			} else { // dash bands of the separator line
				l := s[next.a:next.b]
				for k := 0; k < len(l); {
					if l[k] == '-' || l[k] == '=' {
						a := k
						for k < len(l) && (l[k] == '-' || l[k] == '=') {
							k++
						}
						ranges = append(ranges, cell{a, k})
					} else {
						k++
					}
				}
				if len(ranges) < 1 {
					continue
				}
				if len(ranges) > 1 { // each band goes up to the next one (values may run past the dashes)
					for k := 0; k+1 < len(ranges); k++ {
						ranges[k].b = ranges[k+1].a
					}
				}
				ranges[len(ranges)-1].b = 1 << 20
			}
		} else {
			for _, c := range []byte{'\t', ',', ';', '|'} {
				n := countOutside(hdr, c)
				if n >= 1 && countOutside(s[next.a:next.b], c) == n {
					sep = c
					break
				}
			}
			if sep == 0 {
				fixedOnes = fixedColumns(s, h)
				if fixedOnes == nil {
					continue
				}
			}
		}
		var cs []cell
		if fixedOnes != nil {
			cs = fixedOnes
		} else {
			cs = objCells(s, h.a, h.b, sep, ranges)
		}
		if len(cs) < 2 {
			continue
		}
		ok := true
		for _, c := range cs {
			// header shaped like an identifier: no space, operator or unit ("Speed MiB/s", "for i
			// := 0" are a benchmark table and a code snippet, not a catalog)
			if v := s[c.a:c.b]; v == "#" && len(ranges) > 0 { // index column (df.info())
				continue
			}
			if c.a == c.b || fixedOnes == nil && !headerCell(s[c.a:c.b], sep, ranges != nil) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		// the data lines
		var rows [][]cell
		k := data
		for ; k < len(lines); k++ {
			l := lines[k]
			line := s[l.a:l.b]
			if strings.TrimSpace(line) == "" {
				break
			}
			if reSeparator.MatchString(line) {
				continue
			}
			if fixedOnes != nil {
				vs, ok := alignFixed(s, l, fixedOnes, h.a)
				if !ok {
					break
				}
				rows = append(rows, vs)
				continue
			}
			rows = append(rows, objCells(s, l.a, l.b, sep, ranges))
		}
		if fixedOnes != nil && len(rows) == 0 {
			continue
		}
		noHdr := fixedOnes != nil && len(rows) < 2
		var dv *outputHint
		if d == nil {
			// the listing in sight right above (pasted terminal, README: "$ airflow dags list"):
			// the header belongs to the tool, not an owner's column, and the listing types the
			// identity column as in tool_use. Listing only: under "$ cat x.csv" or "$ python
			// relatorio.py" the header belongs to the owner and is still a column
			if cmd, ok := commandAbove(s, lines, i); ok {
				if dv = listingHint(cmd); dv != nil {
					noHdr = true
				}
			}
		}
		classifyTableD(s, cs, rows, typeTitle(s, lines, i), noHdr, sep == 0 || sep == '|', d, add)
		if dv != nil {
			classifyTableD(s, cs, rows, "", true, sep == 0 || sep == '|', dv, add)
		}
		i = k - 1
	}
}

// shell prompt line: "$ cmd", "% cmd", "user@host:~/x$ cmd", "(venv) $ cmd", "PS C:\x> cmd"
var rePromptShell = regexp.MustCompile(`^(?:\([\w.-]+\)\s*)?(?:[\w.-]+@[\w.-]+(?::[^\s$%]*)?\s*)?[$%]\s+(\S.*)$|^PS [^>\n]{0,80}>\s+(\S.*)$`)

// line that is itself a command: lowercase program and arguments, no sentence punctuation
var reCommandLine = regexp.MustCompile(`^[a-z][\w.-]*(?:\s+[\w./:=@,-]+){1,12}$`)

// SQL in the command (psql -c "select ...", mysql -e "show tables"): the header is the query's
// columns, not the tool's fields
var reSQLInCommand = regexp.MustCompile(`(?i)\b(?:select|insert|update|delete|merge|show|describe|desc|with)\b`)

// commandAbove: the shell command (not SQL) up to 3 lines above the header lines[i]: a prompt line
// or, right above, a line that is a listing command ("airflow dags list").
func commandAbove(s string, lines []cell, i int) (string, bool) {
	for k := i - 1; k >= 0 && k >= i-3; k-- {
		l := strings.TrimSpace(s[lines[k].a:lines[k].b])
		cmd := ""
		if m := rePromptShell.FindStringSubmatch(l); m != nil {
			cmd = m[1] + m[2]
		} else if k == i-1 && reCommandLine.MatchString(l) && listingHint(l) != nil {
			cmd = l
		}
		if cmd != "" {
			return cmd, !reSQLInCommand.MatchString(cmd)
		}
	}
	return "", false
}

// classifyTable: the type of each column by the header and the values of the object columns. title:
// the type the line above the header gives (for the "name" column). noHdr: do not mark the header
// cells as columns (shape still barely confirmed).
//
// Outside CSV/TSV, the header only becomes a column with "_" or a digit (cd_cliente, col1):
// PascalCase tool labels ("CreationDate", "LastModified") are not user columns.
func classifyTable(s string, cs []cell, rows [][]cell, title string, noHdr, strict bool, add func(ObjFinding)) {
	classifyTableD(s, cs, rows, title, noHdr, strict, nil, add)
}

// classifyTableD: with the command hint (d != nil), only the columns untyped by the header that the
// hint types (SELECT the list, SHOW and listings the NAME column); the rest already came out
// without the hint.
func classifyTableD(s string, cs []cell, rows [][]cell, title string, noHdr, strict bool, d *outputHint, add func(ObjFinding)) {
	emitIt := add
	if d != nil {
		add = func(ObjFinding) {}
	}
	ent := map[int]string{}
	hasSchema, name, typ := false, -1, -1
	for k, c := range cs {
		h := s[c.a:c.b]
		v := strings.ToLower(h)
		switch v {
		case "name", "nome":
			name = k
			continue
		case "type", "data_type", "datatype", "tipo", "dtype":
			typ = k
			continue
		}
		if e, ok := entHeader(h); ok {
			ent[k] = e
			hasSchema = hasSchema || e == "schema"
			continue
		}
		if m := reTablesIn.FindStringSubmatchIndex(h); m != nil { // MySQL: Tables_in_<banco>
			ent[k] = "table"
			add(ObjFinding{c.a + m[2], c.a + m[3], "database", "tabela-catálogo", true})
			continue
		}
		if !noHdr && !outputLabels[v] && !publicSQL(v) && !strings.ContainsAny(h, " ().") && identifierShape(h) && (!strict || strings.ContainsAny(h, "_0123456789")) {
			add(ObjFinding{c.a, c.b, "column", "cabeçalho", false})
		}
	}
	if name >= 0 {
		switch {
		case hasSchema: // psql \dt: Schema | Name | Type | Owner
			ent[name] = "table"
		case title != "": // aws --output table: the section title gives the type
			ent[name] = title
		case typ >= 0: // DESCRIBE: name | type, with data types in the type column
			n, t := 0, 0
			for _, r := range rows {
				if typ < len(r) && r[typ].a >= 0 {
					n++
					if isDataType(s[r[typ].a:r[typ].b]) {
						t++
					}
				}
			}
			if n > 0 && t*2 >= n {
				ent[name] = "column"
			}
		}
	}
	if d != nil {
		byHint(s, cs, rows, ent, d, emitIt)
		return
	}
	if len(ent) == 0 {
		return
	}
	for _, r := range rows {
		for col, e := range ent {
			if col >= len(r) || r[col].a < 0 {
				continue
			}
			c := r[col]
			v := s[c.a:c.b]
			if len(v) < 2 || !reIdentCell.MatchString(v) || strings.EqualFold(v, "null") || strings.EqualFold(v, "none") ||
				isDataType(v) || strings.Trim(v, ".-") == "" {
				continue
			}
			addCellParts(s, c.a, c.b, e, add)
		}
	}
}

// byHint: the values of the columns the header did not type (ent) and the command hint types.
func byHint(s string, cs []cell, rows [][]cell, ent map[int]string, d *outputHint, add func(ObjFinding)) {
	types := map[int]string{}
	for k, c := range cs {
		if _, already := ent[k]; already {
			continue
		}
		if e := d.typ(s[c.a:c.b], k, len(cs)); e != "" {
			types[k] = e
		}
	}
	for _, r := range rows {
		for col, e := range types {
			if col < len(r) {
				hintCell(s, r[col], e, d.strong, add)
			}
		}
	}
}

// looseColumn: a single column, with the header being a type word ("bucket", "table_name") and 2+
// lines of one item each shaped like a name, up to the empty line. Returns the line where it
// stopped (i if it is not a loose column).
func looseColumn(s string, lines []cell, i int, add func(ObjFinding)) int {
	h := strings.TrimSpace(s[lines[i].a:lines[i].b])
	if h == "" || strings.ContainsAny(h, " \t,;|") {
		return i
	}
	e, ok := entHeader(h)
	if !ok {
		return i
	}
	k := i + 1
	var vs []cell
	for ; k < len(lines); k++ {
		l := lines[k]
		a, b := l.a, l.b
		for a < b && (s[a] == ' ' || s[a] == '\t') {
			a++
		}
		for b > a && (s[b-1] == ' ' || s[b-1] == '\t' || s[b-1] == '\r') {
			b--
		}
		if a == b {
			break
		}
		v := s[a:b]
		if !reIdentCell.MatchString(v) || !identifierShape(v) {
			return i
		}
		vs = append(vs, cell{a, b})
	}
	if len(vs) < 2 {
		return i
	}
	for _, c := range vs {
		addCellParts(s, c.a, c.b, e, add)
	}
	return k
}

// labelAndValues: TAB-separated line whose first cell is an uppercase type word
// ("BUCKETS\t2024-01-02\tbkt-x", text output of cloud CLIs): the other cells shaped like a name are
// of that type.
func labelAndValues(s string, l cell, add func(ObjFinding)) bool {
	line := s[l.a:l.b]
	t := strings.IndexByte(line, '\t')
	if t < 3 || strings.ToUpper(line[:t]) != line[:t] {
		return false
	}
	e, ok := entHeader(line[:t])
	if !ok {
		return false
	}
	for _, c := range objCells(s, l.a+t+1, l.b, '\t', nil) {
		if v := s[c.a:c.b]; v != "" && reSimpleIdent.MatchString(v) && resourceValue(v, e) && !isDataType(v) {
			add(ObjFinding{c.a, c.b, e, "tabela-rótulo", false})
		}
	}
	return true
}

// verticalRecord: "-[ RECORD 1 ]-" followed by "key | value" (psql \x), up to the empty line.
// Returns the line where it stopped.
func verticalRecord(s string, lines []cell, i int, add func(ObjFinding)) int {
	for ; i < len(lines); i++ {
		l := lines[i]
		line := s[l.a:l.b]
		if strings.TrimSpace(line) == "" {
			return i
		}
		if strings.HasPrefix(strings.TrimSpace(line), "-[ RECORD ") {
			continue
		}
		cs := objCells(s, l.a, l.b, '|', nil)
		if len(cs) != 2 || cs[0].a == cs[0].b {
			continue
		}
		if e, ok := entHeader(s[cs[0].a:cs[0].b]); ok {
			if v := s[cs[1].a:cs[1].b]; v != "" && reIdentCell.MatchString(v) && !isDataType(v) {
				addCellParts(s, cs[1].a, cs[1].b, e, add)
			}
		}
	}
	return i
}

// addCellParts: value of a catalog column, which may be qualified (database.schema.table).
func addCellParts(s string, a, b int, lastPos string, add func(ObjFinding)) {
	ps := strings.Split(s[a:b], ".")
	ents := entQual(len(ps), lastPos)
	for k, p := range ps {
		if p != "" && !publicSQL(p) && reSimpleIdent.MatchString(p) {
			// a short value (up to 4 characters: codes, acronyms) masks in place, but does not
			// teach alone
			add(ObjFinding{a, a + len(p), ents[k], "tabela-catálogo", ents[k] != "column" && len(p) >= 5})
		}
		a += len(p) + 1
	}
}

// --------------------------------------------------------------------------------------- Lines
// that are tuples or lists: ('a', 'b') or ['a', 'b'] per line, list of tuples, list of lists
// (cursor.fetchall(), csv.reader, spreadsheets read line by line). The first tuple is the header
// when all its cells are identifiers and some give the type.

// tuplesIn: the tuples/lists of literals in s, with the positions of the items.
func tuplesIn(s string) [][]cell {
	var out [][]cell
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '(' && c != '[' {
			continue
		}
		closes := byte(')')
		if c == '[' {
			closes = ']'
		}
		var items []cell
		j := i + 1
		ok := false
		for j < len(s) {
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			if j >= len(s) {
				break
			}
			if s[j] == '\'' || s[j] == '"' {
				q := s[j]
				k := j + 1
				for k < len(s) && s[k] != q && s[k] != '\n' {
					if s[k] == '\\' {
						k++
					}
					k++
				}
				if k >= len(s) || s[k] != q {
					break
				}
				items = append(items, cell{j + 1, k})
				j = k + 1
			} else {
				k := j
				for k < len(s) && (isAlnum(s[k]) || s[k] == '.' || s[k] == '-' || s[k] == '_') {
					k++
				}
				if v := s[j:k]; k == j || !(allDigits(strings.Trim(v, ".-")) || v == "None" || v == "NULL" || v == "null" || v == "True" || v == "False" || v == "NA") {
					break
				}
				items = append(items, cell{-1, -1}) // number, None: not a name
				j = k
			}
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			if j < len(s) && s[j] == ',' {
				j++
				continue
			}
			if j < len(s) && s[j] == closes {
				ok = true
			}
			break
		}
		if ok && len(items) >= 2 {
			out = append(out, items)
			i = j
		}
	}
	return out
}

func findTuples(s string, d *outputHint, add func(ObjFinding)) {
	if !strings.Contains(s, "', '") && !strings.Contains(s, `", "`) && !strings.Contains(s, "','") && !strings.Contains(s, `","`) {
		return
	}
	ts := tuplesIn(s)
	for i := 0; i < len(ts); {
		j := i + 1
		for j < len(ts) && len(ts[j]) == len(ts[i]) {
			j++
		}
		group := ts[i:j]
		i = j
		hdr := group[0]
		hasType, everyone := false, true
		for _, c := range hdr {
			if c.a < 0 || !reIdentCell.MatchString(s[c.a:c.b]) {
				everyone = false
				break
			}
			if _, ok := entHeader(s[c.a:c.b]); ok || d != nil && d.names[strings.ToLower(s[c.a:c.b])] != "" {
				hasType = true
			}
		}
		if d != nil && !hasType && len(d.cols) == len(hdr) { // cursor.fetchall(): no header
			for _, t := range group {
				for k, c := range t {
					if e := d.cols[k]; e != "" && c.a >= 0 {
						hintCell(s, c, e, d.strong, add)
					}
				}
			}
			continue
		}
		if !everyone || !hasType || len(group) < 2 {
			continue
		}
		classifyTableD(s, hdr, group[1:], "", false, false, d, add)
	}
}

// ---------------------------------------------------------------------------------------
// Table HTML: <tr><th>...</th></tr> e <tr><td>...</td></tr>.

var (
	reHTMLLine = regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr>`)
	reHTMLCell = regexp.MustCompile(`(?is)<t([hd])\b[^>]*>\s*(.*?)\s*</t[hd]>`)
)

func findHTMLTable(s string, d *outputHint, add func(ObjFinding)) {
	if !strings.Contains(s, "<tr") && !strings.Contains(s, "<TR") {
		return
	}
	var hdr []cell
	var rows [][]cell
	closeIt := func() {
		if len(hdr) >= 1 && len(rows) > 0 {
			classifyTableD(s, hdr, rows, "", false, true, d, add)
		}
		hdr, rows = nil, nil
	}
	lastOne := -1
	for _, m := range reHTMLLine.FindAllStringSubmatchIndex(s, -1) {
		if lastOne >= 0 && strings.Contains(strings.ToLower(s[lastOne:m[0]]), "<table") {
			closeIt()
		}
		lastOne = m[1]
		var cs []cell
		th := false
		for _, c := range reHTMLCell.FindAllStringSubmatchIndex(s[m[2]:m[3]], -1) {
			th = th || s[m[2]+c[2]] == 'h' || s[m[2]+c[2]] == 'H'
			cs = append(cs, cell{m[2] + c[4], m[2] + c[5]})
		}
		if th || hdr == nil {
			closeIt()
			hdr = cs
			continue
		}
		rows = append(rows, cs)
	}
	closeIt()
}
