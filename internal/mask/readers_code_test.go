package mask

import (
	"strings"
	"testing"
	"time"
)

// Code, command-line, path and cloud URL readers. Made-up names.

func TestReaderCodeName(t *testing.T) {
	m := newTest(t)
	pos := []struct{ s, name string }{
		{`DB_HOST = "pg-x1"`, "pg-x1"},
		{`const dbHost = "pg-x2"`, "pg-x2"},
		{`#define DB_NAME "vendas_x3"`, "vendas_x3"},
		{`private static final String QUEUE_NAME = "fila-x4";`, "fila-x4"},
		{`val topicName = "topico-x5"`, "topico-x5"},
		{`QUEUE: str = "fila-x6"`, "fila-x6"},
		{`connect(host="pg-x7", dbname="vendas_x7")`, "vendas_x7"},
		{`client = Client(bucket: "bkt-x8")`, "bkt-x8"},
		{`$config = ['queue' => 'fila-x9'];`, "fila-x9"},
		{`opts := Options{Namespace: "ns-x10"}`, "ns-x10"},
		{`config["Kafka:Topic"] = "topico-x11";`, "topico-x11"},
		{`@Table(name = "tb_x12", schema = "sch_x12")`, "tb_x12"},
		{`@Table(name = "tb_x12", schema = "sch_x12")`, "sch_x12"},
		{`@KafkaListener(topics = "topico-x13", groupId = "grp-x13")`, "topico-x13"},
		{`@KafkaListener(topics = "topico-x13", groupId = "grp-x13")`, "grp-x13"},
		{`[Table("tb_x14", Schema = "sch_x14")]`, "tb_x14"},
		{`props.setProperty("user", "svc_x15");`, "svc_x15"},
		{`bucket = os.getenv("BUCKET", "bkt-x16")`, "bkt-x16"},
		{`const h = process.env.DB_HOST || 'pg-x17';`, "pg-x17"},
		{`ns = ENV.fetch('NAMESPACE', 'ns-x18')`, "ns-x18"},
		{`in := &s3.PutObjectInput{Bucket: aws.String("bkt-x19")}`, "bkt-x19"},
		{`define('DB_HOST', 'pg-x20');`, "pg-x20"},
	}
	for _, c := range pos {
		check(t, m, c.s, []string{c.name}, nil)
	}
	// common variables, functions and classes stay; text that is not a resource name stays
	for _, s := range []string{
		`pedidoId := calcularTotal(itens)`,
		`class PedidoService { String status = "ativo"; }`,
		`host = "localhost"; user = "root"; db = "test"`,
		`log.info("queue", "processando")`,
		`msg = "Erro ao conectar no host"`,
		`if host == "pg-x99" { }`,
		`query = "SELECT 1"`,
		`dbPort = "5432"; hostTimeout = "30s"; userAgent = "curl/8"`,
		`label = "Nome da fila"`,
	} {
		if out, ents := m.Mask(s); len(ents) > 0 {
			t.Errorf("common code masked: %q -> %q", s, out)
		}
	}
}

func TestReaderCodeCall(t *testing.T) {
	m := newTest(t)
	for _, c := range []struct{ s, name string }{
		{`await channel.assertQueue('fila-c1', { durable: true });`, "fila-c1"},
		{`var q = new QueueClient(conn, "fila-c2");`, "fila-c2"},
		{`sqs.getQueueUrl("fila-c3")`, "fila-c3"},
		{`s3.createBucket("bkt-c4")`, "bkt-c4"},
		{`PutObjectRequest.builder().bucket("bkt-c5").build()`, "bkt-c5"},
		{`client.pods().inNamespace("ns-c6").list()`, "ns-c6"},
		{`consumer.subscribe(["topico-c7", "topico-c8"])`, "topico-c8"},
		{`rd_kafka_topic_new(rk, "topico-c9", NULL)`, "topico-c9"},
		{`new BlobContainerClient(conn, "bkt-c10")`, "bkt-c10"},
		{`db.Collection("tb_c11")`, "tb_c11"},
	} {
		check(t, m, c.s, []string{c.name}, nil)
	}
	for _, s := range []string{
		`os.Getenv("NAMESPACE")`,
		`fmt.Println("tabela vazia")`,
		`db.Query("pedido-x")`,
		`logger.getLogger("app.queue")`,
		`Paths.get("dados")`,
	} {
		if out, ents := m.Mask(s); len(ents) > 0 {
			t.Errorf("common call masked: %q -> %q", s, out)
		}
	}
}

func TestReaderCLI(t *testing.T) {
	m := newTest(t)
	check(t, m, `psql -h pg-l1 -d vendas_l1 -U svc_l1 -c "select 1"`, []string{"pg-l1", "vendas_l1", "svc_l1"}, []string{"psql -h ", " -d ", " -U "})
	check(t, m, `kubectl -n ns-l2 rollout restart deploy/api-l2`, []string{"ns-l2", "api-l2"}, []string{"kubectl -n ", " deploy/"})
	check(t, m, `kubectl logs -n ns-l3 svc/cobranca-l3`, []string{"ns-l3", "cobranca-l3"}, nil)
	check(t, m, `scp relatorio.csv backup-l4:/srv/x/`, []string{"backup-l4"}, []string{"scp relatorio.csv "})
	for _, s := range []string{
		"docker run -d nginx",
		"curl -d name=x https://example.com",
		"ls -h /tmp",
		"grep -n padrao arquivo.txt",
		"tar -czf a.tgz -C /tmp dados",
	} {
		if out, ents := m.Mask(s); len(ents) > 0 {
			t.Errorf("common command masked: %q -> %q", s, out)
		}
	}
}

func TestReaderPathsOutside(t *testing.T) {
	m := newTest(t)
	check(t, m, "cp /srv/relatorios_p1/entrada/a.csv /opt/cobranca/conf/app.yml", []string{"relatorios_p1", "entrada", "cobranca"}, []string{"/srv/", "/a.csv", "/opt/", "/conf/app.yml"})
	check(t, m, "tail -f /var/log/app_p2/erro.log", []string{"app_p2"}, []string{"/var/log/", "erro.log"})
	check(t, m, `abrir D:\Relatorios_p3\mensal\x.xlsx`, []string{"Relatorios_p3", "mensal"}, []string{"D:\\", "x.xlsx"})
	check(t, m, `\\srv-arquivos-p4\financeiro$\fechamento\a.pdf`, []string{"srv-arquivos-p4", "financeiro", "fechamento"}, nil)
	for _, s := range []string{
		"/var/lib/postgresql/data e /etc/nginx/nginx.conf",
		"/opt/homebrew/bin e /usr/local/bin",
		"https://site.example.com/srv/docs/x",
		`C:\Windows\System32\drivers`,
	} {
		if out, ents := m.Mask(s); len(ents) > 0 {
			t.Errorf("public path masked: %q -> %q", s, out)
		}
	}
}

func TestReaderCloudURLs(t *testing.T) {
	m := newTest(t)
	check(t, m, "https://stcontasx1.blob.core.windows.net/bkt-n1/entrada/a.csv", []string{"stcontasx1", "bkt-n1"}, []string{".blob.core.windows.net/", "a.csv"})
	check(t, m, "https://stcontasx2.queue.core.windows.net/fila-n2", []string{"stcontasx2", "fila-n2"}, nil)
	check(t, m, "Endpoint=sb://sb-n3.servicebus.windows.net/;SharedAccessKeyName=x;EntityPath=fila-n3", []string{"sb-n3", "fila-n3"}, []string{".servicebus.windows.net/"})
	check(t, m, "https://sqs.sa-east-1.amazonaws.com/210987654321/fila-n4", []string{"210987654321", "fila-n4"}, []string{"sqs.sa-east-1.amazonaws.com/"})
	if got := typeOf(m, "/subscriptions/1a2b3c4d-1111-2222-3333-abcdefabcdef/resourceGroups/rg-n5/providers/Microsoft.Sql/servers/sqlsrv-n5/databases/vendas_n5", "sqlsrv-n5"); got != "obj.server" {
		t.Errorf("Azure: server type %q", got)
	}
	if got := typeOf(m, "/subscriptions/1a2b3c4d-1111-2222-3333-abcdefabcdef/resourceGroups/rg-n5/providers/Microsoft.Sql/servers/sqlsrv-n5/databases/vendas_n5", "vendas_n5"); got != "obj.database" {
		t.Errorf("Azure: database type %q", got)
	}
	for _, s := range []string{"https://docs.aws.amazon.com/sqs/", "https://learn.microsoft.com/azure/storage/"} {
		if out, ents := m.Mask(s); len(ents) > 0 {
			t.Errorf("public documentation masked: %q -> %q", s, out)
		}
	}
}

// A text escape ("\nPALAVRA") is not DOMAIN\user; and a two-word hyphenated value, without a digit,
// is masked in place but does not teach alone.
func TestBrakesCodeAndNetworkUser(t *testing.T) {
	m := newTest(t)
	if out, ents := m.Mask(`fmt.Printf("TEXTO\nPCALIGN %d")`); len(ents) > 0 {
		t.Errorf("escape became a network user: %q", out)
	}
	check(t, m, `await ch.assertQueue("payments-queue")`, []string{"payments-queue"}, nil)
	if out, _ := m.Mask("a payments-queue encheu"); !strings.Contains(out, "payments-queue") {
		t.Errorf("value without digit/_/. taught by itself: %q", out)
	}
	check(t, m, `await ch.assertQueue("payments-queue-01")`, []string{"payments-queue-01"}, nil)
	if out, _ := m.Mask("a payments-queue-01 encheu"); strings.Contains(out, "payments-queue-01") {
		t.Errorf("value with a digit should teach: %q", out)
	}
}

// Ajustes finais 2a-2d.
func TestFinalFormatAdjustments(t *testing.T) {
	m := newTest(t)
	check(t, m, "spring.datasource.url=${erp.url:http://erpprd01.acme.local:8080}", []string{"erpprd01"}, []string{"${erp.url:http://", ":8080}"})
	check(t, m, "ERP_URL=${ERP_URL:-http://erpprd02.acme.local:8080}", []string{"erpprd02"}, []string{"${ERP_URL:-http://"})
	check(t, m, "host: ${DB_HOST:pgprd03}", []string{"pgprd03"}, []string{"${DB_HOST:"})
	check(t, m, `consumer.subscribe(List.of("pedidos-criados", "pedidos-pagos"));`, []string{"pedidos-criados", "pedidos-pagos"}, []string{"List.of("})
	check(t, m, `consumer.subscribe(Arrays.asList("pedidos-x4"));`, []string{"pedidos-x4"}, nil)
	check(t, m, `$redis->connect('cacheprd05', 6379);`, []string{"cacheprd05"}, []string{", 6379"})
	check(t, m, `r = redis.Redis("cacheprd06", 6379)`, []string{"cacheprd06"}, nil)
	check(t, m, "cp /dados/Relatorios_x7/mensal/a.csv /tmp/", []string{"Relatorios_x7", "mensal"}, []string{"/tmp/", "a.csv"})
	for _, s := range []string{
		`log.Printf("%s", 200)`,
		`fmt.Sprintf("total", 10)`,
		`fetch("/api/v1/pedidos")`,
		"veja /usr/local/bin e /etc/hosts",
		"GET /static/css/app.css",
		`${HOME}/x e ${PATH}`,
	} {
		if out, ents := m.Mask(s); len(ents) > 0 {
			t.Errorf("masked needlessly: %q -> %q", s, out)
		}
	}
}

// A long single line (minified JSON, API response) grows linearly. Quadratic would give ~16x from
// 128 KB to 512 KB; the limit is 8x.
func TestLongLineLinear(t *testing.T) {
	if testing.Short() {
		t.Skip("measures time")
	}
	m := newTest(t)
	elapsed := func(s string) time.Duration {
		best := time.Duration(1 << 62)
		for k := 0; k < 2; k++ {
			ini := time.Now()
			m.Detect(s)
			best = min(best, time.Since(ini))
		}
		return best
	}
	for _, typ := range []string{"url", "sqljson", "urn", "email", "k8sdns", "conexao", "urlcolada", "esquemas"} {
		p, g := elapsed(longLine(typ, 128<<10)), elapsed(longLine(typ, 512<<10))
		if r := float64(g) / float64(p); r > 8 {
			t.Errorf("%s: 512 KB took %.1fx the time of 128 KB (%v / %v)", typ, r, g, p)
		}
	}
	// the window does not change the result: git config with the section above the url and git
	// clone at the end of a long line
	cfg := "[remote \"origin\"]\n\turl = https://git.ficticia.local/fin-x1/repo-x1\n"
	check(t, m, cfg, []string{"fin-x1", "repo-x1"}, []string{"[remote \"origin\"]"})
	longOne := strings.Repeat("x ", 3000) + "git clone https://git.ficticia.local/fin-x2/repo-x2"
	check(t, m, longOne, []string{"repo-x2"}, nil)
}

// Uppercase SELECT with one clause teaches, as in lowercase.
func TestUppercaseSQLOneClauseTeaches(t *testing.T) {
	m := newTest(t)
	check(t, m, "SELECT * FROM fin.tb_nota_fiscal", []string{"tb_nota_fiscal"}, []string{"SELECT * FROM "})
	if out, _ := m.Mask("a tb_nota_fiscal cresceu"); strings.Contains(out, "tb_nota_fiscal") {
		t.Errorf("uppercase with one clause did not teach: %q", out)
	}
}

// Dotted topic/queue coming from a queue/topic key; code attribute, domain and file stay.
func TestDottedTopic(t *testing.T) {
	m := newTest(t)
	check(t, m, "KAFKA_TOPIC=fin.notas.emitidas", []string{"fin.notas.emitidas"}, []string{"KAFKA_TOPIC="})
	if out, _ := m.Mask("a fin.notas.emitidas parou"); strings.Contains(out, "fin.notas.emitidas") {
		t.Errorf("dotted topic did not teach: %q", out)
	}
	check(t, m, "kafka.topic=fin.notas.pagas", []string{"fin.notas.pagas"}, nil)
	check(t, m, "  topic: fin.notas.canceladas", []string{"fin.notas.canceladas"}, nil)
	for _, s := range []string{"topic = cfg.topic", "TOPIC=settings.queue_name", "QUEUE_URL=api.example.com", "KAFKA_TOPIC=notas.json"} {
		if out, ents := m.Mask(s); len(ents) > 0 {
			t.Errorf("masked needlessly: %q -> %q", s, out)
		}
	}
}

// Positional user after the PDO DSN and name in Terraform's metadata block.
func TestPDOUserAndTerraformMetadata(t *testing.T) {
	m := newTest(t)
	u := "svc" + "_pdo_x1"
	check(t, m, "$pdo = new PDO('mysql:host=db-x1;dbname=vendas_x1', '"+u+"', $senha);", []string{"db-x1", "vendas_x1", u}, []string{"', $senha);"})
	// without a literal DSN before it, the argument has no owner; "root" is public
	if out, ents := m.Mask("$pdo = new PDO($dsn, 'svc_pdo_x2', $senha);"); len(ents) > 0 {
		t.Errorf("string without a DSN before it masked: %q", out)
	}
	check(t, m, "new PDO('mysql:host=db-x3;dbname=vendas_x3', 'root', '')", []string{"db-x3", "vendas_x3"}, []string{"'root'"})
	tf := "resource \"kubernetes_deployment\" \"d\" {\n  metadata {\n    name = \"api-tf-x5\"\n    namespace = \"ns-tf-x5\"\n  }\n" +
		"  spec {\n    template {\n      spec {\n        container {\n          name = \"app\"\n        }\n      }\n    }\n  }\n}\n"
	check(t, m, tf, []string{"api-tf-x5", "ns-tf-x5"}, []string{"name = \"app\""})
}
