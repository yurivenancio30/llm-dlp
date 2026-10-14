package mask

import "strings"

// Leitores de código: iguais para qualquer linguagem. Não leem a
// gramática de uma linguagem; leem a estrutura comum a todas: um texto entre aspas ao lado de
// um nome. Se o nome contém uma palavra de tipo (host, db, table, queue, topic, bucket,
// namespace, user...) em qualquer estilo (DB_HOST, queueName, queue_name, QueueName, topics,
// groupId), o texto entre aspas é desse tipo. O nome da variável, da função ou da classe nunca
// é mascarado: só o valor entre aspas.
//
// 9. o nome ao lado diz o tipo:
//	atribuição e constante   DB_HOST = "x"   const dbHost = "x"   #define DB_HOST "x"
//	                         static final String DB_HOST = "x";   val/let/var x = "x"
//	parâmetro nomeado        connect(host="x")   f(host: "x")   new X { Namespace = "x" }
//	chave de dicionário      {"host": "x"}   { host: 'x' }   'queue' => 'x'   config["Kafka:Topic"] = "x"
//	anotação/decorator       @Table(name = "x")   @KafkaListener(topics = "x")   [Table("x", Schema = "x")]
//	chave e valor em chamada setProperty("user", "x")   define('DB_HOST', 'x')   getenv("BUCKET", "x")
//	default de ambiente      process.env.DB_HOST || 'x'   os.getenv("X") or "x"
//	valor embrulhado         Bucket: aws.String("x")
//
// 10. a função chamada diz o tipo: o texto entre aspas passado a uma chamada ou construtor
// cujo nome contém a palavra de tipo (assertQueue("x"), new QueueClient(c, "x"),
// getQueueUrl("x"), createBucket("x"), bucket("x"), Table("x"), inNamespace("x"),
// subscribe(["x"])).

// literal: um texto entre aspas na mesma linha. Devolve o fim (aspa de fechamento) ou -1.
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

// beforeBlank: a posição do último caractere antes de i que não é espaço (ou -1).
func beforeBlank(s string, i int) int {
	k := i - 1
	for k >= 0 && (s[k] == ' ' || s[k] == '\t') {
		k--
	}
	return k
}

// identBefore: o identificador que termina em s[k] (inclusive): letras, dígitos, _ $ .
// Devolve o início e o último pedaço depois de "." ou "->" (self.table_name -> table_name).
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

// nameBefore: o nome que fica à esquerda de um operador que termina em s[k]: um identificador,
// um texto entre aspas ('queue' =>, "host":), um índice (config["Kafka:Topic"]) ou um
// símbolo do Ruby (:host =>). Pula a anotação de tipo ("NAME: str =", "x: String =").
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
		// "NAME: str =": o tipo está à direita do ":"; o nome vem antes
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

// markedValue: em código, só ensina sozinho o valor com dígito, "_" ou "." (pgsrv-01, vendas_prd,
// db.interno); "payments-api" é mascarado no lugar e só é aprendido se aparecer em duas regras
// (duas palavras com hífen aparecem demais em código público).
func markedValue(v string) bool { return strings.ContainsAny(v, "0123456789_.") }

// ambiguousInCode: o último pedaço do nome é uma palavra de tipo que, em código, quase sempre
// quer dizer outra coisa: stream (fluxo de entrada e saída), subject (assunto de e-mail)
func ambiguousInCode(name string) bool {
	switch strings.ToLower(lastPiece(name)) {
	case "stream", "streams", "subject", "subjects":
		return true
	}
	return false
}

// embrulho: funções que só embrulham o valor (aws.String("x"), to.Ptr("x"), Optional.of("x"))
var wrapper = setOf("string", "str", "ptr", "toptr", "stringptr", "pointer", "some", "of", "value", "valueof",
	"stringvalue", "new", "text", "s")

// shapedKey: uma chave entre aspas que é um nome ("host", "Kafka:Topic", "db.host"), e não
// uma URL ou uma frase
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

// commonType: nomes de tipo que aparecem entre o nome e o "=" ("String x =" fica com x, mas
// "x: String =" precisa pular "String").
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
		// crase: em prosa (markdown) marca código, não um valor
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
		// prefixo de literal: r"", f"", b"", u"", @"" (C#)
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
			// "? 'x' : 'y'" (operador ternário) não é chave
			if p := beforeBlank(s, k); p >= 0 && isQuote(s[p]) {
				a := max(0, p-lineWindow)
				if o := strings.LastIndexByte(s[a:p], s[p]); o >= 0 {
					if q := beforeBlank(s, a+o); q >= 0 && s[q] == '?' {
						name = ""
					}
				}
			}
		case c == ',':
			// chave e valor em chamada: f("KEY", "v")
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
			// valor embrulhado: KEY: fn("v") / KEY = fn("v") com um argumento só
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
			// @Table(name = "x"), Queue(name="x"): o tipo vem de quem é chamado
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

// Default de placeholder: ${NOME:valor} (Spring, Micronaut), ${NOME:-valor} e ${NOME-valor}
// (shell, docker-compose). O nome da variável diz o tipo do valor, como em "NOME = valor".
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
			if end > 0 && !strings.Contains(s[v:v+end], "://") { // URL: fica com o leitor de URL
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

// defaultPlaceholder: s[k] é o ":" (ou o "-" de ":-") que separa o nome do default em ${NOME:...}.
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

// enclosingCall: o nome da função, construtor ou anotação cujo "(" ainda está aberto
// antes de s[k] (na mesma instrução, até 300 bytes atrás).
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
				if s[i] == '[' { // [Table("x", Schema = "y")] do C#: o nome vem depois do "["
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

// palavras do nome de uma função que recebe (chave, valor): setProperty, define, getenv,
// os.environ.get, ENV.fetch, getOrDefault, Setenv, put, config.set...
var keyValueFunc = setOf("set", "put", "define", "getenv", "env", "environ", "get", "fetch", "default",
	"property", "properties", "setting", "settings", "config", "option", "options", "param", "attr", "setenv",
	"header", "add", "with", "or")

// keyValueCall: o "(" em s[i] abre a chamada de uma função de (chave, valor)?
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

// defaultName: o nome da variável de ambiente antes de "||", "??" ou "or" (process.env.X,
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

// palavras do fim do nome de uma função que não dizem o tipo ("getQueueUrl", "QueueClient")
var funcTail = setOf("client", "url", "uri", "name", "names", "id", "arn", "by", "get", "set", "put", "info", "exists",
	"async", "sync", "request", "input", "command", "builder", "new", "create", "delete", "list", "of", "for", "from",
	"with", "to", "record", "options", "config", "attributes", "properties", "service", "ref", "reference", "handle")

// palavras de tipo que, no nome de uma FUNÇÃO, não dizem o tipo do argumento: ParseAddr("::1")
// recebe um endereço IP, mo.group("x") é grupo de regex, serviceOf/userOf recebem outra coisa
var notFuncType = setOf("service", "app", "application", "user", "login", "addr", "address", "group", "instance",
	"stream", "subject", "exchange", "catalog", "principal", "endpoint", "fqdn", "consumergroup")

// verbos de mensageria: o primeiro texto entre aspas é uma fila ou tópico
var queueVerbs = setOf("subscribe", "publish", "consume", "produce", "producer", "consumer", "unsubscribe")

// entFunc: o tipo que o nome da função indica (do último pedaço para o primeiro, pulando os
// que não dizem o tipo).
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

// construtores de lista: o texto dentro deles conta como argumento da chamada de fora
// (subscribe(List.of("x")), Arrays.asList("x"), listOf("x"), []string{"x"}, new String[]{"x"})
var listConstructor = setOf("of", "aslist", "listof", "setof", "arrayof", "mutablelistof", "singletonlist", "singleton",
	"list", "tuple", "set", "frozenset", "array", "vec")

// palavras do nome de uma chamada de conexão: o texto seguido de um número de porta é o
// servidor ($redis->connect('cache01', 6379), redis.Redis("cache01", 6379), net.Dial(...))
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
		// em código o nome vem colado no "(": "namespace (for x)" é prosa
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
		// argumentos de primeiro nível, e dentro de listas ([..] e construtores de lista), até o
		// ")" correspondente
		var levels []bool // cada nível aberto: é lista?
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
					// "nome: 'v'" / "nome = 'v'" dentro da chamada é parâmetro nomeado
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

// followedByPort: depois de s[i] vem ", <número de porta>" e o fim do argumento.
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

// hasWord: algum pedaço do nome (camelCase, _, .) está em ws.
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
