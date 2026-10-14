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

// Gerador da referência pública. Só roda com LLM_DLP_GERAR_REF=1 e lê só o
// material público da máquina, os diretórios de LLM_DLP_CORPUS (código e documentação de
// terceiros: módulos Go, bibliotecas Python, /usr/share/doc). Grava ref_publica.txt e
// tipos_linguagem.txt (vocab_gerado.go os lê com go:embed). Nada é escrito à mão: para mudar a
// lista, muda-se o critério aqui e gera-se de novo.
//
//	LLM_DLP_GERAR_REF=1 LLM_DLP_CORPUS=dir1:dir2 go test ./internal/mask -run TestGeneratePublicRef -v
//
// Critério (ref_publica.txt): uma palavra entra quando aparece em posição de nome de recurso em
// pelo menos minRefProjects projetos distintos do corpus. Posição de nome, pela gramática:
//   - valor inteiro de uma chave cujo último pedaço é palavra de tipo (entKey: namespace,
//     schema, table, bucket, app, service, user...) ou "name", em YAML, JSON, TOML, INI, .env
//     e argumentos nomeados ("namespace: x", "\"schema\": \"x\"", "bucket = 'x'"); em código
//     (.go, .py), só o literal de texto ("name = x" é atribuição de variável);
//   - valor de uma opção de linha de comando com palavra de tipo ("--namespace x", "--schema=x");
//   - nome depois de FROM, JOIN, INTO, UPDATE, TABLE, SCHEMA, DATABASE no SQL (cada parte de um
//     nome qualificado; em minúsculas, só o nome qualificado e fora de "from x.y import");
//   - primeiro rótulo do host (api.exemplo.org) e primeiro pedaço do caminho de uma URL;
//   - organização e nome de uma imagem de container (image:, FROM) sem registro ou de registro
//     público (bitnami/redis, grafana/loki, redis).
//
// Projeto: o módulo (o caminho até o pedaço com "@" nos módulos Go) ou o primeiro diretório
// abaixo da raiz do corpus (pacote Python, pasta de /usr/share/doc). Um nome que só um projeto
// usa nunca entra; um nome interno de empresa não aparece em projetos públicos distintos.
//
// Critério (tipos_linguagem.txt): um tipo do vocabulário de tipos de dado (dataTypes) entra
// quando é tipo de campo ou de variável ("\tnome    tipo") em código Go de pelo menos
// minTypeProjects projetos distintos.

const (
	minRefProjects  = 8
	minTypeProjects = 5
)

// readCorpus: o arquivo, descompactado se for .gz (no máximo 2 MB).
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
	if os.Getenv("LLM_DLP_GERAR_REF") == "" || os.Getenv("LLM_DLP_CORPUS") == "" {
		t.Skip("LLM_DLP_GERAR_REF e LLM_DLP_CORPUS não definidos")
	}
	dirs := strings.Split(os.Getenv("LLM_DLP_CORPUS"), ":")
	names := map[string]map[string]bool{} // palavra -> projetos
	types := map[string]map[string]bool{} // tipo -> projetos
	projects := map[string]bool{}
	officialOnes := map[string]map[string]bool{} // pasta-mãe -> imagens
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
			// Imagens Oficiais do Docker: no docker-library/docs, uma pasta por imagem com
			// content.md e metadata.json
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
			// páginas de manual (.1.gz, .8.gz...) e documentação compactada também contam
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
		return fmt.Sprintf("# GERADO por TestGerarRefPublica (internal/mask/vocab_gerar_test.go). Não edite à mão.\n"+
			"# %s\n# Gerado em %s, no material público da máquina: %s\n# (%d arquivos, %.0f MB, %d projetos distintos).\n# Critério: %s\n",
			what, data, strings.Join(originV, ", "), fileList, float64(bytes)/(1<<20), len(projects), crit)
	}
	saveIt := func(file, header string, ws []string) {
		if err := os.WriteFile(file, []byte(header+strings.Join(ws, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	saveIt("dados/ref_publica.txt", hdr("Referência pública: palavras muito frequentes como nome de recurso no material público.",
		fmt.Sprintf("palavra em posição de nome em pelo menos %d projetos distintos. %d palavras.\n"+
			"# Posição de nome: valor de chave de tipo (entChave) ou \"name\" em YAML/JSON/TOML/INI/.env (em código, só\n"+
			"# literal de texto); opção --tipo; nome depois de FROM/JOIN/INTO/UPDATE/TABLE/SCHEMA/DATABASE no SQL (em\n"+
			"# minúsculas, só nome qualificado); primeiro rótulo do host e primeiro pedaço do caminho de uma URL;\n"+
			"# organização e nome de imagem de container sem registro ou de registro público.",
			minRefProjects, len(ref))), ref)
	saveIt("dados/tipos_linguagem.txt", hdr("Tipos de dado (vocab_tipos.go) que também são tipo de linguagem.",
		fmt.Sprintf("tipo de campo ou de variável (\"\\tnome    tipo\") em código Go de pelo menos %d projetos distintos. %d tipos.",
			minTypeProjects, len(tl))), tl)
	// a pasta-mãe com mais imagens (o docker-library/docs); com menos de 50 não é ele
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
		saveIt("dados/imagens_oficiais.txt", hdr("Imagens Oficiais do Docker (softwareDoTexto, leitor_kubernetes.go).",
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

// corpusProject: o projeto de um arquivo do corpus (módulo Go até o "@", senão o primeiro
// pedaço do caminho abaixo da raiz, sem a extensão).
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

// nameValue: forma de nome de recurso (uma palavra, letras, dígitos, "_" e "-", começa com
// letra, 2 a 63 caracteres, não é número nem versão).
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

// nameKey: a chave diz que o valor é nome de recurso (palavra de tipo ou "name").
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

// refNamesInLine: os valores em posição de nome numa linha. codigo: a linha é de código-fonte
// (.go, .py), onde só o literal de texto conta como valor ("name = x" é atribuição).
func refNamesInLine(l string, codeValue bool, fn func(v string)) {
	if strings.TrimSpace(l) == "" {
		return
	}
	// chave: valor / chave = valor, em qualquer ponto da linha (YAML, JSON, TOML, INI, .env,
	// argumento nomeado: schema='x')
	for i := 1; i < len(l); i++ {
		c := l[i]
		if c != ':' && c != '=' {
			continue
		}
		if i+1 < len(l) && strings.IndexByte(":=>", l[i+1]) >= 0 || strings.IndexByte(":=!<>", l[i-1]) >= 0 {
			continue // ::, :=, ==, =>, !=, <=, >=
		}
		// a chave: identificador logo antes (com aspas e espaços)
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
		// o valor: literal entre aspas, ou palavra solta (fora de código)
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
		// SQL: FROM a.b, JOIN "c" (em minúsculas, só nome qualificado e fora de "from x import")
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
	// imagem de container (image:, FROM) num registro público ou sem registro: a organização e o
	// nome (bitnami/redis, grafana/loki, redis); registro privado não conta
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
	// URL: o primeiro rótulo do host (api.exemplo.com) e o primeiro pedaço do caminho (/api/...)
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

// goFieldType: o tipo de uma linha "\tnome    tipo" (campo de struct ou variável em bloco)
// quando é do vocabulário de tipos de dado; "" se não.
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

// Software público (TestGeneratePublicSoftware): os repositórios públicos populares do GitHub,
// numa lista JSONL ({"r": "dono/nome", "s": estrelas}) baixada com a busca da API (gh api
// search/repositories, fatiada por faixa de estrelas; ver docs/pt-BR/estruturas.md). Grava:
//   - software_publico.txt: o nome de cada repositório com pelo menos minSoftwareStars
//     estrelas (airflow, datahub, minio, kube-rbac-proxy), normalizado (normSoftware);
//   - fornecedores.txt: o dono de cada um (apache, bitnami, confluentinc, grafana), normalizado.
//
// Só servem juntos, na regra de imagem (publicPath): org/nome fica em claro quando a
// organização é fornecedor E o nome é software público. Sozinha, a lista não libera nada: nome
// de repositório popular inclui codinome típico de cliente (atlas, hermes, phoenix).
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
		return fmt.Sprintf("# GERADO por TestGerarSoftwarePublico (internal/mask/vocab_gerar_test.go). Não edite à mão.\n"+
			"# %s\n# Gerado em %s, dos repositórios públicos do GitHub com pelo menos %d estrelas (%d repositórios).\n"+
			"# %d nomes. Normalizado: minúsculas, sem - _ . (normSoftware).\n", what, time.Now().Format("2006-01-02"), minSoftwareStars, n, k)
	}
	ns, ds := list(names), list(owners)
	for file, content := range map[string]string{
		"dados/software_publico.txt": hdr("Software público: nome de repositório popular (só com fornecedores.txt, na regra de imagem).", len(ns)) + strings.Join(ns, "\n") + "\n",
		"dados/fornecedores.txt":     hdr("Fornecedores: dono de repositório popular (só com software_publico.txt, na regra de imagem).", len(ds)) + strings.Join(ds, "\n") + "\n",
	} {
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("%d repositórios; software_publico: %d; fornecedores: %d", n, len(ns), len(ds))
}

// Dicionário (TestGenerateDictionary): as 50000 palavras mais frequentes de cada idioma no
// FrequencyWords (en e pt_br, OpenSubtitles 2018, CC BY-SA 4.0), só letras, 2 ou mais,
// minúsculas, menos as que já estão em palavras_comuns.txt (as 20000 primeiras). Serve à prova
// de software (memoria_software.go): palavra real (polaris, kraken, nexus) é o que um codinome de
// cliente costuma ser, e nunca recebe a prova. LLM_DLP_FREQWORDS: a pasta com en_50k.txt e
// pt_br_50k.txt (github.com/hermitdave/FrequencyWords, content/2018/<idioma>/).
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
	hdr := "# GERADO por TestGerarDicionario (internal/mask/vocab_gerar_test.go) a partir de FrequencyWords\n" +
		"# (https://github.com/hermitdave/FrequencyWords, commit 525f9b5, content/2018/en/en_50k.txt e\n" +
		"# content/2018/pt_br/pt_br_50k.txt), de Hermit Dave, com dados do OpenSubtitles 2018. Licença do conteúdo:\n" +
		"# CC BY-SA 4.0 (https://creativecommons.org/licenses/by-sa/4.0/); este arquivo é uma adaptação sob a mesma licença.\n" +
		"# Adaptação: as 50000 palavras mais frequentes de cada idioma, só letras, 2 ou mais, minúsculas, menos as de\n" +
		fmt.Sprintf("# palavras_comuns.txt, em ordem alfabética. %d palavras. Não edite à mão: regenere com o mesmo critério.\n", len(out))
	if err := os.WriteFile("dados/palavras_dicionario.txt", []byte(hdr+strings.Join(out, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d palavras", len(out))
}
