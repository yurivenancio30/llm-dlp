package mask

import (
	"regexp"
	"strings"
)

// Lists shared by the development, configuration and code readers: type words (what a key or
// function name points to), internal network suffixes, public TLDs, public names that are never
// masked (software, system standards, public folders and package prefixes). Short lists, from
// public standards and documentation; nothing per customer.

// Internal name suffixes: RFC 6762 (.local, mDNS), RFC 8375 (.home.arpa), ICANN's reservation for
// .internal (2024), the Kubernetes DNS (.svc, .cluster.local) and the most common private network
// conventions (.intra, .intranet, .interno, .corp, .lan, .localdomain).
var internalSuffixes = []string{".cluster.local", ".home.arpa", ".local", ".internal", ".intra", ".intranet",
	".interno", ".corp", ".lan", ".svc", ".localdomain"}

// Most used generic TLDs (IANA list, https://data.iana.org/TLD/tlds-alpha-by-domain.txt, trimmed to
// the commonly used ones). Every 2-letter TLD is a country (ISO 3166) and is also public.
var publicTLDs = setOf("com", "net", "org", "edu", "gov", "mil", "int", "info", "biz", "io", "ai", "app", "dev",
	"cloud", "tech", "online", "site", "xyz", "name", "pro", "aero", "coop", "museum", "mobi", "asia", "tel", "travel",
	"jobs", "page", "blog", "store", "shop", "web", "news", "live", "art", "design", "digital", "network", "systems",
	"solutions", "software", "codes", "tools", "run", "build", "google", "aws", "azure", "microsoft", "amazon", "apple")

// Names reserved for documentation and testing (RFC 2606, RFC 6761): never a real resource.
var reservedTLDs = setOf("invalid", "example", "test", "localhost")

// Generic words ("user", "bucket") and names of public software or standards that appear in a
// resource position. Nothing is left in plain text for "looking like an example" (my-*, example-*):
// such a name may be real; when in doubt, mask. Sources: the default users of cloud and CI images
// (ec2-user, ubuntu, azureuser, opc, runner, jovyan, vagrant, linuxbrew) and the Kubernetes
// namespaces (default, kube-system...).
var vocabDev = setOf(
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

// System folders and public conventions that look like an identifier (plain words such as
// Documents, Desktop, bin, src do not, and already stay). Sources: Windows layout (Known Folders),
// macOS (File System Programming Guide), Python (site-packages, dist-packages, __pycache__), Node
// (node_modules), Go (go-build) and the JetBrains IDEs.
var publicFolders = setOf("node_modules", "site-packages", "dist-packages", "__pycache__", "appdata", "locallow",
	"onedrive", "ideaprojects", "pycharmprojects", "androidstudioprojects", "go-build", "lost+found",
	"application data", "my documents", "program files", "programdata", "windowsapps", "microsoft",
	"github.com", "gitlab.com", "bitbucket.org", "golang.org", "google.golang.org", "gopkg.in", "go.uber.org",
	"k8s.io", "sigs.k8s.io", "pkg", "mod", "cache", "go-mod", "vscode-server", "jetbrains",
	// repository convention folders (every project has them; they say nothing about the customer)
	"examples", "example", "samples", "sample", "demo", "demos", "tests", "test", "testdata", "test_data",
	"fixtures", "scripts", "tools", "vendor", "third_party", "resources", "templates", "benchmarks", "migrations", "internal", "cmd")

// built-in Windows domains and accounts (Well-known SIDs and the Registry roots)
var publicDomains = setOf("AUTHORITY", "BUILTIN", "SERVICE", "WORKGROUP", "HKLM", "HKCU", "HKCR", "HKU", "HKCC",
	"SOFTWARE", "SYSTEM", "MACHINE", "WINDOWS", "SYSTEM32", "PROGRA~1", "FONT", "DEVICE", "GLOBAL", "LOCAL", "PIPE")

var packageRoots = setOf("com", "org", "net", "io", "java", "javax", "jakarta", "sun", "jdk", "android", "androidx",
	"kotlin", "scala", "akka", "golang", "std", "self", "this", "cls", "os", "sys", "threading", "asyncio")

// schemes whose "host" is not a server, or that another reader already covers
var databaseSchemes = setOf("jdbc", "postgres", "postgresql", "mysql", "mariadb", "mssql", "sqlserver", "oracle",
	"redshift", "snowflake", "mongodb", "mongodb+srv", "clickhouse", "db2", "teradata", "presto", "trino", "hive",
	"cockroachdb", "sqlite", "file")

// last piece of the key name (lowercase) -> entity
var entPiece = map[string]string{
	"host": "server", "hostname": "server", "server": "server", "servidor": "server", "endpoint": "server",
	"address": "server", "addr": "server", "fqdn": "server", "broker": "server", "bootstrap": "server",
	"bootstrapservers": "server", "servername": "server",
	"cluster": "server", "instance": "server", "warehouse": "service",
	"database": "database", "db": "database", "dbname": "database", "databasename": "database", "catalog": "database",
	"schema": "schema", "dataset": "schema", "schemaname": "schema",
	"table": "table", "tabela": "table", "collection": "table", "tablename": "table",
	"user": "user", "username": "user", "usuario": "user", "login": "user", "principal": "user",
	"namespace": "namespace",
	"service":   "service", "servico": "service", "app": "service", "application": "service",
	"bucket": "bucket", "container": "bucket",
	"queue": "queue", "topic": "queue", "fila": "queue", "topico": "queue", "exchange": "queue", "stream": "queue", "subject": "queue",
	"group": "service", "consumergroup": "service",
	"repo": "repository", "repository": "repository",
	"org": "organization", "organization": "organization",
	"account": "cloud_account", "tenant": "cloud_account", "subscription": "cloud_account", "projectid": "cloud_account",
	"column": "column", "coluna": "column", "field": "column", "role": "user",
	"procedure": "procedure", "routine": "procedure", "view": "table",
}

// "<x>_name": the piece before "name" gives the entity (db_name, table_name...)
var entBeforeName = map[string]string{"db": "database", "database": "database", "table": "table", "schema": "schema",
	"host": "server", "server": "server", "user": "user", "service": "service", "bucket": "bucket", "queue": "queue",
	"topic": "queue", "group": "service", "namespace": "namespace", "cluster": "server", "collection": "table",
	"stream": "queue", "subject": "queue", "exchange": "queue", "repo": "repository", "container": "bucket",
	"column": "column", "field": "column", "role": "user", "warehouse": "service", "account": "cloud_account", "owner": "user",
	"view": "table", "procedure": "procedure", "function": "procedure", "index": "index", "constraint": "index", "catalog": "database",
	"dataset": "schema", "project": "cloud_account"}

// suffixes glued to a type word ("rolename", "warehousename", "fieldpath")
var gluedSuffixes = []string{"names", "name", "nome", "path", "fqn"}

// plurals accepted as the type (the name holds a list of that type)
var pluralType = setOf("topics", "queues", "buckets", "hosts", "servers", "brokers", "tables", "databases", "schemas",
	"namespaces", "clusters", "subjects", "streams", "exchanges", "columns", "fields", "roles", "warehouses", "views",
	"accounts", "repositories", "repos", "procedures", "routines", "users",
	"tabelas", "colunas", "filas", "topicos", "servidores", "bancos", "usuarios")

// attributes: last key piece that is not the resource name ("db.port", "host.timeout")
var keyAttribute = setOf("name", "names", "id", "ids", "port", "ports", "timeout", "timeouts", "count", "size", "enabled", "enable", "disabled", "max",
	"min", "version", "type", "mode", "ttl", "retries", "retry", "interval", "pass"+"word", "passwd", "pwd", "secret",
	"token", "key", "keys", "agent", "format", "level", "limit", "suffix", "encoding", "charset", "ssl", "tls",
	"protocol", "scheme", "driver", "class", "dialect", "pool", "timezone", "locale", "lang", "url", "uri", "path",
	"file", "dir", "region", "zone", "weight", "priority", "delay", "length", "capacity", "batch", "concurrency")

// cloud prefixes that make "project" the provider's project (GCP_PROJECT, bq_project)
var cloudProjectPrefix = setOf("gcp", "gcloud", "google", "bq", "bigquery", "cloud", "billing", "firebase", "gke", "dataproc")

// fieldName: the value is the name of a field, not data: only type and attribute words, in two or
// more pieces ("project_id", "container_name", "datasetId", "account_id"). It shows up as a value
// when code stores the key name in a constant (CONTAINER_NAME = "container_name") or describes a
// form ({"name": "project_id"}).
func fieldName(v string) bool {
	if len(v) < 4 || len(v) > 40 {
		return false
	}
	for i := 0; i < len(v); i++ {
		if c := v[i]; !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_') {
			return false
		}
	}
	var ps [16][2]int
	n, ok := keyPieces(v, &ps)
	if !ok || n < 2 {
		return false
	}
	var b [24]byte
	for i := 0; i < n; i++ {
		w := string(lowercase(v, ps[i], &b))
		if _, ok := entPiece[w]; ok {
			continue
		}
		if _, ok := entBeforeName[w]; ok {
			continue
		}
		if !keyAttribute[w] {
			return false
		}
	}
	return true
}

// standard vocabulary roles: WAI-ARIA 1.2 (https://www.w3.org/TR/wai-aria-1.2/#role_definitions)
// and LLM chat messages. When the finding's type comes from "role" (database user/role), one of
// these values is the role of an element or of a message, not a user.
var defaultRoles = setOf("alert", "alertdialog", "application", "article", "banner", "blockquote", "button", "caption",
	"cell", "checkbox", "code", "columnheader", "combobox", "complementary", "contentinfo", "definition", "deletion",
	"dialog", "directory", "document", "emphasis", "feed", "figure", "form", "generic", "grid", "gridcell", "group",
	"heading", "img", "insertion", "link", "list", "listbox", "listitem", "log", "main", "marquee", "math", "menu",
	"menubar", "menuitem", "menuitemcheckbox", "menuitemradio", "meter", "navigation", "none", "note", "option",
	"paragraph", "presentation", "progressbar", "radio", "radiogroup", "region", "row", "rowgroup", "rowheader",
	"scrollbar", "search", "searchbox", "separator", "slider", "spinbutton", "status", "strong", "subscript",
	"superscript", "switch", "tab", "table", "tablist", "tabpanel", "term", "textbox", "time", "timer", "toolbar",
	"tooltip", "tree", "treegrid", "treeitem",
	"system", "user", "assistant", "tool", "function", "developer", "model", "human", "ai")

// original generic TLDs (RFC 1591): alone, they are a piece of a domain, never a name
var genericTLDs = setOf("com", "net", "org", "edu", "gov", "mil")

// URI schemes: value of a "schema"/"scheme" key that is the protocol, not a database schema
var uriSchemes = setOf("http", "https", "ftp", "ftps", "sftp", "ssh", "file", "ws", "wss", "s3", "s3a", "s3n", "gs",
	"abfs", "abfss", "wasb", "wasbs", "adl", "hdfs", "jdbc", "odbc", "grpc", "grpcs", "tcp", "udp", "smtp", "smtps")

// reReversedGroup: a name in reversed domain notation (Maven groupId, Java package): org.slf4j,
// com.google.guava, io.netty. The packages reader separates the public prefix from the company name
// (groupIn); the other readers do not take it as a service.
var reReversedGroup = regexp.MustCompile(`^(?:org|com|io|net|edu|gov|dev|br|de|uk|fr|jp)(?:\.[a-z][a-z0-9_-]*)+$`)

// Ownerless value: a name made only of role vocabulary (placeholder, environment, source and
// destination) and type vocabulary (the resource type itself, an attribute, the provider), plus
// numbering, is nobody's name: "test-staging-bucket", "source_host", "stub-user", "MY_DATABASE",
// "ns-1", "imported_user1", "the_account". One piece outside that is enough to make it the owner's
// name ("insurance_claims_db", "count_drink_items", "orders_hx", "risk_data_access_opr").
var roleVocab = setOf("test", "tests", "testing", "stub", "stubbed", "mock", "mocked", "dummy", "fake", "sample",
	"samples", "example", "examples", "placeholder", "my", "your", "our", "the", "some", "any", "foo", "bar",
	"baz", "qux", "source", "src", "target", "tgt", "dest", "dst", "destination", "actual", "expected", "different",
	"other", "another", "new", "old", "default", "primary", "secondary", "remote", "local", "imported", "explicit",
	"custom", "generic", "unknown", "temp", "tmp", "dev", "prod", "production", "staging", "stage", "qa", "uat",
	"sandbox", "conn", "connection", "client", "server", "service", "app", "api", "data", "base", "system", "main",
	"first", "second", "third", "release")

// "demo" is left out: in the project's tests, "app-demo", "repo-demo" play the customer's name

// abbreviations and synonyms of the resource type that are not in the key vocabulary
var typeVocab = setOf("ns", "acct", "proj", "tbl", "svc", "srv", "usr", "inst", "keyspace", "instance", "index",
	"queue", "topic", "wh", "storage", "aws", "azure", "gcp", "cloud",
	// Kubernetes/compose object types that go at the end of the name (minio-deployment, redis-svc)
	"deployment", "controller", "rc", "hpa", "pod", "ingress", "secret", "claim", "pvc", "pv", "job", "worker",
	"node", "consumer", "producer", "gateway", "proxy", "exporter", "operator", "replica", "replicas", "master",
	"slave", "leader", "follower", "standalone", "headless")

// valuePieces: the lowercase pieces of v, split by punctuation, by case change (camelCase) and by
// letter/digit change.
func valuePieces(v string) []string {
	var ps []string
	ini := -1
	for i := 0; i <= len(v); i++ {
		lineBreak := i == len(v) || !isAlnum(v[i])
		if !lineBreak && ini >= 0 {
			p, c := v[i-1], v[i]
			lineBreak = c >= 'A' && c <= 'Z' && p >= 'a' && p <= 'z' || isDigit(c) != isDigit(p)
			if lineBreak {
				ps = append(ps, strings.ToLower(v[ini:i]))
				ini = i
				continue
			}
		}
		if lineBreak {
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

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// ownerless: v has two or more pieces, all of them role, type or numbering, with one role piece or
// two type words; or only filler X/x ("XXXXXXXXX"). extra: public software names seen in the same
// text.
func ownerless(v string, extra map[string]bool) bool {
	if len(v) >= 3 && strings.Trim(v, "Xx") == "" {
		return true
	}
	ps := valuePieces(v)
	if len(ps) == 0 || len(ps) > 8 {
		return false
	}
	if extra[strings.ToLower(v)] { // the exact name of the official image ("redis", "minio")
		return true
	}
	hostRole, letters := false, 0
	for _, p := range ps {
		switch {
		case isDigit(p[0]) || len(p) <= 2 && p[0] == 'v' && len(ps) > 1: // numbering, version (_v1)
			continue
		case roleVocab[p] || extra[p]:
			hostRole = true
		case typeVocab[p] || keyAttribute[p] || pluralType[p]:
		default:
			_, ok1 := entPiece[p]
			_, ok2 := entBeforeName[p]
			if !ok1 && !ok2 {
				return false
			}
		}
		letters++
	}
	// a single word ("SRC", "main") and type + number ("INST01", "ns-1") follow the other brakes
	return len(ps) >= 2 && (hostRole || letters >= 2)
}

// exampleNumber: a documentation account number, not someone's: only blocks of one repeated digit,
// of the same size ("111122223333", "000000000000"). The sequence "123456789012" is left out: the
// project's tests use it as the customer's account.
//
// Applies only to a value that is the number (up to 3 prefix letters and separators): with a name
// attached ("srv_vendas-99999999-...") the name belongs to the owner and the value is still masked.
func exampleNumber(v string) bool {
	var d []byte
	letters := 0
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case isDigit(c):
			d = append(d, c)
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			if letters++; letters > 3 {
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
	// blocks of one repeated digit, all of the same size
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

// tldOnlyGroups: the publicGroups prefixes that are a whole TLD. They count for the packages reader
// (the io.x groupId of a dependency) and for the traceback line, but do not prove a name is public:
// a company with a .io or .dev domain writes io.<company>.<system>.
var tldOnlyGroups = setOf("io.", "dev.")

// publicJavaGroup: pkg (lowercase, with dots) starts with a public Maven group, as a whole piece
// ("com.google" does not count for "com.googlehx"). strict: without the tldOnlyGroups prefixes.
func publicJavaGroup(pkg string, strict bool) bool {
	for _, g := range publicGroups { // the same public groups as the packages reader (groupIn)
		if strict && tldOnlyGroups[g] {
			continue
		}
		if strings.HasPrefix(pkg, g) && (len(pkg) == len(g) || g[len(g)-1] == '.' || g[len(g)-1] == '-' || pkg[len(g)] == '.') {
			return true
		}
	}
	return false
}

// publicGroup: reversed domain (org.apache.airflow, com.linkedin.metadata) whose owner, the second
// label, is public; com.<company>.x and io.<company>.x still carry the company name.
func publicGroup(v string) bool {
	if !reReversedGroup.MatchString(v) {
		return false
	}
	l := strings.ToLower(v)
	if publicJavaGroup(l, true) {
		return true
	}
	ps := strings.Split(l, ".")
	return publicDev(ps[1]) || publicRef[ps[1]]
}

// vocabularyFinding: brakes common to all readers, by the finding's type. sw: public software names
// seen in the same text (see textSoftware).
func vocabularyFinding(l Reader, o ObjFinding, v string, sw map[string]bool) bool {
	// (a label in a non-Latin script, "スキーマ", stays masked: for a Russian, Chinese or Israeli
	// customer the real table name is also in that script)
	if fieldName(v) || ownerless(v, sw) {
		return true
	}
	lv := strings.ToLower(v)
	// a URL or domain piece alone: a generic TLD ("com" of docs.aws.amazon.com) and a URI scheme
	// ("https" of https://...) are never a customer name
	if genericTLDs[lv] || uriSchemes[lv] && (o.Ent == "schema" || o.Ent == "folder" || o.Ent == "server" || o.Ent == "column") {
		return true
	}
	switch o.Ent {
	case "user":
		return defaultRoles[lv]
	case "cloud_account":
		return exampleNumber(v)
	case "service", "namespace", "queue":
		return l.Name != "package" && publicGroup(v)
	}
	return false
}

// common code receivers: "self.host", "cfg.user", "process.env" are attribute access
var codeReceivers = setOf("self", "this", "cls", "cfg", "conf", "config", "settings", "options", "opts", "args",
	"os", "env", "process", "req", "request", "ctx", "params", "props", "state", "data", "obj", "window", "document",
	"module", "exports", "super", "m", "c", "r", "s", "t", "u", "x", "v", "p")

// type words (annotations "host: str", flag help "--host string")
var typeWords = setOf("str", "string", "int", "integer", "bool", "boolean", "float", "number", "bytes", "any",
	"object", "list", "dict", "map", "array", "optional", "none", "text", "uri", "url", "duration")

// common file extensions: "config.yaml" is not a host
var fileExtensions = setOf("json", "yaml", "yml", "toml", "ini", "txt", "csv", "tsv", "go", "py", "js", "ts", "jsx",
	"tsx", "sql", "sh", "md", "xml", "html", "htm", "conf", "cfg", "properties", "env", "log", "jar", "war", "zip",
	"gz", "tgz", "tar", "pem", "key", "crt", "java", "rb", "rs", "c", "h", "cpp", "php", "pdf", "png", "jpg", "parquet",
	"avro", "lock", "mod", "sum", "exe", "dll", "so", "class", "pyc", "whl", "proto")

// Public Go module hosts (the batch request: the short list of the most common ones; plus
// example.com/org/net, reserved for documentation by RFC 2606).
var publicCodeHosts = setOf("github.com", "gitlab.com", "bitbucket.org", "golang.org", "google.golang.org",
	"gopkg.in", "go.uber.org", "k8s.io", "sigs.k8s.io", "example.com", "example.org", "example.net")

// Public groupId prefixes (Maven Central: the coordinate rules at
// https://central.sonatype.org/publish/requirements/coordinates/ and the most used groups at
// https://mvnrepository.com/popular). Whole "io.": .io domains of open projects.
var publicGroups = []string{"org.apache", "org.springframework", "com.google", "io.", "javax", "jakarta", "java.",
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

// Public npm scopes (the most downloaded at https://www.npmjs.com, framework and SDK
// organizations).
var publicNpmScopes = setOf("types", "angular", "babel", "vue", "nestjs", "aws-sdk", "aws-cdk", "google-cloud", "azure",
	"mui", "emotion", "testing-library", "typescript-eslint", "rollup", "vitejs", "storybook", "reduxjs", "tanstack",
	"octokit", "anthropic-ai", "sentry", "nuxt", "sveltejs", "prisma", "apollo", "graphql-tools", "jest", "playwright",
	"swc", "esbuild", "eslint", "fortawesome", "fontsource", "radix-ui", "headlessui", "heroicons", "tailwindcss", "nx",
	"nrwl", "vercel", "next", "react-native", "react-navigation", "expo", "firebase", "opentelemetry", "grpc",
	"protobufjs", "smithy", "commitlint", "changesets", "semantic-release", "microsoft", "mapbox", "turf", "popperjs",
	"floating-ui", "lit", "webcomponents", "ionic", "capacitor", "ngrx", "pnpm", "yarnpkg", "npmcli", "isaacs",
	"sinonjs", "hapi", "fastify", "trpc", "supabase", "auth0", "stripe", "slack", "cloudflare", "netlify",
	"docusaurus", "mdx-js", "iconify", "faker-js", "hookform", "vueuse", "ant-design", "chakra-ui", "mantine",
	"xstate", "oclif", "inquirer", "modelcontextprotocol", "langchain", "huggingface", "google", "openai")
