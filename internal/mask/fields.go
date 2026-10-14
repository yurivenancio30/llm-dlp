package mask

import (
	"regexp"
	"strings"
	"unicode"
)

// Detection by field name, part 2: "field: value", JSON, SQL INSERT and XML.

var notValue = map[string]bool{"": true, "null": true, "none": true, "nil": true, "nan": true, "n/a": true, "na": true, "-": true, "--": true,
	"true": true, "false": true, "undefined": true, "string": true, "varchar": true, "text": true, "number": true, "integer": true, "date": true}

// service accounts and generic names: masking "user=root" would only get in the way

var commonUser = map[string]bool{"root": true, "admin": true, "administrator": true, "postgres": true, "mysql": true, "sa": true, "app": true,
	"user": true, "usuario": true, "test": true, "teste": true, "ubuntu": true, "ec2-user": true, "airflow": true, "datahub": true, "system": true,
	"sys": true, "sysadmin": true, "guest": true, "nobody": true, "www-data": true, "default": true, "anonymous": true, "public": true, "git": true,
	"docker": true, "oracle": true, "hadoop": true, "spark": true, "hive": true, "kafka": true, "jenkins": true, "claude": true, "assistant": true}

var reReadyPseudo = regexp.MustCompile(`^(?:[A-Z:]+-[a-z2-7]{8}|(?:[Pp]erson|[Pp]essoa) [a-z2-7]{8}|(?:PERSON|PESSOA) [A-Z2-7]{8}|\S+\.invalid)$`)

// markIt applies fieldValue to the piece s[start:end] and records the finding. In a name field,
// "Fulano de Tal / Beltrana Souza" are two people: each part is a finding.
func markIt(s string, ini, end int, class string, add func(ini, end int, typ string)) {
	if class == "name_loose" {
		return // only counts in a table or record with another person field, where it becomes "name_full" (see header and nameInRegistry)
	}
	if (class == "name" || class == "name_full") && strings.ContainsAny(s[ini:end], "/;&") {
		for p := ini; p < end; {
			q := p
			for q < end && s[q] != '/' && s[q] != ';' && s[q] != '&' {
				q++
			}
			a, b := p, q
			for a < b && s[a] == ' ' {
				a++
			}
			for b > a && s[b-1] == ' ' {
				b--
			}
			if b > a {
				if typ, ok := fieldValue(class, s[a:b]); ok {
					add(a, b, typ)
				}
			}
			p = q + 1
		}
		return
	}
	if typ, ok := fieldValue(class, s[ini:end]); ok {
		add(ini, end, typ)
	}
}

// fieldValue decides whether v, in a field of the given class, must be masked, and with which type.
func fieldValue(class, v string) (string, bool) {
	if notValue[strings.ToLower(v)] || len(v) > 120 || reReadyPseudo.MatchString(v) || isPseudoObj(v) || isPseudoLabel(v) {
		return "", false
	}
	dig, let := 0, 0
	for _, c := range v {
		switch {
		case unicode.IsDigit(c):
			dig++
		case unicode.IsLetter(c):
			let++
		}
	}
	switch class {
	case "card", "account":
		return class, dig >= 4 && let <= 4
	case "quasi":
		return class, let+dig >= 1 && len(v) <= 60
	case "cpf", "cnpj", "rg", "cnh", "pis", "phone", "cep", "doc":
		// a date or time is not a document, however many digits it has ("2026-10-03", "12:30:00"):
		// without this, a date in one of these columns would be remembered and masked everywhere
		if looksLikeDateTime(v) {
			return "", false
		}
		if class == "doc" && dig >= 3 && dig+let >= 6 {
			return "doc", true
		}
		// number of digits of each document (with slack for cut leading zeros)
		rng := map[string][2]int{"cpf": {9, 11}, "cnpj": {12, 14}, "rg": {5, 14}, "cnh": {9, 11}, "pis": {9, 11},
			"phone": {8, 13}, "cep": {7, 8}, "doc": {5, 20}}[class]
		if class == "cnpj" && let > 0 { // new CNPJ, with letters
			return class, dig+let == 14 && dig >= 2
		}
		return class, dig >= rng[0] && dig <= rng[1] && let <= 3
	case "birthdate":
		return class, dig >= 6 && dig <= 14 && let <= 12
	case "address":
		return class, let >= 4 && len(v) >= 6
	case "user":
		// environment variables, query parameters (":user_id") and pieces of code do not count; nor
		// do ownerless values ("stub-user", "my_user", "XXXXXXXXX", see vocab_dev.go)
		return class, dig+let >= 2 && !commonUser[strings.ToLower(v)] && !strings.ContainsAny(v, " ()[]{}$<>:%") &&
			!ownerless(v, nil)
	case "sensitive":
		return class, let+dig >= 1
	case "name_full":
		// first and last name, each starting with a capital ("Maria Lopes", "MARIA LOPES")
		ps := strings.Fields(v)
		if len(ps) < 2 {
			return "", false
		}
		for _, p := range []string{ps[0], ps[len(ps)-1]} {
			if r := []rune(p)[0]; !unicode.IsUpper(r) {
				return "", false
			}
		}
		return fieldValue("name", v)
	case "name":
		// a person's name: letters, spaces and little else (not "SVC_CARGA", "a.b:c", "x@y")
		for _, c := range v {
			if !unicode.IsLetter(c) && c != ' ' && c != '\'' && c != '-' && c != '.' {
				return "", false
			}
		}
		return class, let >= 3
	}
	return "", false
}

// findFields looks for "field: value", "field=value", "field": "value" (JSON) with a sensitive
// field. It does not use regex: it only walks through the ":" and "=" of the text, looks at the
// word before it (the label) and, if it is a sensitive field, takes the value that comes after. In
// text without a sensitive field the cost is one lookup per separator.
func (m *Masker) findFields(s string, add func(ini, end int, typ string)) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !fieldSeparator(s, i) {
			continue
		}
		next := byte(0)
		if i+1 < len(s) {
			next = s[i+1]
		}
		class := m.classBefore(s, i)
		if class == "name_loose" {
			class = m.nameInRegistry(s, i) // "nome" alone: a person's only with another person field next to it
		}
		if class == "" {
			continue
		}
		// valor
		j := i + 1
		if c == '=' && next == '>' {
			j++
		}
		for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
			j++
		}
		if j >= len(s) {
			break
		}
		// string literal prefix (r'...', b"...", f'...', rb'...', u'...'): it is not the value, the
		// value is what is between the quotes
		j = afterLiteralPrefix(s, j)
		ini, end := j, j
		if q := s[j]; q == '"' || q == '\'' || q == '`' {
			ini = j + 1
			end = ini
			for end < len(s) && s[end] != q && s[end] != '\n' && end-ini < 120 {
				end++
			}
		} else {
			// the backtick and the ">" close code and markup around the value ("`User ID=x`",
			// "<b>x</b>")
			for end < len(s) && end-ini < 120 && !strings.ContainsRune("\n\"'`,;|{}[]<>", rune(s[end])) {
				end++
			}
			if c == '=' {
				// "a=1 b=2" (log, .env): the value ends at the space. A person's name can have
				// spaces; then it goes up to the word that already belongs to another
				// "field=value".
				p := ini
				for p < end && s[p] != ' ' && s[p] != '\t' {
					p++
				}
				if strings.HasPrefix(class, "name") {
					for q := p; q < end; {
						for q < end && (s[q] == ' ' || s[q] == '\t') {
							q++
						}
						r := q
						for r < end && s[r] != ' ' && s[r] != '\t' {
							r++
						}
						if r == q || strings.ContainsAny(s[q:r], "=:") {
							break
						}
						p, q = r, r
					}
				}
				end = p
			}
		}
		end = trimValue(s, ini, end)
		if end > ini && (class == "quasi" || !shortLettersOnly(s[ini:end])) && !defaultOrModel(s[ini:end]) {
			markIt(s, ini, end, class, add)
		}
	}
}

// fieldSeparator: s[i] is the ":" or "=" of a "field: value" or "field=value". Not these: "::",
// "==", "!=", "<=", ">=", ":=" (the "=" counts), "://".
func fieldSeparator(s string, i int) bool {
	c := s[i]
	if c != ':' && c != '=' {
		return false
	}
	next, prev := byte(0), byte(0)
	if i+1 < len(s) {
		next = s[i+1]
	}
	if i > 0 {
		prev = s[i-1]
	}
	return !(c == ':' && (next == ':' || next == '=' || next == '/' || prev == ':')) &&
		!(c == '=' && (next == '=' || prev == '=' || prev == '!' || prev == '<' || prev == '>'))
}

// nameInRegistry: "nome" or "name" alone is a person's name, as in a table (see header), when the
// SAME record has another sensitive field ({"nome": x, "cpf": y}, <nome>x</nome><cpf>y</cpf>,
// nome=x cpf=y). The name of a product or a resource stays. The record ends at s[i] (the separator
// or the "</" of the field): the "{...}" around it; without braces, the neighboring lines with the
// same indentation (XML, YAML, .env) or the line itself (log).
func (m *Masker) nameInRegistry(s string, i int) string {
	a, b := registryIn(s, i)
	for k := a; k < b; k++ {
		var c string
		switch {
		case k == i:
			continue
		case s[k] == ':' || s[k] == '=':
			if fieldSeparator(s, k) {
				c = m.classBefore(s, k)
			}
		case s[k] == '<' && k+1 < b && s[k+1] == '/':
			if f := strings.IndexByte(s[k+2:b], '>'); f > 0 && f <= 60 {
				c = m.class(s[k+2 : k+2+f])
			}
		}
		if c != "" && c != "name_loose" {
			return "name_full"
		}
	}
	return ""
}

// registryIn: the record around s[i] (see nameInRegistry), with a size ceiling.
func registryIn(s string, i int) (int, int) {
	const ceiling = 8 << 10
	depth := 0
	for a := i - 1; a >= 0 && i-a <= ceiling; a-- {
		switch s[a] {
		case '}':
			depth++
		case '{':
			if depth > 0 {
				depth--
				continue
			}
			p := 0
			for b := a + 1; b < len(s) && b-a <= 2*ceiling; b++ {
				switch s[b] {
				case '{':
					p++
				case '}':
					if p == 0 {
						return a, b + 1
					}
					p--
				}
			}
			return a, min(len(s), a+2*ceiling)
		case '\n':
			if depth == 0 && a > 0 && s[a-1] == '\n' {
				a = -1 // a blank line before any "{": it is not between braces
			}
		}
		if a < 0 {
			break
		}
	}
	// without braces: the non-empty neighboring lines with the same indentation
	ini := strings.LastIndexByte(s[:i], '\n') + 1
	indent := func(l int) int {
		n := 0
		for l+n < len(s) && (s[l+n] == ' ' || s[l+n] == '\t') {
			n++
		}
		return n
	}
	r := indent(ini)
	a := ini
	for n := 0; n < 60 && a > 0; n++ {
		p := strings.LastIndexByte(s[:a-1], '\n') + 1
		if p == a-1 || indent(p) != r || p+r >= len(s) || s[p+r] == '\n' {
			break
		}
		a = p
	}
	b := lineEndIn(s, i)
	for n := 0; n < 60 && b < len(s); n++ {
		p := b + 1
		if p >= len(s) || s[p] == '\n' || indent(p) != r || p+r >= len(s) || s[p+r] == '\n' {
			break
		}
		b = lineEndIn(s, p)
	}
	return a, b
}

func lineEndIn(s string, i int) int {
	if j := strings.IndexByte(s[i:], '\n'); j >= 0 {
		return i + j
	}
	return len(s)
}

// afterLiteralPrefix: if s[j:] starts with a Python string literal prefix (r, b, f, u, rb, br, fr,
// rf, in any case) glued to a quote, returns the position of the quote.
func afterLiteralPrefix(s string, j int) int {
	for n := 1; n <= 2 && j+n < len(s); n++ {
		if q := s[j+n]; q != '"' && q != '\'' {
			continue
		}
		switch strings.ToLower(s[j : j+n]) {
		case "r", "b", "f", "u", "rb", "br", "fr", "rf":
			if j == 0 || !isIdent(s[j-1]) {
				return j + n
			}
		}
		return j
	}
	return j
}

// shortLettersOnly: 1 or 2 letters and nothing else ("r", "pt", "M"): in "field: value" this is an
// acronym, a prefix or a piece of code, not the data. The quasi-identifiers (sex "F", state "MG")
// are the exception: there the value really is short (and the detector is optional).
func shortLettersOnly(v string) bool {
	if len(v) > 2 {
		return false
	}
	for i := 0; i < len(v); i++ {
		if !(v[i] >= 'a' && v[i] <= 'z' || v[i] >= 'A' && v[i] <= 'Z') {
			return false
		}
	}
	return true
}

// defaultOrModel: the value is a regex pattern ("(?!x)\\b...", "\\d+") or a text template
// ("{valor}", "%s", "%(nome)s"): it is code, not the field data.
func defaultOrModel(v string) bool {
	if strings.Contains(v, "(?") || len(v) >= 2 && v[0] == '{' && v[len(v)-1] == '}' || v == "%s" || strings.HasPrefix(v, "%(") {
		return true
	}
	for i := 0; i+1 < len(v); i++ {
		if v[i] == '\\' && strings.IndexByte("dDwWsSbB", v[i+1]) >= 0 {
			return true
		}
	}
	return false
}

// trimValue removes from the end of the value what belongs to the sentence or the markup around it,
// and not to the value: space, period, and ")", "]", "}" or "*" without the matching opening inside
// the value ("(User ID=x)", "**User ID=x**"). Without this, the closing went into the pseudonym:
// the model did not see it, and a value with ")" was discarded whole (and went out in the clear).
func trimValue(s string, ini, end int) int {
	for end > ini {
		v := s[ini:end]
		switch s[end-1] {
		case ' ', '\t', '\r', '.', '*':
		case ')':
			if strings.Count(v, "(") >= strings.Count(v, ")") {
				return end
			}
		case ']':
			if strings.Count(v, "[") >= strings.Count(v, "]") {
				return end
			}
		case '}':
			if strings.Count(v, "{") >= strings.Count(v, "}") {
				return end
			}
		default:
			return end
		}
		end--
	}
	return end
}

// classBefore classifies the label that ends right before s[i] (the separator). It tries with the
// last word, then with the last two, then with the last three ("cpf", "cliente cpf", "nome da
// mae"), and keeps the first one that is a sensitive field.
func (m *Masker) classBefore(s string, i int) string {
	end := i
	for end > 0 && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	if end > 0 && (s[end-1] == '"' || s[end-1] == '\'' || s[end-1] == '`') {
		end--
	}
	letter := func(b byte) bool { return isAlnum(b) || b == '_' || b == '.' || b == '-' || b >= 0x80 }
	ini := end
	for words := 0; words < 3; words++ {
		p := ini
		if words > 0 {
			if p == 0 || s[p-1] != ' ' {
				break
			}
			p--
		}
		q := p
		for q > 0 && letter(s[q-1]) && end-q < 60 {
			q--
		}
		if q == p {
			break
		}
		ini = q
		if c := m.class(s[ini:end]); c != "" {
			return c
		}
	}
	return ""
}

// findInsert: INSERT INTO t (col1, col2) VALUES (v1, v2), (...). The columns give the label of each
// value.
func (m *Masker) findInsert(s, lower string, add func(ini, end int, typ string)) {
	if len(s) != len(lower) {
		return
	}
	for pos := 0; ; {
		i := strings.Index(lower[pos:], "insert into")
		if i < 0 {
			return
		}
		pos += i + 11
		a := strings.IndexByte(s[pos:], '(')
		v := strings.Index(lower[pos:], "values")
		if a < 0 || v < 0 || a > v || v > 4000 {
			continue
		}
		b := strings.IndexByte(s[pos+a:], ')')
		if b < 0 || pos+a+b > pos+v {
			continue
		}
		var classes []string
		sens := 0
		for _, col := range strings.Split(s[pos+a+1:pos+a+b], ",") {
			c := m.class(strings.Trim(col, " \t\n\r`\"[]"))
			if c == "name_loose" {
				c = "name_full"
			}
			if c != "" {
				sens++
			}
			classes = append(classes, c)
		}
		p := pos + v + 6
		if sens == 0 {
			pos = p
			continue
		}
		for { // each "(v1, v2, ...)"
			p = skipBlank(s, p, true)
			if p >= len(s) || s[p] != '(' {
				break
			}
			p++
			for col := 0; p < len(s); col++ {
				p = skipBlank(s, p, false)
				ini, end := p, p
				if p < len(s) && s[p] == '\'' {
					ini = p + 1
					end = ini
					for end < len(s) && !(s[end] == '\'' && (end+1 >= len(s) || s[end+1] != '\'')) {
						if s[end] == '\'' {
							end++ // doubled quote inside the text
						}
						end++
					}
					p = end + 1
				} else {
					for end < len(s) && s[end] != ',' && s[end] != ')' {
						end++
					}
					p = end
				}
				if col < len(classes) && classes[col] != "" && end > ini && end <= len(s) {
					markIt(s, ini, end, classes[col], add)
				}
				p = skipBlank(s, p, false)
				if p >= len(s) || s[p] != ',' {
					break
				}
				p++
			}
			if p < len(s) && s[p] == ')' {
				p++
			}
		}
		pos = min(p, len(s))
	}
}

// findXML: <field>value</field>, with a sensitive field.
func (m *Masker) findXML(s string, add func(ini, end int, typ string)) {
	for pos := 0; ; {
		i := strings.Index(s[pos:], "</")
		if i < 0 {
			return
		}
		closes := pos + i
		pos = closes + 2
		f := strings.IndexByte(s[pos:], '>')
		if f < 1 || f > 60 {
			continue
		}
		label := s[pos : pos+f]
		class := m.class(label)
		if class == "name_loose" {
			class = m.nameInRegistry(s, closes)
		}
		if class == "" {
			continue
		}
		// the value is what is between the ">" of the opening and the "</"
		a0 := max(0, closes-122)
		ab := strings.LastIndexByte(s[a0:closes], '>')
		if ab >= 0 {
			ab += a0
		}
		if ab < 0 || closes-ab-1 > 120 || closes-ab-1 < 1 || strings.ContainsAny(s[ab+1:closes], "<\n") {
			continue
		}
		markIt(s, ab+1, closes, class, add)
	}
}

// Column describes how a field name was understood (for the "llm-dlp columns" command).
type Column struct {
	Name, Class string
}

// DescribeFields finds the field names of a text (the header of a table, or the keys of "field:
// value"/JSON) and says how each one was classified.
func (m *Masker) DescribeFields(s string) []Column {
	var out []Column
	seen := map[string]bool{}
	by := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || len(name) > 60 {
			return
		}
		seen[name] = true
		out = append(out, Column{name, m.vocab.labelClass(name)})
	}
	// 1) first line that looks like a table header
	for _, line := range strings.SplitN(s, "\n", 30) {
		if isMarkdownSeparator(line) || strings.ContainsAny(line, "={}()") {
			continue
		}
		best, n := byte(0), 0
		for _, sep := range []byte{'\t', '|', ';', ','} {
			if c := strings.Count(line, string(sep)); c > n {
				best, n = sep, c
			}
		}
		if n == 0 {
			continue
		}
		cells := cellsOf(line, best)
		ok := len(cells) >= 2
		for _, c := range cells {
			a, b := trim(line, c[0], c[1])
			if b-a > 48 || strings.Contains(line[a:b], ":") {
				ok = false
			}
		}
		if ok {
			for _, c := range cells {
				a, b := trim(line, c[0], c[1])
				by(line[a:b])
			}
			return out
		}
	}
	// 2) "field: value" / JSON keys
	for i := 0; i < len(s) && len(out) < 400; i++ {
		if s[i] != ':' && s[i] != '=' {
			continue
		}
		end := i
		for end > 0 && (s[end-1] == ' ' || s[end-1] == '"' || s[end-1] == '\'') {
			end--
		}
		ini := end
		for ini > 0 && (isAlnum(s[ini-1]) || s[ini-1] == '_' || s[ini-1] == '.' || s[ini-1] == '-' || s[ini-1] >= 0x80) {
			ini--
		}
		by(s[ini:end])
	}
	return out
}

// looksLikeDateTime: "2026-10-03", "03/10/2026", "2026-10-03T12:30:00", "12:30:00".
var reDateTime = regexp.MustCompile(`^(?:\d{4}[-/.]\d{1,2}[-/.]\d{1,2}|\d{1,2}[-/.]\d{1,2}[-/.]\d{4})(?:[T ]\d{1,2}:\d{2}(?::\d{2}(?:[.,]\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?)?$|^\d{1,2}:\d{2}(?::\d{2})?$`)

func looksLikeDateTime(v string) bool { return reDateTime.MatchString(v) }

// skipBlank skips spaces, line breaks (and commas, if comma) and the line number Claude Code's read
// tool puts at the start of each line ("12<tab>").
func skipBlank(s string, p int, comma bool) int {
	for p < len(s) {
		switch c := s[p]; {
		case c == ' ' || c == '\t' || c == '\r' || (comma && c == ','):
			p++
		case c == '\n':
			p++
			p += lineNumber(s[p:min(len(s), p+16)])
		default:
			return p
		}
	}
	return p
}
