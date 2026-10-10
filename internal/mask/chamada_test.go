package mask

import (
	"regexp"
	"strings"
	"testing"
)

// Chamada de ferramenta (chamada.go, chamada_decisor.go): proveniência,
// identidade, eco, tipo do pedido. Nomes inventados, vários de palavra comum.

// passoConversa: uma mensagem do usuário (opcional), a chamada e a saída dela.
type passoConversa struct {
	usuario string
	cmd     string
	saida   string
	trad    []PalavraTraduzida
}

// saidaMascarada: o resultado de um passo (texto mascarado, decisões, entrada mascarada).
type saidaMascarada struct {
	texto, entrada string
	dec            []Decisao
	decUso         []Decisao
}

// rodarConversa: os passos em ordem, como o proxy (dicasDosComandos + walker) faz.
func rodarConversa(t *testing.T, m *Masker, ps []passoConversa) []saidaMascarada {
	t.Helper()
	cs := NovosComandos()
	for _, p := range ps {
		cs.Argumentos(p.cmd)
	}
	var out []saidaMascarada
	for _, p := range ps {
		if p.usuario != "" {
			cs.Usuario(p.usuario)
		}
		ch := cs.Uso(p.cmd, p.trad)
		var sm saidaMascarada
		dicaUso := ComExtensao("", ch.ExtUso())
		ru, _ := m.mascararD(p.cmd, true, dicaUso)
		sm.entrada, sm.decUso = ru.texto, ru.decididos
		cs.FimTurno()
		dica := ComExtensao(cs.Dica(p.cmd, p.saida), cs.Resultado(ch, p.saida))
		r, _ := m.mascararD(p.saida, true, dica)
		sm.texto, sm.dec = r.texto, r.decididos
		out = append(out, sm)
	}
	return out
}

func palavraInteira(s, w string) bool {
	return regexp.MustCompile(`(^|[^\pL\pN_])` + regexp.QuoteMeta(w) + `($|[^\pL\pN_])`).MatchString(s)
}

// memoriaSimples: o que a memória da conversa faz com as decisões, de forma simples:
// a palavra inteira vira o pseudônimo do tipo decidido.
func memoriaSimples(m *Masker, s string, ds []Decisao) string {
	for _, d := range ds {
		if d.Generica {
			continue
		}
		re := regexp.MustCompile(`(^|[^\pL\pN_])` + regexp.QuoteMeta(d.Nome) + `($|[^\pL\pN_])`)
		ps := m.Pseudonimo(prefTipoObj+d.Ent, d.Nome)
		s = re.ReplaceAllString(s, "${1}"+ps+"${2}")
	}
	return s
}

// seisFormatos: o mesmo nome solto em 6 formatos.
func seisFormatos(n string) []string {
	return []string{
		"o " + n + " parou de responder hoje cedo",
		"(" + n + ", 3)",
		"['" + n + "', 'outro']",
		`{"nome": "` + n + `", "total": 3}`,
		`print(f"processando {"` + n + `"}")`,
		n + " 3",
	}
}

type areaT2 struct {
	nome    string
	usuario string
	cmd     string
	saida   string
	nomes   []string // os nomes (palavra comum) que a saída traz
	tipo    string   // o tipo esperado ("" = qualquer)
}

var areasT2 = []areaT2{
	{"namespaces", "", "kubectl get ns",
		"NAME          STATUS   AGE\npagamentos    Active   12d\ncobranca      Active   40d\nvitrine       Active   3d\nfaturamento   Active   7d\n",
		[]string{"pagamentos", "cobranca", "vitrine", "faturamento"}, "namespace"},
	{"grupos AD", "lista os grupos com acesso ao banco",
		`Get-ADGroup -Filter 'Name -like "*"' | Format-Table Name,GroupScope,GroupCategory`,
		"Name           GroupScope GroupCategory\n----           ---------- -------------\nTesouraria     Global     Security\nControladoria  Global     Security\nAuditores      Universal  Security\nComprasGerais  Global     Distribution\n",
		[]string{"Tesouraria", "Controladoria", "Auditores"}, ""},
	{"buckets", "", "python3 -c \"import boto3; s3 = boto3.client('s3'); [print(b['Name']) for b in s3.list_buckets()['Buckets']]\"",
		"relatorios\nbalancetes\ncontratos\nhistorico\n",
		[]string{"relatorios", "balancetes", "contratos", "historico"}, "bucket"},
	{"tabelas SHOW", "", `snow sql -q "SHOW TABLES IN SCHEMA vendas"`,
		"+------------+----------+---------+\n| name       | kind     | rows    |\n|------------+----------+---------|\n| clientes   | TABLE    | 120     |\n| pedidos    | TABLE    | 9000    |\n| entregas   | TABLE    | 800     |\n+------------+----------+---------+\n",
		[]string{"clientes", "pedidos", "entregas"}, "tabela"},
	{"tabelas information_schema", "", `psql -At -c "select table_name, table_type from information_schema.tables where table_schema = 'loja'"`,
		"estoques|BASE TABLE\nfornecedores|BASE TABLE\ndevolucoes|BASE TABLE\n",
		[]string{"estoques", "fornecedores", "devolucoes"}, "tabela"},
	{"branches", "lista as branches do repositório", "git branch -a",
		"* principal\n  conciliacao\n  remessas\n  remotes/origin/principal\n  remotes/origin/conciliacao\n  remotes/origin/remessas\n",
		[]string{"conciliacao", "remessas"}, ""},
	{"findings", "", `python3 -c "import boto3; c = boto3.client('securityhub'); [print(f['Id'], f['Severity']['Label']) for f in c.get_findings()['Findings']]"` + "\n" +
		`python3 sdk.py list_findings --detector principal`,
		"vazamento HIGH\nexposicao HIGH\npermissivo MEDIUM\nabandonado LOW\n",
		[]string{"vazamento", "exposicao", "permissivo", "abandonado"}, ""},
	{"repositórios", "", "gh repo list acme --limit 10",
		"acme/conciliador\t\tprivate\tabout 2 days ago\nacme/tesouraria\t\tprivate\tabout 1 month ago\nacme/precificador\t\tinternal\tabout 3 hours ago\n",
		[]string{"conciliador", "tesouraria", "precificador"}, "repositorio"},
	{"filas", "", "curl -s -X GET https://mq.interno.exemplo/api/queues | jq -r '.[].name'",
		"notificacoes\nconciliacoes\nreprocessamento\n",
		[]string{"notificacoes", "conciliacoes", "reprocessamento"}, ""},
}

// Para cada área, a saída do inventário é mascarada e cada nome fica decidido (sem
// Generica); com a memória da conversa (simulada aqui), o mesmo nome solto em 6 formatos fica
// 0 em claro.
func TestT2InventarioPalavraComum(t *testing.T) {
	for _, ar := range areasT2 {
		t.Run(ar.nome, func(t *testing.T) {
			m := novoTeste(t)
			r := rodarConversa(t, m, []passoConversa{{usuario: ar.usuario, cmd: ar.cmd, saida: ar.saida}})[0]
			dec := map[string]Decisao{}
			for _, d := range r.dec {
				dec[d.Nome] = d
			}
			for _, n := range ar.nomes {
				if palavraInteira(r.texto, n) {
					t.Errorf("%s em claro na saída do inventário", n)
				}
				d, ok := dec[n]
				if !ok {
					t.Errorf("%s não ficou nas decisões", n)
					continue
				}
				if d.Generica {
					t.Errorf("%s marcado como genérico", n)
				}
				if ar.tipo != "" && d.Ent != ar.tipo {
					t.Errorf("%s: tipo %q, esperado %q", n, d.Ent, ar.tipo)
				}
				for _, f := range seisFormatos(n) {
					o, _ := m.Mascarar(f)
					if o = memoriaSimples(m, o, r.dec); palavraInteira(o, n) {
						t.Errorf("%s em claro em %q", n, f)
					}
				}
			}
		})
	}
}

// O experimento do catálogo. O CSV (inventado) é lido com head e depois por um script
// Python que imprime "a | b | c" sem cabeçalho; o script traz as tags e as regex como literais.
// Colunas e tabelas saem mascaradas; tags e regex continuam legíveis.
func TestT1Catalogo(t *testing.T) {
	type linha struct{ tag, ordem, regex, tabela, coluna, tipo string }
	ls := []linha{
		{"cpf", "1", "^(cpf|nr_cpf)$", "dim_cliente", "documento", "VARCHAR"},
		{"email", "2", "^e?mail$", "dim_cliente", "contato", "VARCHAR"},
		{"nascimento", "3", "^dt_nasc", "dim_cliente", "aniversario", "DATE"},
		{"renda", "4", "renda|salario", "fato_proposta", "rendimento", "NUMBER"},
		{"limite", "5", "^limite", "fato_proposta", "teto", "NUMBER"},
		{"conta", "6", "^(nr_)?conta$", "cad_conta", "agencia", "VARCHAR"},
		{"saldo", "7", "saldo", "cad_conta", "disponivel", "NUMBER"},
		{"telefone", "8", "^(tel|fone)", "carteira", "celular", "VARCHAR"},
	}
	var csv, saida strings.Builder
	csv.WriteString("tag,ordem,regex,plataforma,tabela,coluna,tipo\n")
	for _, l := range ls {
		csv.WriteString(l.tag + "," + l.ordem + "," + l.regex + ",snowflake," + l.tabela + "," + l.coluna + "," + l.tipo + "\n")
		saida.WriteString(l.tag + " | " + l.ordem + " | " + l.regex + " | snowflake | " + l.tabela + " | " + l.coluna + " | " + l.tipo + "\n")
	}
	var script strings.Builder
	script.WriteString("python3 - <<'EOF'\nimport csv\nTAGS = [")
	for i, l := range ls {
		if i > 0 {
			script.WriteString(", ")
		}
		script.WriteString(`"` + l.tag + `"`)
	}
	script.WriteString("]\nREGEX = {\n")
	for _, l := range ls {
		script.WriteString(`    "` + l.tag + `": r"` + l.regex + `",` + "\n")
	}
	script.WriteString("}\nfor r in csv.DictReader(open('dados/catalogo.csv')):\n" +
		"    if r['tag'] in TAGS:\n" +
		"        print(f\"{r['tag']} | {r['ordem']} | {REGEX[r['tag']]} | {r['plataforma']} | {r['tabela']} | {r['coluna']} | {r['tipo']}\")\nEOF")

	m := novoTeste(t)
	rs := rodarConversa(t, m, []passoConversa{
		{cmd: "head -20 dados/catalogo.csv", saida: csv.String()},
		{cmd: script.String(), saida: saida.String()},
	})
	var dec []Decisao
	for _, r := range rs {
		dec = append(dec, r.dec...)
	}
	py := rs[1].texto
	// a parte desta trilha, sem a memória da conversa: as colunas (identidade + proveniência)
	// e as tabelas com cara de identificador (aprendidas no head)
	for _, l := range ls {
		if palavraInteira(py, l.coluna) {
			t.Errorf("coluna %s em claro na saída do script", l.coluna)
		}
		if l.tabela != "carteira" && palavraInteira(py, l.tabela) {
			t.Errorf("tabela %s em claro na saída do script", l.tabela)
		}
	}
	// com a memória da conversa (simulada): também a tabela de palavra comum
	if o := memoriaSimples(m, py, dec); palavraInteira(o, "carteira") {
		t.Errorf("tabela de palavra comum em claro com a memória da conversa")
	}
	// tags e regex: literais do programa, continuam legíveis
	for _, l := range ls {
		if !palavraInteira(py, l.tag) {
			t.Errorf("tag %s foi mascarada", l.tag)
		}
		if !strings.Contains(py, l.regex) {
			t.Errorf("regex %s foi mascarada", l.regex)
		}
	}
}

// Negativos: saídas comuns de ls, ps, git log e pip list (com a extensão da dica, como o proxy
// manda) continuam sem nada mascarado.
func TestChamadaNegativos(t *testing.T) {
	casos := []passoConversa{
		{usuario: "o build quebrou, veja o que tem na pasta", cmd: "ls -la",
			saida: "total 48\ndrwxr-xr-x  6 dev dev 4096 Oct  6 10:01 .\ndrwxr-xr-x 30 dev dev 4096 Oct  1 09:00 ..\n" +
				"drwxr-xr-x  2 dev dev 4096 Oct  6 10:01 docs\ndrwxr-xr-x  4 dev dev 4096 Oct  5 18:22 internal\n" +
				"drwxr-xr-x  2 dev dev 4096 Oct  2 11:40 scripts\n-rw-r--r--  1 dev dev 1203 Oct  6 10:01 README.md\n" +
				"-rw-r--r--  1 dev dev  311 Oct  3 08:15 Makefile\n"},
		{cmd: "ls", saida: "docs\ninternal\nscripts\nREADME.md\nMakefile\ngo.mod\n"},
		{cmd: "ps aux",
			saida: "USER         PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND\n" +
				"root           1  0.0  0.1 167432 11000 ?        Ss   Oct05   0:03 /sbin/init\n" +
				"dev         1834  0.2  1.5 912344 120000 pts/0   Sl   10:01   0:12 node server.js\n" +
				"dev         2290  0.0  0.0  10072  3300 pts/1    Ss   10:05   0:00 bash\n" +
				"dev         2301  0.0  0.0  12000  3500 pts/1    R+   10:06   0:00 ps aux\n"},
		{cmd: "git log -3",
			saida: "commit 3f2a9c1d8e7b6a5f4e3d2c1b0a9f8e7d6c5b4a3f\nAuthor: Dev <dev@exemplo.invalid>\nDate:   Mon Oct 6 10:00:00 2026 -0300\n\n" +
				"    corrige o leitor de tabela\n\ncommit 1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b\nAuthor: Dev <dev@exemplo.invalid>\nDate:   Sun Oct 5 18:00:00 2026 -0300\n\n" +
				"    ajusta testes\n"},
		{cmd: "pip list",
			saida: "Package    Version\n---------- -------\ncertifi    2024.2.2\nidna       3.6\nrequests   2.31.0\nurllib3    2.2.1\nsetuptools 69.0.3\n"},
		{cmd: "git status --short", saida: " M internal/mask/chamada.go\n M internal/proxy/requisicao.go\n?? notas.txt\n"},
	}
	for _, p := range casos {
		m := novoTeste(t)
		r := rodarConversa(t, m, []passoConversa{p})[0]
		base, _ := m.mascararD(p.saida, true, "")
		if r.texto != base.texto {
			t.Errorf("%s: as regras de chamada mascararam a mais:\n%s", p.cmd, r.texto)
		}
		for _, d := range r.dec {
			if d.Regra != "leitor" {
				t.Errorf("%s: decisão %+v", p.cmd, d)
			}
		}
	}
}

// B6: o pedido do usuário vale só para as chamadas da resposta seguinte.
func TestPedidoDoUsuarioSoNaRespostaSeguinte(t *testing.T) {
	m := novoTeste(t)
	lista := "docs\ninternal\nscripts\nferramentas\n"
	rs := rodarConversa(t, m, []passoConversa{
		{usuario: "lista os namespaces do cluster", cmd: "kubectl get ns -o name", saida: "namespace/pagamentos\nnamespace/cobranca\nnamespace/vitrine\n"},
		{cmd: "ls", saida: lista},
	})
	if palavraInteira(rs[0].texto, "cobranca") {
		t.Errorf("inventário pedido pelo usuário não mascarou")
	}
	if rs[1].texto != lista {
		t.Errorf("o pedido do usuário valeu para a chamada de outro turno: %q", rs[1].texto)
	}
	for _, f := range []string{"lista os grupos com acesso ao banco", "show me all the queues", "Liste as tabelas do schema"} {
		if _, ok := tipoNaProsa(f); !ok {
			t.Errorf("sem tipo em %q", f)
		}
	}
	for _, f := range []string{"o build quebrou", "mostra o arquivo", "quais tabelas existem?", "veja a lista"} {
		if e, ok := tipoNaProsa(f); ok {
			t.Errorf("tipo %q em %q", e, f)
		}
	}
}

// B5: o substantivo do verbo de enumeração, em qualquer convenção.
func TestTipoDoComando(t *testing.T) {
	casos := map[string]string{
		"kubectl get ns":                                   "namespace",
		"kubectl get pods -n x":                            "servico",
		"gh repo list acme":                                "repositorio",
		"python3 sdk.py list_findings":                     entGenerica,
		"Get-ADGroup -Filter *":                            "",
		"aws s3api list-buckets":                           "bucket",
		"curl -X GET https://h/api/queues":                 "fila",
		"r = requests.get('https://h/v1/queues')":          "fila",
		"SHOW TABLES IN SCHEMA x":                          "tabela",
		"select table_name from information_schema.tables": "tabela",
		"client.listBuckets()":                             "bucket",
		"aws ec2 describe-instances":                       "",
	}
	for cmd, esp := range casos {
		e, ok := tipoDoComando(cmd)
		if !ok || esp != "" && e != esp {
			t.Errorf("%q: %q %v, esperado %q", cmd, e, ok, esp)
		}
	}
	for _, cmd := range []string{"ls -la", "ps aux", "git log -3", "pip list", "git branch -a", "log = logging.getLogger(x)",
		"select nome from clientes", "cat notas.txt", "docker ps"} {
		if e, ok := tipoDoComando(cmd); ok {
			t.Errorf("%q: tipo %q", cmd, e)
		}
	}
}

// B4: a palavra de uma saída anterior que volta como argumento é nome (na entrada do tool_use).
// B1 exceção: a palavra que o proxy traduziu de um pseudônimo é nome também na saída.
func TestEcoETraduzidas(t *testing.T) {
	m := novoTeste(t)
	rs := rodarConversa(t, m, []passoConversa{
		{cmd: "cat ambientes.txt", saida: "pagamentos\ncobranca\nvitrine\n"},
		{cmd: "kubectl logs -n cobranca deploy/api --tail 20 && curl -s https://h.exemplo/api/ambientes/vitrine", saida: "iniciando cobranca\npronto\n"},
	})
	if rs[0].texto != "pagamentos\ncobranca\nvitrine\n" {
		t.Errorf("lista sem pedido mascarada: %q", rs[0].texto)
	}
	if palavraInteira(rs[1].entrada, "cobranca") {
		t.Errorf("eco não mascarado na entrada: %q", rs[1].entrada)
	}
	if palavraInteira(rs[1].entrada, "vitrine") {
		t.Errorf("eco no caminho REST não mascarado: %q", rs[1].entrada)
	}
	achou := false
	for _, d := range rs[1].decUso {
		achou = achou || d.Nome == "vitrine" && d.Regra == "eco"
	}
	if !achou {
		t.Errorf("eco não decidido: %+v", rs[1].decUso)
	}

	// o modelo escreveu o pseudônimo; o proxy traduziu; a saída traz a palavra
	m2 := novoTeste(t)
	ps := m2.Pseudonimo(prefTipoObj+"namespace", "pagamentos")
	tab := NovaTabela([]Entrada{{ps, "pagamentos", prefTipoObj + "namespace"}})
	des := m2.RegistrarResposta("kubectl logs -n "+ps+" deploy/api", tab)
	trad := m2.PalavrasTraduzidas(des)
	if len(trad) != 1 || trad[0].Nome != "pagamentos" || trad[0].Ent != "namespace" {
		t.Fatalf("palavras traduzidas: %+v", trad)
	}
	r := rodarConversa(t, m2, []passoConversa{{cmd: des, trad: trad, saida: "conectado a pagamentos\nfila vazia\n"}})[0]
	if palavraInteira(r.texto, "pagamentos") || palavraInteira(r.entrada, "pagamentos") {
		t.Errorf("palavra traduzida em claro: %q / %q", r.entrada, r.texto)
	}
	if !strings.Contains(r.texto, ps) {
		t.Errorf("deveria usar o mesmo pseudônimo (%s): %q", ps, r.texto)
	}
}

// B4: segmento de um nome já mascarado que aparece solto no mesmo texto.
func TestSegmentoDeNomeMascarado(t *testing.T) {
	m := novoTeste(t)
	r := rodarConversa(t, m, []passoConversa{{cmd: "kubectl get pods -A",
		saida: "NAMESPACE    NAME                       READY   STATUS    RESTARTS   AGE\n" +
			"tesouraria   tesouraria-api-7d9f8c6b5   1/1     Running   0          3d\n" +
			"tesouraria   tesouraria-job-5c4b3a2d1   1/1     Running   2          3d\n" +
			"vitrine      vitrine-web-6b5a4c3d2      1/1     Running   0          9d\n"}})[0]
	for _, n := range []string{"tesouraria", "vitrine", "tesouraria-api-7d9f8c6b5"} {
		if palavraInteira(r.texto, n) {
			t.Errorf("%s em claro: %s", n, r.texto)
		}
	}
}

// Os negativos da matriz como saída de comando (com a extensão da dica, sem tipo pedido):
// continuam sem nada mascarado.
func TestMatrizNegativosComoSaida(t *testing.T) {
	if testing.Short() {
		t.Skip("matriz grande")
	}
	n, ruins := 0, 0
	for _, c := range negativosMatriz {
		for _, d := range desenhosMatriz {
			if d.propagado {
				continue
			}
			for _, tr := range transportes {
				m := novoTeste(t)
				m.cfg.DominiosInternos = nil
				m.cfg.Termos = nil
				txt := tr.f(d.f(c.cols, c.rows))
				n++
				cs := NovosComandos()
				ch := cs.Uso("python3 relatorio.py", nil)
				if r, _ := m.mascararD(txt, true, ComExtensao("", cs.Resultado(ch, txt))); r.texto != txt {
					ruins++
					t.Errorf("%s / %s / %s: mascarou\n%s", c.nome, d.nome, tr.nome, r.texto)
				}
			}
		}
	}
	t.Logf("negativos como saída de comando: %d células, %d com máscara", n, ruins)
}
