package mask

import "strings"

// Code readers: the same for any language. They do not read the grammar of a language; they read
// the structure common to all of them: a quoted text next to a name. If the name contains a type
// word (host, db, table, queue, topic, bucket, namespace, user...) in any style (DB_HOST,
// queueName, queue_name, QueueName, topics, groupId), the quoted text is of that type. The name of
// the variable, function or class is never masked: only the quoted value.
//
// 9. the name next to it gives the type:
//	assignment and constant  DB_HOST = "x"   const dbHost = "x"   #define DB_HOST "x"
//	                         static final String DB_HOST = "x";   val/let/var x = "x"
//	named parameter          connect(host="x")   f(host: "x")   new X { Namespace = "x" }
//	dictionary key           {"host": "x"}   { host: 'x' }   'queue' => 'x'   config["Kafka:Topic"] = "x"
//	annotation/decorator     @Table(name = "x")   @KafkaListener(topics = "x")   [Table("x", Schema = "x")]
//	key and value in a call  setProperty("user", "x")   define('DB_HOST', 'x')   getenv("BUCKET", "x")
//	environment default      process.env.DB_HOST || 'x'   os.getenv("X") or "x"
//	wrapped value            Bucket: aws.String("x")
//
// 10. the called function gives the type: the quoted text passed to a call or constructor
// whose name contains the type word (assertQueue("x"), new QueueClient(c, "x"), getQueueUrl("x"),
// createBucket("x"), bucket("x"), Table("x"), inNamespace("x"), subscribe(["x"])).

// literal: a quoted text on the same line. Returns the end (closing quote) or -1.
func literal(s string, i int) int {
	q := s[i]
	lim := min(len(s), i+1+300)
	for j := i + 1; j < lim; j++ {
		switch s[j] {
		case '\\':
			j++
		case '\n':
			return -1
		case q:
			return j
		}
	}
	return -1
}

func isQuote(c byte) bool { return c == '"' || c == '\'' || c == '`' }

// beforeBlank: the position of the last character before i that is not a space (or -1).
func beforeBlank(s string, i int) int {
	k := i - 1
	for k >= 0 && (s[k] == ' ' || s[k] == '\t') {
		k--
	}
	return k
}

// identBefore: the identifier that ends at s[k] (inclusive): letters, digits, _ $ . Returns the
// start and the last piece after "." or "->" (self.table_name -> table_name).
func identBefore(s string, k int) (ini int, lastPos string) {
	a := k
	for a >= 0 && (isAlnum(s[a]) || s[a] == '_' || s[a] == '$' || s[a] == '.' || s[a] == '>' && a > 0 && s[a-1] == '-' || s[a] == '-' && a+1 <= k && s[a+1] == '>') {
		a--
	}
	name := s[a+1 : k+1]
	if i := strings.LastIndexAny(name, ".>"); i >= 0 {
		name = name[i+1:]
	}
	return a + 1, strings.TrimLeft(name, "$")
}

// nameBefore: the name to the left of an operator that ends at s[k]: an identifier, a quoted text
// ('queue' =>, "host":), an index (config["Kafka:Topic"]) or a Ruby symbol (:host =>). Skips the
// type annotation ("NAME: str =", "x: String =").
func nameBefore(s string, k int) string {
	k = beforeBlank(s, k+1)
	if k < 0 {
		return ""
	}
	switch {
	case isQuote(s[k]):
		q := s[k]
		a := strings.LastIndexByte(s[max(0, k-120):k], q)
		if a < 0 {
			return ""
		}
		a += max(0, k-120)
		if v := s[a+1 : k]; shapedKey(v) {
			return v
		}
		return ""
	case s[k] == ']':
		a := strings.LastIndexByte(s[max(0, k-120):k], '[')
		if a < 0 {
			return ""
		}
		a += max(0, k-120)
		v := strings.TrimSpace(s[a+1 : k])
		if len(v) >= 2 && isQuote(v[0]) && v[len(v)-1] == v[0] && shapedKey(v[1:len(v)-1]) {
			return v[1 : len(v)-1]
		}
		return ""
	case isAlnum(s[k]) || s[k] == '_':
		a, name := identBefore(s, k)
		// "NAME: str =": the type is to the right of the ":"; the name comes before
		if typeWords[strings.ToLower(name)] || commonType(name) {
			if p := beforeBlank(s, a); p >= 0 && s[p] == ':' {
				if _, n2 := identBefore(s, beforeBlank(s, p)); n2 != "" {
					return n2
				}
			}
		}
		return name
	}
	return ""
}

// markedValue: in code, only a value with a digit, "_" or "." teaches by itself (pgsrv-01,
// vendas_prd, db.interno); "payments-api" is masked in place and is only learned if it appears in
// two rules (two words with a hyphen appear too often in public code).
func markedValue(v string) bool { return strings.ContainsAny(v, "0123456789_.") }

// ambiguousInCode: the last piece of the name is a type word that, in code, almost always means
// something else: stream (input and output stream), subject (e-mail subject)
func ambiguousInCode(name string) bool {
	switch strings.ToLower(lastPiece(name)) {
	case "stream", "streams", "subject", "subjects":
		return true
	}
	return false
}

// wrapper: functions that only wrap the value (aws.String("x"), to.Ptr("x"), Optional.of("x"))
var wrapper = setOf("string", "str", "ptr", "toptr", "stringptr", "pointer", "some", "of", "value", "valueof",
	"stringvalue", "new", "text", "s")

// shapedKey: a quoted key that is a name ("host", "Kafka:Topic", "db.host"), and not a URL or a
// sentence
func shapedKey(v string) bool {
	if v == "" || len(v) > 80 || strings.Contains(v, "//") || !(letterD(v[0]) || v[0] == '_') {
		return false
	}
	for i := 0; i < len(v); i++ {
		if c := v[i]; !(isAlnum(c) || c == '_' || c == '.' || c == '-' || c == ':') {
			return false
		}
	}
	return true
}

// commonType: type names that appear between the name and the "=" ("String x =" stays with x, but
// "x: String =" needs to skip "String").
func commonType(v string) bool {
	switch v {
	case "String", "str", "string", "Str", "&str", "text", "Text", "char", "Optional", "Any", "any":
		return true
	}
	return false
}

func findCodeName(s string, add func(ObjFinding)) {
	if strings.IndexByte(s, '"') < 0 && strings.IndexByte(s, '\'') < 0 {
		return
	}
	for i := 0; i < len(s); i++ {
		// backtick: in prose (markdown) it marks code, not a value
		if s[i] != '"' && s[i] != '\'' {
			continue
		}
		j := literal(s, i)
		if j < 0 {
			continue
		}
		a, b := i+1, j
		i = j
		if b-a < 2 || b-a > 200 {
			continue
		}
		k := beforeBlank(s, a-1)
		// literal prefix: r"", f"", b"", u"", @"" (C#)
		if k >= 0 && strings.IndexByte("rRfFbBuU@", s[k]) >= 0 && k == a-2 && (k == 0 || !isAlnum(s[k-1])) {
			k = beforeBlank(s, k)
		}
		if k < 0 {
			continue
		}
		name := ""
		switch c := s[k]; {
		case c == '>' && k > 0 && s[k-1] == '=': // 'k' => 'v'
			name = nameBefore(s, k-2)
		case c == '=' && (k == 0 || strings.IndexByte("=!<>+-*/%&|^", s[k-1]) < 0) && (k+1 >= len(s) || s[k+1] != '='):
			p := k - 1
			if p >= 0 && (s[p] == ':' || s[p] == '?') { // := ?=
				p--
			}
			name = nameBefore(s, p)
		case c == ':' && (k == 0 || s[k-1] != ':') && (k+1 >= len(s) || s[k+1] != ':'):
			name = nameBefore(s, k-1)
			// "? 'x' : 'y'" (ternary operator) is not a key
			if p := beforeBlank(s, k); p >= 0 && isQuote(s[p]) {
				a := max(0, p-lineWindow)
				if o := strings.LastIndexByte(s[a:p], s[p]); o >= 0 {
					if q := beforeBlank(s, a+o); q >= 0 && s[q] == '?' {
						name = ""
					}
				}
			}
		case c == ',':
			// key and value in a call: f("KEY", "v")
			p := beforeBlank(s, k)
			if p >= 0 && isQuote(s[p]) {
				q := strings.LastIndexByte(s[max(0, p-120):p], s[p])
				if q >= 0 {
					q += max(0, p-120)
					if r := beforeBlank(s, q); r >= 0 && s[r] == '(' && keyValueCall(s, r) {
						name = s[q+1 : p]
					}
				}
			}
		case c == '|' && k > 0 && s[k-1] == '|', c == '?' && k > 0 && s[k-1] == '?':
			name = defaultName(s, k-2)
		case c == 'r' && k >= 1 && s[k-1] == 'o' && (k < 2 || !isAlnum(s[k-2])): // "... or 'v'"
			name = defaultName(s, k-2)
		case c == '(':
			// wrapped value: KEY: fn("v") / KEY = fn("v") with a single argument
			r := b + 1
			for r < len(s) && (s[r] == ' ' || s[r] == '\t') {
				r++
			}
			if r < len(s) && s[r] == ')' {
				ini, f := identBefore(s, beforeBlank(s, k))
				if p := beforeBlank(s, ini); wrapper[strings.ToLower(f)] && p >= 0 && (s[p] == ':' || s[p] == '=') && (p == 0 || s[p-1] != ':') {
					name = nameBefore(s, p-1)
				}
			}
		case isAlnum(c) || c == '_':
			// #define NOME "v"
			ini, n := identBefore(s, k)
			if p := beforeBlank(s, ini); p >= 6 && s[p-6:p+1] == "#define" {
				name = n
			}
		}
		if name == "" {
			continue
		}
		if ambiguousInCode(name) {
			continue
		}
		ent, strong := entKey(name)
		if ent == "" && (name == "name" || name == "value" || name == "names") {
			// @Table(name = "x"), Queue(name="x"): the type comes from who is called
			if f := enclosingCall(s, k); f != "" {
				ent, strong = entFunc(f), true
			}
		}
		if ent == "" {
			continue
		}
		markValue(s, a, b, ent, "código-nome", strong && markedValue(s[a:b]), add)
	}
}

// Placeholder default: ${NAME:value} (Spring, Micronaut), ${NAME:-value} and ${NAME-value} (shell,
// docker-compose). The variable name gives the type of the value, as in "NAME = value".
func findPlaceholder(s string, add func(ObjFinding)) {
	for i := strings.Index(s, "${"); i >= 0; {
		a := i + 2
		b := a
		for b < len(s) && (isAlnum(s[b]) || s[b] == '_' || s[b] == '.' || s[b] == '-' && b+1 < len(s) && s[b+1] != '}' && !(b > a && s[b-1] == ':')) {
			b++
		}
		if b > a && b < len(s) && (s[b] == ':' || s[b] == '-') {
			v := b + 1
			if s[b] == ':' && v < len(s) && (s[v] == '-' || s[v] == '=') {
				v++
			}
			end := strings.IndexByte(s[v:min(len(s), v+300)], '}')
			if end > 0 && !strings.Contains(s[v:v+end], "://") { // URL: stays with the URL reader
				if ent, strong := entKey(s[a:b]); ent != "" {
					markValue(s, v, v+end, ent, "placeholder", strong, add)
				}
			}
		}
		j := strings.Index(s[i+2:], "${")
		if j < 0 {
			break
		}
		i += 2 + j
	}
}

// defaultPlaceholder: s[k] is the ":" (or the "-" of ":-") that separates the name from the default
// in ${NAME:...}.
func defaultPlaceholder(s string, k int) bool {
	if k > 0 && s[k] == '-' && s[k-1] == ':' {
		k--
	}
	a := strings.LastIndex(s[max(0, k-120):k], "${")
	if a < 0 {
		return false
	}
	a += max(0, k-120) + 2
	for x := a; x < k; x++ {
		if !(isAlnum(s[x]) || s[x] == '_' || s[x] == '.' || s[x] == '-') {
			return false
		}
	}
	return k > a
}

// enclosingCall: the name of the function, constructor or annotation whose "(" is still open before
// s[k] (in the same statement, up to 300 bytes back).
func enclosingCall(s string, k int) string {
	depth := 0
	for i := k; i >= 0 && i > k-300; i-- {
		switch s[i] {
		case ')', ']', '}':
			depth++
		case '(', '[', '{':
			if depth == 0 {
				if s[i] != '(' && s[i] != '[' {
					return ""
				}
				p := beforeBlank(s, i)
				if s[i] == '[' { // C#'s [Table("x", Schema = "y")]: the name comes after the "["
					e := i + 1
					for e < len(s) && (isAlnum(s[e]) || s[e] == '_') {
						e++
					}
					return s[i+1 : e]
				}
				if p >= 0 && (isAlnum(s[p]) || s[p] == '_') {
					_, n := identBefore(s, p)
					return n
				}
				return ""
			}
			depth--
		case ';', '\n':
			if depth == 0 && s[i] == ';' {
				return ""
			}
		}
	}
	return ""
}

// words of the name of a function that receives (key, value): setProperty, define, getenv,
// os.environ.get, ENV.fetch, getOrDefault, Setenv, put, config.set...
var keyValueFunc = setOf("set", "put", "define", "getenv", "env", "environ", "get", "fetch", "default",
	"property", "properties", "setting", "settings", "config", "option", "options", "param", "attr", "setenv",
	"header", "add", "with", "or")

// keyValueCall: does the "(" at s[i] open the call of a (key, value) function?
func keyValueCall(s string, i int) bool {
	k := beforeBlank(s, i)
	if k < 0 || !(isAlnum(s[k]) || s[k] == '_') {
		return false
	}
	_, name := identBefore(s, k)
	var ps [16][2]int
	n, ok := keyPieces(name, &ps)
	if !ok {
		return false
	}
	var buf [24]byte
	for x := 0; x < n; x++ {
		if w := lowercase(name, ps[x], &buf); w != nil && keyValueFunc[string(w)] {
			return true
		}
	}
	return false
}

// defaultName: the name of the environment variable before "||", "??" or "or" (process.env.X,
// os.getenv("X"), ENV["X"]).
func defaultName(s string, k int) string {
	k = beforeBlank(s, k+1)
	if k < 0 {
		return ""
	}
	if s[k] == ')' || s[k] == ']' {
		a := strings.LastIndexAny(s[max(0, k-120):k], "([")
		if a < 0 {
			return ""
		}
		a += max(0, k-120)
		v := strings.TrimSpace(s[a+1 : k])
		if len(v) >= 2 && isQuote(v[0]) && v[len(v)-1] == v[0] {
			return v[1 : len(v)-1]
		}
		return ""
	}
	if isAlnum(s[k]) || s[k] == '_' {
		_, n := identBefore(s, k)
		return n
	}
	return ""
}

// words at the end of a function name that do not give the type ("getQueueUrl", "QueueClient")
var funcTail = setOf("client", "url", "uri", "name", "names", "id", "arn", "by", "get", "set", "put", "info", "exists",
	"async", "sync", "request", "input", "command", "builder", "new", "create", "delete", "list", "of", "for", "from",
	"with", "to", "record", "options", "config", "attributes", "properties", "service", "ref", "reference", "handle")

// type words that, in the name of a FUNCTION, do not give the type of the argument:
// ParseAddr("::1") receives an IP address, mo.group("x") is a regex group, serviceOf/userOf receive
// something else
var notFuncType = setOf("service", "app", "application", "user", "login", "addr", "address", "group", "instance",
	"stream", "subject", "exchange", "catalog", "principal", "endpoint", "fqdn", "consumergroup")

// messaging verbs: the first quoted text is a queue or topic
var queueVerbs = setOf("subscribe", "publish", "consume", "produce", "producer", "consumer", "unsubscribe")

// entFunc: the type the function name indicates (from the last piece to the first, skipping those
// that do not give the type).
func entFunc(name string) string {
	var ps [16][2]int
	n, ok := keyPieces(name, &ps)
	if !ok {
		return ""
	}
	var buf [24]byte
	for i := n - 1; i >= 0; i-- {
		w := lowercase(name, ps[i], &buf)
		if w == nil {
			return ""
		}
		p := string(w)
		if queueVerbs[p] {
			return "fila"
		}
		if e, ok := entPiece[p]; ok && !notFuncType[p] {
			return e
		}
		if e, ok := entPiece[strings.TrimSuffix(p, "s")]; ok && strings.HasSuffix(p, "s") && p != "users" {
			return e
		}
		if !funcTail[p] {
			return ""
		}
	}
	return ""
}

// list constructors: the text inside them counts as an argument of the outer call
// (subscribe(List.of("x")), Arrays.asList("x"), listOf("x"), []string{"x"}, new String[]{"x"})
var listConstructor = setOf("of", "aslist", "listof", "setof", "arrayof", "mutablelistof", "singletonlist", "singleton",
	"list", "tuple", "set", "frozenset", "array", "vec")

// words of the name of a connection call: the text followed by a port number is the server
// ($redis->connect('cache01', 6379), redis.Redis("cache01", 6379), net.Dial(...))
var connectionWords = setOf("connect", "connection", "conn", "dial", "open", "client", "socket", "session", "pool",
	"link", "redis", "mongo", "connector", "strictredis")

func findCodeCall(s string, add func(ObjFinding)) {
	if strings.IndexByte(s, '(') < 0 {
		return
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '(' {
			continue
		}
		// in code the name comes glued to the "(": "namespace (for x)" is prose
		k := i - 1
		if k < 0 || !(isAlnum(s[k]) || s[k] == '_') {
			continue
		}
		_, name := identBefore(s, k)
		if len(name) < 3 {
			continue
		}
		ent := entFunc(name)
		conn := hasWord(name, connectionWords)
		if ent == "" && !conn {
			continue
		}
		// top-level arguments, and inside lists ([..] and list constructors), up to the matching
		// ")"
		var levels []bool // each open level: is it a list?
		allList := func() bool {
			for _, l := range levels {
				if !l {
					return false
				}
			}
			return true
		}
		for j := i + 1; j < len(s) && j < i+400; j++ {
			c := s[j]
			switch {
			case c == '(':
				_, f := identBefore(s, j-1)
				levels = append(levels, j > 0 && (isAlnum(s[j-1]) || s[j-1] == '_') && listConstructor[strings.ToLower(f)])
			case c == '{':
				p := beforeBlank(s, j)
				list := p >= 0 && s[p] == ']' // []string{...}, new String[]{...}
				if p >= 0 && !list && (isAlnum(s[p]) || s[p] == '_') {
					q, _ := identBefore(s, p)
					list = q >= 2 && s[q-2:q] == "[]"
				}
				levels = append(levels, list)
			case c == ')' || c == '}':
				if len(levels) == 0 {
					j = len(s)
					continue
				}
				levels = levels[:len(levels)-1]
			case c == '\n' && len(levels) == 0 && j > i+200:
				j = len(s)
				continue
			case isQuote(c):
				e := literal(s, j)
				if e < 0 {
					j = len(s)
					continue
				}
				if allList() {
					// "name: 'v'" / "name = 'v'" inside the call is a named parameter
					if p := beforeBlank(s, j); p >= 0 && (s[p] == ':' || s[p] == '=') {
						j = e
						continue
					}
					switch {
					case conn && len(levels) == 0 && followedByPort(s, e+1):
						markValue(s, j+1, e, "servidor", "código-chamada", markedValue(s[j+1:e]), add)
					case ent != "":
						markValue(s, j+1, e, ent, "código-chamada", markedValue(s[j+1:e]), add)
					}
				}
				j = e
			}
		}
	}
}

// followedByPort: after s[i] comes ", <port number>" and the end of the argument.
func followedByPort(s string, i int) bool {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	if i >= len(s) || s[i] != ',' {
		return false
	}
	i++
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	a := i
	for i < len(s) && isDig(s[i]) {
		i++
	}
	if n := i - a; n < 2 || n > 5 {
		return false
	}
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i < len(s) && (s[i] == ',' || s[i] == ')')
}

// hasWord: some piece of the name (camelCase, _, .) is in ws.
func hasWord(name string, ws map[string]bool) bool {
	var ps [16][2]int
	n, ok := keyPieces(name, &ps)
	if !ok {
		return false
	}
	var buf [24]byte
	for x := 0; x < n; x++ {
		if w := lowercase(name, ps[x], &buf); w != nil && ws[string(w)] {
			return true
		}
	}
	return false
}
