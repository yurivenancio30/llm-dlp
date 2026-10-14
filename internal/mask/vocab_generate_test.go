package mask

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"
)

// Generator of the public reference. Only runs with LLM_DLP_GEN_REF=1 and reads only the machine's
// public material, the directories in LLM_DLP_CORPUS (third-party code and documentation: Go
// modules, Python libraries, /usr/share/doc). Writes public_ref.txt and language_types.txt
// (vocab_generated.go reads them with go:embed). Nothing is written by hand: to change the list,
// change the criterion here and generate again.
//
//	LLM_DLP_GEN_REF=1 LLM_DLP_CORPUS=dir1:dir2 go test ./internal/mask -run TestGeneratePublicRef -v
//
// Criterion (public_ref.txt): a word gets in when it appears in a resource name position in at
// least minRefProjects distinct projects of the corpus. Name position, by the grammar:
//   - whole value of a key whose last piece is a type word (entKey: namespace,
//     schema, table, bucket, app, service, user...) or "name", in YAML, JSON, TOML, INI, .env
//     and named arguments ("namespace: x", "\"schema\": \"x\"", "bucket = 'x'"); in code
//     (.go, .py), only the string literal ("name = x" is a variable assignment);
//   - value of a command-line option with a type word ("--namespace x", "--schema=x");
//   - name after FROM, JOIN, INTO, UPDATE, TABLE, SCHEMA, DATABASE in SQL (each part of a
//     qualified name; in lowercase, only the qualified name and outside "from x.y import");
//   - first label of the host (api.exemplo.org) and first piece of a URL's path;
//   - organization and name of a container image (image:, FROM) without a registry or from a
//     public registry (bitnami/redis, grafana/loki, redis).
//
// Project: the module (the path up to the piece with "@" in Go modules) or the first directory
// under the corpus root (Python package, /usr/share/doc folder). A name only one project uses never
// gets in; a company's internal name does not appear in distinct public projects.
//
// Criterion (language_types.txt): a type from the data type vocabulary (dataTypes) gets in when it
// is a field or variable type ("\tname type") in Go code from at least minTypeProjects distinct
// projects.

const (
	minRefProjects  = 8
	minTypeProjects = 5
)

// readCorpus: the file, decompressed if it is .gz (at most 2 MB).
func readCorpus(p string, gz bool) ([]byte, error) {
	if !gz {
		return os.ReadFile(p)
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(z, 2<<20))
}

func TestGeneratePublicRef(t *testing.T) {
	if os.Getenv("LLM_DLP_GEN_REF") == "" || os.Getenv("LLM_DLP_CORPUS") == "" {
		t.Skip("LLM_DLP_GEN_REF e LLM_DLP_CORPUS não definidos")
	}
	dirs := strings.Split(os.Getenv("LLM_DLP_CORPUS"), ":")
	names := map[string]map[string]bool{} // palavra -> projetos
	types := map[string]map[string]bool{} // tipo -> projetos
	projects := map[string]bool{}
	officialOnes := map[string]map[string]bool{} // parent folder -> images
	var fileList int
	var bytes int64
	exts := setOf(".go", ".py", ".md", ".rst", ".txt", ".yaml", ".yml", ".json", ".toml", ".sql", ".sh", ".cfg", ".ini", ".conf", ".env")
	for _, rootDir := range dirs {
		rootDir = filepath.Clean(rootDir)
		filepath.WalkDir(rootDir, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if e.IsDir() {
				if n := e.Name(); n == ".git" || n == "node_modules" || p == filepath.Join(rootDir, "cache") {
					return filepath.SkipDir
				}
				return nil
			}
			// Docker Official Images: in docker-library/docs, one folder per image with content.md
			// and metadata.json
			if e.Name() == "metadata.json" {
				if _, err := os.Stat(filepath.Join(filepath.Dir(p), "content.md")); err == nil {
					parent := filepath.Dir(filepath.Dir(p))
					if officialOnes[parent] == nil {
						officialOnes[parent] = map[string]bool{}
					}
					officialOnes[parent][strings.ToLower(filepath.Base(filepath.Dir(p)))] = true
				}
			}
			gz := strings.HasSuffix(p, ".gz")
			ext := strings.ToLower(filepath.Ext(strings.TrimSuffix(p, ".gz")))
			// manual pages (.1.gz, .8.gz...) and compressed documentation count too
			if !exts[ext] && !(gz && len(ext) == 2 && ext[1] >= '1' && ext[1] <= '9') && !(gz && (ext == ".md" || ext == ".txt")) &&
				!strings.Contains(p, "/locales/") && !strings.HasSuffix(p, ".catalog") {
				return nil
			}
			if i, err := e.Info(); err != nil || i.Size() >= 512<<10 {
				return nil
			}
			b, err := readCorpus(p, gz)
			if err != nil {
				return nil
			}
			proj := corpusProject(rootDir, p)
			projects[proj] = true
			fileList++
			bytes += int64(len(b))
			mark := func(m map[string]map[string]bool, v string) {
				v = strings.ToLower(v)
				if m[v] == nil {
					m[v] = map[string]bool{}
				}
				m[v][proj] = true
			}
			s := string(b)
			codeValue := strings.HasSuffix(p, ".go") || strings.HasSuffix(p, ".py")
			for _, l := range strings.Split(s, "\n") {
				refNamesInLine(l, codeValue, func(v string) { mark(names, v) })
				if strings.HasSuffix(p, ".go") {
					if tp := goFieldType(l); tp != "" {
						mark(types, tp)
					}
				}
			}
			return nil
		})
	}
	ref := refSelect(names, minRefProjects)
	tl := refSelect(types, minTypeProjects)
	originV := make([]string, len(dirs))
	home, _ := os.UserHomeDir()
	for i, d := range dirs {
		if home != "" && strings.HasPrefix(d, home) {
			d = "~" + strings.TrimPrefix(d, home)
		}
		originV[i] = d
	}
	data := time.Now().Format("2006-01-02")
	hdr := func(what, crit string) string {
		return fmt.Sprintf("# GERADO por TestGerarRefPublica (internal/mask/vocab_generate_test.go). Não edite à mão.\n"+
			"# %s\n# Gerado em %s, no material público da máquina: %s\n# (%d arquivos, %.0f MB, %d projetos distintos).\n# Critério: %s\n",
			what, data, strings.Join(originV, ", "), fileList, float64(bytes)/(1<<20), len(projects), crit)
	}
	saveIt := func(file, header string, ws []string) {
		if err := os.WriteFile(file, []byte(header+strings.Join(ws, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	saveIt("data/public_ref.txt", hdr("Referência pública: palavras muito frequentes como nome de recurso no material público.",
		fmt.Sprintf("palavra em posição de nome em pelo menos %d projetos distintos. %d palavras.\n"+
			"# Posição de nome: valor de chave de tipo (entChave) ou \"name\" em YAML/JSON/TOML/INI/.env (em código, só\n"+
			"# literal de texto); opção --tipo; nome depois de FROM/JOIN/INTO/UPDATE/TABLE/SCHEMA/DATABASE no SQL (em\n"+
			"# minúsculas, só nome qualificado); primeiro rótulo do host e primeiro pedaço do caminho de uma URL;\n"+
			"# organização e nome de imagem de container sem registro ou de registro público.",
			minRefProjects, len(ref))), ref)
	saveIt("data/language_types.txt", hdr("Tipos de dado (vocab_types.go) que também são tipo de linguagem.",
		fmt.Sprintf("tipo de campo ou de variável (\"\\tnome    tipo\") em código Go de pelo menos %d projetos distintos. %d tipos.",
			minTypeProjects, len(tl))), tl)
	// the parent folder with the most images (docker-library/docs); with fewer than 50 it is not it
	var img []string
	for _, ims := range officialOnes {
		if len(ims) >= 50 && len(ims) > len(img) {
			img = img[:0]
			for v := range ims {
				img = append(img, v)
			}
		}
	}
	if len(img) > 0 {
		sort.Strings(img)
		saveIt("data/official_images.txt", hdr("Imagens Oficiais do Docker (softwareDoTexto, reader_kubernetes.go).",
			fmt.Sprintf("pasta com content.md e metadata.json no docker-library/docs. %d imagens.", len(img))), img)
	}
	t.Logf("%d arquivos, %.0f MB, %d projetos; ref_publica: %d palavras; tipos_linguagem: %d tipos",
		fileList, float64(bytes)/(1<<20), len(projects), len(ref), len(tl))
	for _, w := range []string{"default", "kube-system", "public", "dbo", "api", "app"} {
		t.Logf("  %-12s %3d projetos", w, len(names[w]))
	}
}

func refSelect(m map[string]map[string]bool, min int) []string {
	var out []string
	for v, ps := range m {
		if len(ps) >= min {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// corpusProject: the project of a corpus file (Go module up to the "@", otherwise the first path
// piece under the root, without the extension).
func corpusProject(rootDir, p string) string {
	rel, err := filepath.Rel(rootDir, p)
	if err != nil {
		return p
	}
	ps := strings.Split(rel, string(filepath.Separator))
	for i, x := range ps {
		if strings.Contains(x, "@") {
			return strings.Join(ps[:i+1], "/")
		}
	}
	return rootDir + "/" + strings.TrimSuffix(ps[0], filepath.Ext(ps[0]))
}

// nameValue: resource name shape (one word, letters, digits, "_" and "-", starts with a letter, 2
// to 63 characters, not a number nor a version).
func nameValue(v string) bool {
	if len(v) < 2 || len(v) > 63 || !(v[0] >= 'a' && v[0] <= 'z' || v[0] >= 'A' && v[0] <= 'Z') {
		return false
	}
	for i := 0; i < len(v); i++ {
		if c := v[i]; !(isAlnum(c) || c == '_' || c == '-') {
			return false
		}
	}
	return !(hasDigit(v) && reVersionOrHash.MatchString(v))
}

// nameKey: the key says the value is a resource name (type word or "name").
func nameKey(k string) bool {
	if k == "" || len(k) > 64 {
		return false
	}
	for i := 0; i < len(k); i++ {
		if c := k[i]; !(isAlnum(c) || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	if e, _ := entKey(k); e != "" {
		return true
	}
	lastPos := strings.ToLower(k[strings.LastIndexAny(k, "._-")+1:])
	return lastPos == "name"
}

var sqlNameWords = setOf("from", "join", "into", "update", "table", "schema", "database")

// refNamesInLine: the values in name position on a line. codeValue: the line is source code (.go,
// .py), where only the string literal counts as a value ("name = x" is an assignment).
func refNamesInLine(l string, codeValue bool, fn func(v string)) {
	if strings.TrimSpace(l) == "" {
		return
	}
	// key: value / key = value, anywhere on the line (YAML, JSON, TOML, INI, .env, named argument:
	// schema='x')
	for i := 1; i < len(l); i++ {
		c := l[i]
		if c != ':' && c != '=' {
			continue
		}
		if i+1 < len(l) && strings.IndexByte(":=>", l[i+1]) >= 0 || strings.IndexByte(":=!<>", l[i-1]) >= 0 {
			continue // ::, :=, ==, =>, !=, <=, >=
		}
		// the key: identifier right before (with quotes and spaces)
		k := i
		for k > 0 && (l[k-1] == ' ' || l[k-1] == '\t') {
			k--
		}
		if k > 0 && (l[k-1] == '"' || l[k-1] == '\'') {
			k--
		}
		fk := k
		for k > 0 && (isAlnum(l[k-1]) || l[k-1] == '_' || l[k-1] == '-' || l[k-1] == '.') {
			k--
		}
		if k == fk || !nameKey(l[k:fk]) {
			continue
		}
		// the value: quoted literal, or a loose word (outside code)
		v := strings.TrimLeft(l[i+1:], " \t")
		var val string
		if v != "" && (v[0] == '"' || v[0] == '\'' || v[0] == '`') {
			if e := strings.IndexByte(v[1:], v[0]); e >= 0 {
				val = v[1 : 1+e]
			}
		} else if !codeValue {
			e := 0
			for e < len(v) && !strings.ContainsRune(" \t,;}])#", rune(v[e])) {
				e++
			}
			val = v[:e]
		}
		if nameValue(val) {
			fn(val)
		}
	}
	ws := strings.Fields(l)
	for i, w := range ws {
		// --tipo valor / --tipo=valor
		if strings.HasPrefix(w, "--") && len(w) > 3 {
			k, v, has := strings.Cut(w[2:], "=")
			if !has && i+1 < len(ws) {
				v = ws[i+1]
			}
			v = strings.Trim(v, `"'`)
			if nameValue(v) && nameKey(k) {
				fn(v)
			}
			continue
		}
		// SQL: FROM a.b, JOIN "c" (in lowercase, only a qualified name and outside "from x import")
		if lw := strings.ToLower(w); sqlNameWords[lw] && i+1 < len(ws) {
			q := strings.TrimRight(ws[i+1], ",;()")
			if w != strings.ToUpper(w) && (!strings.Contains(q, ".") || i+2 < len(ws) && ws[i+2] == "import") {
				continue
			}
			for _, p := range strings.Split(q, ".") {
				if p = strings.Trim(p, "\"`[]"); nameValue(p) && !publicSQL(p) {
					fn(p)
				}
			}
		}
	}
	// container image (image:, FROM) on a public registry or without a registry: the organization
	// and the name (bitnami/redis, grafana/loki, redis); a private registry does not count
	if m := reAnyImage.FindStringSubmatch(l); m != nil {
		ref, _, _ := strings.Cut(m[1], "@")
		ps := strings.Split(ref, "/")
		if len(ps) > 1 && (strings.ContainsAny(ps[0], ".:") || ps[0] == "localhost") {
			if !publicRegistries[strings.ToLower(ps[0])] {
				ps = nil
			} else {
				ps = ps[1:]
			}
		}
		if len(ps) > 0 && len(ps) <= 2 {
			ps[len(ps)-1], _, _ = strings.Cut(ps[len(ps)-1], ":")
			for _, p := range ps {
				if p != "library" && nameValue(p) {
					fn(p)
				}
			}
		}
	}
	// URL: the first label of the host (api.exemplo.com) and the first piece of the path (/api/...)
	for o := strings.Index(l, "://"); o >= 0; {
		r := l[o+3:]
		e := 0
		for e < len(r) && !strings.ContainsRune(" \t\"'<>()[]{},", rune(r[e])) {
			e++
		}
		u := r[:e]
		host, pth, _ := strings.Cut(u, "/")
		host, _, _ = strings.Cut(host, ":")
		if ps := strings.Split(host, "."); len(ps) >= 3 && nameValue(ps[0]) {
			fn(ps[0])
		}
		pth, _, _ = strings.Cut(pth, "?")
		pth, _, _ = strings.Cut(pth, "#")
		for n, p := range strings.Split(pth, "/") {
			if n == 0 && nameValue(p) {
				fn(p)
			}
		}
		k := strings.Index(r[e:], "://")
		if k < 0 {
			break
		}
		o += 3 + e + k
	}
}

// goFieldType: the type of a "\tname type" line (struct field or variable in a block) when it is in
// the data type vocabulary; "" otherwise.
func goFieldType(l string) string {
	if !strings.HasPrefix(l, "\t") {
		return ""
	}
	if i := strings.Index(l, "//"); i >= 0 {
		l = l[:i]
	}
	f := strings.Fields(l)
	if len(f) < 2 {
		return ""
	}
	tp := f[len(f)-1]
	for _, n := range f[:len(f)-1] { // nomes: "a, b int"
		n = strings.TrimSuffix(n, ",")
		if n == "" || !(n[0] >= 'a' && n[0] <= 'z' || n[0] >= 'A' && n[0] <= 'Z' || n[0] == '_') || strings.ContainsAny(n, "(){}=:\"") {
			return ""
		}
	}
	if dataTypes[tp] {
		return tp
	}
	return ""
}

// Public software (TestGeneratePublicSoftware): GitHub's popular public repositories, in a JSONL
// list ({"r": "owner/name", "s": stars}) downloaded with the API search (gh api
// search/repositories, sliced by star range; see docs/structures.md). Writes:
//   - public_software.txt: the name of each repository with at least minSoftwareStars
//     stars (airflow, datahub, minio, kube-rbac-proxy), normalized (normSoftware);
//   - vendors.txt: the owner of each one (apache, bitnami, confluentinc, grafana), normalized.
//
// They only work together, in the image rule (publicPath): org/name stays in plain text when the
// organization is a vendor AND the name is public software. Alone, the list lets nothing through:
// popular repository names include typical customer codenames (atlas, hermes, phoenix).
const minSoftwareStars = 3000

func TestGeneratePublicSoftware(t *testing.T) {
	file := os.Getenv("LLM_DLP_GITHUB_TOP")
	if file == "" {
		t.Skip("LLM_DLP_GITHUB_TOP não definido")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	names, owners := map[string]bool{}, map[string]bool{}
	n := 0
	for _, l := range strings.Split(string(b), "\n") {
		var x struct {
			R string
			S int
		}
		if json.Unmarshal([]byte(l), &x) != nil || x.S < minSoftwareStars {
			continue
		}
		owner, name, ok := strings.Cut(x.R, "/")
		if !ok {
			continue
		}
		n++
		names[normSoftware(name)] = true
		owners[normSoftware(owner)] = true
	}
	list := func(m map[string]bool) []string {
		var out []string
		for v := range m {
			if len(v) >= 2 {
				out = append(out, v)
			}
		}
		sort.Strings(out)
		return out
	}
	hdr := func(what string, k int) string {
		return fmt.Sprintf("# GERADO por TestGerarSoftwarePublico (internal/mask/vocab_generate_test.go). Não edite à mão.\n"+
			"# %s\n# Gerado em %s, dos repositórios públicos do GitHub com pelo menos %d estrelas (%d repositórios).\n"+
			"# %d nomes. Normalizado: minúsculas, sem - _ . (normSoftware).\n", what, time.Now().Format("2006-01-02"), minSoftwareStars, n, k)
	}
	ns, ds := list(names), list(owners)
	for file, content := range map[string]string{
		"data/public_software.txt": hdr("Software público: nome de repositório popular (só com vendors.txt, na regra de imagem).", len(ns)) + strings.Join(ns, "\n") + "\n",
		"data/vendors.txt":         hdr("Fornecedores: dono de repositório popular (só com public_software.txt, na regra de imagem).", len(ds)) + strings.Join(ds, "\n") + "\n",
	} {
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("%d repositórios; software_publico: %d; fornecedores: %d", n, len(ns), len(ds))
}

// Dictionary (TestGenerateDictionary): the 50000 most frequent words of each language in
// FrequencyWords (en and pt_br, OpenSubtitles 2018, CC BY-SA 4.0), letters only, 2 or more,
// lowercase, minus the ones already in common_words.txt (the first 20000). Serves the software
// proof (memory_software.go): a real word (polaris, kraken, nexus) is what a customer codename
// usually is, and never gets the proof. LLM_DLP_FREQWORDS: the folder with en_50k.txt and
// pt_br_50k.txt (github.com/hermitdave/FrequencyWords, content/2018/<language>/).
func TestGenerateDictionary(t *testing.T) {
	dir := os.Getenv("LLM_DLP_FREQWORDS")
	if dir == "" {
		t.Skip("LLM_DLP_FREQWORDS não definido")
	}
	ws := map[string]bool{}
	for _, f := range []string{"en_50k.txt", "pt_br_50k.txt"} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(string(b), "\n") {
			w := strings.ToLower(strings.TrimSpace(strings.SplitN(strings.TrimSpace(l), " ", 2)[0]))
			if len(w) < 2 || commonWords[w] {
				continue
			}
			only := true
			for _, r := range w {
				only = only && unicode.IsLetter(r)
			}
			if only {
				ws[w] = true
			}
		}
	}
	var out []string
	for w := range ws {
		out = append(out, w)
	}
	sort.Strings(out)
	hdr := "# GERADO por TestGerarDicionario (internal/mask/vocab_generate_test.go) a partir de FrequencyWords\n" +
		"# (https://github.com/hermitdave/FrequencyWords, commit 525f9b5, content/2018/en/en_50k.txt e\n" +
		"# content/2018/pt_br/pt_br_50k.txt), de Hermit Dave, com dados do OpenSubtitles 2018. Licença do conteúdo:\n" +
		"# CC BY-SA 4.0 (https://creativecommons.org/licenses/by-sa/4.0/); este arquivo é uma adaptação sob a mesma licença.\n" +
		"# Adaptação: as 50000 palavras mais frequentes de cada idioma, só letras, 2 ou mais, minúsculas, menos as de\n" +
		fmt.Sprintf("# common_words.txt, em ordem alfabética. %d palavras. Não edite à mão: regenere com o mesmo critério.\n", len(out))
	if err := os.WriteFile("data/dictionary_words.txt", []byte(hdr+strings.Join(out, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d palavras", len(out))
}
