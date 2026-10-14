package mask

import "strings"

// Managed cloud service hosts: the domain belongs to the provider (public) and the leftmost label
// is the name the client gave to the resource. The shape of each host is the one documented by the
// provider; the reader masks only the client label and leaves the domain:
//
//	bd-pedidos.c9akciq32.us-east-1.rds.amazonaws.com        RDS instance
//	dw-vendas.abc123.sa-east-1.redshift.amazonaws.com       Redshift cluster
//	<bucket>.s3[.<region>|-<region>].amazonaws.com          S3 bucket (virtual address)
//	<account>.dkr.ecr.<region>.amazonaws.com                AWS account in the ECR registry
//	<name>.database.windows.net, <name>.vault.azure.net ... Azure resource
//	dbc-xxxx.cloud.databricks.com, adb-N.N.azuredatabricks.net  Databricks workspace
//	<bucket>.storage.googleapis.com, <project>.appspot.com  Google Cloud
//
// (https://docs.aws.amazon.com/general/latest/gr/rande.html,
// https://learn.microsoft.com/azure/azure-resource-manager/management/azure-services-resource-providers,
// https://docs.databricks.com/workspace/workspace-details.html)

// host suffix -> type of the client label (the first label of the host)
var cloudHosts = []struct{ suf, ent string }{
	{".rds.amazonaws.com", "server"},
	{".redshift.amazonaws.com", "server"},
	{".redshift-serverless.amazonaws.com", "server"},
	{".cache.amazonaws.com", "server"},
	{".es.amazonaws.com", "server"},
	{".elb.amazonaws.com", "server"},
	{".execute-api.", "service"}, // <id>.execute-api.<region>.amazonaws.com
	{".database.windows.net", "server"},
	{".database.azure.com", "server"}, // <name>.postgres|mysql.database.azure.com
	{".documents.azure.com", "server"},
	{".redis.cache.windows.net", "server"},
	{".vault.azure.net", "service"},
	{".azurewebsites.net", "service"},
	{".azurecr.io", "server"},
	{".openai.azure.com", "service"},
	{".cognitiveservices.azure.com", "service"},
	{".azuredatabricks.net", "server"},
	{".cloud.databricks.com", "server"},
	{".gcp.databricks.com", "server"},
	{".storage.googleapis.com", "bucket"},
	{".appspot.com", "cloud_account"},
}

// labels that are part of the host's shape, not of the client name
var providerLabels = map[string]bool{"s3": true, "s3-website": true, "dkr": true, "ecr": true, "www": true,
	"postgres": true, "mysql": true, "mariadb": true, "privatelink": true}

func findCloudHosts(s string, add func(ObjFinding)) {
	for _, h := range cloudHosts {
		for i := strings.Index(s, h.suf); i >= 0; {
			if a, b, ok := clientLabel(s, i); ok && (h.suf != ".execute-api." || strings.Contains(s[i:min(len(s), i+80)], ".amazonaws.com")) {
				add(ObjFinding{a, b, h.ent, "nuvem-host", true})
			}
			j := strings.Index(s[i+1:], h.suf)
			if j < 0 {
				break
			}
			i += 1 + j
		}
	}
	findS3Virtual(s, add)
	findECRHost(s, add)
}

// clientLabel: the first label of the host that ends at s[i] (the start of the suffix).
func clientLabel(s string, i int) (int, int, bool) {
	a := i
	for a > 0 && i-a < 253 && (isAlnum(s[a-1]) || s[a-1] == '-' || s[a-1] == '.') {
		a--
	}
	for a < i && s[a] == '.' {
		a++
	}
	if a == i {
		return 0, 0, false
	}
	b := a + strings.IndexByte(s[a:i]+".", '.')
	v := s[a:b]
	if v == "" || providerLabels[strings.ToLower(v)] || reRegion.MatchString(strings.ToLower(v)) || publicDev(v) {
		return 0, 0, false
	}
	return a, b, true
}

// S3 with a virtual address: <bucket>.s3.amazonaws.com, <bucket>.s3.<region>.amazonaws.com,
// <bucket>.s3-<region>.amazonaws.com, <bucket>.s3-website[.-]<region>.amazonaws.com. The bucket may
// have dots: it is everything before ".s3".
func findS3Virtual(s string, add func(ObjFinding)) {
	for i := strings.Index(s, ".s3"); i >= 0; {
		rest := s[i+3 : min(len(s), i+64)]
		if (strings.HasPrefix(rest, ".") || strings.HasPrefix(rest, "-")) && strings.Contains(rest, "amazonaws.com") {
			// the host goes up to amazonaws.com without spaces or slashes
			if k := strings.Index(rest, "amazonaws.com"); k >= 0 && strings.IndexAny(rest[:k], " /\n\t\"'") < 0 {
				a := i
				for a > 0 && (isAlnum(s[a-1]) || s[a-1] == '-' || s[a-1] == '.') {
					a--
				}
				if v := s[a:i]; len(v) >= 3 && !publicDev(v) {
					add(ObjFinding{a, i, "bucket", "nuvem-host", true})
				}
			}
		}
		j := strings.Index(s[i+1:], ".s3")
		if j < 0 {
			break
		}
		i += 1 + j
	}
}

// <account>.dkr.ecr.<region>.amazonaws.com: the AWS account (12 digits).
func findECRHost(s string, add func(ObjFinding)) {
	for i := strings.Index(s, ".dkr.ecr."); i >= 0; {
		if i >= 12 && allDigits(s[i-12:i]) && (i == 12 || !isAlnum(s[i-13])) {
			add(ObjFinding{i - 12, i, "cloud_account", "nuvem-host", true})
		}
		j := strings.Index(s[i+1:], ".dkr.ecr.")
		if j < 0 {
			break
		}
		i += 1 + j
	}
}
