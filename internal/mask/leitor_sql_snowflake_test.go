package mask

import "testing"

// O mesmo conjunto de nomes do Snowflake em todas as formas.

var nomesSnow = []string{"acme-erpfin", "papel_fin_leitura", "wh_fin_carga", "pol_mascara_cpf", "stg_notas_fin", "tsk_carga_fin"}

var casosSnow = []struct {
	nome, texto string
	nomes       []string
}{
	{"connections.toml", "[connections.fin]\naccount = \"acme-erpfin\"\nuser = \"svc_carga_fin\"\nrole = \"papel_fin_leitura\"\nwarehouse = \"wh_fin_carga\"\n",
		[]string{"acme-erpfin", "svc_carga_fin", "papel_fin_leitura", "wh_fin_carga"}},
	{"YAML dbt", "fin:\n  outputs:\n    dev:\n      type: snowflake\n      account: acme-erpfin\n      role: papel_fin_leitura\n      warehouse: wh_fin_carga\n",
		[]string{"acme-erpfin", "papel_fin_leitura", "wh_fin_carga"}},
	{".env", "SNOWFLAKE_ACCOUNT=acme-erpfin\nSNOWFLAKE_ROLE=papel_fin_leitura\nSNOWFLAKE_WAREHOUSE=wh_fin_carga\n",
		[]string{"acme-erpfin", "papel_fin_leitura", "wh_fin_carga"}},
	{"JSON", `{"account": "acme-erpfin", "role": "papel_fin_leitura", "warehouse": "wh_fin_carga"}`,
		[]string{"acme-erpfin", "papel_fin_leitura", "wh_fin_carga"}},
	{"conector", `conn = snowflake.connector.connect(account="acme-erpfin", role="papel_fin_leitura", warehouse="wh_fin_carga")`,
		[]string{"acme-erpfin", "papel_fin_leitura", "wh_fin_carga"}},
	{"host e URL", "https://acme-erpfin.snowflakecomputing.com/console\nhttps://app.snowflake.com/acme/erpfin_prd/worksheets\n",
		[]string{"acme-erpfin", "erpfin_prd"}},
	{"SQL", `USE ROLE papel_fin_leitura;
USE WAREHOUSE wh_fin_carga;
GRANT SELECT ON ALL TABLES IN SCHEMA fin_contab TO ROLE papel_fin_leitura;
GRANT ROLE papel_fin_leitura TO USER svc_carga_fin;
REVOKE USAGE ON WAREHOUSE wh_fin_carga FROM ROLE papel_fin_leitura;
CREATE ROLE IF NOT EXISTS papel_fin_escrita;
ALTER WAREHOUSE wh_fin_carga SET WAREHOUSE_SIZE = 'XSMALL';
CREATE OR REPLACE MASKING POLICY erp_fin.fin_contab.pol_mascara_cpf AS (v STRING) RETURNS STRING -> v;
CREATE STAGE erp_fin.fin_contab.stg_notas_fin URL = 's3://x/';
CREATE TASK erp_fin.fin_contab.tsk_carga_fin WAREHOUSE = wh_fin_carga SCHEDULE = '5 MINUTE' AS SELECT 1;
COPY INTO erp_fin.fin_contab.lanc_diario FROM @erp_fin.fin_contab.stg_notas_fin;
`, []string{"papel_fin_leitura", "wh_fin_carga", "svc_carga_fin", "papel_fin_escrita", "pol_mascara_cpf", "stg_notas_fin", "tsk_carga_fin"}},
	{"SHOW em JSON", `[{"created_on": "2026-01-01", "name": "lanc_diario", "database_name": "erp_fin", "schema_name": "fin_contab", "owner": "papel_fin_leitura"}]`,
		[]string{"lanc_diario", "erp_fin", "fin_contab", "papel_fin_leitura"}},
	{"SHOW tabela", `+---------------------+-------------+---------------+-------------+-------------------+
| created_on          | name        | database_name | schema_name | owner             |
|---------------------+-------------+---------------+-------------+-------------------|
| 2026-01-01 00:00:00 | lanc_diario | erp_fin       | fin_contab  | papel_fin_leitura |
+---------------------+-------------+---------------+-------------+-------------------+
`, []string{"lanc_diario", "erp_fin", "fin_contab", "papel_fin_leitura"}},
	{"listas allow/deny", `{"allowed_roles": ["papel_fin_leitura", "papel_fin_escrita"], "blocked_users": ["svc_carga_fin"]}`,
		[]string{"papel_fin_leitura", "papel_fin_escrita", "svc_carga_fin"}},
	{"URN DB2", "urn:li:dataset:(urn:li:dataPlatform:db2,FINDB   .LANCDIARIO,PROD)\n", []string{"FINDB", "LANCDIARIO"}},
}

func TestSnowflakeFormas(t *testing.T) {
	for _, c := range casosSnow {
		for _, tr := range transportes {
			m := novoTeste(t)
			txt := tr.f(c.texto)
			if ok, tot := conferirMascarado(t, m, c.nome+"/"+tr.nome, txt, c.nomes); ok != tot {
				out, _ := m.Mascarar(txt)
				t.Errorf("%s / %s: %d/%d\n%s", c.nome, tr.nome, ok, tot, out)
			}
		}
	}
}

// depois de aprendidos, os nomes são mascarados soltos no texto
func TestSnowflakeSolto(t *testing.T) {
	m := novoTeste(t)
	m.Mascarar("SNOWFLAKE_ACCOUNT=acme-erpfin\nSNOWFLAKE_ROLE=papel_fin_leitura\nSNOWFLAKE_WAREHOUSE=wh_fin_carga\n")
	txt := "a conta acme-erpfin usa o papel papel_fin_leitura no wh_fin_carga"
	if ok, tot := conferirMascarado(t, m, "solto", txt, []string{"acme-erpfin", "papel_fin_leitura", "wh_fin_carga"}); ok != tot {
		out, _ := m.Mascarar(txt)
		t.Errorf("solto: %d/%d: %s", ok, tot, out)
	}
}
