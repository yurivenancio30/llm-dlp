package mask

import "testing"

// A regra "o nome ao lado diz o tipo" em todas as sintaxes de chave → valor.

var casosChaveValor = []struct {
	nome, texto string
	nomes       []string
}{
	{"JSON coluna", `{"coluna": "vl_lancto", "column_name": "cd_conta", "fieldPath": "dt_lancto"}`, []string{"vl_lancto", "cd_conta", "dt_lancto"}},
	{"JSON lista", `{"tables": ["lanc_diario", "plano_contas"], "schema": "fin_contab"}`, []string{"lanc_diario", "plano_contas", "fin_contab"}},
	{"YAML fluxo", "tabelas: [lanc_diario, plano_contas]\n", []string{"lanc_diario", "plano_contas"}},
	{"YAML lista", "tables:\n  - lanc_diario\n  - plano_contas\nschema: fin_contab\n", []string{"lanc_diario", "plano_contas", "fin_contab"}},
	{"k=v na linha", "carga ok db=erp_fin schema=fin_contab tabela=lanc_diario rows=1200\n", []string{"erp_fin", "fin_contab", "lanc_diario"}},
	{"aspas simples", "cfg = {'database': 'erp_fin', 'schema': 'fin_contab', 'table': 'lanc_diario'}\n", []string{"erp_fin", "fin_contab", "lanc_diario"}},
	{"colado", "dbname=erp_fin tablename=lanc_diario schemaname=fin_contab rolename=papel_fin_leitura warehousename=wh_fin_carga accountname=acme-erpfin\n",
		[]string{"erp_fin", "lanc_diario", "fin_contab", "papel_fin_leitura", "wh_fin_carga", "acme-erpfin"}},
	{"datahub get", `{"urn": "urn:li:dataset:(urn:li:dataPlatform:snowflake,erp_fin.fin_contab.lanc_diario,PROD)",
 "aspects": {"schemaMetadata": {"value": {"schemaName": "erp_fin.fin_contab.lanc_diario",
   "fields": [{"fieldPath": "vl_lancto", "nativeDataType": "NUMBER(12,2)"}, {"fieldPath": "cd_conta", "nativeDataType": "VARCHAR(20)"}]}},
  "datasetProperties": {"value": {"name": "lanc_diario", "qualifiedName": "erp_fin.fin_contab.lanc_diario"}}}}`,
		[]string{"erp_fin", "fin_contab", "lanc_diario", "vl_lancto", "cd_conta"}},
	{"SQLAlchemy", `class Lancamento(Base):
    __tablename__ = 'lanc_diario'
    __table_args__ = {'schema': 'fin_contab'}
    valor = Column('vl_lancto', Numeric(12, 2))
    conta = Column(String(20), name='cd_conta')
t = Table('plano_contas', metadata, schema='fin_contab')
`, []string{"lanc_diario", "fin_contab", "vl_lancto", "cd_conta", "plano_contas"}},
	{"JPA", `@Entity
@Table(name = "lanc_diario", schema = "fin_contab")
public class Lancamento {
    @Column(name = "vl_lancto", precision = 12)
    private BigDecimal valor;
    @JoinColumn(name = "cd_conta")
    private Conta conta;
}
`, []string{"lanc_diario", "fin_contab", "vl_lancto", "cd_conta"}},
	{"Django", `class Lancamento(models.Model):
    conta = models.CharField(max_length=20, db_column='cd_conta')
    class Meta:
        db_table = 'lanc_diario'
`, []string{"cd_conta", "lanc_diario"}},
	{"Liquibase", `<createTable tableName="lanc_diario" schemaName="fin_contab">
    <column name="vl_lancto" type="NUMERIC(12,2)"/>
    <column name="cd_conta" type="VARCHAR(20)"/>
</createTable>
`, []string{"lanc_diario", "fin_contab", "vl_lancto", "cd_conta"}},
	{"XML elemento", "<datasource><database>erp_fin</database><schema>fin_contab</schema></datasource>\n", []string{"erp_fin", "fin_contab"}},
	{"ssh config", "Host bastiao-fin\n    HostName srv-bastiao-fin01\n    User svc_deploy_fin\n    Port 22\n", []string{"bastiao-fin", "srv-bastiao-fin01", "svc_deploy_fin"}},
	{"/etc/hosts", "127.0.0.1   localhost\n10.20.30.40   srv-erp-fin01 srv-erp-fin01.corp.local\n10.20.30.41\tdbfin02\n", []string{"srv-erp-fin01", "dbfin02"}},
	{"opção longa", "carga --database erp_fin --schema=fin_contab --table lanc_diario\n", []string{"erp_fin", "fin_contab", "lanc_diario"}},
	{"chave valor por espaço", "Database erp_fin\nSchema fin_contab\n", []string{"erp_fin", "fin_contab"}},
	{"name em objeto", `{"dataset": {"name": "erp_fin.fin_contab.lanc_diario", "owner": "joana_fin"}}`, []string{"erp_fin", "fin_contab", "lanc_diario", "joana_fin"}},
}

func TestChaveValorFormas(t *testing.T) {
	for _, c := range casosChaveValor {
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

func TestEntChaveNormalizada(t *testing.T) {
	for k, e := range map[string]string{"dbname": "database", "dbName": "database", "db_name": "database", "database_name": "database",
		"tableName": "tabela", "schemaname": "schema", "rolename": "usuario", "warehousename": "servico", "accountname": "conta_nuvem",
		"HostName": "servidor", "fieldPath": "coluna", "column_name": "coluna", "COLUMN": "coluna", "role": "usuario", "serviceAccountName": "usuario"} {
		if g, forte := entChave(k); g != e || !forte {
			t.Errorf("%s: %q forte=%v, esperado %q", k, g, forte, e)
		}
	}
}
