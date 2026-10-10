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

func ehAspa(c byte) bool { return c == '"' || c == '\'' || c == '`' }

// antesBranco: a posição do último caractere antes de i que não é espaço (ou -1).
func antesBranco(s string, i int) int {
	k := i - 1
	for k >= 0 && (s[k] == ' ' || s[k] == '\t') {
		k--
	}
	return k
}

// identAntes: o identificador que termina em s[k] (inclusive): letras, dígitos, _ $ .
// Devolve o início e o último pedaço depois de "." ou "->" (self.table_name -> table_name).
func identAntes(s string, k int) (ini int, ult string) {
	a := k
	for a >= 0 && (ehAlnum(s[a]) || s[a] == '_' || s[a] == '$' || s[a] == '.' || s[a] == '>' && a > 0 && s[a-1] == '-' || s[a] == '-' && a+1 <= k && s[a+1] == '>') {
		a--
	}
	nome := s[a+1 : k+1]
	if i := strings.LastIndexAny(nome, ".>"); i >= 0 {
		nome = nome[i+1:]
	}
	return a + 1, strings.TrimLeft(nome, "$")
}

// nomeAntes: o nome que fica à esquerda de um operador que termina em s[k]: um identificador,
// um texto entre aspas ('queue' =>, "host":), um índice (config["Kafka:Topic"]) ou um
// símbolo do Ruby (:host =>). Pula a anotação de tipo ("NAME: str =", "x: String =").
func nomeAntes(s string, k int) string {
	k = antesBranco(s, k+1)
	if k < 0 {
		return ""
	}
	switch {
	case ehAspa(s[k]):
		q := s[k]
		a := strings.LastIndexByte(s[max(0, k-120):k], q)
		if a < 0 {
			return ""
		}
		a += max(0, k-120)
		if v := s[a+1 : k]; chaveComForma(v) {
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
		if len(v) >= 2 && ehAspa(v[0]) && v[len(v)-1] == v[0] && chaveComForma(v[1:len(v)-1]) {
			return v[1 : len(v)-1]
		}
		return ""
	case ehAlnum(s[k]) || s[k] == '_':
		a, nome := identAntes(s, k)
		// "NAME: str =": o tipo está à direita do ":"; o nome vem antes
		if palavrasTipo[strings.ToLower(nome)] || tipoComum(nome) {
			if p := antesBranco(s, a); p >= 0 && s[p] == ':' {
				if _, n2 := identAntes(s, antesBranco(s, p)); n2 != "" {
					return n2
				}
			}
		}
		return nome
	}
	return ""
}

// valorMarcado: em código, só ensina sozinho o valor com dígito, "_" ou "." (pgsrv-01, vendas_prd,
// db.interno); "payments-api" é mascarado no lugar e só é aprendido se aparecer em duas regras
// (duas palavras com hífen aparecem demais em código público).
func valorMarcado(v string) bool { return strings.ContainsAny(v, "0123456789_.") }

// ambiguaNoCodigo: o último pedaço do nome é uma palavra de tipo que, em código, quase sempre
// quer dizer outra coisa: stream (fluxo de entrada e saída), subject (assunto de e-mail)
func ambiguaNoCodigo(nome string) bool {
	switch strings.ToLower(ultimoPedaco(nome)) {
	case "stream", "streams", "subject", "subjects":
		return true
	}
	return false
}

// embrulho: funções que só embrulham o valor (aws.String("x"), to.Ptr("x"), Optional.of("x"))
var embrulho = conj("string", "str", "ptr", "toptr", "stringptr", "pointer", "some", "of", "value", "valueof",
	"stringvalue", "new", "text", "s")

// chaveComForma: uma chave entre aspas que é um nome ("host", "Kafka:Topic", "db.host"), e não
// uma URL ou uma frase
func chaveComForma(v string) bool {
	if v == "" || len(v) > 80 || strings.Contains(v, "//") || !(letraD(v[0]) || v[0] == '_') {
		return false
	}
	for i := 0; i < len(v); i++ {
		if c := v[i]; !(ehAlnum(c) || c == '_' || c == '.' || c == '-' || c == ':') {
			return false
		}
	}
	return true
}

// tipoComum: nomes de tipo que aparecem entre o nome e o "=" ("String x =" fica com x, mas
// "x: String =" precisa pular "String").
func tipoComum(v string) bool {
	switch v {
	case "String", "str", "string", "Str", "&str", "text", "Text", "char", "Optional", "Any", "any":
		return true
	}
	return false
}

func acharCodigoNome(s string, add func(ObjAchado)) {
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
		k := antesBranco(s, a-1)
		// prefixo de literal: r"", f"", b"", u"", @"" (C#)
		if k >= 0 && strings.IndexByte("rRfFbBuU@", s[k]) >= 0 && k == a-2 && (k == 0 || !ehAlnum(s[k-1])) {
			k = antesBranco(s, k)
		}
		if k < 0 {
			continue
		}
		nome := ""
		switch c := s[k]; {
		case c == '>' && k > 0 && s[k-1] == '=': // 'k' => 'v'
			nome = nomeAntes(s, k-2)
		case c == '=' && (k == 0 || strings.IndexByte("=!<>+-*/%&|^", s[k-1]) < 0) && (k+1 >= len(s) || s[k+1] != '='):
			p := k - 1
			if p >= 0 && (s[p] == ':' || s[p] == '?') { // := ?=
				p--
			}
			nome = nomeAntes(s, p)
		case c == ':' && (k == 0 || s[k-1] != ':') && (k+1 >= len(s) || s[k+1] != ':'):
			nome = nomeAntes(s, k-1)
			// "? 'x' : 'y'" (operador ternário) não é chave
			if p := antesBranco(s, k); p >= 0 && ehAspa(s[p]) {
				a := max(0, p-janelaLinha)
				if o := strings.LastIndexByte(s[a:p], s[p]); o >= 0 {
					if q := antesBranco(s, a+o); q >= 0 && s[q] == '?' {
						nome = ""
					}
				}
			}
		case c == ',':
			// chave e valor em chamada: f("KEY", "v")
			p := antesBranco(s, k)
			if p >= 0 && ehAspa(s[p]) {
				q := strings.LastIndexByte(s[max(0, p-120):p], s[p])
				if q >= 0 {
					q += max(0, p-120)
					if r := antesBranco(s, q); r >= 0 && s[r] == '(' && chamadaChaveValor(s, r) {
						nome = s[q+1 : p]
					}
				}
			}
		case c == '|' && k > 0 && s[k-1] == '|', c == '?' && k > 0 && s[k-1] == '?':
			nome = nomeDefault(s, k-2)
		case c == 'r' && k >= 1 && s[k-1] == 'o' && (k < 2 || !ehAlnum(s[k-2])): // "... or 'v'"
			nome = nomeDefault(s, k-2)
		case c == '(':
			// valor embrulhado: KEY: fn("v") / KEY = fn("v") com um argumento só
			r := b + 1
			for r < len(s) && (s[r] == ' ' || s[r] == '\t') {
				r++
			}
			if r < len(s) && s[r] == ')' {
				ini, f := identAntes(s, antesBranco(s, k))
				if p := antesBranco(s, ini); embrulho[strings.ToLower(f)] && p >= 0 && (s[p] == ':' || s[p] == '=') && (p == 0 || s[p-1] != ':') {
					nome = nomeAntes(s, p-1)
				}
			}
		case ehAlnum(c) || c == '_':
			// #define NOME "v"
			ini, n := identAntes(s, k)
			if p := antesBranco(s, ini); p >= 6 && s[p-6:p+1] == "#define" {
				nome = n
			}
		}
		if nome == "" {
			continue
		}
		if ambiguaNoCodigo(nome) {
			continue
		}
		ent, forte := entChave(nome)
		if ent == "" && (nome == "name" || nome == "value" || nome == "names") {
			// @Table(name = "x"), Queue(name="x"): o tipo vem de quem é chamado
			if f := chamadaEnvolvente(s, k); f != "" {
				ent, forte = entFuncao(f), true
			}
		}
		if ent == "" {
			continue
		}
		marcarValor(s, a, b, ent, "código-nome", forte && valorMarcado(s[a:b]), add)
	}
}

// Default de placeholder: ${NOME:valor} (Spring, Micronaut), ${NOME:-valor} e ${NOME-valor}
// (shell, docker-compose). O nome da variável diz o tipo do valor, como em "NOME = valor".
func acharPlaceholder(s string, add func(ObjAchado)) {
	for i := strings.Index(s, "${"); i >= 0; {
		a := i + 2
		b := a
		for b < len(s) && (ehAlnum(s[b]) || s[b] == '_' || s[b] == '.' || s[b] == '-' && b+1 < len(s) && s[b+1] != '}' && !(b > a && s[b-1] == ':')) {
			b++
		}
		if b > a && b < len(s) && (s[b] == ':' || s[b] == '-') {
			v := b + 1
			if s[b] == ':' && v < len(s) && (s[v] == '-' || s[v] == '=') {
				v++
			}
			fim := strings.IndexByte(s[v:min(len(s), v+300)], '}')
			if fim > 0 && !strings.Contains(s[v:v+fim], "://") { // URL: fica com o leitor de URL
				if ent, forte := entChave(s[a:b]); ent != "" {
					marcarValor(s, v, v+fim, ent, "placeholder", forte, add)
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
		if !(ehAlnum(s[x]) || s[x] == '_' || s[x] == '.' || s[x] == '-') {
			return false
		}
	}
	return k > a
}

// chamadaEnvolvente: o nome da função, construtor ou anotação cujo "(" ainda está aberto
// antes de s[k] (na mesma instrução, até 300 bytes atrás).
func chamadaEnvolvente(s string, k int) string {
	prof := 0
	for i := k; i >= 0 && i > k-300; i-- {
		switch s[i] {
		case ')', ']', '}':
			prof++
		case '(', '[', '{':
			if prof == 0 {
				if s[i] != '(' && s[i] != '[' {
					return ""
				}
				p := antesBranco(s, i)
				if s[i] == '[' { // [Table("x", Schema = "y")] do C#: o nome vem depois do "["
					e := i + 1
					for e < len(s) && (ehAlnum(s[e]) || s[e] == '_') {
						e++
					}
					return s[i+1 : e]
				}
				if p >= 0 && (ehAlnum(s[p]) || s[p] == '_') {
					_, n := identAntes(s, p)
					return n
				}
				return ""
			}
			prof--
		case ';', '\n':
			if prof == 0 && s[i] == ';' {
				return ""
			}
		}
	}
	return ""
}

// palavras do nome de uma função que recebe (chave, valor): setProperty, define, getenv,
// os.environ.get, ENV.fetch, getOrDefault, Setenv, put, config.set...
var funcaoChaveValor = conj("set", "put", "define", "getenv", "env", "environ", "get", "fetch", "default",
	"property", "properties", "setting", "settings", "config", "option", "options", "param", "attr", "setenv",
	"header", "add", "with", "or")

// chamadaChaveValor: o "(" em s[i] abre a chamada de uma função de (chave, valor)?
func chamadaChaveValor(s string, i int) bool {
	k := antesBranco(s, i)
	if k < 0 || !(ehAlnum(s[k]) || s[k] == '_') {
		return false
	}
	_, nome := identAntes(s, k)
	var ps [16][2]int
	n, ok := pedacosChave(nome, &ps)
	if !ok {
		return false
	}
	var buf [24]byte
	for x := 0; x < n; x++ {
		if w := minusculo(nome, ps[x], &buf); w != nil && funcaoChaveValor[string(w)] {
			return true
		}
	}
	return false
}

// nomeDefault: o nome da variável de ambiente antes de "||", "??" ou "or" (process.env.X,
// os.getenv("X"), ENV["X"]).
func nomeDefault(s string, k int) string {
	k = antesBranco(s, k+1)
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
		if len(v) >= 2 && ehAspa(v[0]) && v[len(v)-1] == v[0] {
			return v[1 : len(v)-1]
		}
		return ""
	}
	if ehAlnum(s[k]) || s[k] == '_' {
		_, n := identAntes(s, k)
		return n
	}
	return ""
}

// palavras do fim do nome de uma função que não dizem o tipo ("getQueueUrl", "QueueClient")
var caudaFuncao = conj("client", "url", "uri", "name", "names", "id", "arn", "by", "get", "set", "put", "info", "exists",
	"async", "sync", "request", "input", "command", "builder", "new", "create", "delete", "list", "of", "for", "from",
	"with", "to", "record", "options", "config", "attributes", "properties", "service", "ref", "reference", "handle")

// palavras de tipo que, no nome de uma FUNÇÃO, não dizem o tipo do argumento: ParseAddr("::1")
// recebe um endereço IP, mo.group("x") é grupo de regex, serviceOf/userOf recebem outra coisa
var naoTipoFuncao = conj("service", "app", "application", "user", "login", "addr", "address", "group", "instance",
	"stream", "subject", "exchange", "catalog", "principal", "endpoint", "fqdn", "consumergroup")

// verbos de mensageria: o primeiro texto entre aspas é uma fila ou tópico
var verbosFila = conj("subscribe", "publish", "consume", "produce", "producer", "consumer", "unsubscribe")

// entFuncao: o tipo que o nome da função indica (do último pedaço para o primeiro, pulando os
// que não dizem o tipo).
func entFuncao(nome string) string {
	var ps [16][2]int
	n, ok := pedacosChave(nome, &ps)
	if !ok {
		return ""
	}
	var buf [24]byte
	for i := n - 1; i >= 0; i-- {
		w := minusculo(nome, ps[i], &buf)
		if w == nil {
			return ""
		}
		p := string(w)
		if verbosFila[p] {
			return "fila"
		}
		if e, ok := entPedaco[p]; ok && !naoTipoFuncao[p] {
			return e
		}
		if e, ok := entPedaco[strings.TrimSuffix(p, "s")]; ok && strings.HasSuffix(p, "s") && p != "users" {
			return e
		}
		if !caudaFuncao[p] {
			return ""
		}
	}
	return ""
}

// construtores de lista: o texto dentro deles conta como argumento da chamada de fora
// (subscribe(List.of("x")), Arrays.asList("x"), listOf("x"), []string{"x"}, new String[]{"x"})
var construtorLista = conj("of", "aslist", "listof", "setof", "arrayof", "mutablelistof", "singletonlist", "singleton",
	"list", "tuple", "set", "frozenset", "array", "vec")

// palavras do nome de uma chamada de conexão: o texto seguido de um número de porta é o
// servidor ($redis->connect('cache01', 6379), redis.Redis("cache01", 6379), net.Dial(...))
var palavrasConexao = conj("connect", "connection", "conn", "dial", "open", "client", "socket", "session", "pool",
	"link", "redis", "mongo", "connector", "strictredis")

func acharCodigoChamada(s string, add func(ObjAchado)) {
	if strings.IndexByte(s, '(') < 0 {
		return
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '(' {
			continue
		}
		// em código o nome vem colado no "(": "namespace (for x)" é prosa
		k := i - 1
		if k < 0 || !(ehAlnum(s[k]) || s[k] == '_') {
			continue
		}
		_, nome := identAntes(s, k)
		if len(nome) < 3 {
			continue
		}
		ent := entFuncao(nome)
		conex := temPalavra(nome, palavrasConexao)
		if ent == "" && !conex {
			continue
		}
		// argumentos de primeiro nível, e dentro de listas ([..] e construtores de lista), até o
		// ")" correspondente
		var niveis []bool // cada nível aberto: é lista?
		todosLista := func() bool {
			for _, l := range niveis {
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
				_, f := identAntes(s, j-1)
				niveis = append(niveis, j > 0 && (ehAlnum(s[j-1]) || s[j-1] == '_') && construtorLista[strings.ToLower(f)])
			case c == '{':
				p := antesBranco(s, j)
				lista := p >= 0 && s[p] == ']' // []string{...}, new String[]{...}
				if p >= 0 && !lista && (ehAlnum(s[p]) || s[p] == '_') {
					q, _ := identAntes(s, p)
					lista = q >= 2 && s[q-2:q] == "[]"
				}
				niveis = append(niveis, lista)
			case c == ')' || c == '}':
				if len(niveis) == 0 {
					j = len(s)
					continue
				}
				niveis = niveis[:len(niveis)-1]
			case c == '\n' && len(niveis) == 0 && j > i+200:
				j = len(s)
				continue
			case ehAspa(c):
				e := literal(s, j)
				if e < 0 {
					j = len(s)
					continue
				}
				if todosLista() {
					// "nome: 'v'" / "nome = 'v'" dentro da chamada é parâmetro nomeado
					if p := antesBranco(s, j); p >= 0 && (s[p] == ':' || s[p] == '=') {
						j = e
						continue
					}
					switch {
					case conex && len(niveis) == 0 && seguidoDePorta(s, e+1):
						marcarValor(s, j+1, e, "servidor", "código-chamada", valorMarcado(s[j+1:e]), add)
					case ent != "":
						marcarValor(s, j+1, e, ent, "código-chamada", valorMarcado(s[j+1:e]), add)
					}
				}
				j = e
			}
		}
	}
}

// seguidoDePorta: depois de s[i] vem ", <número de porta>" e o fim do argumento.
func seguidoDePorta(s string, i int) bool {
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
	for i < len(s) && ehDig(s[i]) {
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

// temPalavra: algum pedaço do nome (camelCase, _, .) está em ws.
func temPalavra(nome string, ws map[string]bool) bool {
	var ps [16][2]int
	n, ok := pedacosChave(nome, &ps)
	if !ok {
		return false
	}
	var buf [24]byte
	for x := 0; x < n; x++ {
		if w := minusculo(nome, ps[x], &buf); w != nil && ws[string(w)] {
			return true
		}
	}
	return false
}
