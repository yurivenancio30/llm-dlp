package mask

import (
	"strings"
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
				// grupo de recursos: agrupa recursos dentro da assinatura (como um namespace)
				add(ObjAchado{ps[3][0], ps[3][1], "namespace", "azure", true})
			}
			k = 4
		}
		if len(ps) >= k+4 && strings.EqualFold(seg(k), "providers") {
			// Ns, depois pares tipo/nome
			for t := k + 2; t+1 < len(ps); t += 2 {
				if v := seg(t + 1); nomeSimples(v) && !publicoDev(v) {
					ent := entTipoAzure[strings.ToLower(seg(t))]
					if ent == "" {
						ent = "servico"
					}
					add(ObjAchado{ps[t+1][0], ps[t+1][1], ent, "azure", true})
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

// tipo de recurso do Azure (o segmento antes do nome) -> entidade
var entTipoAzure = map[string]string{"servers": "servidor", "flexibleservers": "servidor", "managedinstances": "servidor",
	"databases": "database", "storageaccounts": "conta_nuvem", "containers": "bucket", "shares": "bucket",
	"filesystems": "bucket", "queues": "fila", "topics": "fila", "subscriptions": "fila", "eventhubs": "fila",
	"namespaces": "servidor", "managedclusters": "servidor", "virtualmachines": "servidor", "databaseaccounts": "servidor",
	"registries": "servidor", "redis": "servidor", "sites": "servico", "vaults": "servico", "workspaces": "servico",
	"factories": "servico", "accounts": "conta_nuvem", "tables": "tabela", "schemas": "schema"}

// URLs de nuvem com estrutura fixa:
//
//	https://<conta>.(blob|dfs|file|queue|table).core.windows.net/<contêiner>/...   Azure Storage
//	sb://<namespace>.servicebus.windows.net/ ... EntityPath=<fila>                Service Bus/Event Hubs
//	https://sqs.<região>.amazonaws.com/<conta>/<fila>                              SQS
//
// (https://learn.microsoft.com/azure/storage/common/storage-account-overview#storage-account-endpoints,
// https://learn.microsoft.com/azure/service-bus-messaging/service-bus-dotnet-get-started-with-queues,
// https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-queue-message-identifiers.html)
var servicoArmazenamento = map[string]string{"blob": "bucket", "dfs": "bucket", "file": "bucket", "queue": "fila", "table": "tabela"}

func acharURLsNuvem(s string, add func(ObjAchado)) {
	if strings.Contains(s, "snowflake") {
		acharSnowflake(s, add)
	}
	if strings.Contains(s, ".core.windows.net") {
		for i := strings.Index(s, ".core.windows.net"); i >= 0; {
			// <conta>.<serviço>.core.windows.net
			d := strings.LastIndexByte(s[max(0, i-40):i], '.')
			if d >= 0 {
				d += max(0, i-40)
				serv := s[d+1 : i]
				a := d
				for a > 0 && (ehAlnum(s[a-1]) || s[a-1] == '-') {
					a--
				}
				if ent, ok := servicoArmazenamento[serv]; ok && a < d && (a == 0 || !ehAlnum(s[a-1])) {
					if conta := s[a:d]; !publicoDev(conta) {
						add(ObjAchado{a, d, "conta_nuvem", "azure", true})
					}
					e := i + len(".core.windows.net")
					if e < len(s) && s[e] == '/' {
						b := e + 1
						for b < len(s) && (ehAlnum(s[b]) || s[b] == '-' || s[b] == '_' || s[b] == '$') {
							b++
						}
						if v := s[e+1 : b]; v != "" && !publicoDev(v) && v[0] != '$' {
							add(ObjAchado{e + 1, b, ent, "azure", true})
							if ent == "bucket" && b < len(s) && s[b] == '/' {
								addPastas(s, b+1, fimCaminhoURL(s, b+1), "azure", add)
							}
						}
					}
				}
			}
			j := strings.Index(s[i+1:], ".core.windows.net")
			if j < 0 {
				break
			}
			i += 1 + j
		}
	}
	if strings.Contains(s, ".servicebus.windows.net") {
		for i := strings.Index(s, ".servicebus.windows.net"); i >= 0; {
			a := i
			for a > 0 && (ehAlnum(s[a-1]) || s[a-1] == '-') {
				a--
			}
			if a < i && !publicoDev(s[a:i]) {
				add(ObjAchado{a, i, "servidor", "azure", true})
			}
			j := strings.Index(s[i+1:], ".servicebus.windows.net")
			if j < 0 {
				break
			}
			i += 1 + j
		}
		if k := strings.Index(s, "EntityPath="); k >= 0 {
			a := k + len("EntityPath=")
			b := a
			for b < len(s) && (ehAlnum(s[b]) || s[b] == '-' || s[b] == '_' || s[b] == '.' || s[b] == '/') {
				b++
			}
			if b > a {
				add(ObjAchado{a, b, "fila", "azure", true})
			}
		}
	}
	if strings.Contains(s, "amazonaws.com/") {
		for i := strings.Index(s, "amazonaws.com/"); i >= 0; {
			// só SQS: sqs.<região>.amazonaws.com ou queue.amazonaws.com
			h := s[max(0, i-40):i]
			if strings.Contains(h, "sqs.") || strings.HasSuffix(h, "queue.") {
				a := i + len("amazonaws.com/")
				b := a
				for b < len(s) && ehDig(s[b]) {
					b++
				}
				if b-a == 12 && b < len(s) && s[b] == '/' {
					add(ObjAchado{a, b, "conta_nuvem", "aws", true})
					c := b + 1
					for c < len(s) && (ehAlnum(s[c]) || s[c] == '-' || s[c] == '_' || s[c] == '.') {
						c++
					}
					if c > b+1 {
						add(ObjAchado{b + 1, c, "fila", "aws", true})
					}
				}
			}
			j := strings.Index(s[i+1:], "amazonaws.com/")
			if j < 0 {
				break
			}
			i += 1 + j
		}
	}
}

// Snowflake: <conta>.snowflakecomputing.com (a conta pode vir como org-conta ou com a região
// depois: xy12345.us-east-1) e app.snowflake.com/<org>/<conta>/.
func acharSnowflake(s string, add func(ObjAchado)) {
	for i := strings.Index(s, ".snowflakecomputing.com"); i >= 0; {
		a := i
		for a > 0 && (ehAlnum(s[a-1]) || s[a-1] == '-' || s[a-1] == '_' || s[a-1] == '.') {
			a--
		}
		if d := strings.IndexByte(s[a:i], '.'); d >= 0 { // conta.região
			i2 := a + d
			if i2 > a {
				add(ObjAchado{a, i2, "conta_nuvem", "snowflake", true})
			}
		} else if a < i && !publicoDev(s[a:i]) {
			add(ObjAchado{a, i, "conta_nuvem", "snowflake", true})
		}
		j := strings.Index(s[i+1:], ".snowflakecomputing.com")
		if j < 0 {
			break
		}
		i += 1 + j
	}
	for i := strings.Index(s, "app.snowflake.com/"); i >= 0; {
		a := i + len("app.snowflake.com/")
		for k, ent := range []string{"organizacao", "conta_nuvem"} {
			b := a
			for b < len(s) && (ehAlnum(s[b]) || s[b] == '-' || s[b] == '_') {
				b++
			}
			if b == a || k == 0 && (b >= len(s) || s[b] != '/') {
				break
			}
			add(ObjAchado{a, b, ent, "snowflake", true})
			a = b + 1
		}
		j := strings.Index(s[i+1:], "app.snowflake.com/")
		if j < 0 {
			break
		}
		i += 1 + j
	}
}

func fimCaminhoURL(s string, a int) int {
	for a < len(s) && !fimURL(s[a]) && s[a] != '?' && s[a] != '#' {
		a++
	}
	return a
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
