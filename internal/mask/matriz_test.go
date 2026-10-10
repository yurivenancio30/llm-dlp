package mask

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Matriz de cobertura: o mesmo conjunto de nomes internos fictícios escrito em várias
// linguagens e formatos, nas formas constante, parâmetro nomeado, anotação, chamada de função e
// linha de comando, mais AWS, GCP e Azure. Roda SEM nada no config (sem domínio interno, sem
// termos). Meta: pelo menos 90% das ocorrências mascaradas.

// os nomes (todos fictícios)
var matrizNomes = []string{
	"pgsrv-vendas-01",       // servidor
	"vendas_prd",            // banco
	"tb_pedidos_x9",         // tabela
	"svc_relatorio_x9",      // usuário
	"bkt-relatorios-x9",     // bucket
	"fila-pedidos-x9",       // fila
	"topico-eventos-x9",     // tópico
	"ns-pagamentos-x9",      // namespace
	"svc-cobranca-x9",       // serviço
	"relatorios_x9",         // pasta (/srv/relatorios_x9)
	"api-x9.vendas.interno", // domínio interno
	// ajustes finais: caixa de servidor, placeholder, lista aninhada, host+porta, caminho fora da lista
	"sqlprd01", "SQLPRD01", "SqlPrd01", "erpprd01", "pedidos-criados", "pedidos-pagos", "cacheprd01", "Relatorios_x9",
	// nuvem
	"proj-vendas-x9", "rg-vendas-x9", "stvendasx9", "sb-vendas-x9", "123456789012",
	// revisão 68d44da: tópico com ponto
	"fin.notas.emitidas",
}

var matrizCasos = []struct{ nome, texto string }{
	{"Python", `DB_HOST = "pgsrv-vendas-01"
DATABASE = "vendas_prd"
conn = psycopg2.connect(host="pgsrv-vendas-01", dbname="vendas_prd", user="svc_relatorio_x9")
cur.execute("SELECT * FROM tb_pedidos_x9 WHERE id = %s", (x,))
s3.upload_file(caminho, Bucket="bkt-relatorios-x9", Key="a.csv")
fila = sqs.get_queue_url(QueueName="fila-pedidos-x9")
consumer.subscribe(["topico-eventos-x9"])
NAMESPACE = os.getenv("K8S_NAMESPACE", "ns-pagamentos-x9")
SERVICE = os.environ.get("SERVICE_NAME", "svc-cobranca-x9")
OUTPUT_DIR = "/srv/relatorios_x9/saida"
API = "https://api-x9.vendas.interno/v1"

@app.task(queue="fila-pedidos-x9")
def processar(): ...
`},
	{"Java", `private static final String DB_HOST = "pgsrv-vendas-01";
@Table(name = "tb_pedidos_x9", schema = "vendas_prd")
public class Pedido {}
@KafkaListener(topics = "topico-eventos-x9", groupId = "svc-cobranca-x9")
public void ouvir(String m) {}
String url = "jdbc:postgresql://pgsrv-vendas-01:5432/vendas_prd";
props.setProperty("user", "svc_relatorio_x9");
PutObjectRequest req = PutObjectRequest.builder().bucket("bkt-relatorios-x9").key(k).build();
String q = sqs.getQueueUrl("fila-pedidos-x9").getQueueUrl();
client.pods().inNamespace("ns-pagamentos-x9").list();
Path p = Paths.get("/srv/relatorios_x9/entrada");
URI api = URI.create("https://api-x9.vendas.interno/v1");
`},
	{"C#", `const string DbServer = "pgsrv-vendas-01";
[Table("tb_pedidos_x9", Schema = "vendas_prd")]
public class Pedido {}
var conexao = "Server=pgsrv-vendas-01;Database=vendas_prd;User Id=svc_relatorio_x9;";
var fila = new QueueClient(connStr, "fila-pedidos-x9");
var container = new BlobContainerClient(conn, "bkt-relatorios-x9");
config["Kafka:Topic"] = "topico-eventos-x9";
var opcoes = new Opcoes { Namespace = "ns-pagamentos-x9", ServiceName = "svc-cobranca-x9" };
var arquivos = Directory.GetFiles(@"D:\relatorios_x9\entrada");
var api = new Uri("https://api-x9.vendas.interno/v1");
`},
	{"Go", `const dbHost = "pgsrv-vendas-01"
db, _ := sql.Open("mysql", "svc_relatorio_x9:pw@tcp(pgsrv-vendas-01:3306)/vendas_prd")
rows, _ := db.Query("SELECT id FROM tb_pedidos_x9 WHERE a = ?", x)
_, err = s3c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("bkt-relatorios-x9")})
r := kafka.NewReader(kafka.ReaderConfig{Topic: "topico-eventos-x9", GroupID: "svc-cobranca-x9"})
queueURL := "https://sqs.us-east-1.amazonaws.com/123456789012/fila-pedidos-x9"
namespace := "ns-pagamentos-x9"
dir := "/srv/relatorios_x9"
api := "https://api-x9.vendas.interno/v1"
`},
	{"Node/TS", `const DB_HOST = process.env.DB_HOST || 'pgsrv-vendas-01';
await channel.assertQueue('fila-pedidos-x9', { durable: true });
const pool = new Pool({ host: 'pgsrv-vendas-01', database: 'vendas_prd', user: 'svc_relatorio_x9' });
await pool.query('SELECT * FROM tb_pedidos_x9 WHERE id = $1', [id]);
await consumer.subscribe({ topic: 'topico-eventos-x9' });
await s3.send(new PutObjectCommand({ Bucket: 'bkt-relatorios-x9', Key: 'x' }));
const cfg = { namespace: 'ns-pagamentos-x9', service: 'svc-cobranca-x9' };
const arquivos = fs.readdirSync('/srv/relatorios_x9/entrada');
const r = await fetch('https://api-x9.vendas.interno/v1');
`},
	{"PHP", `define('DB_HOST', 'pgsrv-vendas-01');
$pdo = new PDO('mysql:host=pgsrv-vendas-01;dbname=vendas_prd', 'svc_relatorio_x9', $senha);
$st = $pdo->query("SELECT * FROM tb_pedidos_x9");
$config = ['queue' => 'fila-pedidos-x9', 'bucket' => 'bkt-relatorios-x9', 'topic' => 'topico-eventos-x9'];
$k8s = ['namespace' => 'ns-pagamentos-x9', 'service' => 'svc-cobranca-x9'];
$arquivos = scandir('/srv/relatorios_x9/entrada');
$api = 'https://api-x9.vendas.interno/v1';
`},
	{"Ruby", `DB_HOST = 'pgsrv-vendas-01'
ActiveRecord::Base.establish_connection(adapter: 'postgresql', host: 'pgsrv-vendas-01', database: 'vendas_prd', username: 'svc_relatorio_x9')
self.table_name = 'tb_pedidos_x9'
s3.put_object(bucket: 'bkt-relatorios-x9', key: 'x')
sqs.send_message(queue_url: 'https://sqs.us-east-1.amazonaws.com/123456789012/fila-pedidos-x9', message_body: 'x')
kafka.deliver_message('x', topic: 'topico-eventos-x9')
namespace = ENV.fetch('NAMESPACE', 'ns-pagamentos-x9')
service = ENV.fetch('SERVICE', 'svc-cobranca-x9')
arquivos = Dir.glob('/srv/relatorios_x9/*')
API = 'https://api-x9.vendas.interno/v1'
`},
	{"C/C++", `#define DB_HOST "pgsrv-vendas-01"
#define DB_NAME "vendas_prd"
static const char *QUEUE_NAME = "fila-pedidos-x9";
PGconn *c = PQconnectdb("host=pgsrv-vendas-01 dbname=vendas_prd user=svc_relatorio_x9");
const char *bucket = "bkt-relatorios-x9";
rd_kafka_topic_t *t = rd_kafka_topic_new(rk, "topico-eventos-x9", NULL);
const char *ns = getenv_or("NAMESPACE", "ns-pagamentos-x9");
FILE *f = fopen("/srv/relatorios_x9/entrada.csv", "r");
const char *API = "https://api-x9.vendas.interno/v1";
`},
	{"shell", `psql -h pgsrv-vendas-01 -d vendas_prd -U svc_relatorio_x9 -c "select count(*) from tb_pedidos_x9"
aws s3 cp relatorio.csv s3://bkt-relatorios-x9/2026/
kubectl -n ns-pagamentos-x9 rollout restart deploy/svc-cobranca-x9
aws sqs send-message --queue-url https://sqs.us-east-1.amazonaws.com/123456789012/fila-pedidos-x9 --message-body x
kafka-console-consumer --bootstrap-server pgsrv-vendas-01:9092 --topic topico-eventos-x9
rsync -av /srv/relatorios_x9/ svc_relatorio_x9@pgsrv-vendas-01:/srv/relatorios_x9/
curl https://api-x9.vendas.interno/v1/health
`},
	{"YAML", `apiVersion: apps/v1
kind: Deployment
metadata:
  name: svc-cobranca-x9
  namespace: ns-pagamentos-x9
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: app
          image: registry.vendas.interno/svc-cobranca-x9:1.2
          env:
            - name: DB_HOST
              value: pgsrv-vendas-01
            - name: DB_NAME
              value: vendas_prd
            - name: DB_USER
              value: svc_relatorio_x9
            - name: QUEUE_NAME
              value: fila-pedidos-x9
            - name: S3_BUCKET
              value: bkt-relatorios-x9
            - name: KAFKA_TOPIC
              value: topico-eventos-x9
            - name: API_URL
              value: https://api-x9.vendas.interno/v1
          volumeMounts:
            - name: dados
              mountPath: /srv/relatorios_x9
`},
	{"Terraform", `resource "aws_s3_bucket" "rel" {
  bucket = "bkt-relatorios-x9"
}
resource "aws_sqs_queue" "q" {
  name = "fila-pedidos-x9"
}
resource "aws_db_instance" "db" {
  identifier = "pgsrv-vendas-01"
  db_name    = "vendas_prd"
  username   = "svc_relatorio_x9"
}
resource "kubernetes_namespace" "ns" {
  metadata {
    name = "ns-pagamentos-x9"
  }
}
resource "kafka_topic" "t" {
  name = "topico-eventos-x9"
}
`},
	{".properties", `spring.datasource.url=jdbc:postgresql://pgsrv-vendas-01:5432/vendas_prd
spring.datasource.username=svc_relatorio_x9
app.queue.pedidos=fila-pedidos-x9
spring.kafka.template.default-topic=topico-eventos-x9
aws.s3.bucket=bkt-relatorios-x9
app.kubernetes.namespace=ns-pagamentos-x9
app.cobranca.service=svc-cobranca-x9
app.relatorios.dir=/srv/relatorios_x9
cobranca.url=https://api-x9.vendas.interno
`},
	{"Caixa", `DB_HOST=sqlprd01
o servidor SQLPRD01 caiu de novo; o SqlPrd01 voltou
`},
	{"Placeholder", `spring.datasource.url=${erp.url:http://erpprd01.acme.local:8080}
ERP_URL=${ERP_URL:-http://erpprd01.acme.local:8080}
`},
	{"Lista aninhada", `consumer.subscribe(List.of("pedidos-criados", "pedidos-pagos"));
consumer.subscribe(Arrays.asList("pedidos-criados"));
consumer.subscribe(["pedidos-pagos"])
`},
	{"Host e porta", `$redis->connect('cacheprd01', 6379);
r = redis.Redis("cacheprd01", 6379)
`},
	{"Caminho raiz", `cp /dados/Relatorios_x9/mensal/a.csv /tmp/
ls /dados/Relatorios_x9
`},
	{"AWS", `arn:aws:s3:::bkt-relatorios-x9
https://sqs.sa-east-1.amazonaws.com/123456789012/fila-pedidos-x9
arn:aws:sqs:sa-east-1:123456789012:fila-pedidos-x9
`},
	{"GCP", "SELECT * FROM `proj-vendas-x9.vendas_prd.tb_pedidos_x9`\n" + `SELECT count(*) FROM proj-vendas-x9.vendas_prd.tb_pedidos_x9 WHERE a = 1 AND b = 2
gs://bkt-relatorios-x9/entrada/
projects/proj-vendas-x9/topics/topico-eventos-x9
`},
	{"Azure", `https://stvendasx9.blob.core.windows.net/bkt-relatorios-x9/a.csv
Endpoint=sb://sb-vendas-x9.servicebus.windows.net/;SharedAccessKeyName=x;EntityPath=fila-pedidos-x9
/subscriptions/00000000-1111-2222-3333-444444444444/resourceGroups/rg-vendas-x9/providers/Microsoft.Sql/servers/pgsrv-vendas-01/databases/vendas_prd
`},
	{"Tópico com ponto", `KAFKA_TOPIC=fin.notas.emitidas
kafka.topic=fin.notas.emitidas
`},
	{"Terraform aninhado", `resource "kubernetes_namespace" "pag" {
  metadata {
    name = "ns-pagamentos-x9"
  }
}
`},
}

// matrizCobertura devolve, por caso, quantas ocorrências dos nomes havia e quantas sobraram.
func matrizCobertura(t *testing.T) (map[string][2]int, map[string]int) {
	cob := map[string][2]int{}
	sobra := map[string]int{}
	for _, c := range matrizCasos {
		m := novoTeste(t)
		m.cfg.DominiosInternos = nil
		m.cfg.Termos = nil
		out, _ := m.Mascarar(c.texto)
		tot, ok := 0, 0
		for _, n := range matrizNomes {
			a, b := strings.Count(c.texto, n), strings.Count(out, n)
			tot += a
			ok += a - b
			if b > 0 {
				sobra[c.nome+": "+n] += b
			}
		}
		cob[c.nome] = [2]int{ok, tot}
	}
	return cob, sobra
}

func TestMatrizCobertura(t *testing.T) {
	cob, sobra := matrizCobertura(t)
	ok, tot := 0, 0
	nomes := make([]string, 0, len(cob))
	for k := range cob {
		nomes = append(nomes, k)
	}
	sort.Strings(nomes)
	var linhas []string
	for _, k := range nomes {
		c := cob[k]
		ok += c[0]
		tot += c[1]
		linhas = append(linhas, fmt.Sprintf("%-12s %3d/%-3d %5.1f%%", k, c[0], c[1], 100*float64(c[0])/float64(c[1])))
	}
	pct := 100 * float64(ok) / float64(tot)
	t.Logf("cobertura por caso:\n%s\nTOTAL %d/%d = %.1f%%", strings.Join(linhas, "\n"), ok, tot, pct)
	if testing.Verbose() {
		var ss []string
		for k, v := range sobra {
			ss = append(ss, fmt.Sprintf("%s (%d)", k, v))
		}
		sort.Strings(ss)
		t.Logf("em claro:\n  %s", strings.Join(ss, "\n  "))
	}
	if pct < 90 {
		t.Errorf("cobertura %.1f%% abaixo da meta de 90%%", pct)
	}
}
