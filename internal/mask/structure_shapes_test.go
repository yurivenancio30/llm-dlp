package mask

import (
	"regexp"
	"strings"
	"testing"
)

// The structure readers only read a name where the structure is the one they read: a list of labels
// is not a list of names, a field that names itself is not a resource, a call is not a tuple, a line
// without the separator ends the table, a sentence that cites CREATE TABLE is not DDL, the FROM of a
// function is not a table, the system's catalog is public, a DOMAIN\user is not a folder of a path
// and a path in the program's data (its template variables) is not database.schema.table. Each case
// has the other side: the real name next to it is still masked.

type shapeCase struct {
	name, text string
	gone, stay []string
}

func checkShapes(t *testing.T, cases []shapeCase) {
	t.Helper()
	reTok := func(w string) *regexp.Regexp {
		return regexp.MustCompile(`(^|[^\pL\pN_])` + regexp.QuoteMeta(w) + `($|[^\pL\pN_])`)
	}
	for _, c := range cases {
		out, _ := newTest(t).Mask(c.text)
		for _, w := range c.gone {
			if reTok(w).MatchString(out) {
				t.Errorf("%s: %q went through: %q", c.name, w, out)
			}
		}
		for _, w := range c.stay {
			if !reTok(w).MatchString(out) {
				t.Errorf("%s: %q was masked: %q", c.name, w, out)
			}
		}
	}
}

// entOf: the object type with which v was masked in s ("" if it was not).
func entOf(t *testing.T, s, v string) string {
	t.Helper()
	return strings.TrimPrefix(typeOf(newTest(t), s, v), objTypePrefix)
}

func TestLabelsAreNotNames(t *testing.T) {
	checkShapes(t, []shapeCase{
		{"report header", `{"titulo": "4A", "colunas": ["Elemento", "O que se mediu", "Veredito", "cd_pedido_hx"]}`,
			[]string{"cd_pedido_hx"}, []string{"Elemento", "Veredito"}},
		{"escaped report header", `"{\"colunas\": [\"Tema\", \"De onde vem a lacuna\", \"Motivo\"]}"`,
			nil, []string{"Tema", "Motivo"}},
		{"list of names", `{"colunas": ["cd_pedido_hx", "vl_total_hx"], "tabelas": ["tb_pedido_hx"]}`,
			[]string{"cd_pedido_hx", "vl_total_hx", "tb_pedido_hx"}, nil},
		{"field that names itself", `{"mapa": {"Tabelas": "tabelas", "Colunas": "colunas"}, "usuarios": "Usuários"}`,
			nil, []string{"tabelas", "colunas", "Usuários"}},
		{"code map that names itself", "rotulos = {\"Tabelas\": \"tabelas\", 'usuarios': 'Usuários'}\n",
			nil, []string{"tabelas", "Usuários"}},
		{"key with a resource", `{"tabela": "tb_pedido_hx", "usuario": "svc_carga_hx"}`,
			[]string{"tb_pedido_hx", "svc_carga_hx"}, nil},
	})
}

func TestTablesEndWhereTheirShapeEnds(t *testing.T) {
	checkShapes(t, []shapeCase{
		{"loose column", "table_name\ntb_pedido_hx\ntb_item_hx\n", []string{"tb_pedido_hx", "tb_item_hx"}, nil},
		{"header of a result", "[('table_schema', 'table_name'), ('vendas_hx', 'tb_pedido_hx'), ('vendas_hx', 'tb_item_hx')]\n",
			[]string{"vendas_hx", "tb_pedido_hx", "tb_item_hx"}, nil},
		// f("a", "b") is a call, not a tuple
		{"calls", "f(\"table_name\", \"schema_name\")\nf(\"pedidos\", \"vendas\")\n",
			nil, []string{"pedidos", "vendas"}},
		{"bare tuples", "(\"table_name\", \"schema_name\")\n(\"tb_pedido_hx\", \"vendas_hx\")\n",
			[]string{"tb_pedido_hx", "vendas_hx"}, nil},
		// "--" or a title between two tables ends the first one
		{"two tables", "Dataset | Tabelas | Linhas\nvendas_hx | 3 | 10\n--\nComponente | O desenho afirma | Veredito\nCamada | algo | ok\n",
			[]string{"vendas_hx"}, []string{"Componente", "Camada"}},
		// a quoted cell across lines (RFC 4180) does not end the table
		{"cell across lines", "DATABASE_NAME,SCHEMA_NAME,TASK_NAME,ERROR\ndb_hx,sch_um_hx,tsk_a_hx,\"SQL compilation error:\n" +
			"Object 'x' does not exist.\"\ndb_hx,sch_dois_hx,tsk_b_hx,\n",
			[]string{"sch_um_hx", "sch_dois_hx"}, nil},
	})
}

func TestSQLGrammarOfCreateAndFrom(t *testing.T) {
	checkShapes(t, []shapeCase{
		{"CREATE TABLE in a sentence", "Os 12 tipos mais usados entre as colunas dos CREATE TABLE lidos (recorte: os 12 mais frequentes).",
			nil, []string{"lidos", "recorte", "frequentes"}},
		{"CREATE TABLE followed by prose", "As opções escritas no CREATE TABLE de cada schema que veio com DDL.",
			nil, []string{"cada", "schema"}},
		{"CREATE TABLE", "CREATE TABLE vendas_hx.tb_pedido_hx (\n  id_pedido_hx INT,\n  vl_total_hx NUMERIC\n);",
			[]string{"vendas_hx", "tb_pedido_hx", "id_pedido_hx", "vl_total_hx"}, nil},
		{"Teradata options", "CREATE MULTISET TABLE db_hx.tb_carga_hx ,NO FALLBACK (\n  id_carga_hx INTEGER\n);",
			[]string{"db_hx", "tb_carga_hx"}, nil},
		{"CREATE TABLE AS", "CREATE TABLE tb_copia_hx AS SELECT * FROM tb_origem_hx;",
			[]string{"tb_copia_hx", "tb_origem_hx"}, nil},
		{"columns only", "CREATE TABLE tb_resumo_hx (a_hx, b_hx) AS SELECT x, y FROM tb_base_hx;",
			[]string{"tb_resumo_hx", "tb_base_hx"}, nil},
		{"EXTRACT", "SELECT AVG(EXTRACT(EPOCH FROM dt_chegada_hx - dt_previsao_hx)) / 60 AS mae_hx FROM tb_eventos_hx",
			[]string{"dt_chegada_hx", "dt_previsao_hx", "tb_eventos_hx"}, []string{"EPOCH"}},
		{"system catalog", "SELECT relname, n_live_tup, pg_size_pretty(pg_total_relation_size(relid)) FROM pg_stat_user_tables;\n" +
			"SELECT extname, extversion FROM pg_extension;\nSELECT * FROM information_schema.EVENTS;\n" +
			"SELECT TABLE_NAME, CREATE_OPTIONS FROM INFORMATION_SCHEMA.TABLES WHERE CREATE_OPTIONS LIKE '%x%';",
			nil, []string{"n_live_tup", "relid", "pg_extension", "extname", "extversion", "EVENTS", "CREATE_OPTIONS"}},
		{"database before the system schema", "SELECT * FROM db_cliente_hx.information_schema.tables;",
			[]string{"db_cliente_hx"}, nil},
	})
	ext := "SELECT EXTRACT(EPOCH FROM dt_chegada_hx) FROM tb_eventos_hx"
	if e := entOf(t, ext, "dt_chegada_hx"); e != "column" {
		t.Errorf("the argument of EXTRACT is %q, want column", e)
	}
	if e := entOf(t, ext, "tb_eventos_hx"); e != "table" {
		t.Errorf("the FROM of the query is %q, want table", e)
	}
	sub := "SELECT a FROM (SELECT b FROM tb_interna_hx) x WHERE c IN (SELECT d FROM tb_lista_hx)"
	for _, v := range []string{"tb_interna_hx", "tb_lista_hx"} {
		if e := entOf(t, sub, v); e != "table" {
			t.Errorf("the FROM of a subquery: %s is %q, want table", v, e)
		}
	}
}

func TestFolderIsNotADomainUser(t *testing.T) {
	// the folder after a folder with a space in the name: still masked, as a folder
	for _, c := range []struct{ s, folder string }{
		{`['\\srv01\dados\Power BI\Banco de Dados\estados.xlsx', 'File', 1]`, "Banco"},
		{`['\\\\srv01\\dados\\07-Gerencial PBI\\Forecast', 'Folder', 1]`, "Forecast"},
		{`   15 C:\Users\ana.souza\OneDrive - Empresa SA\Arquivos de RH\x.xlsx`, "Arquivos"},
	} {
		if e := entOf(t, c.s, c.folder); e != "folder" {
			t.Errorf("%q: %s masked as %q, want folder", c.s, c.folder, e)
		}
	}
	for _, c := range []struct{ s, user string }{
		{`runas /user:CORP\joao.silva cmd`, "joao.silva"},
		{`Login failed for user 'CORP\svc_carga'.`, "svc_carga"},
		{`Membros: CORP\ana.lima, CORP\rui.costa`, "rui.costa"},
		{`net use \\srv01\share /user:CORP\maria.reis`, "maria.reis"},
	} {
		if e := entOf(t, c.s, c.user); e != "user" {
			t.Errorf("%q: %s masked as %q, want user", c.s, c.user, e)
		}
	}
}

func TestTemplatePathIsNotATable(t *testing.T) {
	checkShapes(t, []shapeCase{
		{"template root", "16 h1 5. O relato\n17 tabela analise_hx.confronto_hx\n18 p {{analise_hx.veredito}}\n",
			nil, []string{"analise_hx", "confronto_hx"}},
		{"other root in the same text", "17 tabela fin_hx.tb_pedido_hx: 1200 linhas\n18 p {{analise_hx.veredito}}\n",
			[]string{"fin_hx", "tb_pedido_hx"}, nil},
		{"no template", "tabela fin_hx.tb_pedido_hx: 1200 linhas",
			[]string{"fin_hx", "tb_pedido_hx"}, nil},
	})
}
