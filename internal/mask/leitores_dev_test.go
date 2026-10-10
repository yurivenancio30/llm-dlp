package mask

import (
	"sort"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// Testes dos leitores de desenvolvimento: chave-valor, endereços, git, pacotes, caminhos, usuário de
// rede, nuvem, IP público e termos embutidos. Dados fictícios.

// lidos: o que o leitor achou em s, como "ent:valor" (+ "!" quando forte), em ordem.
func lidos(achar func(string, func(ObjAchado)), s string) []string {
	var out []string
	achar(s, func(o ObjAchado) {
		v := o.Ent + ":" + s[o.Ini:o.Fim]
		if o.Forte {
			v += "!"
		}
		out = append(out, v)
	})
	sort.Strings(out)
	// sem repetição (dois caminhos podem achar o mesmo trecho)
	var u []string
	for i, v := range out {
		if i == 0 || v != out[i-1] {
			u = append(u, v)
		}
	}
	return u
}

type casoLeitor struct {
	s    string
	quer []string // "ent:valor" ou "ent:valor!" (forte)
}

func conferirLeitor(t *testing.T, nome string, achar func(string, func(ObjAchado)), pub func(string) bool, casos []casoLeitor) {
	t.Helper()
	for _, c := range casos {
		got := lidos(func(s string, add func(ObjAchado)) {
			achar(s, func(o ObjAchado) {
				if pub != nil && pub(s[o.Ini:o.Fim]) {
					return // acharObjetos descarta o vocabulário público
				}
				add(o)
			})
		}, c.s)
		if strings.Join(got, " ") != strings.Join(c.quer, " ") {
			t.Errorf("%s: %q\n   achou %q\n   quer  %q", nome, c.s, got, c.quer)
		}
	}
}

func TestLeitorChaveValor(t *testing.T) {
	conferirLeitor(t, "chave-valor", acharChaveValor, publicoDev, []casoLeitor{
		// YAML, JSON, TOML, INI, .env, .properties
		{"host: db-exemplo-01.interno\nport: 5432\n", []string{"servidor:db-exemplo-01.interno!"}},
		{"database: vendas_x\nschema: financeiro\n", []string{"database:vendas_x!", "schema:financeiro!"}},
		{`{"host": "db-exemplo-01", "user": "svc_relatorio", "timeout": 30}`, []string{"servidor:db-exemplo-01!", "usuario:svc_relatorio!"}},
		{"[conexao]\nhost=db-exemplo-01\ndbname=vendas_x\n", []string{"database:vendas_x!", "servidor:db-exemplo-01!"}},
		{"export DB_HOST=db-exemplo-01\nDB_USER=svc_relatorio\n", []string{"servidor:db-exemplo-01!", "usuario:svc_relatorio!"}},
		{"spring.datasource.username=svc_relatorio\n", []string{"usuario:svc_relatorio!"}},
		{"bootstrap.servers=kafka-01:9092,kafka-02:9092\n", []string{"servidor:kafka-01!", "servidor:kafka-02!"}},
		{"queue: fila-pedidos-x9\nbucket = \"bkt-relatorios-demo\"\n", []string{"bucket:bkt-relatorios-demo!", "fila:fila-pedidos-x9!"}},
		{"table: financeiro.tb_pedido_x9\n", []string{"schema:financeiro!", "tabela:tb_pedido_x9!"}},
		{"namespace: vendas-prod\napp: pedidos-api\n", []string{"namespace:vendas-prod!", "servico:pedidos-api!"}},
		{"project_id = \"proj-exemplo-01\"\n", []string{"conta_nuvem:proj-exemplo-01!"}},
		{`dbHost: "db-exemplo-02"`, []string{"servidor:db-exemplo-02!"}},
		// XML: elemento e atributo
		{"<host>db-exemplo-01</host><database>vendas_x</database>", []string{"database:vendas_x!", "servidor:db-exemplo-01!"}},
		{`<conexao host="db-exemplo-01" porta="1">`, []string{"servidor:db-exemplo-01!"}},
		// linha de comando
		{"pg_dump --host db-exemplo-01 --dbname=vendas_x --username svc_relatorio", []string{"database:vendas_x!", "servidor:db-exemplo-01!", "usuario:svc_relatorio!"}},
		{"kubectl get pods --namespace vendas-prod", []string{"namespace:vendas-prod!"}},
		// valor que fica: número, booleano, localhost, caminho, expressão, domínio público
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
	// código Go/Python/JS comum: nada
	conferirLeitor(t, "chave-valor (código)", acharChaveValor, publicoDev, []casoLeitor{
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

func TestLeitorEnderecos(t *testing.T) {
	conferirLeitor(t, "endereço", acharEnderecos, publicoDev, []casoLeitor{
		// host interno em URL
		{"veja http://wiki-interna/pagina e https://grafana.vendas.svc.cluster.local:3000/d/x", []string{"servidor:wiki-interna!"}}, // o DNS do Kubernetes fica com o leitor de Kubernetes
		{"grpc://svc-pedidos.internal:50051", []string{"servidor:svc-pedidos.internal!"}},
		{"http://svc_relatorio@intranet-x9/painel", []string{"servidor:intranet-x9!", "usuario:svc_relatorio!"}},
		// armazenamento
		{"aws s3 cp s3://bkt-relatorios-demo/carga_diaria/2026/arquivo.csv .", []string{"bucket:bkt-relatorios-demo!", "pasta:carga_diaria"}},
		{"gs://bkt-relatorios-demo/dados_x/", []string{"bucket:bkt-relatorios-demo!", "pasta:dados_x"}},
		{"abfss://ctn-dados@contaexemplo01.dfs.core.windows.net/bruto_x/", []string{"bucket:ctn-dados!", "conta_nuvem:contaexemplo01!", "pasta:bruto_x"}},
		{"hdfs://namenode-x1:8020/user/etl_x/", []string{"pasta:etl_x", "servidor:namenode-x1!"}},
		// filas
		{"amqp://svc_relatorio:x@rabbit-01.interno:5672/vhost_pedidos", []string{"fila:vhost_pedidos!", "servidor:rabbit-01.interno!", "usuario:svc_relatorio!"}},
		{"kafka://broker-01:9092/fila-pedidos-x9", []string{"fila:fila-pedidos-x9!", "servidor:broker-01!"}},
		// git em contexto
		{"git clone https://github.com/org-exemplo/repo-demo.git", []string{"organizacao:org-exemplo!", "repositorio:repo-demo!"}},
		{"git remote add origin https://gitlab.interno/org-exemplo/sub/repo-demo", []string{"organizacao:org-exemplo!", "organizacao:sub!", "repositorio:repo-demo!", "servidor:gitlab.interno!"}},
		{"ssh://git@git-interno/org-exemplo/repo-demo.git", []string{"organizacao:org-exemplo!", "repositorio:repo-demo!", "servidor:git-interno!"}},
		{"[remote \"origin\"]\n\turl = https://bitbucket.org/org-exemplo/repo-demo\n", []string{"organizacao:org-exemplo!", "repositorio:repo-demo!"}},
		// endereço sem esquema
		{"conecte em db01.corp:5432 ou redis.vendas.svc.cluster.local", []string{"servidor:db01.corp!"}}, // o DNS do Kubernetes fica com o leitor de Kubernetes
		// fica: domínio público, documentação, localhost, código
		{"veja https://github.com/golang/go/issues/123 e https://pkg.go.dev/net/http", nil},
		{"http://localhost:8080/ e http://127.0.0.1:9000 e http://host:port/", nil},
		{"local = threading.local()\nimport com.acme.internal\nx := os.local", nil},
		// nome com cara de exemplo também é mascarado (pode ser real; na dúvida, mascara)
		{"s3://my-bucket-x1/path/", []string{"bucket:my-bucket-x1!"}},
		{"file:///home/x e postgres://db/x", nil},
		{"~/.local/share e /usr/local/bin", nil},
	})
}

func TestLeitorGitSCP(t *testing.T) {
	conferirLeitor(t, "git", acharGitSCP, publicoDev, []casoLeitor{
		{"git clone git@github.com:org-exemplo/repo-demo.git", []string{"organizacao:org-exemplo!", "repositorio:repo-demo!"}},
		{"url = git@gitlab.corp:org-exemplo/repo-demo", []string{"organizacao:org-exemplo!", "repositorio:repo-demo!", "servidor:gitlab.corp!"}},
		{"e-mail digit@x.com e git@host:22", nil},
	})
}

func TestLeitorPacotes(t *testing.T) {
	conferirLeitor(t, "pacote", acharPacotes, publicoDev, []casoLeitor{
		{"module git.interno/org-exemplo/svc-pedidos\n\ngo 1.22\n", []string{"pacote:org-exemplo!", "pacote:svc-pedidos!", "servidor:git.interno!"}},
		{"module go.empresa-ficticia.com.br/vendas/v2\n", []string{"pacote:vendas!"}},
		{"module github.com/org-exemplo/repo-demo\n", nil},
		{"<groupId>br.com.exemplo01.vendas</groupId>", []string{"pacote:exemplo01!", "pacote:vendas!"}},
		{"<groupId>org.springframework.boot</groupId><groupId>io.netty</groupId>", nil},
		{"group = 'com.exemplo01.relatorio'\n", []string{"pacote:exemplo01!", "pacote:relatorio!"}},
		{`{"name": "@org-exemplo/pacote-demo", "version": "1.0.0"}`, []string{"organizacao:org-exemplo!", "pacote:pacote-demo!"}},
		{`{"name": "@types/node"}`, nil},
		{"the module system is described below\n", nil},
	})
}

func TestLeitorCaminhos(t *testing.T) {
	conferirLeitor(t, "caminho", acharCaminhos, publicoDev, []casoLeitor{
		{"/home/joao_x/projetos/cliente_x9/main.go", []string{"pasta:cliente_x9", "usuario:joao_x!"}},
		{"/Users/maria.s/Documents/rel_2026/", []string{"pasta:rel_2026", "usuario:maria.s!"}},
		{`C:\Users\jsilva01\AppData\Local\proj_x\`, []string{"pasta:proj_x", "usuario:jsilva01!"}},
		{`"C:\\Users\\jsilva01\\Desktop"`, []string{"usuario:jsilva01!"}},
		{"/home/runner/work/x e /home/user/.config e /Users/Shared/x", nil},
		{"/home/$USER/x e /home/<voce>/x", nil},
		{"/home/ana/go/pkg/mod/github.com/x/y@v1.2.3/", []string{"usuario:ana!"}},
	})
}

func TestLeitorUsuariosRede(t *testing.T) {
	conferirLeitor(t, "usuário de rede", acharUsuariosRede, publicoDev, []casoLeitor{
		{`entrou como CORPX\jsilva01 ontem`, []string{"usuario:jsilva01!"}},
		{`"user": "CORPX\\svc_relatorio"`, []string{"usuario:svc_relatorio!"}},
		{"ssh -i ~/.ssh/id -p 2222 svc_relatorio@db-exemplo-01 'ls'", []string{"servidor:db-exemplo-01!", "usuario:svc_relatorio!"}},
		{"scp arquivo.csv joao_x@bastion-x1.interno:/tmp/", []string{"servidor:bastion-x1.interno!", "usuario:joao_x!"}},
		{"ssh ubuntu@ec2-1-2-3-4.compute-1.amazonaws.com", nil},
		{`NT AUTHORITY\SYSTEM e BUILTIN\Administrators e HKLM\SOFTWARE\Microsoft`, nil},
		{`fmt.Println("ERRO\nfalhou") e "MAX\tvalor" e regex [A-Z]\w+`, nil},
		{`\\SERVIDOR\compartilhado\pasta`, nil},
	})
}

func TestLeitorNuvem(t *testing.T) {
	conferirLeitor(t, "nuvem", acharNuvem, publicoDev, []casoLeitor{
		{"arn:aws:s3:::bkt-relatorios-demo/*", []string{"bucket:bkt-relatorios-demo!"}},
		{"arn:aws:sqs:us-east-1:210987654321:fila-pedidos-x9", []string{"conta_nuvem:210987654321!", "fila:fila-pedidos-x9!"}},
		{"arn:aws:dynamodb:sa-east-1:210987654321:table/tb_pedido_x9/stream/2026", []string{"conta_nuvem:210987654321!", "tabela:tb_pedido_x9!"}},
		{"arn:aws:lambda:us-east-1:210987654321:function:fn-carga-x9:prod", []string{"conta_nuvem:210987654321!", "servico:fn-carga-x9!"}},
		{"arn:aws:iam::210987654321:role/servico/role-etl-x9", []string{"conta_nuvem:210987654321!", "servico:role-etl-x9!"}},
		{"arn:aws:iam::123456789012:user/svc-etl-x9 e arn:aws:iam::aws:policy/ReadOnlyAccess", []string{"conta_nuvem:123456789012!", "usuario:svc-etl-x9!"}},
		{"/subscriptions/1a2b3c4d-1111-2222-3333-abcdefabcdef/resourceGroups/rg-dados-x9/providers/Microsoft.Storage/storageAccounts/contaexemplo01",
			[]string{"conta_nuvem:1a2b3c4d-1111-2222-3333-abcdefabcdef!", "conta_nuvem:contaexemplo01!", "namespace:rg-dados-x9!"}},
		{"projects/proj-exemplo-01/topics/fila-pedidos-x9", []string{"conta_nuvem:proj-exemplo-01!", "fila:fila-pedidos-x9!"}},
		{"projects/proj-exemplo-01/datasets/ds_vendas/tables/tb_pedido_x9", []string{"conta_nuvem:proj-exemplo-01!", "schema:ds_vendas!", "tabela:tb_pedido_x9!"}},
		{"projects/proj-exemplo-01/locations/us-central1/functions/fn-carga", []string{"conta_nuvem:proj-exemplo-01!", "servico:fn-carga!"}},
		{"projects/my-project/topics/my-topic e ~/projects/x/y/z", []string{"conta_nuvem:my-project!", "fila:my-topic!"}},
	})
}

func TestLeitorIPPublico(t *testing.T) {
	conferirLeitor(t, "ip-público", acharIPPublico, nil, []casoLeitor{
		{"servidor em 34.120.10.5 e 2600:1f18:abcd::12", []string{"servidor:2600:1f18:abcd::12!", "servidor:34.120.10.5!"}},
		{"10.0.0.1 192.168.1.1 127.0.0.1 8.8.8.8 1.1.1.1 192.0.2.10 ::1 fe80::1 2001:db8::1", nil},
		{"versão 1.2.3.4.5, hora 12:30:45, mac aa:bb:cc:dd:ee:ff, slice x[1::2], std::vector", nil},
	})
	// desligado por padrão
	if ls := leitoresConfig(config.Padrao()); len(ls) != 0 {
		t.Errorf("leitores de configuração ligados por padrão: %d", len(ls))
	}
	cfg := config.Padrao()
	cfg.Objetos.IPPublico = true
	m, _ := NovoMasker(cfg, chaveTeste, nil, nil)
	out, _ := m.Mascarar("servidor em 34.120.10.5")
	if strings.Contains(out, "34.120.10.5") {
		t.Errorf("IP público ficou com a opção ligada: %q", out)
	}
	m2, _ := NovoMasker(config.Padrao(), chaveTeste, nil, nil)
	if out, _ := m2.Mascarar("servidor em 34.120.10.5"); !strings.Contains(out, "34.120.10.5") {
		t.Errorf("IP público mascarado com a opção desligada: %q", out)
	}
}

func TestLeitorTermosEmbutidos(t *testing.T) {
	ts := []string{"acmex"}
	achar := func(s string, add func(ObjAchado)) { acharTermos(s, ts, add) }
	conferirLeitor(t, "termo-embutido", achar, nil, []casoLeitor{
		{"SELECT * FROM acmex_pedidos", []string{"tabela:acmex_pedidos!"}},
		{"host: dbAcmexVendas01", []string{"servidor:dbAcmexVendas01!"}},
		{"o job svc-acmex-carga falhou", []string{"servico:svc-acmex-carga!"}},
		{"a acmex e acmexvendas e macmex_x", nil}, // termo sozinho (outro detector) e pedaço que não é inteiro
	})
	cfg := config.Padrao()
	cfg.Termos = []config.Termo{{Rotulo: "empresa", Valores: []string{"Acmex"}}}
	m, _ := NovoMasker(cfg, chaveTeste, nil, nil)
	out, _ := m.Mascarar("o job svc-acmex-carga falhou")
	if strings.Contains(strings.ToLower(out), "acmex") {
		t.Errorf("termo embutido ficou: %q", out)
	}
	// aprendido em posição forte: volta a ser mascarado em prosa
	if out, _ := m.Mascarar("reinicie o svc-acmex-carga"); strings.Contains(out, "svc-acmex-carga") {
		t.Errorf("nome com termo não propagou: %q", out)
	}
}

// Ida e volta: o pseudônimo de host, caminho, usuário, bucket e repositório volta ao real.
func TestLeitoresDevIdaEVolta(t *testing.T) {
	m := novoTeste(t)
	for _, s := range []string{
		"host: db-exemplo-01.interno\nuser: svc_relatorio\n",
		"abra /home/joao_x/projetos/cliente_x9/ agora",
		"aws s3 ls s3://bkt-relatorios-demo/carga_diaria/",
		"git clone git@github.com:org-exemplo/repo-demo.git",
		"ssh svc_relatorio@bastion-x1",
		"arn:aws:sqs:us-east-1:210987654321:fila-pedidos-x9",
	} {
		out, ents := m.Mascarar(s)
		if out == s {
			t.Errorf("nada mascarado: %q", s)
			continue
		}
		if back := NovaTabela(ents).Desmascarar(out, false); back != s {
			t.Errorf("ida e volta:\n   %q\n-> %q\n-> %q", s, out, back)
		}
	}
}

// Nome aprendido em posição forte é mascarado em prosa e em outro formato.
func TestLeitoresDevPropagam(t *testing.T) {
	m := novoTeste(t)
	m.Mascarar("namespace: vendas-prod-x9\nbucket: bkt-relatorios-demo\n")
	m.Mascarar("git clone git@github.com:org-exemplo/repo-demo.git")
	for _, s := range []string{
		"o pod caiu no vendas-prod-x9 de novo",
		`{"destino": "bkt-relatorios-demo"}`,
		"abra um PR no repo-demo",
		"| repo-demo | org-exemplo |",
	} {
		out, _ := m.Mascarar(s)
		for _, v := range []string{"vendas-prod-x9", "bkt-relatorios-demo", "repo-demo", "org-exemplo"} {
			if strings.Contains(out, v) {
				t.Errorf("%q não propagou:\n   %q\n-> %q", v, s, out)
			}
		}
	}
	// palavra simples aprendida não propaga (sem cara de identificador)
	m.Mascarar("database: vendas\n")
	if out, _ := m.Mascarar("as vendas subiram"); !strings.Contains(out, "vendas") {
		t.Errorf("palavra comum propagou: %q", out)
	}
}

// Os leitores que dependem da configuração também nunca quebram com entrada estranha.
func FuzzLeitoresDev(f *testing.F) {
	for _, s := range []string{"host: ", "--host ", "</host>", "s3://", "git@x:", "arn:aws:", "/subscriptions/x/",
		"projects/a/b/c", "C:\\Users\\", "A\\b", "ssh a@", "::1:", "module ", "\"name\": \"@a/\"", "acmex_"} {
		f.Add(s)
	}
	ls := leitoresConfig(config.Config{Objetos: config.Objetos{IPPublico: true}, Termos: []config.Termo{{Valores: []string{"acmex"}}}})
	f.Fuzz(func(t *testing.T, s string) {
		for _, l := range append(leitoresPadrao(), ls...) {
			l.Achar(s, func(o ObjAchado) {
				if o.Ini < 0 || o.Fim > len(s) || o.Fim < o.Ini {
					t.Fatalf("%s: trecho fora do texto %d..%d de %d", l.Nome, o.Ini, o.Fim, len(s))
				}
			})
		}
	})
}

// Valor seguido de operador é expressão de código, não nome de recurso.
func TestChaveValorExpressaoNaoENome(t *testing.T) {
	m := novoTeste(t)
	for _, s := range []string{"// addr = arg0 + auxInt + aux", "host = base_x1 + sufixo", "server = srv_a1 - 1", "user = u_x1 * 2"} {
		if out, ents := m.Mascarar(s); len(ents) > 0 {
			t.Errorf("expressão virou nome:\n   %q\n-> %q", s, out)
		}
	}
	if out, _ := m.Mascarar("host = db-exemplo-01.interno\n"); strings.Contains(out, "db-exemplo-01") {
		t.Errorf("valor simples deixou de ser mascarado: %q", out)
	}
}

// Bucket e fila de chave explícita e metadata.name de Deployment/Service/StatefulSet
// são evidência forte: o nome aprendido é mascarado também em outro texto.
func TestChaveExplicitaEnsina(t *testing.T) {
	for _, c := range []struct{ ensina, nome string }{
		{"S3_BUCKET=bkt-relatorios-x9\n", "bkt-relatorios-x9"},
		{"export ORDERS_BUCKET=bkt-pedidos-x9\n", "bkt-pedidos-x9"},
		{"KAFKA_TOPIC=topico-eventos-x9\n", "topico-eventos-x9"},
		{"PAYMENTS_QUEUE: fila-pagamentos-x9\n", "fila-pagamentos-x9"},
		{"orders.topic=topico-pedidos-x9\n", "topico-pedidos-x9"},
		{"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api-cobranca-x9\nspec:\n  replicas: 2\n", "api-cobranca-x9"},
		{"apiVersion: v1\nkind: Service\nmetadata:\n  name: svc-cobranca-x9\nspec:\n  type: ClusterIP\n", "svc-cobranca-x9"},
		{"apiVersion: apps/v1\nkind: StatefulSet\nmetadata:\n  name: pg-cobranca-x9\nspec:\n  serviceName: pg\n", "pg-cobranca-x9"},
	} {
		m := novoTeste(t)
		if out, _ := m.Mascarar(c.ensina); strings.Contains(out, c.nome) {
			t.Errorf("na posição: %q", out)
		}
		if out, _ := m.Mascarar("depois, em prosa: " + c.nome + " caiu"); strings.Contains(out, c.nome) {
			t.Errorf("não aprendeu %q: %q", c.nome, out)
		}
	}
}
