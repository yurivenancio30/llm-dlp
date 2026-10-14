package mask

import (
	"strings"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// Registry of all structure readers: the fixed ones and the ones that depend on the configuration.
// To add a format, write the reader in the family's file and register it here.

// readersOf: all readers for this configuration (the fixed ones, then the configuration ones).
func readersOf(cfg config.Config) []Reader {
	return append(defaultReaders(), configReaders(cfg)...)
}

// DefaultReaders: the fixed readers (for measurement tools).
func DefaultReaders() []Reader { return defaultReaders() }

// defaultReaders: the structure readers that come on.
func defaultReaders() []Reader {
	return []Reader{
		{Name: "sql", Find: findSQL, Public: publicSQL},
		{Name: "erro", Find: findObjectError, Public: publicSQL},
		{Name: "conexão", Find: findConnections, Public: publicConnection},
		{Name: "tabela", Find: findTablesObj, Public: publicSQL},
		{Name: "esquema", Find: findSchema, Public: publicSQL},
		{Name: "nome-contexto", Find: findNameByContext, Public: publicDev},
		{Name: "devops-yaml", Find: findStructuredDevops, Public: publicDevops},
		{Name: "terraform", Find: findTerraform, Public: publicDevops},
		{Name: "ansible-ini", Find: findAnsibleINI, Public: publicDevops},
		{Name: "bicep", Find: findBicep, Public: publicDevops},
		{Name: "jenkins", Find: findJenkins, Public: publicDevops},
		{Name: "k8s-dns", Find: findK8sDNS, Public: publicDevops},
		{Name: "imagem", Find: findImages, Public: publicDevops},
		{Name: "saída-cli", Find: findCLITable, Public: publicDevops},
		{Name: "ip-infra", Find: findInfraIP},
		{Name: "chave-valor", Find: findKeyValue, Public: publicDev},
		{Name: "endereço", Find: findAddresses, Public: publicDev},
		{Name: "git", Find: findGitSCP, Public: publicDev},
		{Name: "pacote", Find: findPackages, Public: publicDev},
		{Name: "caminho", Find: findPaths, Public: publicDev},
		{Name: "usuário de rede", Find: findNetworkUsers, Public: publicDev},
		{Name: "nuvem", Find: findCloud, Public: publicDev},
		{Name: "código-nome", Find: findCodeName, Public: publicDev},
		{Name: "código-chamada", Find: findCodeCall, Public: publicDev},
		{Name: "linha-de-comando", Find: findCLI, Public: publicDev},
		{Name: "caminho-sistema", Find: findPathsOutside, Public: publicDev},
		{Name: "url-nuvem", Find: findCloudURLs, Public: publicDev},
		{Name: "dsn", Find: findDSN, Public: publicConnection},
		{Name: "env-lista", Find: findEnvList, Public: publicDev},
		{Name: "placeholder", Find: findPlaceholder, Public: publicDev},
		{Name: "ip-interno", Find: findInternalIP},
	}
}

// configReaders: the readers turned on according to the configuration (public IP, terms).
func configReaders(cfg config.Config) []Reader {
	var ls []Reader
	if cfg.Objects.PublicIP {
		ls = append(ls, Reader{Name: "ip-público", Find: findPublicIP})
	}
	var ts []string
	for _, t := range cfg.Terms {
		for _, v := range t.Values {
			n := normTerm(v)
			if len(n) >= 3 && !strings.ContainsAny(v, " \t") {
				ts = append(ts, n)
			}
		}
	}
	if len(ts) > 0 {
		ls = append(ls, Reader{Name: "termo-embutido", Find: func(s string, add func(ObjFinding)) { findTerms(s, ts, add) }})
	}
	return ls
}
