package mask

import (
	"regexp"
	"strings"
	"testing"
)

// Cases taken from the measurement on public repositories (datahub, airflow, dbt-core,
// sfquickstarts, kubernetes/examples): the real format, with the client names replaced by made-up
// names (_hx, -hx, fake accounts and IDs). "mask" has to leave the text; "keep" is the public part
// of the same piece, which has to stay as it is.
type benchCase struct {
	name     string
	src      string
	maskIt   []string
	keep     []string
	noReader bool // the case does not depend on a structure reader (documentation only)
}

func checkBench(t *testing.T, cases []benchCase) {
	t.Helper()
	tok := func(w string) *regexp.Regexp {
		return regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(w) + `($|[^A-Za-z0-9_])`)
	}
	for _, c := range cases {
		m := newTest(t)
		out, _ := m.Mask(c.src)
		for _, w := range c.maskIt {
			if tok(w).MatchString(out) {
				t.Errorf("%s: %q em claro:\n%s", c.name, w, out)
			}
		}
		for _, w := range c.keep {
			if n, k := len(tok(w).FindAllStringIndex(c.src, -1)), len(tok(w).FindAllStringIndex(out, -1)); k < n {
				t.Errorf("%s: %q (público) mascarado %d de %d:\n%s", c.name, w, n-k, n, out)
			}
		}
	}
}

// L1: container image outside a complete manifest, in Helm, JSON, Dockerfile and docker CLI; cloud
// registry with the project or the account in the path or in the host.
func TestBenchImages(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "trecho de manifesto sem kind", src: "    spec:\n      containers:\n        - name: api\n          image: registry.vendas-hx.com.br/time-dados-hx/api-cobranca-hx:1.4.2\n",
			maskIt: []string{"vendas-hx", "time-dados-hx", "api-cobranca-hx"}, keep: []string{"1.4.2", "containers"}},
		{name: "ECR", src: "image: 812345678901.dkr.ecr.sa-east-1.amazonaws.com/pedidos-hx/api-cobranca-hx:1.4.2\n",
			maskIt: []string{"812345678901", "pedidos-hx", "api-cobranca-hx"}, keep: []string{"amazonaws.com", "1.4.2"}},
		{name: "GCR e Artifact Registry", src: "image: gcr.io/projeto-vendas-hx/etl-carga-hx:latest\nimage: southamerica-east1-docker.pkg.dev/projeto-vendas-hx/repo-hx/worker-hx:2.0\n",
			maskIt: []string{"projeto-vendas-hx", "etl-carga-hx", "repo-hx", "worker-hx"}, keep: []string{"gcr.io", "pkg.dev", "latest"}},
		{name: "Helm values", src: "image:\n  repository: harbor.interno-hx.net/dados-hx/airflow-hx\n  tag: 2.9.1\n  pullPolicy: IfNotPresent\n",
			maskIt: []string{"interno-hx", "dados-hx", "airflow-hx"}, keep: []string{"IfNotPresent", "2.9.1"}},
		{name: "JSON", src: `{"containers": [{"name": "app", "image": "registry.vendas-hx.com.br/time-hx/meteor-hx:latest"}]}`,
			maskIt: []string{"vendas-hx", "time-hx", "meteor-hx"}},
		{name: "Dockerfile e docker CLI", src: "FROM registry.vendas-hx.com.br/base-hx/python-hx:3.12\nRUN pip install x\n\n$ docker pull 812345678901.dkr.ecr.us-east-1.amazonaws.com/carga-hx:1.0\n",
			maskIt: []string{"vendas-hx", "base-hx", "python-hx", "812345678901", "carga-hx"}},
		{name: "imagens públicas ficam", src: "image: nginx:1.27\nimage: bitnami/redis:7.2\nimage: registry.k8s.io/pause:3.9\nimage: docker.io/library/postgres:16\nimage: ghcr.io/apache/airflow:2.9.1\n",
			keep: []string{"nginx", "bitnami", "redis", "registry.k8s.io", "pause", "postgres", "apache", "airflow"}},
	})
}

// L2: the name of the client resource in the cloud service host.
func TestBenchCloudHosts(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "RDS e Redshift", src: "\"host\": \"bd-pedidos-hx.c9akciq32.us-east-1.rds.amazonaws.com\",\nhost=\"dw-vendas-hx.abc123xyz.sa-east-1.redshift.amazonaws.com\"\n",
			maskIt: []string{"bd-pedidos-hx", "dw-vendas-hx"}, keep: []string{"rds", "redshift", "amazonaws.com", "us-east-1"}},
		{name: "S3 no host", src: "Download https://dados-vendas-hx.s3.us-west-1.amazonaws.com/carga/arquivo.csv e https://relatorios-hx.s3.amazonaws.com/x.pdf\n",
			maskIt: []string{"dados-vendas-hx", "relatorios-hx"}, keep: []string{"s3", "amazonaws.com"}},
		{name: "Azure", src: "Server=tcp:sqlsrv-vendas-hx.database.windows.net,1433;\nacr: acrvendashx.azurecr.io/api:1\nvault: https://kv-pedidos-hx.vault.azure.net/\n",
			// the host of the connection string (Server=) belongs wholly to the client: the
			// connection reader masks all of it, like any connection server
			maskIt: []string{"sqlsrv-vendas-hx", "acrvendashx", "kv-pedidos-hx"}, keep: []string{"azurecr.io", "vault.azure.net"}},
		{name: "Databricks", src: "HOST = \"dbc-vendas-hx.cloud.databricks.com\"\nhost: adb-1234567890123456.7.azuredatabricks.net\n",
			maskIt: []string{"dbc-vendas-hx", "adb-1234567890123456"}, keep: []string{"cloud.databricks.com", "azuredatabricks.net"}},
		{name: "Snowsight", src: "snowsight_base_url: \"https://app.org-hx-conta-hx.snowflakecomputing.com/\"\n",
			maskIt: []string{"org-hx-conta-hx"}, keep: []string{"app", "snowflakecomputing.com"}},
	})
}

// L3 and L4: cloud account in an ARN with a wildcard, URL-encoded ARN, Snowflake and AWS account
// outside a URL.
func TestBenchCloudAccounts(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "ARN com região curinga", src: "\"Resource\": [\n    \"arn:aws:glue:*:795512345678:schema/*\",\n    \"arn:aws:s3:::dados-vendas-hx/*\"\n]\n",
			maskIt: []string{"795512345678", "dados-vendas-hx"}, keep: []string{"glue", "schema", "Resource"}},
		{name: "ARN codificado em URL", src: "AIRFLOW_CONN_AWS_DEFAULT=\"aws://?role_arn=arn%3Aaws%3Aiam%3A%3A240012345678%3Arole%2Fcarga-vendas-hx&region_name=us-east-1\"\n",
			maskIt: []string{"240012345678", "carga-vendas-hx"}, keep: []string{"role_arn", "region_name"}},
		{name: "conta do Snowflake fora de URL", src: "config = SnowflakeV2Config(\n    account_id=\"XY12345.sa-east-1.aws\",\n)\nSNOWFLAKE_ACCOUNT=org-hx-conta-hx\naccount: ab98765.us-east-1\n",
			maskIt: []string{"XY12345", "org-hx-conta-hx", "ab98765"}, keep: []string{"account_id", "us-east-1"}},
		{name: "conta AWS por chave", src: "DEFAULT_AWS_ACCOUNT_ID = \"240012345678\"\nCATALOG_ID = '849312345678'\n\"aws_account_id\": \"301234567891\"\n",
			maskIt: []string{"240012345678", "849312345678", "301234567891"}},
	})
}

// L5: the type by the key, with prefixes and suffixes (TEST_GCP_PROJECT, GCP_PROJECT_ID,
// DATASET_ID) and in a dictionary.
func TestBenchKeyVariants(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "constantes", src: "TEST_GCP_PROJECT = \"projeto-vendas-hx\"\nGCP_PROJECT_ID = \"projeto-carga-hx\"\nDATASET_ID = \"ds_vendas_hx\"\nBQ_DATASET = \"ds_pedidos_hx\"\n",
			maskIt: []string{"projeto-vendas-hx", "projeto-carga-hx", "ds_vendas_hx", "ds_pedidos_hx"}},
		{name: "dicionário", src: "default_args={\"project\": \"projeto-kylin-hx\", \"cube\": \"cubo_vendas\"}\nconn = {\"host\": \"bd-interno-hx.corp-hx.local\", \"schema\": \"stg_vendas_hx\"}\n",
			maskIt: []string{"projeto-kylin-hx", "bd-interno-hx", "stg_vendas_hx"}},
		{name: "o valor é o nome do campo (não é valor)", src: "CONTAINER_NAME = \"container_name\"\nFILTER_PROJECT_ID = \"project_id\"\nDATASET_ID = \"dataset_id\"\nfields = [{\"name\": \"project_id\", \"label\": \"Project\"}, {\"name\": \"account_id\"}]\n",
			keep: []string{"container_name", "project_id", "dataset_id", "account_id"}},
	})
}

// L6 and F: dbt and Snowflake SQL (Jinja, dash comments, CTE).
func TestBenchSQLDbt(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "model do dbt", src: "with source as (\n\n    select * from {{ source('erp_hx','tb_pedido_hx') }}\n\n),\n\nrenamed as (\n\n    select\n\n        ----------  ids\n        id as order_id,\n        cod_loja_hx as location_id,\n\n        ---------- numerics\n        vl_total_hx as order_total\n\n    from source\n\n)\n\nselect * from renamed\n",
			// the alias (order_id) is also a client name: it may be masked; the CTE name is local
			maskIt: []string{"erp_hx", "tb_pedido_hx", "cod_loja_hx", "vl_total_hx"}, keep: []string{"renamed", "source"}},
		{name: "CTE não é tabela", src: "WITH vendas_dia AS (\n  SELECT dt, SUM(vl_venda_hx) AS total FROM dw_hx.fato_venda_hx GROUP BY dt\n)\nSELECT * FROM vendas_dia\n",
			maskIt: []string{"vl_venda_hx", "dw_hx", "fato_venda_hx"}, keep: []string{"vendas_dia"}},
	})
}

// L7: kubectl get output (the first column is the object name).
func TestBenchKubectlGet(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "get svc", src: "$ kubectl get svc -n loja\nNAME             TYPE           CLUSTER-IP     EXTERNAL-IP   PORT(S)\napi-pedidos-hx   LoadBalancer   10.0.171.239   34.95.120.7   80:31380/TCP\nworker-carga-hx  ClusterIP      10.0.12.4      <none>        8080/TCP\n",
			maskIt: []string{"api-pedidos-hx", "worker-carga-hx", "10.0.171.239", "34.95.120.7"}, keep: []string{"LoadBalancer", "ClusterIP"}},
		{name: "get pods", src: "```\nkubectl get pods\n\nNAME                              READY   STATUS    RESTARTS   AGE\ncarga-pedidos-hx-7d9f8b6c5-x2x4z    1/1     Running   0          3d\n",
			maskIt: []string{"carga-pedidos-hx-7d9f8b6c5-x2x4z"}, keep: []string{"Running"}},
		{name: "tabela solta sem o comando (fica com a dica)", src: "NAME          STATUS    AGE\nmysql-0       Running   3d\n",
			keep: []string{"mysql-0", "Running"}},
	})
}

// L8: DataHub URN with a user, an instance and a single-part name.
func TestBenchURNs(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "urns", src: "\"actor\": \"urn:li:corpuser:joao.silva.hx\",\n\"instance\": \"urn:li:dataPlatformInstance:(urn:li:dataPlatform:airflow,airflow-prd-hx)\",\n\"dataset\": \"urn:li:dataset:(urn:li:dataPlatform:postgres,pedido_hx,PROD)\"\n",
			maskIt: []string{"joao.silva.hx", "airflow-prd-hx", "pedido_hx"}, keep: []string{"airflow", "postgres", "PROD", "dataPlatform"}},
		// the client's application (system), even a short acronym
		{name: "urn de aplicação", src: "\"application\": \"urn:li:application:SGP\"\n",
			maskIt: []string{"SGP"}, keep: []string{"application", "urn:li"}},
	})
}

// Public IP in the context that says it belongs to the infrastructure (EXTERNAL-IP,
// loadBalancer.ingress, ExternalIP). Outside that context, the public IP stays as it is (public_ip
// option).
func TestBenchPublicIPInContext(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "status do Service", src: "status:\n  loadBalancer:\n    ingress:\n    - ip: 34.95.120.7\n", maskIt: []string{"34.95.120.7"}},
		{name: "ExternalIP do nó", src: "\"addresses\": [\n  {\"type\": \"ExternalIP\", \"address\": \"104.197.41.23\"},\n  {\"type\": \"InternalIP\", \"address\": \"10.240.0.4\"}\n]\n", maskIt: []string{"104.197.41.23", "10.240.0.4"}},
		{name: "IP público fora de contexto fica", src: "O DNS do Google é 8.8.8.8 e o da Cloudflare é 1.1.1.1.\n", keep: []string{"8.8.8.8", "1.1.1.1"}},
	})
}

// A and B: SQL is not read in prose, and a decided common word does not spread through the text.
func TestBenchSQLInProse(t *testing.T) {
	license := "# Licensed to the Apache Software Foundation (ASF) under one\n# or more contributor license agreements.  See the NOTICE file\n"
	checkBench(t, []benchCase{
		{name: "comentário com create", src: license + "    /**\n     * Create a DatabaseOperations implementation for the specified database type.\n     */\n",
			keep: []string{"the", "a", "DatabaseOperations", "specified"}},
		{name: "prosa com on e inner", src: license + "| `exit code` | `breeze run` propagate the inner command's exit code: 0 = success. For `airflow dags test`, do not rely on the exit code alone — also read the final Dag run states. |\n",
			keep: []string{"the", "inner", "exit", "alone", "final"}},
		{name: "SQL de verdade no mesmo arquivo ainda é lido", src: license + "query = \"SELECT cod_cli_hx FROM dw_hx.tb_cliente_hx WHERE ativo = 1\"\n",
			maskIt: []string{"cod_cli_hx", "dw_hx", "tb_cliente_hx"}, keep: []string{"the", "Licensed"}},
	})
}

// C, D and E: fixed vocabulary, URL pieces and Python mocks are not names.
func TestBenchVocabularyAndCode(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "papel de ARIA e de chat", src: "const b = await screen.findByRole(\"button\", { name: /confirm/i });\npage.getByRole(\"status\");\nconversation = [{\"role\": \"assistant\", \"content\": x}, {\"role\": \"user\"}]\n",
			keep: []string{"button", "status", "assistant", "user"}},
		{name: "esquema de URL e pacote Java", src: "TEST_CONN_SCHEMA = \"https\"\n<groupId>org.slf4j</groupId>\n<component group=\"org.slf4j\" name=\"slf4j-api\" version=\"1.7.36\">\n'slf4jApi': \"org.slf4j:slf4j-api:$slf4jVersion\",\n",
			keep: []string{"https", "org.slf4j", "slf4j-api"}},
		{name: "pedaços de URL", src: "See `Creating IAM Policy <https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies_create-console.html>`__.\nhosts:\n  - name: airflow.example.com\nprivate static final String URL = \"https://example.org/a\";\n",
			keep: []string{"com", "https", "docs.aws.amazon.com", "example.com", "org"}},
		{name: "mock do Python", src: "self.conn.cursor.return_value = self.cur\nself.cur.fetch_arrow_table.return_value = table\nmock_session.scalars.return_value = mock_scalars\nresponse.json.return_value = json_data\n",
			keep: []string{"return_value", "table", "cursor"}},
		{name: "import do Python", src: "from fastapi import Depends, HTTPException, Query, Response, status\nraise HTTPException(status.HTTP_404_NOT_FOUND, f\"Unable to obtain dag with id {dag_id}\")\n",
			keep: []string{"status", "fastapi", "dag_id"}},
	})
}

// The same errors in the real snippets (Apache Airflow, DataHub; Apache 2.0 license) where the
// measurement found them: the wrong decision came from another line of the same file and spread.
func TestBenchRealSnippets(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "doc da AWS (Select ... from scratch)", src: "6. Review your web identity information and then choose **Next: Permissions**.\n" +
			"7. Select the policy to use for the permissions policy or choose **Create policy** to open a new browser tab and create a new policy from scratch. For more information, see `Creating IAM Policy <https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies_create-console.html>`__.\n" +
			"9. (Optional) Add metadata to the role by attaching tags as key–value pairs.\n",
			keep: []string{"the", "policy", "scratch", "com", "https", "role"}},
		{name: "SQL citado em comentário", src: "    def test_get_records_duplicate_column_names(self):\n        # Two columns both named \"id\" — as produced by SELECT a.id, b.id FROM a JOIN b.\n" +
			"        table = pa.table({\"a_id\": [10, 20], \"b_id\": [30, 40]}).rename_columns([\"id\", \"id\"])\n        self.cur.fetch_arrow_table.return_value = table\n\n        result = self.hook.get_records(\"SELECT 1\")\n",
			keep: []string{"return_value", "table", "result"}},
		{name: "select() do SQLAlchemy", src: "    dag_model = session.scalar(select(DagModel).where(DagModel.dag_id == dag_id).options(*eager_load_teams()))\n" +
			"    if not dag_model:\n        raise HTTPException(status.HTTP_404_NOT_FOUND, f\"Unable to obtain dag with id {dag_id} from session\")\n",
			keep: []string{"status", "session", "DagModel", "dag_id"}},
		{name: "Javadoc com Create", src: "  /**\n   * Create a DatabaseOperations implementation for the specified database type.\n   *\n   * @param dbType the database type\n   */\n",
			keep: []string{"the", "specified", "type"}},
		{name: "tabela markdown", src: "| `exit code` | `breeze run` / `breeze shell \"<cmd>\"` propagate the inner command's exit code: 0 = success, non-zero = failure. For `airflow dags test`, do not rely on the exit code alone — also read the final Dag run / task instance states and any traceback in the output. |\n",
			keep: []string{"the", "inner", "exit", "final"}},
		{name: "domínio e classe Java como valor", src: "hosts:\n  - name: airflow.example.com\n    tls:\n      enabled: true\nSCHEMA_REGISTRY_KAFKASTORE_SASL_JAAS_CONFIG=com.sun.security.auth.module.PlainLoginModule required;\n",
			keep: []string{"com", "example"}},
		{name: "JSON Patch e pastas de repositório", src: "assertEquals(op.getPath(), \"/elements/https:~1~1example.org~1~0user~1a/note~0 1\");\n/metadata-ingestion/tests\n/metadata-ingestion/examples\n",
			keep: []string{"https", "examples", "tests"}},
		{name: "parágrafo recuado de RST", src: ".. warning::\n\n" +
			"  Both options only reach tables whose cleanup configuration declares a DAG column. Every other table is\n" +
			"  cleaned whatever you pass, so ``--exclude-dag-ids`` does not preserve everything connected to the DAGs\n" +
			"  you name -- ``trigger``, ``callback`` and ``import_error`` declare none, for instance, so the filters\n" +
			"  never narrow them.\n",
			keep: []string{"so", "the", "everything", "filters"}},
	})
}

// A schema learned in one message does not make "mock.<schema>.return_value = x" a table in the
// next one: an assignment to an attribute is code.
func TestBenchAttributeAssignment(t *testing.T) {
	m := newTest(t)
	m.Mask("select * from fin_contab.t_lanc_diario d join fin_contab.t_saldo_mes s on s.cd = d.cd join fin_contab.t_plano_contas p on p.cd = d.cd\n")
	src := "        mock_scalars.first.return_value = role\n        mock_session.fin_contab.return_value = mock_scalars\n"
	out, _ := m.Mask(src)
	for _, w := range []string{"return_value", "mock_session", "mock_scalars"} {
		if !strings.Contains(out, w) {
			t.Errorf("%q mascarado:\n%s", w, out)
		}
	}
	// the qualified name outside the assignment is still caught
	if out, _ := m.Mask("copiado para hx_dw.fin_contab.vendas_hx ontem\n"); strings.Contains(out, "vendas_hx") {
		t.Errorf("qualificado com schema conhecido em claro:\n%s", out)
	}
}

// Ownerless value (name template, see docs): a name made only of role/placeholder, type and
// numbering is nobody's name; one distinctive piece is enough for it to stay a client name.
func TestBenchOwnerlessValue(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "placeholders de teste", src: "TEST_BUCKET = \"test-staging-bucket\"\nhook = Hook(username=\"stub-user\", host=\"source_host\", database=\"actual_database\")\n" +
			"CONTAINER_NAME = \"azure_container\"\ncfg = {\"namespace\": \"mock_namespace\", \"login\": \"my_user\", \"username\": \"XXXXXXXXX\"}\n",
			keep: []string{"test-staging-bucket", "stub-user", "source_host", "actual_database", "azure_container", "mock_namespace", "my_user", "XXXXXXXXX"}},
		{name: "pedaço distintivo continua nome", src: "TEST_BUCKET = \"test-sinistros-hx\"\nhook = Hook(username=\"svc_cobranca_hx\", host=\"pg-faturamento-hx\", database=\"insurance_claims_db\")\n" +
			"cfg = {\"namespace\": \"ns-pedidos-hx\", \"login\": \"my_vendas_hx\"}\n",
			maskIt: []string{"test-sinistros-hx", "svc_cobranca_hx", "pg-faturamento-hx", "insurance_claims_db", "ns-pedidos-hx", "my_vendas_hx"}},
		{name: "palavra sozinha e tipo + número seguem os outros freios", src: "Server=srv-hx-01\\INST01,1433;Database=vendas_hx;\nselect * from RAW_HX.SRC.PEDIDOS_HX;\n",
			maskIt: []string{"INST01", "SRC", "PEDIDOS_HX"}},
		{name: "conta de exemplo da documentação", src: "AWS_ACCOUNT_ID = \"111122223333\"\nACCOUNT_ID = \"000000000000\"\nREAL_ACCOUNT_ID = \"339713033063\"\n",
			keep: []string{"111122223333", "000000000000"}, maskIt: []string{"339713033063"}},
		// a fake number next to a name: the name belongs to the owner
		{name: "nome com número de preenchimento", src: "ACCOUNT_ID = \"vendashx-99999999-9999\"\n",
			maskIt: []string{"vendashx"}},
		// non-Latin script is not a sign of translation: for a Russian, Chinese or Korean client
		// the real name is also in that script
		{name: "nome em escrita não latina", src: "{\"database\": \"Продажи_хх\", \"schema\": \"客户数据\", \"user\": \"판매팀\"}\n",
			maskIt: []string{"Продажи_хх", "客户数据", "판매팀"}},
		{name: "namespace de grupo público e de empresa", src: "LOGGER_NAMESPACE = \"org.apache.airflow\"\nAPP_NAMESPACE = \"com.vendashx.billing\"\n",
			keep: []string{"org.apache.airflow"}, maskIt: []string{"vendashx"}},
	})
}

// A service with the name of the official image it runs is the software, not the client; an
// organization's image (acme/acme-api) does not release the name.
func TestBenchImageSoftware(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "compose com imagens oficiais", src: "services:\n  redis:\n    image: redis:7.2\n  postgres:\n    image: postgres:16\n  minio:\n    image: minio/minio:latest\n" +
			"  api-cobranca-hx:\n    image: vendashx/api-cobranca-hx:1.0\n    depends_on:\n      - redis\n      - postgres\n",
			keep: []string{"redis", "postgres", "minio"}, maskIt: []string{"api-cobranca-hx"}},
		{name: "k8s com nome derivado do software", src: "kind: Deployment\nmetadata:\n  name: redis-master\nspec:\n  template:\n    spec:\n      containers:\n      - name: redis\n        image: redis:7\n",
			keep: []string{"redis-master"}},
		// an image without an organization outside the Docker Official Images is a local image: the
		// name belongs to the owner
		{name: "imagem local construída", src: "services:\n  server:\n    container_name: yudao-server\n    build:\n      context: ./yudao-server/\n    image: yudao-server\n",
			maskIt: []string{"yudao-server"}},
		// an organization equal to the name only proves software with a public word; "acme/acme"
		// does not
		{name: "organização igual ao nome, de empresa", src: "services:\n  vendashx:\n    image: vendashx/vendashx:2.1\n",
			maskIt: []string{"vendashx"}},
		// vendor image: the two keys (vendor organization and software name); a piece of the owner
		// in the name, or an unknown organization, masks
		{name: "imagem de fornecedor", src: "image: clickhouse/clickhouse-server:24.3\nimage: quay.io/prometheus/statsd-exporter:v0.26\n",
			keep: []string{"clickhouse/clickhouse-server", "prometheus/statsd-exporter"}},
		{name: "pedaço do dono na imagem de fornecedor", src: "image: grafana/billing-vendashx:1.0\n", maskIt: []string{"billing-vendashx"}},
		{name: "organização do dono com nome de software", src: "image: ghcr.io/vendashx/clickhouse-server:24.3\n", maskIt: []string{"ghcr.io/vendashx"}},
	})
}

var _ = strings.TrimSpace

// A name in non-Latin script is a name like any other, in every structure that reads it: for a
// Russian, Chinese or Korean client the real database, table and user are in that script.
func TestBenchNonLatinScript(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "SQL", src: "SELECT сумма FROM продажи_хх.клиенты;\nCREATE TABLE 客户数据 (编号 INT);\nUSE 판매DB;\n",
			maskIt: []string{"продажи_хх", "клиенты", "客户数据", "판매DB"}},
		{name: "código, YAML e .env", src: "conn = connect(database=\"Продажи_хх\", user=\"판매팀\")\nschema: 客户数据\nDB_USER=josé.antônio\n",
			maskIt: []string{"Продажи_хх", "판매팀", "客户数据", "josé.antônio"}},
		{name: "URL de conexão", src: "jdbc:postgresql://pg-hx:5432/Продажи_хх\n", maskIt: []string{"Продажи_хх"}},
	})
}

// A Compose service is any child of "services:" with a service key of the specification, including
// one that only inherits from an anchor ("<<: *comum") and has no image of its own.
func TestBenchComposeService(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "herança e depends_on", src: "x-comum: &comum\n  restart: always\nservices:\n  api-cobranca-hx:\n    <<: *comum\n" +
			"  redis:\n    image: redis:7\n  fila-sinistro-hx:\n    depends_on: [redis]\n",
			keep: []string{"redis"}, maskIt: []string{"api-cobranca-hx", "fila-sinistro-hx"}},
		{name: "services do GitLab CI não é compose", src: "test:\n  services:\n    - name: postgres:16\n      alias: db\n      command: [\"postgres\"]\n",
			keep: []string{"postgres:16", "alias: db"}},
	})
}

// Data JSON with labels that are sentences ("Nome do Cliente", as it comes out of a spreadsheet):
// it has the same shape as a translation catalog ("Manage Role": "Gestionați Rolul"), and so no
// rule releases the value by the shape of the key. A brake like that existed and let through the
// person's name, the user and the server; the cost of not having it is the translated label of
// "User" or "Role" going out masked in a catalog in another script.
func TestBenchLabelsInSentence(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "registro único", src: "{\n  \"Nome do Cliente\": \"Marilda Quixabeira\",\n  \"Nome da Mãe\": \"Ana Quixabeira\",\n" +
			"  \"Estado Civil\": \"Casada\",\n  \"Cidade Natal\": \"Sao Paulo\",\n  \"Plano Contratado\": \"Ouro\",\n" +
			"  \"Usuario\": \"mquixabeira\",\n  \"Servidor\": \"srv-hx-01\"\n}\n",
			maskIt: []string{"Marilda Quixabeira", "Ana Quixabeira", "mquixabeira", "srv-hx-01"}},
		{name: "lista de registros", src: "[\n" +
			" {\"Nome do Cliente\": \"Marilda Quixabeira\", \"Data de Cadastro\": \"2024-01-02\", \"Plano Contratado\": \"Ouro\", \"Database\": \"vendashx_prod\"},\n" +
			" {\"Nome do Cliente\": \"Jurandir Taquaritinga\", \"Data de Cadastro\": \"2024-01-03\", \"Plano Contratado\": \"Prata\", \"Database\": \"cobrancahx\"}\n]\n",
			maskIt: []string{"Marilda Quixabeira", "Jurandir Taquaritinga", "vendashx_prod", "cobrancahx"}},
		{name: "configuração JSON", src: "{\"user\": \"jsilva_hx\", \"role\": \"fin_hx_reader\", \"database\": \"vendas_hx\"}\n",
			maskIt: []string{"jsilva_hx", "vendas_hx"}},
	})
}

// A command in view above the table (pasted terminal): the listing of any type types the identity
// column (dags -> dag_id) and the header belongs to the tool; SQL in the command does not.
func TestBenchCommandInView(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "airflow dags list", src: "$ airflow dags list\ndag_id                | owners  | is_paused\n======================+=========+==========\n" +
			"carga_sinistros_hx    | airflow | False\nfaturamento_hx_diario | airflow | True\n",
			keep: []string{"dag_id", "is_paused"}, maskIt: []string{"carga_sinistros_hx", "faturamento_hx_diario"}},
		{name: "gcloud projects list", src: "$ gcloud projects list\nPROJECT_ID          NAME          PROJECT_NUMBER\nseguradora-hx-prod  seguradorahx  123456789012\n",
			maskIt: []string{"seguradora-hx-prod"}},
		{name: "SQL no comando: cabeçalho é coluna", src: "$ psql -c \"select cod_sinistro_hx, vlr_pago_hx from apolice_hx\"\n cod_sinistro_hx | vlr_pago_hx\n-----------------+-------------\n 1               | 2\n",
			maskIt: []string{"cod_sinistro_hx"}},
		{name: "frase não é comando", src: "Then list the tables below\nname | rows\n-----+-----\nfoo  | 3\n", keep: []string{"foo"}},
		// only a listing: under a command that shows a file or runs a script, the header belongs to
		// the owner
		{name: "cat de CSV: cabeçalho é coluna", src: "$ cat apolices.csv\ncod_apolice_hx,vlr_premio_hx,nome_segurado\n1,2,3\n4,5,6\n7,8,9\n",
			maskIt: []string{"cod_apolice_hx", "vlr_premio_hx"}},
		{name: "script: cabeçalho é coluna", src: "$ python relatorio.py\ncod_apolice_hx   vlr_premio_hx   dt_emissao_hx\n1                2               3\n4                5               6\n7                8               9\n",
			maskIt: []string{"cod_apolice_hx", "vlr_premio_hx", "dt_emissao_hx"}},
	})
}

// Traceback: the line that points to a dependency or the standard library belongs to the vendor and
// stays; the one that points to the project belongs to the client (function, module, code and the
// value cited in the error).
func TestBenchTraceback(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "python", src: "Traceback (most recent call last):\n" +
			"  File \"/home/ubuntu/seguradora-hx/etl/carga_sinistros_hx.py\", line 42, in calcular_premio_hx\n" +
			"    total = apolice_hx.premio_liquido()\n" +
			"  File \"/opt/venv/lib/python3.11/site-packages/pandas/core/frame.py\", line 3761, in __getitem__\n" +
			"    indexer = self.columns.get_loc(key)\n" +
			"  File \"/usr/lib/python3.11/json/decoder.py\", line 337, in decode\n" +
			"    obj, end = self.raw_decode(s, idx=_w(s, 0).end())\n" +
			"KeyError: 'COD_APOLICE_HX'\n",
			// the pandas line stays (function and code); the "pandas/core" folders of the path
			// follow the usual path rule (pandas is a dictionary word: it does not stop the reader)
			keep:   []string{"__getitem__", "get_loc", "raw_decode", "json/decoder.py", "ubuntu"},
			maskIt: []string{"seguradora-hx", "carga_sinistros_hx", "calcular_premio_hx", "apolice_hx", "premio_liquido", "COD_APOLICE_HX"}},
		{name: "python: dependência que não é palavra de dicionário fica inteira", src: "Traceback (most recent call last):\n" +
			"  File \"/home/ubuntu/seguradora-hx/etl/carga_hx.py\", line 7, in gravar_sinistros_hx\n" +
			"    conn.execute(sql_hx)\n" +
			"  File \"/opt/venv/lib/python3.11/site-packages/sqlalchemy/engine/base.py\", line 1967, in _exec_single_context\n" +
			"    self.dialect.do_execute(cursor, str_statement, effective_parameters, context)\n" +
			"RuntimeError: falhou\n",
			keep:   []string{"site-packages/sqlalchemy/engine/base.py", "_exec_single_context", "do_execute"},
			maskIt: []string{"gravar_sinistros_hx", "sql_hx"}},
		{name: "pacote interno instalado no ambiente", src: "Traceback (most recent call last):\n" +
			"  File \"/opt/venv/lib/python3.11/site-packages/vendashx_core/precos.py\", line 9, in calcular_tarifa_hx\n" +
			"    return tabela_hx[cod]\n" +
			"KeyError: 'PLANO_OURO_HX'\n",
			maskIt: []string{"calcular_tarifa_hx", "tabela_hx", "PLANO_OURO_HX"}},
		{name: "pacote interno com nome de papel (common)", src: "Traceback (most recent call last):\n" +
			"  File \"/opt/venv/lib/python3.11/site-packages/common/precos_hx.py\", line 9, in calcular_tarifa_hx\n" +
			"    return tabela_hx[cod]\n" +
			"KeyError: 'PLANO_OURO_HX'\n",
			maskIt: []string{"precos_hx", "calcular_tarifa_hx", "tabela_hx", "PLANO_OURO_HX"}},
		{name: "projeto com pasta lib/python3", src: "Traceback (most recent call last):\n" +
			"  File \"/home/ubuntu/proj-hx/lib/python3/carga_hx.py\", line 3, in somar_premio_hx\n" +
			"    return premio_hx + taxa_hx\n" +
			"NameError: name 'taxa_hx' is not defined\n",
			maskIt: []string{"somar_premio_hx", "premio_hx", "carga_hx"}},
		{name: "java", src: "java.lang.IllegalStateException: falhou\n" +
			"\tat br.com.seguradorahx.sinistros.CargaSinistros.calcularPremio(CargaSinistros.java:42)\n" +
			"\tat org.springframework.boot.SpringApplication.run(SpringApplication.java:315)\n",
			keep: []string{"org.springframework.boot.SpringApplication.run"}, maskIt: []string{"seguradorahx", "CargaSinistros", "calcularPremio"}},
		{name: "javascript", src: "TypeError: x is undefined\n" +
			"    at calcularTarifaHx (/srv/app-hx/src/tarifas.js:12:7)\n" +
			"    at Layer.handle (/srv/app-hx/node_modules/express/lib/router/layer.js:95:5)\n" +
			"    at Function.map (/srv/app-hx/node_modules/lodash/lodash.js:9590:14)\n",
			keep: []string{"Layer.handle", "Function.map", "node_modules/lodash/lodash.js"}, maskIt: []string{"calcularTarifaHx"}},
		{name: "go", src: "panic: runtime error: index out of range\n\ngoroutine 1 [running]:\n" +
			"main.calcularPremioHx(0x1)\n\t/home/ubuntu/seguradora-hx/cmd/premio.go:42 +0x1d\n" +
			"runtime.goexit()\n\t/usr/local/go/src/runtime/asm_amd64.s:1650 +0x1\n",
			keep: []string{"runtime.goexit"}, maskIt: []string{"calcularPremioHx", "seguradora-hx"}},
		{name: "go: a pasta internal do projeto é do cliente", src: "panic: boom\n\ngoroutine 1 [running]:\n" +
			"github.com/seguradorahx/faturahx/internal/tarifas.calcularTarifaHx(0x1)\n\t/home/ubuntu/faturahx/internal/tarifas/calc.go:7 +0x1d\n",
			keep: []string{"github.com", "internal"}, maskIt: []string{"calcularTarifaHx", "seguradorahx", "faturahx/internal", "tarifas"}},
		{name: "go: receptor e biblioteca padrão", src: "goroutine 7 [running]:\n" +
			"github.com/seguradorahx/faturahx/servico.(*CargaSinistrosHx).Calcular(0x1)\n\t/srv/build/servico/carga.go:7 +0x1d\n" +
			"net/http.(*conn).serve(0xc0001)\n\t/usr/local/go/src/net/http/server.go:2009 +0x5f4\n",
			keep: []string{"net/http.(*conn).serve"}, maskIt: []string{"CargaSinistrosHx", "seguradorahx"}},
		// in the module cache, the client's private module belongs to the client; the public one
		// (vendor owner and popular repository) belongs to the vendor
		{name: "go: módulo privado no cache", src: "goroutine 1 [running]:\n" +
			"github.com/seguradora-hx/lib-tarifas-hx/calc.CalcularPremioHx(0x1)\n\t/home/ubuntu/go/pkg/mod/github.com/seguradora-hx/lib-tarifas-hx@v1.2.0/calc/calc.go:12 +0x1d\n",
			maskIt: []string{"seguradora-hx", "lib-tarifas-hx", "CalcularPremioHx"}},
		{name: "go: módulo público no cache", src: "goroutine 1 [running]:\n" +
			"github.com/stretchr/testify/assert.ObjectsAreEqual(0x1)\n\t/home/ubuntu/go/pkg/mod/github.com/stretchr/testify@v1.8.4/assert/assertions.go:65 +0x1d\n" +
			"golang.org/x/sync/errgroup.(*Group).Go.func1()\n\t/home/ubuntu/go/pkg/mod/golang.org/x/sync@v0.6.0/errgroup/errgroup.go:78 +0x56\n",
			// ("testify@v1.8.4" in the path goes out as an e-mail: that is the e-mail detector's,
			// not this one's)
			keep: []string{"github.com/stretchr/testify/assert.ObjectsAreEqual", "assert/assertions.go", "errgroup.(*Group).Go"}},
		{name: "maven: artefato privado no repositório local", src: "lendo /home/ubuntu/.m2/repository/br/com/seguradora-hx/tarifas-hx/1.0/pom.xml\n",
			maskIt: []string{"seguradora-hx", "tarifas-hx"}},
		{name: "python: pasta lib/python_x do projeto é do cliente", src: "Traceback (most recent call last):\n" +
			"  File \"/home/ubuntu/faturahx/lib/python_etl/carga_hx.py\", line 3, in somar_premio_hx\n" +
			"    return premio_hx + taxa_hx\n" +
			"NameError: name 'taxa_hx' is not defined\n",
			maskIt: []string{"somar_premio_hx", "premio_hx", "taxa_hx", "python_etl"}},
	})
}

// The prose and code brakes of the SQL reader cannot cut real SQL: a table alias that is an English
// article (an, the, this), SELECT glued to the parenthesis and the SET list with the qualified
// column at the start of the line. The query builder stays out.
func TestBenchSQLTheBrakesDoNotCut(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "apelido an", src: "SELECT an.cod_apolice_hx, an.vlr_hx FROM apolices_hx an WHERE an.dt_emissao_hx > 1 AND an.cd_filial_hx = 2;\n",
			maskIt: []string{"cod_apolice_hx", "vlr_hx", "apolices_hx", "dt_emissao_hx", "cd_filial_hx"}},
		{name: "apelido an em minúsculas", src: "select an.cod_apolice_hx from apolices_hx an where an.dt_emissao_hx > 1 and an.cd_filial_hx = 2;\n",
			maskIt: []string{"cod_apolice_hx", "apolices_hx", "dt_emissao_hx", "cd_filial_hx"}},
		{name: "apelidos the e this", src: "SELECT this.cod_apolice_hx FROM apolices_hx this JOIN sinistros_hx the ON the.cod_sin_hx = this.cod_apolice_hx WHERE the.vlr_pago_hx > 0;\n",
			maskIt: []string{"cod_apolice_hx", "apolices_hx", "sinistros_hx", "cod_sin_hx", "vlr_pago_hx"}},
		{name: "SELECT colado ao parêntese", src: "SELECT(cod_apolice_hx) FROM apolices_hx WHERE dt_emissao_hx > 1;\n",
			maskIt: []string{"cod_apolice_hx", "apolices_hx", "dt_emissao_hx"}},
		{name: "SET com coluna qualificada por linha", src: "UPDATE vendashx.dbo.apolices_hx\nSET\n  vendashx.dbo.apolices_hx.status_hx = 'x',\n" +
			"  vendashx.dbo.apolices_hx.vlr_premio_hx = 2\nWHERE cod_apolice_hx = 1;\n",
			maskIt: []string{"vendashx", "apolices_hx", "status_hx", "vlr_premio_hx", "cod_apolice_hx"}},
		{name: "construtor de consultas não é SQL", src: "stmt = select(Usuario).where(Usuario.ativo == True)\nresultado = sessao.execute(stmt)\n",
			keep: []string{"select(Usuario).where(Usuario.ativo"}},
		{name: "prosa com artigo não é SQL", src: "Select the policy you want to remove from the list below.\n",
			keep: []string{"policy", "list"}},
	})
}

// Group in a reversed domain: io.<company> and dev.<company> belong to the company (the .io or .dev
// domain is its own), like com.<company>; only the known public group stays.
func TestBenchCompanyGroup(t *testing.T) {
	checkBench(t, []benchCase{
		{name: "namespace io da empresa", src: "{\"type\": \"record\", \"namespace\": \"io.empresahx.pagamentos\", \"name\": \"PedidoHx\"}\n",
			maskIt: []string{"io.empresahx.pagamentos"}},
		{name: "namespace dev da empresa", src: "{\"type\": \"record\", \"namespace\": \"dev.empresahx.pagamentos\", \"name\": \"PedidoHx\"}\n",
			maskIt: []string{"dev.empresahx.pagamentos"}},
		{name: "grupo público", src: "{\"type\": \"record\", \"namespace\": \"org.apache.kafka.connect\", \"name\": \"Envelope\"}\n",
			keep: []string{"org.apache.kafka.connect"}},
		{name: "maven: grupo io da empresa no repositório local", src: "lendo /home/ubuntu/.m2/repository/io/empresa-hx/tarifas-hx/1.0/pom.xml\n",
			maskIt: []string{"empresa-hx", "tarifas-hx"}},
	})
}
