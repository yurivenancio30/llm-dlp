package mask

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// Gerador da referência pública (P1, item D3). Só roda com LLM_DLP_GERAR_REF=1 e lê só o
// material público da máquina, os diretórios de LLM_DLP_CORPUS (código e documentação de
// terceiros: módulos Go, bibliotecas Python, /usr/share/doc). Grava ref_publica.txt e
// tipos_linguagem.txt (ref_publica.go os lê com go:embed). Nada é escrito à mão: para mudar a
// lista, muda-se o critério aqui e gera-se de novo.
//
//	LLM_DLP_GERAR_REF=1 LLM_DLP_CORPUS=dir1:dir2 go test ./internal/mask -run TestGerarRefPublica -v
//
// Critério (ref_publica.txt): uma palavra entra quando aparece em posição de nome de recurso em
// pelo menos minProjetosRef projetos distintos do corpus. Posição de nome, pela gramática:
//   - valor inteiro de uma chave cujo último pedaço é palavra de tipo (entChave: namespace,
//     schema, table, bucket, app, service, user...) ou "name", em YAML, JSON, TOML, INI, .env
//     e argumentos nomeados ("namespace: x", "\"schema\": \"x\"", "bucket = 'x'"); em código
//     (.go, .py), só o literal de texto ("name = x" é atribuição de variável);
//   - valor de uma opção de linha de comando com palavra de tipo ("--namespace x", "--schema=x");
//   - nome depois de FROM, JOIN, INTO, UPDATE, TABLE, SCHEMA, DATABASE no SQL (cada parte de um
//     nome qualificado; em minúsculas, só o nome qualificado e fora de "from x.y import");
//   - primeiro rótulo do host (api.exemplo.org) e primeiro pedaço do caminho de uma URL.
//
// Projeto: o módulo (o caminho até o pedaço com "@" nos módulos Go) ou o primeiro diretório
// abaixo da raiz do corpus (pacote Python, pasta de /usr/share/doc). Um nome que só um projeto
// usa nunca entra; um nome interno de empresa não aparece em projetos públicos distintos.
//
// Critério (tipos_linguagem.txt): um tipo do vocabulário de tipos de dado (tiposDado) entra
// quando é tipo de campo ou de variável ("\tnome    tipo") em código Go de pelo menos
// minProjetosTipo projetos distintos.

const (
	minProjetosRef  = 8
	minProjetosTipo = 5
)

func TestGerarRefPublica(t *testing.T) {
	if os.Getenv("LLM_DLP_GERAR_REF") == "" || os.Getenv("LLM_DLP_CORPUS") == "" {
		t.Skip("LLM_DLP_GERAR_REF e LLM_DLP_CORPUS não definidos")
	}
	dirs := strings.Split(os.Getenv("LLM_DLP_CORPUS"), ":")
	nomes := map[string]map[string]bool{} // palavra -> projetos
	tipos := map[string]map[string]bool{} // tipo -> projetos
	projetos := map[string]bool{}
	var arquivos int
	var bytes int64
	exts := conj(".go", ".py", ".md", ".rst", ".txt", ".yaml", ".yml", ".json", ".toml", ".sql", ".sh", ".cfg", ".ini", ".conf", ".env")
	for _, raiz := range dirs {
		raiz = filepath.Clean(raiz)
		filepath.WalkDir(raiz, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if e.IsDir() {
				if n := e.Name(); n == ".git" || n == "node_modules" || p == filepath.Join(raiz, "cache") {
					return filepath.SkipDir
				}
				return nil
			}
			if !exts[strings.ToLower(filepath.Ext(p))] {
				return nil
			}
			if i, err := e.Info(); err != nil || i.Size() >= 512<<10 {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			proj := projetoDoCorpus(raiz, p)
			projetos[proj] = true
			arquivos++
			bytes += int64(len(b))
			marca := func(m map[string]map[string]bool, v string) {
				v = strings.ToLower(v)
				if m[v] == nil {
					m[v] = map[string]bool{}
				}
				m[v][proj] = true
			}
			s := string(b)
			codigo := strings.HasSuffix(p, ".go") || strings.HasSuffix(p, ".py")
			for _, l := range strings.Split(s, "\n") {
				refNomesNaLinha(l, codigo, func(v string) { marca(nomes, v) })
				if strings.HasSuffix(p, ".go") {
					if tp := tipoDeCampoGo(l); tp != "" {
						marca(tipos, tp)
					}
				}
			}
			return nil
		})
	}
	ref := refSelecionar(nomes, minProjetosRef)
	tl := refSelecionar(tipos, minProjetosTipo)
	origem := make([]string, len(dirs))
	home, _ := os.UserHomeDir()
	for i, d := range dirs {
		if home != "" && strings.HasPrefix(d, home) {
			d = "~" + strings.TrimPrefix(d, home)
		}
		origem[i] = d
	}
	data := time.Now().Format("2006-01-02")
	cab := func(oque, crit string) string {
		return fmt.Sprintf("# GERADO por TestGerarRefPublica (internal/mask/ref_publica_gerar_test.go). Não edite à mão.\n"+
			"# %s\n# Gerado em %s, no material público da máquina: %s\n# (%d arquivos, %.0f MB, %d projetos distintos).\n# Critério: %s\n",
			oque, data, strings.Join(origem, ", "), arquivos, float64(bytes)/(1<<20), len(projetos), crit)
	}
	gravar := func(arq, cabecalho string, ws []string) {
		if err := os.WriteFile(arq, []byte(cabecalho+strings.Join(ws, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gravar("ref_publica.txt", cab("Referência pública: palavras muito frequentes como nome de recurso no material público.",
		fmt.Sprintf("palavra em posição de nome em pelo menos %d projetos distintos. %d palavras.\n"+
			"# Posição de nome: valor de chave de tipo (entChave) ou \"name\" em YAML/JSON/TOML/INI/.env (em código, só\n"+
			"# literal de texto); opção --tipo; nome depois de FROM/JOIN/INTO/UPDATE/TABLE/SCHEMA/DATABASE no SQL (em\n"+
			"# minúsculas, só nome qualificado); primeiro rótulo do host e primeiro pedaço do caminho de uma URL.",
			minProjetosRef, len(ref))), ref)
	gravar("tipos_linguagem.txt", cab("Tipos de dado (vocab_tipos.go) que também são tipo de linguagem.",
		fmt.Sprintf("tipo de campo ou de variável (\"\\tnome    tipo\") em código Go de pelo menos %d projetos distintos. %d tipos.",
			minProjetosTipo, len(tl))), tl)
	t.Logf("%d arquivos, %.0f MB, %d projetos; ref_publica: %d palavras; tipos_linguagem: %d tipos",
		arquivos, float64(bytes)/(1<<20), len(projetos), len(ref), len(tl))
	for _, w := range []string{"default", "kube-system", "public", "dbo", "api", "app"} {
		t.Logf("  %-12s %3d projetos", w, len(nomes[w]))
	}
}

func refSelecionar(m map[string]map[string]bool, min int) []string {
	var out []string
	for v, ps := range m {
		if len(ps) >= min {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// projetoDoCorpus: o projeto de um arquivo do corpus (módulo Go até o "@", senão o primeiro
// pedaço do caminho abaixo da raiz, sem a extensão).
func projetoDoCorpus(raiz, p string) string {
	rel, err := filepath.Rel(raiz, p)
	if err != nil {
		return p
	}
	ps := strings.Split(rel, string(filepath.Separator))
	for i, x := range ps {
		if strings.Contains(x, "@") {
			return strings.Join(ps[:i+1], "/")
		}
	}
	return raiz + "/" + strings.TrimSuffix(ps[0], filepath.Ext(ps[0]))
}

// valorDeNome: forma de nome de recurso (uma palavra, letras, dígitos, "_" e "-", começa com
// letra, 2 a 63 caracteres, não é número nem versão).
func valorDeNome(v string) bool {
	if len(v) < 2 || len(v) > 63 || !(v[0] >= 'a' && v[0] <= 'z' || v[0] >= 'A' && v[0] <= 'Z') {
		return false
	}
	for i := 0; i < len(v); i++ {
		if c := v[i]; !(ehAlnum(c) || c == '_' || c == '-') {
			return false
		}
	}
	return !(temDigito(v) && reVersaoOuHash.MatchString(v))
}

// chaveDeNome: a chave diz que o valor é nome de recurso (palavra de tipo ou "name").
func chaveDeNome(k string) bool {
	if k == "" || len(k) > 64 {
		return false
	}
	for i := 0; i < len(k); i++ {
		if c := k[i]; !(ehAlnum(c) || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	if e, _ := entChave(k); e != "" {
		return true
	}
	ult := strings.ToLower(k[strings.LastIndexAny(k, "._-")+1:])
	return ult == "name"
}

var palavrasNomeSQL = conj("from", "join", "into", "update", "table", "schema", "database")

// refNomesNaLinha: os valores em posição de nome numa linha. codigo: a linha é de código-fonte
// (.go, .py), onde só o literal de texto conta como valor ("name = x" é atribuição).
func refNomesNaLinha(l string, codigo bool, fn func(v string)) {
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
		for k > 0 && (ehAlnum(l[k-1]) || l[k-1] == '_' || l[k-1] == '-' || l[k-1] == '.') {
			k--
		}
		if k == fk || !chaveDeNome(l[k:fk]) {
			continue
		}
		// o valor: literal entre aspas, ou palavra solta (fora de código)
		v := strings.TrimLeft(l[i+1:], " \t")
		var val string
		if v != "" && (v[0] == '"' || v[0] == '\'' || v[0] == '`') {
			if e := strings.IndexByte(v[1:], v[0]); e >= 0 {
				val = v[1 : 1+e]
			}
		} else if !codigo {
			e := 0
			for e < len(v) && !strings.ContainsRune(" \t,;}])#", rune(v[e])) {
				e++
			}
			val = v[:e]
		}
		if valorDeNome(val) {
			fn(val)
		}
	}
	ws := strings.Fields(l)
	for i, w := range ws {
		// --tipo valor / --tipo=valor
		if strings.HasPrefix(w, "--") && len(w) > 3 {
			k, v, tem := strings.Cut(w[2:], "=")
			if !tem && i+1 < len(ws) {
				v = ws[i+1]
			}
			v = strings.Trim(v, `"'`)
			if valorDeNome(v) && chaveDeNome(k) {
				fn(v)
			}
			continue
		}
		// SQL: FROM a.b, JOIN "c" (em minúsculas, só nome qualificado e fora de "from x import")
		if lw := strings.ToLower(w); palavrasNomeSQL[lw] && i+1 < len(ws) {
			q := strings.TrimRight(ws[i+1], ",;()")
			if w != strings.ToUpper(w) && (!strings.Contains(q, ".") || i+2 < len(ws) && ws[i+2] == "import") {
				continue
			}
			for _, p := range strings.Split(q, ".") {
				if p = strings.Trim(p, "\"`[]"); valorDeNome(p) && !publicoSQL(p) {
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
		host, cam, _ := strings.Cut(u, "/")
		host, _, _ = strings.Cut(host, ":")
		if ps := strings.Split(host, "."); len(ps) >= 3 && valorDeNome(ps[0]) {
			fn(ps[0])
		}
		cam, _, _ = strings.Cut(cam, "?")
		cam, _, _ = strings.Cut(cam, "#")
		for n, p := range strings.Split(cam, "/") {
			if n == 0 && valorDeNome(p) {
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

// tipoDeCampoGo: o tipo de uma linha "\tnome    tipo" (campo de struct ou variável em bloco)
// quando é do vocabulário de tipos de dado; "" se não.
func tipoDeCampoGo(l string) string {
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
	if tiposDado[tp] {
		return tp
	}
	return ""
}
