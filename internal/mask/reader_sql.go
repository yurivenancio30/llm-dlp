package mask

import (
	_ "embed"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SQL and DDL reader (see docs/structures.md, section SQL and DDL). A statement only counts if it
// has the minimal shape of the grammar (SELECT ... FROM x, INSERT INTO x, CREATE TABLE x...).
// Inside it, the name after FROM, JOIN, INTO, UPDATE, TABLE, VIEW, PROCEDURE, DATABASE, SCHEMA,
// INDEX... is an object (table, procedure, database, schema, index), with the parts of a qualified
// name server.database.schema.object; the other identifiers are columns.

//go:embed data/vocab_sql.txt
var vocabSQLTxt string

var vocabSQL = func() map[string]bool {
	m := map[string]bool{}
	for _, l := range strings.Split(vocabSQLTxt, "\n") {
		if strings.HasPrefix(l, "#") {
			continue
		}
		for _, w := range strings.Fields(l) {
			m[strings.ToLower(w)] = true
		}
	}
	return m
}()

func publicSQL(v string) bool { return vocabSQL[strings.ToLower(v)] }

// account objects (role, user, warehouse, integration) and schema objects (policies, stage, stream,
// task, pipe, sequence, file format, tag, alert) in DDL statements
const (
	reAccountTypes = `(?:DATABASE\s+)?ROLE|USER|WAREHOUSE|(?:STORAGE\s+|API\s+|NOTIFICATION\s+|SECURITY\s+)?INTEGRATION|RESOURCE\s+MONITOR`
	reSchemaTypes  = `(?:MASKING|ROW\s+ACCESS|NETWORK|PASSWORD|SESSION|AGGREGATION|PROJECTION|AUTHENTICATION)\s+POLICY|TAG|FILE\s+FORMAT|SECRET|ALERT|DYNAMIC\s+TABLE|NOTEBOOK`
)

const (
	reIdSQL   = "(?:\\[[^\\]\\n]{1,128}\\]|\"[^\"\\n]{1,128}\"|`[^`\\n]{1,128}`|[\\p{L}_][\\p{L}0-9_$#]*)"
	reSQLQual = reIdSQL + "(?:\\." + reIdSQL + ")*"
	// target of FROM/INTO/UPDATE: a name, or the place of a name in a code template (SQL built by
	// the program: f"... FROM {tbl}", "FROM ${tabela}", "FROM %s", dbt "{{ ref('x') }}"). The
	// statement is still SQL; its columns are read as always
	reSQLTarget = "(?:" + reSQLQual + "|\\{\\{?[^{}\\n]{1,80}\\}\\}?|\\$\\{[^}\\n]{1,60}\\}|%(?:\\(\\w+\\))?s)"
)

var (
	// minimal shape of each statement: the grammar requires these pieces, in this order
	reSQLShape = regexp.MustCompile(`(?is)^(?:SELECT\b[^;]*?\bFROM\s+` + reSQLTarget + `|WITH\s+(?:RECURSIVE\s+)?` + reIdSQL + `\s*(?:\([^)]{0,500}\)\s*)?AS\s*\(` +
		`|INSERT\s+(?:INTO\s+|OVERWRITE\s+(?:TABLE\s+)?)` + reSQLTarget + `|UPDATE\s+` + reSQLTarget + `(?:\s+(?:AS\s+)?\w+)?\s+SET\s+(?:\(|` + reSQLQual + `\s*=)|DELETE\s+FROM\s+` + reSQLTarget +
		`|MERGE\s+INTO\s+` + reSQLQual + `|(?:CREATE|ALTER|DROP)\s+(?:OR\s+(?:REPLACE|ALTER)\s+)?(?:(?:GLOBAL|LOCAL|SECURE|EXTERNAL|MATERIALIZED|TRANSIENT|TEMP(?:ORARY)?|UNIQUE|CLUSTERED|NONCLUSTERED)\s+)*` +
		`(?:TABLE|VIEW|PROCEDURE|PROC|FUNCTION|TRIGGER|SCHEMA|DATABASE|SEQUENCE|INDEX|STAGE|TASK|PIPE|STREAM|SYNONYM|PACKAGE(?:\s+BODY)?|` + reAccountTypes + `|` + reSchemaTypes + `)\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?` + reSQLQual +
		`|TRUNCATE\s+TABLE\s+` + reSQLQual + `|COPY\s+INTO\s+'?@?` + reSQLQual + `|(?:EXEC|EXECUTE|CALL)\s+` + reSQLQual + `|USE\s+(?:ROLE\s+|WAREHOUSE\s+|DATABASE\s+|SCHEMA\s+|SECONDARY\s+ROLES\s+)?` + reSQLQual + `\s*(?:;|$|\n)` +
		`|(?:GRANT|REVOKE)\s+(?:DATABASE\s+)?ROLE\s+` + reSQLQual + `\s+(?:TO|FROM)\s+` +
		`|(?:GRANT|REVOKE)\s[^;]{0,300}?\bON\s+(?:TABLE\s+|SCHEMA\s+|DATABASE\s+|VIEW\s+)?` + reSQLQual +
		`|(?:DESCRIBE|DESC)\s+(?:TABLE\s+)?` + reSQLQual + `\s*(?:;|$|\n)|SHOW\s+\w+(?:\s+\w+)?\s+(?:IN|FROM)\s+` + reSQLQual + `)`)
	reSQLClauses = regexp.MustCompile(`(?i)\b(FROM|WHERE|JOIN|GROUP\s+BY|ORDER\s+BY|SET|VALUES|INTO|HAVING|UNION|AS|ON|TABLE)\b`)
	// line that is no longer SQL (surrounding code): the statement stops there
	reNotSQL = regexp.MustCompile(`^\s*(?:def |class |func |return\b|if\s*\(|for\s*\(|import |from \S+ import|package |\}|\)\s*$|#|//|print\(|echo |cd |\$ ` +
		// Python: commands that do not exist in SQL and "if x:" / "while x:" at the end of the line
		`|(?:raise|assert|yield|await|elif|except|async)\b|(?:else|try|finally)\s*:|(?:if|while)\s[^\n]*:\s*(?:\n|$))`)
	// assignment to an object attribute at the start of the line (self.cur.x = ...,
	// mock.y.return_value = ...): surrounding code, unless inside a SET list (see setList)
	reAttrAssign = regexp.MustCompile(`^\s*(?:(?:self|this|cls|mock\w*|\w+_mock)\.[\w.]+\s*=[^=]|\w+(?:\.\w+){2,}\s*=[^=])`)
	reSQLEnd     = regexp.MustCompile("(?m);|\\n[ \\t]*\\n|```")
	reSQLComment = regexp.MustCompile(`(?s)--[^\n]*|/\*.*?\*/`)
	reSQLLit     = regexp.MustCompile(`(?s)'(?:[^']|'')*'|--[^\n]*|/\*.*?\*/`)
	// template placeholder in SQL built by code ({tbl}, ${tabela}, %(nome)s, {{ ref('x') }}): what
	// is inside belongs to the program (variable, function), not a database name
	reSQLModel = regexp.MustCompile(`\{\{[^{}\n]{1,80}\}\}|\$\{[^}\n]{1,60}\}|\{[^{}\n]{1,80}\}|%\(\w+\)s`)
	reQualTok  = regexp.MustCompile(reSQLQual)
	reSQLPart  = regexp.MustCompile(reIdSQL)
	// object positions: keyword -> entity of the last piece has: words (in uppercase) without which
	// the regex does not match; avoids running it for nothing
	reSQLObjPos = []struct {
		re  *regexp.Regexp
		ent string
		has []string
	}{
		{regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE|TABLE|VIEW|REFERENCES|USING|OVERWRITE)\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+|ONLY\s+)?(` + reSQLQual + `)`), "table", nil},
		{regexp.MustCompile(`(?i)\b(?:PROCEDURE|PROC|FUNCTION|TRIGGER|PACKAGE(?:\s+BODY)?|EXEC|EXECUTE|CALL)\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(` + reSQLQual + `)`), "procedure", []string{"PROC", "FUNCTION", "TRIGGER", "PACKAGE", "EXEC", "CALL"}},
		{regexp.MustCompile(`(?i)\b(?:USE|DATABASE)\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(` + reSQLQual + `)`), "database", []string{"USE", "DATABASE"}},
		{regexp.MustCompile(`(?i)\bSCHEMA\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(` + reSQLQual + `)`), "schema", []string{"SCHEMA"}},
		{regexp.MustCompile(`(?i)\bINDEX\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(` + reIdSQL + `)`), "index", []string{"INDEX"}},
		{regexp.MustCompile(`(?i)\bCONSTRAINT\s+(` + reIdSQL + `)`), "index", []string{"CONSTRAINT"}},
		{regexp.MustCompile(`(?i)\b(?:ROLE|USER)\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(` + reIdSQL + `)`), "user", []string{"ROLE", "USER"}},
		{regexp.MustCompile(`(?i)\bWAREHOUSE\s*(?:=\s*)?(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(` + reIdSQL + `)`), "service", []string{"WAREHOUSE"}},
		{regexp.MustCompile(`(?i)\b(?:` + reSchemaTypes + `|STAGE|STREAM|TASK|PIPE|SEQUENCE)\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(` + reSQLQual + `)`), "table",
			[]string{"POLICY", "TAG", "FORMAT", "SECRET", "ALERT", "DYNAMIC", "NOTEBOOK", "STAGE", "STREAM", "TASK", "PIPE", "SEQUENCE"}},
		{regexp.MustCompile(`(?i)(?:\bFROM|\bINTO|\bLIST|\bLS|\bREMOVE|\bRM|=)\s*'?@(` + reSQLQual + `)`), "table", []string{"@"}},
	}
	reOnObjSQL = regexp.MustCompile(`(?i)\bON\s+(` + reSQLQual + `)\s*(?:\(|TO\b|FROM\b|;|$)`)
	// DDL of any object type, by the grammar and not by a list of types: CREATE/ALTER/ DROP [OR
	// REPLACE] + 1 to 4 uppercase type words + name (CREATE NETWORK RULE x, CREATE SEMANTIC VIEW x,
	// CREATE CORTEX SEARCH SERVICE x). Dialects gain new types every year; in uppercase, the shape
	// is not confused with prose ("create a new table"). The known types (TABLE, INDEX, USER...)
	// still give the type of the name (reSQLObjPos).
	reDDLGenerico = regexp.MustCompile(`^(?:CREATE|ALTER|DROP)\s+(?:OR\s+(?:REPLACE|ALTER)\s+)?(?:[A-Z]+\s+){1,4}(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(` + reSQLQual + `)`)
	// CTE declaration: WITH [RECURSIVE] x [(columns)] AS ( and ), y AS (
	reCTE     = regexp.MustCompile(`(?i)(?:\bWITH\s+(?:RECURSIVE\s+)?|\)\s*,\s*)(` + reIdSQL + `)\s*(?:\([^)]{0,500}\)\s*)?AS\s*\(`)
	reSQLList = regexp.MustCompile(`^\s*(?:(?:AS\s+)?[A-Za-z_]\w*\s*)?,\s*(` + reSQLQual + `)`)
)

func hasAnyOf(s string, ps []string) bool {
	for _, p := range ps {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// entQual: the entities of each part of a qualified name a.b.c.d (last = that of the last one).
func entQual(n int, lastPos string) []string {
	above := []string{"schema", "database", "server"}
	if lastPos == "column" {
		above = []string{"table", "schema", "database"}
	} else if lastPos == "schema" {
		above = []string{"database", "server"}
	} else if lastPos == "database" {
		above = []string{"server"}
	}
	out := make([]string, n)
	out[n-1] = lastPos
	for i := n - 2; i >= 0; i-- {
		k := n - 2 - i
		if k >= len(above) {
			k = len(above) - 1
		}
		out[i] = above[k]
	}
	return out
}

// sqlParts: the parts of a qualified name in lit[a:b]. In BigQuery the whole name may be between
// backticks with the dots inside (`projeto.dataset.tabela`): each part counts.
func sqlParts(lit string, a, b int) [][2]int {
	var out [][2]int
	for _, p := range sqlPartsIn(lit[a:b]) {
		x, y := a+p[0], a+p[1]
		if lit[x] == '`' && strings.Contains(lit[x:y], ".") {
			k := x + 1
			for _, q := range strings.Split(lit[x+1:y-1], ".") {
				if q != "" {
					out = append(out, [2]int{k, k + len(q)})
				}
				k += len(q) + 1
			}
			continue
		}
		out = append(out, [2]int{x, y})
	}
	return out
}

// stripQuote: start and end of the name without the surrounding brackets/quotes/backticks.
func stripQuote(s string, a, b int) (int, int) {
	if b-a >= 2 {
		switch s[a] {
		case '[', '"', '`':
			return a + 1, b - 1
		}
	}
	return a, b
}

// statementEnd: where the statement starting at i ends.
func statementEnd(s string, i, j int) int {
	lim := min(len(s), i+8000)
	// SQL inside a code string, in any language: the statement does not go past the end of the
	// string (of the last one, in a concatenation)
	if a, ok := openingString(s, i); ok {
		lim = stringEnd(s, j, lim, a)
	}
	// SQL quoted in a line comment (# ..., // ..., Javadoc * ...): ends with the line; the next
	// line is already the surrounding code. Only looks 2000 bytes back (a long single line,
	// minified JSON, would be quadratic): a line comment is not that long
	w := max(0, i-2000)
	if w == 0 || strings.IndexByte(s[w:i], '\n') >= 0 {
		if with, _ := lineComment(s[w:], i-w); with {
			if e := strings.IndexByte(s[j:lim], '\n'); e >= 0 {
				lim = j + e
			}
		}
	}
	end := lim
	for k := j; k < lim; {
		loc := reSQLEnd.FindStringIndex(s[k:lim])
		if loc == nil {
			break
		}
		e := k + loc[0]
		// blank line in the middle of the statement (dbt style: CTEs and clauses separated by a
		// blank line): continues if there is an open parenthesis or if the next line continues the
		// SQL
		if s[e] == '\n' && continuesSQL(s, i, e, k+loc[1], lim) {
			k += loc[1]
			continue
		}
		end = e
		break
	}
	for o := strings.IndexByte(s[j:end], '\n'); o >= 0; {
		p := j + o + 1
		if l := s[p:min(end, p+120)]; reNotSQL.MatchString(l) || reAttrAssign.MatchString(l) && !setList(s, j, p) || looseLine(s, j, p, end) {
			return p - 1
		}
		k := strings.IndexByte(s[p:end], '\n')
		if k < 0 {
			break
		}
		o = p - j + k
	}
	return end
}

// English articles and determiners (in lowercase): never a name in a SQL statement
var proseArticles = setOf("the", "an", "this", "these", "those", "your", "our", "their", "its")

// reSQLContinues: line start that only exists in the middle of a statement: clause, closing
// parenthesis, list comma, SQL comment, Jinja (dbt) or the next CTE ("x as ("). "and", "on",
// "with", "case" are left out: they start a prose sentence.
var reSQLContinues = regexp.MustCompile(`(?i)^[ \t]*(?:(?:select|from|where|qualify|having|union|except|intersect)\b|(?:inner|left|right|full|cross)\s+(?:outer\s+)?join\b|join\b|(?:group|order)\s+by\b|\)|,|--|\{\{|\{%|` + reIdSQL + `\s+as\s*\()`)

// continuesSQL: after the blank line that ends at p (the statement started at i and the blank line
// at e), does the statement continue?
func continuesSQL(s string, i, e, p, lim int) bool {
	openOnes := 0
	quote := false
	for k := i; k < e; k++ {
		switch c := s[k]; {
		case c == '\'':
			quote = !quote
		case quote:
		case c == '(':
			openOnes++
		case c == ')':
			openOnes--
		}
	}
	for p < lim && (s[p] == '\n' || s[p] == '\r' || s[p] == ' ' || s[p] == '\t') {
		p++
	}
	if p >= lim {
		return false
	}
	return openOnes > 0 || reSQLContinues.MatchString(s[p:min(lim, p+120)])
}

// clauses that come right after a table alias (FROM anuncios an WHERE ...)
var afterAlias = setOf("where", "join", "inner", "left", "right", "full", "cross", "on", "group", "order",
	"having", "limit", "union", "set", "using", "natural", "qualify", "window", "fetch", "offset")

// sqlAlias: the article v, which ends at b, is a table alias and not prose: it is used as a
// qualifier in the statement ("an.cod") or is followed by a clause ("apolices an WHERE").
func sqlAlias(lit, v string, b int) bool {
	for k := strings.Index(lit, v+"."); k >= 0; {
		if k == 0 || !isIdent(lit[k-1]) {
			return true
		}
		n := strings.Index(lit[k+1:], v+".")
		if n < 0 {
			break
		}
		k += 1 + n
	}
	e := b + 1
	for e < len(lit) && letterD(lit[e]) {
		e++
	}
	return afterAlias[strings.ToLower(lit[b+1:e])]
}

// chainedCall: after the parenthesis that opens at j and the one that closes it comes "." (the
// select(User).where(...) of a query builder): a method call, not a statement.
func chainedCall(s string, j int) bool {
	n := 0
	for k := j; k < len(s) && k < j+400; k++ {
		switch s[k] {
		case '(':
			n++
		case ')':
			if n--; n == 0 {
				for k++; k < len(s) && (s[k] == ' ' || s[k] == '\t' || s[k] == '\r' || s[k] == '\n'); k++ {
				}
				return k < len(s) && s[k] == '.'
			}
		}
	}
	return true // no close nearby: it is not "SELECT(col) FROM x"
}

// setList: the line starting at p continues an SQL assignment list: the previous line ends in a
// comma or is the SET ("UPDATE t\nSET\n t.a = 1,\n t.b = 2").
func setList(s string, j, p int) bool {
	prev := strings.TrimRight(s[j:max(j, p-1)], " \t\r\n")
	return strings.HasSuffix(prev, ",") || len(prev) >= 3 && strings.EqualFold(prev[len(prev)-3:], "SET") &&
		(len(prev) == 3 || !isIdent(prev[len(prev)-4]))
}

// sqlStarts: the words that start a statement (in uppercase).
var sqlStarts = map[string]bool{"SELECT": true, "WITH": true, "INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true,
	"CREATE": true, "ALTER": true, "DROP": true, "TRUNCATE": true, "EXEC": true, "EXECUTE": true, "CALL": true, "USE": true,
	"GRANT": true, "REVOKE": true, "DESCRIBE": true, "SHOW": true, "COPY": true}

// startWords calls fn(i, j) for each word of s that starts an SQL statement. Walks word by word
// (much faster than a case-insensitive regex over the whole text).
func startWords(s string, fn func(i, j int)) {
	var buf [8]byte
	for i := 0; i < len(s); {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') || i > 0 && (isIdent(s[i-1]) || s[i-1] == '.') {
			i++
			continue
		}
		j := i
		for j < len(s) && (s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z') {
			j++
		}
		if n := j - i; n >= 3 && n <= 8 && (j == len(s) || !isIdent(s[j])) {
			for k := 0; k < n; k++ {
				buf[k] = s[i+k] &^ 0x20
			}
			if sqlStarts[string(buf[:n])] {
				fn(i, j)
			}
		}
		i = j
	}
}

func findSQL(s string, add func(ObjFinding)) {
	until := 0
	startWords(s, func(i, j int) {
		if i < until {
			return
		}
		// select(...).where(...), update(...), delete(...): a function call (SQLAlchemy, query
		// builders), not a statement. Only SELECT exists glued to the parenthesis in SQL
		// ("SELECT(col) FROM x"), and then no "." comes after the closing parenthesis
		if j < len(s) && s[j] == '(' && (!strings.EqualFold(s[i:j], "SELECT") || chainedCall(s, j)) {
			return
		}
		upperCase := strings.ToUpper(s[i:j]) == s[i:j]
		kw := strings.ToUpper(s[i:j])
		// common words in prose (use, copy, show, call, exec...): outside uppercase, only with an
		// unambiguous shape
		prose := !upperCase && (kw == "USE" || kw == "COPY" || kw == "SHOW" || kw == "DESCRIBE" || kw == "CALL" || kw == "EXEC" || kw == "EXECUTE" || kw == "GRANT" || kw == "REVOKE")
		end := statementEnd(s, i, j)
		body := s[i:end]
		// the shape is read without the comments ("UPDATE t /* x */ SET", "FROM t -- x\nWHERE")
		shape := body
		if strings.Contains(body, "--") || strings.Contains(body, "/*") {
			shape = reSQLComment.ReplaceAllStringFunc(body, func(x string) string { return strings.Repeat(" ", len(x)) })
		}
		// the sentence continues in prose after the keywords ("o SELECT pega os dados FROM da
		// tabela certa"): the statement ends where the prose begins
		if k := sqlProseStart(shape); k >= 0 {
			end, body, shape = i+k, body[:k], shape[:k]
		}
		if !reSQLShape.MatchString(shape) && !(upperCase && reDDLGenerico.MatchString(shape)) {
			return
		}
		if !upperCase && !statementOutsideProse(s, i, shape, kw, prose) {
			return
		}
		if upperCase && !statementStart(s, i) {
			return
		}
		claus := countClauses(shape, 4)
		// a single clause: in lowercase, only counts with an unambiguous shape; in uppercase it
		// always counts, and teaches when the shape is also unambiguous
		unambiguous := false
		if claus < 2 || prose {
			if unambiguous = unambiguousShape(shape, kw); !unambiguous && (!upperCase || prose) {
				return
			}
		}
		until = end
		strong := claus >= 2 || unambiguous
		sqlStatement(s, i, body, kw, strong, add)
	})
}

// sqlProseStart: where, in the body of a statement, a sequence of 3 or more loose words side by
// side starts (only space between them): lowercase words, without "_" or digit, that are not in the
// SQL vocabulary, are not qualified (a.b) nor a function call. In SQL grammar, at most two stand
// like that side by side (name and alias: "pedidos p", "a AS b"); three are prose. Returns -1 if
// there is none.
func sqlProseStart(body string) int {
	lit := reSQLLit.ReplaceAllStringFunc(body, func(x string) string { return strings.Repeat(" ", len(x)) })
	run, ini := 0, -1
	for _, p := range sqlPartsIn(lit) {
		a, b := p[0], p[1]
		v := lit[a:b]
		// English article or determiner followed by a word: does not exist in SQL grammar ("Select
		// the policy ... from scratch", "Create a DatabaseOperations for the ...")
		if proseArticles[v] && b+1 < len(lit) && lit[b] == ' ' && letterD(lit[b+1]) && !sqlAlias(lit, v, b) {
			return a
		}
		loose := strings.ToLower(v) == v && !publicSQL(v) && !identifierShape(v) && strings.IndexByte(v, '_') < 0 &&
			!strings.ContainsAny(v, "[\"`$#") && !(a > 1 && lit[a-1] == '.' && isIdent(lit[a-2])) &&
			(b == len(lit) || lit[b] != '(' && !(lit[b] == '.' && b+1 < len(lit) && (isIdent(lit[b+1]) || strings.IndexByte("[\"`*", lit[b+1]) >= 0)))
		if !loose {
			run = 0
			continue
		}
		// only space (no comma, operator or line break) since the previous word
		if run > 0 && !onlySpaceBetweenWords(lit[ini:a]) {
			run = 0
		}
		if run == 0 {
			ini = a
		}
		run++
		if run >= 3 {
			return ini
		}
	}
	return -1
}

// onlySpaceBetweenWords: the snippet has only letters, digits, "_" and space/tab (the words of the
// sequence and the spaces between them).
func onlySpaceBetweenWords(t string) bool {
	for _, r := range t {
		if !(r == ' ' || r == '\t' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

// Lowercase statement with a single clause ("select * from tb_pedido", "delete from tb_log where
// ..."): counts when the shape leaves no doubt, so as not to catch prose ("select from the list",
// "update the readme"). The SELECT list is "*" or comma-separated names (no loose words side by
// side), and after the table name comes the end, ";" or another clause; or the table name looks
// like an identifier.
var (
	reSelectMin = regexp.MustCompile(`(?is)^select\s+(?:distinct\s+|top\s+\d+\s+)?(\*|` + reSQLQual + `(?:\s*\([^)]{0,80}\))?(?:\s+as\s+\w+)?(?:\s*,\s*(?:\*|` + reSQLQual + `(?:\s*\([^)]{0,80}\))?(?:\s+as\s+\w+)?))*)\s+from\s+(` + reSQLQual + `)(.{0,12})`)
	reMinTarget = regexp.MustCompile(`(?is)^(?:insert\s+into|delete\s+from|update)\s+(` + reSQLQual + `)(.{0,12})`)
	// DDL, TRUNCATE, EXEC/CALL and USE outside uppercase ("create table t_x (", "exec sp_x @a = 1")
	reDDLMin = regexp.MustCompile(`(?is)^(?:(?:create|alter|drop)\s+(?:or\s+(?:replace|alter)\s+)?(?:(?:global|local|temp|temporary|unique|external|materialized|transient|secure|clustered|nonclustered)\s+)*` +
		`(?:table|view|procedure|proc|function|schema|database|index|sequence)|truncate\s+table|exec|execute|call|use|describe|desc)\s+(?:if\s+(?:not\s+)?exists\s+)?(` + reSQLQual + `)(.{0,12})`)
	reAfterDDL = regexp.MustCompile(`^\s*(?:\(|;|@|\n|$)|^\s+as\s+(?:select|\()`)
	reAfterMin = regexp.MustCompile(`(?i)^(?:\s*(?:;|$)|\s+(?:where|join|inner|left|right|full|cross|limit|order|group|having|union|values|set|select|as\s+\w+\s+(?:where|join)|\(|[a-z]\w{0,2}\s+(?:where|join|on)\b)|\s*\n)`)
)

func unambiguousShape(body, kw string) bool {
	var target, after string
	switch kw {
	case "SELECT":
		m := reSelectMin.FindStringSubmatch(body)
		if m == nil {
			return false
		}
		target, after = m[2], m[3]
		if m[1] == "*" || strings.Contains(m[1], ",") {
			return reAfterMin.MatchString(after) || identifierShape(target)
		}
	case "CREATE", "ALTER", "DROP", "TRUNCATE", "EXEC", "EXECUTE", "CALL", "USE", "DESCRIBE":
		// a name that looks like an identifier, or followed by what only SQL puts there: "(", ";",
		// "@" parameter, end of line. Prose ("drop table permissions") has neither.
		m := reDDLMin.FindStringSubmatch(body)
		if m == nil {
			return false
		}
		target := strings.Trim(m[1], "[]\"`")
		if kw == "USE" || kw == "DESCRIBE" || kw == "EXEC" || kw == "EXECUTE" || kw == "CALL" {
			return identifierShape(target) && reAfterDDL.MatchString(m[2])
		}
		return identifierShape(target) || strings.HasPrefix(strings.TrimSpace(m[2]), "(") || strings.HasPrefix(strings.TrimSpace(m[2]), ";")
	case "INSERT", "DELETE", "UPDATE":
		m := reMinTarget.FindStringSubmatch(body)
		if m == nil {
			return false
		}
		target, after = m[1], m[2]
	default:
		return false
	}
	return identifierShape(target) || reAfterMin.MatchString(after)
}

// sqlStatement classifies the identifiers of an already recognized statement.
func sqlStatement(s string, base int, body, kw string, strong bool, add func(ObjFinding)) {
	blank := func(x string) string { return strings.Repeat(" ", len(x)) }
	lit := reSQLLit.ReplaceAllStringFunc(body, blank)
	if strings.ContainsAny(lit, "{%") {
		lit = reSQLModel.ReplaceAllStringFunc(lit, blank)
	}
	ent := map[[2]int]string{} // parte -> entidade
	objects := map[string]bool{}
	// CTE names (WITH x AS (, ), y AS (): local names of the query, neither table nor column (like
	// a table alias, they do not exist in the database)
	ctes := map[string]bool{}
	if strings.Contains(strings.ToUpper(lit[:min(len(lit), 16)]), "WITH") {
		for _, m := range reCTE.FindAllStringSubmatch(lit, -1) {
			ctes[strings.ToLower(strings.Trim(m[1], "[]\"`"))] = true
		}
	}
	isCTE := func(a, b int) bool {
		return len(ctes) > 0 && ctes[strings.ToLower(strings.Trim(lit[a:b], "[]\"`"))]
	}
	markWhich := func(a, b int, lastPos string) {
		if isCTE(a, b) {
			return
		}
		var vs [][2]int
		for _, p := range sqlParts(lit, a, b) {
			if v := strings.Trim(lit[p[0]:p[1]], "[]\"`"); len(v) > 1 && !publicSQL(v) {
				vs = append(vs, p)
			}
		}
		if len(vs) == 0 {
			return
		}
		for k, e := range entQual(len(vs), lastPos) {
			ent[vs[k]] = e
		}
		objects[strings.ToLower(strings.Trim(lit[vs[len(vs)-1][0]:vs[len(vs)-1][1]], "[]\"`"))] = true
	}
	// object type outside the position rules: the DDL name is an object (of a schema, like a
	// table); the position rules below correct the type when they know it
	if kw == "CREATE" || kw == "ALTER" || kw == "DROP" {
		if m := reDDLGenerico.FindStringSubmatchIndex(lit); m != nil {
			if !reSQLShape.MatchString(lit) {
				// type the rules do not know (NETWORK RULE, AGENT...): the body is properties (TYPE
				// = IPV4, VALUE_LIST = (...)), not columns; only the object name is a name
				ps := sqlParts(lit, m[2], m[3])
				ents := entQual(len(ps), "table") // banco.schema.objeto
				for k, p := range ps {
					if v := strings.Trim(lit[p[0]:p[1]], "[]\"`"); len(v) > 1 && !publicSQL(v) {
						ia, ib := stripQuote(lit, p[0], p[1])
						add(ObjFinding{base + ia, base + ib, ents[k], "sql", strong})
					}
				}
				return
			}
			markWhich(m[2], m[3], "table")
		}
	}
	up := strings.ToUpper(lit)
	for _, p := range reSQLObjPos {
		if p.has != nil && !hasAnyOf(up, p.has) {
			continue
		}
		for _, m := range p.re.FindAllStringSubmatchIndex(lit, -1) {
			markWhich(m[2], m[3], p.ent)
			if p.ent == "table" { // FROM a, b, c
				for k := m[3]; ; {
					l := reSQLList.FindStringSubmatchIndex(lit[k:])
					if l == nil {
						break
					}
					markWhich(k+l[2], k+l[3], "table")
					k += l[3]
				}
			}
		}
	}
	if kw == "CREATE" || kw == "GRANT" || kw == "REVOKE" || kw == "ALTER" {
		for _, m := range reOnObjSQL.FindAllStringSubmatchIndex(lit, -1) {
			markWhich(m[2], m[3], "table")
		}
	}
	for _, q := range sqlQualsIn(lit) {
		ps := sqlParts(lit, q[0], q[1])
		for k, p := range ps {
			a, b := p[0], p[1]
			if b <= a || isCTE(a, b) {
				continue
			}
			v := lit[a:b]
			e, hasPos := ent[[2]int{a, b}]
			if !hasPos {
				if publicSQL(strings.Trim(v, "[]\"`")) || v[0] == '@' || (a > 0 && (lit[a-1] == ':' || lit[a-1] == '@' || lit[a-1] == '$')) {
					continue
				}
				if v[0] != '[' && v[0] != '"' && v[0] != '`' && len(ps) == 1 && b < len(lit) && lit[b] == '(' {
					continue // function call
				}
				e = "column"
				if k < len(ps)-1 { // qualifier of a column: table (or short alias, which stays)
					if len(v) <= 3 && !objects[strings.ToLower(v)] {
						continue
					}
					e = "table"
				} else if len(ps) == 1 && len(v) <= 2 {
					continue // alias curto (p, t1)
				}
			}
			ia, ib := stripQuote(lit, a, b)
			if ib <= ia {
				continue
			}
			add(ObjFinding{base + ia, base + ib, e, "sql", strong && hasPos && e != "column"})
		}
	}
}

// Error message that cites an object: type word followed by the name in quotes or brackets
// ("relation \"x\" does not exist", "Invalid object name 'x'", "Table 'db.x' doesn't exist",
// "Object 'A.B.C' does not exist"), or "Table/Dataset projeto:dataset(.x)". Generic rule: applies
// to any database, without knowing the text of each message.
var (
	reBQError = regexp.MustCompile(`(?i)\b(table|dataset)\s+([A-Za-z][\w-]{2,62}):([A-Za-z_][\w$]*(?:\.[A-Za-z_][\w$]*)?)\b`)
	entError  = map[string]string{"table": "table", "relation": "table", "object": "table", "view": "table", "column": "column",
		"schema": "schema", "database": "database", "procedure": "procedure", "function": "procedure", "index": "index",
		"sequence": "table", "collection": "table", "dataset": "schema"}
)

func findObjectError(s string, add func(ObjFinding)) {
	letter := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
	var buf [10]byte
	wordBefore := func(k int) (int, string) { // the word that ends at s[k] (inclusive)
		a := k
		for a > 0 && letter(s[a-1]) && k-a < 10 {
			a--
		}
		if a > 0 && letter(s[a-1]) || k-a+1 > 10 {
			return a, ""
		}
		n := k - a + 1
		for x := 0; x < n; x++ {
			buf[x] = s[a+x] | 0x20
		}
		return a, string(buf[:n])
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c != '"' && c != '\'' && c != '`' && c != '[' {
			continue
		}
		k := i - 1
		for k >= 0 && (s[k] == ' ' || s[k] == '\t') {
			k--
		}
		if k < 2 || k == i-1 || !letter(s[k]) { // needs a space between the word and the quote
			continue
		}
		a, w := wordBefore(k)
		if w == "name" { // "object name 'x'"
			k = a - 1
			for k >= 0 && s[k] == ' ' {
				k--
			}
			if k < 2 || !letter(s[k]) {
				continue
			}
			_, w = wordBefore(k)
		}
		e, ok := entError[w]
		if !ok {
			continue
		}
		closes := c
		if c == '[' {
			closes = ']'
		}
		b := i + 1
		for b < len(s) && b-i <= 200 && s[b] != closes && s[b] != ' ' && s[b] != '\n' && s[b] != '"' && s[b] != '\'' && s[b] != '`' {
			b++
		}
		if b >= len(s) || s[b] != closes || b == i+1 {
			continue
		}
		errorIn(s, i+1, b, e, add)
	}
	if strings.IndexByte(s, '.') >= 0 {
		findQualifiedType(s, add)
	}
	if strings.Contains(s, ":") && (strings.Contains(s, "able ") || strings.Contains(s, "ataset ") || strings.Contains(s, "ABLE ") || strings.Contains(s, "ATASET ")) {
		findBQError(s, add)
	}
}

// Portuguese type words (without accents: the text is read in ASCII) that also cite an object
var entTypePT = map[string]string{"tabela": "table", "objeto": "table", "esquema": "schema", "banco": "database",
	"coluna": "column", "indice": "index", "colecao": "table", "visao": "table"}

// findQualifiedType: qualified name WITHOUT quotes right after a type word, in any sentence
// ("tabela fin.t_x: 1200 linhas", "Loading table a.b.c", "created sql table model fin.t_x").
// Between the word and the name one lowercase word fits ("table model", "view model"). Without
// quotes, only a qualified name counts: "table x" alone is too much like prose. Strong evidence
// with the type word right next to the name; with a word in between, weak.
func findQualifiedType(s string, add func(ObjFinding)) {
	letter := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
	var buf [10]byte
	for i := 0; i < len(s); i++ {
		if !letter(s[i]) || i > 0 && (letter(s[i-1]) || s[i-1] == '_' || s[i-1] == '.' || s[i-1] >= '0' && s[i-1] <= '9') {
			continue
		}
		j := i
		for j < len(s) && letter(s[j]) && j-i < 10 {
			j++
		}
		if j-i < 4 || j < len(s) && (letter(s[j]) || s[j] != ' ' && s[j] != '\t') {
			i = j
			continue
		}
		n := j - i
		for x := 0; x < n; x++ {
			buf[x] = s[i+x] | 0x20
		}
		w := string(buf[:n])
		e, ok := entError[w]
		if !ok {
			e, ok = entTypePT[w]
		}
		if !ok {
			i = j - 1
			continue
		}
		p := j
		for p < len(s) && (s[p] == ' ' || s[p] == '\t') {
			p++
		}
		middle := false
		if q := p; q < len(s) && s[q] >= 'a' && s[q] <= 'z' { // one word in between ("table model")
			for q < len(s) && s[q] >= 'a' && s[q] <= 'z' {
				q++
			}
			if q-p >= 2 && q-p <= 12 && q < len(s) && (s[q] == ' ' || s[q] == '\t') {
				r := q
				for r < len(s) && (s[r] == ' ' || s[r] == '\t') {
					r++
				}
				if a, b := qualifiedIn(s, r); b > a {
					middle = true
					p = r
				}
			}
		}
		a, b := qualifiedIn(s, p)
		if b <= a {
			i = j - 1
			continue
		}
		ps := strings.Split(s[a:b], ".")
		if codeReceivers[strings.ToLower(ps[0])] || publicDev(ps[0]) {
			i = b
			continue
		}
		strong := !middle && e != "column"
		ents := entQual(len(ps), e)
		for k, v := range ps {
			if !publicSQL(v) {
				add(ObjFinding{a, a + len(v), ents[k], "qualified-type", strong && len(v) >= 3})
			}
			a += len(v) + 1
		}
		i = b
	}
}

// qualifiedIn: the qualified name (2 or 3 parts, no quotes) that starts at s[p], or a == b. A file
// (fin.csv), public domain, call (a.b(), path (a.b/c) or version does not count.
func qualifiedIn(s string, p int) (int, int) {
	ident := func(c byte) bool { return isAlnum(c) || c == '_' || c == '$' }
	if p >= len(s) || !(letterD(s[p]) || s[p] == '_') {
		return p, p
	}
	q, parts := p, 0
	for {
		k := q
		for k < len(s) && ident(s[k]) {
			k++
		}
		if k == q {
			return p, p
		}
		parts++
		q = k
		if q+1 < len(s) && s[q] == '.' && (letterD(s[q+1]) || s[q+1] == '_') && parts < 4 {
			q++
			continue
		}
		break
	}
	if parts < 2 || parts > 3 || q < len(s) && (ident(s[q]) || s[q] == '(' || s[q] == '/' || s[q] == '-' || s[q] == '.' && q+1 < len(s) && isAlnum(s[q+1])) {
		return p, p
	}
	lastPos := s[strings.LastIndexByte(s[p:q], '.')+p+1 : q]
	if strings.IndexByte(s[p:q], '.') < 2 || len(lastPos) < 3 { // "i.e.", "e.g."
		return p, p
	}
	if l := strings.ToLower(lastPos); fileExtensions[l] || publicTLDs[l] {
		return p, p
	}
	return p, q
}

// errorWords: the line looks like an error message (only then does the cited name teach)
var errorWords = []string{"error", "erro", "exist", "not found", "invalid", "unknown", "not authorized", "denied",
	"failed", "falhou", "inválid", "não encontrad", "msg ", "ora-", "sqlstate", "exception"}

func errorLine(s string, i int) bool {
	a, z := lineStartJ(s, i), lineEndJ(s, i)
	l := strings.ToLower(s[max(a, i-300):min(z, i+300)])
	for _, p := range errorWords {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}

func errorIn(s string, a, b int, e string, add func(ObjFinding)) {
	if publicSQL(s[a:b]) {
		return
	}
	strong := e != "column" && errorLine(s, a)
	ps := strings.Split(s[a:b], ".")
	ents := entQual(len(ps), e)
	for k, p := range ps {
		if p != "" && reSimpleIdent.MatchString(p) {
			add(ObjFinding{a, a + len(p), ents[k], "error", strong})
		}
		a += len(p) + 1
	}
}

func findBQError(s string, add func(ObjFinding)) {
	for _, m := range reBQError.FindAllStringSubmatchIndex(s, -1) {
		add(ObjFinding{m[4], m[5], "database", "error", true})
		a := m[6]
		ps := strings.Split(s[m[6]:m[7]], ".")
		ents := []string{"schema", "table"}
		for k, p := range ps {
			add(ObjFinding{a, a + len(p), ents[k], "error", true})
			a += len(p) + 1
		}
	}
}

// reSimpleIdent: an identifier (with accented letters: SQL and catalogs accept them).
var reSimpleIdent = regexp.MustCompile(`^[\p{L}_][\p{L}0-9_$#-]*$`)

// Regex-free scan of the SQL pieces (the reader runs on each statement; on a long line with
// thousands of short statements, the case-insensitive regexes took most of the time). They do the
// same as reSQLPart, reQualTok and reSQLClauses.

// sqlPartIn: the end of the SQL identifier (reIdSQL) that starts at s[i], or -1.
func sqlPartIn(s string, i int) int {
	switch c := s[i]; c {
	case '[', '"', '`':
		f := c
		if c == '[' {
			f = ']'
		}
		for k, n := i+1, 0; k < len(s) && n <= 128; n++ { // up to 128 characters (not bytes)
			if s[k] == '\n' {
				return -1
			}
			if s[k] == f {
				if k == i+1 {
					return -1
				}
				return k + 1
			}
			_, sz := utf8.DecodeRuneInString(s[k:])
			k += sz
		}
		return -1
	}
	k := i
	if c := s[i]; c < 0x80 {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_') {
			return -1
		}
		k++
	} else if r, n := utf8.DecodeRuneInString(s[i:]); unicode.IsLetter(r) {
		k += n
	} else {
		return -1
	}
	for k < len(s) {
		if c := s[k]; c < 0x80 {
			if isAlnum(c) || c == '_' || c == '$' || c == '#' {
				k++
				continue
			}
			break
		}
		r, n := utf8.DecodeRuneInString(s[k:])
		if !unicode.IsLetter(r) {
			break
		}
		k += n
	}
	return k
}

// sqlPartsIn: the positions of each identifier of s, like reSQLPart.FindAllStringIndex.
func sqlPartsIn(s string) [][2]int {
	var out [][2]int
	for i := 0; i < len(s); {
		if f := sqlPartIn(s, i); f > i {
			out = append(out, [2]int{i, f})
			i = f
			continue
		}
		_, n := utf8.DecodeRuneInString(s[i:])
		i += n
	}
	return out
}

// sqlQualsIn: the qualified names (a.b.c) of s, like reQualTok.FindAllStringIndex.
func sqlQualsIn(s string) [][2]int {
	var out [][2]int
	for i := 0; i < len(s); {
		f := sqlPartIn(s, i)
		if f <= i {
			_, n := utf8.DecodeRuneInString(s[i:])
			i += n
			continue
		}
		for f+1 < len(s) && s[f] == '.' {
			g := sqlPartIn(s, f+1)
			if g <= f+1 {
				break
			}
			f = g
		}
		out = append(out, [2]int{i, f})
		i = f
	}
	return out
}

var sqlClauses = setOf("FROM", "WHERE", "JOIN", "SET", "VALUES", "INTO", "HAVING", "UNION", "AS", "ON", "TABLE")

// countClauses: how many clauses (reSQLClauses) there are in s, up to max.
func countClauses(s string, max int) int {
	n := 0
	word := func(c byte) bool { return isAlnum(c) || c == '_' }
	var buf [6]byte
	for i := 0; i < len(s) && n < max; {
		if !word(s[i]) {
			i++
			continue
		}
		j := i
		for j < len(s) && word(s[j]) {
			j++
		}
		if l := j - i; l >= 2 && l <= 6 {
			for k := 0; k < l; k++ {
				buf[k] = s[i+k] &^ 0x20
			}
			w := string(buf[:l])
			if sqlClauses[w] {
				n++
			} else if w == "GROUP" || w == "ORDER" {
				k := j
				for k < len(s) && (s[k] == ' ' || s[k] == '\t' || s[k] == '\n' || s[k] == '\r' || s[k] == '\f' || s[k] == '\v') {
					k++
				}
				if k > j && k+2 <= len(s) && s[k]&^0x20 == 'B' && s[k+1]&^0x20 == 'Y' && (k+2 == len(s) || !word(s[k+2])) {
					n++
					j = k + 2
				}
			}
		}
		i = j
	}
	return n
}

// asksContinuation: words a SQL line ends with when the statement continues on the next line (the
// grammar requires a complement after them).
var asksContinuation = setOf("select", "from", "join", "on", "and", "or", "where", "by", "into", "update",
	"table", "view", "as", "set", "values", "using", "in", "not", "is", "like", "then", "when", "else",
	"case", "union", "all", "distinct", "having", "with", "over", "partition", "merge", "delete", "insert",
	"create", "replace", "alter", "drop", "exists", "between", "returning", "inner", "left", "right",
	"full", "outer", "cross", "natural", "lateral", "limit", "offset", "qualify", "window", "match", "matched")

// looseLine: the line starting at p does not continue the statement that started at j: the line
// before is complete (does not end in a comma, open parenthesis, operator or word that asks for a
// complement), this one is a loose word, with nothing of SQL, and the next one does not start with
// an SQL word either (a file name from a listing right after an SQL cut without ";": "linhagem.sql"
// is not table.column).
func looseLine(s string, j, p, end int) bool {
	e := strings.IndexByte(s[p:end], '\n')
	if e < 0 {
		e = end - p
	}
	l := strings.TrimSpace(s[p : p+e])
	if l == "" || strings.ContainsAny(l, " \t,()=<>+*/|;'\"") || vocabSQL[strings.ToLower(l)] {
		return false
	}
	a := strings.LastIndexByte(s[j:p-1], '\n')
	prev := strings.TrimSpace(s[j+a+1 : p-1])
	if prev == "" {
		return false
	}
	switch prev[len(prev)-1] {
	case ',', '(', '=', '<', '>', '+', '-', '*', '/', '|', '.':
		return false
	}
	ws := strings.Fields(prev)
	if asksContinuation[strings.ToLower(strings.Trim(ws[len(ws)-1], "(),"))] {
		return false
	}
	// the next line cannot continue the SQL either (in SQL with one word per line, the loose alias
	// is followed by JOIN, ON, WHERE...; in a listing, by another file name)
	q := p + e + 1
	for q < end && (s[q] == ' ' || s[q] == '\t' || s[q] == '\n' || s[q] == '\r') {
		q++
	}
	if q >= end {
		return true
	}
	next := strings.Fields(s[q:min(end, q+80)])
	return len(next) == 0 || !vocabSQL[strings.ToLower(strings.Trim(next[0], "(),;"))]
}

// SQL embedded in code. The string containing the statement is recognized by the shape of the
// delimiters, not by the language: single, double quotes and backticks ("...", '...', `...`),
// triple quotes (Python, Java, Kotlin, Scala, C#), raw string (Rust r#"..."#, C++ R"x(...)x"),
// heredoc (shell, Ruby, PHP: <<FIM, <<-FIM, <<~FIM, <<'FIM', <<<FIM) and concatenation ("a" + "b",
// "a" . "b", "a" & "b", "a" || "b", "a" "b"). Applies to a language not in this list that uses the
// same shapes.

// openingWindow: how much of the line before the statement openingString looks at.
const openingWindow = 2048

// openingStr: how the string in which the statement started closes.
type openingStr struct {
	closes  string // the closing delimiter (in a heredoc, the word on the final line)
	heredoc bool
}

var (
	reHeredoc = regexp.MustCompile(`<<<?[-~]?[ \t]*(['"]?)([A-Za-z_]\w*)['"]?`)
	reRustRaw = regexp.MustCompile(`\br(#+)"$`)
	reCppRaw  = regexp.MustCompile(`\bu?8?R"([^()\\\s"]{0,16})$`)
)

// openingString: is the statement starting at i inside a code string? Looks back over spaces, line
// breaks and "(" (cur.execute(\n """\n SELECT ...). A quote is only an opening if, on its line, the
// previous ones of the same kind are paired (x = "abc" on the line above closes a string, it does
// not open one).
func openingString(s string, i int) (openingStr, bool) {
	k := i
	for n := 0; k > 0 && n < 400; n++ {
		if c := s[k-1]; c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '(' {
			k--
			continue
		}
		break
	}
	// the opening line, up to openingWindow bytes back: on a long single line (minified JSON)
	// looking at the whole line for each statement would be quadratic
	j0 := max(0, k-openingWindow)
	ini := j0 + strings.LastIndexByte(s[j0:k], '\n') + 1
	cutOff := ini == j0 && j0 > 0 && s[j0-1] != '\n'
	prev := s[ini:k]
	prevEnd := prev[max(0, len(prev)-24):] // the raw openings fit at the end of the line
	// heredoc: the opening line ends before, and the statement starts on a following line
	if strings.Contains(s[k:i], "\n") {
		if ms := reHeredoc.FindAllStringSubmatch(prev, -1); ms != nil {
			return openingStr{closes: ms[len(ms)-1][2], heredoc: true}, true
		}
	}
	if m := reRustRaw.FindStringSubmatch(prevEnd); m != nil {
		return openingStr{closes: `"` + m[1]}, true
	}
	if m := reCppRaw.FindStringSubmatch(prevEnd); m != nil {
		return openingStr{closes: ")" + m[1] + `"`}, true
	}
	for _, q := range []string{`"""`, `'''`, `"`, `'`, "`"} {
		if strings.HasSuffix(prev, q) {
			// on the cut line the parity is not known: the quote right next to the statement opens
			if cutOff || countQuotes(prev[:len(prev)-len(q)], q)%2 == 0 {
				return openingStr{closes: q}, true
			}
			return openingStr{}, false
		}
	}
	return openingStr{}, false
}

// countQuotes: occurrences of q in s, without the escaped ones (\").
func countQuotes(s, q string) int {
	n := 0
	for p := 0; p+len(q) <= len(s); {
		switch {
		case s[p] == '\\':
			p += 2
		case s[p:p+len(q)] == q:
			n++
			p += len(q)
		default:
			p++
		}
	}
	return n
}

// stringEnd: where the open string (a) closes, starting from j; in a concatenation, the last one.
func stringEnd(s string, j, lim int, a openingStr) int {
	if a.heredoc {
		// the line that has only the word (with indentation; ";" after it, in PHP)
		for p := j; p < lim; {
			e := strings.IndexByte(s[p:lim], '\n')
			if e < 0 {
				e = lim - p
			}
			if strings.TrimRight(strings.TrimSpace(s[p:p+e]), ";") == a.closes && p > j {
				return p
			}
			p += e + 1
		}
		return lim
	}
	for p := j; p < lim; {
		k := closeIndex(s[p:lim], a.closes)
		if k < 0 {
			return lim
		}
		f := p + k
		if q, n := stringContinuation(s, f+len(a.closes), lim); n > 0 {
			a.closes, p = q, n
			continue
		}
		return f
	}
	return lim
}

// closeIndex: where the delimiter d is in s; single, double quotes and backticks escaped with \ do
// not close.
func closeIndex(s, d string) int {
	if len(d) > 1 {
		return strings.Index(s, d)
	}
	for p := 0; p < len(s); p++ {
		switch s[p] {
		case '\\':
			p++
		case d[0]:
			return p
		}
	}
	return -1
}

// stringContinuation: after a string closed at p, does another one that continues it come
// (concatenation operator, or just space, as in Python and C)? Returns the delimiter and where its
// content starts.
func stringContinuation(s string, p, lim int) (string, int) {
	skip := func(p int) int {
		for p < lim && (s[p] == ' ' || s[p] == '\t' || s[p] == '\n' || s[p] == '\r' || s[p] == '\\') {
			p++
		}
		return p
	}
	p = skip(p)
	for _, op := range []string{"||", "+", ".", "&", ".."} {
		if strings.HasPrefix(s[p:min(lim, p+len(op))], op) {
			p = skip(p + len(op))
			break
		}
	}
	for n := 0; n < 2 && p < lim && strings.IndexByte("fFrRbBuU@$", s[p]) >= 0; n++ {
		p++
	}
	for _, q := range []string{`"""`, `'''`, `"`, `'`, "`"} {
		if strings.HasPrefix(s[p:min(lim, p+len(q))], q) {
			return q, p + len(q)
		}
	}
	return "", 0
}
