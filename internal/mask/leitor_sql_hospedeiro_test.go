package mask

import (
	"regexp"
	"strings"
	"testing"
)

// SQL embutido em código (string, concatenação, heredoc) de várias linguagens, com código
// depois da string: o SQL é lido (nomes inventados do cliente mascarados) e o código depois
// da string não é lido como SQL (chaves e nomes da linguagem ficam).
var casosSQLHospedeiro = []struct{ nome, src string }{
	{"python aspas triplas em outra linha", "cur.execute(\n    \"\"\"\n    SELECT p.col_valor_hx, p.col_data_hx\n    FROM sch_vendas_hx.tb_pedido_hx p\n    WHERE p.col_valor_hx > 10\n    \"\"\")\nfor row in cur.fetchall():\n    item = {}\n    item['urn'] = f\"urn:li:dataset:({row['payload']})\"\n"},
	{"python f-string na linha", "cur.execute(f\"SELECT col_valor_hx FROM sch_vendas_hx.tb_pedido_hx WHERE id = {x}\")\nitem['urn'] = row['payload']\n"},
	{"python concatenação implícita", "sql = (\"SELECT col_valor_hx \"\n       \"FROM sch_vendas_hx.tb_pedido_hx \"\n       \"WHERE col_data_hx > :d\")\nitem['urn'] = row['payload']\n"},
	{"python aspas simples", "q = 'SELECT col_valor_hx FROM sch_vendas_hx.tb_pedido_hx'\nitem['urn'] = row['payload']\n"},
	{"java concatenação", "String sql = \"SELECT col_valor_hx, col_data_hx \" +\n             \"FROM sch_vendas_hx.tb_pedido_hx \" +\n             \"WHERE col_valor_hx > ?\";\nitem.put(\"urn\", rs.getString(\"payload\"));\n"},
	{"java text block", "String sql = \"\"\"\n    SELECT col_valor_hx\n    FROM sch_vendas_hx.tb_pedido_hx\n    \"\"\";\nitem.put(\"urn\", rs.getString(\"payload\"));\n"},
	{"js template", "const sql = `\n  SELECT col_valor_hx\n  FROM sch_vendas_hx.tb_pedido_hx\n  WHERE col_data_hx > $1\n`;\nitem['urn'] = row['payload'];\n"},
	{"go crua", "q := `SELECT col_valor_hx\nFROM sch_vendas_hx.tb_pedido_hx\nWHERE col_data_hx > $1`\nitem[\"urn\"] = row[\"payload\"]\n"},
	{"c# verbatim", "var sql = @\"SELECT col_valor_hx\n            FROM sch_vendas_hx.tb_pedido_hx\";\nitem[\"urn\"] = reader[\"payload\"];\n"},
	{"kotlin aspas triplas", "val sql = \"\"\"\n    SELECT col_valor_hx\n    FROM sch_vendas_hx.tb_pedido_hx\n\"\"\".trimIndent()\nitem[\"urn\"] = row[\"payload\"]\n"},
	{"ruby heredoc", "sql = <<~SQL\n  SELECT col_valor_hx\n  FROM sch_vendas_hx.tb_pedido_hx\nSQL\nitem['urn'] = row['payload']\n"},
	{"php heredoc", "$sql = <<<SQL\nSELECT col_valor_hx\nFROM sch_vendas_hx.tb_pedido_hx\nSQL;\n$item['urn'] = $row['payload'];\n"},
	{"php concatenação com ponto", "$sql = \"SELECT col_valor_hx \" .\n       \"FROM sch_vendas_hx.tb_pedido_hx\";\n$item['urn'] = $row['payload'];\n"},
	{"shell heredoc", "psql -d dw <<'EOF'\nSELECT col_valor_hx\nFROM sch_vendas_hx.tb_pedido_hx\nEOF\nitem_urn=$(jq -r .payload out.json)\n"},
	{"rust crua", "let sql = r#\"SELECT col_valor_hx\nFROM sch_vendas_hx.tb_pedido_hx\"#;\nitem.insert(\"urn\", row.get(\"payload\"));\n"},
	{"r", "res <- dbGetQuery(con, \"\n  SELECT col_valor_hx\n  FROM sch_vendas_hx.tb_pedido_hx\n\")\nitem$urn <- row$payload\n"},
	{"linguagem desconhecida", "qry := \"SELECT col_valor_hx FROM sch_vendas_hx.tb_pedido_hx\" ~>\nout{urn} <~ in{payload}\n"},
}

func TestSQLEmbutidoEmCodigo(t *testing.T) {
	m := novoTeste(t)
	reTok := func(w string) *regexp.Regexp {
		return regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(w) + `($|[^A-Za-z0-9_])`)
	}
	for _, c := range casosSQLHospedeiro {
		out, _ := m.Mascarar(c.src)
		for _, n := range []string{"tb_pedido_hx", "sch_vendas_hx", "col_valor_hx"} {
			if reTok(n).MatchString(out) {
				t.Errorf("%s: %s (do SQL) em claro:\n%s", c.nome, n, out)
			}
		}
		for _, p := range []string{"urn", "payload"} {
			if len(reTok(p).FindAllString(out, -1)) < len(reTok(p).FindAllString(c.src, -1)) {
				t.Errorf("%s: %s (do código depois da string) lido como nome:\n%s", c.nome, p, out)
			}
		}
	}
}

// Uma string que fecha na linha de cima não é abertura: o SQL solto que vem depois continua
// sendo lido inteiro (as linhas de continuação também).
func TestSQLDepoisDeStringFechada(t *testing.T) {
	m := novoTeste(t)
	out, _ := m.Mascarar("x = \"abc\"\nSELECT col_valor_hx FROM sch_vendas_hx.tb_pedido_hx\nWHERE col_data_hx > 1\n")
	for _, n := range []string{"tb_pedido_hx", "col_data_hx"} {
		if strings.Contains(out, n) {
			t.Errorf("%s em claro: o SQL depois de uma string fechada deixou de ser lido:\n%s", n, out)
		}
	}
}

// Outros formatos (CSV, JSON, YAML, esquema, conexão, linha de comando) em string de código,
// seguidos de código: os nomes do cliente (_hx, -hx) são mascarados e o código depois não.
var casosEmbutidos = []struct{ nome, src string }{
	{"csv em aspas triplas", "dados = \"\"\"tabela,coluna,tipo\ntb_pedido_hx,col_valor_hx,NUMBER\ntb_cliente_hx,col_doc_hx,VARCHAR\n\"\"\"\nfor linha in dados.splitlines():\n    item['urn'] = row['payload']\n"},
	{"json em string java", "String body = \"{\\\"database\\\": \\\"dw_vendas_hx\\\", \\\"schema\\\": \\\"sch_vendas_hx\\\", \\\"table\\\": \\\"tb_pedido_hx\\\"}\";\nitem.put(\"urn\", rs.getString(\"payload\"));\n"},
	{"conexao em python", "engine = create_engine(\"postgresql://svc_carga_hx@db-vendas-hx.interno:5432/dw_vendas_hx\")\nitem['urn'] = row['payload']\n"},
	{"yaml em aspas triplas", "cfg = yaml.safe_load(\"\"\"\ndatabase: dw_vendas_hx\nschema: sch_vendas_hx\nwarehouse: wh_carga_hx\n\"\"\")\nitem['urn'] = row['payload']\n"},
	{"esquema em string go", "ddl := `col_valor_hx    NUMBER(18,2)\ncol_data_hx     DATE\ncol_doc_hx      VARCHAR(14)`\nitem[\"urn\"] = row[\"payload\"]\n"},
	{"kubectl em lista python", "subprocess.run([\"kubectl\", \"-n\", \"ns-vendas-hx\", \"get\", \"pods\"])\nitem['urn'] = row['payload']\n"},
	{"kubectl em exec.Command go", "cmd := exec.Command(\"kubectl\", \"-n\", \"ns-vendas-hx\", \"get\", \"pods\")\nitem[\"urn\"] = row[\"payload\"]\n"},
	{"kubectl em ProcessBuilder java", "Process p = new ProcessBuilder(\"kubectl\", \"-n\", \"ns-vendas-hx\", \"get\", \"pods\").start();\nitem.put(\"urn\", rs.getString(\"payload\"));\n"},
}

func TestFormatosEmbutidosEmCodigo(t *testing.T) {
	m := novoTeste(t)
	reTok := func(w string) *regexp.Regexp {
		return regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(w) + `($|[^A-Za-z0-9_])`)
	}
	reHx := regexp.MustCompile(`[A-Za-z0-9_.-]*_hx\b|[A-Za-z0-9-]*-hx\b`)
	for _, c := range casosEmbutidos {
		out, _ := m.Mascarar(c.src)
		for _, n := range reHx.FindAllString(c.src, -1) {
			if reTok(n).MatchString(out) {
				t.Errorf("%s: %s em claro:\n%s", c.nome, n, out)
			}
		}
		for _, p := range []string{"urn", "payload"} {
			if len(reTok(p).FindAllString(out, -1)) < len(reTok(p).FindAllString(c.src, -1)) {
				t.Errorf("%s: %s (do código depois da string) lido como nome:\n%s", c.nome, p, out)
			}
		}
	}
}
