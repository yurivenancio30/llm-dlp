package mask

import (
	"regexp"
	"strings"
	"unicode"
)

// Software proof (see docs/structures.md, section Public only with proof): DataHub's database is
// called "datahub", Airflow's "airflow". The default name of a public software says nothing about
// the client, but once decided as a name (the DSN .../datahub) the conversation memory spread it
// through the whole conversation, including where it is the tool ("o datahub ingere metadados").
//
// The proof comes from the conversation itself, the way a compiler resolves a name: the word is
// tied to an external package when it is installed (pip/npm/brew/helm/go get... install), imported
// (import datahub, from datahub.x import, require('x'), import "github.com/org/x"), run as a public
// image (the two keys of publicPath) or executed as a program (RecordCommand). With the proof,
// neither the conversation memory nor the accumulated one (RAM and seen.json, findKnown) spreads the
// word; where a reader decides it (the DSN itself), it stays masked. After a restart, the proofs of a
// text that already went out are recorded when it is read back from sent.log (frozen).
//
// Two keys, fail closed: besides the structural proof, the word has to be the name of a public
// software (public_software.txt or a Docker Official Image). A real dictionary word
// (common_words.txt and dictionary_words.txt: atlas, apollo, polaris, kraken, nexus) never gets the
// proof: it is what a client codename usually is, and an internal package with that name is
// imported the same way. Measured: of the popular repository names that are a typical codename,
// none passes; datahub, airflow, superset, minio, trino, grafana pass.
//
// Shadowing (as in a compiler, the local definition hides the global one): if the conversation
// shows the name defined in the project (name = "x" in a manifest, "name": "x" in package.json,
// module .../x in go.mod, a folder x/__init__.py outside site-packages), it belongs to the owner
// and the proof does not count. Shadowing only blocks: matching too much never releases anything.
//
// A registered term always wins. An outdated list only stops proving (the word stays masked), it
// never releases.
//
// Cost: one pass per line; only a line with the right shape at the start (from, import, quotes,
// image, FROM, name, module) or with the install word goes through a regex.

var (
	reInstall = regexp.MustCompile(`\b(?:pip3?|pipx|uv(?:\s+pip)?|poetry|conda|mamba|npm|pnpm|yarn|bun|brew|apt(?:-get)?|apk|dnf|yum|gem|cargo|go|helm|choco|winget|snap)` +
		`(?:\s+-{1,2}[\w-]+(?:=\S+)?)*\s+(?:install|add|get|i)\b([^;&|#]*)`)
	reImportPy = regexp.MustCompile(`^(?:from[ \t]+([A-Za-z_]\w*)(?:\.\w+)*[ \t]+import\b|import[ \t]+([A-Za-z_]\w*(?:[ \t]*,[ \t]*[A-Za-z_]\w*)*))`)
	reImportJS = regexp.MustCompile(`(?:\brequire\(\s*|\bfrom\s+|^import\s+)['"](@?[\w.-]+)(?:/([\w.-]+))?[^'"]*['"]`)
	reImportGo = regexp.MustCompile(`"(?:[a-z0-9-]+\.)+[a-z]{2,}/([\w.-]+)/([\w.-]+)[^"]*"`)

	reManifestDef = regexp.MustCompile(`^(?:name[ \t]*=[ \t]*|"name"[ \t]*:[ \t]*|setup\([^)]*\bname[ \t]*=[ \t]*)["']@?(?:[\w.-]+/)?([\w.-]+)["']|^module[ \t]+\S*?([\w.-]+)[ \t]*$`)
	rePackageDef  = regexp.MustCompile(`(?:^|[\s/"'(])([\w.-]+)/__init__\.py`)
)

// lines: calls f with each line of s (without the line break), without allocating.
func lines(s string, f func(l string)) {
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			f(s)
			return
		}
		f(s[:i])
		s = s[i+1:]
	}
}

// softwareProofs: the words s ties to an external package (still without the second key).
func softwareProofs(s string) []string {
	var out []string
	add := func(vs ...string) {
		for _, v := range vs {
			if v = strings.ToLower(strings.Trim(v, `"'`)); len(v) >= 3 {
				out = append(out, v)
			}
		}
	}
	lines(s, func(l string) {
		t := strings.TrimLeft(l, " \t")
		if t == "" {
			return
		}
		// installed package: the name without version, extras, scope and registry; pieces only with
		// the whole public name (apache-airflow; acme-airflow does not prove "airflow")
		if strings.Contains(t, "install") || strings.Contains(t, " add ") || strings.Contains(t, " get ") || strings.Contains(t, " i ") {
			for _, m := range reInstall.FindAllStringSubmatch(t, -1) {
				for _, x := range strings.Fields(m[1]) {
					if strings.HasPrefix(x, "-") || strings.ContainsAny(x, "$<>{}") || fileExtensions[strings.ToLower(x[strings.LastIndexByte(x, '.')+1:])] {
						continue
					}
					x = strings.Trim(x, `"'`)
					if i := strings.IndexAny(x, "[=<>~!@:"); i > 0 {
						x = x[:i]
					}
					ps := strings.Split(strings.TrimPrefix(x, "@"), "/")
					name := ps[len(ps)-1]
					add(name)
					if len(ps) > 1 {
						add(ps[len(ps)-2]) // org/name (helm repo/chart, npm @org/name, go host/org/name)
					}
					if publicName(name) {
						add(valuePieces(name)...)
					}
				}
			}
		}
		if strings.HasPrefix(t, "from ") || strings.HasPrefix(t, "import ") {
			for _, m := range reImportPy.FindAllStringSubmatch(t, -1) {
				add(m[1])
				for _, x := range strings.Split(m[2], ",") {
					add(strings.TrimSpace(x))
				}
			}
		}
		// Go: import "host/org/name", with or without an alias, inside or outside the block
		if q := strings.IndexByte(t, '"'); q >= 0 && strings.Count(t, "/") >= 2 && (q == 0 || strings.HasPrefix(t, "import ") || aliasShape(t[:q])) {
			for _, m := range reImportGo.FindAllStringSubmatch(t, -1) {
				add(m[1], m[2])
			}
		}
		if strings.Contains(t, "require(") || strings.Contains(t, "from '") || strings.Contains(t, `from "`) || strings.HasPrefix(t, "import ") {
			for _, m := range reImportJS.FindAllStringSubmatch(t, -1) {
				if !strings.HasPrefix(m[1], ".") {
					add(strings.TrimPrefix(m[1], "@"), m[2])
				}
			}
		}
		// public image with both keys (redis, bitnami/redis, acryldata/datahub-gms)
		if strings.Contains(t, "image") || strings.HasPrefix(t, "FROM") {
			for _, m := range reAnyImage.FindAllStringSubmatch(t, -1) {
				ref, _, _ := strings.Cut(m[1], "@")
				ps := strings.Split(ref, "/")
				if len(ps) > 1 && strings.ContainsAny(ps[0], ".:") {
					if !publicRegistries[strings.ToLower(ps[0])] {
						continue
					}
					ps = ps[1:]
				}
				ps[len(ps)-1], _, _ = strings.Cut(ps[len(ps)-1], ":")
				name := ps[len(ps)-1]
				if len(ps) == 1 && officialImages[strings.ToLower(name)] || len(ps) == 2 && publicOrg(ps[0]) && publicName(name) {
					add(name)
					add(valuePieces(name)...)
				}
			}
		}
	})
	return out
}

// aliasShape: what comes before the quote in a Go import with an alias ("es ", "_ ", ". ").
func aliasShape(p string) bool {
	p = strings.TrimSpace(p)
	if p == "_" || p == "." {
		return true
	}
	for i := 0; i < len(p); i++ {
		if !(isAlnum(p[i]) || p[i] == '_') {
			return false
		}
	}
	return p != ""
}

// localDefinitions: the names s defines in the project itself (see Shadowing above).
func localDefinitions(s string) []string {
	var out []string
	lines(s, func(l string) {
		t := strings.TrimLeft(l, " \t")
		if strings.HasPrefix(t, "name") || strings.HasPrefix(t, `"name"`) || strings.HasPrefix(t, "module ") || strings.HasPrefix(t, "setup(") {
			for _, m := range reManifestDef.FindAllStringSubmatch(strings.TrimRight(t, "\r,"), -1) {
				out = append(out, strings.ToLower(m[1]+m[2]))
			}
		}
		if strings.Contains(t, "__init__.py") {
			for _, m := range rePackageDef.FindAllStringSubmatchIndex(t, -1) {
				ini := strings.LastIndexAny(t[:m[2]], " \t\"'(") + 1
				if c := t[ini:m[2]]; !strings.Contains(c, "site-packages") && !strings.Contains(c, "dist-packages") {
					out = append(out, strings.ToLower(t[m[2]:m[3]]))
				}
			}
		}
	})
	return out
}

// dictionaryWord: v is a real word of one of the languages (the 50000 most frequent).
func dictionaryWord(v string) bool {
	return commonWord(v) || dictionaryWords[strings.ToLower(v)]
}

// recordSoftware: the proofs of s that pass the second key enter the Masker's set, and the local
// definitions of s, the set that cancels them (a fact about the world and about the project: it
// applies to any conversation, RAM only).
//
// Both sets are limited by the software list (only a name that could be proven or pass as a public
// dependency enters: a local definition of another name changes nothing) and never keep a client
// name. Shadowing is stored without - _ . (normSoftware): the manifest writes "vendas-core" and the
// package folder is vendas_core.
func (m *Masker) recordSoftware(s string) {
	for _, v := range localDefinitions(s) {
		if likely(v) || dependencyPackage(v) {
			m.localSoft.Store(normSoftware(v), true)
		}
	}
	for _, v := range softwareProofs(s) {
		m.prove(v)
	}
}

// likely: v can get the proof (second key and not a real word). A pure number is not a software
// name, even if there is a repository with that name (2048, 12306).
func likely(v string) bool {
	return len(v) >= 3 && strings.IndexFunc(v, unicode.IsLetter) >= 0 && !dictionaryWord(v) &&
		(publicSoftware[normSoftware(v)] || officialImages[v])
}

// prove: v enters as proven software if it can get the proof.
func (m *Masker) prove(v string) {
	if likely(v) {
		m.soft.Store(v, true)
	}
}

// provenSoftware: v (whole word, any case) is public software with structural proof, is not defined
// in the project (shadowing) and does not contain a registered term.
func (m *Masker) provenSoftware(v string) bool {
	l := strings.ToLower(v)
	if _, ok := m.soft.Load(l); !ok {
		return false
	}
	_, local := m.localSoft.Load(normSoftware(l))
	return !local && !m.hasTerm(v)
}

var reCommandSnippet = regexp.MustCompile(`\|\||&&|[|;\n&()]`)

// programs that wrap another one (the real program comes after): sudo airflow ..., python -m
// datahub ..., npx superset ...
var wrappers = setOf("sudo", "time", "nohup", "env", "exec", "nice", "timeout", "xargs", "watch", "npx", "uvx", "bunx", "pipx", "run", "poetry", "pdm", "uv", "command")

// RecordCommand: the program a shell command runs (the first name of each pipeline piece, "airflow
// dags list", "datahub check ...", "python -m dagster ...") is a software proof, with the same two
// keys as recordSoftware. The proxy calls it with the input of each call of the shell tool (only it
// knows the text is a command).
func (m *Masker) RecordCommand(cmd string) {
	for _, sgm := range reCommandSnippet.Split(cmd, -1) {
		ts := strings.Fields(sgm)
		for i := 0; i < len(ts); i++ {
			t := ts[i]
			switch {
			case strings.Contains(t, "=") && !strings.HasPrefix(t, "-"), strings.HasPrefix(t, "-"), wrappers[t], len(t) > 0 && t[0] >= '0' && t[0] <= '9':
				continue // VAR=x, option, wrapper, number (timeout 30)
			case (t == "python" || t == "python3") && i+2 < len(ts) && ts[i+1] == "-m":
				t = ts[i+2]
			}
			if j := strings.LastIndexByte(t, '/'); j >= 0 {
				t = t[j+1:]
			}
			m.prove(strings.ToLower(strings.Trim(t, "\"'")))
			break
		}
	}
}
