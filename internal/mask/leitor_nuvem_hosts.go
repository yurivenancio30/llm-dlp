package mask

import "strings"

// Hosts de serviço gerenciado de nuvem: o domínio é do provedor (público) e o rótulo da
// esquerda é o nome que o cliente deu ao recurso. A forma de cada host é a documentada pelo
// provedor; o leitor mascara só o rótulo do cliente e deixa o domínio:
//
//	bd-pedidos.c9akciq32.us-east-1.rds.amazonaws.com        instância do RDS
//	dw-vendas.abc123.sa-east-1.redshift.amazonaws.com       cluster do Redshift
//	<bucket>.s3[.<região>|-<região>].amazonaws.com          bucket do S3 (endereço virtual)
//	<conta>.dkr.ecr.<região>.amazonaws.com                  conta da AWS no registro do ECR
//	<nome>.database.windows.net, <nome>.vault.azure.net ... recurso do Azure
//	dbc-xxxx.cloud.databricks.com, adb-N.N.azuredatabricks.net  workspace do Databricks
//	<bucket>.storage.googleapis.com, <projeto>.appspot.com  Google Cloud
//
// (https://docs.aws.amazon.com/general/latest/gr/rande.html,
// https://learn.microsoft.com/azure/azure-resource-manager/management/azure-services-resource-providers,
// https://docs.databricks.com/workspace/workspace-details.html)

// sufixo do host -> tipo do rótulo do cliente (o primeiro rótulo do host)
var cloudHosts = []struct{ suf, ent string }{
	{".rds.amazonaws.com", "servidor"},
	{".redshift.amazonaws.com", "servidor"},
	{".redshift-serverless.amazonaws.com", "servidor"},
	{".cache.amazonaws.com", "servidor"},
	{".es.amazonaws.com", "servidor"},
	{".elb.amazonaws.com", "servidor"},
	{".execute-api.", "servico"}, // <id>.execute-api.<região>.amazonaws.com
	{".database.windows.net", "servidor"},
	{".database.azure.com", "servidor"}, // <nome>.postgres|mysql.database.azure.com
	{".documents.azure.com", "servidor"},
	{".redis.cache.windows.net", "servidor"},
	{".vault.azure.net", "servico"},
	{".azurewebsites.net", "servico"},
	{".azurecr.io", "servidor"},
	{".openai.azure.com", "servico"},
	{".cognitiveservices.azure.com", "servico"},
	{".azuredatabricks.net", "servidor"},
	{".cloud.databricks.com", "servidor"},
	{".gcp.databricks.com", "servidor"},
	{".storage.googleapis.com", "bucket"},
	{".appspot.com", "conta_nuvem"},
}

// rótulos que fazem parte da forma do host, não do nome do cliente
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

// clientLabel: o primeiro rótulo do host que termina em s[i] (o começo do sufixo).
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

// S3 com endereço virtual: <bucket>.s3.amazonaws.com, <bucket>.s3.<região>.amazonaws.com,
// <bucket>.s3-<região>.amazonaws.com, <bucket>.s3-website[.-]<região>.amazonaws.com. O bucket
// pode ter pontos: é tudo antes de ".s3".
func findS3Virtual(s string, add func(ObjFinding)) {
	for i := strings.Index(s, ".s3"); i >= 0; {
		rest := s[i+3 : min(len(s), i+64)]
		if (strings.HasPrefix(rest, ".") || strings.HasPrefix(rest, "-")) && strings.Contains(rest, "amazonaws.com") {
			// o host vai até amazonaws.com sem espaço nem barra
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

// <conta>.dkr.ecr.<região>.amazonaws.com: a conta da AWS (12 dígitos).
func findECRHost(s string, add func(ObjFinding)) {
	for i := strings.Index(s, ".dkr.ecr."); i >= 0; {
		if i >= 12 && allDigits(s[i-12:i]) && (i == 12 || !isAlnum(s[i-13])) {
			add(ObjFinding{i - 12, i, "conta_nuvem", "nuvem-host", true})
		}
		j := strings.Index(s[i+1:], ".dkr.ecr.")
		if j < 0 {
			break
		}
		i += 1 + j
	}
}
