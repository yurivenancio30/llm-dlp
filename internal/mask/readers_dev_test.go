package mask

import (
	"sort"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// Tests of the development readers: key-value, addresses, git, packages, paths, network user,
// cloud, public IP and embedded terms. Made-up data.

// readOnes: what the reader found in s, as "ent:value" (+ "!" when strong), in order.
func readOnes(find func(string, func(ObjFinding)), s string) []string {
	var out []string
	find(s, func(o ObjFinding) {
		v := o.Ent + ":" + s[o.Start:o.End]
		if o.Strong {
			v += "!"
		}
		out = append(out, v)
	})
	sort.Strings(out)
	// no repetition (two paths can find the same snippet)
	var u []string
	for i, v := range out {
		if i == 0 || v != out[i-1] {
			u = append(u, v)
		}
	}
	return u
}

type readerCase struct {
	s     string
	wants []string // "ent:value" or "ent:value!" (strong)
}

func checkReader(t *testing.T, name string, find func(string, func(ObjFinding)), pub func(string) bool, cases []readerCase) {
	t.Helper()
	for _, c := range cases {
		got := readOnes(func(s string, add func(ObjFinding)) {
			find(s, func(o ObjFinding) {
				if pub != nil && pub(s[o.Start:o.End]) {
					return // findObjects discards the public vocabulary
				}
				add(o)
			})
		}, c.s)
		if strings.Join(got, " ") != strings.Join(c.wants, " ") {
			t.Errorf("%s: %q\n   achou %q\n   quer  %q", name, c.s, got, c.wants)
		}
	}
}

func TestReaderKeyValue(t *testing.T) {
	checkReader(t, "chave-valor", findKeyValue, publicDev, []readerCase{
		// YAML, JSON, TOML, INI, .env, .properties
		{"host: db-exemplo-01.interno\nport: 5432\n", []string{"server:db-exemplo-01.interno!"}},
		{"database: vendas_x\nschema: financeiro\n", []string{"database:vendas_x!", "schema:financeiro!"}},
		{`{"host": "db-exemplo-01", "user": "svc_relatorio", "timeout": 30}`, []string{"server:db-exemplo-01!", "user:svc_relatorio!"}},
		{"[conexao]\nhost=db-exemplo-01\ndbname=vendas_x\n", []string{"database:vendas_x!", "server:db-exemplo-01!"}},
		{"export DB_HOST=db-exemplo-01\nDB_USER=svc_relatorio\n", []string{"server:db-exemplo-01!", "user:svc_relatorio!"}},
		{"spring.datasource.username=svc_relatorio\n", []string{"user:svc_relatorio!"}},
		{"bootstrap.servers=kafka-01:9092,kafka-02:9092\n", []string{"server:kafka-01!", "server:kafka-02!"}},
		{"queue: fila-pedidos-x9\nbucket = \"bkt-relatorios-demo\"\n", []string{"bucket:bkt-relatorios-demo!", "queue:fila-pedidos-x9!"}},
		{"table: financeiro.tb_pedido_x9\n", []string{"schema:financeiro!", "table:tb_pedido_x9!"}},
		{"namespace: vendas-prod\napp: pedidos-api\n", []string{"namespace:vendas-prod!", "service:pedidos-api!"}},
		{"project_id = \"proj-exemplo-01\"\n", []string{"cloud_account:proj-exemplo-01!"}},
		{`dbHost: "db-exemplo-02"`, []string{"server:db-exemplo-02!"}},
		// XML: elemento e atributo
		{"<host>db-exemplo-01</host><database>vendas_x</database>", []string{"database:vendas_x!", "server:db-exemplo-01!"}},
		{`<conexao host="db-exemplo-01" porta="1">`, []string{"server:db-exemplo-01!"}},
		// command line
		{"pg_dump --host db-exemplo-01 --dbname=vendas_x --username svc_relatorio", []string{"database:vendas_x!", "server:db-exemplo-01!", "user:svc_relatorio!"}},
		{"kubectl get pods --namespace vendas-prod", []string{"namespace:vendas-prod!"}},
		// value that stays: number, boolean, localhost, path, expression, public domain
		{"host: localhost\nport: 5432\nuser: root\nenabled: true\n", nil},
		{"host: ${DB_HOST}\nuser: {{ .Values.user }}\ndb: $DATABASE\n", nil},
		{"host=%s user=%s", nil},
		{"host: api.github.com\nendpoint: https://example.com/x\n", nil},
		{"user_count: 10\nhost_port: 8080\nmax_user: 3\n", nil},
		{"config: /etc/app/config.yaml\n", nil},
		{"--host string   endereço do servidor\n--user USER\n", nil},
		{"pip install --user requests\n", nil},
		{"Server: nginx\nUser: alice\n", nil},
	})
	// common Go/Python/JS code: nothing
	checkReader(t, "chave-valor (código)", findKeyValue, publicDev, []readerCase{
		{"host := cfg.Host\nuser := u.User\n", nil},
		{"    user = models.ForeignKey(User)\n    host = self.host\n    db = get_db()\n", nil},
		{"    table = table_name\n    user = request.user\n", nil},
		{"    self.user = user_obj\n", nil},
		{"return &Server{Host: host, User: u}\n", nil},
		{"conn = connect(host=db_host, user=user_name)\n", nil},
		{"const opts = { host: process.env.HOST, user: config.user };\n", nil},
		{"def f(host: str, user: str = None):\n", nil},
		{"case \"host\":\n\treturn x\n", nil},
		{"if x == y { host = h }\n", nil},
		{"for (i=0; i<n; i++) { x=y; }\n", nil},
		{"`json:\"host\"`\n", nil},
	})
}

func TestReaderAddresses(t *testing.T) {
	checkReader(t, "endereço", findAddresses, publicDev, []readerCase{
		// internal host in a URL
		{"veja http://wiki-interna/pagina e https://grafana.vendas.svc.cluster.local:3000/d/x", []string{"server:wiki-interna!"}}, // the Kubernetes DNS is left to the Kubernetes reader
		{"grpc://svc-pedidos.internal:50051", []string{"server:svc-pedidos.internal!"}},
		{"http://svc_relatorio@intranet-x9/painel", []string{"server:intranet-x9!", "user:svc_relatorio!"}},
		// armazenamento
		{"aws s3 cp s3://bkt-relatorios-demo/carga_diaria/2026/arquivo.csv .", []string{"bucket:bkt-relatorios-demo!", "folder:carga_diaria"}},
		{"gs://bkt-relatorios-demo/dados_x/", []string{"bucket:bkt-relatorios-demo!", "folder:dados_x"}},
		{"abfss://ctn-dados@contaexemplo01.dfs.core.windows.net/bruto_x/", []string{"bucket:ctn-dados!", "cloud_account:contaexemplo01!", "folder:bruto_x"}},
		{"hdfs://namenode-x1:8020/user/etl_x/", []string{"folder:etl_x", "server:namenode-x1!"}},
		// filas
		{"amqp://svc_relatorio:x@rabbit-01.interno:5672/vhost_pedidos", []string{"queue:vhost_pedidos!", "server:rabbit-01.interno!", "user:svc_relatorio!"}},
		{"kafka://broker-01:9092/fila-pedidos-x9", []string{"queue:fila-pedidos-x9!", "server:broker-01!"}},
		// git in context
		{"git clone https://github.com/org-exemplo/repo-demo.git", []string{"organization:org-exemplo!", "repository:repo-demo!"}},
		{"git remote add origin https://gitlab.interno/org-exemplo/sub/repo-demo", []string{"organization:org-exemplo!", "organization:sub!", "repository:repo-demo!", "server:gitlab.interno!"}},
		{"ssh://git@git-interno/org-exemplo/repo-demo.git", []string{"organization:org-exemplo!", "repository:repo-demo!", "server:git-interno!"}},
		{"[remote \"origin\"]\n\turl = https://bitbucket.org/org-exemplo/repo-demo\n", []string{"organization:org-exemplo!", "repository:repo-demo!"}},
		// address without a scheme
		{"conecte em db01.corp:5432 ou redis.vendas.svc.cluster.local", []string{"server:db01.corp!"}}, // the Kubernetes DNS is left to the Kubernetes reader
		// stays: public domain, documentation, localhost, code
		{"veja https://github.com/golang/go/issues/123 e https://pkg.go.dev/net/http", nil},
		{"http://localhost:8080/ e http://127.0.0.1:9000 e http://host:port/", nil},
		{"local = threading.local()\nimport com.acme.internal\nx := os.local", nil},
		// a name that looks like an example is also masked (it may be real; when in doubt, mask)
		{"s3://my-bucket-x1/path/", []string{"bucket:my-bucket-x1!"}},
		{"file:///home/x e postgres://db/x", nil},
		{"~/.local/share e /usr/local/bin", nil},
	})
}

func TestReaderGitSCP(t *testing.T) {
	checkReader(t, "git", findGitSCP, publicDev, []readerCase{
		{"git clone git@github.com:org-exemplo/repo-demo.git", []string{"organization:org-exemplo!", "repository:repo-demo!"}},
		{"url = git@gitlab.corp:org-exemplo/repo-demo", []string{"organization:org-exemplo!", "repository:repo-demo!", "server:gitlab.corp!"}},
		{"e-mail digit@x.com e git@host:22", nil},
	})
}

func TestReaderPackages(t *testing.T) {
	checkReader(t, "package", findPackages, publicDev, []readerCase{
		{"module git.interno/org-exemplo/svc-pedidos\n\ngo 1.22\n", []string{"package:org-exemplo!", "package:svc-pedidos!", "server:git.interno!"}},
		{"module go.empresa-ficticia.com.br/vendas/v2\n", []string{"package:vendas!"}},
		{"module github.com/org-exemplo/repo-demo\n", nil},
		{"<groupId>br.com.exemplo01.vendas</groupId>", []string{"package:exemplo01!", "package:vendas!"}},
		{"<groupId>org.springframework.boot</groupId><groupId>io.netty</groupId>", nil},
		{"group = 'com.exemplo01.relatorio'\n", []string{"package:exemplo01!", "package:relatorio!"}},
		{`{"name": "@org-exemplo/pacote-demo", "version": "1.0.0"}`, []string{"organization:org-exemplo!", "package:pacote-demo!"}},
		{`{"name": "@types/node"}`, nil},
		{"the module system is described below\n", nil},
	})
}

func TestReaderPaths(t *testing.T) {
	checkReader(t, "caminho", findPaths, publicDev, []readerCase{
		{"/home/joao_x/projetos/cliente_x9/main.go", []string{"folder:cliente_x9", "user:joao_x!"}},
		{"/Users/maria.s/Documents/rel_2026/", []string{"folder:rel_2026", "user:maria.s!"}},
		{`C:\Users\jsilva01\AppData\Local\proj_x\`, []string{"folder:proj_x", "user:jsilva01!"}},
		{`"C:\\Users\\jsilva01\\Desktop"`, []string{"user:jsilva01!"}},
		{"/home/runner/work/x e /home/user/.config e /Users/Shared/x", nil},
		{"/home/$USER/x e /home/<voce>/x", nil},
		{"/home/ana/go/pkg/mod/github.com/x/y@v1.2.3/", []string{"user:ana!"}},
	})
}

func TestReaderNetworkUsers(t *testing.T) {
	checkReader(t, "usuário de rede", findNetworkUsers, publicDev, []readerCase{
		{`entrou como CORPX\jsilva01 ontem`, []string{"user:jsilva01!"}},
		{`"user": "CORPX\\svc_relatorio"`, []string{"user:svc_relatorio!"}},
		{"ssh -i ~/.ssh/id -p 2222 svc_relatorio@db-exemplo-01 'ls'", []string{"server:db-exemplo-01!", "user:svc_relatorio!"}},
		{"scp arquivo.csv joao_x@bastion-x1.interno:/tmp/", []string{"server:bastion-x1.interno!", "user:joao_x!"}},
		{"ssh ubuntu@ec2-1-2-3-4.compute-1.amazonaws.com", nil},
		{`NT AUTHORITY\SYSTEM e BUILTIN\Administrators e HKLM\SOFTWARE\Microsoft`, nil},
		{`fmt.Println("ERRO\nfalhou") e "MAX\tvalor" e regex [A-Z]\w+`, nil},
		{`\\SERVIDOR\compartilhado\pasta`, nil},
	})
}

func TestReaderCloud(t *testing.T) {
	checkReader(t, "nuvem", findCloud, publicDev, []readerCase{
		{"arn:aws:s3:::bkt-relatorios-demo/*", []string{"bucket:bkt-relatorios-demo!"}},
		{"arn:aws:sqs:us-east-1:210987654321:fila-pedidos-x9", []string{"cloud_account:210987654321!", "queue:fila-pedidos-x9!"}},
		{"arn:aws:dynamodb:sa-east-1:210987654321:table/tb_pedido_x9/stream/2026", []string{"cloud_account:210987654321!", "table:tb_pedido_x9!"}},
		{"arn:aws:lambda:us-east-1:210987654321:function:fn-carga-x9:prod", []string{"cloud_account:210987654321!", "service:fn-carga-x9!"}},
		{"arn:aws:iam::210987654321:role/servico/role-etl-x9", []string{"cloud_account:210987654321!", "service:role-etl-x9!"}},
		{"arn:aws:iam::123456789012:user/svc-etl-x9 e arn:aws:iam::aws:policy/ReadOnlyAccess", []string{"cloud_account:123456789012!", "user:svc-etl-x9!"}},
		{"/subscriptions/1a2b3c4d-1111-2222-3333-abcdefabcdef/resourceGroups/rg-dados-x9/providers/Microsoft.Storage/storageAccounts/contaexemplo01",
			[]string{"cloud_account:1a2b3c4d-1111-2222-3333-abcdefabcdef!", "cloud_account:contaexemplo01!", "namespace:rg-dados-x9!"}},
		{"projects/proj-exemplo-01/topics/fila-pedidos-x9", []string{"cloud_account:proj-exemplo-01!", "queue:fila-pedidos-x9!"}},
		{"projects/proj-exemplo-01/datasets/ds_vendas/tables/tb_pedido_x9", []string{"cloud_account:proj-exemplo-01!", "schema:ds_vendas!", "table:tb_pedido_x9!"}},
		{"projects/proj-exemplo-01/locations/us-central1/functions/fn-carga", []string{"cloud_account:proj-exemplo-01!", "service:fn-carga!"}},
		{"projects/my-project/topics/my-topic e ~/projects/x/y/z", []string{"cloud_account:my-project!", "queue:my-topic!"}},
	})
}

func TestReaderPublicIP(t *testing.T) {
	checkReader(t, "ip-público", findPublicIP, nil, []readerCase{
		{"servidor em 34.120.10.5 e 2600:1f18:abcd::12", []string{"server:2600:1f18:abcd::12!", "server:34.120.10.5!"}},
		{"10.0.0.1 192.168.1.1 127.0.0.1 8.8.8.8 1.1.1.1 192.0.2.10 ::1 fe80::1 2001:db8::1", nil},
		{"versão 1.2.3.4.5, hora 12:30:45, mac aa:bb:cc:dd:ee:ff, slice x[1::2], std::vector", nil},
	})
	// off by default
	if ls := configReaders(config.Default()); len(ls) != 0 {
		t.Errorf("leitores de configuração ligados por padrão: %d", len(ls))
	}
	cfg := config.Default()
	cfg.Objects.PublicIP = true
	m, _ := NewMasker(cfg, testKey, nil, nil)
	out, _ := m.Mask("servidor em 34.120.10.5")
	if strings.Contains(out, "34.120.10.5") {
		t.Errorf("IP público ficou com a opção ligada: %q", out)
	}
	m2, _ := NewMasker(config.Default(), testKey, nil, nil)
	if out, _ := m2.Mask("servidor em 34.120.10.5"); !strings.Contains(out, "34.120.10.5") {
		t.Errorf("IP público mascarado com a opção desligada: %q", out)
	}
}

func TestReaderEmbeddedTerms(t *testing.T) {
	ts := []string{"acmex"}
	find := func(s string, add func(ObjFinding)) { findTerms(s, ts, add) }
	checkReader(t, "termo-embutido", find, nil, []readerCase{
		{"SELECT * FROM acmex_pedidos", []string{"table:acmex_pedidos!"}},
		{"host: dbAcmexVendas01", []string{"server:dbAcmexVendas01!"}},
		{"o job svc-acmex-carga falhou", []string{"service:svc-acmex-carga!"}},
		{"a acmex e acmexvendas e macmex_x", nil}, // term alone (another detector) and a piece that is not whole
	})
	cfg := config.Default()
	cfg.Terms = []config.Term{{Label: "empresa", Values: []string{"Acmex"}}}
	m, _ := NewMasker(cfg, testKey, nil, nil)
	out, _ := m.Mask("o job svc-acmex-carga falhou")
	if strings.Contains(strings.ToLower(out), "acmex") {
		t.Errorf("termo embutido ficou: %q", out)
	}
	// learned in a strong position: masked again in prose
	if out, _ := m.Mask("reinicie o svc-acmex-carga"); strings.Contains(out, "svc-acmex-carga") {
		t.Errorf("nome com termo não propagou: %q", out)
	}
}

// Round trip: the pseudonym of host, path, user, bucket and repository goes back to the real one.
func TestDevReadersRoundTrip(t *testing.T) {
	m := newTest(t)
	for _, s := range []string{
		"host: db-exemplo-01.interno\nuser: svc_relatorio\n",
		"abra /home/joao_x/projetos/cliente_x9/ agora",
		"aws s3 ls s3://bkt-relatorios-demo/carga_diaria/",
		"git clone git@github.com:org-exemplo/repo-demo.git",
		"ssh svc_relatorio@bastion-x1",
		"arn:aws:sqs:us-east-1:210987654321:fila-pedidos-x9",
	} {
		out, ents := m.Mask(s)
		if out == s {
			t.Errorf("nada mascarado: %q", s)
			continue
		}
		if back := NewTable(ents).Unmask(out, false); back != s {
			t.Errorf("ida e volta:\n   %q\n-> %q\n-> %q", s, out, back)
		}
	}
}

// A name learned in a strong position is masked in prose and in another format.
func TestDevReadersPropagate(t *testing.T) {
	m := newTest(t)
	m.Mask("namespace: vendas-prod-x9\nbucket: bkt-relatorios-demo\n")
	m.Mask("git clone git@github.com:org-exemplo/repo-demo.git")
	for _, s := range []string{
		"o pod caiu no vendas-prod-x9 de novo",
		`{"destino": "bkt-relatorios-demo"}`,
		"abra um PR no repo-demo",
		"| repo-demo | org-exemplo |",
	} {
		out, _ := m.Mask(s)
		for _, v := range []string{"vendas-prod-x9", "bkt-relatorios-demo", "repo-demo", "org-exemplo"} {
			if strings.Contains(out, v) {
				t.Errorf("%q não propagou:\n   %q\n-> %q", v, s, out)
			}
		}
	}
	// a learned plain word does not propagate (it does not look like an identifier)
	m.Mask("database: vendas\n")
	if out, _ := m.Mask("as vendas subiram"); !strings.Contains(out, "vendas") {
		t.Errorf("palavra comum propagou: %q", out)
	}
}

// The readers that depend on the configuration never break on odd input either.
func FuzzReadersDev(f *testing.F) {
	for _, s := range []string{"host: ", "--host ", "</host>", "s3://", "git@x:", "arn:aws:", "/subscriptions/x/",
		"projects/a/b/c", "C:\\Users\\", "A\\b", "ssh a@", "::1:", "module ", "\"name\": \"@a/\"", "acmex_"} {
		f.Add(s)
	}
	ls := configReaders(config.Config{Objects: config.Objects{PublicIP: true}, Terms: []config.Term{{Values: []string{"acmex"}}}})
	f.Fuzz(func(t *testing.T, s string) {
		for _, l := range append(defaultReaders(), ls...) {
			l.Find(s, func(o ObjFinding) {
				if o.Start < 0 || o.End > len(s) || o.End < o.Start {
					t.Fatalf("%s: trecho fora do texto %d..%d de %d", l.Name, o.Start, o.End, len(s))
				}
			})
		}
	})
}

// A value followed by an operator is a code expression, not a resource name.
func TestKeyValueExpressionIsNotName(t *testing.T) {
	m := newTest(t)
	for _, s := range []string{"// addr = arg0 + auxInt + aux", "host = base_x1 + sufixo", "server = srv_a1 - 1", "user = u_x1 * 2"} {
		if out, ents := m.Mask(s); len(ents) > 0 {
			t.Errorf("expressão virou nome:\n   %q\n-> %q", s, out)
		}
	}
	if out, _ := m.Mask("host = db-exemplo-01.interno\n"); strings.Contains(out, "db-exemplo-01") {
		t.Errorf("valor simples deixou de ser mascarado: %q", out)
	}
}

// Bucket and queue from an explicit key and metadata.name of Deployment/Service/StatefulSet are
// strong evidence: the learned name is masked in other text too.
func TestExplicitKeyTeaches(t *testing.T) {
	for _, c := range []struct{ teaches, name string }{
		{"S3_BUCKET=bkt-relatorios-x9\n", "bkt-relatorios-x9"},
		{"export ORDERS_BUCKET=bkt-pedidos-x9\n", "bkt-pedidos-x9"},
		{"KAFKA_TOPIC=topico-eventos-x9\n", "topico-eventos-x9"},
		{"PAYMENTS_QUEUE: fila-pagamentos-x9\n", "fila-pagamentos-x9"},
		{"orders.topic=topico-pedidos-x9\n", "topico-pedidos-x9"},
		{"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api-cobranca-x9\nspec:\n  replicas: 2\n", "api-cobranca-x9"},
		{"apiVersion: v1\nkind: Service\nmetadata:\n  name: svc-cobranca-x9\nspec:\n  type: ClusterIP\n", "svc-cobranca-x9"},
		{"apiVersion: apps/v1\nkind: StatefulSet\nmetadata:\n  name: pg-cobranca-x9\nspec:\n  serviceName: pg\n", "pg-cobranca-x9"},
	} {
		m := newTest(t)
		if out, _ := m.Mask(c.teaches); strings.Contains(out, c.name) {
			t.Errorf("na posição: %q", out)
		}
		if out, _ := m.Mask("depois, em prosa: " + c.name + " caiu"); strings.Contains(out, c.name) {
			t.Errorf("não aprendeu %q: %q", c.name, out)
		}
	}
}
