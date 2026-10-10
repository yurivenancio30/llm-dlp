package mask

import (
	"regexp"
	"strings"
)

// Listas compartilhadas pelos leitores de desenvolvimento, configuração e código: palavras de
// tipo (o que um nome de chave ou de função indica), sufixos de rede interna, TLDs públicos,
// nomes públicos que nunca são mascarados (software, padrões de sistema, pastas e prefixos
// públicos de pacote). Listas curtas, de padrões e documentações públicas; nada por cliente.

// Sufixos de nome interno: RFC 6762 (.local, mDNS), RFC 8375 (.home.arpa), a reserva da ICANN
// para .internal (2024), o DNS do Kubernetes (.svc, .cluster.local) e as convenções de rede
// privada mais comuns (.intra, .intranet, .interno, .corp, .lan, .localdomain).
var sufixosInternos = []string{".cluster.local", ".home.arpa", ".local", ".internal", ".intra", ".intranet",
	".interno", ".corp", ".lan", ".svc", ".localdomain"}

// TLDs genéricos mais usados (lista da IANA, https://data.iana.org/TLD/tlds-alpha-by-domain.txt,
// recortada nos de uso comum). Todo TLD de 2 letras é de país (ISO 3166) e também é público.
var tldsPublicos = conj("com", "net", "org", "edu", "gov", "mil", "int", "info", "biz", "io", "ai", "app", "dev",
	"cloud", "tech", "online", "site", "xyz", "name", "pro", "aero", "coop", "museum", "mobi", "asia", "tel", "travel",
	"jobs", "page", "blog", "store", "shop", "web", "news", "live", "art", "design", "digital", "network", "systems",
	"solutions", "software", "codes", "tools", "run", "build", "google", "aws", "azure", "microsoft", "amazon", "apple")

// Nomes reservados para documentação e teste (RFC 2606, RFC 6761): nunca são recurso real.
var tldsReservados = conj("invalid", "example", "test", "localhost")

// Palavras genéricas ("user", "bucket") e nomes de software ou padrões públicos que aparecem na
// posição de um recurso. Nada é deixado em claro por "parecer exemplo" (my-*, example-*): um
// nome desses pode ser real; na dúvida, mascara. Fontes: os usuários padrão das imagens de nuvem e de CI (ec2-user, ubuntu, azureuser, opc, runner,
// jovyan, vagrant, linuxbrew) e os namespaces do Kubernetes (default, kube-system...).
var vocabDev = conj(
	"localhost", "test", "testing", "host", "hostname", "server", "domain", "user", "username", "usuario",
	"name", "value", "nobody", "anonymous", "guest", "admin", "administrator", "administrators", "root",
	"system", "users", "shared", "public", "default", "all users", "default user", "dev", "prod", "production",
	"staging", "stage", "development", "sandbox", "main", "master", "origin", "upstream", "remote", "local",
	"true", "false", "null", "nil", "none", "yes", "no", "on", "off", "undefined", "required", "optional",
	"auto", "git", "ubuntu", "ec2-user", "centos", "debian", "fedora", "bitnami", "azureuser", "opc", "pi",
	"vagrant", "runner", "jovyan", "linuxbrew", "travis", "circleci", "jenkins", "node", "www-data", "gopher",
	"vscode", "codespace", "postgres", "sqlite", "redis", "mongo", "mongodb", "kafka", "rabbitmq", "nginx",
	"apache", "httpd", "docker", "kubernetes", "k8s", "kube-system", "kube-public", "kube-node-lease",
	"gunicorn", "uvicorn", "envoy", "bucket", "topic", "queue", "app", "service", "cluster", "instance",
	"namespace", "repo", "owner", "org", "database", "db", "dbo", "schema", "table",
)

// Pastas de sistema e convenções públicas que têm cara de identificador (as palavras simples
// como Documents, Desktop, bin, src não têm, e já ficam). Fontes: layout do Windows (Known
// Folders), do macOS (File System Programming Guide), do Python (site-packages,
// dist-packages, __pycache__), do Node (node_modules), do Go (go-build) e das IDEs da JetBrains.
var pastasPublicas = conj("node_modules", "site-packages", "dist-packages", "__pycache__", "appdata", "locallow",
	"onedrive", "ideaprojects", "pycharmprojects", "androidstudioprojects", "go-build", "lost+found",
	"application data", "my documents", "program files", "programdata", "windowsapps", "microsoft",
	"github.com", "gitlab.com", "bitbucket.org", "golang.org", "google.golang.org", "gopkg.in", "go.uber.org",
	"k8s.io", "sigs.k8s.io", "pkg", "mod", "cache", "go-mod", "vscode-server", "jetbrains",
	// pastas de convenção de repositório (todo projeto tem; não dizem nada do cliente)
	"examples", "example", "samples", "sample", "demo", "demos", "tests", "test", "testdata", "test_data",
	"fixtures", "scripts", "tools", "vendor", "third_party", "resources", "templates", "benchmarks", "migrations", "internal", "cmd")

// domínios e contas embutidos do Windows (Well-known SIDs e as raízes do Registro)
var dominiosPublicos = conj("AUTHORITY", "BUILTIN", "SERVICE", "WORKGROUP", "HKLM", "HKCU", "HKCR", "HKU", "HKCC",
	"SOFTWARE", "SYSTEM", "MACHINE", "WINDOWS", "SYSTEM32", "PROGRA~1", "FONT", "DEVICE", "GLOBAL", "LOCAL", "PIPE")

var raizesPacote = conj("com", "org", "net", "io", "java", "javax", "jakarta", "sun", "jdk", "android", "androidx",
	"kotlin", "scala", "akka", "golang", "std", "self", "this", "cls", "os", "sys", "threading", "asyncio")

// esquemas cujo "host" não é servidor, ou que outro leitor já cobre
var esquemasBanco = conj("jdbc", "postgres", "postgresql", "mysql", "mariadb", "mssql", "sqlserver", "oracle",
	"redshift", "snowflake", "mongodb", "mongodb+srv", "clickhouse", "db2", "teradata", "presto", "trino", "hive",
	"cockroachdb", "sqlite", "file")

// último pedaço do nome da chave (minúsculas) -> entidade
var entPedaco = map[string]string{
	"host": "servidor", "hostname": "servidor", "server": "servidor", "servidor": "servidor", "endpoint": "servidor",
	"address": "servidor", "addr": "servidor", "fqdn": "servidor", "broker": "servidor", "bootstrap": "servidor",
	"bootstrapservers": "servidor", "servername": "servidor",
	"cluster": "servidor", "instance": "servidor", "warehouse": "servico",
	"database": "database", "db": "database", "dbname": "database", "databasename": "database", "catalog": "database",
	"schema": "schema", "dataset": "schema", "schemaname": "schema",
	"table": "tabela", "tabela": "tabela", "collection": "tabela", "tablename": "tabela",
	"user": "usuario", "username": "usuario", "usuario": "usuario", "login": "usuario", "principal": "usuario",
	"namespace": "namespace",
	"service":   "servico", "servico": "servico", "app": "servico", "application": "servico",
	"bucket": "bucket", "container": "bucket",
	"queue": "fila", "topic": "fila", "fila": "fila", "topico": "fila", "exchange": "fila", "stream": "fila", "subject": "fila",
	"group": "servico", "consumergroup": "servico",
	"repo": "repositorio", "repository": "repositorio",
	"org": "organizacao", "organization": "organizacao",
	"account": "conta_nuvem", "tenant": "conta_nuvem", "subscription": "conta_nuvem", "projectid": "conta_nuvem",
	"column": "coluna", "coluna": "coluna", "field": "coluna", "role": "usuario",
	"procedure": "procedure", "routine": "procedure", "view": "tabela",
}

// "<x>_name": o pedaço antes de "name" diz a entidade (db_name, table_name...)
var entAntesDeNome = map[string]string{"db": "database", "database": "database", "table": "tabela", "schema": "schema",
	"host": "servidor", "server": "servidor", "user": "usuario", "service": "servico", "bucket": "bucket", "queue": "fila",
	"topic": "fila", "group": "servico", "namespace": "namespace", "cluster": "servidor", "collection": "tabela",
	"stream": "fila", "subject": "fila", "exchange": "fila", "repo": "repositorio", "container": "bucket",
	"column": "coluna", "field": "coluna", "role": "usuario", "warehouse": "servico", "account": "conta_nuvem", "owner": "usuario",
	"view": "tabela", "procedure": "procedure", "function": "procedure", "index": "indice", "constraint": "indice", "catalog": "database",
	"dataset": "schema", "project": "conta_nuvem"}

// sufixos colados a uma palavra de tipo ("rolename", "warehousename", "fieldpath")
var sufixosColados = []string{"names", "name", "nome", "path", "fqn"}

// plurais aceitos como o tipo (o nome guarda uma lista desse tipo)
var pluralTipo = conj("topics", "queues", "buckets", "hosts", "servers", "brokers", "tables", "databases", "schemas",
	"namespaces", "clusters", "subjects", "streams", "exchanges", "columns", "fields", "roles", "warehouses", "views",
	"accounts", "repositories", "repos", "procedures", "routines", "users",
	"tabelas", "colunas", "filas", "topicos", "servidores", "bancos", "usuarios")

// atributos: último pedaço de chave que não é o nome do recurso ("db.port", "host.timeout")
var atributoChave = conj("name", "names", "id", "ids", "port", "ports", "timeout", "timeouts", "count", "size", "enabled", "enable", "disabled", "max",
	"min", "version", "type", "mode", "ttl", "retries", "retry", "interval", "pass"+"word", "passwd", "pwd", "secret",
	"token", "key", "keys", "agent", "format", "level", "limit", "suffix", "encoding", "charset", "ssl", "tls",
	"protocol", "scheme", "driver", "class", "dialect", "pool", "timezone", "locale", "lang", "url", "uri", "path",
	"file", "dir", "region", "zone", "weight", "priority", "delay", "length", "capacity", "batch", "concurrency")

// prefixos de nuvem que tornam "project" o projeto do provedor (GCP_PROJECT, bq_project)
var prefixoProjetoNuvem = conj("gcp", "gcloud", "google", "bq", "bigquery", "cloud", "billing", "firebase", "gke", "dataproc")

// nomeDeCampo: o valor é o nome de um campo, não um dado: só palavras de tipo e de atributo,
// em dois ou mais pedaços ("project_id", "container_name", "datasetId", "account_id"). Aparece
// como valor quando o código guarda o nome da chave numa constante (CONTAINER_NAME =
// "container_name") ou descreve um formulário ({"name": "project_id"}).
func nomeDeCampo(v string) bool {
	if len(v) < 4 || len(v) > 40 {
		return false
	}
	for i := 0; i < len(v); i++ {
		if c := v[i]; !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_') {
			return false
		}
	}
	var ps [16][2]int
	n, ok := pedacosChave(v, &ps)
	if !ok || n < 2 {
		return false
	}
	var b [24]byte
	for i := 0; i < n; i++ {
		w := string(minusculo(v, ps[i], &b))
		if _, ok := entPedaco[w]; ok {
			continue
		}
		if _, ok := entAntesDeNome[w]; ok {
			continue
		}
		if !atributoChave[w] {
			return false
		}
	}
	return true
}

// papéis de vocabulário padrão: WAI-ARIA 1.2 (https://www.w3.org/TR/wai-aria-1.2/#role_definitions)
// e mensagens de chat de LLM. Quando o tipo do achado vem de "role" (usuário/role de banco),
// um destes valores é o papel de um elemento ou de uma mensagem, não um usuário.
var papeisPadrao = conj("alert", "alertdialog", "application", "article", "banner", "blockquote", "button", "caption",
	"cell", "checkbox", "code", "columnheader", "combobox", "complementary", "contentinfo", "definition", "deletion",
	"dialog", "directory", "document", "emphasis", "feed", "figure", "form", "generic", "grid", "gridcell", "group",
	"heading", "img", "insertion", "link", "list", "listbox", "listitem", "log", "main", "marquee", "math", "menu",
	"menubar", "menuitem", "menuitemcheckbox", "menuitemradio", "meter", "navigation", "none", "note", "option",
	"paragraph", "presentation", "progressbar", "radio", "radiogroup", "region", "row", "rowgroup", "rowheader",
	"scrollbar", "search", "searchbox", "separator", "slider", "spinbutton", "status", "strong", "subscript",
	"superscript", "switch", "tab", "table", "tablist", "tabpanel", "term", "textbox", "time", "timer", "toolbar",
	"tooltip", "tree", "treegrid", "treeitem",
	"system", "user", "assistant", "tool", "function", "developer", "model", "human", "ai")

// TLDs genéricos originais (RFC 1591): sozinhos, são pedaço de domínio, nunca nome
var tldsGenericos = conj("com", "net", "org", "edu", "gov", "mil")

// esquemas de URI: valor de uma chave "schema"/"scheme" que é o protocolo, não um schema de banco
var esquemasURI = conj("http", "https", "ftp", "ftps", "sftp", "ssh", "file", "ws", "wss", "s3", "s3a", "s3n", "gs",
	"abfs", "abfss", "wasb", "wasbs", "adl", "hdfs", "jdbc", "odbc", "grpc", "grpcs", "tcp", "udp", "smtp", "smtps")

// reGrupoInvertido: nome em notação de domínio invertido (groupId do Maven, pacote Java):
// org.slf4j, com.google.guava, io.netty. O leitor de pacotes separa o prefixo público do nome
// da empresa (grupoEm); os outros leitores não o tomam como serviço.
var reGrupoInvertido = regexp.MustCompile(`^(?:org|com|io|net|edu|gov|dev|br|de|uk|fr|jp)(?:\.[a-z][a-z0-9_-]*)+$`)

// Valor sem dono: um nome feito só de vocabulário de papel (placeholder, ambiente, origem e
// destino) e de tipo (o próprio tipo do recurso, um atributo, o provedor), mais numeração, não é
// nome de ninguém: "test-staging-bucket", "source_host", "stub-user", "MY_DATABASE", "ns-1",
// "imported_user1", "the_account". Basta um pedaço fora disso para ser nome do dono
// ("insurance_claims_db", "count_drink_items", "orders_hx", "risk_data_access_opr").
var vocabPapel = conj("test", "tests", "testing", "stub", "stubbed", "mock", "mocked", "dummy", "fake", "sample",
	"samples", "example", "examples", "placeholder", "my", "your", "our", "the", "some", "any", "foo", "bar",
	"baz", "qux", "source", "src", "target", "tgt", "dest", "dst", "destination", "actual", "expected", "different",
	"other", "another", "new", "old", "default", "primary", "secondary", "remote", "local", "imported", "explicit",
	"custom", "generic", "unknown", "temp", "tmp", "dev", "prod", "production", "staging", "stage", "qa", "uat",
	"sandbox", "conn", "connection", "client", "server", "service", "app", "api", "data", "base", "system", "main",
	"first", "second", "third", "release")

// "demo" fica fora: nos testes do projeto, "app-demo", "repo-demo" fazem o papel do nome do cliente

// abreviações e sinônimos do tipo do recurso que não estão no vocabulário das chaves
var vocabTipo = conj("ns", "acct", "proj", "tbl", "svc", "srv", "usr", "inst", "keyspace", "instance", "index",
	"queue", "topic", "wh", "storage", "aws", "azure", "gcp", "cloud",
	// tipos de objeto do Kubernetes/compose que vão no fim do nome (minio-deployment, redis-svc)
	"deployment", "controller", "rc", "hpa", "pod", "ingress", "secret", "claim", "pvc", "pv", "job", "worker",
	"node", "consumer", "producer", "gateway", "proxy", "exporter", "operator", "replica", "replicas", "master",
	"slave", "leader", "follower", "standalone", "headless")

// pedacosValor: os pedaços de v em minúsculas, separados por pontuação, por troca de caixa
// (camelCase) e por troca entre letra e dígito.
func pedacosValor(v string) []string {
	var ps []string
	ini := -1
	for i := 0; i <= len(v); i++ {
		quebra := i == len(v) || !ehAlnum(v[i])
		if !quebra && ini >= 0 {
			p, c := v[i-1], v[i]
			quebra = c >= 'A' && c <= 'Z' && p >= 'a' && p <= 'z' || ehDigito(c) != ehDigito(p)
			if quebra {
				ps = append(ps, strings.ToLower(v[ini:i]))
				ini = i
				continue
			}
		}
		if quebra {
			if ini >= 0 {
				ps = append(ps, strings.ToLower(v[ini:i]))
			}
			ini = -1
		} else if ini < 0 {
			ini = i
		}
	}
	return ps
}

func ehDigito(c byte) bool { return c >= '0' && c <= '9' }

// semDono: v tem dois ou mais pedaços, todos de papel, de tipo ou de numeração, com um de papel
// ou duas palavras de tipo; ou só X/x de preenchimento ("XXXXXXXXX"). extra: nomes de software
// público vistos no mesmo texto.
func semDono(v string, extra map[string]bool) bool {
	if len(v) >= 3 && strings.Trim(v, "Xx") == "" {
		return true
	}
	ps := pedacosValor(v)
	if len(ps) == 0 || len(ps) > 8 {
		return false
	}
	if extra[strings.ToLower(v)] { // o nome exato da imagem oficial ("redis", "minio")
		return true
	}
	papel, letras := false, 0
	for _, p := range ps {
		switch {
		case ehDigito(p[0]) || len(p) <= 2 && p[0] == 'v' && len(ps) > 1: // numeração, versão (_v1)
			continue
		case vocabPapel[p] || extra[p]:
			papel = true
		case vocabTipo[p] || atributoChave[p] || pluralTipo[p]:
		default:
			_, ok1 := entPedaco[p]
			_, ok2 := entAntesDeNome[p]
			if !ok1 && !ok2 {
				return false
			}
		}
		letras++
	}
	// uma palavra sozinha ("SRC", "main") e tipo + número ("INST01", "ns-1") seguem os outros freios
	return len(ps) >= 2 && (papel || letras >= 2)
}

// numeroDeExemplo: número de conta de documentação, não de alguém: só blocos de um dígito
// repetido, do mesmo tamanho ("111122223333", "000000000000"). A sequência "123456789012" fica
// de fora: os testes do projeto a usam como conta do cliente.
//
// Só vale para o valor que é o número (até 3 letras de prefixo e separadores): com um nome junto
// ("srv_vendas-99999999-...") o nome é do dono e o valor continua mascarado.
func numeroDeExemplo(v string) bool {
	var d []byte
	letras := 0
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case ehDigito(c):
			d = append(d, c)
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			if letras++; letras > 3 {
				return false
			}
		case c == '-' || c == '.' || c == ' ':
		default:
			return false
		}
	}
	if len(d) < 8 {
		return false
	}
	// blocos de um dígito repetido, todos do mesmo tamanho
	var runs []int
	for i := 0; i < len(d); i++ {
		if i == 0 || d[i] != d[i-1] {
			runs = append(runs, 0)
		}
		runs[len(runs)-1]++
	}
	if len(runs) > 4 {
		return false
	}
	for _, r := range runs {
		if r != runs[0] || r < 2 {
			return false
		}
	}
	return true
}

// gruposSoTLD: os prefixos de gruposPublicos que são um TLD inteiro. Valem para o leitor de
// pacotes (o groupId io.x de uma dependência) e para a linha de traceback, mas não provam que um
// nome é público: a empresa com domínio .io ou .dev escreve io.<empresa>.<sistema>.
var gruposSoTLD = conj("io.", "dev.")

// grupoJavaPublico: pkg (minúsculas, com pontos) começa por um grupo público do Maven, pedaço
// inteiro ("com.google" não vale para "com.googlehx"). estrito: sem os prefixos de gruposSoTLD.
func grupoJavaPublico(pkg string, estrito bool) bool {
	for _, g := range gruposPublicos { // os mesmos grupos públicos do leitor de pacotes (grupoEm)
		if estrito && gruposSoTLD[g] {
			continue
		}
		if strings.HasPrefix(pkg, g) && (len(pkg) == len(g) || g[len(g)-1] == '.' || g[len(g)-1] == '-' || pkg[len(g)] == '.') {
			return true
		}
	}
	return false
}

// grupoPublico: domínio invertido (org.apache.airflow, com.linkedin.metadata) cujo dono, o segundo
// rótulo, é público; com.<empresa>.x e io.<empresa>.x continuam com o nome da empresa.
func grupoPublico(v string) bool {
	if !reGrupoInvertido.MatchString(v) {
		return false
	}
	l := strings.ToLower(v)
	if grupoJavaPublico(l, true) {
		return true
	}
	ps := strings.Split(l, ".")
	return publicoDev(ps[1]) || refPublica[ps[1]]
}

// achadoDeVocabulario: freios comuns a todos os leitores, pelo tipo do achado. sw: nomes de
// software público vistos no mesmo texto (ver softwareDoTexto).
func achadoDeVocabulario(l Leitor, o ObjAchado, v string, sw map[string]bool) bool {
	// (rótulo em escrita não latina, "スキーマ", fica mascarado: para um cliente russo, chinês ou
	// israelense o nome real da tabela também é nessa escrita)
	if nomeDeCampo(v) || semDono(v, sw) {
		return true
	}
	lv := strings.ToLower(v)
	// pedaço de URL ou de domínio sozinho: TLD genérico ("com" de docs.aws.amazon.com) e esquema
	// de URI ("https" de https://...) nunca são nome do cliente
	if tldsGenericos[lv] || esquemasURI[lv] && (o.Ent == "schema" || o.Ent == "pasta" || o.Ent == "servidor" || o.Ent == "coluna") {
		return true
	}
	switch o.Ent {
	case "usuario":
		return papeisPadrao[lv]
	case "conta_nuvem":
		return numeroDeExemplo(v)
	case "servico", "namespace", "fila":
		return l.Nome != "pacote" && grupoPublico(v)
	}
	return false
}

// receptores comuns de código: "self.host", "cfg.user", "process.env" são acesso a atributo
var receptoresCodigo = conj("self", "this", "cls", "cfg", "conf", "config", "settings", "options", "opts", "args",
	"os", "env", "process", "req", "request", "ctx", "params", "props", "state", "data", "obj", "window", "document",
	"module", "exports", "super", "m", "c", "r", "s", "t", "u", "x", "v", "p")

// palavras de tipo (anotações "host: str", ajuda de flags "--host string")
var palavrasTipo = conj("str", "string", "int", "integer", "bool", "boolean", "float", "number", "bytes", "any",
	"object", "list", "dict", "map", "array", "optional", "none", "text", "uri", "url", "duration")

// extensões de arquivo comuns: "config.yaml" não é host
var extensoesArquivo = conj("json", "yaml", "yml", "toml", "ini", "txt", "csv", "tsv", "go", "py", "js", "ts", "jsx",
	"tsx", "sql", "sh", "md", "xml", "html", "htm", "conf", "cfg", "properties", "env", "log", "jar", "war", "zip",
	"gz", "tgz", "tar", "pem", "key", "crt", "java", "rb", "rs", "c", "h", "cpp", "php", "pdf", "png", "jpg", "parquet",
	"avro", "lock", "mod", "sum", "exe", "dll", "so", "class", "pyc", "whl", "proto")

// Hosts públicos de módulos Go (o pedido do lote: a lista pequena dos mais comuns; mais
// example.com/org/net, reservados para documentação pela RFC 2606).
var hostsCodigoPublico = conj("github.com", "gitlab.com", "bitbucket.org", "golang.org", "google.golang.org",
	"gopkg.in", "go.uber.org", "k8s.io", "sigs.k8s.io", "example.com", "example.org", "example.net")

// Prefixos públicos de groupId (Maven Central: as regras de coordenadas em
// https://central.sonatype.org/publish/requirements/coordinates/ e os grupos mais usados em
// https://mvnrepository.com/popular). "io." inteiro: domínios .io de projetos abertos.
var gruposPublicos = []string{"org.apache", "org.springframework", "com.google", "io.", "javax", "jakarta", "java.",
	"org.jetbrains", "org.junit", "junit", "org.slf4j", "ch.qos", "com.fasterxml", "org.hibernate", "org.projectlombok",
	"org.mockito", "org.eclipse", "com.amazonaws", "software.amazon", "com.microsoft", "com.azure", "org.postgresql",
	"com.mysql", "mysql", "org.mariadb", "com.oracle", "com.h2database", "org.xerial", "org.yaml", "com.squareup",
	"org.json", "org.codehaus", "org.ow2", "org.gradle", "com.android", "androidx", "org.jboss", "org.assertj",
	"org.testcontainers", "org.flywaydb", "org.liquibase", "com.zaxxer", "commons-", "org.scala-lang", "com.typesafe",
	"org.bouncycastle", "org.example", "com.example", "net.bytebuddy", "org.aspectj", "com.github", "org.openjdk",
	"org.glassfish", "org.webjars", "org.mybatis", "com.alibaba", "org.elasticsearch", "co.elastic", "org.mongodb",
	"redis.clients", "org.lz4", "org.kordamp", "org.immutables", "org.reactivestreams", "net.java", "org.antlr",
	"com.jayway", "org.hamcrest", "org.awaitility", "org.objenesis", "org.checkerframework", "com.puppycrawl",
	"org.freemarker", "org.thymeleaf", "org.quartz-scheduler", "org.apache.", "dev.", "net.sourceforge"}

// Escopos públicos do npm (os mais baixados em https://www.npmjs.com, organizações de
// frameworks e SDKs).
var escoposNpmPublicos = conj("types", "angular", "babel", "vue", "nestjs", "aws-sdk", "aws-cdk", "google-cloud", "azure",
	"mui", "emotion", "testing-library", "typescript-eslint", "rollup", "vitejs", "storybook", "reduxjs", "tanstack",
	"octokit", "anthropic-ai", "sentry", "nuxt", "sveltejs", "prisma", "apollo", "graphql-tools", "jest", "playwright",
	"swc", "esbuild", "eslint", "fortawesome", "fontsource", "radix-ui", "headlessui", "heroicons", "tailwindcss", "nx",
	"nrwl", "vercel", "next", "react-native", "react-navigation", "expo", "firebase", "opentelemetry", "grpc",
	"protobufjs", "smithy", "commitlint", "changesets", "semantic-release", "microsoft", "mapbox", "turf", "popperjs",
	"floating-ui", "lit", "webcomponents", "ionic", "capacitor", "ngrx", "pnpm", "yarnpkg", "npmcli", "isaacs",
	"sinonjs", "hapi", "fastify", "trpc", "supabase", "auth0", "stripe", "slack", "cloudflare", "netlify",
	"docusaurus", "mdx-js", "iconify", "faker-js", "hookform", "vueuse", "ant-design", "chakra-ui", "mantine",
	"xstate", "oclif", "inquirer", "modelcontextprotocol", "langchain", "huggingface", "google", "openai")
