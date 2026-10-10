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
var hostsNuvem = []struct{ suf, ent string }{
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
var rotulosProvedor = map[string]bool{"s3": true, "s3-website": true, "dkr": true, "ecr": true, "www": true,
	"postgres": true, "mysql": true, "mariadb": true, "privatelink": true}

func acharHostsNuvem(s string, add func(ObjAchado)) {
	for _, h := range hostsNuvem {
		for i := strings.Index(s, h.suf); i >= 0; {
			if a, b, ok := rotuloCliente(s, i); ok && (h.suf != ".execute-api." || strings.Contains(s[i:min(len(s), i+80)], ".amazonaws.com")) {
				add(ObjAchado{a, b, h.ent, "nuvem-host", true})
			}
			j := strings.Index(s[i+1:], h.suf)
			if j < 0 {
				break
			}
			i += 1 + j
		}
	}
	acharS3Virtual(s, add)
	acharECRHost(s, add)
}

// rotuloCliente: o primeiro rótulo do host que termina em s[i] (o começo do sufixo).
func rotuloCliente(s string, i int) (int, int, bool) {
	a := i
	for a > 0 && i-a < 253 && (ehAlnum(s[a-1]) || s[a-1] == '-' || s[a-1] == '.') {
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
	if v == "" || rotulosProvedor[strings.ToLower(v)] || reRegiao.MatchString(strings.ToLower(v)) || publicoDev(v) {
		return 0, 0, false
	}
	return a, b, true
}

// S3 com endereço virtual: <bucket>.s3.amazonaws.com, <bucket>.s3.<região>.amazonaws.com,
// <bucket>.s3-<região>.amazonaws.com, <bucket>.s3-website[.-]<região>.amazonaws.com. O bucket
// pode ter pontos: é tudo antes de ".s3".
func acharS3Virtual(s string, add func(ObjAchado)) {
	for i := strings.Index(s, ".s3"); i >= 0; {
		resto := s[i+3 : min(len(s), i+64)]
		if (strings.HasPrefix(resto, ".") || strings.HasPrefix(resto, "-")) && strings.Contains(resto, "amazonaws.com") {
			// o host vai até amazonaws.com sem espaço nem barra
			if k := strings.Index(resto, "amazonaws.com"); k >= 0 && strings.IndexAny(resto[:k], " /\n\t\"'") < 0 {
				a := i
				for a > 0 && (ehAlnum(s[a-1]) || s[a-1] == '-' || s[a-1] == '.') {
					a--
				}
				if v := s[a:i]; len(v) >= 3 && !publicoDev(v) {
					add(ObjAchado{a, i, "bucket", "nuvem-host", true})
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
func acharECRHost(s string, add func(ObjAchado)) {
	for i := strings.Index(s, ".dkr.ecr."); i >= 0; {
		if i >= 12 && todoDigitos(s[i-12:i]) && (i == 12 || !ehAlnum(s[i-13])) {
			add(ObjAchado{i - 12, i, "conta_nuvem", "nuvem-host", true})
		}
		j := strings.Index(s[i+1:], ".dkr.ecr.")
		if j < 0 {
			break
		}
		i += 1 + j
	}
}
