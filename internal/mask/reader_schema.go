package mask

import (
	"regexp"
	"strings"
)

// Schema reader (see docs/structures.md, section Schemas): where a name comes with a data type
// beside it, the name is a column. No existing reader reads this shape (it is not configuration
// key-value, not SQL, not a table): "name int64" per line (dtypes), Index([...]) of columns, " |--
// name: string" (printSchema), "name: int64" per line at the start of the line (Arrow schema),
// "string name = 1;" (protobuf). The vocabulary is only that of data types (vocab_types.go). "name
// type" is decided by the TYPE, with or without indentation: the block is a schema when at least
// one type is data-only (object, category, datetime64[ns], VARCHAR(n), TIMESTAMP...), or when the
// pandas "dtype: object" footer comes. A type that is also a language type (string, int, bool,
// uint64, float64 — measured on public Go code, language_types.txt) does not decide alone: a Go
// struct writes "name type" the same way. A type with a code shape (*T, []T, package.Type, map[K]V)
// is never a schema one. Nested shapes in YAML/JSON (model columns, Avro "fields") are left to the
// YAML/JSON engine (findNameByContext).

var (
	reSchemeSpace = regexp.MustCompile(`^[ \t]*([\p{L}_][\p{L}\w$#.\-]*)[ \t]{2,}(\S+)[ \t]*$`)
	reSchemePrint = regexp.MustCompile(`^[ |]*\|-- ([\p{L}_][\p{L}\w$#.\-]*): (\S+)`)
	reSchemeColon = regexp.MustCompile(`^([\p{L}_][\p{L}\w$#.\-]*): (\S+(?: not null)?)[ \t]*$`)
	reSchemeProto = regexp.MustCompile(`^[ \t]*(?:(?:repeated|optional|required)[ \t]+)?([A-Za-z_][\w.]*)[ \t]+([A-Za-z_]\w*)[ \t]*=[ \t]*\d+[ \t]*(?:\[[^\]]*\])?[ \t]*;`)
	reIndexCols   = regexp.MustCompile(`Index\(\[((?:\s*'[^'\n]*',?)+)\s*\],\s*dtype='object'`)
)

func findSchema(s string, add func(ObjFinding)) {
	if strings.Contains(s, "Index([") {
		for _, m := range reIndexCols.FindAllStringSubmatchIndex(s, -1) {
			for i := m[2]; i < m[3]; i++ {
				if s[i] != '\'' {
					continue
				}
				j := strings.IndexByte(s[i+1:m[3]], '\'')
				if j < 0 {
					break
				}
				addColumn(s, i+1, i+1+j, add)
				i += j + 1
			}
		}
	}
	if !strings.Contains(s, "\n") {
		return
	}
	// per line: a block of consecutive lines of the same shape (at least 2, or 1 with the pandas
	// "dtype: object" footer)
	type pair struct{ a, b int }
	var block []pair
	shape := 0
	datum := false // "name    type": some type in the block is data-only
	closeIt := func(footer bool) {
		if (len(block) >= 2 || footer && len(block) == 1 || shape == 3 || shape == 4) && (shape != 1 || datum || footer) {
			for _, p := range block {
				addColumn(s, p.a, p.b, add)
			}
		}
		block, shape, datum = block[:0], 0, false
	}
	for _, l := range wrapLines(s) {
		line := s[l[0]:l[1]]
		if strings.HasPrefix(line, "dtype: ") { // pandas footer: closes the block above
			closeIt(true)
			continue
		}
		f, a, b := 0, -1, -1
		tl := false
		if m := reSchemeSpace.FindStringSubmatchIndex(line); m != nil && isDataType(line[m[4]:m[5]]) && !codeTypeShape(line[m[4]:m[5]]) {
			f, a, b = 1, m[2], m[3]
			tl = !dataOnlyType(line[m[4]:m[5]])
		} else if m := reSchemeColon.FindStringSubmatchIndex(line); m != nil && isDataType(strings.TrimSuffix(line[m[4]:m[5]], " not null")) {
			f, a, b = 2, m[2], m[3]
		} else if m := reSchemePrint.FindStringSubmatchIndex(line); m != nil && isDataType(line[m[4]:m[5]]) {
			f, a, b = 3, m[2], m[3]
		} else if m := reSchemeProto.FindStringSubmatchIndex(line); m != nil && isDataType(line[m[2]:m[3]]) {
			f, a, b = 4, m[4], m[5]
		}
		if f == 0 {
			closeIt(false)
			continue
		}
		if shape != 0 && f != shape {
			closeIt(false)
		}
		shape = f
		datum = datum || f == 1 && !tl
		block = append(block, pair{l[0] + a, l[0] + b})
	}
	closeIt(false)
}

// codeTypeShape: the type has a shape only code writes: pointer (*T), list ([]T), package-qualified
// type (strings.Builder), map (map[K]V), channel.
func codeTypeShape(t string) bool {
	return t != "" && (t[0] == '*' || t[0] == '[' || strings.Contains(t, ".") || strings.HasPrefix(t, "map[") || strings.HasPrefix(t, "chan"))
}

// dataOnlyType: the data type is not also a language type (languageTypes, measured). An uppercase
// type (INT, TIMESTAMP) is SQL: no language writes a type like that.
func dataOnlyType(t string) bool {
	if strings.ToUpper(t) == t && strings.ToLower(t) != t {
		return true
	}
	if i := strings.IndexAny(t, "([<"); i > 0 {
		t = t[:i]
	}
	return !languageTypes[strings.ToLower(t)]
}

// addColumn: column name (not propagated; counts in place).
func addColumn(s string, a, b int, add func(ObjFinding)) {
	v := s[a:b]
	if v == "" || isDataType(v) || publicSQL(v) || outputLabels[strings.ToLower(v)] || keyAttribute[strings.ToLower(v)] || !reSimpleIdent.MatchString(strings.ReplaceAll(v, ".", "_")) {
		return
	}
	add(ObjFinding{a, b, "column", "esquema", true})
}

// --------------------------------------------------------------------------------------- "name" by
// context, in YAML and JSON: the "name" of a list item or of an object counts by the container's
// name ("columns: - name: x" is a column; "tables:", "models:" is a table; Avro "fields" is a
// column; {"table": {"name": "x"}} is a table; "owner" is a user). The container gives the type
// through the type word (entKey), singular or plural.

// containers of names that are not a resource type word, but say what they hold
var containerName = map[string]string{"columns": "column", "column": "column", "fields": "column", "field": "column",
	"colunas": "column", "coluna": "column", "campos": "column", "models": "table", "model": "table",
	"sources": "schema", "seeds": "table", "snapshots": "table", "views": "table", "view": "table",
	"datasets": "schema", "tabelas": "table"}

func entContainer(k string) string {
	if e, ok := containerName[strings.ToLower(k)]; ok {
		return e
	}
	if e, strong := entKey(k); strong {
		return e
	}
	return ""
}

func findNameByContext(s string, add func(ObjFinding)) {
	if !strings.Contains(s, "name") {
		return
	}
	if !hasAny(s, []string{"columns", "fields", "tables", "models", "sources", "table", "dataset", "schema", "database", "_name",
		"seeds", "views", "owner", "colunas", "tabelas"}) {
		return
	}
	w := &yCollector{}
	w.readYAML(s)
	if strings.Contains(s, `"name"`) {
		w.readJSON(s)
	}
	siblings := map[int]map[string]bool{} // container -> keys
	for i := range w.ents {
		e := &w.ents[i]
		if siblings[e.parent()] == nil {
			siblings[e.parent()] = map[string]bool{}
		}
		siblings[e.parent()][strings.ToLower(e.key())] = true
	}
	seen := map[[2]int]bool{}
	for i := range w.ents {
		e := &w.ents[i]
		if e.vi < 0 || e.tmpl || len(e.path) < 1 {
			continue
		}
		k := e.key()
		ent := ""
		switch strings.ToLower(k) {
		case "name", "nome":
			if len(e.path) >= 2 {
				c := e.path[len(e.path)-2]
				if c == "-" && len(e.path) >= 3 {
					c = e.path[len(e.path)-3]
				}
				ent = entContainer(c)
			}
			// listing output (SHOW in JSON): the name beside the schema is a table; beside only the
			// database, it is a schema
			if ir := siblings[e.parent()]; ir["schema_name"] || ir["table_schema"] {
				ent = "table"
			} else if ent == "" && (ir["database_name"] || ir["table_catalog"]) {
				ent = "schema"
			}
		case "owner":
			ent = "user"
		}
		if ent == "" || seen[[2]int{e.vi, e.vf}] {
			continue
		}
		seen[[2]int{e.vi, e.vf}] = true
		if v := s[e.vi:e.vf]; ent == "column" {
			addColumn(s, e.vi, e.vf, add)
		} else if strings.Count(v, ".") > 0 && strings.Count(v, ".") <= 3 && reIdentCell.MatchString(v) {
			addCellParts(s, e.vi, e.vf, ent, add) // "db.schema.tabela": nome qualificado
		} else {
			addName(s, e.vi, e.vf, ent, "nome-contexto", true, add)
		}
	}
}
