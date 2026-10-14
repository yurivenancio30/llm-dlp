package mask

import (
	"regexp"
	"strings"
)

// Leitores de conexão (ver docs/pt-BR/structures.md, seção Strings de conexão e endereços): strings
// de conexão ODBC/ADO.NET/JDBC, DSN do libpq, URIs de banco, tnsnames.ora, URNs do DataHub e
// as referências do dbt e do Airflow. Só padrões estáveis (especificação de cada formato).

// chaves de conexão (sem diferença de caixa, sem espaços) -> entidade
var connectionKeys = map[string]string{
	"server": "servidor", "datasource": "servidor", "address": "servidor", "addr": "servidor", "networkaddress": "servidor",
	"host": "servidor", "hostname": "servidor", "hostaddr": "servidor", "failoverpartner": "servidor", "servername": "servidor",
	"account":  "servidor",
	"database": "database", "databasename": "database", "initialcatalog": "database", "dbname": "database", "db": "database",
	"catalog": "database", "project": "database", "projectid": "database",
	"schema": "schema", "currentschema": "schema", "defaultdataset": "schema", "dataset": "schema",
	"userid": "usuario", "uid": "usuario", "user": "usuario", "username": "usuario",
	"warehouse": "servico", "role": "usuario", "servicename": "database", "sid": "database",
}

var (
	reDatabaseURI = regexp.MustCompile(`(?i)\b(?:jdbc:[a-z0-9]+(?::[a-z]+)*:|(?:postgres(?:ql)?|mysql|mariadb|mssql|sqlserver|oracle|redshift|snowflake|mongodb(?:\+srv)?|clickhouse|db2|teradata|presto|trino|hive|cockroachdb|sqlite)(?:\+[a-z0-9_]+)?:)//(?:([^\s:/@;?"']+)(?::[^\s@/"']*)?@)?([A-Za-z0-9_.\-,:\\]+)(?:/([\pL_][\pL\w$\-]*))?`)
	reJDBCOracle  = regexp.MustCompile(`(?i)\bjdbc:oracle:thin:(?:[^\s@/"']+@)?@?(?://)?([A-Za-z0-9_.\-]+)(?::\d+)?[:/]([A-Za-z_][\w.$\-]*)`)
	reTNS         = regexp.MustCompile(`(?i)\(\s*(HOST|SERVICE_NAME|SID)\s*=\s*([A-Za-z0-9_.\-]+)\s*\)`)
	reURN         = regexp.MustCompile(`urn:li:dataset:\(urn:li:dataPlatform:[\w-]+,([^,()\n]+),[A-Z]+\)`)
	reURNUser     = regexp.MustCompile(`urn:li:corpuser:([\w.\-@]+)`)
	reURNFlow     = regexp.MustCompile(`urn:li:dataFlow:\([\w-]+,([\w.\-]+),[A-Za-z]+\)(?:,([\w.\-]+)\))?`)
	// outras URNs com nome do cliente (https://docs.datahub.com/docs/what/urn): instância da
	// plataforma, grupo (time), gráfico e painel (id na plataforma) e campo de dataset (coluna)
	reURNInstance  = regexp.MustCompile(`urn:li:dataPlatformInstance:\(urn:li:dataPlatform:[\w-]+,([\w.\-]+)\)`)
	reURNGroup     = regexp.MustCompile(`urn:li:corpGroup:([\w.\-@]+)`)
	reURNDashboard = regexp.MustCompile(`urn:li:(?:chart|dashboard):\([\w-]+,([\w.\-]+)\)`)
	reURNField     = regexp.MustCompile(`urn:li:schemaField:\(urn:li:dataset:\([^()]*\),([\w.\-\[\]=]+)\)`)
	reURNApp       = regexp.MustCompile(`urn:li:application:([\w.\-]+)`) // a aplicação (sistema) do cliente
	reDbtRef       = regexp.MustCompile(`\{\{[^}]*?\bref\(\s*['"]([\w.\-]+)['"](?:\s*,\s*['"]([\w.\-]+)['"])?\s*\)`)
	reDbtSource    = regexp.MustCompile(`\{\{[^}]*?\bsource\(\s*['"]([\w.\-]+)['"]\s*,\s*['"]([\w.\-]+)['"]\s*\)`)
	reConnID       = regexp.MustCompile(`\b\w*conn_id\s*=\s*['"]([\w.\-]+)['"]`)
)

// valores que nunca são nome de recurso
var publicConnectionValues = map[string]bool{"localhost": true, "true": true, "false": true, "yes": true, "no": true,
	"sspi": true, "none": true, "null": true, "default": true, "admin": true, "root": true, "sa": true, "postgres": true}

func publicConnection(v string) bool {
	return publicConnectionValues[strings.ToLower(v)] || publicSQL(v)
}

func addParts(s string, a, b int, lastPos, rule string, add func(ObjFinding)) {
	ps := strings.Split(s[a:b], ".")
	ents := entQual(len(ps), lastPos)
	for k, p := range ps {
		if p != "" && !publicConnection(p) && reSimpleIdent.MatchString(p) {
			add(ObjFinding{a, a + len(p), ents[k], rule, true})
		}
		a += len(p) + 1
	}
}

// addURNParts: o nome do dataset de uma URN, com as partes preenchidas com espaços à direita
// (colunas CHAR de catálogo, como no DB2: "ABCD    .TABELA").
func addURNParts(s string, a, b int, add func(ObjFinding)) {
	ps := strings.Split(s[a:b], ".")
	ents := entQual(len(ps), "tabela")
	for k, p := range ps {
		q := strings.TrimRight(p, " ")
		if q != "" && !strings.Contains(q, " ") && !publicConnection(q) && reSimpleIdent.MatchString(q) {
			add(ObjFinding{a, a + len(q), ents[k], "urn", true})
		}
		a += len(p) + 1
	}
}

// addServer: servidor com instância e porta ("srv01\INST,1433", "tcp:srv01,1433"):
// mascara o nome e a instância, não a porta.
func addServer(s string, a, b int, rule string, add func(ObjFinding)) {
	v := s[a:b]
	if strings.HasPrefix(strings.ToLower(v), "tcp:") {
		a += 4
		v = v[4:]
	}
	for _, c := range []string{",", ":"} {
		if i := strings.Index(v, c); i >= 0 {
			v = v[:i]
		}
	}
	if i := strings.IndexByte(v, '\\'); i >= 0 {
		if inst := v[i+1:]; inst != "" && !publicConnection(inst) {
			add(ObjFinding{a + i + 1, a + len(v), "servidor", rule, true})
		}
		v = v[:i]
	}
	if v == "" || publicConnection(v) || v == "." || strings.HasPrefix(v, "(") {
		return
	}
	// nome DNS de serviço do Kubernetes: fica com o leitor de Kubernetes (serviço e namespace)
	if l := strings.ToLower(v); strings.HasSuffix(l, ".svc.cluster.local") || strings.HasSuffix(l, ".svc") {
		return
	}
	add(ObjFinding{a, a + len(v), "servidor", rule, true})
}

func findConnections(s string, add func(ObjFinding)) {
	// string de conexão: pelo menos dois pares, um deles com chave de conexão (ou um par que é
	// propriedade de uma URI de banco)
	if n := strings.Count(s, "="); n >= 2 || n == 1 && strings.Contains(s, "://") {
		pairs := connectionPairs(s)
		// agrupa pares próximos (mesma string): basta que haja 2 chaves conhecidas a menos de 300 bytes
		for k, m := range pairs {
			key := strings.ToLower(strings.ReplaceAll(s[m[2]:m[3]], " ", ""))
			ent, ok := connectionKeys[key]
			if !ok {
				continue
			}
			neighbor := false
			for _, n := range pairs[max(0, k-4):min(len(pairs), k+5)] {
				if n[0] == m[0] {
					continue
				}
				if _, ok2 := connectionKeys[strings.ToLower(strings.ReplaceAll(s[n[2]:n[3]], " ", ""))]; !ok2 || abs(n[0]-m[0]) >= 300 {
					continue
				}
				between := s[min(m[0], n[0]):max(m[1], n[1])]
				// a mesma string de conexão: mesma linha, e pares ligados por ; ou & (ODBC, ADO.NET,
				// JDBC, URL) ou escritos sem espaço em volta do "=" (DSN do libpq)
				if !strings.Contains(between, "\n") && (strings.ContainsAny(between, ";&") || noSpaceEquals(s, m) && noSpaceEquals(s, n)) {
					neighbor = true
					break
				}
			}
			// o par é uma propriedade de uma URI de banco ("jdbc:sqlserver://h:1433;databaseName=x",
			// "postgresql://h/db?currentSchema=x"): a URI conta como o outro par
			if !neighbor && !uriProperty(s, m[2]) {
				continue
			}
			a, b := m[4], m[5]
			if s[a] == '{' { // ODBC: {valor}
				a, b = a+1, b-1
			}
			if ent == "servidor" {
				addServer(s, a, b, "conexão", add)
			} else if !publicConnection(s[a:b]) && reSimpleIdent.MatchString(strings.ReplaceAll(s[a:b], ".", "_")) {
				addParts(s, a, b, ent, "conexão", add)
			}
		}
	}
	// URIs de banco: em vez de rodar a regex no texto inteiro, parte de cada "://" e lê o
	// esquema para trás; a regex só roda numa janela curta a partir do esquema
	for i := strings.Index(s, "://"); i >= 0; {
		a := i
		for a > 0 && (isAlnum(s[a-1]) || s[a-1] == '+' || s[a-1] == ':' || s[a-1] == '_') {
			a--
		}
		ini := a
		end := min(len(s), i+3+300)
		if !databaseScheme(s[ini:i]) {
			ini = end // http://, https://, ftp://...: não é URI de banco
		}
		for _, m := range reDatabaseURI.FindAllStringSubmatchIndex(s[ini:end], 1) {
			for k := range m {
				if m[k] >= 0 {
					m[k] += ini
				}
			}
			if m[2] >= 0 && !publicConnection(s[m[2]:m[3]]) && !modelName(s[m[2]:m[3]]) {
				add(ObjFinding{m[2], m[3], "usuario", "uri", true})
			}
			// vários hosts: h1:p1,h2:p2
			a := m[4]
			for _, h := range strings.Split(s[m[4]:m[5]], ",") {
				if modelName(strings.SplitN(h, ":", 2)[0]) { // reader_connection_brakes.go
					a += len(h) + 1
					continue
				}
				addServer(s, a, a+len(h), "uri", add)
				a += len(h) + 1
			}
			if m[6] >= 0 && !publicConnection(s[m[6]:m[7]]) && !modelName(s[m[6]:m[7]]) {
				add(ObjFinding{m[6], m[7], "database", "uri", true})
			}
		}
		j := strings.Index(s[i+3:], "://")
		if j < 0 {
			break
		}
		i += 3 + j
	}
	if strings.Contains(s, "oracle:thin") {
		for _, m := range reJDBCOracle.FindAllStringSubmatchIndex(s, -1) {
			addServer(s, m[2], m[3], "uri", add)
			addParts(s, m[4], m[5], "database", "uri", add)
		}
	}
	if strings.Contains(s, "(") {
		for _, m := range reTNS.FindAllStringSubmatchIndex(s, -1) {
			if modelName(s[m[4]:m[5]]) { // reader_connection_brakes.go
				continue
			}
			if strings.EqualFold(s[m[2]:m[3]], "HOST") {
				addServer(s, m[4], m[5], "tnsnames", add)
			} else {
				addParts(s, m[4], m[5], "database", "tnsnames", add)
			}
		}
	}
	if strings.Contains(s, "urn:li:") {
		for _, m := range reURN.FindAllStringSubmatchIndex(s, -1) {
			addURNParts(s, m[2], m[3], add)
		}
		for _, m := range reURNUser.FindAllStringSubmatchIndex(s, -1) {
			add(ObjFinding{m[2], m[3], "usuario", "urn", true})
		}
		for _, m := range reURNFlow.FindAllStringSubmatchIndex(s, -1) {
			add(ObjFinding{m[2], m[3], "servico", "urn", true})
			if m[4] >= 0 {
				add(ObjFinding{m[4], m[5], "servico", "urn", true})
			}
		}
		for _, u := range []struct {
			re  *regexp.Regexp
			ent string
		}{{reURNInstance, "servidor"}, {reURNGroup, "usuario"}, {reURNDashboard, "servico"}, {reURNField, "coluna"}, {reURNApp, "servico"}} {
			for _, m := range u.re.FindAllStringSubmatchIndex(s, -1) {
				if v := s[m[2]:m[3]]; !publicDev(v) {
					add(ObjFinding{m[2], m[3], u.ent, "urn", true})
				}
			}
		}
	}
	if strings.Contains(s, "{{") {
		for _, m := range reDbtRef.FindAllStringSubmatchIndex(s, -1) {
			if m[4] >= 0 { // ref('pacote', 'modelo')
				add(ObjFinding{m[4], m[5], "tabela", "dbt", true})
			} else {
				add(ObjFinding{m[2], m[3], "tabela", "dbt", true})
			}
		}
		for _, m := range reDbtSource.FindAllStringSubmatchIndex(s, -1) {
			add(ObjFinding{m[2], m[3], "schema", "dbt", true})
			add(ObjFinding{m[4], m[5], "tabela", "dbt", true})
		}
	}
	if strings.Contains(s, "onect") || strings.Contains(s, "onnect") || strings.Contains(s, "onex") || strings.Contains(s, "ONNECT") || strings.Contains(s, "ONECT") {
		findDatabaseAddress(s, add)
	}
	if strings.Contains(s, "conn_id") {
		for _, m := range reConnID.FindAllStringSubmatchIndex(s, -1) {
			if !publicConnection(s[m[2]:m[3]]) {
				add(ObjFinding{m[2], m[3], "servico", "airflow", true})
			}
		}
	}
}

// uriProperty: a chave em s[k] vem logo depois de ; ? ou & de uma URI de banco da mesma
// linha (sem espaço nem aspas entre o "://" e a chave).
func uriProperty(s string, k int) bool {
	if k == 0 || strings.IndexByte(";?&", s[k-1]) < 0 {
		return false
	}
	ini := max(lineStartJ(s, k), k-300)
	i := strings.LastIndex(s[ini:k], "://")
	if i < 0 {
		return false
	}
	i += ini
	if strings.ContainsAny(s[i:k], " \t\"'`") {
		return false
	}
	a := i
	for a > ini && (isAlnum(s[a-1]) || s[a-1] == '+' || s[a-1] == ':' || s[a-1] == '_') {
		a--
	}
	return databaseScheme(s[a:i])
}

// connectionPairs: os pares chave=valor de s cuja chave é de conexão, no formato de índices de
// regex [ini, fim, chaveIni, chaveFim, valorIni, valorFim]. A chave pode ter até duas palavras
// ("Initial Catalog", "User ID"); o valor vai até ; & espaço aspas parênteses ou fim, ou é
// {entre chaves} (ODBC).
func connectionPairs(s string) [][]int {
	var out [][]int
	letter := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
	for i := strings.IndexByte(s, '='); i >= 0 && i < len(s); {
		if i > 0 && i+1 < len(s) && s[i+1] != '=' && s[i-1] != '=' && s[i-1] != '!' && s[i-1] != '<' && s[i-1] != '>' {
			keyEnd := i
			for keyEnd > 0 && s[keyEnd-1] == ' ' {
				keyEnd--
			}
			k := keyEnd
			for words := 0; words < 2; words++ {
				for k > 0 && letter(s[k-1]) {
					k--
				}
				if words == 0 && k > 1 && s[k-1] == ' ' && letter(s[k-2]) {
					k--
					continue
				}
				break
			}
			for k < keyEnd && s[k] == ' ' {
				k++
			}
			// a chave tem que começar no início de um par: começo, ; & ? espaço aspas ( ou o ":"
			// do prefixo de DSN do PDO (mysql:host=...;dbname=...)
			for k < keyEnd && !(k == 0 || strings.IndexByte(";&? \t\n\"'(", s[k-1]) >= 0 || pdoPrefix(s, k-1)) {
				k++
				for k < keyEnd && letter(s[k]) {
					k++
				}
				for k < keyEnd && s[k] == ' ' {
					k++
				}
			}
			if k < keyEnd {
				key := strings.ToLower(strings.ReplaceAll(s[k:keyEnd], " ", ""))
				if _, ok := connectionKeys[key]; ok {
					j := i + 1
					for j < len(s) && s[j] == ' ' {
						j++
					}
					a, b := j, j
					if j < len(s) && s[j] == '{' {
						if e := strings.IndexByte(s[j:], '}'); e > 0 {
							b = j + e + 1
						}
					} else {
						for b < len(s) && strings.IndexByte(";& \t\n\r\"'(){}", s[b]) < 0 {
							b++
						}
					}
					if b > a {
						out = append(out, []int{max(0, k-1), b, k, keyEnd, a, b})
					}
				}
			}
		}
		n := strings.IndexByte(s[i+1:], '=')
		if n < 0 {
			break
		}
		i += n + 1
	}
	return out
}

// databaseScheme: "jdbc:...", "postgresql", "mysql+pymysql", "mongodb+srv"... (o que vem antes de "://")
func databaseScheme(e string) bool {
	e = strings.ToLower(e)
	if strings.HasPrefix(e, "jdbc:") {
		return true
	}
	if i := strings.IndexByte(e, '+'); i >= 0 {
		e = e[:i]
	}
	switch e {
	case "postgres", "postgresql", "mysql", "mariadb", "mssql", "sqlserver", "oracle", "redshift", "snowflake",
		"mongodb", "clickhouse", "db2", "teradata", "presto", "trino", "hive", "cockroachdb", "sqlite":
		return true
	}
	return false
}

// pdoPrefix: s[i] é o ":" de um DSN do PDO (mysql:, pgsql:, sqlsrv:, oci:, odbc:, dblib:,
// firebird:, ibm:, informix:) (https://www.php.net/manual/pdo.drivers.php)
func pdoPrefix(s string, i int) bool {
	if i < 2 || s[i] != ':' {
		return false
	}
	a := i
	for a > 0 && s[a-1] >= 'a' && s[a-1] <= 'z' {
		a--
	}
	switch s[a:i] {
	case "mysql", "pgsql", "sqlsrv", "oci", "odbc", "dblib", "firebird", "ibm", "informix", "cubrid":
		return a == 0 || !isAlnum(s[a-1])
	}
	return false
}

// Endereço de banco sem esquema, host:porta/banco (a forma do EZConnect do Oracle e das
// mensagens de conexão: "conectando em srv01:5432/dwprd como svc_x", "connecting to
// srv01:5432/dwprd as svc_x"). Só numa linha que fala de conexão (conectar, connect, conexão),
// para "localhost:8080/api" de uma URL sem esquema não virar banco; "como|as <usuário>" logo
// depois é o usuário.
var (
	reDatabaseAddress = regexp.MustCompile(`(?:^|[\s"'=@(\[])([A-Za-z][\w.\-]*[A-Za-z0-9]):(\d{2,5})/([\pL_][\pL\w$\-]*)(?:\s+(?:como|as)\s+(?:(?:o\s+)?(?:usu[aá]rio|user|role)\s+)?([\pL_][\pL\w.$\-]*))?`)
	reConnectionTalk  = regexp.MustCompile(`(?i)\b(?:conect|connect|conex)`)
)

func findDatabaseAddress(s string, add func(ObjFinding)) {
	for _, m := range reDatabaseAddress.FindAllStringSubmatchIndex(s, -1) {
		if f := m[7]; f < len(s) && (isAlnum(s[f]) || s[f] == '/' || s[f] == '_') && m[8] < 0 {
			continue // caminho mais longo: host:porta/a/b é URL
		}
		a, z := lineStartJ(s, m[2]), lineEndJ(s, m[2])
		if !reConnectionTalk.MatchString(s[a:z]) {
			continue
		}
		// "connecting to localhost:8080/api": sem usuário, só vale com host ou banco com cara de nome
		if db := s[m[6]:m[7]]; m[8] < 0 && publicConnection(s[m[2]:m[3]]) && !identifierShape(db) {
			continue
		}
		addServer(s, m[2], m[3], "conexão", add)
		if db := s[m[6]:m[7]]; !publicConnection(db) {
			add(ObjFinding{m[6], m[7], "database", "conexão", true})
		}
		if m[8] >= 0 {
			if u := s[m[8]:m[9]]; resourceValue(u, "usuario") && !publicConnection(u) {
				add(ObjFinding{m[8], m[9], "usuario", "conexão", true})
			}
		}
	}
}

// DSN do driver MySQL do Go: usuario[:senha]@protocolo(endereço)/banco[?parâmetros]
// (https://github.com/go-sql-driver/mysql#dsn-data-source-name)
var reDSNGo = regexp.MustCompile(`(?:^|[\s"'\x60=(,])([\pL_][\pL\w.\-]*)(?::[^@\s"'\x60]*)?@(?:tcp6?|unix)\(([^)\s"'\x60]+)\)/([\pL_][\pL\w$\-]*)`)

// PDO do Oracle: oci:dbname=//host:porta/serviço
var reDSNOci = regexp.MustCompile(`\boci:dbname=//([A-Za-z0-9_.\-]+)(?::\d+)?/([A-Za-z_][\w.$\-]*)`)

func findDSN(s string, add func(ObjFinding)) {
	if strings.Contains(s, "@tcp") || strings.Contains(s, "@unix(") {
		for _, m := range reDSNGo.FindAllStringSubmatchIndex(s, -1) {
			if !publicConnection(s[m[2]:m[3]]) {
				add(ObjFinding{m[2], m[3], "usuario", "dsn", true})
			}
			if s[m[4]] != '/' { // unix(/caminho/socket) não tem host
				addServer(s, m[4], m[5], "dsn", add)
			}
			if !publicConnection(s[m[6]:m[7]]) {
				add(ObjFinding{m[6], m[7], "database", "dsn", true})
			}
		}
	}
	if strings.Contains(s, "oci:dbname=//") {
		for _, m := range reDSNOci.FindAllStringSubmatchIndex(s, -1) {
			addServer(s, m[2], m[3], "dsn", add)
			addParts(s, m[4], m[5], "database", "dsn", add)
		}
	}
	// PDO: new PDO('mysql:host=...;dbname=...', 'usuario', $senha): a string logo depois do
	// DSN, no mesmo argumento seguinte, é o usuário
	for i := strings.IndexByte(s, ':'); i >= 0; {
		if u, a, ok := userAfterDSN(s, i); ok {
			add(ObjFinding{a, a + len(u), "usuario", "dsn", true})
		}
		j := strings.IndexByte(s[i+1:], ':')
		if j < 0 {
			break
		}
		i += 1 + j
	}
}

func userAfterDSN(s string, i int) (string, int, bool) {
	if !pdoPrefix(s, i) {
		return "", 0, false
	}
	a := i
	for a > 0 && s[a-1] >= 'a' && s[a-1] <= 'z' {
		a--
	}
	if a == 0 || s[a-1] != '\'' && s[a-1] != '"' {
		return "", 0, false
	}
	e := strings.IndexByte(s[i:min(len(s), i+512)], s[a-1])
	if e < 0 {
		return "", 0, false
	}
	p := i + e + 1
	for p < len(s) && (s[p] == ' ' || s[p] == '\t') {
		p++
	}
	if p >= len(s) || s[p] != ',' {
		return "", 0, false
	}
	for p++; p < len(s) && (s[p] == ' ' || s[p] == '\t'); p++ {
	}
	if p >= len(s) || s[p] != '\'' && s[p] != '"' {
		return "", 0, false
	}
	f := strings.IndexByte(s[p+1:min(len(s), p+130)], s[p])
	if f <= 0 {
		return "", 0, false
	}
	u := s[p+1 : p+1+f]
	if !resourceValue(u, "usuario") || publicConnection(u) {
		return "", 0, false
	}
	return u, p + 1, true
}

func noSpaceEquals(s string, m []int) bool {
	return m[3] < len(s) && s[m[3]] == '=' && s[m[4]-1] == '='
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
