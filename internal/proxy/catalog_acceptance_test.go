package proxy

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// Acceptance test of the real experiment (made-up catalog): Claude reads a catalog CSV with head,
// writes a script that generates the tags (with the tags and the regexes as literals), runs the
// script, which prints "ordem | plataforma | objeto | coluna | tipo | [regex]" without a header,
// and then reads an SQL file with a comment in Portuguese.
//
// Expected: in the script output, columns and tables masked (except the words the script itself
// brings, which stay readable), the script's regexes intact, and the SQL comment without
// pseudonyms.

var (
	acSchemas = []string{"fin_contab", "rh_folha", "crm_vendas"}
	acTables  = []string{"lanc_diario", "plano_contas", "clientes", "pedidos", "contratos", "saldo_mensal",
		"faturas", "notas_fiscais", "fornecedores", "titulos_pagar", "titulos_receber", "centros_custo",
		"funcionarios", "dependentes", "beneficios", "ponto_diario", "ferias", "oportunidades", "campanhas",
		"leads", "metas_vendas", "comissoes"}
	acColumns = []string{"cd_cliente", "nm_cliente", "nr_cpf", "dt_nascimento", "ds_email", "nr_telefone",
		"vl_saldo", "vl_lancto", "dt_lancto", "cd_conta", "ds_historico", "cd_empresa", "nr_documento",
		"dt_emissao", "vl_total", "cd_produto", "qt_itens", "cd_funcionario", "nm_funcionario",
		"dt_admissao", "vl_salario", "cd_cargo", "nome", "valor", "situacao"}
	acTypes = []string{"VARCHAR(20)", "NUMBER(12,2)", "DATE", "VARCHAR(100)", "INTEGER"}
	// the script's tags: word -> regex (program literals)
	acTags = [][2]string{{"cpf", `.*cpf.*`}, {"email", `.*email.*`}, {"telefone", `.*telefone.*`},
		{"nascimento", `.*nascimento.*`}, {"salario", `.*salario.*`}, {"documento", `^nr_documento$`}}
)

type catLine struct{ plat, obj, col, typ string }

func acceptanceCatalog() []catLine {
	var ls []catLine
	for i := 0; len(ls) < 99; i++ {
		t := acTables[i%len(acTables)]
		s := acSchemas[(i/len(acTables)+i)%len(acSchemas)]
		plat := "snowflake"
		if i%3 == 0 {
			plat = "postgres"
		}
		ls = append(ls, catLine{plat, s + "." + t, acColumns[(i*7)%len(acColumns)], acTypes[i%len(acTypes)]})
	}
	return ls
}

func scriptTags() string {
	var b strings.Builder
	b.WriteString("import csv\n\n# tags de dado pessoal, pela forma do nome da coluna\nTAGS = {\n")
	for _, t := range acTags {
		fmt.Fprintf(&b, "    %q: r'%s',\n", t[0], t[1])
	}
	b.WriteString("}\n\nwith open('dados/catalogo.csv') as f:\n    for i, l in enumerate(csv.DictReader(f), 1):\n" +
		"        regex = [r for r in TAGS.values() if re.match(r, l['coluna'])]\n" +
		"        print(i, '|', l['plataforma'], '|', l['objeto'], '|', l['coluna'], '|', l['tipo'], '|', ' '.join(regex))\n")
	return b.String()
}

func TestAcceptanceCatalogScript(t *testing.T) {
	cat := acceptanceCatalog()
	var csvB strings.Builder
	csvB.WriteString("plataforma,objeto,coluna,tipo\n")
	for _, l := range cat {
		fmt.Fprintf(&csvB, "%s,%s,%s,%s\n", l.plat, l.obj, l.col, l.typ)
	}
	head := strings.Join(strings.SplitN(csvB.String(), "\n", 6)[:5], "\n") + "\n"
	script := scriptTags()
	var output strings.Builder
	for i, l := range cat {
		var rs []string
		for _, tg := range acTags {
			if regexp.MustCompile(tg[1]).MatchString(l.col) {
				rs = append(rs, tg[1])
			}
		}
		fmt.Fprintf(&output, "%d | %s | %s | %s | %s | %s\n", i+1, l.plat, l.obj, l.col, l.typ, strings.Join(rs, " "))
	}
	sql := "     1\t-- busca o nome e o valor do cliente pelo codigo\n     2\tSELECT nome, valor FROM fin_contab.clientes WHERE cd_cliente = :id;\n"

	var bodies [][]byte
	px, _ := buildWithMasker(t, &bodies)
	send(t, px, []any{
		msg("user", "gera as tags de dado pessoal do catálogo em dados/catalogo.csv"),
		msg("assistant", []any{bashCall("c1", "head -5 dados/catalogo.csv")}),
		msg("user", []any{result("c1", head)}),
		msg("assistant", []any{map[string]any{"type": "tool_use", "id": "c2", "name": "Write",
			"input": map[string]any{"file_path": "/home/u/proj/scripts/gerar_tags.py", "content": script}}}),
		msg("user", []any{result("c2", "File created successfully at: /home/u/proj/scripts/gerar_tags.py")}),
		msg("assistant", []any{bashCall("c3", "python3 scripts/gerar_tags.py")}),
		msg("user", []any{result("c3", output.String())}),
		msg("assistant", []any{map[string]any{"type": "tool_use", "id": "c4", "name": "Read",
			"input": map[string]any{"file_path": "/home/u/proj/sql/consulta.sql"}}}),
		msg("user", []any{result("c4", sql)}),
	})
	r := messages(t, bodies[len(bodies)-1])
	var out, outSQL string
	{
		var v map[string]any
		json.Unmarshal([]byte(r[6]), &v)
		out = textOf(v["content"].([]any)[0].(map[string]any)["content"])
		json.Unmarshal([]byte(r[8]), &v)
		outSQL = textOf(v["content"].([]any)[0].(map[string]any)["content"])
	}
	// program words (the command and the script): stay readable, by rule
	prog := map[string]bool{}
	for _, w := range regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`).FindAllString(script+" python3 scripts/gerar_tags.py", -1) {
		prog[w] = true
	}
	wholeOne := func(s, w string) bool {
		return regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(w) + `($|[^A-Za-z0-9_])`).MatchString(s)
	}
	ls := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(ls) != len(cat) {
		t.Fatalf("output with %d lines", len(ls))
	}
	colClear, colTotal, tabClear := 0, 0, map[string]bool{}
	regexOK := 0
	leaked := map[string]int{}
	for i, l := range ls {
		c := cat[i]
		if !prog[c.col] {
			colTotal++
			if wholeOne(l, c.col) {
				colClear++
				leaked[c.col]++
			}
		}
		for _, p := range strings.Split(c.obj, ".") {
			if wholeOne(l, p) {
				tabClear[p] = true
			}
		}
		ok := true
		for _, tg := range acTags {
			if regexp.MustCompile(tg[1]).MatchString(c.col) && !strings.Contains(l, tg[1]) {
				ok = false
			}
		}
		if ok {
			regexOK++
		}
	}
	nTab := map[string]bool{}
	for _, l := range cat {
		for _, p := range strings.Split(l.obj, ".") {
			nTab[p] = true
		}
	}
	t.Logf("columns in plain text: %d of %d lines (apart from the program's words); tables/schemas in plain text: %d of %d; regexes intact: %d of %d lines",
		colClear, colTotal, len(tabClear), len(nTab), regexOK, len(ls))
	nHead := 0
	for c, n := range leaked {
		inHead := strings.Contains(head, ","+c+",")
		ident := strings.ContainsAny(c, "_0123456789")
		if inHead {
			nHead++
		}
		t.Logf("leaked column: %d times, in the head=%v, looks like an identifier=%v, %d letters", n, inHead, ident, len(c))
	}
	if colClear > 0 || len(tabClear) > 0 {
		var cs []string
		for p := range tabClear {
			cs = append(cs, p)
		}
		t.Errorf("in plain text: %d columns, tables %v", colClear, cs)
	}
	if regexOK != len(ls) {
		t.Errorf("script regexes swapped in %d lines", len(ls)-regexOK)
	}
	// the SQL comment in Portuguese stays readable
	if !strings.Contains(outSQL, "-- busca o nome e o valor do cliente pelo codigo") {
		t.Errorf("SQL comment changed: %q", outSQL)
	}
}
