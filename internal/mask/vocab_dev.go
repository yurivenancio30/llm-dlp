package mask

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
	"k8s.io", "sigs.k8s.io", "pkg", "mod", "cache", "go-mod", "vscode-server", "jetbrains")

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
	"cluster": "servidor", "instance": "servidor", "warehouse": "servidor",
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
	"column": "coluna", "field": "coluna", "role": "usuario", "warehouse": "servidor", "account": "conta_nuvem", "owner": "usuario",
	"view": "tabela", "procedure": "procedure", "function": "procedure", "index": "indice", "constraint": "indice", "catalog": "database"}

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
