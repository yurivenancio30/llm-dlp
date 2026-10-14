package mask

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The command says what the output is (see docs/structures.md, section Loose script and shell
// outputs). The proxy sees the tool call (tool_use) and the result (tool_result) with the same id:
// the command types the columns of the result. No reader can do this, because a reader only sees
// its own text; the hint enters as evidence in the table reader that already exists
// (classifyTableD) and, for output without a header (psql -At, cut, uniq -c), in a per-line reading
// with the same number of cells on every line.
//
// Three forms, none per tool:
//   - SQL in the command: the SELECT list gives the type of each column, in order and by name
//     (alias or column); "name" counts by the FROM catalog (sys.tables -> table); SHOW <types>
//     gives the type of the name column.
//   - column extraction by position (cut -fN, awk '{print $N}') over a file whose header
//     already went through the conversation: column N has the type of that header.
//   - resource listing (<cli> get|list|ls <type>, <type> list, list-<type>, <cli> ps): the
//     requested type gives the type of the NAME column. It is weak evidence: it masks in place
//     and only teaches with a second rule.

// outputHint: what the command says about the output columns.
type outputHint struct {
	cols   []string          // type of each column, in order ("" = no type); empty = unknown position
	names  map[string]string // column name in the header (lowercase) -> type
	name   string            // type of the NAME/NAMES column without a type of its own
	strong bool              // unambiguous position (SQL, file header): teaches by itself
}

func (d *outputHint) empty() bool {
	if d == nil {
		return true
	}
	for _, c := range d.cols {
		if c != "" {
			return false
		}
	}
	return len(d.names) == 0 && d.name == ""
}

// encode: the hint as text (it goes with the text in the key of the result memory).
func (d *outputHint) encode() string {
	if d.empty() {
		return ""
	}
	var b strings.Builder
	if d.strong {
		b.WriteString("F")
	} else {
		b.WriteString("f")
	}
	b.WriteString("|" + strings.Join(d.cols, ",") + "|")
	ks := make([]string, 0, len(d.names))
	for k := range d.names {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for i, k := range ks {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k + ":" + d.names[k])
	}
	b.WriteString("|" + d.name)
	return b.String()
}

// sepExt separates, in the hint, the table reader's part (encode) from the extension the deciders
// read (TextCtx.Ext).
const sepExt = "\x1e"

// hintParts: the table reader's hint and the extension.
func hintParts(s string) (table, ext string) {
	t, e, _ := strings.Cut(s, sepExt)
	return t, e
}

// readHint: the inverse of encode (nil if empty or invalid). The extension (after sepExt) is
// ignored here.
func readHint(s string) *outputHint {
	s, _ = hintParts(s)
	ps := strings.Split(s, "|")
	if len(ps) != 4 {
		return nil
	}
	d := &outputHint{strong: ps[0] == "F", name: ps[3], names: map[string]string{}}
	if ps[1] != "" {
		d.cols = strings.Split(ps[1], ",")
	}
	if ps[2] != "" {
		for _, kv := range strings.Split(ps[2], ",") {
			if k, v, ok := strings.Cut(kv, ":"); ok {
				d.names[k] = v
			}
		}
	}
	if d.empty() {
		return nil
	}
	return d
}

// typ: the type of column k (of n) whose header is h.
func (d *outputHint) typ(h string, k, n int) string {
	l := strings.ToLower(h)
	if t, ok := d.names[l]; ok {
		return t
	}
	if n > 0 && len(d.cols) == n && d.cols[k] != "" {
		return d.cols[k]
	}
	if (l == "name" || l == "names" || l == "nome") && d.name != "" {
		return d.name
	}
	return ""
}

// Commands: the commands of a conversation, in order (the proxy creates one per request). It keeps
// the header of the files read, for column extraction by position.
type Commands struct {
	hdrs map[string][]string // path (and file name) -> header cells
	cv   conversationCmd     // provenance, echo and requested type (call.go)
}

func NewCommands() *Commands { return &Commands{hdrs: map[string][]string{}} }

// Hint: what the command says about the output (empty text: nothing). output is the command result:
// if it shows the start of a file with a header, the header is remembered for the following
// commands.
func (c *Commands) Hint(cmd, output string) string {
	if len(cmd) > 8<<10 {
		cmd = cmd[:8<<10]
	}
	d := c.hint(cmd)
	c.rememberHeader(cmd, output)
	return d.encode()
}

func (c *Commands) hint(cmd string) *outputHint {
	if d := sqlHint(cmd); d != nil {
		return d
	}
	if d := c.columnHint(cmd); d != nil {
		return d
	}
	return listingHint(cmd)
}

// ---- SQL in the command ------------------------------------------------------------------

var (
	reSelectFrom = regexp.MustCompile(`(?is)\bselect\s+(.+?)\s+from\s+((?:[\w$]+|"[^"\n]+"|\x60[^\x60\n]+\x60|\[[^\]\n]+\])(?:\.(?:[\w$]+|"[^"\n]+"|\x60[^\x60\n]+\x60|\[[^\]\n]+\]))*)`)
	reShow       = regexp.MustCompile(`(?i)\bshow\s+(?:terse\s+|full\s+)?([a-z]+)\b`)
	reDescribe   = regexp.MustCompile(`(?i)\b(?:describe|desc)\s+(?:table\s+|view\s+)?[\w$."\x60\[\]]+`)
	reItemAlias  = regexp.MustCompile(`(?is)^(.*?\S)\s+(?:as\s+)?([A-Za-z_][\w$]*|"[^"]+")$`)
	reSimpleCol  = regexp.MustCompile(`^(?:[\w$]+|"[^"]+"|\x60[^\x60]+\x60|\[[^\]]+\])(?:\.(?:[\w$]+|"[^"]+"|\x60[^\x60]+\x60|\[[^\]]+\]))*$`)
)

// sqlHint: the hint of an SQL statement in the command (the last SELECT, or SHOW/DESCRIBE).
func sqlHint(cmd string) *outputHint {
	if !hasAny(strings.ToLower(cmd), []string{"select", "show", "desc"}) {
		return nil
	}
	ms := reSelectFrom.FindAllStringSubmatchIndex(cmd, -1)
	if len(ms) > 0 {
		m := ms[len(ms)-1]
		if d := selectHint(cmd[m[2]:m[3]], cmd[m[4]:m[5]]); d != nil {
			return d
		}
	}
	if systemTarget(cmd) {
		return nil // describes or lists a system catalog: the listed structure is public
	}
	if m := reShow.FindStringSubmatch(cmd); m != nil {
		if e, ok := pluralEnt(m[1]); ok {
			return &outputHint{names: map[string]string{"name": e}, name: e, strong: true}
		}
	}
	if reDescribe.MatchString(cmd) {
		return &outputHint{names: map[string]string{"name": "column"}, name: "column", strong: true}
	}
	return nil
}

// pluralEnt: the entity of a type word in the plural or in the singular ("tables", "schemas").
func pluralEnt(w string) (string, bool) {
	l := strings.ToLower(w)
	if !pluralType[l] && entPiece[l] == "" {
		return "", false
	}
	e, strong := entKey(l)
	return e, strong && e != ""
}

// selectHint: SELECT list and the FROM object.
func selectHint(list, from string) *outputHint {
	items := splitList(list)
	if len(items) == 0 || len(items) > 60 {
		return nil
	}
	fps := strings.Split(from, ".")
	catalog := unquoted(fps[len(fps)-1])
	d := &outputHint{names: map[string]string{}, strong: true}
	positional := true
	for k, it := range items {
		it = strings.TrimSpace(it)
		if k == 0 { // DISTINCT, TOP n
			for _, p := range []*regexp.Regexp{reDistinct, reTop} {
				if m := p.FindStringIndex(it); m != nil {
					it = strings.TrimSpace(it[m[1]:])
				}
			}
		}
		if it == "*" || strings.HasSuffix(it, ".*") {
			positional = false
			continue
		}
		expr, alias := it, ""
		if !reSimpleCol.MatchString(it) {
			if m := reItemAlias.FindStringSubmatch(it); m != nil && !publicSQL(m[2]) {
				expr, alias = m[1], unquoted(m[2])
			}
		}
		base := ""
		if reSimpleCol.MatchString(expr) {
			ps := strings.Split(expr, ".")
			base = unquoted(ps[len(ps)-1])
		}
		e := ""
		if base != "" {
			if t, ok := entHeader(base); ok {
				e = t
			} else if l := strings.ToLower(base); l == "name" || l == "nome" {
				e, _ = pluralEnt(catalog)
			}
		}
		if e == "" && alias != "" {
			e, _ = entHeader(alias)
		}
		d.cols = append(d.cols, e)
		if e == "" {
			continue
		}
		for _, n := range []string{alias, base} {
			if n != "" && reSimpleIdent.MatchString(n) {
				d.names[strings.ToLower(n)] = e
			}
		}
	}
	if !positional {
		d.cols = nil
	}
	if d.empty() {
		return nil
	}
	return d
}

var (
	reDistinct = regexp.MustCompile(`(?i)^(?:distinct|all)\s+`)
	reTop      = regexp.MustCompile(`(?i)^top\s+\(?\d+\)?\s+`)
)

// splitList: the items of a comma-separated list, outside parentheses and quotes.
func splitList(s string) []string {
	var out []string
	depth, a := 0, 0
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '\'' || c == '"' || c == '`':
			q = c
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case c == ',' && depth == 0:
			out = append(out, s[a:i])
			a = i + 1
		}
	}
	return append(out, s[a:])
}

// ---- column extraction by position -------------------------------------------------------

var (
	reCut      = regexp.MustCompile(`\bcut\b(?:'[^'\n]*'|"[^"\n]*"|[^|;&\n'"])*`)
	reCutDelim = regexp.MustCompile(`(?:-d\s*|--delimiter=)(?:'([^']{1,4})'|"([^"]{1,4})"|(\S))`)
	reCutField = regexp.MustCompile(`(?:-f\s*|--fields=)([\d,\-]+)`)
	reAwk      = regexp.MustCompile(`\b[gnm]?awk\b(?:\s+-F\s*(?:'[^'\n]*'|"[^"\n]*"|\S+))?[^{|\n]*?\{\s*print\s+(\$\d+(?:\s*,\s*\$\d+){0,9})\s*;?\s*\}`)
	rePath     = regexp.MustCompile(`[\w~./\-]*[\w\-]\.[A-Za-z][\w]{0,5}\b`)
)

// columnHint: cut -fN / awk '{print $N}' over a file with a known header.
func (c *Commands) columnHint(cmd string) *outputHint {
	if len(c.hdrs) == 0 || !strings.Contains(cmd, "cut") && !strings.Contains(cmd, "awk") {
		return nil
	}
	hdr := c.quotedHeader(cmd)
	if hdr == nil {
		return nil
	}
	var fields []int
	if m := reCut.FindString(cmd); m != "" {
		if f := reCutField.FindStringSubmatch(m); f != nil {
			for _, p := range strings.Split(f[1], ",") {
				a, b, rng := strings.Cut(p, "-")
				i, err := strconv.Atoi(a)
				if err != nil || i < 1 {
					continue
				}
				j := i
				if rng {
					if j, err = strconv.Atoi(b); err != nil {
						j = len(hdr)
					}
				}
				for ; i <= j && i <= len(hdr) && len(fields) < 20; i++ {
					fields = append(fields, i)
				}
			}
		}
	} else if m := reAwk.FindStringSubmatch(cmd); m != nil {
		for _, p := range strings.Split(m[1], ",") {
			if i, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(p), "$")); err == nil && i >= 1 {
				fields = append(fields, i)
			}
		}
	}
	if len(fields) == 0 {
		return nil
	}
	d := &outputHint{names: map[string]string{}, strong: true}
	for _, i := range fields {
		e := ""
		if i <= len(hdr) {
			e, _ = entHeader(hdr[i-1])
			if e != "" {
				d.names[strings.ToLower(hdr[i-1])] = e
			}
		}
		d.cols = append(d.cols, e)
	}
	if d.empty() {
		return nil
	}
	return d
}

// quotedHeader: the header of a known file cited in the command.
func (c *Commands) quotedHeader(cmd string) []string {
	for _, p := range rePath.FindAllString(cmd, 20) {
		if hdr, ok := c.hdrs[p]; ok {
			return hdr
		}
		if hdr, ok := c.hdrs[fileNameOf(p)]; ok {
			return hdr
		}
	}
	return nil
}

func fileNameOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// rememberHeader: does the output start with the header of a file cited in the command ("a,b,c"
// with each cell an identifier)? It is remembered by the path and by the file name.
func (c *Commands) rememberHeader(cmd, output string) {
	ps := rePath.FindAllString(cmd, 4)
	if len(ps) == 0 || output == "" {
		return
	}
	if len(output) > 4096 {
		output = output[:4096]
	}
	if n, ok := normalize(output); ok {
		output = n.t
	}
	line := ""
	for _, l := range strings.Split(output, "\n") {
		if l = strings.TrimSpace(strings.TrimPrefix(l, "\ufeff")); l != "" {
			line = l
			break
		}
	}
	for _, sep := range []string{",", "\t", ";", "|"} {
		cs := strings.Split(line, sep)
		if len(cs) < 2 {
			continue
		}
		ok := true
		for i, v := range cs {
			v = strings.Trim(strings.TrimSpace(v), `"`)
			cs[i] = v
			if !reIdentCell.MatchString(v) {
				ok = false
				break
			}
		}
		if !ok {
			return
		}
		for _, p := range ps {
			c.hdrs[p] = cs
			c.hdrs[fileNameOf(p)] = cs
		}
		return
	}
}

// ---- resource listing --------------------------------------------------------------------

// container and orchestrator resource types (with the CLIs' own abbreviations) that the generic
// type word does not cover; the rest comes from pluralEnt (tables, topics, buckets...)
var listingResource = map[string]string{
	"pod": "service", "pods": "service", "po": "service", "deployment": "service", "deployments": "service",
	"deploy": "service", "statefulset": "service", "statefulsets": "service", "sts": "service",
	"daemonset": "service", "daemonsets": "service", "ds": "service", "replicaset": "service",
	"replicasets": "service", "rs": "service", "job": "service", "jobs": "service", "cronjob": "service",
	"cronjobs": "service", "cj": "service", "svc": "service", "services": "service", "service": "service",
	"release": "service", "releases": "service", "container": "service", "containers": "service",
	"ingress": "service", "ingresses": "service", "ing": "service", "configmap": "service",
	"configmaps": "service", "cm": "service", "secret": "service", "secrets": "service",
	"ns": "namespace", "namespaces": "namespace", "node": "server", "nodes": "server", "no": "server",
	"instances": "server", "clusters": "server", "vm": "server", "vms": "server",
	"apps": "service", "functions": "service", "groups": "service", "repos": "repository",
}

// listing verbs (as a subcommand, never the program itself: "ls" and "ps" alone belong to the
// shell)
var listingVerbs = setOf("get", "list", "ls", "ps")

// listingHint: <cli> get|list|ls <tipo>, <cli> <tipo> list|ls, <cli> list-<tipo>, <cli> ps.
func listingHint(cmd string) *outputHint {
	sgm := cmd
	if i := strings.IndexAny(sgm, "|;&\n"); i >= 0 {
		sgm = sgm[:i]
	}
	ts := strings.Fields(sgm)
	if len(ts) < 2 || len(ts) > 30 {
		return nil
	}
	typeOf := func(w string) string {
		w = strings.ToLower(strings.Trim(w, `"'`))
		if i := strings.IndexAny(w, "/,."); i > 0 {
			w = w[:i]
		}
		if e, ok := listingResource[w]; ok {
			return e
		}
		if e, ok := pluralEnt(w); ok && e != "column" {
			return e
		}
		return ""
	}
	// the listed type: the vocabulary one or, for any other plural noun ("dags", "projects",
	// "pipelines"), a named object (service)
	hintOf := func(w string) *outputHint {
		if e := typeOf(w); e != "" {
			return nameHint(e)
		}
		if pluralNoun(w) {
			return identityHint("service", w)
		}
		return nil
	}
	for i := 1; i < len(ts); i++ {
		v := strings.ToLower(ts[i])
		if strings.HasPrefix(v, "list-") {
			ps := strings.Split(v[5:], "-")
			return hintOf(ps[len(ps)-1])
		}
		if !listingVerbs[v] {
			continue
		}
		for j := i + 1; j < len(ts); j++ {
			if strings.HasPrefix(ts[j], "-") {
				continue
			}
			if d := hintOf(ts[j]); d != nil {
				return d
			}
			break
		}
		if i >= 2 {
			if d := hintOf(ts[i-1]); d != nil {
				return d
			}
		}
		if v == "ps" || v == "list" && i == 1 {
			return nameHint("service") // docker ps, helm list: containers and releases
		}
		return nil
	}
	return nil
}

func nameHint(e string) *outputHint {
	return &outputHint{names: map[string]string{"name": e, "names": e}, name: e}
}

// identityHint: the listing of a type outside the vocabulary types only the compound identity
// column, the singular type with _id or _name (dags -> dag_id, projects -> project_id, clusters ->
// cluster_name). NAME or the type alone does not: "UNIT" of systemctl list-units does not say the
// value is someone's name.
func identityHint(e, typ string) *outputHint {
	d := &outputHint{names: map[string]string{}}
	t := strings.ToLower(strings.Trim(typ, `"'`))
	switch {
	case strings.HasSuffix(t, "ies"):
		t = t[:len(t)-3] + "y"
	case strings.HasSuffix(t, "sses"), strings.HasSuffix(t, "xes"):
		t = t[:len(t)-2]
	case strings.HasSuffix(t, "s"):
		t = t[:len(t)-1]
	}
	if len(t) >= 2 {
		for _, suf := range []string{"_id", "_name", "id", "name", "-id", "-name"} {
			d.names[t+suf] = e
		}
	}
	return d
}

// pluralNoun: a lowercase word ending in "s" (not "ss"): the listed type.
func pluralNoun(w string) bool {
	w = strings.Trim(w, `"'`)
	if len(w) < 4 || w[len(w)-1] != 's' || w[len(w)-2] == 's' {
		return false
	}
	for i := 0; i < len(w); i++ {
		if c := w[i]; !(c >= 'a' && c <= 'z' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// ---- a dica aplicada -------------------------------------------------------------------

// publicHint: the vocabulary of the formats in which the hint appears.
func publicHint(v string) bool { return publicSQL(v) || publicDevops(v) }

// findWithHint: the columns typed by the hint, in the table with a header (the table reader, in
// hint mode) and in the output without a header (one line per record, the same number of cells).
func findWithHint(s string, d *outputHint, add func(ObjFinding)) {
	findTablesD(s, d, add)
	if len(d.cols) > 0 {
		hintLines(s, d, add)
	}
}

var (
	reCount  = regexp.MustCompile(`^\s*\d+\s+\S`)
	reFooter = regexp.MustCompile(`^\(\d+ (?:rows?|linhas?|registros?)(?: affected| afetad[oa]s?)?\)$`)
)

// hintLines: output without a header (psql -At, cut, awk, sort | uniq -c): every line has the same
// number of cells as the hint has columns. A line out of shape undoes everything.
func hintLines(s string, d *outputHint, add func(ObjFinding)) {
	n := len(d.cols)
	type lineV struct{ a, b int }
	var ls []lineV
	counting := true
	for _, l := range wrapLines(s) {
		a, b := l[0], l[1]
		for a < b && (s[a] == ' ' || s[a] == '\t') {
			a++
		}
		for b > a && (s[b-1] == ' ' || s[b-1] == '\t') {
			b--
		}
		if a == b || reFooter.MatchString(s[a:b]) {
			continue
		}
		ls = append(ls, lineV{a, b})
		counting = counting && reCount.MatchString(s[a:b])
		if len(ls) > 20000 {
			return
		}
	}
	if len(ls) == 0 {
		return
	}
	if counting && n == 1 { // sort | uniq -c: "   12 valor"
		for i, l := range ls {
			k := l.a
			for k < l.b && s[k] >= '0' && s[k] <= '9' {
				k++
			}
			for k < l.b && (s[k] == ' ' || s[k] == '\t') {
				k++
			}
			ls[i].a = k
		}
	}
	var sep byte
	if n > 1 {
		for _, c := range []byte{'|', '\t', ',', ';'} {
			ok := true
			for _, l := range ls {
				if countOutside(s[l.a:l.b], c) != n-1 {
					ok = false
					break
				}
			}
			if ok {
				sep = c
				break
			}
		}
	}
	var cells [][]cell
	for _, l := range ls {
		var cs []cell
		switch {
		case sep != 0:
			cs = objCells(s, l.a, l.b, sep, nil)
		default:
			cs = lineTokens(s, l.a, l.b)
		}
		if len(cs) != n {
			return
		}
		cells = append(cells, cs)
	}
	for _, cs := range cells {
		hdr := true // the header line (cut -f2 file.csv prints the header)
		for k, c := range cs {
			if d.typ(s[c.a:c.b], k, 0) == "" || d.names[strings.ToLower(s[c.a:c.b])] == "" {
				hdr = false
			}
		}
		if hdr {
			continue
		}
		for k, c := range cs {
			if e := d.cols[k]; e != "" {
				hintCell(s, c, e, d.strong, add)
			}
		}
	}
}

// hintCell: the value of a column typed by the hint (the brakes of the catalog cell).
func hintCell(s string, c cell, e string, strong bool, add func(ObjFinding)) {
	if c.a < 0 || c.b <= c.a {
		return
	}
	v := strings.Trim(s[c.a:c.b], `"'`)
	a := c.a + strings.Index(s[c.a:c.b], v)
	if v == "" || !reIdentCell.MatchString(v) || strings.EqualFold(v, "null") || strings.EqualFold(v, "none") ||
		isDataType(v) || strings.Trim(v, ".-") == "" || !strong && !identifierShape(v) {
		return
	}
	ps := strings.Split(v, ".")
	ents := entQual(len(ps), e)
	for k, p := range ps {
		if p != "" && !publicHint(p) && reSimpleIdent.MatchString(p) {
			add(ObjFinding{a, a + len(p), ents[k], "comando", strong && ents[k] != "column" && len(p) >= 5})
		}
		a += len(p) + 1
	}
}

// systemCatalogs: namespaces that are system ones in every installation (the names inside them are
// public documentation). public and dbo stay out: they hold the client's objects.
var systemCatalogs = setOf("information_schema", "pg_catalog", "pg_toast", "performance_schema", "sys",
	"sysibm", "syscat", "sysstat", "snowflake", "account_usage", "organization_usage", "data_sharing_usage",
	"readers_account_usage", "master", "msdb", "mysql")

var reDescShowTarget = regexp.MustCompile(`(?i)\b(?:describe|desc)\s+(?:table\s+|view\s+)?([\w$."]+)|\bshow\b[^;\n]*?\bin\s+(?:account\s+|database\s+|schema\s+)?([\w$."]+)`)

// systemTarget: the command describes (DESC) or lists (SHOW ... IN) an object that is in a system
// catalog (SNOWFLAKE.ACCOUNT_USAGE.TAG_REFERENCES, information_schema.columns). Only when ALL the
// statements of the command are that: a system DESC together with a SELECT (or a SHOW without IN, a
// DESC of a client table) in the same command brings client data in the output.
func systemTarget(cmd string) bool {
	sysName := false
	for _, inst := range strings.Split(cmd, ";") {
		ms := reDescShowTarget.FindAllStringSubmatchIndex(inst, -1)
		rest := inst
		for k := len(ms) - 1; k >= 0; k-- {
			m := ms[k]
			target := ""
			for _, g := range [][2]int{{m[2], m[3]}, {m[4], m[5]}} {
				if g[0] >= 0 {
					target += inst[g[0]:g[1]]
				}
			}
			if !systemObject(target) {
				return false
			}
			rest = rest[:m[0]] + rest[m[1]:]
		}
		if reSQLStatement.MatchString(rest) {
			return false // another statement in the same piece (SELECT, SHOW without IN...)
		}
		sysName = sysName || len(ms) > 0
	}
	return sysName
}

// systemObject: some part of the qualified name is a system catalog.
func systemObject(target string) bool {
	for _, p := range strings.Split(target, ".") {
		if systemCatalogs[strings.ToLower(strings.Trim(p, `"`))] {
			return true
		}
	}
	return false
}

// reSQLStatement: a word that starts a statement that reads or changes data.
var reSQLStatement = regexp.MustCompile(`(?i)\b(?:select|with|insert|update|delete|merge|copy|call|execute|show|desc|describe|list|get|put|values)\b`)
