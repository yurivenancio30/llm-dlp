package mask

import (
	"regexp"
	"strings"
)

// Leitores de conexão (ver docs/estruturas.md, seção Strings de conexão e endereços): strings
// de conexão ODBC/ADO.NET/JDBC, DSN do libpq, URIs de banco, tnsnames.ora, URNs do DataHub e
// as referências do dbt e do Airflow. Só padrões estáveis (especificação de cada formato).

// chaves de conexão (sem diferença de caixa, sem espaços) -> entidade
var chavesConexao = map[string]string{
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
	reURIBanco   = regexp.MustCompile(`(?i)\b(?:jdbc:[a-z0-9]+(?::[a-z]+)*:|(?:postgres(?:ql)?|mysql|mariadb|mssql|sqlserver|oracle|redshift|snowflake|mongodb(?:\+srv)?|clickhouse|db2|teradata|presto|trino|hive|cockroachdb|sqlite)(?:\+[a-z0-9_]+)?:)//(?:([^\s:/@;?"']+)(?::[^\s@/"']*)?@)?([A-Za-z0-9_.\-,:\\]+)(?:/([A-Za-z_][\w$\-]*))?`)
	reJDBCOracle = regexp.MustCompile(`(?i)\bjdbc:oracle:thin:(?:[^\s@/"']+@)?@?(?://)?([A-Za-z0-9_.\-]+)(?::\d+)?[:/]([A-Za-z_][\w.$\-]*)`)
	reTNS        = regexp.MustCompile(`(?i)\(\s*(HOST|SERVICE_NAME|SID)\s*=\s*([A-Za-z0-9_.\-]+)\s*\)`)
	reURN        = regexp.MustCompile(`urn:li:dataset:\(urn:li:dataPlatform:[\w-]+,([^,()\s]+),[A-Z]+\)`)
	reURNUsuario = regexp.MustCompile(`urn:li:corpuser:([\w.\-@]+)`)
	reURNFluxo   = regexp.MustCompile(`urn:li:dataFlow:\([\w-]+,([\w.\-]+),[A-Za-z]+\)(?:,([\w.\-]+)\))?`)
	reDbtRef     = regexp.MustCompile(`\{\{[^}]*?\bref\(\s*['"]([\w.\-]+)['"](?:\s*,\s*['"]([\w.\-]+)['"])?\s*\)`)
	reDbtSource  = regexp.MustCompile(`\{\{[^}]*?\bsource\(\s*['"]([\w.\-]+)['"]\s*,\s*['"]([\w.\-]+)['"]\s*\)`)
	reConnID     = regexp.MustCompile(`\b\w*conn_id\s*=\s*['"]([\w.\-]+)['"]`)
)

// valores que nunca são nome de recurso
var valoresPublicosConexao = map[string]bool{"localhost": true, "true": true, "false": true, "yes": true, "no": true,
	"sspi": true, "none": true, "null": true, "default": true, "admin": true, "root": true, "sa": true, "postgres": true}

func publicoConexao(v string) bool {
	return valoresPublicosConexao[strings.ToLower(v)] || publicoSQL(v)
}

func addPartes(s string, a, b int, ult, regra string, add func(ObjAchado)) {
	ps := strings.Split(s[a:b], ".")
	ents := entQual(len(ps), ult)
	for k, p := range ps {
		if p != "" && !publicoConexao(p) && reIdentSimples.MatchString(p) {
			add(ObjAchado{a, a + len(p), ents[k], regra, true})
		}
		a += len(p) + 1
	}
}

// addServidor: servidor com instância e porta ("srv01\INST,1433", "tcp:srv01,1433"):
// mascara o nome e a instância, não a porta.
func addServidor(s string, a, b int, regra string, add func(ObjAchado)) {
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
		if inst := v[i+1:]; inst != "" && !publicoConexao(inst) {
			add(ObjAchado{a + i + 1, a + len(v), "servidor", regra, true})
		}
		v = v[:i]
	}
	if v == "" || publicoConexao(v) || v == "." || strings.HasPrefix(v, "(") {
		return
	}
	// nome DNS de serviço do Kubernetes: fica com o leitor de Kubernetes (serviço e namespace)
	if l := strings.ToLower(v); strings.HasSuffix(l, ".svc.cluster.local") || strings.HasSuffix(l, ".svc") {
		return
	}
	add(ObjAchado{a, a + len(v), "servidor", regra, true})
}

func acharConexoes(s string, add func(ObjAchado)) {
	// string de conexão: pelo menos dois pares, um deles com chave de conexão
	if strings.Count(s, "=") >= 2 {
		pares := paresConexao(s)
		// agrupa pares próximos (mesma string): basta que haja 2 chaves conhecidas a menos de 300 bytes
		for k, m := range pares {
			chave := strings.ToLower(strings.ReplaceAll(s[m[2]:m[3]], " ", ""))
			ent, ok := chavesConexao[chave]
			if !ok {
				continue
			}
			vizinho := false
			for _, n := range pares[max(0, k-4):min(len(pares), k+5)] {
				if n[0] == m[0] {
					continue
				}
				if _, ok2 := chavesConexao[strings.ToLower(strings.ReplaceAll(s[n[2]:n[3]], " ", ""))]; !ok2 || abs(n[0]-m[0]) >= 300 {
					continue
				}
				entre := s[min(m[0], n[0]):max(m[1], n[1])]
				// a mesma string de conexão: mesma linha, e pares ligados por ; ou & (ODBC, ADO.NET,
				// JDBC, URL) ou escritos sem espaço em volta do "=" (DSN do libpq)
				if !strings.Contains(entre, "\n") && (strings.ContainsAny(entre, ";&") || semEspacoIgual(s, m) && semEspacoIgual(s, n)) {
					vizinho = true
					break
				}
			}
			if !vizinho {
				continue
			}
			a, b := m[4], m[5]
			if s[a] == '{' { // ODBC: {valor}
				a, b = a+1, b-1
			}
			if ent == "servidor" {
				addServidor(s, a, b, "conexão", add)
			} else if !publicoConexao(s[a:b]) && reIdentSimples.MatchString(strings.ReplaceAll(s[a:b], ".", "_")) {
				addPartes(s, a, b, ent, "conexão", add)
			}
		}
	}
	if strings.Contains(s, "//") {
		for _, m := range reURIBanco.FindAllStringSubmatchIndex(s, -1) {
			if m[2] >= 0 && !publicoConexao(s[m[2]:m[3]]) {
				add(ObjAchado{m[2], m[3], "usuario", "uri", true})
			}
			// vários hosts: h1:p1,h2:p2
			a := m[4]
			for _, h := range strings.Split(s[m[4]:m[5]], ",") {
				addServidor(s, a, a+len(h), "uri", add)
				a += len(h) + 1
			}
			if m[6] >= 0 && !publicoConexao(s[m[6]:m[7]]) {
				add(ObjAchado{m[6], m[7], "database", "uri", true})
			}
		}
	}
	for _, m := range reJDBCOracle.FindAllStringSubmatchIndex(s, -1) {
		addServidor(s, m[2], m[3], "uri", add)
		addPartes(s, m[4], m[5], "database", "uri", add)
	}
	if strings.Contains(s, "(") {
		for _, m := range reTNS.FindAllStringSubmatchIndex(s, -1) {
			if strings.EqualFold(s[m[2]:m[3]], "HOST") {
				addServidor(s, m[4], m[5], "tnsnames", add)
			} else {
				addPartes(s, m[4], m[5], "database", "tnsnames", add)
			}
		}
	}
	if strings.Contains(s, "urn:li:") {
		for _, m := range reURN.FindAllStringSubmatchIndex(s, -1) {
			addPartes(s, m[2], m[3], "tabela", "urn", add)
		}
		for _, m := range reURNUsuario.FindAllStringSubmatchIndex(s, -1) {
			add(ObjAchado{m[2], m[3], "usuario", "urn", true})
		}
		for _, m := range reURNFluxo.FindAllStringSubmatchIndex(s, -1) {
			add(ObjAchado{m[2], m[3], "servico", "urn", true})
			if m[4] >= 0 {
				add(ObjAchado{m[4], m[5], "servico", "urn", true})
			}
		}
	}
	if strings.Contains(s, "{{") {
		for _, m := range reDbtRef.FindAllStringSubmatchIndex(s, -1) {
			if m[4] >= 0 { // ref('pacote', 'modelo')
				add(ObjAchado{m[4], m[5], "tabela", "dbt", true})
			} else {
				add(ObjAchado{m[2], m[3], "tabela", "dbt", true})
			}
		}
		for _, m := range reDbtSource.FindAllStringSubmatchIndex(s, -1) {
			add(ObjAchado{m[2], m[3], "schema", "dbt", true})
			add(ObjAchado{m[4], m[5], "tabela", "dbt", true})
		}
	}
	if strings.Contains(s, "conn_id") {
		for _, m := range reConnID.FindAllStringSubmatchIndex(s, -1) {
			if !publicoConexao(s[m[2]:m[3]]) {
				add(ObjAchado{m[2], m[3], "servico", "airflow", true})
			}
		}
	}
}

// paresConexao: os pares chave=valor de s cuja chave é de conexão, no formato de índices de
// regex [ini, fim, chaveIni, chaveFim, valorIni, valorFim]. A chave pode ter até duas palavras
// ("Initial Catalog", "User ID"); o valor vai até ; & espaço aspas parênteses ou fim, ou é
// {entre chaves} (ODBC).
func paresConexao(s string) [][]int {
	var out [][]int
	letra := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
	for i := strings.IndexByte(s, '='); i >= 0 && i < len(s); {
		if i > 0 && i+1 < len(s) && s[i+1] != '=' && s[i-1] != '=' && s[i-1] != '!' && s[i-1] != '<' && s[i-1] != '>' {
			fimChave := i
			for fimChave > 0 && s[fimChave-1] == ' ' {
				fimChave--
			}
			k := fimChave
			for palavras := 0; palavras < 2; palavras++ {
				for k > 0 && letra(s[k-1]) {
					k--
				}
				if palavras == 0 && k > 1 && s[k-1] == ' ' && letra(s[k-2]) {
					k--
					continue
				}
				break
			}
			for k < fimChave && s[k] == ' ' {
				k++
			}
			// a chave tem que começar no início de um par: começo, ; & ? espaço aspas ( ou o ":"
			// do prefixo de DSN do PDO (mysql:host=...;dbname=...)
			for k < fimChave && !(k == 0 || strings.IndexByte(";&? \t\n\"'(", s[k-1]) >= 0 || prefixoPDO(s, k-1)) {
				k++
				for k < fimChave && letra(s[k]) {
					k++
				}
				for k < fimChave && s[k] == ' ' {
					k++
				}
			}
			if k < fimChave {
				chave := strings.ToLower(strings.ReplaceAll(s[k:fimChave], " ", ""))
				if _, ok := chavesConexao[chave]; ok {
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
						out = append(out, []int{max(0, k-1), b, k, fimChave, a, b})
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

// prefixoPDO: s[i] é o ":" de um DSN do PDO (mysql:, pgsql:, sqlsrv:, oci:, odbc:, dblib:,
// firebird:, ibm:, informix:) (https://www.php.net/manual/pdo.drivers.php)
func prefixoPDO(s string, i int) bool {
	if i < 2 || s[i] != ':' {
		return false
	}
	a := i
	for a > 0 && s[a-1] >= 'a' && s[a-1] <= 'z' {
		a--
	}
	switch s[a:i] {
	case "mysql", "pgsql", "sqlsrv", "oci", "odbc", "dblib", "firebird", "ibm", "informix", "cubrid":
		return a == 0 || !ehAlnum(s[a-1])
	}
	return false
}

// DSN do driver MySQL do Go: usuario[:senha]@protocolo(endereço)/banco[?parâmetros]
// (https://github.com/go-sql-driver/mysql#dsn-data-source-name)
var reDSNGo = regexp.MustCompile(`(?:^|[\s"'\x60=(,])([A-Za-z_][\w.\-]*)(?::[^@\s"'\x60]*)?@(?:tcp6?|unix)\(([^)\s"'\x60]+)\)/([A-Za-z_][\w$\-]*)`)

// PDO do Oracle: oci:dbname=//host:porta/serviço
var reDSNOci = regexp.MustCompile(`\boci:dbname=//([A-Za-z0-9_.\-]+)(?::\d+)?/([A-Za-z_][\w.$\-]*)`)

func acharDSN(s string, add func(ObjAchado)) {
	if strings.Contains(s, "@tcp") || strings.Contains(s, "@unix(") {
		for _, m := range reDSNGo.FindAllStringSubmatchIndex(s, -1) {
			if !publicoConexao(s[m[2]:m[3]]) {
				add(ObjAchado{m[2], m[3], "usuario", "dsn", true})
			}
			if s[m[4]] != '/' { // unix(/caminho/socket) não tem host
				addServidor(s, m[4], m[5], "dsn", add)
			}
			if !publicoConexao(s[m[6]:m[7]]) {
				add(ObjAchado{m[6], m[7], "database", "dsn", true})
			}
		}
	}
	if strings.Contains(s, "oci:dbname=//") {
		for _, m := range reDSNOci.FindAllStringSubmatchIndex(s, -1) {
			addServidor(s, m[2], m[3], "dsn", add)
			addPartes(s, m[4], m[5], "database", "dsn", add)
		}
	}
}

func semEspacoIgual(s string, m []int) bool {
	return m[3] < len(s) && s[m[3]] == '=' && s[m[4]-1] == '='
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
