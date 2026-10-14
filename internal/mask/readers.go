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
		{Name: "error", Find: findObjectError, Public: publicSQL},
		{Name: "connection", Find: findConnections, Public: publicConnection},
		{Name: "table", Find: findTablesObj, Public: publicSQL},
		{Name: "schema", Find: findSchema, Public: publicSQL},
		{Name: "name-context", Find: findNameByContext, Public: publicDev},
		{Name: "devops-yaml", Find: findStructuredDevops, Public: publicDevops},
		{Name: "terraform", Find: findTerraform, Public: publicDevops},
		{Name: "ansible-ini", Find: findAnsibleINI, Public: publicDevops},
		{Name: "bicep", Find: findBicep, Public: publicDevops},
		{Name: "jenkins", Find: findJenkins, Public: publicDevops},
		{Name: "k8s-dns", Find: findK8sDNS, Public: publicDevops},
		{Name: "image", Find: findImages, Public: publicDevops},
		{Name: "cli-output", Find: findCLITable, Public: publicDevops},
		{Name: "infra-ip", Find: findInfraIP},
		{Name: "key-value", Find: findKeyValue, Public: publicDev},
		{Name: "address", Find: findAddresses, Public: publicDev},
		{Name: "git", Find: findGitSCP, Public: publicDev},
		{Name: "package", Find: findPackages, Public: publicDev},
		{Name: "path", Find: findPaths, Public: publicDev},
		{Name: "network-user", Find: findNetworkUsers, Public: publicDev},
		{Name: "cloud", Find: findCloud, Public: publicDev},
		{Name: "code-name", Find: findCodeName, Public: publicDev},
		{Name: "code-call", Find: findCodeCall, Public: publicDev},
		{Name: "command-line", Find: findCLI, Public: publicDev},
		{Name: "system-path", Find: findPathsOutside, Public: publicDev},
		{Name: "cloud-url", Find: findCloudURLs, Public: publicDev},
		{Name: "dsn", Find: findDSN, Public: publicConnection},
		{Name: "env-list", Find: findEnvList, Public: publicDev},
		{Name: "placeholder", Find: findPlaceholder, Public: publicDev},
		{Name: "internal-ip", Find: findInternalIP},
	}
}

// configReaders: the readers turned on according to the configuration (public IP, terms).
func configReaders(cfg config.Config) []Reader {
	var ls []Reader
	if cfg.Objects.PublicIP {
		ls = append(ls, Reader{Name: "public-ip", Find: findPublicIP})
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
		ls = append(ls, Reader{Name: "embedded-term", Find: func(s string, add func(ObjFinding)) { findTerms(s, ts, add) }})
	}
	return ls
}
