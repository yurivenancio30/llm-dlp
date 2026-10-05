package mask

import (
	"strings"
	"testing"
)

// Leitores de código, linha de comando, caminhos e URLs de nuvem (itens 9 a 14 da revisão).
// Nomes fictícios.

func TestLeitorCodigoNome(t *testing.T) {
	m := novoTeste(t)
	pos := []struct{ s, nome string }{
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
		confere(t, m, c.s, []string{c.nome}, nil)
	}
	// variável, função e classe comuns ficam; texto que não é nome de recurso fica
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
		if out, ents := m.Mascarar(s); len(ents) > 0 {
			t.Errorf("código comum mascarado: %q -> %q", s, out)
		}
	}
}

func TestLeitorCodigoChamada(t *testing.T) {
	m := novoTeste(t)
	for _, c := range []struct{ s, nome string }{
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
		confere(t, m, c.s, []string{c.nome}, nil)
	}
	for _, s := range []string{
		`os.Getenv("NAMESPACE")`,
		`fmt.Println("tabela vazia")`,
		`db.Query("pedido-x")`,
		`logger.getLogger("app.queue")`,
		`Paths.get("dados")`,
	} {
		if out, ents := m.Mascarar(s); len(ents) > 0 {
			t.Errorf("chamada comum mascarada: %q -> %q", s, out)
		}
	}
}

func TestLeitorCLI(t *testing.T) {
	m := novoTeste(t)
	confere(t, m, `psql -h pg-l1 -d vendas_l1 -U svc_l1 -c "select 1"`, []string{"pg-l1", "vendas_l1", "svc_l1"}, []string{"psql -h ", " -d ", " -U "})
	confere(t, m, `kubectl -n ns-l2 rollout restart deploy/api-l2`, []string{"ns-l2", "api-l2"}, []string{"kubectl -n ", " deploy/"})
	confere(t, m, `kubectl logs -n ns-l3 svc/cobranca-l3`, []string{"ns-l3", "cobranca-l3"}, nil)
	confere(t, m, `scp relatorio.csv backup-l4:/srv/x/`, []string{"backup-l4"}, []string{"scp relatorio.csv "})
	for _, s := range []string{
		"docker run -d nginx",
		"curl -d name=x https://example.com",
		"ls -h /tmp",
		"grep -n padrao arquivo.txt",
		"tar -czf a.tgz -C /tmp dados",
	} {
		if out, ents := m.Mascarar(s); len(ents) > 0 {
			t.Errorf("comando comum mascarado: %q -> %q", s, out)
		}
	}
}

func TestLeitorCaminhosFora(t *testing.T) {
	m := novoTeste(t)
	confere(t, m, "cp /srv/relatorios_p1/entrada/a.csv /opt/cobranca/conf/app.yml", []string{"relatorios_p1", "entrada", "cobranca"}, []string{"/srv/", "/a.csv", "/opt/", "/conf/app.yml"})
	confere(t, m, "tail -f /var/log/app_p2/erro.log", []string{"app_p2"}, []string{"/var/log/", "erro.log"})
	confere(t, m, `abrir D:\Relatorios_p3\mensal\x.xlsx`, []string{"Relatorios_p3", "mensal"}, []string{"D:\\", "x.xlsx"})
	confere(t, m, `\\srv-arquivos-p4\financeiro$\fechamento\a.pdf`, []string{"srv-arquivos-p4", "financeiro", "fechamento"}, nil)
	for _, s := range []string{
		"/var/lib/postgresql/data e /etc/nginx/nginx.conf",
		"/opt/homebrew/bin e /usr/local/bin",
		"https://site.example.com/srv/docs/x",
		`C:\Windows\System32\drivers`,
	} {
		if out, ents := m.Mascarar(s); len(ents) > 0 {
			t.Errorf("caminho público mascarado: %q -> %q", s, out)
		}
	}
}

func TestLeitorURLsNuvem(t *testing.T) {
	m := novoTeste(t)
	confere(t, m, "https://stcontasx1.blob.core.windows.net/bkt-n1/entrada/a.csv", []string{"stcontasx1", "bkt-n1"}, []string{".blob.core.windows.net/", "a.csv"})
	confere(t, m, "https://stcontasx2.queue.core.windows.net/fila-n2", []string{"stcontasx2", "fila-n2"}, nil)
	confere(t, m, "Endpoint=sb://sb-n3.servicebus.windows.net/;SharedAccessKeyName=x;EntityPath=fila-n3", []string{"sb-n3", "fila-n3"}, []string{".servicebus.windows.net/"})
	confere(t, m, "https://sqs.sa-east-1.amazonaws.com/210987654321/fila-n4", []string{"210987654321", "fila-n4"}, []string{"sqs.sa-east-1.amazonaws.com/"})
	if got := tipoDe(m, "/subscriptions/1a2b3c4d-1111-2222-3333-abcdefabcdef/resourceGroups/rg-n5/providers/Microsoft.Sql/servers/sqlsrv-n5/databases/vendas_n5", "sqlsrv-n5"); got != "obj.servidor" {
		t.Errorf("Azure: servidor tipo %q", got)
	}
	if got := tipoDe(m, "/subscriptions/1a2b3c4d-1111-2222-3333-abcdefabcdef/resourceGroups/rg-n5/providers/Microsoft.Sql/servers/sqlsrv-n5/databases/vendas_n5", "vendas_n5"); got != "obj.database" {
		t.Errorf("Azure: banco tipo %q", got)
	}
	for _, s := range []string{"https://docs.aws.amazon.com/sqs/", "https://learn.microsoft.com/azure/storage/"} {
		if out, ents := m.Mascarar(s); len(ents) > 0 {
			t.Errorf("documentação pública mascarada: %q -> %q", s, out)
		}
	}
}

// Escape de texto ("\nPALAVRA") não é DOMINIO\usuario; e valor de duas palavras com hífen,
// sem dígito, é mascarado no lugar mas não ensina sozinho.
func TestFreiosCodigoEUsuarioRede(t *testing.T) {
	m := novoTeste(t)
	if out, ents := m.Mascarar(`fmt.Printf("TEXTO\nPCALIGN %d")`); len(ents) > 0 {
		t.Errorf("escape virou usuário de rede: %q", out)
	}
	confere(t, m, `await ch.assertQueue("payments-queue")`, []string{"payments-queue"}, nil)
	if out, _ := m.Mascarar("a payments-queue encheu"); !strings.Contains(out, "payments-queue") {
		t.Errorf("valor sem dígito/_/. ensinou sozinho: %q", out)
	}
	confere(t, m, `await ch.assertQueue("payments-queue-01")`, []string{"payments-queue-01"}, nil)
	if out, _ := m.Mascarar("a payments-queue-01 encheu"); strings.Contains(out, "payments-queue-01") {
		t.Errorf("valor com dígito deveria ensinar: %q", out)
	}
}
