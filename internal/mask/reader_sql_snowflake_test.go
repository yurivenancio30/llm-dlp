package mask

import "testing"

// The same set of Snowflake names in every shape.

var snowNames = []string{"acme-erpfin", "papel_fin_leitura", "wh_fin_carga", "pol_mascara_cpf", "stg_notas_fin", "tsk_carga_fin"}

var snowCases = []struct {
	name, text string
	names      []string
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

func TestSnowflakeForms(t *testing.T) {
	for _, c := range snowCases {
		for _, tr := range transports {
			m := newTest(t)
			txt := tr.f(c.text)
			if ok, tot := checkMasked(t, m, c.name+"/"+tr.name, txt, c.names); ok != tot {
				out, _ := m.Mask(txt)
				t.Errorf("%s / %s: %d/%d\n%s", c.name, tr.name, ok, tot, out)
			}
		}
	}
}

// once learned, the names are masked loose in the text
func TestSnowflakeLoose(t *testing.T) {
	m := newTest(t)
	m.Mask("SNOWFLAKE_ACCOUNT=acme-erpfin\nSNOWFLAKE_ROLE=papel_fin_leitura\nSNOWFLAKE_WAREHOUSE=wh_fin_carga\n")
	txt := "a conta acme-erpfin usa o papel papel_fin_leitura no wh_fin_carga"
	if ok, tot := checkMasked(t, m, "solto", txt, []string{"acme-erpfin", "papel_fin_leitura", "wh_fin_carga"}); ok != tot {
		out, _ := m.Mask(txt)
		t.Errorf("solto: %d/%d: %s", ok, tot, out)
	}
}
