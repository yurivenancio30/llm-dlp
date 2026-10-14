package mask

import (
	"strings"
)

// Cloud resources: AWS ARN, Azure Resource Manager IDs and Google Cloud resource names (see
// docs/structures.md, section Cloud resources and network users). The managed service hosts are in
// reader_cloud_hosts.go.

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

// awsPrefixes: AWS resource IDs (EC2 and VPC), prefix-hexadecimal with 8 or 17 characters (the long
// format, since 2018:
// aws.amazon.com/blogs/compute/longer-resource-ids-in-2018-for-amazon-ec2-amazon-ebs-and-amazon-vpc).
// The prefixes are those of the AWS CLI documentation examples (docs.aws.amazon.com/code-library,
// cli_2_ec2_code_examples). ami- and pl- stay out: there are public images and prefix lists, AWS's
// own.
var awsPrefixes = setOf("i", "vpc", "subnet", "sg", "eni", "vol", "snap", "rtb", "igw", "eigw", "nat", "acl",
	"vpce", "pcx", "tgw", "tgw-attach", "vgw", "cgw", "vpn", "dopt", "eipalloc", "eipassoc", "lt", "fl")

// findAWSIDs: the loose ID (vpc-0a1b2c3d4e5f67890), with the same pseudonym it has inside the ARN.
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
				add(ObjFinding{a, j, "service", "aws-id", true})
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

// arn:partition:service:region:account:resource
// (https://docs.aws.amazon.com/IAM/latest/UserGuide/reference-arns.html)
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
	var cs [5]int // positions of the ":" (after arn, partition, service, region and account)
	p := i + 3
	cs[0] = p
	for k := 1; k < 5; k++ {
		q := p + 1
		// region and account can be a wildcard in a policy (arn:aws:glue:*:123456789012:...)
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
		add(ObjFinding{cs[3] + 1, cs[4], "cloud_account", "arn", true})
	}
	if account == "aws" {
		return // AWS-managed resource (arn:aws:iam::aws:policy/...): public
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
	cut := func(v string) int { // end of the first piece
		if k := strings.IndexAny(v, "/:"); k >= 0 {
			return k
		}
		return len(v)
	}
	switch svc {
	case "s3":
		piece(ra, ra+cut(rec), "bucket")
	case "sqs", "sns":
		piece(ra, ra+cut(rec), "queue")
	case "dynamodb":
		if strings.HasPrefix(rec, "table/") {
			a := ra + 6
			piece(a, a+cut(s[a:rb]), "table")
		}
	default:
		// type/name or type:name (the last piece is the name; "role/path/name")
		k := cut(rec)
		if k == len(rec) {
			piece(ra, rb, "service")
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
		ent := "service"
		if svc == "iam" && typ == "user" {
			ent = "user"
		}
		piece(a, b, ent)
	}
}

// /subscriptions/<guid>/resourceGroups/<rg>/providers/<Ns>/<type>/<name>[/<subtype>/<subname>]...
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
				add(ObjFinding{ps[1][0], ps[1][1], "cloud_account", "azure", true})
			}
		}
		k := 2
		if len(ps) >= 4 && strings.EqualFold(sgm(2), "resourceGroups") {
			if v := sgm(3); simpleName(v) && !publicDev(v) {
				// resource group: groups resources inside the subscription (like a namespace)
				add(ObjFinding{ps[3][0], ps[3][1], "namespace", "azure", true})
			}
			k = 4
		}
		if len(ps) >= k+4 && strings.EqualFold(sgm(k), "providers") {
			// Ns, then type/name pairs
			for t := k + 2; t+1 < len(ps); t += 2 {
				if v := sgm(t + 1); simpleName(v) && !publicDev(v) {
					ent := entAzureType[strings.ToLower(sgm(t))]
					if ent == "" {
						ent = "service"
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

// Azure resource type (the segment before the name) -> entity
var entAzureType = map[string]string{"servers": "server", "flexibleservers": "server", "managedinstances": "server",
	"databases": "database", "storageaccounts": "cloud_account", "containers": "bucket", "shares": "bucket",
	"filesystems": "bucket", "queues": "queue", "topics": "queue", "subscriptions": "queue", "eventhubs": "queue",
	"namespaces": "server", "managedclusters": "server", "virtualmachines": "server", "databaseaccounts": "server",
	"registries": "server", "redis": "server", "sites": "service", "vaults": "service", "workspaces": "service",
	"factories": "service", "accounts": "cloud_account", "tables": "table", "schemas": "schema"}

// Cloud URLs with a fixed structure:
//
//	https://<account>.(blob|dfs|file|queue|table).core.windows.net/<container>/...   Azure Storage
//	sb://<namespace>.servicebus.windows.net/ ... EntityPath=<queue>                Service Bus/Event Hubs
//	https://sqs.<region>.amazonaws.com/<account>/<queue>                           SQS
//
// (https://learn.microsoft.com/azure/storage/common/storage-account-overview#storage-account-endpoints,
// https://learn.microsoft.com/azure/service-bus-messaging/service-bus-dotnet-get-started-with-queues,
// https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-queue-message-identifiers.html)
var storageService = map[string]string{"blob": "bucket", "dfs": "bucket", "file": "bucket", "queue": "queue", "table": "table"}

func findCloudURLs(s string, add func(ObjFinding)) {
	findCloudHosts(s, add)
	if strings.Contains(s, "snowflake") {
		findSnowflake(s, add)
	}
	if strings.Contains(s, ".core.windows.net") {
		for i := strings.Index(s, ".core.windows.net"); i >= 0; {
			// <account>.<service>.core.windows.net
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
						add(ObjFinding{a, d, "cloud_account", "azure", true})
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
				add(ObjFinding{a, i, "server", "azure", true})
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
				add(ObjFinding{a, b, "queue", "azure", true})
			}
		}
	}
	if strings.Contains(s, "amazonaws.com/") {
		for i := strings.Index(s, "amazonaws.com/"); i >= 0; {
			// SQS only: sqs.<region>.amazonaws.com or queue.amazonaws.com
			h := s[max(0, i-40):i]
			if strings.Contains(h, "sqs.") || strings.HasSuffix(h, "queue.") {
				a := i + len("amazonaws.com/")
				b := a
				for b < len(s) && isDig(s[b]) {
					b++
				}
				if b-a == 12 && b < len(s) && s[b] == '/' {
					add(ObjFinding{a, b, "cloud_account", "aws", true})
					c := b + 1
					for c < len(s) && (isAlnum(s[c]) || s[c] == '-' || s[c] == '_' || s[c] == '.') {
						c++
					}
					if c > b+1 {
						add(ObjFinding{b + 1, c, "queue", "aws", true})
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

// Snowflake: <account>.snowflakecomputing.com (the account may come as org-account or with the
// region after it: xy12345.us-east-1), app.<org>-<account>.snowflakecomputing.com (Snowsight) and
// app.snowflake.com/<org>/<account>/.
func findSnowflake(s string, add func(ObjFinding)) {
	for i := strings.Index(s, ".snowflakecomputing.com"); i >= 0; {
		a := i
		for a > 0 && (isAlnum(s[a-1]) || s[a-1] == '-' || s[a-1] == '_' || s[a-1] == '.') {
			a--
		}
		if strings.HasPrefix(strings.ToLower(s[a:i]), "app.") { // Snowsight: the account comes after "app."
			a += 4
		}
		if d := strings.IndexByte(s[a:i], '.'); d >= 0 { // account.region
			i2 := a + d
			if i2 > a {
				add(ObjFinding{a, i2, "cloud_account", "snowflake", true})
			}
		} else if a < i && !publicDev(s[a:i]) {
			add(ObjFinding{a, i, "cloud_account", "snowflake", true})
		}
		j := strings.Index(s[i+1:], ".snowflakecomputing.com")
		if j < 0 {
			break
		}
		i += 1 + j
	}
	for i := strings.Index(s, "app.snowflake.com/"); i >= 0; {
		a := i + len("app.snowflake.com/")
		for k, ent := range []string{"organization", "cloud_account"} {
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

// projects/<p>/(topics|subscriptions|datasets|buckets|instances|...)/<name> and
// projects/<p>/locations/<l>/<type>/<name> (https://cloud.google.com/apis/design/resource_names)
var entGCPType = map[string]string{"topics": "queue", "subscriptions": "queue", "datasets": "schema", "buckets": "bucket",
	"tables": "table", "queues": "queue"}

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
		add(ObjFinding{ps[1][0], ps[1][1], "cloud_account", "gcp", true})
	}
	for ; k+1 < len(ps); k += 2 {
		ent, ok := entGCPType[sgm(k)]
		if !ok {
			ent = "service"
		}
		if v := sgm(k + 1); simpleName(v) && !publicDev(v) {
			add(ObjFinding{ps[k+1][0], ps[k+1][1], ent, "gcp", true})
		}
	}
}
