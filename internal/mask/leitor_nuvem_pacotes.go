package mask

import (
	"net/netip"
	"strings"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// Recursos de nuvem (ARN da AWS, IDs do Azure Resource Manager, nomes de recurso do Google
// Cloud), pacotes internos (go.mod, groupId do Maven/Gradle, escopo do npm) e os leitores que
// dependem da configuração: IP público (opção ip_publico) e termos cadastrados embutidos em
// identificadores. Ver docs/estruturas.md, seções Repositórios, pacotes e caminhos e Recursos
// de nuvem e usuários de rede.

// ---- recursos de nuvem ----------------------------------------------------------------

func acharNuvem(s string, add func(ObjAchado)) {
	if strings.Contains(s, "arn:") {
		acharARN(s, add)
	}
	if strings.Contains(s, "/subscriptions/") {
		acharAzure(s, add)
	}
	if strings.Contains(s, "projects/") {
		acharGCP(s, add)
	}
}

func fimRecurso(c byte) bool {
	return fimURL(c) || c == '*' || c == '$' || c == '{' || c == '?' || c == '#'
}

// arn:partição:serviço:região:conta:recurso (https://docs.aws.amazon.com/IAM/latest/UserGuide/reference-arns.html)
func acharARN(s string, add func(ObjAchado)) {
	for i := strings.Index(s, "arn:"); i >= 0; {
		if i == 0 || !(ehAlnum(s[i-1]) || s[i-1] == '_' || s[i-1] == '-' || s[i-1] == ':') {
			arnEm(s, i, add)
		}
		j := strings.Index(s[i+4:], "arn:")
		if j < 0 {
			break
		}
		i += 4 + j
	}
}

func arnEm(s string, i int, add func(ObjAchado)) {
	var cs [5]int // posições dos ":" (depois de arn, partição, serviço, região e conta)
	p := i + 3
	cs[0] = p
	for k := 1; k < 5; k++ {
		q := p + 1
		for q < len(s) && q-p < 40 && s[q] != ':' && !fimRecurso(s[q]) {
			q++
		}
		if q >= len(s) || s[q] != ':' {
			return
		}
		cs[k] = q
		p = q
	}
	part := s[cs[0]+1 : cs[1]]
	if !strings.HasPrefix(part, "aws") {
		return
	}
	serv := s[cs[1]+1 : cs[2]]
	conta := s[cs[3]+1 : cs[4]]
	if len(conta) == 12 && strings.Trim(conta, "0123456789") == "" && !publicoDev(conta) {
		add(ObjAchado{cs[3] + 1, cs[4], "conta_nuvem", "arn", true})
	}
	if conta == "aws" {
		return // recurso gerenciado pela AWS (arn:aws:iam::aws:policy/...): público
	}
	ra := cs[4] + 1
	rb := ra
	for rb < len(s) && !fimRecurso(s[rb]) {
		rb++
	}
	curinga := rb < len(s) && strings.IndexByte("*${", s[rb]) >= 0 // "fila-*", "${Nome}"
	for rb > ra && (s[rb-1] == '.' || s[rb-1] == '/' || s[rb-1] == ':') {
		rb--
	}
	rec := s[ra:rb]
	if curinga && (rb == len(s) || s[rb] != '/' && s[rb] != ':') {
		return
	}
	pedaco := func(a, b int, ent string) {
		if v := s[a:b]; nomeSimples(v) && !publicoDev(v) {
			add(ObjAchado{a, b, ent, "arn", true})
		}
	}
	corte := func(v string) int { // fim do primeiro pedaço
		if k := strings.IndexAny(v, "/:"); k >= 0 {
			return k
		}
		return len(v)
	}
	switch serv {
	case "s3":
		pedaco(ra, ra+corte(rec), "bucket")
	case "sqs", "sns":
		pedaco(ra, ra+corte(rec), "fila")
	case "dynamodb":
		if strings.HasPrefix(rec, "table/") {
			a := ra + 6
			pedaco(a, a+corte(s[a:rb]), "tabela")
		}
	default:
		// tipo/nome ou tipo:nome (o último pedaço é o nome; "role/caminho/nome")
		k := corte(rec)
		if k == len(rec) {
			pedaco(ra, rb, "servico")
			return
		}
		tipo := rec[:k]
		a := ra + k + 1
		b := a + corte(s[a:rb])
		if serv == "iam" || tipo == "role" || tipo == "user" {
			b = rb
			if l := strings.LastIndexByte(s[a:rb], '/'); l >= 0 {
				a += l + 1
			}
		}
		ent := "servico"
		if serv == "iam" && tipo == "user" {
			ent = "usuario"
		}
		pedaco(a, b, ent)
	}
}

// /subscriptions/<guid>/resourceGroups/<rg>/providers/<Ns>/<tipo>/<nome>[/<subtipo>/<subnome>]...
// (https://learn.microsoft.com/azure/azure-resource-manager/management/resource-name-rules)
func acharAzure(s string, add func(ObjAchado)) {
	for i := strings.Index(s, "/subscriptions/"); i >= 0; {
		var ps [][2]int
		for x := i + 1; x < len(s) && len(ps) < 16; {
			y := x
			for y < len(s) && s[y] != '/' && !fimRecurso(s[y]) {
				y++
			}
			if y == x {
				break
			}
			ps = append(ps, [2]int{x, y})
			if y >= len(s) || s[y] != '/' {
				break
			}
			x = y + 1
		}
		seg := func(k int) string { return s[ps[k][0]:ps[k][1]] }
		if len(ps) >= 2 {
			if g := seg(1); len(g) == 36 && strings.Count(g, "-") == 4 && !publicoDev(g) {
				add(ObjAchado{ps[1][0], ps[1][1], "conta_nuvem", "azure", true})
			}
		}
		k := 2
		if len(ps) >= 4 && strings.EqualFold(seg(2), "resourceGroups") {
			if v := seg(3); nomeSimples(v) && !publicoDev(v) {
				add(ObjAchado{ps[3][0], ps[3][1], "servico", "azure", true})
			}
			k = 4
		}
		if len(ps) >= k+4 && strings.EqualFold(seg(k), "providers") {
			// Ns, depois pares tipo/nome
			for t := k + 2; t+1 < len(ps); t += 2 {
				if v := seg(t + 1); nomeSimples(v) && !publicoDev(v) {
					add(ObjAchado{ps[t+1][0], ps[t+1][1], "servico", "azure", true})
				}
			}
		}
		j := strings.Index(s[i+1:], "/subscriptions/")
		if j < 0 {
			break
		}
		i += 1 + j
	}
}

// projects/<p>/(topics|subscriptions|datasets|buckets|instances|...)/<nome> e
// projects/<p>/locations/<l>/<tipo>/<nome> (https://cloud.google.com/apis/design/resource_names)
var entTipoGCP = map[string]string{"topics": "fila", "subscriptions": "fila", "datasets": "schema", "buckets": "bucket",
	"tables": "tabela", "queues": "fila"}

func acharGCP(s string, add func(ObjAchado)) {
	for i := strings.Index(s, "projects/"); i >= 0; {
		if i == 0 || strings.IndexByte("/ \t\n\"'=:(`", s[i-1]) >= 0 {
			var ps [][2]int
			for x := i; x < len(s) && len(ps) < 12; {
				y := x
				for y < len(s) && s[y] != '/' && !fimRecurso(s[y]) {
					y++
				}
				if y == x {
					break
				}
				ps = append(ps, [2]int{x, y})
				if y >= len(s) || s[y] != '/' {
					break
				}
				x = y + 1
			}
			gcpEm(s, ps, add)
		}
		j := strings.Index(s[i+9:], "projects/")
		if j < 0 {
			break
		}
		i += 9 + j
	}
}

func gcpEm(s string, ps [][2]int, add func(ObjAchado)) {
	if len(ps) < 4 {
		return
	}
	seg := func(k int) string { return s[ps[k][0]:ps[k][1]] }
	p := seg(1)
	if len(p) < 4 || len(p) > 30 || !letraD(p[0]) || !nomeSimples(p) || strings.IndexByte(p, '.') >= 0 {
		return
	}
	k := 2
	if seg(2) == "locations" || seg(2) == "regions" || seg(2) == "zones" {
		k = 4
	}
	if len(ps) < k+2 {
		return
	}
	tipo := seg(k)
	for i := 0; i < len(tipo); i++ {
		if !(tipo[i] >= 'a' && tipo[i] <= 'z' || tipo[i] >= 'A' && tipo[i] <= 'Z') {
			return
		}
	}
	if !publicoDev(p) {
		add(ObjAchado{ps[1][0], ps[1][1], "conta_nuvem", "gcp", true})
	}
	for ; k+1 < len(ps); k += 2 {
		ent, ok := entTipoGCP[seg(k)]
		if !ok {
			ent = "servico"
		}
		if v := seg(k + 1); nomeSimples(v) && !publicoDev(v) {
			add(ObjAchado{ps[k+1][0], ps[k+1][1], ent, "gcp", true})
		}
	}
}

// ---- pacotes internos -----------------------------------------------------------------

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

func acharPacotes(s string, add func(ObjAchado)) {
	if strings.Contains(s, "module ") {
		for i := strings.Index(s, "module "); i >= 0; {
			if i == 0 || s[i-1] == '\n' {
				goModEm(s, i+7, add)
			}
			j := strings.Index(s[i+7:], "module ")
			if j < 0 {
				break
			}
			i += 7 + j
		}
	}
	if strings.Contains(s, "<groupId>") {
		for i := strings.Index(s, "<groupId>"); i >= 0; {
			a := i + 9
			if e := strings.Index(s[a:min(len(s), a+200)], "</groupId>"); e > 0 {
				grupoEm(s, a, a+e, add)
			}
			j := strings.Index(s[i+9:], "<groupId>")
			if j < 0 {
				break
			}
			i += 9 + j
		}
	}
	if strings.Contains(s, "group") {
		for i := strings.Index(s, "group"); i >= 0; {
			gradleEm(s, i, add)
			j := strings.Index(s[i+5:], "group")
			if j < 0 {
				break
			}
			i += 5 + j
		}
	}
	if strings.Contains(s, "\"@") && strings.Contains(s, "\"name\"") {
		for i := strings.Index(s, "\"name\""); i >= 0; {
			npmEm(s, i+6, add)
			j := strings.Index(s[i+6:], "\"name\"")
			if j < 0 {
				break
			}
			i += 6 + j
		}
	}
}

// module host/org/x (go.mod)
func goModEm(s string, a int, add func(ObjAchado)) {
	for a < len(s) && s[a] == ' ' {
		a++
	}
	if a < len(s) && s[a] == '"' {
		a++
	}
	b := a
	for b < len(s) && (ehAlnum(s[b]) || strings.IndexByte("._~/-", s[b]) >= 0) {
		b++
	}
	r := b
	if r < len(s) && s[r] == '"' {
		r++
	}
	for r < len(s) && (s[r] == ' ' || s[r] == '\t' || s[r] == '\r') {
		r++
	}
	if b == a || r < len(s) && s[r] != '\n' && !strings.HasPrefix(s[r:], "//") {
		return // "module " no meio de uma frase
	}
	segs := strings.Split(s[a:b], "/")
	x := a
	if strings.IndexByte(segs[0], '.') >= 0 {
		h := strings.ToLower(segs[0])
		if hostsCodigoPublico[h] {
			return
		}
		if hostInterno(h) {
			add(ObjAchado{a, a + len(segs[0]), "servidor", "pacote", true})
		}
		x += len(segs[0]) + 1
		segs = segs[1:]
	}
	for _, sg := range segs {
		if nomeSimples(sg) && !publicoDev(sg) && !(len(sg) >= 2 && sg[0] == 'v' && strings.Trim(sg[1:], "0123456789") == "") {
			add(ObjAchado{x, x + len(sg), "pacote", "pacote", true})
		}
		x += len(sg) + 1
	}
}

// groupId do Maven/Gradle: os pedaços depois do TLD invertido ("com.empresa.vendas" ->
// empresa, vendas), fora dos prefixos públicos.
func grupoEm(s string, a, b int, add func(ObjAchado)) {
	v := s[a:b]
	if !nomeSimples(v) || strings.IndexByte(v, '.') < 0 {
		return
	}
	l := strings.ToLower(v)
	for _, p := range gruposPublicos {
		if strings.HasPrefix(l, p) && (len(l) == len(p) || p[len(p)-1] == '.' || p[len(p)-1] == '-' || l[len(p)] == '.') {
			return
		}
	}
	ps := strings.Split(v, ".")
	x := a
	inicio := true // TLD invertido no começo: "br.com.", "com.", "org."
	for _, p := range ps {
		tld := inicio && (tldsPublicos[strings.ToLower(p)] || len(p) == 2)
		inicio = tld
		if !tld && len(p) >= 2 && reIdentSimples.MatchString(p) && !publicoDev(p) {
			add(ObjAchado{x, x + len(p), "pacote", "pacote", true})
		}
		x += len(p) + 1
	}
}

// group = 'com.empresa' / group "com.empresa" (build.gradle, no começo da linha)
func gradleEm(s string, i int, add func(ObjAchado)) {
	j := i
	for j > 0 && (s[j-1] == ' ' || s[j-1] == '\t') {
		j--
	}
	if j > 0 && s[j-1] != '\n' {
		return
	}
	p := i + 5
	for p < len(s) && s[p] == ' ' {
		p++
	}
	if p < len(s) && s[p] == '=' {
		p++
		for p < len(s) && s[p] == ' ' {
			p++
		}
	}
	if p == i+5 || p >= len(s) || s[p] != '\'' && s[p] != '"' {
		return
	}
	q := s[p]
	e := strings.IndexByte(s[p+1:min(len(s), p+200)], q)
	if e <= 0 {
		return
	}
	grupoEm(s, p+1, p+1+e, add)
}

// "name": "@escopo/pacote" (package.json)
func npmEm(s string, p int, add func(ObjAchado)) {
	for p < len(s) && (s[p] == ' ' || s[p] == '\t') {
		p++
	}
	if p >= len(s) || s[p] != ':' {
		return
	}
	p++
	for p < len(s) && (s[p] == ' ' || s[p] == '\t') {
		p++
	}
	if p+2 >= len(s) || s[p] != '"' || s[p+1] != '@' {
		return
	}
	a := p + 2
	e := strings.IndexByte(s[a:min(len(s), a+214)], '"')
	if e <= 0 {
		return
	}
	v := s[a : a+e]
	k := strings.IndexByte(v, '/')
	if k <= 0 || k == len(v)-1 || !nomeSimples(v[:k]) || !nomeSimples(v[k+1:]) {
		return
	}
	if escoposNpmPublicos[strings.ToLower(v[:k])] {
		return
	}
	add(ObjAchado{a, a + k, "organizacao", "pacote", true})
	add(ObjAchado{a + k + 1, a + e, "pacote", "pacote", true})
}

// ---- leitores que dependem da configuração --------------------------------------------

// leitoresConfig: os leitores ligados conforme a configuração (IP público, termos).
func leitoresConfig(cfg config.Config) []Leitor {
	var ls []Leitor
	if cfg.Objetos.IPPublico {
		ls = append(ls, Leitor{Nome: "ip-público", Achar: acharIPPublico})
	}
	var ts []string
	for _, t := range cfg.Termos {
		for _, v := range t.Valores {
			n := normTermo(v)
			if len(n) >= 3 && !strings.ContainsAny(v, " \t") {
				ts = append(ts, n)
			}
		}
	}
	if len(ts) > 0 {
		ls = append(ls, Leitor{Nome: "termo-embutido", Achar: func(s string, add func(ObjAchado)) { acharTermos(s, ts, add) }})
	}
	return ls
}

// DNS públicos (Google, Cloudflare, Quad9, OpenDNS): aparecem em toda documentação.
var dnsPublicos = conj("8.8.8.8", "8.8.4.4", "1.1.1.1", "1.0.0.1", "9.9.9.9", "208.67.222.222",
	"2001:4860:4860::8888", "2606:4700:4700::1111")

// faixas de documentação (RFC 5737 e RFC 3849): não são endereço real
var redesDoc = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("2001:db8::/32")}

func ipPublico(a netip.Addr) bool {
	if !a.IsGlobalUnicast() || a.Is4() && (a.IsPrivate() || fakeNet.Contains(a)) || dnsPublicos[a.String()] {
		return false
	}
	for _, p := range redesDoc {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

func acharIPPublico(s string, add func(ObjAchado)) {
	if strings.Count(s, ".") >= 3 {
		for _, ix := range reIPv4.FindAllStringIndex(s, -1) {
			if !bordaNum(s, ix[0], ix[1]) || ix[0] > 0 && (letraD(s[ix[0]-1]) || s[ix[0]-1] == '.') {
				continue
			}
			// "6.0.6.1", "8.2.4.44": versão ou número de seção (três primeiros com um dígito)
			if v := s[ix[0]:ix[1]]; len(v) >= 6 && v[1] == '.' && v[3] == '.' && v[5] == '.' {
				continue
			}
			if a, err := netip.ParseAddr(s[ix[0]:ix[1]]); err == nil && ipPublico(a) {
				add(ObjAchado{ix[0], ix[1], "servidor", "ip-público", true})
			}
		}
	}
	if strings.Count(s, ":") < 2 {
		return
	}
	hex := func(c byte) bool { return ehDig(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(hex(c) || c == ':') || i > 0 && (ehAlnum(s[i-1]) || s[i-1] == ':' || s[i-1] == '.' || s[i-1] == '_') {
			continue
		}
		j := i
		for j < len(s) && j-i < 45 && (hex(s[j]) || s[j] == ':' || s[j] == '.') {
			j++
		}
		k := j
		for k > i && s[k-1] == '.' {
			k--
		}
		if j < len(s) && (ehAlnum(s[j]) || s[j] == '_') {
			i = j
			continue
		}
		v := s[i:k]
		dois, longo, grupo := strings.Count(v, ":"), false, 0
		for x := 0; x < len(v); x++ {
			if v[x] == ':' {
				grupo = 0
			} else if grupo++; grupo >= 3 {
				longo = true
			}
		}
		if dois >= 2 && (dois >= 3 || longo) {
			if a, err := netip.ParseAddr(v); err == nil && a.Is6() && !a.Is4In6() && ipPublico(a) {
				add(ObjAchado{i, k, "servidor", "ip-público", true})
			}
		}
		i = j
	}
}

// normTermo: minúsculas, sem separadores ("Acme_Corp" -> "acmecorp").
func normTermo(v string) string {
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c == '_' || c == '-' || c == '.' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b.WriteByte(c)
	}
	return b.String()
}

func identPonto(c byte) bool { return ehAlnum(c) || c == '_' || c == '-' || c == '.' }

// acharTermos: identificador que CONTÉM um termo cadastrado como pedaço ("acme_pedidos",
// "dbAcmeVendas"). O termo sozinho fica com o detector de termos.
func acharTermos(s string, ts []string, add func(ObjAchado)) {
	for _, t := range ts {
		c0 := t[0]
		for i := 0; i+len(t) <= len(s); i++ {
			if s[i]|0x20 != c0 || !strings.EqualFold(s[i:i+len(t)], t) {
				continue
			}
			a, b := i, i+len(t)
			for a > 0 && identPonto(s[a-1]) {
				a--
			}
			for b < len(s) && identPonto(s[b]) {
				b++
			}
			for a < b && (s[a] == '.' || s[a] == '-') {
				a++
			}
			for b > a && (s[b-1] == '.' || s[b-1] == '-') {
				b--
			}
			i = max(i, b-1)
			id := s[a:b]
			if len(id) <= len(t) || !caraDeIdentificador(id) || !temPedaco(id, t) || ehPseudoObj(id) {
				continue
			}
			add(ObjAchado{a, b, entPosicao(s, a), "termo-embutido", true})
		}
	}
}

// temPedaco: o termo t (normalizado) é uma sequência de pedaços inteiros de id.
func temPedaco(id, t string) bool {
	var ps [16][2]int
	n, ok := pedacosChave(id, &ps)
	if !ok {
		n = len(ps)
	}
	for i := 0; i < n; i++ {
		k := 0
		for j := i; j < n && k < len(t); j++ {
			p := id[ps[j][0]:ps[j][1]]
			if k+len(p) > len(t) || !strings.EqualFold(p, t[k:k+len(p)]) {
				break
			}
			k += len(p)
		}
		if k == len(t) {
			return true
		}
	}
	return false
}

// entPosicao: a entidade indicada pelo que vem antes de s[a] na linha (host de URL, valor de
// chave conhecida, nome depois de FROM/JOIN/DATABASE...); sem posição, serviço.
func entPosicao(s string, a int) string {
	k := a
	for k > 0 && a-k < 8 && (s[k-1] == ' ' || s[k-1] == '\t' || s[k-1] == '"' || s[k-1] == '\'' || s[k-1] == '`') {
		k--
	}
	if k >= 3 && s[k-3:k] == "://" || k > 0 && s[k-1] == '@' && k == a {
		return "servidor"
	}
	if k > 0 && (s[k-1] == ':' || s[k-1] == '=') {
		e := k - 1
		for e > 0 && (s[e-1] == ' ' || s[e-1] == '"' || s[e-1] == '\'') {
			e--
		}
		b := e
		for b > 0 && e-b < 64 && (ehAlnum(s[b-1]) || s[b-1] == '_' || s[b-1] == '.' || s[b-1] == '-') {
			b--
		}
		if ent, _ := entChave(strings.TrimLeft(s[b:e], "-")); ent != "" {
			return ent
		}
		return "servico"
	}
	b := k
	for b > 0 && k-b < 10 && letraD(s[b-1]) {
		b--
	}
	switch strings.ToUpper(s[b:k]) {
	case "FROM", "JOIN", "INTO", "UPDATE", "TABLE", "VIEW":
		return "tabela"
	case "DATABASE", "USE":
		return "database"
	case "SCHEMA":
		return "schema"
	case "PROCEDURE", "PROC", "CALL", "EXEC", "EXECUTE", "FUNCTION":
		return "procedure"
	}
	return "servico"
}
