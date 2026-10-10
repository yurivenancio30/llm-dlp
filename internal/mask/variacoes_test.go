package mask

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// Gerador de variações aplicado aos casos de cada leitor. Cada caso é um
// modelo com lugares para nomes («1» «2» «3» = nome simples, «Q» = nome qualificado) e para
// comentário («c»). O gerador muda UMA dimensão de cada vez a partir do caso base: caixa,
// espaços/tabs/quebras entre tokens, citação do nome, comentário no meio, pontuação colada no
// fim, nome com 2, 3 e 4 partes, nome com $ # - . e acento, e vários itens na mesma linha.
// Confere as duas coisas: todo nome mascarado, e nenhuma palavra do modelo mascarada a mais.
// Todos os nomes são fictícios.

type casoVar struct {
	leitor, modelo string
	esp            []string // separadores alternativos para os espaços do modelo
	cit            []string // citações do nome: `"` `'` "`" "[]"
	com            []string // comentários no lugar de «c»
	pont           bool     // pontuação colada no fim
	partes         bool     // «Q» com 2, 3 e 4 partes
	chars          []string // variantes de nome (no lugar de «1»)
	varios         string   // separador para repetir o item na mesma linha ("" = não)
	semCaixa       bool     // a caixa do modelo não pode mudar (formato sensível à caixa)
	base           []string // nomes base próprios (nome DNS não tem "_")
}

var (
	varEspSQL   = []string{"\t", "  ", "\n", " \n\t"}
	varEspLinha = []string{"\t", "  "}
	varCitSQL   = []string{`"`, "`", "[]"}
	varComSQL   = []string{" /* obs */ ", " -- obs\n", "\n-- obs\n"}
	varNomes    = []string{"ped$hist01", "tmp#ped01", "ped-hist-01", "relatório_01", "ação_mês01"}
	varNomesSQL = []string{"ped$hist01", "tmp#ped01", "relatório_01", "ação_mês01"}
)

var casosVar = []casoVar{
	// SQL
	{leitor: "sql", modelo: "SELECT a, b FROM «Q»«c»WHERE b = 1", esp: varEspSQL, cit: varCitSQL, com: varComSQL, pont: true, partes: true, chars: varNomesSQL, varios: "; "},
	{leitor: "sql", modelo: "SELECT a FROM «1» t JOIN «2» u ON t.id = u.id", esp: varEspSQL, cit: varCitSQL, com: varComSQL, pont: true, chars: varNomesSQL},
	{leitor: "sql", modelo: "INSERT INTO «Q» (a, b) VALUES (1, 2)", esp: varEspSQL, cit: varCitSQL, partes: true, chars: varNomesSQL, varios: "; "},
	{leitor: "sql", modelo: "UPDATE «Q»«c»SET a = 1 WHERE b = 2", esp: varEspSQL, cit: varCitSQL, com: varComSQL, partes: true, chars: varNomesSQL, varios: "; "},
	{leitor: "sql", modelo: "DELETE FROM «Q» WHERE id = 1", esp: varEspSQL, cit: varCitSQL, pont: true, partes: true, chars: varNomesSQL, varios: "; "},
	{leitor: "sql", modelo: "CREATE TABLE «Q» (id INT, «2» VARCHAR(10))", esp: varEspSQL, cit: varCitSQL, partes: true, chars: varNomesSQL, varios: "; "},
	{leitor: "sql", modelo: "TRUNCATE TABLE «Q»", esp: varEspSQL, cit: varCitSQL, pont: true, partes: true, chars: varNomesSQL, varios: "; "},
	{leitor: "sql", modelo: "MERGE INTO «1» USING «2» ON «1».id = «2».id", esp: varEspSQL, cit: varCitSQL, chars: varNomesSQL},
	{leitor: "sql", modelo: "EXEC «Q» @a = 1", esp: varEspLinha, cit: []string{"[]"}, partes: true, chars: varNomesSQL, varios: "; "},
	// mensagens de erro
	{leitor: "erro", modelo: `ERROR: relation "«Q»" does not exist`, pont: true, partes: true, chars: varNomesSQL},
	{leitor: "erro", modelo: `Invalid object name '«Q»'`, pont: true, partes: true, chars: varNomesSQL},
	{leitor: "erro", modelo: `Table '«Q»' doesn't exist`, pont: true, partes: true, chars: varNomesSQL},
	// strings de conexão e URIs
	{leitor: "conexão", modelo: "Server=«1»;Database=«2»;User Id=«3»;", esp: []string{" "}, chars: []string{"srv-ped-01", "srv_ped01"}, varios: " "},
	{leitor: "conexão", modelo: `String url = "jdbc:sqlserver://«1»:1433;databaseName=«2»;encrypt=true";`, esp: varEspLinha, pont: true, chars: []string{"srv-ped-01"}},
	{leitor: "conexão", modelo: `props.put("url", "jdbc:sqlserver://«1»;databaseName=«2»");`, esp: varEspLinha, chars: []string{"srv-ped-01"}},
	{leitor: "conexão", modelo: "postgresql://«3»@«1»:5432/«2»", pont: true, chars: []string{"srv-ped-01"}, varios: " "},
	{leitor: "conexão", modelo: "urn:li:dataset:(urn:li:dataPlatform:mssql,«Q»,PROD)", partes: true, semCaixa: true, pont: true, varios: " "},
	// tabela (resultado de consulta)
	{leitor: "tabela", modelo: "table_schema,table_name\n«1»,«2»\n«1»,«3»\n", esp: []string{"\t", ";", "|", " , "}, cit: []string{`"`}, chars: varNomesSQL},
	// chave-valor
	{leitor: "chave-valor", modelo: "database: «1»«c»\nschema: «2»\ntable: «3»\n", esp: varEspLinha, cit: []string{`"`, "'"}, com: []string{"  # obs", " # obs: x"}, chars: varNomes},
	{leitor: "chave-valor", modelo: "[banco]\nhost = «1»«c»\ndatabase = «2»\n", esp: varEspLinha, com: []string{" ; obs", " # obs"}, chars: []string{"srv-ped-01"}},
	{leitor: "chave-valor", modelo: "database=«1» schema=«2» table=«3»", cit: []string{`"`, "'"}, chars: varNomes},
	{leitor: "chave-valor", modelo: `{"database": "«1»", "schema": "«2»", "table": "«3»"}`, esp: varEspLinha, chars: varNomes},
	// endereços e caminhos
	{leitor: "endereço", modelo: "veja http://«1»:8080/api/v1", pont: true, chars: []string{"srv-ped-01"}, varios: " e "},
	{leitor: "caminho", modelo: "lendo /srv/«1»/«2»/saida.csv", semCaixa: true, pont: true, chars: []string{"ped-hist-01", "ped$hist01"}, varios: " e "},
	// código
	{leitor: "código-chamada", modelo: `df = spark.table("«Q»")«c»`, com: []string{"  # obs", " // obs", " /* obs */"}, semCaixa: true, esp: varEspLinha, cit: []string{"'"}, partes: true, chars: varNomesSQL, varios: "; "},
	{leitor: "código-chamada", modelo: `df = pd.read_sql_table("«1»", con, schema="«2»")`, semCaixa: true, esp: varEspLinha, chars: varNomesSQL},
	// esquema (nome + tipo de dado)
	{leitor: "esquema", modelo: "root\n |-- «1»: string (nullable = true)\n |-- «2»: integer (nullable = true)\n", semCaixa: true, chars: varNomesSQL},
	{leitor: "esquema", modelo: "«1»    object\n«2»     int64\n«3»    float64\ndtype: object\n", semCaixa: true, chars: varNomesSQL},
	// devops: manifesto, terraform, nuvem, DSN, DNS do Kubernetes, git
	{leitor: "devops-yaml", modelo: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: «1»\n  namespace: «2»\n", esp: []string{"   "}, semCaixa: true, cit: []string{`"`, "'"}, chars: []string{"svc-ped-01"}},
	{leitor: "terraform", modelo: "resource \"aws_s3_bucket\" \"dados\" {\n  bucket = \"«1»\"\n}\n", esp: []string{"\t"}, semCaixa: true, chars: []string{"bkt-ped-01"}},
	{leitor: "url-nuvem", modelo: "copiando para s3://«1»/«2»/arquivo.parquet", semCaixa: true, pont: true, chars: []string{"bkt-ped-01"}, varios: " e "},
	{leitor: "dsn", modelo: `dsn := "«3»:senha@tcp(«1»:3306)/«2»"`, semCaixa: true, esp: varEspLinha, chars: []string{"srv-ped-01"}},
	{leitor: "k8s-dns", modelo: "curl http://«1».«2».svc.cluster.local:8080/saude", semCaixa: true, pont: true, chars: []string{"svc-ped-01"}, varios: " && ", base: []string{"xq-pedidos01", "xq-clientes02"}},
	{leitor: "git", modelo: "git clone git@«1»:«2»/«3».git", semCaixa: true, pont: true, chars: []string{"srv-ped-01"}, varios: " && ", base: []string{"xq-git01", "xq-org02", "xq-repo03"}},
	// linha de comando
	{leitor: "linha-de-comando", modelo: "psql -h «1» -d «2» -U «3»", semCaixa: true, esp: varEspLinha, cit: []string{`"`, "'"}, chars: []string{"srv-ped-01"}, varios: " && "},
	{leitor: "linha-de-comando", modelo: "kubectl -n «1» get pods", esp: varEspLinha, semCaixa: true, chars: []string{"ns-ped-01"}, varios: " && "},
}

// nomes base de cada lugar (fictícios)
var varBase = map[string]string{"1": "xq_pedidos01", "2": "xq_clientes02", "3": "xq_itens03"}

var varPartes = []string{"xq_srv01", "xq_banco01", "xq_esq01", "xq_tab01"}

type varTexto struct {
	rot, texto string
	nomes      []string
	fica       []string
}

// citar põe o nome entre a citação (cada parte, num nome qualificado, se porParte).
func citar(n, cit string, porParte bool) string {
	if cit == "" {
		return n
	}
	a, b := cit, cit
	if cit == "[]" {
		a, b = "[", "]"
	}
	if !porParte {
		return a + n + b
	}
	ps := strings.Split(n, ".")
	for i := range ps {
		ps[i] = a + ps[i] + b
	}
	return strings.Join(ps, ".")
}

func caixaVar(s string, modo int) string {
	switch modo {
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
	case 4: // cada palavra com inicial maiúscula: Select, Database
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
	reLugar      = regexp.MustCompile(`«[123Q]»`)
	reLugarAspas = regexp.MustCompile(`"(«[123Q]»)"`)
)

// montar: o texto do caso com as escolhas dadas. Devolve também os nomes e as palavras do
// modelo que têm que ficar.
func montar(c casoVar, caixa int, esp, cit, com string, partes int, n1 string, varios bool) varTexto {
	mod := c.modelo
	if strings.Contains(mod, "«c»") {
		if com == "" {
			com = " "
		}
		mod = strings.ReplaceAll(mod, "«c»", com)
	}
	if !c.semCaixa {
		// muda a caixa só fora dos lugares
		var b strings.Builder
		last := 0
		for _, ix := range reLugar.FindAllStringIndex(mod, -1) {
			b.WriteString(caixaVar(mod[last:ix[0]], caixa))
			b.WriteString(mod[ix[0]:ix[1]])
			last = ix[1]
		}
		b.WriteString(caixaVar(mod[last:], caixa))
		mod = b.String()
	}
	if esp != " " && esp != "" {
		mod = strings.ReplaceAll(mod, " ", esp)
		if c.leitor == "tabela" { // na tabela, o separador de colunas
			mod = strings.ReplaceAll(c.modelo, ",", esp)
		}
	}
	fica := palavrasModelo(mod)
	nomes := map[string]string{"1": varBase["1"], "2": varBase["2"], "3": varBase["3"]}
	for i, n := range c.base {
		nomes[fmt.Sprint(i+1)] = n
	}
	if n1 != "" {
		nomes["1"] = n1
	}
	q := nomes["1"]
	if n1 != "" {
		q = n1
	}
	if partes > 1 {
		q = strings.Join(append(append([]string{}, varPartes[4-partes:3]...), q), ".")
	}
	// o modelo já tem aspas em volta do lugar (código, JSON): a citação troca essas aspas
	if cit != "" {
		mod = reLugarAspas.ReplaceAllString(mod, "$1")
	}
	porParte := c.leitor == "sql" || c.leitor == "tabela"
	var lista []string
	texto := reLugar.ReplaceAllStringFunc(mod, func(l string) string {
		k := l[2 : len(l)-2]
		n := q
		if k != "Q" {
			n = nomes[k]
		}
		lista = append(lista, strings.Split(n, ".")...)
		return citar(n, cit, porParte)
	})
	if varios {
		outro := strings.NewReplacer("xq_", "xr_", "xq-", "xr-").Replace(texto)
		for _, n := range lista {
			lista = append(lista, strings.NewReplacer("xq_", "xr_", "xq-", "xr-").Replace(n))
		}
		texto += c.varios + outro
	}
	return varTexto{texto: texto, nomes: lista, fica: fica}
}

// palavrasModelo: as palavras de 4 ou mais letras do modelo (fora dos lugares): não podem
// ser mascaradas.
func palavrasModelo(mod string) []string {
	var out []string
	for _, w := range regexp.MustCompile(`[A-Za-z]{4,}`).FindAllString(reLugar.ReplaceAllString(mod, " "), -1) {
		out = append(out, w)
	}
	return out
}

// variacoes: o caso base e uma dimensão mudada de cada vez.
func variacoes(c casoVar) []varTexto {
	var out []varTexto
	add := func(rot string, v varTexto) { v.rot = c.leitor + " | " + c.modelo + " | " + rot; out = append(out, v) }
	add("base", montar(c, 0, " ", "", "", 1, "", false))
	if !c.semCaixa {
		for k, nome := range []string{"MAIÚSCULAS", "minúsculas", "aLtErNaDa", "Inicial"} {
			if k == 2 && c.leitor != "sql" && c.leitor != "erro" {
				continue // caixa alternada só onde a linguagem não diferencia caixa
			}
			add("caixa "+nome, montar(c, k+1, " ", "", "", 1, "", false))
		}
	}
	for _, e := range c.esp {
		add(fmt.Sprintf("espaço %q", e), montar(c, 0, e, "", "", 1, "", false))
	}
	for _, q := range c.cit {
		add("citação "+q, montar(c, 0, " ", q, "", 1, "", false))
	}
	for _, cm := range c.com {
		add(fmt.Sprintf("comentário %q", cm), montar(c, 0, " ", "", cm, 1, "", false))
	}
	if c.pont {
		for _, p := range []string{".", ",", ";", ")", ":"} {
			v := montar(c, 0, " ", "", "", 1, "", false)
			v.texto = "(" + v.texto + p + " ok"
			add("pontuação "+p, v)
		}
	}
	if c.partes {
		for n := 2; n <= 4; n++ {
			add(fmt.Sprintf("%d partes", n), montar(c, 0, " ", "", "", n, "", false))
			if len(c.cit) > 0 {
				add(fmt.Sprintf("%d partes citadas %s", n, c.cit[0]), montar(c, 0, " ", c.cit[0], "", n, "", false))
			}
		}
	}
	for _, n := range c.chars {
		add("nome "+n, montar(c, 0, " ", "", "", 1, n, false))
	}
	if c.varios != "" {
		add("vários na linha", montar(c, 0, " ", "", "", 1, "", true))
	}
	// aspas escapadas: o texto inteiro dentro de uma string JSON (resultado de ferramenta)
	v := montar(c, 0, " ", "", "", 1, "", false)
	j, _ := json.Marshal(map[string]string{"stdout": v.texto})
	v.texto = string(j)
	add("dentro de JSON (escapado)", v)
	return out
}

// reiniciar: o masker volta a não saber nada (sem refazer o NovoMasker, que é caro).
func reiniciar(m *Masker) {
	m.conh = novosConhecidos()
	m.fracos = fracos{}
	m.vistos = nil
	m.mu.Lock()
	m.memo, m.velho = map[[32]byte]resultado{}, nil
	m.mu.Unlock()
}

func legivel(out, v string) bool {
	return regexp.MustCompile(`(?i)(^|[^\p{L}0-9_$#])` + regexp.QuoteMeta(v) + `($|[^\p{L}0-9_$#])`).MatchString(out)
}

func TestVariacoesLeitores(t *testing.T) {
	m := novoTeste(t)
	total, falhas := 0, 0
	for _, c := range casosVar {
		for _, v := range variacoes(c) {
			total++
			reiniciar(m)
			out, _ := m.Mascarar(v.texto)
			var claros, amais []string
			for _, n := range v.nomes {
				if legivel(out, n) {
					claros = append(claros, n)
				}
			}
			for _, w := range v.fica {
				if !strings.Contains(strings.ToLower(out), strings.ToLower(w)) {
					amais = append(amais, w)
				}
			}
			if len(claros)+len(amais) > 0 {
				falhas++
				t.Errorf("%s\n   texto: %q\n   saída: %q\n   em claro: %v  mascarado a mais: %v", v.rot, v.texto, out, claros, amais)
			}
		}
	}
	t.Logf("%d variações, %d falhas", total, falhas)
}

// Prosa em português com trechos de SQL entre crases (e palavras-chave soltas em maiúsculas):
// o nome da tabela é mascarado, as palavras da frase não.
func TestProsaComSQLEntreCrases(t *testing.T) {
	casos := []struct {
		texto string
		some  []string
		fica  []string
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
	for _, c := range casos {
		m := novoTeste(t)
		out, _ := m.Mascarar(c.texto)
		for _, n := range c.some {
			if legivel(out, n) {
				t.Errorf("%q ficou legível:\n%s", n, out)
			}
		}
		for _, w := range c.fica {
			if !strings.Contains(out, w) {
				t.Errorf("%q foi mascarada (prosa):\n%s", w, out)
			}
		}
	}
}
