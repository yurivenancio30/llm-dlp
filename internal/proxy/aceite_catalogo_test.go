package proxy

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// Teste de aceite do experimento real (catálogo inventado): o Claude lê um CSV de catálogo com
// head, escreve um script que gera as tags (com as tags e as regex como literais), roda o
// script, que imprime "ordem | plataforma | objeto | coluna | tipo | [regex]" sem cabeçalho, e
// depois lê um SQL com comentário em português.
//
// Esperado: na saída do script, colunas e tabelas mascaradas (menos as palavras que o próprio
// script traz, que continuam legíveis), as regex do script intactas, e o comentário do SQL sem
// pseudônimo.

var (
	acSchemas = []string{"fin_contab", "rh_folha", "crm_vendas"}
	acTabelas = []string{"lanc_diario", "plano_contas", "clientes", "pedidos", "contratos", "saldo_mensal",
		"faturas", "notas_fiscais", "fornecedores", "titulos_pagar", "titulos_receber", "centros_custo",
		"funcionarios", "dependentes", "beneficios", "ponto_diario", "ferias", "oportunidades", "campanhas",
		"leads", "metas_vendas", "comissoes"}
	acColunas = []string{"cd_cliente", "nm_cliente", "nr_cpf", "dt_nascimento", "ds_email", "nr_telefone",
		"vl_saldo", "vl_lancto", "dt_lancto", "cd_conta", "ds_historico", "cd_empresa", "nr_documento",
		"dt_emissao", "vl_total", "cd_produto", "qt_itens", "cd_funcionario", "nm_funcionario",
		"dt_admissao", "vl_salario", "cd_cargo", "nome", "valor", "situacao"}
	acTipos = []string{"VARCHAR(20)", "NUMBER(12,2)", "DATE", "VARCHAR(100)", "INTEGER"}
	// as tags do script: palavra -> regex (literais do programa)
	acTags = [][2]string{{"cpf", `.*cpf.*`}, {"email", `.*email.*`}, {"telefone", `.*telefone.*`},
		{"nascimento", `.*nascimento.*`}, {"salario", `.*salario.*`}, {"documento", `^nr_documento$`}}
)

type linhaCat struct{ plat, obj, col, tipo string }

func catalogoAceite() []linhaCat {
	var ls []linhaCat
	for i := 0; len(ls) < 99; i++ {
		t := acTabelas[i%len(acTabelas)]
		s := acSchemas[(i/len(acTabelas)+i)%len(acSchemas)]
		plat := "snowflake"
		if i%3 == 0 {
			plat = "postgres"
		}
		ls = append(ls, linhaCat{plat, s + "." + t, acColunas[(i*7)%len(acColunas)], acTipos[i%len(acTipos)]})
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

func TestAceiteCatalogoScript(t *testing.T) {
	cat := catalogoAceite()
	var csvB strings.Builder
	csvB.WriteString("plataforma,objeto,coluna,tipo\n")
	for _, l := range cat {
		fmt.Fprintf(&csvB, "%s,%s,%s,%s\n", l.plat, l.obj, l.col, l.tipo)
	}
	head := strings.Join(strings.SplitN(csvB.String(), "\n", 6)[:5], "\n") + "\n"
	script := scriptTags()
	var saida strings.Builder
	for i, l := range cat {
		var rs []string
		for _, tg := range acTags {
			if regexp.MustCompile(tg[1]).MatchString(l.col) {
				rs = append(rs, tg[1])
			}
		}
		fmt.Fprintf(&saida, "%d | %s | %s | %s | %s | %s\n", i+1, l.plat, l.obj, l.col, l.tipo, strings.Join(rs, " "))
	}
	sql := "     1\t-- busca o nome e o valor do cliente pelo codigo\n     2\tSELECT nome, valor FROM fin_contab.clientes WHERE cd_cliente = :id;\n"

	var corpos [][]byte
	px, _ := montarComMasker(t, &corpos)
	enviar(t, px, []any{
		msg("user", "gera as tags de dado pessoal do catálogo em dados/catalogo.csv"),
		msg("assistant", []any{chamadaBash("c1", "head -5 dados/catalogo.csv")}),
		msg("user", []any{resultado("c1", head)}),
		msg("assistant", []any{map[string]any{"type": "tool_use", "id": "c2", "name": "Write",
			"input": map[string]any{"file_path": "/home/u/proj/scripts/gerar_tags.py", "content": script}}}),
		msg("user", []any{resultado("c2", "File created successfully at: /home/u/proj/scripts/gerar_tags.py")}),
		msg("assistant", []any{chamadaBash("c3", "python3 scripts/gerar_tags.py")}),
		msg("user", []any{resultado("c3", saida.String())}),
		msg("assistant", []any{map[string]any{"type": "tool_use", "id": "c4", "name": "Read",
			"input": map[string]any{"file_path": "/home/u/proj/sql/consulta.sql"}}}),
		msg("user", []any{resultado("c4", sql)}),
	})
	r := mensagens(t, corpos[len(corpos)-1])
	var out, outSQL string
	{
		var v map[string]any
		json.Unmarshal([]byte(r[6]), &v)
		out = textoDe(v["content"].([]any)[0].(map[string]any)["content"])
		json.Unmarshal([]byte(r[8]), &v)
		outSQL = textoDe(v["content"].([]any)[0].(map[string]any)["content"])
	}
	// palavras do programa (o comando e o script): continuam legíveis, por regra
	prog := map[string]bool{}
	for _, w := range regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`).FindAllString(script+" python3 scripts/gerar_tags.py", -1) {
		prog[w] = true
	}
	inteira := func(s, w string) bool {
		return regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(w) + `($|[^A-Za-z0-9_])`).MatchString(s)
	}
	ls := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(ls) != len(cat) {
		t.Fatalf("saída com %d linhas", len(ls))
	}
	colClaro, colTot, tabClaro := 0, 0, map[string]bool{}
	regexOK := 0
	vazou := map[string]int{}
	for i, l := range ls {
		c := cat[i]
		if !prog[c.col] {
			colTot++
			if inteira(l, c.col) {
				colClaro++
				vazou[c.col]++
			}
		}
		for _, p := range strings.Split(c.obj, ".") {
			if inteira(l, p) {
				tabClaro[p] = true
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
	t.Logf("colunas em claro: %d de %d linhas (fora as palavras do programa); tabelas/schemas em claro: %d de %d; regex intactas: %d de %d linhas",
		colClaro, colTot, len(tabClaro), len(nTab), regexOK, len(ls))
	nHead := 0
	for c, n := range vazou {
		emHead := strings.Contains(head, ","+c+",")
		ident := strings.ContainsAny(c, "_0123456789")
		if emHead {
			nHead++
		}
		t.Logf("coluna vazada: %d vezes, no head=%v, cara de identificador=%v, %d letras", n, emHead, ident, len(c))
	}
	if colClaro > 0 || len(tabClaro) > 0 {
		var cs []string
		for p := range tabClaro {
			cs = append(cs, p)
		}
		t.Errorf("em claro: %d colunas, tabelas %v", colClaro, cs)
	}
	if regexOK != len(ls) {
		t.Errorf("regex do script trocadas em %d linhas", len(ls)-regexOK)
	}
	// o comentário do SQL em português continua legível
	if !strings.Contains(outSQL, "-- busca o nome e o valor do cliente pelo codigo") {
		t.Errorf("comentário do SQL alterado: %q", outSQL)
	}
}
