package mask

import (
	"regexp"
	"strings"
	"testing"
)

// Casos tirados da medição em repositórios públicos (datahub, airflow, dbt-core,
// sfquickstarts, kubernetes/examples): o formato real, com os nomes do cliente trocados por
// nomes inventados (_hx, -hx, contas e IDs falsos). "mascarar" tem de sair do texto; "manter"
// é o público do mesmo trecho, que tem de ficar como está.
type casoBench struct {
	nome      string
	src       string
	mascarar  []string
	manter    []string
	semLeitor bool // o caso não depende de leitor de estrutura (só para documentação)
}

func conferirBench(t *testing.T, casos []casoBench) {
	t.Helper()
	tok := func(w string) *regexp.Regexp {
		return regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(w) + `($|[^A-Za-z0-9_])`)
	}
	for _, c := range casos {
		m := novoTeste(t)
		out, _ := m.Mascarar(c.src)
		for _, w := range c.mascarar {
			if tok(w).MatchString(out) {
				t.Errorf("%s: %q em claro:\n%s", c.nome, w, out)
			}
		}
		for _, w := range c.manter {
			if n, k := len(tok(w).FindAllStringIndex(c.src, -1)), len(tok(w).FindAllStringIndex(out, -1)); k < n {
				t.Errorf("%s: %q (público) mascarado %d de %d:\n%s", c.nome, w, n-k, n, out)
			}
		}
	}
}

// L1: imagem de contêiner fora de manifesto completo, em Helm, JSON, Dockerfile e docker CLI;
// registro de nuvem com o projeto ou a conta no caminho ou no host.
func TestBenchImagens(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "trecho de manifesto sem kind", src: "    spec:\n      containers:\n        - name: api\n          image: registry.vendas-hx.com.br/time-dados-hx/api-cobranca-hx:1.4.2\n",
			mascarar: []string{"vendas-hx", "time-dados-hx", "api-cobranca-hx"}, manter: []string{"1.4.2", "containers"}},
		{nome: "ECR", src: "image: 812345678901.dkr.ecr.sa-east-1.amazonaws.com/pedidos-hx/api-cobranca-hx:1.4.2\n",
			mascarar: []string{"812345678901", "pedidos-hx", "api-cobranca-hx"}, manter: []string{"amazonaws.com", "1.4.2"}},
		{nome: "GCR e Artifact Registry", src: "image: gcr.io/projeto-vendas-hx/etl-carga-hx:latest\nimage: southamerica-east1-docker.pkg.dev/projeto-vendas-hx/repo-hx/worker-hx:2.0\n",
			mascarar: []string{"projeto-vendas-hx", "etl-carga-hx", "repo-hx", "worker-hx"}, manter: []string{"gcr.io", "pkg.dev", "latest"}},
		{nome: "Helm values", src: "image:\n  repository: harbor.interno-hx.net/dados-hx/airflow-hx\n  tag: 2.9.1\n  pullPolicy: IfNotPresent\n",
			mascarar: []string{"interno-hx", "dados-hx", "airflow-hx"}, manter: []string{"IfNotPresent", "2.9.1"}},
		{nome: "JSON", src: `{"containers": [{"name": "app", "image": "registry.vendas-hx.com.br/time-hx/meteor-hx:latest"}]}`,
			mascarar: []string{"vendas-hx", "time-hx", "meteor-hx"}},
		{nome: "Dockerfile e docker CLI", src: "FROM registry.vendas-hx.com.br/base-hx/python-hx:3.12\nRUN pip install x\n\n$ docker pull 812345678901.dkr.ecr.us-east-1.amazonaws.com/carga-hx:1.0\n",
			mascarar: []string{"vendas-hx", "base-hx", "python-hx", "812345678901", "carga-hx"}},
		{nome: "imagens públicas ficam", src: "image: nginx:1.27\nimage: bitnami/redis:7.2\nimage: registry.k8s.io/pause:3.9\nimage: docker.io/library/postgres:16\nimage: ghcr.io/apache/airflow:2.9.1\n",
			manter: []string{"nginx", "bitnami", "redis", "registry.k8s.io", "pause", "postgres", "apache", "airflow"}},
	})
}

// L2: o nome do recurso do cliente no host de serviço de nuvem.
func TestBenchHostsDeNuvem(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "RDS e Redshift", src: "\"host\": \"bd-pedidos-hx.c9akciq32.us-east-1.rds.amazonaws.com\",\nhost=\"dw-vendas-hx.abc123xyz.sa-east-1.redshift.amazonaws.com\"\n",
			mascarar: []string{"bd-pedidos-hx", "dw-vendas-hx"}, manter: []string{"rds", "redshift", "amazonaws.com", "us-east-1"}},
		{nome: "S3 no host", src: "Download https://dados-vendas-hx.s3.us-west-1.amazonaws.com/carga/arquivo.csv e https://relatorios-hx.s3.amazonaws.com/x.pdf\n",
			mascarar: []string{"dados-vendas-hx", "relatorios-hx"}, manter: []string{"s3", "amazonaws.com"}},
		{nome: "Azure", src: "Server=tcp:sqlsrv-vendas-hx.database.windows.net,1433;\nacr: acrvendashx.azurecr.io/api:1\nvault: https://kv-pedidos-hx.vault.azure.net/\n",
			// o host da connection string (Server=) é do cliente inteiro: o leitor de conexão o
			// mascara todo, como qualquer servidor de conexão
			mascarar: []string{"sqlsrv-vendas-hx", "acrvendashx", "kv-pedidos-hx"}, manter: []string{"azurecr.io", "vault.azure.net"}},
		{nome: "Databricks", src: "HOST = \"dbc-vendas-hx.cloud.databricks.com\"\nhost: adb-1234567890123456.7.azuredatabricks.net\n",
			mascarar: []string{"dbc-vendas-hx", "adb-1234567890123456"}, manter: []string{"cloud.databricks.com", "azuredatabricks.net"}},
		{nome: "Snowsight", src: "snowsight_base_url: \"https://app.org-hx-conta-hx.snowflakecomputing.com/\"\n",
			mascarar: []string{"org-hx-conta-hx"}, manter: []string{"app", "snowflakecomputing.com"}},
	})
}

// L3 e L4: conta de nuvem em ARN com curinga, ARN codificado em URL, conta do Snowflake e da
// AWS fora de URL.
func TestBenchContasDeNuvem(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "ARN com região curinga", src: "\"Resource\": [\n    \"arn:aws:glue:*:795512345678:schema/*\",\n    \"arn:aws:s3:::dados-vendas-hx/*\"\n]\n",
			mascarar: []string{"795512345678", "dados-vendas-hx"}, manter: []string{"glue", "schema", "Resource"}},
		{nome: "ARN codificado em URL", src: "AIRFLOW_CONN_AWS_DEFAULT=\"aws://?role_arn=arn%3Aaws%3Aiam%3A%3A240012345678%3Arole%2Fcarga-vendas-hx&region_name=us-east-1\"\n",
			mascarar: []string{"240012345678", "carga-vendas-hx"}, manter: []string{"role_arn", "region_name"}},
		{nome: "conta do Snowflake fora de URL", src: "config = SnowflakeV2Config(\n    account_id=\"XY12345.sa-east-1.aws\",\n)\nSNOWFLAKE_ACCOUNT=org-hx-conta-hx\naccount: ab98765.us-east-1\n",
			mascarar: []string{"XY12345", "org-hx-conta-hx", "ab98765"}, manter: []string{"account_id", "us-east-1"}},
		{nome: "conta AWS por chave", src: "DEFAULT_AWS_ACCOUNT_ID = \"240012345678\"\nCATALOG_ID = '849312345678'\n\"aws_account_id\": \"301234567891\"\n",
			mascarar: []string{"240012345678", "849312345678", "301234567891"}},
	})
}

// L5: o tipo pela chave, com prefixos e sufixos (TEST_GCP_PROJECT, GCP_PROJECT_ID, DATASET_ID)
// e em dicionário.
func TestBenchChavesVariantes(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "constantes", src: "TEST_GCP_PROJECT = \"projeto-vendas-hx\"\nGCP_PROJECT_ID = \"projeto-carga-hx\"\nDATASET_ID = \"ds_vendas_hx\"\nBQ_DATASET = \"ds_pedidos_hx\"\n",
			mascarar: []string{"projeto-vendas-hx", "projeto-carga-hx", "ds_vendas_hx", "ds_pedidos_hx"}},
		{nome: "dicionário", src: "default_args={\"project\": \"projeto-kylin-hx\", \"cube\": \"cubo_vendas\"}\nconn = {\"host\": \"bd-interno-hx.corp-hx.local\", \"schema\": \"stg_vendas_hx\"}\n",
			mascarar: []string{"projeto-kylin-hx", "bd-interno-hx", "stg_vendas_hx"}},
		{nome: "o valor é o nome do campo (não é valor)", src: "CONTAINER_NAME = \"container_name\"\nFILTER_PROJECT_ID = \"project_id\"\nDATASET_ID = \"dataset_id\"\nfields = [{\"name\": \"project_id\", \"label\": \"Project\"}, {\"name\": \"account_id\"}]\n",
			manter: []string{"container_name", "project_id", "dataset_id", "account_id"}},
	})
}

// L6 e F: SQL do dbt e do Snowflake (Jinja, comentários com traços, CTE).
func TestBenchSQLDbt(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "model do dbt", src: "with source as (\n\n    select * from {{ source('erp_hx','tb_pedido_hx') }}\n\n),\n\nrenamed as (\n\n    select\n\n        ----------  ids\n        id as order_id,\n        cod_loja_hx as location_id,\n\n        ---------- numerics\n        vl_total_hx as order_total\n\n    from source\n\n)\n\nselect * from renamed\n",
			// o alias (order_id) também é nome do cliente: pode ser mascarado; o nome da CTE é local
			mascarar: []string{"erp_hx", "tb_pedido_hx", "cod_loja_hx", "vl_total_hx"}, manter: []string{"renamed", "source"}},
		{nome: "CTE não é tabela", src: "WITH vendas_dia AS (\n  SELECT dt, SUM(vl_venda_hx) AS total FROM dw_hx.fato_venda_hx GROUP BY dt\n)\nSELECT * FROM vendas_dia\n",
			mascarar: []string{"vl_venda_hx", "dw_hx", "fato_venda_hx"}, manter: []string{"vendas_dia"}},
	})
}

// L7: saída de kubectl get (a primeira coluna é o nome do objeto).
func TestBenchKubectlGet(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "get svc", src: "$ kubectl get svc -n loja\nNAME             TYPE           CLUSTER-IP     EXTERNAL-IP   PORT(S)\napi-pedidos-hx   LoadBalancer   10.0.171.239   34.95.120.7   80:31380/TCP\nworker-carga-hx  ClusterIP      10.0.12.4      <none>        8080/TCP\n",
			mascarar: []string{"api-pedidos-hx", "worker-carga-hx", "10.0.171.239", "34.95.120.7"}, manter: []string{"LoadBalancer", "ClusterIP"}},
		{nome: "get pods", src: "```\nkubectl get pods\n\nNAME                              READY   STATUS    RESTARTS   AGE\ncarga-pedidos-hx-7d9f8b6c5-x2x4z    1/1     Running   0          3d\n",
			mascarar: []string{"carga-pedidos-hx-7d9f8b6c5-x2x4z"}, manter: []string{"Running"}},
		{nome: "tabela solta sem o comando (fica com a dica)", src: "NAME          STATUS    AGE\nmysql-0       Running   3d\n",
			manter: []string{"mysql-0", "Running"}},
	})
}

// L8: URN do DataHub com usuário, instância e nome de uma parte só.
func TestBenchURNs(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "urns", src: "\"actor\": \"urn:li:corpuser:joao.silva.hx\",\n\"instance\": \"urn:li:dataPlatformInstance:(urn:li:dataPlatform:airflow,airflow-prd-hx)\",\n\"dataset\": \"urn:li:dataset:(urn:li:dataPlatform:postgres,pedido_hx,PROD)\"\n",
			mascarar: []string{"joao.silva.hx", "airflow-prd-hx", "pedido_hx"}, manter: []string{"airflow", "postgres", "PROD", "dataPlatform"}},
	})
}

// IP público no contexto que diz que é da infraestrutura (EXTERNAL-IP, loadBalancer.ingress,
// ExternalIP). Fora desse contexto, o IP público continua como está (opção ip_publico).
func TestBenchIPPublicoNoContexto(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "status do Service", src: "status:\n  loadBalancer:\n    ingress:\n    - ip: 34.95.120.7\n", mascarar: []string{"34.95.120.7"}},
		{nome: "ExternalIP do nó", src: "\"addresses\": [\n  {\"type\": \"ExternalIP\", \"address\": \"104.197.41.23\"},\n  {\"type\": \"InternalIP\", \"address\": \"10.240.0.4\"}\n]\n", mascarar: []string{"104.197.41.23", "10.240.0.4"}},
		{nome: "IP público fora de contexto fica", src: "O DNS do Google é 8.8.8.8 e o da Cloudflare é 1.1.1.1.\n", manter: []string{"8.8.8.8", "1.1.1.1"}},
	})
}

// A e B: o SQL não é lido em prosa, e palavra comum decidida não se espalha pelo texto.
func TestBenchSQLEmProsa(t *testing.T) {
	licenca := "# Licensed to the Apache Software Foundation (ASF) under one\n# or more contributor license agreements.  See the NOTICE file\n"
	conferirBench(t, []casoBench{
		{nome: "comentário com create", src: licenca + "    /**\n     * Create a DatabaseOperations implementation for the specified database type.\n     */\n",
			manter: []string{"the", "a", "DatabaseOperations", "specified"}},
		{nome: "prosa com on e inner", src: licenca + "| `exit code` | `breeze run` propagate the inner command's exit code: 0 = success. For `airflow dags test`, do not rely on the exit code alone — also read the final Dag run states. |\n",
			manter: []string{"the", "inner", "exit", "alone", "final"}},
		{nome: "SQL de verdade no mesmo arquivo ainda é lido", src: licenca + "query = \"SELECT cod_cli_hx FROM dw_hx.tb_cliente_hx WHERE ativo = 1\"\n",
			mascarar: []string{"cod_cli_hx", "dw_hx", "tb_cliente_hx"}, manter: []string{"the", "Licensed"}},
	})
}

// C, D e E: vocabulário fixo, pedaços de URL e mock do Python não são nomes.
func TestBenchVocabularioECodigo(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "papel de ARIA e de chat", src: "const b = await screen.findByRole(\"button\", { name: /confirm/i });\npage.getByRole(\"status\");\nconversation = [{\"role\": \"assistant\", \"content\": x}, {\"role\": \"user\"}]\n",
			manter: []string{"button", "status", "assistant", "user"}},
		{nome: "esquema de URL e pacote Java", src: "TEST_CONN_SCHEMA = \"https\"\n<groupId>org.slf4j</groupId>\n<component group=\"org.slf4j\" name=\"slf4j-api\" version=\"1.7.36\">\n'slf4jApi': \"org.slf4j:slf4j-api:$slf4jVersion\",\n",
			manter: []string{"https", "org.slf4j", "slf4j-api"}},
		{nome: "pedaços de URL", src: "See `Creating IAM Policy <https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies_create-console.html>`__.\nhosts:\n  - name: airflow.example.com\nprivate static final String URL = \"https://example.org/a\";\n",
			manter: []string{"com", "https", "docs.aws.amazon.com", "example.com", "org"}},
		{nome: "mock do Python", src: "self.conn.cursor.return_value = self.cur\nself.cur.fetch_arrow_table.return_value = table\nmock_session.scalars.return_value = mock_scalars\nresponse.json.return_value = json_data\n",
			manter: []string{"return_value", "table", "cursor"}},
		{nome: "import do Python", src: "from fastapi import Depends, HTTPException, Query, Response, status\nraise HTTPException(status.HTTP_404_NOT_FOUND, f\"Unable to obtain dag with id {dag_id}\")\n",
			manter: []string{"status", "fastapi", "dag_id"}},
	})
}

// Os mesmos erros nos trechos reais (Apache Airflow, DataHub; licença Apache 2.0) em que a
// medição os achou: a decisão errada vinha de outra linha do mesmo arquivo e se espalhava.
func TestBenchTrechosReais(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "doc da AWS (Select ... from scratch)", src: "6. Review your web identity information and then choose **Next: Permissions**.\n" +
			"7. Select the policy to use for the permissions policy or choose **Create policy** to open a new browser tab and create a new policy from scratch. For more information, see `Creating IAM Policy <https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies_create-console.html>`__.\n" +
			"9. (Optional) Add metadata to the role by attaching tags as key–value pairs.\n",
			manter: []string{"the", "policy", "scratch", "com", "https", "role"}},
		{nome: "SQL citado em comentário", src: "    def test_get_records_duplicate_column_names(self):\n        # Two columns both named \"id\" — as produced by SELECT a.id, b.id FROM a JOIN b.\n" +
			"        table = pa.table({\"a_id\": [10, 20], \"b_id\": [30, 40]}).rename_columns([\"id\", \"id\"])\n        self.cur.fetch_arrow_table.return_value = table\n\n        result = self.hook.get_records(\"SELECT 1\")\n",
			manter: []string{"return_value", "table", "result"}},
		{nome: "select() do SQLAlchemy", src: "    dag_model = session.scalar(select(DagModel).where(DagModel.dag_id == dag_id).options(*eager_load_teams()))\n" +
			"    if not dag_model:\n        raise HTTPException(status.HTTP_404_NOT_FOUND, f\"Unable to obtain dag with id {dag_id} from session\")\n",
			manter: []string{"status", "session", "DagModel", "dag_id"}},
		{nome: "Javadoc com Create", src: "  /**\n   * Create a DatabaseOperations implementation for the specified database type.\n   *\n   * @param dbType the database type\n   */\n",
			manter: []string{"the", "specified", "type"}},
		{nome: "tabela markdown", src: "| `exit code` | `breeze run` / `breeze shell \"<cmd>\"` propagate the inner command's exit code: 0 = success, non-zero = failure. For `airflow dags test`, do not rely on the exit code alone — also read the final Dag run / task instance states and any traceback in the output. |\n",
			manter: []string{"the", "inner", "exit", "final"}},
		{nome: "domínio e classe Java como valor", src: "hosts:\n  - name: airflow.example.com\n    tls:\n      enabled: true\nSCHEMA_REGISTRY_KAFKASTORE_SASL_JAAS_CONFIG=com.sun.security.auth.module.PlainLoginModule required;\n",
			manter: []string{"com", "example"}},
		{nome: "JSON Patch e pastas de repositório", src: "assertEquals(op.getPath(), \"/elements/https:~1~1example.org~1~0user~1a/note~0 1\");\n/metadata-ingestion/tests\n/metadata-ingestion/examples\n",
			manter: []string{"https", "examples", "tests"}},
		{nome: "parágrafo recuado de RST", src: ".. warning::\n\n" +
			"  Both options only reach tables whose cleanup configuration declares a DAG column. Every other table is\n" +
			"  cleaned whatever you pass, so ``--exclude-dag-ids`` does not preserve everything connected to the DAGs\n" +
			"  you name -- ``trigger``, ``callback`` and ``import_error`` declare none, for instance, so the filters\n" +
			"  never narrow them.\n",
			manter: []string{"so", "the", "everything", "filters"}},
	})
}

// Schema aprendido numa mensagem não faz de "mock.<schema>.return_value = x" uma tabela na
// seguinte: atribuição a atributo é código.
func TestBenchAtribuicaoAtributo(t *testing.T) {
	m := novoTeste(t)
	m.Mascarar("select * from fin_contab.t_lanc_diario d join fin_contab.t_saldo_mes s on s.cd = d.cd join fin_contab.t_plano_contas p on p.cd = d.cd\n")
	src := "        mock_scalars.first.return_value = role\n        mock_session.fin_contab.return_value = mock_scalars\n"
	out, _ := m.Mascarar(src)
	for _, w := range []string{"return_value", "mock_session", "mock_scalars"} {
		if !strings.Contains(out, w) {
			t.Errorf("%q mascarado:\n%s", w, out)
		}
	}
	// o nome qualificado fora da atribuição continua pego
	if out, _ := m.Mascarar("copiado para hx_dw.fin_contab.vendas_hx ontem\n"); strings.Contains(out, "vendas_hx") {
		t.Errorf("qualificado com schema conhecido em claro:\n%s", out)
	}
}

// Valor sem dono (gabarito de nomes, ver docs): nome feito só de papel/placeholder, de tipo e de
// numeração não é nome de ninguém; um pedaço distintivo basta para continuar nome do cliente.
func TestBenchValorSemDono(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "placeholders de teste", src: "TEST_BUCKET = \"test-staging-bucket\"\nhook = Hook(username=\"stub-user\", host=\"source_host\", database=\"actual_database\")\n" +
			"CONTAINER_NAME = \"azure_container\"\ncfg = {\"namespace\": \"mock_namespace\", \"login\": \"my_user\", \"username\": \"XXXXXXXXX\"}\n",
			manter: []string{"test-staging-bucket", "stub-user", "source_host", "actual_database", "azure_container", "mock_namespace", "my_user", "XXXXXXXXX"}},
		{nome: "pedaço distintivo continua nome", src: "TEST_BUCKET = \"test-sinistros-hx\"\nhook = Hook(username=\"svc_cobranca_hx\", host=\"pg-faturamento-hx\", database=\"insurance_claims_db\")\n" +
			"cfg = {\"namespace\": \"ns-pedidos-hx\", \"login\": \"my_vendas_hx\"}\n",
			mascarar: []string{"test-sinistros-hx", "svc_cobranca_hx", "pg-faturamento-hx", "insurance_claims_db", "ns-pedidos-hx", "my_vendas_hx"}},
		{nome: "palavra sozinha e tipo + número seguem os outros freios", src: "Server=srv-hx-01\\INST01,1433;Database=vendas_hx;\nselect * from RAW_HX.SRC.PEDIDOS_HX;\n",
			mascarar: []string{"INST01", "SRC", "PEDIDOS_HX"}},
		{nome: "conta de exemplo da documentação", src: "AWS_ACCOUNT_ID = \"111122223333\"\nACCOUNT_ID = \"000000000000\"\nREAL_ACCOUNT_ID = \"339713033063\"\n",
			manter: []string{"111122223333", "000000000000"}, mascarar: []string{"339713033063"}},
		// número falso junto de um nome: o nome é do dono
		{nome: "nome com número de preenchimento", src: "ACCOUNT_ID = \"vendashx-99999999-9999\"\n",
			mascarar: []string{"vendashx"}},
		// escrita não latina não é sinal de tradução: para um cliente russo, chinês ou coreano o
		// nome real também é nessa escrita
		{nome: "nome em escrita não latina", src: "{\"database\": \"Продажи_хх\", \"schema\": \"客户数据\", \"user\": \"판매팀\"}\n",
			mascarar: []string{"Продажи_хх", "客户数据", "판매팀"}},
		{nome: "namespace de grupo público e de empresa", src: "LOGGER_NAMESPACE = \"org.apache.airflow\"\nAPP_NAMESPACE = \"com.vendashx.billing\"\n",
			manter: []string{"org.apache.airflow"}, mascarar: []string{"vendashx"}},
	})
}

// Serviço com o nome da imagem oficial que roda é o software, não o cliente; imagem de
// organização (acme/acme-api) não libera o nome.
func TestBenchSoftwareDaImagem(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "compose com imagens oficiais", src: "services:\n  redis:\n    image: redis:7.2\n  postgres:\n    image: postgres:16\n  minio:\n    image: minio/minio:latest\n" +
			"  api-cobranca-hx:\n    image: vendashx/api-cobranca-hx:1.0\n    depends_on:\n      - redis\n      - postgres\n",
			manter: []string{"redis", "postgres", "minio"}, mascarar: []string{"api-cobranca-hx"}},
		{nome: "k8s com nome derivado do software", src: "kind: Deployment\nmetadata:\n  name: redis-master\nspec:\n  template:\n    spec:\n      containers:\n      - name: redis\n        image: redis:7\n",
			manter: []string{"redis-master"}},
		// imagem sem organização fora das Imagens Oficiais do Docker é imagem local: o nome é do dono
		{nome: "imagem local construída", src: "services:\n  server:\n    container_name: yudao-server\n    build:\n      context: ./yudao-server/\n    image: yudao-server\n",
			mascarar: []string{"yudao-server"}},
		// organização igual ao nome só prova software com palavra pública; "acme/acme" não
		{nome: "organização igual ao nome, de empresa", src: "services:\n  vendashx:\n    image: vendashx/vendashx:2.1\n",
			mascarar: []string{"vendashx"}},
		// imagem de fornecedor: as duas chaves (organização fornecedora e nome de software); um
		// pedaço do dono no nome, ou uma organização desconhecida, mascara
		{nome: "imagem de fornecedor", src: "image: clickhouse/clickhouse-server:24.3\nimage: quay.io/prometheus/statsd-exporter:v0.26\n",
			manter: []string{"clickhouse/clickhouse-server", "prometheus/statsd-exporter"}},
		{nome: "pedaço do dono na imagem de fornecedor", src: "image: grafana/billing-vendashx:1.0\n", mascarar: []string{"billing-vendashx"}},
		{nome: "organização do dono com nome de software", src: "image: ghcr.io/vendashx/clickhouse-server:24.3\n", mascarar: []string{"ghcr.io/vendashx"}},
	})
}

var _ = strings.TrimSpace

// Nome em escrita não latina é nome como outro qualquer, em toda estrutura que o lê: para um
// cliente russo, chinês ou coreano o banco, a tabela e o usuário reais são nessa escrita.
func TestBenchEscritaNaoLatina(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "SQL", src: "SELECT сумма FROM продажи_хх.клиенты;\nCREATE TABLE 客户数据 (编号 INT);\nUSE 판매DB;\n",
			mascarar: []string{"продажи_хх", "клиенты", "客户数据", "판매DB"}},
		{nome: "código, YAML e .env", src: "conn = connect(database=\"Продажи_хх\", user=\"판매팀\")\nschema: 客户数据\nDB_USER=josé.antônio\n",
			mascarar: []string{"Продажи_хх", "판매팀", "客户数据", "josé.antônio"}},
		{nome: "URL de conexão", src: "jdbc:postgresql://pg-hx:5432/Продажи_хх\n", mascarar: []string{"Продажи_хх"}},
	})
}

// Serviço do Compose é qualquer filho de "services:" com uma chave de serviço da especificação,
// inclusive o que só herda de uma âncora ("<<: *comum") e não tem imagem própria.
func TestBenchServicoCompose(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "herança e depends_on", src: "x-comum: &comum\n  restart: always\nservices:\n  api-cobranca-hx:\n    <<: *comum\n" +
			"  redis:\n    image: redis:7\n  fila-sinistro-hx:\n    depends_on: [redis]\n",
			manter: []string{"redis"}, mascarar: []string{"api-cobranca-hx", "fila-sinistro-hx"}},
		{nome: "services do GitLab CI não é compose", src: "test:\n  services:\n    - name: postgres:16\n      alias: db\n      command: [\"postgres\"]\n",
			manter: []string{"postgres:16", "alias: db"}},
	})
}

// JSON de dados com rótulos que são frases ("Nome do Cliente", como sai de uma planilha): tem a
// mesma forma de um catálogo de tradução ("Manage Role": "Gestionați Rolul"), e por isso
// nenhuma regra libera o valor pela forma da chave. Um freio assim existiu e deixava passar o
// nome da pessoa, o usuário e o servidor; o custo de não tê-lo é o rótulo traduzido de "User"
// ou "Role" sair mascarado num catálogo em outra escrita.
func TestBenchRotulosEmFrase(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "registro único", src: "{\n  \"Nome do Cliente\": \"Marilda Quixabeira\",\n  \"Nome da Mãe\": \"Ana Quixabeira\",\n" +
			"  \"Estado Civil\": \"Casada\",\n  \"Cidade Natal\": \"Sao Paulo\",\n  \"Plano Contratado\": \"Ouro\",\n" +
			"  \"Usuario\": \"mquixabeira\",\n  \"Servidor\": \"srv-hx-01\"\n}\n",
			mascarar: []string{"Marilda Quixabeira", "Ana Quixabeira", "mquixabeira", "srv-hx-01"}},
		{nome: "lista de registros", src: "[\n" +
			" {\"Nome do Cliente\": \"Marilda Quixabeira\", \"Data de Cadastro\": \"2024-01-02\", \"Plano Contratado\": \"Ouro\", \"Database\": \"vendashx_prod\"},\n" +
			" {\"Nome do Cliente\": \"Jurandir Taquaritinga\", \"Data de Cadastro\": \"2024-01-03\", \"Plano Contratado\": \"Prata\", \"Database\": \"cobrancahx\"}\n]\n",
			mascarar: []string{"Marilda Quixabeira", "Jurandir Taquaritinga", "vendashx_prod", "cobrancahx"}},
		{nome: "configuração JSON", src: "{\"user\": \"jsilva_hx\", \"role\": \"fin_hx_reader\", \"database\": \"vendas_hx\"}\n",
			mascarar: []string{"jsilva_hx", "vendas_hx"}},
	})
}

// Comando à vista acima da tabela (terminal colado): a listagem de qualquer tipo tipa a coluna
// de identidade (dags -> dag_id) e o cabeçalho é da ferramenta; SQL no comando não.
func TestBenchComandoAVista(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "airflow dags list", src: "$ airflow dags list\ndag_id                | owners  | is_paused\n======================+=========+==========\n" +
			"carga_sinistros_hx    | airflow | False\nfaturamento_hx_diario | airflow | True\n",
			manter: []string{"dag_id", "is_paused"}, mascarar: []string{"carga_sinistros_hx", "faturamento_hx_diario"}},
		{nome: "gcloud projects list", src: "$ gcloud projects list\nPROJECT_ID          NAME          PROJECT_NUMBER\nseguradora-hx-prod  seguradorahx  123456789012\n",
			mascarar: []string{"seguradora-hx-prod"}},
		{nome: "SQL no comando: cabeçalho é coluna", src: "$ psql -c \"select cod_sinistro_hx, vlr_pago_hx from apolice_hx\"\n cod_sinistro_hx | vlr_pago_hx\n-----------------+-------------\n 1               | 2\n",
			mascarar: []string{"cod_sinistro_hx"}},
		{nome: "frase não é comando", src: "Then list the tables below\nname | rows\n-----+-----\nfoo  | 3\n", manter: []string{"foo"}},
		// só listagem: debaixo de um comando que mostra arquivo ou roda script, o cabeçalho é do dono
		{nome: "cat de CSV: cabeçalho é coluna", src: "$ cat apolices.csv\ncod_apolice_hx,vlr_premio_hx,nome_segurado\n1,2,3\n4,5,6\n7,8,9\n",
			mascarar: []string{"cod_apolice_hx", "vlr_premio_hx"}},
		{nome: "script: cabeçalho é coluna", src: "$ python relatorio.py\ncod_apolice_hx   vlr_premio_hx   dt_emissao_hx\n1                2               3\n4                5               6\n7                8               9\n",
			mascarar: []string{"cod_apolice_hx", "vlr_premio_hx", "dt_emissao_hx"}},
	})
}

// Traceback: a linha que aponta para dependência ou biblioteca padrão é do fornecedor e fica; a
// que aponta para o projeto é do cliente (função, módulo, código e o valor citado no erro).
func TestBenchTraceback(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "python", src: "Traceback (most recent call last):\n" +
			"  File \"/home/ubuntu/seguradora-hx/etl/carga_sinistros_hx.py\", line 42, in calcular_premio_hx\n" +
			"    total = apolice_hx.premio_liquido()\n" +
			"  File \"/opt/venv/lib/python3.11/site-packages/pandas/core/frame.py\", line 3761, in __getitem__\n" +
			"    indexer = self.columns.get_loc(key)\n" +
			"  File \"/usr/lib/python3.11/json/decoder.py\", line 337, in decode\n" +
			"    obj, end = self.raw_decode(s, idx=_w(s, 0).end())\n" +
			"KeyError: 'COD_APOLICE_HX'\n",
			// a linha do pandas fica (função e código); as pastas "pandas/core" do caminho seguem
			// a regra de caminho de sempre (pandas é palavra de dicionário: não para o leitor)
			manter:   []string{"__getitem__", "get_loc", "raw_decode", "json/decoder.py", "ubuntu"},
			mascarar: []string{"seguradora-hx", "carga_sinistros_hx", "calcular_premio_hx", "apolice_hx", "premio_liquido", "COD_APOLICE_HX"}},
		{nome: "python: dependência que não é palavra de dicionário fica inteira", src: "Traceback (most recent call last):\n" +
			"  File \"/home/ubuntu/seguradora-hx/etl/carga_hx.py\", line 7, in gravar_sinistros_hx\n" +
			"    conn.execute(sql_hx)\n" +
			"  File \"/opt/venv/lib/python3.11/site-packages/sqlalchemy/engine/base.py\", line 1967, in _exec_single_context\n" +
			"    self.dialect.do_execute(cursor, str_statement, effective_parameters, context)\n" +
			"RuntimeError: falhou\n",
			manter:   []string{"site-packages/sqlalchemy/engine/base.py", "_exec_single_context", "do_execute"},
			mascarar: []string{"gravar_sinistros_hx", "sql_hx"}},
		{nome: "pacote interno instalado no ambiente", src: "Traceback (most recent call last):\n" +
			"  File \"/opt/venv/lib/python3.11/site-packages/vendashx_core/precos.py\", line 9, in calcular_tarifa_hx\n" +
			"    return tabela_hx[cod]\n" +
			"KeyError: 'PLANO_OURO_HX'\n",
			mascarar: []string{"calcular_tarifa_hx", "tabela_hx", "PLANO_OURO_HX"}},
		{nome: "pacote interno com nome de papel (common)", src: "Traceback (most recent call last):\n" +
			"  File \"/opt/venv/lib/python3.11/site-packages/common/precos_hx.py\", line 9, in calcular_tarifa_hx\n" +
			"    return tabela_hx[cod]\n" +
			"KeyError: 'PLANO_OURO_HX'\n",
			mascarar: []string{"precos_hx", "calcular_tarifa_hx", "tabela_hx", "PLANO_OURO_HX"}},
		{nome: "projeto com pasta lib/python3", src: "Traceback (most recent call last):\n" +
			"  File \"/home/ubuntu/proj-hx/lib/python3/carga_hx.py\", line 3, in somar_premio_hx\n" +
			"    return premio_hx + taxa_hx\n" +
			"NameError: name 'taxa_hx' is not defined\n",
			mascarar: []string{"somar_premio_hx", "premio_hx", "carga_hx"}},
		{nome: "java", src: "java.lang.IllegalStateException: falhou\n" +
			"\tat br.com.seguradorahx.sinistros.CargaSinistros.calcularPremio(CargaSinistros.java:42)\n" +
			"\tat org.springframework.boot.SpringApplication.run(SpringApplication.java:315)\n",
			manter: []string{"org.springframework.boot.SpringApplication.run"}, mascarar: []string{"seguradorahx", "CargaSinistros", "calcularPremio"}},
		{nome: "javascript", src: "TypeError: x is undefined\n" +
			"    at calcularTarifaHx (/srv/app-hx/src/tarifas.js:12:7)\n" +
			"    at Layer.handle (/srv/app-hx/node_modules/express/lib/router/layer.js:95:5)\n" +
			"    at Function.map (/srv/app-hx/node_modules/lodash/lodash.js:9590:14)\n",
			manter: []string{"Layer.handle", "Function.map", "node_modules/lodash/lodash.js"}, mascarar: []string{"calcularTarifaHx"}},
		{nome: "go", src: "panic: runtime error: index out of range\n\ngoroutine 1 [running]:\n" +
			"main.calcularPremioHx(0x1)\n\t/home/ubuntu/seguradora-hx/cmd/premio.go:42 +0x1d\n" +
			"runtime.goexit()\n\t/usr/local/go/src/runtime/asm_amd64.s:1650 +0x1\n",
			manter: []string{"runtime.goexit"}, mascarar: []string{"calcularPremioHx", "seguradora-hx"}},
		{nome: "go: a pasta internal do projeto é do cliente", src: "panic: boom\n\ngoroutine 1 [running]:\n" +
			"github.com/seguradorahx/faturahx/internal/tarifas.calcularTarifaHx(0x1)\n\t/home/ubuntu/faturahx/internal/tarifas/calc.go:7 +0x1d\n",
			manter: []string{"github.com", "internal"}, mascarar: []string{"calcularTarifaHx", "seguradorahx", "faturahx/internal", "tarifas"}},
		{nome: "go: receptor e biblioteca padrão", src: "goroutine 7 [running]:\n" +
			"github.com/seguradorahx/faturahx/servico.(*CargaSinistrosHx).Calcular(0x1)\n\t/srv/build/servico/carga.go:7 +0x1d\n" +
			"net/http.(*conn).serve(0xc0001)\n\t/usr/local/go/src/net/http/server.go:2009 +0x5f4\n",
			manter: []string{"net/http.(*conn).serve"}, mascarar: []string{"CargaSinistrosHx", "seguradorahx"}},
		// no cache de módulos, o módulo privado do cliente é do cliente; o público (dono
		// fornecedor e repositório popular) é do fornecedor
		{nome: "go: módulo privado no cache", src: "goroutine 1 [running]:\n" +
			"github.com/seguradora-hx/lib-tarifas-hx/calc.CalcularPremioHx(0x1)\n\t/home/ubuntu/go/pkg/mod/github.com/seguradora-hx/lib-tarifas-hx@v1.2.0/calc/calc.go:12 +0x1d\n",
			mascarar: []string{"seguradora-hx", "lib-tarifas-hx", "CalcularPremioHx"}},
		{nome: "go: módulo público no cache", src: "goroutine 1 [running]:\n" +
			"github.com/stretchr/testify/assert.ObjectsAreEqual(0x1)\n\t/home/ubuntu/go/pkg/mod/github.com/stretchr/testify@v1.8.4/assert/assertions.go:65 +0x1d\n" +
			"golang.org/x/sync/errgroup.(*Group).Go.func1()\n\t/home/ubuntu/go/pkg/mod/golang.org/x/sync@v0.6.0/errgroup/errgroup.go:78 +0x56\n",
			// ("testify@v1.8.4" no caminho sai como e-mail: é do detector de e-mail, não daqui)
			manter: []string{"github.com/stretchr/testify/assert.ObjectsAreEqual", "assert/assertions.go", "errgroup.(*Group).Go"}},
		{nome: "maven: artefato privado no repositório local", src: "lendo /home/ubuntu/.m2/repository/br/com/seguradora-hx/tarifas-hx/1.0/pom.xml\n",
			mascarar: []string{"seguradora-hx", "tarifas-hx"}},
		{nome: "python: pasta lib/python_x do projeto é do cliente", src: "Traceback (most recent call last):\n" +
			"  File \"/home/ubuntu/faturahx/lib/python_etl/carga_hx.py\", line 3, in somar_premio_hx\n" +
			"    return premio_hx + taxa_hx\n" +
			"NameError: name 'taxa_hx' is not defined\n",
			mascarar: []string{"somar_premio_hx", "premio_hx", "taxa_hx", "python_etl"}},
	})
}

// Os freios de prosa e de código do leitor de SQL não podem cortar SQL de verdade: apelido de
// tabela que é artigo do inglês (an, the, this), SELECT colado ao parêntese e a lista de SET
// com a coluna qualificada no começo da linha. O construtor de consultas continua fora.
func TestBenchSQLQueOsFreiosNaoCortam(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "apelido an", src: "SELECT an.cod_apolice_hx, an.vlr_hx FROM apolices_hx an WHERE an.dt_emissao_hx > 1 AND an.cd_filial_hx = 2;\n",
			mascarar: []string{"cod_apolice_hx", "vlr_hx", "apolices_hx", "dt_emissao_hx", "cd_filial_hx"}},
		{nome: "apelido an em minúsculas", src: "select an.cod_apolice_hx from apolices_hx an where an.dt_emissao_hx > 1 and an.cd_filial_hx = 2;\n",
			mascarar: []string{"cod_apolice_hx", "apolices_hx", "dt_emissao_hx", "cd_filial_hx"}},
		{nome: "apelidos the e this", src: "SELECT this.cod_apolice_hx FROM apolices_hx this JOIN sinistros_hx the ON the.cod_sin_hx = this.cod_apolice_hx WHERE the.vlr_pago_hx > 0;\n",
			mascarar: []string{"cod_apolice_hx", "apolices_hx", "sinistros_hx", "cod_sin_hx", "vlr_pago_hx"}},
		{nome: "SELECT colado ao parêntese", src: "SELECT(cod_apolice_hx) FROM apolices_hx WHERE dt_emissao_hx > 1;\n",
			mascarar: []string{"cod_apolice_hx", "apolices_hx", "dt_emissao_hx"}},
		{nome: "SET com coluna qualificada por linha", src: "UPDATE vendashx.dbo.apolices_hx\nSET\n  vendashx.dbo.apolices_hx.status_hx = 'x',\n" +
			"  vendashx.dbo.apolices_hx.vlr_premio_hx = 2\nWHERE cod_apolice_hx = 1;\n",
			mascarar: []string{"vendashx", "apolices_hx", "status_hx", "vlr_premio_hx", "cod_apolice_hx"}},
		{nome: "construtor de consultas não é SQL", src: "stmt = select(Usuario).where(Usuario.ativo == True)\nresultado = sessao.execute(stmt)\n",
			manter: []string{"select(Usuario).where(Usuario.ativo"}},
		{nome: "prosa com artigo não é SQL", src: "Select the policy you want to remove from the list below.\n",
			manter: []string{"policy", "list"}},
	})
}

// Grupo em domínio invertido: io.<empresa> e dev.<empresa> são da empresa (o domínio .io ou .dev
// é dela), como com.<empresa>; só o grupo público conhecido fica.
func TestBenchGrupoDeEmpresa(t *testing.T) {
	conferirBench(t, []casoBench{
		{nome: "namespace io da empresa", src: "{\"type\": \"record\", \"namespace\": \"io.empresahx.pagamentos\", \"name\": \"PedidoHx\"}\n",
			mascarar: []string{"io.empresahx.pagamentos"}},
		{nome: "namespace dev da empresa", src: "{\"type\": \"record\", \"namespace\": \"dev.empresahx.pagamentos\", \"name\": \"PedidoHx\"}\n",
			mascarar: []string{"dev.empresahx.pagamentos"}},
		{nome: "grupo público", src: "{\"type\": \"record\", \"namespace\": \"org.apache.kafka.connect\", \"name\": \"Envelope\"}\n",
			manter: []string{"org.apache.kafka.connect"}},
		{nome: "maven: grupo io da empresa no repositório local", src: "lendo /home/ubuntu/.m2/repository/io/empresa-hx/tarifas-hx/1.0/pom.xml\n",
			mascarar: []string{"empresa-hx", "tarifas-hx"}},
	})
}
