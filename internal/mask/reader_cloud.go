package mask

import (
	"strings"
)

// Recursos de nuvem: ARN da AWS, IDs do Azure Resource Manager e nomes de recurso do Google
// Cloud (ver docs/pt-BR/structures.md, seção Recursos de nuvem e usuários de rede). Os hosts de
// serviço gerenciado ficam em reader_cloud_hosts.go.

func findCloud(s string, add func(ObjFinding)) {
	if strings.Contains(s, "arn:") {
		findARN(s, add)
	}
	if strings.Contains(s, "/subscriptions/") {
		findAzure(s, add)
	}
	if strings.Contains(s, "projects/") {
		findGCP(s, add)
	}
	if strings.IndexByte(s, '-') >= 0 {
		findAWSIDs(s, add)
	}
}

// awsPrefixes: IDs de recurso da AWS (EC2 e VPC), prefixo-hexadecimal com 8 ou 17 caracteres (o
// formato longo, desde 2018: aws.amazon.com/blogs/compute/longer-resource-ids-in-2018-for-amazon-ec2-amazon-ebs-and-amazon-vpc).
// Os prefixos são os dos exemplos da documentação da AWS CLI (docs.aws.amazon.com/code-library,
// cli_2_ec2_code_examples). Ficam de fora ami- e pl-: há imagens e listas de prefixos públicas,
// da própria AWS.
var awsPrefixes = setOf("i", "vpc", "subnet", "sg", "eni", "vol", "snap", "rtb", "igw", "eigw", "nat", "acl",
	"vpce", "pcx", "tgw", "tgw-attach", "vgw", "cgw", "vpn", "dopt", "eipalloc", "eipassoc", "lt", "fl")

// findAWSIDs: o ID solto (vpc-0a1b2c3d4e5f67890), com o mesmo pseudônimo que tem dentro do ARN.
func findAWSIDs(s string, add func(ObjFinding)) {
	looseOne := func(c byte) bool { return !(isAlnum(c) || c == '_' || c == '-') }
	for i := strings.IndexByte(s, '-'); i >= 0; {
		j := i + 1
		for j < len(s) && (isDig(s[j]) || s[j] >= 'a' && s[j] <= 'f') {
			j++
		}
		if n := j - i - 1; (n == 8 || n == 17) && (j == len(s) || looseOne(s[j])) && hasDigit(s[i+1:j]) {
			a := i
			for a > 0 && s[a-1] >= 'a' && s[a-1] <= 'z' {
				a--
			}
			pfx := s[a:i]
			if pfx == "attach" && a >= 4 && s[a-4:a] == "tgw-" {
				a, pfx = a-4, "tgw-attach"
			}
			if awsPrefixes[pfx] && (a == 0 || looseOne(s[a-1])) {
				add(ObjFinding{a, j, "servico", "id-aws", true})
			}
		}
		k := strings.IndexByte(s[i+1:], '-')
		if k < 0 {
			return
		}
		i += 1 + k
	}
}

func resourceEnd(c byte) bool {
	return urlEnd(c) || c == '*' || c == '$' || c == '{' || c == '?' || c == '#'
}

// arn:partição:serviço:região:conta:recurso (https://docs.aws.amazon.com/IAM/latest/UserGuide/reference-arns.html)
func findARN(s string, add func(ObjFinding)) {
	for i := strings.Index(s, "arn:"); i >= 0; {
		if i == 0 || !(isAlnum(s[i-1]) || s[i-1] == '_' || s[i-1] == '-' || s[i-1] == ':') {
			arnEm(s, i, add)
		}
		j := strings.Index(s[i+4:], "arn:")
		if j < 0 {
			break
		}
		i += 4 + j
	}
}

func arnEm(s string, i int, add func(ObjFinding)) {
	var cs [5]int // posições dos ":" (depois de arn, partição, serviço, região e conta)
	p := i + 3
	cs[0] = p
	for k := 1; k < 5; k++ {
		q := p + 1
		// região e conta podem ser curinga em policy (arn:aws:glue:*:123456789012:...)
		for q < len(s) && q-p < 40 && s[q] != ':' && (!resourceEnd(s[q]) || s[q] == '*') {
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
	svc := s[cs[1]+1 : cs[2]]
	account := s[cs[3]+1 : cs[4]]
	if len(account) == 12 && strings.Trim(account, "0123456789") == "" && !publicDev(account) {
		add(ObjFinding{cs[3] + 1, cs[4], "conta_nuvem", "arn", true})
	}
	if account == "aws" {
		return // recurso gerenciado pela AWS (arn:aws:iam::aws:policy/...): público
	}
	ra := cs[4] + 1
	rb := ra
	for rb < len(s) && !resourceEnd(s[rb]) {
		rb++
	}
	wildcard := rb < len(s) && strings.IndexByte("*${", s[rb]) >= 0 // "fila-*", "${Name}"
	for rb > ra && (s[rb-1] == '.' || s[rb-1] == '/' || s[rb-1] == ':') {
		rb--
	}
	rec := s[ra:rb]
	if wildcard && (rb == len(s) || s[rb] != '/' && s[rb] != ':') {
		return
	}
	piece := func(a, b int, ent string) {
		if v := s[a:b]; simpleName(v) && !publicDev(v) {
			add(ObjFinding{a, b, ent, "arn", true})
		}
	}
	cut := func(v string) int { // fim do primeiro pedaço
		if k := strings.IndexAny(v, "/:"); k >= 0 {
			return k
		}
		return len(v)
	}
	switch svc {
	case "s3":
		piece(ra, ra+cut(rec), "bucket")
	case "sqs", "sns":
		piece(ra, ra+cut(rec), "fila")
	case "dynamodb":
		if strings.HasPrefix(rec, "table/") {
			a := ra + 6
			piece(a, a+cut(s[a:rb]), "tabela")
		}
	default:
		// tipo/nome ou tipo:nome (o último pedaço é o nome; "role/caminho/nome")
		k := cut(rec)
		if k == len(rec) {
			piece(ra, rb, "servico")
			return
		}
		typ := rec[:k]
		a := ra + k + 1
		b := a + cut(s[a:rb])
		if svc == "iam" || typ == "role" || typ == "user" {
			b = rb
			if l := strings.LastIndexByte(s[a:rb], '/'); l >= 0 {
				a += l + 1
			}
		}
		ent := "servico"
		if svc == "iam" && typ == "user" {
			ent = "usuario"
		}
		piece(a, b, ent)
	}
}

// /subscriptions/<guid>/resourceGroups/<rg>/providers/<Ns>/<tipo>/<nome>[/<subtipo>/<subnome>]...
// (https://learn.microsoft.com/azure/azure-resource-manager/management/resource-name-rules)
func findAzure(s string, add func(ObjFinding)) {
	for i := strings.Index(s, "/subscriptions/"); i >= 0; {
		var ps [][2]int
		for x := i + 1; x < len(s) && len(ps) < 16; {
			y := x
			for y < len(s) && s[y] != '/' && !resourceEnd(s[y]) {
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
		sgm := func(k int) string { return s[ps[k][0]:ps[k][1]] }
		if len(ps) >= 2 {
			if g := sgm(1); len(g) == 36 && strings.Count(g, "-") == 4 && !publicDev(g) {
				add(ObjFinding{ps[1][0], ps[1][1], "conta_nuvem", "azure", true})
			}
		}
		k := 2
		if len(ps) >= 4 && strings.EqualFold(sgm(2), "resourceGroups") {
			if v := sgm(3); simpleName(v) && !publicDev(v) {
				// grupo de recursos: agrupa recursos dentro da assinatura (como um namespace)
				add(ObjFinding{ps[3][0], ps[3][1], "namespace", "azure", true})
			}
			k = 4
		}
		if len(ps) >= k+4 && strings.EqualFold(sgm(k), "providers") {
			// Ns, depois pares tipo/nome
			for t := k + 2; t+1 < len(ps); t += 2 {
				if v := sgm(t + 1); simpleName(v) && !publicDev(v) {
					ent := entAzureType[strings.ToLower(sgm(t))]
					if ent == "" {
						ent = "servico"
					}
					add(ObjFinding{ps[t+1][0], ps[t+1][1], ent, "azure", true})
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
var entAzureType = map[string]string{"servers": "servidor", "flexibleservers": "servidor", "managedinstances": "servidor",
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
var storageService = map[string]string{"blob": "bucket", "dfs": "bucket", "file": "bucket", "queue": "fila", "table": "tabela"}

func findCloudURLs(s string, add func(ObjFinding)) {
	findCloudHosts(s, add)
	if strings.Contains(s, "snowflake") {
		findSnowflake(s, add)
	}
	if strings.Contains(s, ".core.windows.net") {
		for i := strings.Index(s, ".core.windows.net"); i >= 0; {
			// <conta>.<serviço>.core.windows.net
			d := strings.LastIndexByte(s[max(0, i-40):i], '.')
			if d >= 0 {
				d += max(0, i-40)
				svc := s[d+1 : i]
				a := d
				for a > 0 && (isAlnum(s[a-1]) || s[a-1] == '-') {
					a--
				}
				if ent, ok := storageService[svc]; ok && a < d && (a == 0 || !isAlnum(s[a-1])) {
					if account := s[a:d]; !publicDev(account) {
						add(ObjFinding{a, d, "conta_nuvem", "azure", true})
					}
					e := i + len(".core.windows.net")
					if e < len(s) && s[e] == '/' {
						b := e + 1
						for b < len(s) && (isAlnum(s[b]) || s[b] == '-' || s[b] == '_' || s[b] == '$') {
							b++
						}
						if v := s[e+1 : b]; v != "" && !publicDev(v) && v[0] != '$' {
							add(ObjFinding{e + 1, b, ent, "azure", true})
							if ent == "bucket" && b < len(s) && s[b] == '/' {
								addFolders(s, b+1, urlPathEnd(s, b+1), "azure", add)
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
			for a > 0 && (isAlnum(s[a-1]) || s[a-1] == '-') {
				a--
			}
			if a < i && !publicDev(s[a:i]) {
				add(ObjFinding{a, i, "servidor", "azure", true})
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
			for b < len(s) && (isAlnum(s[b]) || s[b] == '-' || s[b] == '_' || s[b] == '.' || s[b] == '/') {
				b++
			}
			if b > a {
				add(ObjFinding{a, b, "fila", "azure", true})
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
				for b < len(s) && isDig(s[b]) {
					b++
				}
				if b-a == 12 && b < len(s) && s[b] == '/' {
					add(ObjFinding{a, b, "conta_nuvem", "aws", true})
					c := b + 1
					for c < len(s) && (isAlnum(s[c]) || s[c] == '-' || s[c] == '_' || s[c] == '.') {
						c++
					}
					if c > b+1 {
						add(ObjFinding{b + 1, c, "fila", "aws", true})
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
// depois: xy12345.us-east-1), app.<org>-<conta>.snowflakecomputing.com (Snowsight) e
// app.snowflake.com/<org>/<conta>/.
func findSnowflake(s string, add func(ObjFinding)) {
	for i := strings.Index(s, ".snowflakecomputing.com"); i >= 0; {
		a := i
		for a > 0 && (isAlnum(s[a-1]) || s[a-1] == '-' || s[a-1] == '_' || s[a-1] == '.') {
			a--
		}
		if strings.HasPrefix(strings.ToLower(s[a:i]), "app.") { // Snowsight: a conta vem depois de "app."
			a += 4
		}
		if d := strings.IndexByte(s[a:i], '.'); d >= 0 { // conta.região
			i2 := a + d
			if i2 > a {
				add(ObjFinding{a, i2, "conta_nuvem", "snowflake", true})
			}
		} else if a < i && !publicDev(s[a:i]) {
			add(ObjFinding{a, i, "conta_nuvem", "snowflake", true})
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
			for b < len(s) && (isAlnum(s[b]) || s[b] == '-' || s[b] == '_') {
				b++
			}
			if b == a || k == 0 && (b >= len(s) || s[b] != '/') {
				break
			}
			add(ObjFinding{a, b, ent, "snowflake", true})
			a = b + 1
		}
		j := strings.Index(s[i+1:], "app.snowflake.com/")
		if j < 0 {
			break
		}
		i += 1 + j
	}
}

func urlPathEnd(s string, a int) int {
	for a < len(s) && !urlEnd(s[a]) && s[a] != '?' && s[a] != '#' {
		a++
	}
	return a
}

// projects/<p>/(topics|subscriptions|datasets|buckets|instances|...)/<nome> e
// projects/<p>/locations/<l>/<tipo>/<nome> (https://cloud.google.com/apis/design/resource_names)
var entGCPType = map[string]string{"topics": "fila", "subscriptions": "fila", "datasets": "schema", "buckets": "bucket",
	"tables": "tabela", "queues": "fila"}

func findGCP(s string, add func(ObjFinding)) {
	for i := strings.Index(s, "projects/"); i >= 0; {
		if i == 0 || strings.IndexByte("/ \t\n\"'=:(`", s[i-1]) >= 0 {
			var ps [][2]int
			for x := i; x < len(s) && len(ps) < 12; {
				y := x
				for y < len(s) && s[y] != '/' && !resourceEnd(s[y]) {
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
			gcpIn(s, ps, add)
		}
		j := strings.Index(s[i+9:], "projects/")
		if j < 0 {
			break
		}
		i += 9 + j
	}
}

func gcpIn(s string, ps [][2]int, add func(ObjFinding)) {
	if len(ps) < 4 {
		return
	}
	sgm := func(k int) string { return s[ps[k][0]:ps[k][1]] }
	p := sgm(1)
	if len(p) < 4 || len(p) > 30 || !letterD(p[0]) || !simpleName(p) || strings.IndexByte(p, '.') >= 0 {
		return
	}
	k := 2
	if sgm(2) == "locations" || sgm(2) == "regions" || sgm(2) == "zones" {
		k = 4
	}
	if len(ps) < k+2 {
		return
	}
	typ := sgm(k)
	for i := 0; i < len(typ); i++ {
		if !(typ[i] >= 'a' && typ[i] <= 'z' || typ[i] >= 'A' && typ[i] <= 'Z') {
			return
		}
	}
	if !publicDev(p) {
		add(ObjFinding{ps[1][0], ps[1][1], "conta_nuvem", "gcp", true})
	}
	for ; k+1 < len(ps); k += 2 {
		ent, ok := entGCPType[sgm(k)]
		if !ok {
			ent = "servico"
		}
		if v := sgm(k + 1); simpleName(v) && !publicDev(v) {
			add(ObjFinding{ps[k+1][0], ps[k+1][1], ent, "gcp", true})
		}
	}
}
