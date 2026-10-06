package mask

import (
	"regexp"
	"strings"
	"testing"
)

// Lote B (Dados): leitores de SQL, mensagens de erro, conexões e tabelas. Nomes fictícios.

var rePseudoQualquer = regexp.MustCompile(`\b(?i:host|db|sch|t|c|proc|idx|usr|ns|svc|bkt|top)_[a-z2-7]{8}\b`)

// mascara confere que cada nome de deve sumiu, que cada nome de fica ficou, e devolve a saída.
func confere(t *testing.T, m *Masker, s string, some, fica []string) string {
	t.Helper()
	out, _ := m.Mascarar(s)
	for _, v := range some {
		if regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(v) + `($|[^A-Za-z0-9_])`).MatchString(out) {
			t.Errorf("%q continuou legível em:\n   %q\n-> %q", v, s, out)
		}
	}
	for _, v := range fica {
		if !strings.Contains(out, v) {
			t.Errorf("%q deveria ficar em:\n   %q\n-> %q", v, s, out)
		}
	}
	return out
}

// tipoDe: o tipo do pseudônimo que substituiu real (pelas entradas).
func tipoDe(m *Masker, s, real string) string {
	_, ents := m.Mascarar(s)
	for _, e := range ents {
		if strings.EqualFold(e.Real, real) {
			return e.Tipo
		}
	}
	return ""
}

func TestLeitorSQL(t *testing.T) {
	m := novoTeste(t)
	out := confere(t, m, "SELECT p.[vl_total], c.cd_cliente FROM [srv-exemplo-01].vendas_demo.financeiro.tb_pedido_x9 AS p JOIN dbo.tb_cliente_x2 c ON c.id = p.id_cliente WHERE p.dt > '2026-01-01'",
		[]string{"srv-exemplo-01", "vendas_demo", "financeiro", "tb_pedido_x9", "tb_cliente_x2", "vl_total", "cd_cliente", "id_cliente"},
		[]string{"SELECT p.[", "], c.", "dbo.", " AS p JOIN ", " c ON c.", "'2026-01-01'"})
	if !strings.Contains(out, "[HOST_") && !strings.Contains(out, "[host_") {
		t.Errorf("o colchete em volta do servidor tem que ficar: %q", out)
	}
	for real, tipo := range map[string]string{"srv-exemplo-01": "obj.servidor", "vendas_demo": "obj.database", "financeiro": "obj.schema",
		"tb_pedido_x9": "obj.tabela", "vl_total": "obj.coluna"} {
		if got := tipoDe(m, "SELECT p.[vl_total] FROM [srv-exemplo-01].vendas_demo.financeiro.tb_pedido_x9 AS p", real); got != tipo {
			t.Errorf("%s: tipo %q, esperado %q", real, got, tipo)
		}
	}
	// Snowflake, minúsculas, com várias cláusulas
	confere(t, m, "create or replace table ANALYTICS_X.STG.TB_PEDIDO_X9 as select * from RAW_X.SRC.PEDIDOS_X1 where 1=1;",
		[]string{"ANALYTICS_X", "STG", "TB_PEDIDO_X9", "RAW_X", "SRC", "PEDIDOS_X1"}, []string{"create or replace table ", " as select * from "})
	// BigQuery: o nome inteiro entre crases
	confere(t, m, "SELECT col_a FROM `projeto-demo.dataset_x.tb_y_01` WHERE col_b IS NULL",
		[]string{"projeto-demo", "dataset_x", "tb_y_01", "col_a", "col_b"}, []string{"`", "IS NULL"})
	// PostgreSQL / SQL dentro de string de código
	confere(t, m, "INSERT INTO financeiro.tb_pedido_x9 (id_x, vl_total) VALUES (1, 2)", []string{"financeiro", "tb_pedido_x9", "vl_total", "id_x"}, []string{"INSERT INTO ", "VALUES (1, 2)"})
	confere(t, m, `cur.execute("SELECT vl_total FROM tb_pedido_x9 WHERE id = %s", (x,))`+"\nresultado = cur.fetchall()",
		[]string{"tb_pedido_x9", "vl_total"}, []string{"cur.execute(", "cur.fetchall()", "resultado"})
	// funções e palavras-chave ficam
	confere(t, m, "SELECT COUNT(*), MAX(dt_ref_x) FROM tb_pedido_x9 GROUP BY cd_loja_x ORDER BY 1", []string{"dt_ref_x", "cd_loja_x"}, []string{"COUNT(*)", "MAX(", "GROUP BY", "ORDER BY"})
	confere(t, m, "EXEC financeiro.sp_fecha_mes_x 2026", []string{"financeiro", "sp_fecha_mes_x"}, []string{"EXEC "})
	if tipoDe(m, "EXEC financeiro.sp_fecha_mes_x 2026", "sp_fecha_mes_x") != "obj.procedure" {
		t.Errorf("EXEC: tipo procedure")
	}
	confere(t, m, "CREATE INDEX ix_pedido_x9 ON tb_pedido_x9 (dt_ref)", []string{"ix_pedido_x9", "tb_pedido_x9"}, []string{"CREATE INDEX ", " ON "})
}

func TestLeitorSQLNaoPegaProsaNemCodigo(t *testing.T) {
	m := novoTeste(t)
	for _, s := range []string{
		"select the best option from the list below",
		"Please update the readme and set the version",
		"from collections import defaultdict\nwith open(arq) as f:\n    dados = f.read()",
		"use the SQL editor to create tables",
		"db.Select(&rows, query)\nreturn rows.Scan(&x)",
		"Selecione os campos e depois clique em FROM para ver",
	} {
		if out, ents := m.Mascarar(s); len(ents) > 0 {
			t.Errorf("mascarou fora de SQL:\n   %q\n-> %q", s, out)
		}
	}
}

func TestLeitorErro(t *testing.T) {
	m := novoTeste(t)
	confere(t, m, `ERROR:  relation "financeiro.tb_pedido_x9" does not exist`, []string{"financeiro", "tb_pedido_x9"}, []string{"ERROR:  relation \"", "\" does not exist"})
	confere(t, m, "Msg 208, Level 16, State 1, Line 1\nInvalid object name 'vendas_demo.dbo.tb_pedido_x9'.", []string{"vendas_demo", "tb_pedido_x9"}, []string{"Invalid object name '", ".dbo."})
	confere(t, m, "ERROR 1146 (42S02): Table 'vendas_demo.tb_pedido_x9' doesn't exist", []string{"vendas_demo", "tb_pedido_x9"}, []string{"doesn't exist"})
	confere(t, m, "SQL compilation error: Object 'ANALYTICS_X.STG.TB_PEDIDO_X9' does not exist or not authorized.", []string{"ANALYTICS_X", "STG", "TB_PEDIDO_X9"}, nil)
	confere(t, m, "Not Found: Dataset projeto-demo:dataset_x", []string{"projeto-demo", "dataset_x"}, []string{"Not Found: Dataset "})
	if out, ents := m.Mascarar(`if (typeof x === "object") { return 'table' }`); len(ents) > 0 {
		t.Errorf("código JS virou objeto: %q", out)
	}
}

func TestLeitorConexao(t *testing.T) {
	m := novoTeste(t)
	confere(t, m, `Server=srv-exemplo-01\INST01,1433;Database=vendas_demo;User Id=svc_relatorio;Encrypt=True;`,
		[]string{"srv-exemplo-01", "INST01", "vendas_demo", "svc_relatorio"}, []string{",1433;", "Encrypt=True;"})
	confere(t, m, "Driver={ODBC Driver 18 for SQL Server};Server=tcp:srv-exemplo-02,1433;Database=vendas_x9;Uid=svc_x9;",
		[]string{"srv-exemplo-02", "vendas_x9", "svc_x9"}, []string{"Driver={ODBC Driver 18 for SQL Server}", "tcp:"})
	confere(t, m, "jdbc:sqlserver://srv-exemplo-03:1433;databaseName=vendas_y1;user=svc_y1", []string{"srv-exemplo-03", "vendas_y1", "svc_y1"}, []string{"jdbc:sqlserver://", ":1433;"})
	confere(t, m, `psql "host=db-exemplo-01 dbname=vendas_demo user=svc_relatorio sslmode=require"`, []string{"db-exemplo-01", "vendas_demo", "svc_relatorio"}, []string{"sslmode=require"})
	confere(t, m, "postgresql://svc_relatorio@db-exemplo-01:5432/vendas_demo?sslmode=require", []string{"svc_relatorio", "db-exemplo-01", "vendas_demo"}, []string{"postgresql://", ":5432/"})
	confere(t, m, "mongodb+srv://u_app01@cluster-x1.exemplo.net/app_db_x", []string{"u_app01", "cluster-x1.exemplo.net", "app_db_x"}, []string{"mongodb+srv://"})
	confere(t, m, "jdbc:oracle:thin:@//db-ora-01:1521/ORCLPDB_X", []string{"db-ora-01", "ORCLPDB_X"}, []string{":1521/"})
	confere(t, m, "(ADDRESS=(PROTOCOL=TCP)(HOST = db-ora-02)(PORT=1521))(CONNECT_DATA=(SERVICE_NAME=vendas_svc))", []string{"db-ora-02", "vendas_svc"}, []string{"PROTOCOL=TCP", "PORT=1521"})
	confere(t, m, "urn:li:dataset:(urn:li:dataPlatform:mssql,vendas_demo.financeiro.tb_pedido_x9,PROD)", []string{"vendas_demo", "financeiro", "tb_pedido_x9"}, []string{"urn:li:dataPlatform:mssql,", ",PROD)"})
	confere(t, m, "select * from {{ ref('stg_pedidos_x9') }} join {{ source('erp_x', 'tb_cliente_x2') }}", []string{"stg_pedidos_x9", "erp_x", "tb_cliente_x2"}, []string{"{{ ref('", "{{ source('"})
	confere(t, m, `SQLExecuteQueryOperator(task_id="t1", conn_id="conn_vendas_demo")`, []string{"conn_vendas_demo"}, []string{"task_id=\"t1\""})
	// não é string de conexão
	for _, s := range []string{
		"user = request.user\ndb = get_db()",
		"a=1;b=2;c=3",
		"host=localhost user=root",
		"Server: srv\nDatabase: x",
		"for (i=0; i<n; i++) { x=y; }",
	} {
		if out, ents := m.Mascarar(s); len(ents) > 0 {
			t.Errorf("não é conexão:\n   %q\n-> %q", s, out)
		}
	}
}

func TestLeitorTabela(t *testing.T) {
	m := novoTeste(t)
	confere(t, m, " Schema     |     Name     | Type  | Owner\n------------+--------------+-------+---------\n financeiro | tb_pedido_x9 | table | svc_dono\n(1 row)\n",
		[]string{"financeiro", "tb_pedido_x9", "svc_dono"}, []string{"Schema", "Name", "Type", "Owner", "(1 row)", "table"})
	confere(t, m, "table_schema,table_name,column_name\nfinanceiro,tb_pedido_x9,vl_total\nfinanceiro,tb_cliente_x2,cd_cliente\n",
		[]string{"financeiro", "tb_pedido_x9", "vl_total", "tb_cliente_x2", "cd_cliente"}, []string{"table_schema,table_name,column_name"})
	confere(t, m, "TABLE_NAME          COLUMN_NAME\n------------------- -----------\ntb_pedido_x9        vl_total\n\n(1 rows affected)\n",
		[]string{"tb_pedido_x9", "vl_total"}, []string{"TABLE_NAME", "(1 rows affected)"})
	confere(t, m, "+-----------------------+\n| Tables_in_vendas_demo |\n+-----------------------+\n| tb_pedido_x9          |\n+-----------------------+\n1 row in set (0.00 sec)\n",
		[]string{"vendas_demo", "tb_pedido_x9"}, []string{"Tables_in_", "1 row in set"})
	// cabeçalho de dados: colunas com cara de identificador
	confere(t, m, "cd_cliente_x,vl_total,dt_ref\n1,2,3\n4,5,6\n", []string{"cd_cliente_x", "vl_total", "dt_ref"}, []string{"1,2,3"})
	// não é tabela de objeto: palavras simples e tabelas de documentação ficam
	for _, s := range []string{
		"nome,idade,cidade\nAna,30,BH\n",
		"| Option | Description |\n|---|---|\n| a | b |\n",
		"NAME          STATUS    AGE\nmysql-0       Running   3d\n",
	} {
		if out, ents := m.Mascarar(s); len(ents) > 0 {
			t.Errorf("tabela comum foi mascarada:\n   %q\n-> %q", s, out)
		}
	}
}

// Nome aprendido num formato é mascarado em outro formato e em prosa, em qualquer caixa.
// Coluna não propaga.
func TestNomeAprendidoEmOutroFormato(t *testing.T) {
	m := novoTeste(t)
	m.Mascarar("SELECT vl_total_x7 FROM financeiro_x.tb_pedido_x9 WHERE id > 0")
	for _, s := range []string{
		"a carga da tb_pedido_x9 falhou de novo",
		"sql/carga.sql:12:  -- ver TB_PEDIDO_X9",
		"tabela: tb_pedido_x9\nschema: financeiro_x",
		"ls /dados/tb_pedido_x9/",
	} {
		if out, _ := m.Mascarar(s); strings.Contains(strings.ToLower(out), "tb_pedido_x9") || strings.Contains(strings.ToLower(out), "financeiro_x") {
			t.Errorf("não propagou:\n   %q\n-> %q", s, out)
		}
	}
	if out, _ := m.Mascarar("soma de vl_total_x7 no relatório"); !strings.Contains(out, "vl_total_x7") {
		t.Errorf("coluna não deveria propagar: %q", out)
	}
	// um nome visto só numa instrução de uma cláusula sem forma inequívoca (evidência fraca)
	// não ensina; "UPDATE tb_x SET a = 1" tem forma inequívoca e ensina, em qualquer caixa
	m2 := novoTeste(t)
	m2.Mascarar("SELECT nome total FROM tb_fraca_x1")
	if out, _ := m2.Mascarar("depois: tb_fraca_x1"); !strings.Contains(out, "tb_fraca_x1") {
		t.Errorf("evidência fraca ensinou: %q", out)
	}
}

// O SQL mascarado continua SQL: a instrução é reconhecida de novo e só os nomes mudaram.
func TestSQLMascaradoContinuaValido(t *testing.T) {
	m := novoTeste(t)
	orig := "SELECT p.vl_total, [cd cliente] FROM vendas_demo.financeiro.tb_pedido_x9 AS p WHERE p.id_x = 10;"
	out, _ := m.Mascarar(orig)
	if !reFormaSQL.MatchString(out) {
		t.Fatalf("a saída deixou de ter a forma de SQL: %q", out)
	}
	tira := func(s string) string { return rePseudoQualquer.ReplaceAllString(s, "X") }
	if !strings.Contains(out, "[") || strings.Count(tira(out), "X") < 5 {
		t.Errorf("estrutura mudou: %q", out)
	}
	n := 0
	acharSQL(out, func(o ObjAchado) {
		if !rePseudoObj.MatchString(out[o.Ini:o.Fim]) && out[o.Ini:o.Fim] != "p" {
			n++
		}
	})
	if n > 0 {
		t.Errorf("sobrou nome legível na instrução mascarada: %q", out)
	}
}

// Item 5: minúsculas com uma cláusula, quando a forma não deixa dúvida; prosa fica.
func TestSQLMinusculoUmaClausula(t *testing.T) {
	m := novoTeste(t)
	for _, c := range []struct{ s, nome string }{
		{"select * from tb_pedido", "tb_pedido"},
		{"select id_x, vl_total from pedidos_x9;", "pedidos_x9"},
		{"delete from tb_log_x1 where dt < now()", "tb_log_x1"},
		{"insert into tb_auditoria (id) values (1)", "tb_auditoria"},
		{"update tb_cliente_x2 set ativo = 0", "tb_cliente_x2"},
		{"select count(*) from vendas", "vendas"},
	} {
		if out, _ := m.Mascarar(c.s); strings.Contains(out, c.nome) {
			t.Errorf("não reconheceu %q -> %q", c.s, out)
		}
	}
	for _, s := range []string{
		"select from the list below",
		"select the best option from the menu",
		"please select one option from the dropdown menu and continue",
		"delete from the list the items you do not need",
		"update the docs and insert into the index the new page",
	} {
		if out, ents := m.Mascarar(s); len(ents) > 0 {
			t.Errorf("prosa virou SQL: %q -> %q", s, out)
		}
	}
}

// Item 7: colunas e views públicas do information_schema e dos catálogos ficam.
func TestVocabularioCatalogoFica(t *testing.T) {
	m := novoTeste(t)
	for _, s := range []string{
		"SELECT table_schema, table_name, column_name, data_type FROM information_schema.columns WHERE table_schema = 'x'",
		"SELECT schemaname, tablename FROM pg_tables",
		"SELECT owner, table_name, num_rows FROM all_tables",
		"select start_time, user_name, role_name, query_type from snowflake.account_usage.query_history where 1=1",
	} {
		if out, ents := m.Mascarar(s); len(ents) > 0 {
			t.Errorf("vocabulário de catálogo mascarado: %q -> %q", s, out)
		}
	}
}

// Item 13: DSN do driver MySQL do Go e DSN do PDO.
func TestDSNGoEPDO(t *testing.T) {
	m := novoTeste(t)
	confere(t, m, `db, err := sql.Open("mysql", "svc_app_x1:pw@tcp(db-mysql-x1:3306)/vendas_x1?parseTime=true")`,
		[]string{"svc_app_x1", "db-mysql-x1", "vendas_x1"}, []string{"@tcp(", ":3306)/", "?parseTime=true"})
	confere(t, m, `$pdo = new PDO('mysql:host=db-mysql-x2;port=3306;dbname=vendas_x2;charset=utf8mb4', $u, $p);`,
		[]string{"db-mysql-x2", "vendas_x2"}, []string{"mysql:host=", ";port=3306;", ";charset=utf8mb4"})
	confere(t, m, `new PDO("pgsql:host=db-pg-x3;dbname=vendas_x3")`, []string{"db-pg-x3", "vendas_x3"}, []string{"pgsql:host="})
	confere(t, m, `new PDO("sqlsrv:Server=srv-sql-x4,1433;Database=vendas_x4")`, []string{"srv-sql-x4", "vendas_x4"}, []string{"sqlsrv:Server=", ",1433;"})
	confere(t, m, `new PDO("oci:dbname=//db-ora-x5:1521/ORCL_X5")`, []string{"db-ora-x5", "ORCL_X5"}, []string{"oci:dbname=//", ":1521/"})
	for _, s := range []string{"git push origin main@tcp", "time:12:00 a=b"} {
		if out, ents := m.Mascarar(s); len(ents) > 0 {
			t.Errorf("não é DSN: %q -> %q", s, out)
		}
	}
}

// Ajuste final 3: só é catálogo a tabela cujo cabeçalho tem forma de identificador.
func TestTabelaCabecalhoComFormaDeIdentificador(t *testing.T) {
	m := novoTeste(t)
	for _, s := range []string{
		"| Benchmark | Speed MiB/s | Allocs |\n|---|---|---|\n| tb_x1 | 12 | 3 |\n",
		"| for i := 0 | table |\n|---|---|\n| x | tb_x2 |\n",
		"name    | Data Type | table\n--------+-----------+------\nid_x3   | int       | tb_x3\n",
	} {
		if out, ents := m.Mascarar(s); len(ents) > 0 {
			t.Errorf("não é catálogo: %q -> %q", s, out)
		}
	}
	confere(t, m, "table_schema,table_name\nfinanceiro_x4,tb_pedido_x4\n", []string{"financeiro_x4", "tb_pedido_x4"}, nil)
}
