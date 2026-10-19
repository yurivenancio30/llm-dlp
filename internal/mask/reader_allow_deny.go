package mask

import (
	"regexp"
	"strings"
)

// Filters of a DataHub ingestion recipe (AllowDenyPattern, in each source's page, e.g.
// https://docs.datahub.com/docs/generated/ingestion/sources/mssql): under a "<type>_pattern" key
// (database_pattern, schema_pattern, table_pattern...), the "allow" and "deny" lists hold regular
// expressions of names of that type. An anchored literal is the name: "^DWVENDAS$" is the database,
// "^DW\.dbo\..*" a database and its schema. A regex with alternatives or classes ("^(dev|test)_.*")
// is not a name and stays. In YAML, JSON and inside a command (escaped quotes and "\n").

// <type>_pattern -> the type of the last part of the literal
var patternTypes = map[string]string{"database": "database", "schema": "schema", "table": "table", "view": "table",
	"column": "column", "topic": "queue", "procedure": "procedure", "stored_procedure": "procedure", "dataset": "schema"}

var (
	rePatternKey = regexp.MustCompile(`\b([a-z]+(?:_[a-z]+)?)_pattern\\?["']?\s*[:=]`)
	// ^literal$ or ^literal\..* (the dot escaped with one or two backslashes), right after a quote or
	// "- ". "^stg.*" is the start of a name, not a name, and stays.
	rePatternLiteral = regexp.MustCompile(`(?:\\?["']|-[ \t]+)\^((?:[A-Za-z0-9_-]|\\{1,2}\.)+?)(\$|\\{1,2}\.\.\*)`)
	rePatternFiller  = regexp.MustCompile(`\\[nrt]|\\?"[^"\n]*?\\?"|'[^'\n]*'|-[ \t]+[^\s"'\\]\S*|[A-Za-z_]+`)
)

// words that may come between the key and an item of its lists
var patternWords = setOf("allow", "deny", "ignorecase", "true", "false")

func findAllowDeny(s string, add func(ObjFinding)) {
	keys := rePatternKey.FindAllStringSubmatchIndex(s, -1)
	if len(keys) == 0 {
		return
	}
	for _, m := range rePatternLiteral.FindAllStringSubmatchIndex(s, -1) {
		// the nearest key before the item, at most 3000 bytes away
		k := -1
		for i, km := range keys {
			if km[1] <= m[0] && m[0]-km[1] <= 3000 {
				k = i
			}
		}
		if k < 0 {
			continue
		}
		ent, ok := patternTypes[s[keys[k][2]:keys[k][3]]]
		if !ok || !patternListBetween(s[keys[k][1]:m[0]]) {
			continue
		}
		addPatternParts(s, m[2], m[3], ent, s[m[4]] != '$', add)
	}
}

// patternListBetween: between the key and the item there is only the structure of the lists (allow:,
// deny:, ignoreCase:, other quoted items, punctuation): no other key.
func patternListBetween(b string) bool {
	for _, w := range rePatternFiller.FindAllString(b, -1) {
		if c := w[0]; c == '\\' || c == '"' || c == '\'' || c == '-' { // escape, quoted or YAML item
			continue
		}
		if !patternWords[strings.ToLower(w)] {
			return false
		}
	}
	return true
}

// addPatternParts: the parts of the literal s[a:b], separated by an escaped dot ("DW\.dbo\.tb"),
// with the types of a qualified name ending in ent; a prefix ("DW\.dbo\..*" in table_pattern) ends
// one level above (database.schema).
func addPatternParts(s string, a, b int, ent string, prefix bool, add func(ObjFinding)) {
	type part struct{ a, b int }
	var ps []part
	x := a
	for i := a; i <= b; {
		if i == b || s[i] == '\\' {
			ps = append(ps, part{x, i})
			if i == b {
				break
			}
			for i < b && s[i] == '\\' {
				i++
			}
			i++ // the dot
			x = i
			continue
		}
		i++
	}
	ents := entQual(len(ps), ent)
	if prefix {
		ents = entQual(len(ps)+1, ent)[:len(ps)]
	}
	for k, p := range ps {
		if v := s[p.a:p.b]; v != "" && !publicConnection(v) && resourceValue(v, ents[k]) {
			add(ObjFinding{p.a, p.b, ents[k], "recipe-pattern", true})
		}
	}
}
