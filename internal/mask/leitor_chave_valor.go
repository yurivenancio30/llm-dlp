package mask

import (
	"regexp"
	"strings"
	"unicode"
)

// Reader de chave-valor genérico (ver docs/pt-BR/estruturas.md, seção JSON, YAML, TOML, INI, .env,
// .properties, XML): o valor de uma chave cujo ÚLTIMO pedaço do nome diz o que ele é
// ("host", "db_host", "spring.datasource.username", "bootstrap.servers"). Vale para
// "chave: valor" (YAML, .properties, JSON com aspas), "chave=valor" (INI, TOML, .env,
// .properties, atributo XML), <chave>valor</chave> e opções longas de linha de comando
// (--chave valor, --chave=valor). Nada de ferramenta específica: só a forma.
//
// Freios contra código (Go, Python, JS): ":=" e operadores ficam de fora; valor seguido de
// "(", "[" ou "." é chamada ou acesso; valor sem aspas, sem hífen e sem dígito (um nome que
// pode ser variável) só vale em linha de configuração (chave no começo da linha, valor até o
// fim dela, ":" de YAML, chave de .env em MAIÚSCULAS, chave pontuada de .properties, ou "="
// sem espaços em volta); nome pontuado sem dígito/hífen ("self.host", "cfg.user") é acesso a
// atributo. Domínio público (TLD conhecido) não é mascarado.

// keyPieces divide um nome em pedaços por . _ - : e camelCase ("dbHost" -> db, Host;
// "DBHost" -> DB, Host). Devolve no máximo len(ps) pedaços; ok=false se havia mais.
func keyPieces(k string, ps *[16][2]int) (n int, ok bool) {
	ini := 0
	for i := 0; i <= len(k); i++ {
		if i == len(k) || k[i] == '.' || k[i] == '_' || k[i] == '-' || k[i] == ':' {
			if i > ini {
				if n == len(ps) {
					return n, false
				}
				ps[n] = [2]int{ini, i}
				n++
			}
			ini = i + 1
			continue
		}
		c := k[i]
		if c >= 'A' && c <= 'Z' && i > ini {
			p := k[i-1]
			if p >= 'a' && p <= 'z' || p >= '0' && p <= '9' || p >= 'A' && p <= 'Z' && i+1 < len(k) && k[i+1] >= 'a' && k[i+1] <= 'z' {
				if n == len(ps) {
					return n, false
				}
				ps[n] = [2]int{ini, i}
				n++
				ini = i
			}
		}
	}
	return n, true
}

// minusculo copia k[p] em minúsculas para buf (sem alocar).
func lowercase(k string, p [2]int, buf *[24]byte) []byte {
	if p[1]-p[0] > len(buf) {
		return nil
	}
	b := buf[:p[1]-p[0]]
	for i := range b {
		c := k[p[0]+i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b[i] = c
	}
	return b
}

// entKey: a entidade que o nome da chave indica e se isso é evidência forte. É forte quando o
// último pedaço da chave é o próprio tipo: "host", "--host", "S3_BUCKET", "KAFKA_TOPIC",
// "spring.datasource.username", "queueName", "bootstrap.servers" dizem explicitamente o que o
// valor é. (Uma chave em que o tipo não é o último pedaço não indica entidade: "hostPort".)
func entKey(k string) (ent string, strong bool) {
	var ps [16][2]int
	n, ok := keyPieces(k, &ps)
	if !ok || n == 0 {
		return "", false
	}
	var b1, b2 [24]byte
	lastPos := lowercase(k, ps[n-1], &b1)
	if lastPos == nil {
		return "", false
	}
	if e, ok := entPiece[string(lastPos)]; ok {
		return e, true
	}
	// "project": o projeto do provedor de nuvem quando a chave diz qual (GCP_PROJECT,
	// bq_project); sozinho, é evidência fraca (mascara no lugar, não ensina)
	if string(lastPos) == "project" {
		strong := false
		if n >= 2 {
			var b [24]byte
			strong = cloudProjectPrefix[string(lowercase(k, ps[n-2], &b))]
		}
		return "conta_nuvem", strong
	}
	// colado: "rolename", "warehousename", "accountname", "fieldpath"
	for _, suf := range gluedSuffixes {
		if u := string(lastPos); len(u) > len(suf)+1 && strings.HasSuffix(u, suf) {
			if e, ok := entBeforeName[u[:len(u)-len(suf)]]; ok {
				return e, true
			}
		}
	}
	// plural: topics, queues, buckets, hosts, brokers
	if u := string(lastPos); len(u) > 3 && u[len(u)-1] == 's' {
		if e, ok := entPiece[u[:len(u)-1]]; ok && pluralType[u] {
			return e, true
		}
	}
	if n >= 2 {
		pen := lowercase(k, ps[n-2], &b2)
		// "<tipo>Name", "<tipo>_id", "groupId": o pedaço antes de name/id diz o tipo
		if string(pen) == "account" && n >= 3 && strings.EqualFold(k[ps[n-3][0]:ps[n-3][1]], "service") {
			return "usuario", true // serviceAccountName: conta de serviço é usuário
		}
		if u := string(lastPos); u == "name" || u == "names" || u == "id" || u == "ids" || u == "path" && (string(pen) == "field" || string(pen) == "column") {
			if e, ok := entBeforeName[string(pen)]; ok {
				return e, true
			}
		}
		// "app.queue.pedidos": a palavra de tipo no meio da chave (evidência fraca), desde que o
		// último pedaço não seja um atributo (porta, timeout, versão, senha...)
		if !keyAttribute[string(lastPos)] {
			var b3 [24]byte
			for i := n - 2; i >= 0; i-- {
				if w := lowercase(k, ps[i], &b3); w != nil {
					if e, ok := entPiece[string(w)]; ok && string(w) != "app" && string(w) != "application" {
						return e, false
					}
				}
			}
		}
		switch string(lastPos) {
		case "servers":
			if string(pen) == "bootstrap" {
				return "servidor", true
			}
		case "id":
			if string(pen) == "project" {
				return "conta_nuvem", true
			}
		case "name":
			if e, ok := entBeforeName[string(pen)]; ok {
				return e, true
			}
		}
	}
	return "", false
}

// dashOrDigit: o valor tem dígito ou hífen entre letras/dígitos, que nome de variável não tem
func dashOrDigit(v string) bool {
	for i := 0; i < len(v); i++ {
		if v[i] >= '0' && v[i] <= '9' || v[i] == '-' && i > 0 && i+1 < len(v) && isAlnum(v[i-1]) && isAlnum(v[i+1]) {
			return true
		}
	}
	return false
}

func kvValueEnd(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', ',', ';', '&', ')', '}', ']', '"', '\'', '<', '>', '`', '|', '(', '[', '{':
		return true
	}
	return false
}

func letterD(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// titulo: uma palavra com só a primeira letra maiúscula ("Server", "Host-Name" não).
func title(v string) bool {
	if v == "" || !(v[0] >= 'A' && v[0] <= 'Z') {
		return false
	}
	for i := 1; i < len(v); i++ {
		if !(v[i] >= 'a' && v[i] <= 'z') {
			return false
		}
	}
	return true
}

// dottedName: nome separado por pontos, em minúsculas: tópico/fila (fin.notas.emitidas) ou
// usuário (maria.souza, josé.antônio). Não vale se um pedaço é receptor ou atributo de código
// (cfg.topic, settings.db_user), se é domínio público ou se termina em extensão de arquivo.
func dottedName(v string, user bool) bool {
	ps := strings.Split(v, ".")
	if len(ps) < 2 || publicDomain(v) || fileExtensions[ps[len(ps)-1]] {
		return false
	}
	for _, p := range ps {
		if p == "" || codeReceivers[p] || keyAttribute[p] || typeWords[p] {
			return false
		}
		for _, c := range p {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c >= 0x80 && unicode.IsLower(c)) {
				return false
			}
		}
		// "cfg.queue_name", "settings.db_user": o pedaço é nome de atributo ou de chave
		if u := strings.LastIndexByte(p, '_'); u >= 0 && keyAttribute[p[u+1:]] {
			return false
		}
		if e, _ := entKey(p); user && e != "" {
			return false
		}
	}
	return true
}

func uppercaseOnly(v string) bool {
	has := false
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c >= 'a' && c <= 'z' {
			return false
		}
		has = has || c >= 'A' && c <= 'Z'
	}
	return has
}

func findKeyValue(s string, add func(ObjFinding)) {
	for p := 1; p+1 < len(s); p++ {
		if c := s[p]; c == '=' || c == ':' {
			kvIn(s, p, add)
		}
	}
	if strings.Contains(s, "</") {
		findXMLValue(s, add)
	}
	if strings.Count(s, "\n") >= 1 {
		findSpaceKey(s, add)
	}
	if strings.Contains(s, "--") {
		findLongOptions(s, add)
	}
}

// kvIn examina o separador em s[p].
func kvIn(s string, p int, add func(ObjFinding)) {
	c, nx, pv := s[p], s[p+1], s[p-1]
	if nx == '=' || nx == ':' || pv == ':' || pv == '=' {
		return // ==, :=, ::, =:
	}
	if c == '=' && strings.IndexByte("!<>+-*/%&|^~?", pv) >= 0 {
		return
	}
	k := p
	for k > 0 && p-k < 3 && (s[k-1] == ' ' || s[k-1] == '\t') {
		k--
	}
	if k == 0 {
		return
	}
	var ka, kb, ini int
	keyQuotes := false
	if q := s[k-1]; q == '"' || q == '\'' {
		lim := max(0, k-1-64)
		j := strings.LastIndexByte(s[lim:k-1], q)
		if j < 0 {
			return
		}
		ka, kb, ini = lim+j+1, k-1, lim+j
		keyQuotes = true
	} else {
		a := k
		for a > 0 && k-a <= 64 && (isAlnum(s[a-1]) || s[a-1] == '_' || s[a-1] == '.' || s[a-1] == '-') {
			a--
		}
		ka, kb, ini = a, k, a
	}
	// ":" de YAML/.properties pede espaço depois; JSON (chave entre aspas) não
	if c == ':' && !keyQuotes && nx != ' ' && nx != '\t' && nx != '\n' && nx != '\r' {
		return
	}
	if kb <= ka || kb-ka > 64 {
		return
	}
	cli := false
	if !keyQuotes {
		for ka < kb && s[ka] == '-' {
			ka++
			cli = true
		}
	}
	for !keyQuotes && kb-ka > 4 && s[ka] == '_' && s[kb-1] == '_' { // __tablename__ (Python)
		ka, kb = ka+1, kb-1
	}
	if ka >= kb || !letterD(s[ka]) || s[kb-1] == '.' {
		return
	}
	if ini > 0 && strings.IndexByte(" \t\n\r{,([;&?", s[ini-1]) < 0 {
		return
	}
	key := s[ka:kb]
	ent, exact := entKey(key)
	if ent == "" && c == '=' && (key == "name" || key == "nome") {
		ent, exact = enclosingTag(s, ini), true // <column name="x">
	}
	if ent == "" {
		return
	}
	// começo de linha (com recuo, "- " de lista ou "export ")
	j := ini
	for j > 0 && (s[j-1] == ' ' || s[j-1] == '\t') {
		j--
	}
	if j > 0 && s[j-1] == '-' {
		j--
		for j > 0 && (s[j-1] == ' ' || s[j-1] == '\t') {
			j--
		}
	}
	if j >= 6 && s[j-6:j] == "export" {
		j -= 6
	}
	lineStartPos := j == 0 || s[j-1] == '\n'
	// valor
	v := p + 1
	for v < len(s) && (s[v] == ' ' || s[v] == '\t') {
		v++
	}
	if v >= len(s) || s[v] == '\n' || s[v] == '\r' {
		if c == ':' && lineStartPos {
			yamlList(s, v, ent, add) // "tables:\n  - a\n  - b"
		}
		return
	}
	if s[v] == '[' {
		inlineList(s, v, ent, add) // "tables": ["a", "b"], tabelas: [a, b]
		return
	}
	va, vb := v, v
	quotes := false
	if q := s[v]; q == '"' || q == '\'' {
		lim := min(len(s), v+1+256)
		e := strings.IndexByte(s[v+1:lim], q)
		if e <= 0 {
			return
		}
		va, vb, quotes = v+1, v+1+e, true
	} else {
		for vb < len(s) && !kvValueEnd(s[vb]) {
			vb++
			// lista de servidores sem espaço: "b1:9092,b2:9092"
			if ent == "servidor" && vb+1 < len(s) && s[vb] == ',' && isAlnum(s[vb+1]) {
				vb++
			}
		}
		// chamada, índice, ou prefixo de literal (f"...", r'...'). Aspa depois de um valor maior
		// é a que fecha a string em volta ("...table=x"}): o valor vale.
		if vb == va || vb < len(s) && (strings.IndexByte("([{", s[vb]) >= 0 || strings.IndexByte("\"'`", s[vb]) >= 0 && vb-va <= 2) {
			return
		}
		// seguido de operador: é expressão ("addr = arg0 + aux", "host = base + sufixo")
		for r := vb; r < len(s) && r < vb+4; r++ {
			if s[r] == ' ' || s[r] == '\t' {
				continue
			}
			if strings.IndexByte("+*/%&|^<>?=", s[r]) >= 0 || s[r] == '-' && r+1 < len(s) && (s[r+1] == ' ' || s[r+1] == '>') {
				return
			}
			break
		}
		for vb > va && s[vb-1] == '.' { // ponto final de frase
			vb--
		}
	}
	val := s[va:vb]
	if strings.EqualFold(val, key) || typeWords[strings.ToLower(val)] {
		return
	}
	if !quotes && !dashOrDigit(val) && !(cli && identifierShape(val) && !uppercaseOnly(val)) {
		// pode ser nome de variável: só em linha de configuração
		r := vb
		for r < len(s) && (s[r] == ' ' || s[r] == '\t' || s[r] == '\r') {
			r++
		}
		lineEnd := r == len(s) || s[r] == '\n' || s[r] == '#' && r > vb
		if (!lineStartPos || !lineEnd) && !(c == '=' && pv != ' ' && nx != ' ' && pairsInLine(s, p) >= 2) {
			return
		}
		punctuated := strings.IndexByte(key, '.') >= 0
		if punctuated {
			if i := strings.IndexByte(key, '.'); codeReceivers[strings.ToLower(key[:i])] {
				return
			}
		}
		// "chave = valor" com espaços é atribuição de código, salvo dentro de uma seção de INI
		// ("[db]\ntable_name = pedidos"): ver iniSection (decisor_lexico.go)
		if !(c == ':' || uppercaseOnly(key) || punctuated || cli || pv != ' ' && nx != ' ' || c == '=' && lineEnd && lineStartPos && iniSection(s, ini)) {
			return
		}
		// "Server: nginx", "User: alice": rótulo de prosa ou cabeçalho HTTP/e-mail (Título),
		// não chave de configuração (minúsculas, snake_case, camelCase ou MAIÚSCULAS)
		if c == ':' && title(key) {
			return
		}
		// tópico/fila (fin.notas.emitidas) ou usuário (maria.souza) com ponto em chave de
		// configuração (KAFKA_TOPIC=, kafka.topic=, user:): nome, não acesso a atributo nem domínio
		topic := (ent == "fila" || ent == "usuario") && (c == ':' || uppercaseOnly(key) || punctuated) && dottedName(val, ent == "usuario")
		if i := strings.IndexByte(val, '.'); i >= 0 && ent != "database" && ent != "schema" && ent != "tabela" &&
			!topic && !internalSuffix(strings.ToLower(val)) {
			return
		}
		if i := strings.IndexByte(val, '.'); i >= 0 && !topic && (c != ':' || codeReceivers[strings.ToLower(val[:i])]) {
			return
		}
		if c != ':' {
			if lastConv := lastPiece(key); strings.EqualFold(val, lastConv) {
				return
			}
		}
	}
	markValue(s, va, vb, ent, "chave-valor", exact, add)
}

// yamlList: os itens "- x" logo abaixo de "chave:" (recuados) são do tipo da chave.
func yamlList(s string, v int, ent string, add func(ObjFinding)) {
	i := v
	for n := 0; n < 200; n++ {
		nl := strings.IndexByte(s[i:], '\n')
		if nl < 0 {
			return
		}
		i += nl + 1
		a := i
		for a < len(s) && (s[a] == ' ' || s[a] == '\t') {
			a++
		}
		if a == i || a+1 >= len(s) || s[a] != '-' || s[a+1] != ' ' {
			return // fim da lista (ou item sem recuo)
		}
		a += 2
		b := a
		for b < len(s) && s[b] != '\n' && s[b] != '\r' && s[b] != '#' {
			b++
		}
		for b > a && (s[b-1] == ' ' || s[b-1] == '\t') {
			b--
		}
		if b-a >= 2 && (s[a] == '"' || s[a] == '\'') && s[b-1] == s[a] {
			a, b = a+1, b-1
		}
		if v := s[a:b]; strings.ContainsAny(v, ":{}[] ") {
			return // item que é mapa ou frase: não é lista de nomes
		}
		markValue(s, a, b, ent, "chave-valor", true, add)
	}
}

// inlineList: [a, b] ou ["a", "b"] numa linha só: cada item é do tipo da chave.
func inlineList(s string, v int, ent string, add func(ObjFinding)) {
	f := strings.IndexByte(s[v:min(len(s), v+2000)], ']')
	if f < 0 || strings.ContainsAny(s[v+1:v+f], "\n[{(") {
		return
	}
	i := v + 1
	for i < v+f {
		for i < v+f && (s[i] == ' ' || s[i] == ',') {
			i++
		}
		a, b := i, i
		if i < v+f && (s[i] == '"' || s[i] == '\'') {
			e := strings.IndexByte(s[i+1:v+f], s[i])
			if e < 0 {
				return
			}
			a, b = i+1, i+1+e
			i = b + 1
		} else {
			for b < v+f && s[b] != ',' {
				b++
			}
			i = b
			for b > a && s[b-1] == ' ' {
				b--
			}
		}
		if b > a {
			markValue(s, a, b, ent, "chave-valor", true, add)
		}
	}
}

// pairsInLine: quantos "chave=valor" (sem espaço em volta do "=") separados por espaço há na
// linha de s[p] (log em chave=valor, "db=x schema=y tabela=z"). Line com "(" ou ", " é código
// (argumentos nomeados) e não conta.
func pairsInLine(s string, p int) int {
	l := s[lineStartJ(s, p):lineEndJ(s, p)]
	if strings.Contains(l, "(") || strings.Contains(l, ", ") {
		return 0
	}
	n := 0
	for _, t := range strings.Fields(l) {
		if e := strings.IndexByte(t, '='); e > 0 && e+1 < len(t) && letterD(t[0]) && t[e+1] != '=' {
			n++
		}
	}
	return n
}

// enclosingTag: a entidade que a tag XML aberta antes de s[i] indica (<column name="x">,
// <createTable tableName=...>): a palavra de tipo no nome da tag.
func enclosingTag(s string, i int) string {
	a := max(0, i-300)
	lt := strings.LastIndexByte(s[a:i], '<')
	if lt < 0 || strings.IndexByte(s[a+lt:i], '>') >= 0 {
		return ""
	}
	t := a + lt + 1
	e := t
	for e < i && (isAlnum(s[e]) || s[e] == '_' || s[e] == '-' || s[e] == ':') {
		e++
	}
	if e == t {
		return ""
	}
	tag := s[t:e]
	if k := strings.LastIndexByte(tag, ':'); k >= 0 {
		tag = tag[k+1:]
	}
	return entContainer(tag)
}

func lastPiece(k string) string {
	var ps [16][2]int
	n, _ := keyPieces(k, &ps)
	if n == 0 {
		return k
	}
	return k[ps[n-1][0]:ps[n-1][1]]
}

// resourceValue: o valor pode ser nome de recurso (e não número, caminho, expressão, palavra
// pública, domínio público ou nome de arquivo).
func resourceValue(v, ent string) bool {
	if len(v) < 2 || len(v) > 200 {
		return false
	}
	// nomes de SQL aceitam "$" no meio (V$SESSION, ped$hist); eles e o usuário aceitam letras
	// de qualquer escrita ("Продажи", "客户数据", "josé"): para um cliente russo ou chinês o
	// nome real é nessa escrita. Host, bucket, fila e namespace não (o sistema só aceita ASCII).
	sqlEnt := entSQL[ent] && ent != "servidor"
	unicodeOK := sqlEnt || ent == "usuario"
	c0 := v[0]
	if !(isAlnum(c0) || c0 == '_' || unicodeOK && utf8Letter(v, 0) > 0) {
		return false // caminho, $VAR, ${...}, {{...}}, %s, <x>, @x
	}
	numOnly := true
	for i := 0; i < len(v); i++ {
		c := v[i]
		if unicodeOK && c >= 0x80 {
			if n := utf8Letter(v, i); n > 0 {
				i += n - 1
				numOnly = false
				continue
			}
			return false
		}
		if sqlEnt && c == '$' && i+1 < len(v) && v[i+1] != '{' && v[i+1] != '(' {
			continue
		}
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '/' || c == '@' || c == '$' || c == '%' || c == '{' || c == '}' ||
			c == '<' || c == '>' || c == '(' || c == ')' || c == '[' || c == ']' || c == '*' || c == '?' || c == '!' ||
			c == '|' || c == '^' || c == '~' || c == '`' || c == '\'' || c == '"' || c == ',' || c == ';' || c == '&' ||
			c == '=' || c == '+' || c >= 0x80:
			return false
		case (c == ':' || c == '\\') && ent != "servidor":
			return false
		}
		if !(c >= '0' && c <= '9' || c == '.' || c == ':' || c == '-' || c == '_') {
			numOnly = false
		}
	}
	if numOnly && !(ent == "conta_nuvem" && len(v) == 12 && allDigits(v)) || len(v) > 2 && (v[0] == '0' && (v[1] == 'x' || v[1] == 'X')) {
		return false // número não é nome (a conta da AWS, 12 dígitos, é)
	}
	h := strings.ToLower(v)
	if i := strings.IndexAny(h, ":\\"); i >= 0 {
		h = h[:i]
	}
	if h == "" || publicDev(h) || ent != "conta_nuvem" && reVersionOrHash.MatchString(h) {
		return false
	}
	if d := strings.LastIndexByte(h, '.'); d >= 0 && ent != "database" && ent != "schema" && ent != "tabela" {
		if publicDomain(h) || fileExtensions[h[d+1:]] {
			return false
		}
		// "http.server", "xmlrpc.server": caminho de módulo, não nome de máquina
		if ent == "servidor" && !dashOrDigit(h) && !internalSuffix(h) {
			return false
		}
	}
	return true
}

// markValue entrega o valor s[a:b] com a entidade da chave.
func markValue(s string, a, b int, ent, rule string, strong bool, add func(ObjFinding)) {
	// 12 dígitos numa chave de conta ou de catálogo: a conta da AWS (o catálogo do Glue é a conta)
	if (ent == "conta_nuvem" || ent == "database") && b-a == 12 && allDigits(s[a:b]) {
		add(ObjFinding{a, b, "conta_nuvem", rule, strong})
		return
	}
	// conta com região depois (Snowflake: xy12345.us-east-1, ABC12345.ap-south-1.aws): só a conta
	if ent == "conta_nuvem" {
		if d := strings.IndexByte(s[a:b], '.'); d > 0 && accountWithRegion(s[a+d+1:b]) {
			b = a + d
		}
	}
	switch ent {
	case "servidor": // lista "h1:9092,h2:9092"
		for x := a; x < b; {
			y := strings.IndexByte(s[x:b], ',')
			if y < 0 {
				y = b
			} else {
				y += x
			}
			if resourceValue(s[x:y], ent) {
				addHost(s, x, y, rule, strong, add)
			}
			x = y + 1
		}
	case "database", "schema", "tabela":
		if !resourceValue(s[a:b], ent) {
			return
		}
		ps := strings.Split(s[a:b], ".")
		ents := entQual(len(ps), ent)
		for k, p := range ps {
			if p != "" && !publicDev(p) && reSimpleIdent.MatchString(p) {
				add(ObjFinding{a, a + len(p), ents[k], rule, strong})
			}
			a += len(p) + 1
		}
	default:
		if resourceValue(s[a:b], ent) {
			add(ObjFinding{a, b, ent, rule, strong})
		}
	}
}

// accountWithRegion: o que vem depois do primeiro ponto de uma conta é região e nuvem
// ("us-east-1", "sa-east-1.aws", "east-us-2.azure", "privatelink").
func accountWithRegion(r string) bool {
	for _, p := range strings.Split(strings.ToLower(r), ".") {
		switch {
		case reRegion.MatchString(p), p == "aws", p == "gcp", p == "azure", p == "privatelink":
		case strings.Count(p, "-") >= 1 && strings.Trim(p, "abcdefghijklmnopqrstuvwxyz0123456789-") == "" && isDig(p[len(p)-1]):
			// forma de região do Snowflake/Azure: east-us-2, us-central1, north-europe-1
		default:
			return false
		}
	}
	return r != ""
}

// addHost: nome de servidor sem a porta (e "srv\INST": nome e instância).
func addHost(s string, a, b int, rule string, strong bool, add func(ObjFinding)) {
	v := s[a:b]
	// nome DNS de serviço do Kubernetes: fica com o leitor de Kubernetes (serviço e namespace)
	if l := strings.ToLower(v); strings.Contains(l, ".svc.cluster.local") || strings.HasSuffix(l, ".svc") {
		return
	}
	if i := strings.IndexByte(v, ':'); i >= 0 {
		if p := strings.ToLower(v[:i]); p == "tcp" || p == "np" || p == "lpc" || p == "udp" { // SQL Server: tcp:srv,1433
			a += i + 1
			v = v[i+1:]
			if i = strings.IndexByte(v, ':'); i < 0 {
				i = len(v)
			}
		}
		v = v[:i]
	}
	if i := strings.IndexByte(v, '\\'); i >= 0 {
		if inst := v[i+1:]; inst != "" && reSimpleIdent.MatchString(inst) && !publicDev(inst) {
			add(ObjFinding{a + i + 1, a + len(v), "servidor", rule, strong})
		}
		v = v[:i]
	}
	v = strings.TrimRight(v, ".")
	if len(v) < 2 || publicDev(v) || isSimpleIPv4(v) {
		return
	}
	add(ObjFinding{a, a + len(v), "servidor", rule, strong})
}

// isSimpleIPv4: só dígitos e pontos (o IP fica com o detector de IP).
func isSimpleIPv4(v string) bool {
	for i := 0; i < len(v); i++ {
		if !(v[i] >= '0' && v[i] <= '9' || v[i] == '.') {
			return false
		}
	}
	return true
}

// <host>valor</host> (sem atributos na tag de abertura; atributo host="x" fica com kvIn)
func findXMLValue(s string, add func(ObjFinding)) {
	for i := strings.Index(s, "</"); i >= 0; {
		a := i + 2
		b := a
		for b < len(s) && b-a < 64 && (isAlnum(s[b]) || s[b] == '_' || s[b] == '-' || s[b] == '.' || s[b] == ':') {
			b++
		}
		if b < len(s) && s[b] == '>' && b > a {
			name := s[a:b]
			local := name
			if k := strings.LastIndexByte(name, ':'); k >= 0 {
				local = name[k+1:]
			}
			if ent, exact := entKey(local); ent != "" {
				lim := max(0, i-256)
				if g := strings.LastIndexByte(s[lim:i], '>'); g >= 0 {
					g += lim
					o := g - len(name) - 1
					if o >= 0 && s[o] == '<' && s[o+1:g] == name {
						va, vb := g+1, i
						for va < vb && (s[va] == ' ' || s[va] == '\t') {
							va++
						}
						for vb > va && (s[vb-1] == ' ' || s[vb-1] == '\t') {
							vb--
						}
						if vb > va && !strings.EqualFold(s[va:vb], local) {
							markValue(s, va, vb, ent, "chave-valor", exact, add)
						}
					}
				}
			}
		}
		j := strings.Index(s[i+2:], "</")
		if j < 0 {
			break
		}
		i += 2 + j
	}
}

// --host valor (o "--host=valor" fica com kvIn)
func findLongOptions(s string, add func(ObjFinding)) {
	for i := strings.Index(s, "--"); i >= 0; {
		if (i == 0 || strings.IndexByte(" \t\n\"'(`", s[i-1]) >= 0) && i+2 < len(s) && letterD(s[i+2]) {
			a := i + 2
			b := a
			for b < len(s) && b-a < 64 && (isAlnum(s[b]) || s[b] == '-' || s[b] == '_' || s[b] == '.') {
				b++
			}
			if b+1 < len(s) && s[b] == ' ' && s[b+1] != '-' {
				if ent, exact := entKey(s[a:b]); ent != "" {
					va, vb := b+1, b+1
					quotes := false
					if q := s[va]; q == '"' || q == '\'' {
						if e := strings.IndexByte(s[va+1:min(len(s), va+257)], q); e > 0 {
							va, vb, quotes = va+1, va+1+e, true
						}
					} else {
						for vb < len(s) && !kvValueEnd(s[vb]) {
							vb++
						}
					}
					if vb > va {
						v := s[va:vb]
						if (quotes || dashOrDigit(v) || identifierShape(v)) && !uppercaseOnly(v) && !typeWords[strings.ToLower(v)] {
							markValue(s, va, vb, ent, "chave-valor", exact, add)
						}
					}
				}
			}
		}
		j := strings.Index(s[i+2:], "--")
		if j < 0 {
			break
		}
		i += 2 + j
	}
}

// ---------------------------------------------------------------------------------------
// "Key Valor" separados por espaço, uma por linha (ssh config, arquivos de configuração no
// estilo Apache): vale num bloco de 2+ linhas consecutivas nessa forma, com a chave sendo uma
// palavra de tipo. "IP nome [nome...]" (/etc/hosts): os nomes são servidores.

func findSpaceKey(s string, add func(ObjFinding)) {
	ls := wrapLines(s)
	shape := make([]bool, len(ls))
	toks := make([][]cell, len(ls))
	for k, l := range ls {
		ts := lineTokens(s, l[0], l[1])
		toks[k] = ts
		if len(ts) >= 2 && !strings.HasPrefix(s[ts[0].a:ts[0].b], "#") {
			if ip := s[ts[0].a:ts[0].b]; isIPv4(ip) || strings.Count(ip, ":") >= 2 && strings.Trim(ip, "0123456789abcdefABCDEF:") == "" {
				ipNames(s, ts[1:], add)
				continue
			}
		}
		shape[k] = len(ts) == 2 && reSpaceKey.MatchString(s[ts[0].a:ts[0].b])
	}
	for k := range ls {
		if !shape[k] || !(k > 0 && shape[k-1] || k+1 < len(ls) && shape[k+1]) {
			continue
		}
		ch, v := toks[k][0], toks[k][1]
		ent, strong := entKey(s[ch.a:ch.b])
		if ent == "" || !strong {
			continue
		}
		// o valor não é tipo de dado nem tipo qualificado de código ("Name uint64", "B strings.Builder")
		if val := s[v.a:v.b]; (identifierShape(val) || dashOrDigit(val)) && !isDataType(val) && !reQualifiedType.MatchString(val) {
			markValue(s, v.a, v.b, ent, "chave-espaço", true, add)
		}
	}
}

var reQualifiedType = regexp.MustCompile(`[*\[\]()]|\.[A-Z]`)

var reSpaceKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*$`)

// ipNames: os nomes depois do IP numa linha de hosts (todos com forma de nome de servidor;
// qualquer outra coisa na linha, como em log de acesso, desfaz).
func ipNames(s string, ts []cell, add func(ObjFinding)) {
	if len(ts) > 8 {
		return
	}
	for _, t := range ts {
		if v := s[t.a:t.b]; strings.HasPrefix(v, "#") {
			ts = ts[:0:0]
			break
		} else if !reHostName.MatchString(v) {
			return
		}
	}
	for _, t := range ts {
		if v := s[t.a:t.b]; v != "localhost" && !strings.HasPrefix(v, "localhost.") && !strings.HasPrefix(v, "ip6-") {
			addHost(s, t.a, t.b, "hosts", true, add)
		}
	}
}

var reHostName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*[A-Za-z0-9]$`)
