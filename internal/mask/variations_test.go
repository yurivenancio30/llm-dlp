package mask

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// Variation generator applied to each reader's cases. Each case is a template with slots for names
// («1» «2» «3» = simple name, «Q» = qualified name) and for a comment («c»). The generator changes
// ONE dimension at a time from the base case: case, spaces/tabs/breaks between tokens, name
// quoting, comment in the middle, punctuation glued at the end, name with 2, 3 and 4 parts, name
// with $ # - . and accents, and several items on the same line. It checks both things: every name
// masked, and no extra template word masked. All names are made up.

type varCase struct {
	reader, model string
	spc           []string // alternative separators for the template's spaces
	quoted        []string // name quoting: `"` `'` "`" "[]"
	with          []string // comments in place of «c»
	punct         bool     // punctuation glued at the end
	parts         bool     // «Q» with 2, 3 and 4 parts
	chars         []string // name variants (in place of «1»)
	several       string   // separator to repeat the item on the same line ("" = no)
	caseless      bool     // the template's case cannot change (case-sensitive format)
	base          []string // own base names (a DNS name has no "_")
}

var (
	varSQLSpace   = []string{"\t", "  ", "\n", " \n\t"}
	varLineSpace  = []string{"\t", "  "}
	varQuotedSQL  = []string{`"`, "`", "[]"}
	varSQLComment = []string{" /* obs */ ", " -- obs\n", "\n-- obs\n"}
	varNames      = []string{"ped$hist01", "tmp#ped01", "ped-hist-01", "relatório_01", "ação_mês01"}
	varSQLNames   = []string{"ped$hist01", "tmp#ped01", "relatório_01", "ação_mês01"}
)

var varCases = []varCase{
	// SQL
	{reader: "sql", model: "SELECT a, b FROM «Q»«c»WHERE b = 1", spc: varSQLSpace, quoted: varQuotedSQL, with: varSQLComment, punct: true, parts: true, chars: varSQLNames, several: "; "},
	{reader: "sql", model: "SELECT a FROM «1» t JOIN «2» u ON t.id = u.id", spc: varSQLSpace, quoted: varQuotedSQL, with: varSQLComment, punct: true, chars: varSQLNames},
	{reader: "sql", model: "INSERT INTO «Q» (a, b) VALUES (1, 2)", spc: varSQLSpace, quoted: varQuotedSQL, parts: true, chars: varSQLNames, several: "; "},
	{reader: "sql", model: "UPDATE «Q»«c»SET a = 1 WHERE b = 2", spc: varSQLSpace, quoted: varQuotedSQL, with: varSQLComment, parts: true, chars: varSQLNames, several: "; "},
	{reader: "sql", model: "DELETE FROM «Q» WHERE id = 1", spc: varSQLSpace, quoted: varQuotedSQL, punct: true, parts: true, chars: varSQLNames, several: "; "},
	{reader: "sql", model: "CREATE TABLE «Q» (id INT, «2» VARCHAR(10))", spc: varSQLSpace, quoted: varQuotedSQL, parts: true, chars: varSQLNames, several: "; "},
	{reader: "sql", model: "TRUNCATE TABLE «Q»", spc: varSQLSpace, quoted: varQuotedSQL, punct: true, parts: true, chars: varSQLNames, several: "; "},
	{reader: "sql", model: "MERGE INTO «1» USING «2» ON «1».id = «2».id", spc: varSQLSpace, quoted: varQuotedSQL, chars: varSQLNames},
	{reader: "sql", model: "EXEC «Q» @a = 1", spc: varLineSpace, quoted: []string{"[]"}, parts: true, chars: varSQLNames, several: "; "},
	// error messages
	{reader: "error", model: `ERROR: relation "«Q»" does not exist`, punct: true, parts: true, chars: varSQLNames},
	{reader: "error", model: `Invalid object name '«Q»'`, punct: true, parts: true, chars: varSQLNames},
	{reader: "error", model: `Table '«Q»' doesn't exist`, punct: true, parts: true, chars: varSQLNames},
	// connection strings and URIs
	{reader: "connection", model: "Server=«1»;Database=«2»;User Id=«3»;", spc: []string{" "}, chars: []string{"srv-ped-01", "srv_ped01"}, several: " "},
	{reader: "connection", model: `String url = "jdbc:sqlserver://«1»:1433;databaseName=«2»;encrypt=true";`, spc: varLineSpace, punct: true, chars: []string{"srv-ped-01"}},
	{reader: "connection", model: `props.put("url", "jdbc:sqlserver://«1»;databaseName=«2»");`, spc: varLineSpace, chars: []string{"srv-ped-01"}},
	{reader: "connection", model: "postgresql://«3»@«1»:5432/«2»", punct: true, chars: []string{"srv-ped-01"}, several: " "},
	{reader: "connection", model: "urn:li:dataset:(urn:li:dataPlatform:mssql,«Q»,PROD)", parts: true, caseless: true, punct: true, several: " "},
	// table (query result)
	{reader: "table", model: "table_schema,table_name\n«1»,«2»\n«1»,«3»\n", spc: []string{"\t", ";", "|", " , "}, quoted: []string{`"`}, chars: varSQLNames},
	// key-value
	{reader: "key-value", model: "database: «1»«c»\nschema: «2»\ntable: «3»\n", spc: varLineSpace, quoted: []string{`"`, "'"}, with: []string{"  # obs", " # obs: x"}, chars: varNames},
	{reader: "key-value", model: "[banco]\nhost = «1»«c»\ndatabase = «2»\n", spc: varLineSpace, with: []string{" ; obs", " # obs"}, chars: []string{"srv-ped-01"}},
	{reader: "key-value", model: "database=«1» schema=«2» table=«3»", quoted: []string{`"`, "'"}, chars: varNames},
	{reader: "key-value", model: `{"database": "«1»", "schema": "«2»", "table": "«3»"}`, spc: varLineSpace, chars: varNames},
	// addresses and paths
	{reader: "address", model: "veja http://«1»:8080/api/v1", punct: true, chars: []string{"srv-ped-01"}, several: " e "},
	{reader: "path", model: "lendo /srv/«1»/«2»/saida.csv", caseless: true, punct: true, chars: []string{"ped-hist-01", "ped$hist01"}, several: " e "},
	// code
	{reader: "code-call", model: `df = spark.table("«Q»")«c»`, with: []string{"  # obs", " // obs", " /* obs */"}, caseless: true, spc: varLineSpace, quoted: []string{"'"}, parts: true, chars: varSQLNames, several: "; "},
	{reader: "code-call", model: `df = pd.read_sql_table("«1»", con, schema="«2»")`, caseless: true, spc: varLineSpace, chars: varSQLNames},
	// schema (name + data type)
	{reader: "schema", model: "root\n |-- «1»: string (nullable = true)\n |-- «2»: integer (nullable = true)\n", caseless: true, chars: varSQLNames},
	{reader: "schema", model: "«1»    object\n«2»     int64\n«3»    float64\ndtype: object\n", caseless: true, chars: varSQLNames},
	// devops: manifest, terraform, cloud, DSN, Kubernetes DNS, git
	{reader: "devops-yaml", model: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: «1»\n  namespace: «2»\n", spc: []string{"   "}, caseless: true, quoted: []string{`"`, "'"}, chars: []string{"svc-ped-01"}},
	{reader: "terraform", model: "resource \"aws_s3_bucket\" \"dados\" {\n  bucket = \"«1»\"\n}\n", spc: []string{"\t"}, caseless: true, chars: []string{"bkt-ped-01"}},
	{reader: "cloud-url", model: "copiando para s3://«1»/«2»/arquivo.parquet", caseless: true, punct: true, chars: []string{"bkt-ped-01"}, several: " e "},
	{reader: "dsn", model: `dsn := "«3»:senha@tcp(«1»:3306)/«2»"`, caseless: true, spc: varLineSpace, chars: []string{"srv-ped-01"}},
	{reader: "k8s-dns", model: "curl http://«1».«2».svc.cluster.local:8080/saude", caseless: true, punct: true, chars: []string{"svc-ped-01"}, several: " && ", base: []string{"xq-pedidos01", "xq-clientes02"}},
	{reader: "git", model: "git clone git@«1»:«2»/«3».git", caseless: true, punct: true, chars: []string{"srv-ped-01"}, several: " && ", base: []string{"xq-git01", "xq-org02", "xq-repo03"}},
	// command line
	{reader: "command-line", model: "psql -h «1» -d «2» -U «3»", caseless: true, spc: varLineSpace, quoted: []string{`"`, "'"}, chars: []string{"srv-ped-01"}, several: " && "},
	{reader: "command-line", model: "kubectl -n «1» get pods", spc: varLineSpace, caseless: true, chars: []string{"ns-ped-01"}, several: " && "},
}

// base names for each slot (made up)
var varBase = map[string]string{"1": "xq_pedidos01", "2": "xq_clientes02", "3": "xq_itens03"}

var varParts = []string{"xq_srv01", "xq_banco01", "xq_esq01", "xq_tab01"}

type varText struct {
	lbl, text string
	names     []string
	stays     []string
}

// cite puts the name inside the quoting (each part, in a qualified name, if byPart).
func cite(n, quoted string, byPart bool) string {
	if quoted == "" {
		return n
	}
	a, b := quoted, quoted
	if quoted == "[]" {
		a, b = "[", "]"
	}
	if !byPart {
		return a + n + b
	}
	ps := strings.Split(n, ".")
	for i := range ps {
		ps[i] = a + ps[i] + b
	}
	return strings.Join(ps, ".")
}

func varLetterCase(s string, mode int) string {
	switch mode {
	case 1:
		return strings.ToUpper(s)
	case 2:
		return strings.ToLower(s)
	case 3: // alternada: sElEcT
		r := []rune(s)
		for i := range r {
			if i%2 == 0 {
				r[i] = unicode.ToLower(r[i])
			} else {
				r[i] = unicode.ToUpper(r[i])
			}
		}
		return string(r)
	case 4: // each word with an uppercase initial: Select, Database
		r := []rune(strings.ToLower(s))
		for i := range r {
			if i == 0 || !unicode.IsLetter(r[i-1]) {
				r[i] = unicode.ToUpper(r[i])
			}
		}
		return string(r)
	}
	return s
}

var (
	rePlace       = regexp.MustCompile(`«[123Q]»`)
	reQuotedPlace = regexp.MustCompile(`"(«[123Q]»)"`)
)

// build: the case text with the given choices. Also returns the names and the template words that
// have to stay.
func build(c varCase, letterCase int, spc, quoted, with string, parts int, n1 string, several bool) varText {
	mod := c.model
	if strings.Contains(mod, "«c»") {
		if with == "" {
			with = " "
		}
		mod = strings.ReplaceAll(mod, "«c»", with)
	}
	if !c.caseless {
		// changes the case only outside the slots
		var b strings.Builder
		last := 0
		for _, ix := range rePlace.FindAllStringIndex(mod, -1) {
			b.WriteString(varLetterCase(mod[last:ix[0]], letterCase))
			b.WriteString(mod[ix[0]:ix[1]])
			last = ix[1]
		}
		b.WriteString(varLetterCase(mod[last:], letterCase))
		mod = b.String()
	}
	if spc != " " && spc != "" {
		mod = strings.ReplaceAll(mod, " ", spc)
		if c.reader == "table" { // in the table, the column separator
			mod = strings.ReplaceAll(c.model, ",", spc)
		}
	}
	stays := modelWords(mod)
	names := map[string]string{"1": varBase["1"], "2": varBase["2"], "3": varBase["3"]}
	for i, n := range c.base {
		names[fmt.Sprint(i+1)] = n
	}
	if n1 != "" {
		names["1"] = n1
	}
	q := names["1"]
	if n1 != "" {
		q = n1
	}
	if parts > 1 {
		q = strings.Join(append(append([]string{}, varParts[4-parts:3]...), q), ".")
	}
	// the template already has quotes around the slot (code, JSON): the quoting replaces those
	// quotes
	if quoted != "" {
		mod = reQuotedPlace.ReplaceAllString(mod, "$1")
	}
	byPart := c.reader == "sql" || c.reader == "table"
	var list []string
	text := rePlace.ReplaceAllStringFunc(mod, func(l string) string {
		k := l[2 : len(l)-2]
		n := q
		if k != "Q" {
			n = names[k]
		}
		list = append(list, strings.Split(n, ".")...)
		return cite(n, quoted, byPart)
	})
	if several {
		other := strings.NewReplacer("xq_", "xr_", "xq-", "xr-").Replace(text)
		for _, n := range list {
			list = append(list, strings.NewReplacer("xq_", "xr_", "xq-", "xr-").Replace(n))
		}
		text += c.several + other
	}
	return varText{text: text, names: list, stays: stays}
}

// modelWords: the template's words with 4 or more letters (outside the slots): they cannot be
// masked.
func modelWords(mod string) []string {
	var out []string
	for _, w := range regexp.MustCompile(`[A-Za-z]{4,}`).FindAllString(rePlace.ReplaceAllString(mod, " "), -1) {
		out = append(out, w)
	}
	return out
}

// variations: the base case and one dimension changed at a time.
func variations(c varCase) []varText {
	var out []varText
	add := func(lbl string, v varText) { v.lbl = c.reader + " | " + c.model + " | " + lbl; out = append(out, v) }
	add("base", build(c, 0, " ", "", "", 1, "", false))
	if !c.caseless {
		for k, name := range []string{"MAIÚSCULAS", "minúsculas", "aLtErNaDa", "Inicial"} {
			if k == 2 && c.reader != "sql" && c.reader != "error" {
				continue // alternating case only where the language is case-insensitive
			}
			add("caixa "+name, build(c, k+1, " ", "", "", 1, "", false))
		}
	}
	for _, e := range c.spc {
		add(fmt.Sprintf("space %q", e), build(c, 0, e, "", "", 1, "", false))
	}
	for _, q := range c.quoted {
		add("citação "+q, build(c, 0, " ", q, "", 1, "", false))
	}
	for _, cm := range c.with {
		add(fmt.Sprintf("comment %q", cm), build(c, 0, " ", "", cm, 1, "", false))
	}
	if c.punct {
		for _, p := range []string{".", ",", ";", ")", ":"} {
			v := build(c, 0, " ", "", "", 1, "", false)
			v.text = "(" + v.text + p + " ok"
			add("pontuação "+p, v)
		}
	}
	if c.parts {
		for n := 2; n <= 4; n++ {
			add(fmt.Sprintf("%d partes", n), build(c, 0, " ", "", "", n, "", false))
			if len(c.quoted) > 0 {
				add(fmt.Sprintf("%d parts quoted %s", n, c.quoted[0]), build(c, 0, " ", c.quoted[0], "", n, "", false))
			}
		}
	}
	for _, n := range c.chars {
		add("nome "+n, build(c, 0, " ", "", "", 1, n, false))
	}
	if c.several != "" {
		add("vários na linha", build(c, 0, " ", "", "", 1, "", true))
	}
	// escaped quotes: the whole text inside a JSON string (tool result)
	v := build(c, 0, " ", "", "", 1, "", false)
	j, _ := json.Marshal(map[string]string{"stdout": v.text})
	v.text = string(j)
	add("dentro de JSON (escapado)", v)
	return out
}

// restart: the masker goes back to knowing nothing (without redoing NewMasker, which is expensive).
func restart(m *Masker) {
	m.known = newKnown()
	m.weakSet = weakSet{}
	m.seenSet = nil
	m.mu.Lock()
	m.memo, m.stale = map[[32]byte]result{}, nil
	m.mu.Unlock()
}

func readable(out, v string) bool {
	return regexp.MustCompile(`(?i)(^|[^\p{L}0-9_$#])` + regexp.QuoteMeta(v) + `($|[^\p{L}0-9_$#])`).MatchString(out)
}

func TestReaderVariations(t *testing.T) {
	m := newTest(t)
	total, failures := 0, 0
	for _, c := range varCases {
		for _, v := range variations(c) {
			total++
			restart(m)
			out, _ := m.Mask(v.text)
			var clearOnes, surplus []string
			for _, n := range v.names {
				if readable(out, n) {
					clearOnes = append(clearOnes, n)
				}
			}
			for _, w := range v.stays {
				if !strings.Contains(strings.ToLower(out), strings.ToLower(w)) {
					surplus = append(surplus, w)
				}
			}
			if len(clearOnes)+len(surplus) > 0 {
				failures++
				t.Errorf("%s\n   text:   %q\n   output: %q\n   in plain text: %v  masked too much: %v", v.lbl, v.text, out, clearOnes, surplus)
			}
		}
	}
	t.Logf("%d variations, %d failures", total, failures)
}

// Portuguese prose with SQL snippets between backticks (and loose uppercase keywords): the table
// name is masked, the words of the sentence are not.
func TestProseWithSQLInBackticks(t *testing.T) {
	cases := []struct {
		text  string
		gone  []string
		stays []string
	}{
		{"Para conferir, rode `SELECT nome, idade FROM xq_clientes02` e veja se a coluna idade está preenchida; depois o `WHERE` filtra por data e o `JOIN` junta as tabelas.",
			[]string{"xq_clientes02"}, []string{"conferir", "rode", "veja", "coluna", "está", "preenchida", "depois", "filtra", "data", "junta", "tabelas"}},
		{"O SELECT pega os dados FROM da tabela certa e a gente confere depois.",
			nil, []string{"pega", "dados", "tabela", "certa", "gente", "confere", "depois"}},
		{"Use o comando `UPDATE` quando precisar, e o `DELETE FROM` só com cuidado; o UPDATE de ontem SET o campo errado.",
			nil, []string{"comando", "quando", "precisar", "cuidado", "ontem", "campo", "errado"}},
		{"Depois do `INSERT INTO xq_itens03` vem a lista de valores, que a gente monta no código.",
			[]string{"xq_itens03"}, []string{"Depois", "lista", "valores", "gente", "monta", "código"}},
		{"A consulta `SELECT * FROM xq_pedidos01 WHERE situacao = 'ok'` retorna os pedidos; a coluna situacao tem três valores e a ação seguinte é exportar.",
			[]string{"xq_pedidos01"}, []string{"consulta", "retorna", "pedidos", "coluna", "três", "valores", "ação", "seguinte", "exportar"}},
		{"Se o `CREATE TABLE` falhar, crie a tabela na mão e depois rode `DROP TABLE xq_tmp01;` para limpar.",
			[]string{"xq_tmp01"}, []string{"falhar", "crie", "tabela", "mão", "depois", "limpar"}},
		{"Vamos usar o banco certo: use xq_banco01 e depois exec a rotina; drop table permissions não existe.",
			nil, []string{"Vamos", "banco", "certo", "depois", "rotina", "permissions", "existe"}},
	}
	for _, c := range cases {
		m := newTest(t)
		out, _ := m.Mask(c.text)
		for _, n := range c.gone {
			if readable(out, n) {
				t.Errorf("%q stayed readable:\n%s", n, out)
			}
		}
		for _, w := range c.stays {
			if !strings.Contains(out, w) {
				t.Errorf("%q was masked (prose):\n%s", w, out)
			}
		}
	}
}
