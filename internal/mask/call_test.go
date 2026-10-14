package mask

import (
	"regexp"
	"strings"
	"testing"
)

// Tool call (call.go, call_decider.go): provenance, identity, echo, request type. Made-up names,
// several of them common words.

// conversationStep: a user message (optional), the call and its output.
type conversationStep struct {
	user   string
	cmd    string
	output string
	trans  []TranslatedWord
}

// maskedOutput: the result of a step (masked text, decisions, masked input).
type maskedOutput struct {
	text, entry string
	dec         []Decision
	decUsage    []Decision
}

// runConversation: the steps in order, as the proxy (commandHints + walker) does.
func runConversation(t *testing.T, m *Masker, ps []conversationStep) []maskedOutput {
	t.Helper()
	cs := NewCommands()
	for _, p := range ps {
		cs.ArgumentText(p.cmd)
	}
	var out []maskedOutput
	for _, p := range ps {
		if p.user != "" {
			cs.User(p.user)
		}
		ch := cs.Usage(p.cmd, p.trans)
		var sm maskedOutput
		usageHint := WithExtension("", ch.UsageExt())
		ru, _ := m.maskD(p.cmd, true, usageHint)
		sm.entry, sm.decUsage = ru.text, ru.decidedSet
		cs.EndTurn()
		hint := WithExtension(cs.Hint(p.cmd, p.output), cs.Result(ch, p.output))
		r, _ := m.maskD(p.output, true, hint)
		sm.text, sm.dec = r.text, r.decidedSet
		out = append(out, sm)
	}
	return out
}

func wholeWord(s, w string) bool {
	return regexp.MustCompile(`(^|[^\pL\pN_])` + regexp.QuoteMeta(w) + `($|[^\pL\pN_])`).MatchString(s)
}

// simpleMemory: what the conversation memory does with the decisions, in a simple way: the whole
// word becomes the pseudonym of the decided type.
func simpleMemory(m *Masker, s string, ds []Decision) string {
	for _, d := range ds {
		if d.Generic {
			continue
		}
		re := regexp.MustCompile(`(^|[^\pL\pN_])` + regexp.QuoteMeta(d.Name) + `($|[^\pL\pN_])`)
		ps := m.Pseudonym(objTypePrefix+d.Ent, d.Name)
		s = re.ReplaceAllString(s, "${1}"+ps+"${2}")
	}
	return s
}

// sixFormats: the same loose name in 6 formats.
func sixFormats(n string) []string {
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
	name   string
	user   string
	cmd    string
	output string
	names  []string // the names (common words) the output brings
	typ    string   // o tipo esperado ("" = qualquer)
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

// For each area, the inventory output is masked and each name is decided (without Generic); with
// the conversation memory (simulated here), the same loose name in 6 formats is 0 in the clear.
func TestT2InventoryCommonWord(t *testing.T) {
	for _, ar := range areasT2 {
		t.Run(ar.name, func(t *testing.T) {
			m := newTest(t)
			r := runConversation(t, m, []conversationStep{{user: ar.user, cmd: ar.cmd, output: ar.output}})[0]
			dec := map[string]Decision{}
			for _, d := range r.dec {
				dec[d.Name] = d
			}
			for _, n := range ar.names {
				if wholeWord(r.text, n) {
					t.Errorf("%s em claro na saída do inventário", n)
				}
				d, ok := dec[n]
				if !ok {
					t.Errorf("%s não ficou nas decisões", n)
					continue
				}
				if d.Generic {
					t.Errorf("%s marcado como genérico", n)
				}
				if ar.typ != "" && d.Ent != ar.typ {
					t.Errorf("%s: tipo %q, esperado %q", n, d.Ent, ar.typ)
				}
				for _, f := range sixFormats(n) {
					o, _ := m.Mask(f)
					if o = simpleMemory(m, o, r.dec); wholeWord(o, n) {
						t.Errorf("%s em claro em %q", n, f)
					}
				}
			}
		})
	}
}

// The catalog experiment. The (made-up) CSV is read with head and then by a Python script that
// prints "a | b | c" without a header; the script brings the tags and the regexes as literals.
// Columns and tables go out masked; tags and regexes stay readable.
func TestT1Catalog(t *testing.T) {
	type line struct{ tag, order, regex, table, column, typ string }
	ls := []line{
		{"cpf", "1", "^(cpf|nr_cpf)$", "dim_cliente", "documento", "VARCHAR"},
		{"email", "2", "^e?mail$", "dim_cliente", "contato", "VARCHAR"},
		{"nascimento", "3", "^dt_nasc", "dim_cliente", "aniversario", "DATE"},
		{"renda", "4", "renda|salario", "fato_proposta", "rendimento", "NUMBER"},
		{"limite", "5", "^limite", "fato_proposta", "teto", "NUMBER"},
		{"conta", "6", "^(nr_)?conta$", "cad_conta", "agencia", "VARCHAR"},
		{"saldo", "7", "saldo", "cad_conta", "disponivel", "NUMBER"},
		{"telefone", "8", "^(tel|fone)", "carteira", "celular", "VARCHAR"},
	}
	var csv, output strings.Builder
	csv.WriteString("tag,ordem,regex,plataforma,tabela,coluna,tipo\n")
	for _, l := range ls {
		csv.WriteString(l.tag + "," + l.order + "," + l.regex + ",snowflake," + l.table + "," + l.column + "," + l.typ + "\n")
		output.WriteString(l.tag + " | " + l.order + " | " + l.regex + " | snowflake | " + l.table + " | " + l.column + " | " + l.typ + "\n")
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

	m := newTest(t)
	rs := runConversation(t, m, []conversationStep{
		{cmd: "head -20 dados/catalogo.csv", output: csv.String()},
		{cmd: script.String(), output: output.String()},
	})
	var dec []Decision
	for _, r := range rs {
		dec = append(dec, r.dec...)
	}
	py := rs[1].text
	// this track's part, without the conversation memory: the columns (identity + provenance) and
	// the identifier-shaped tables (learned in the head)
	for _, l := range ls {
		if wholeWord(py, l.column) {
			t.Errorf("coluna %s em claro na saída do script", l.column)
		}
		if l.table != "carteira" && wholeWord(py, l.table) {
			t.Errorf("tabela %s em claro na saída do script", l.table)
		}
	}
	// with the conversation memory (simulated): also the common-word table
	if o := simpleMemory(m, py, dec); wholeWord(o, "carteira") {
		t.Errorf("tabela de palavra comum em claro com a memória da conversa")
	}
	// tags and regexes: program literals, stay readable
	for _, l := range ls {
		if !wholeWord(py, l.tag) {
			t.Errorf("tag %s foi mascarada", l.tag)
		}
		if !strings.Contains(py, l.regex) {
			t.Errorf("regex %s foi mascarada", l.regex)
		}
	}
}

// Negatives: common outputs of ls, ps, git log and pip list (with the hint extension, as the proxy
// sends it) stay with nothing masked.
func TestCallNegatives(t *testing.T) {
	cases := []conversationStep{
		{user: "o build quebrou, veja o que tem na pasta", cmd: "ls -la",
			output: "total 48\ndrwxr-xr-x  6 dev dev 4096 Oct  6 10:01 .\ndrwxr-xr-x 30 dev dev 4096 Oct  1 09:00 ..\n" +
				"drwxr-xr-x  2 dev dev 4096 Oct  6 10:01 docs\ndrwxr-xr-x  4 dev dev 4096 Oct  5 18:22 internal\n" +
				"drwxr-xr-x  2 dev dev 4096 Oct  2 11:40 scripts\n-rw-r--r--  1 dev dev 1203 Oct  6 10:01 README.md\n" +
				"-rw-r--r--  1 dev dev  311 Oct  3 08:15 Makefile\n"},
		{cmd: "ls", output: "docs\ninternal\nscripts\nREADME.md\nMakefile\ngo.mod\n"},
		{cmd: "ps aux",
			output: "USER         PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND\n" +
				"root           1  0.0  0.1 167432 11000 ?        Ss   Oct05   0:03 /sbin/init\n" +
				"dev         1834  0.2  1.5 912344 120000 pts/0   Sl   10:01   0:12 node server.js\n" +
				"dev         2290  0.0  0.0  10072  3300 pts/1    Ss   10:05   0:00 bash\n" +
				"dev         2301  0.0  0.0  12000  3500 pts/1    R+   10:06   0:00 ps aux\n"},
		{cmd: "git log -3",
			output: "commit 3f2a9c1d8e7b6a5f4e3d2c1b0a9f8e7d6c5b4a3f\nAuthor: Dev <dev@exemplo.invalid>\nDate:   Mon Oct 6 10:00:00 2026 -0300\n\n" +
				"    corrige o leitor de tabela\n\ncommit 1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b\nAuthor: Dev <dev@exemplo.invalid>\nDate:   Sun Oct 5 18:00:00 2026 -0300\n\n" +
				"    ajusta testes\n"},
		{cmd: "pip list",
			output: "Package    Version\n---------- -------\ncertifi    2024.2.2\nidna       3.6\nrequests   2.31.0\nurllib3    2.2.1\nsetuptools 69.0.3\n"},
		{cmd: "git status --short", output: " M internal/mask/call.go\n M internal/proxy/request.go\n?? notas.txt\n"},
	}
	for _, p := range cases {
		m := newTest(t)
		r := runConversation(t, m, []conversationStep{p})[0]
		base, _ := m.maskD(p.output, true, "")
		if r.text != base.text {
			t.Errorf("%s: as regras de chamada mascararam a mais:\n%s", p.cmd, r.text)
		}
		for _, d := range r.dec {
			if d.Rule != "leitor" {
				t.Errorf("%s: decisão %+v", p.cmd, d)
			}
		}
	}
}

// B6: the user request only applies to the calls of the next response.
func TestUserRequestOnlyInNextResponse(t *testing.T) {
	m := newTest(t)
	list := "docs\ninternal\nscripts\nferramentas\n"
	rs := runConversation(t, m, []conversationStep{
		{user: "lista os namespaces do cluster", cmd: "kubectl get ns -o name", output: "namespace/pagamentos\nnamespace/cobranca\nnamespace/vitrine\n"},
		{cmd: "ls", output: list},
	})
	if wholeWord(rs[0].text, "cobranca") {
		t.Errorf("inventário pedido pelo usuário não mascarou")
	}
	if rs[1].text != list {
		t.Errorf("o pedido do usuário valeu para a chamada de outro turno: %q", rs[1].text)
	}
	for _, f := range []string{"lista os grupos com acesso ao banco", "show me all the queues", "Liste as tabelas do schema"} {
		if _, ok := typeInProse(f); !ok {
			t.Errorf("sem tipo em %q", f)
		}
	}
	for _, f := range []string{"o build quebrou", "mostra o arquivo", "quais tabelas existem?", "veja a lista"} {
		if e, ok := typeInProse(f); ok {
			t.Errorf("tipo %q em %q", e, f)
		}
	}
}

// B5: the noun of the enumeration verb, in any convention.
func TestCommandType(t *testing.T) {
	cases := map[string]string{
		"kubectl get ns":                                   "namespace",
		"kubectl get pods -n x":                            "servico",
		"gh repo list acme":                                "repositorio",
		"python3 sdk.py list_findings":                     genericEnt,
		"Get-ADGroup -Filter *":                            "",
		"aws s3api list-buckets":                           "bucket",
		"curl -X GET https://h/api/queues":                 "fila",
		"r = requests.get('https://h/v1/queues')":          "fila",
		"SHOW TABLES IN SCHEMA x":                          "tabela",
		"select table_name from information_schema.tables": "tabela",
		"client.listBuckets()":                             "bucket",
		"aws ec2 describe-instances":                       "",
	}
	for cmd, spc := range cases {
		e, ok := commandType(cmd)
		if !ok || spc != "" && e != spc {
			t.Errorf("%q: %q %v, esperado %q", cmd, e, ok, spc)
		}
	}
	for _, cmd := range []string{"ls -la", "ps aux", "git log -3", "pip list", "git branch -a", "log = logging.getLogger(x)",
		"select nome from clientes", "cat notas.txt", "docker ps"} {
		if e, ok := commandType(cmd); ok {
			t.Errorf("%q: tipo %q", cmd, e)
		}
	}
}

// B4: a word of a previous output that comes back as an argument is a name (in the tool_use input).
// B1 exception: a word the proxy translated from a pseudonym is a name in the output too.
func TestEchoAndTranslated(t *testing.T) {
	m := newTest(t)
	rs := runConversation(t, m, []conversationStep{
		{cmd: "cat ambientes.txt", output: "pagamentos\ncobranca\nvitrine\n"},
		{cmd: "kubectl logs -n cobranca deploy/api --tail 20 && curl -s https://h.exemplo/api/ambientes/vitrine", output: "iniciando cobranca\npronto\n"},
	})
	if rs[0].text != "pagamentos\ncobranca\nvitrine\n" {
		t.Errorf("lista sem pedido mascarada: %q", rs[0].text)
	}
	if wholeWord(rs[1].entry, "cobranca") {
		t.Errorf("eco não mascarado na entrada: %q", rs[1].entry)
	}
	if wholeWord(rs[1].entry, "vitrine") {
		t.Errorf("eco no caminho REST não mascarado: %q", rs[1].entry)
	}
	foundIt := false
	for _, d := range rs[1].decUsage {
		foundIt = foundIt || d.Name == "vitrine" && d.Rule == "eco"
	}
	if !foundIt {
		t.Errorf("eco não decidido: %+v", rs[1].decUsage)
	}

	// the model wrote the pseudonym; the proxy translated it; the output brings the word
	m2 := newTest(t)
	ps := m2.Pseudonym(objTypePrefix+"namespace", "pagamentos")
	tab := NewTable([]Entry{{ps, "pagamentos", objTypePrefix + "namespace"}})
	des := m2.RecordResponse("kubectl logs -n "+ps+" deploy/api", tab)
	trans := m2.TranslatedWords(des)
	if len(trans) != 1 || trans[0].Name != "pagamentos" || trans[0].Ent != "namespace" {
		t.Fatalf("palavras traduzidas: %+v", trans)
	}
	r := runConversation(t, m2, []conversationStep{{cmd: des, trans: trans, output: "conectado a pagamentos\nfila vazia\n"}})[0]
	if wholeWord(r.text, "pagamentos") || wholeWord(r.entry, "pagamentos") {
		t.Errorf("palavra traduzida em claro: %q / %q", r.entry, r.text)
	}
	if !strings.Contains(r.text, ps) {
		t.Errorf("deveria usar o mesmo pseudônimo (%s): %q", ps, r.text)
	}
}

// B4: a segment of an already masked name that appears loose in the same text.
func TestNameSegmentMasked(t *testing.T) {
	m := newTest(t)
	r := runConversation(t, m, []conversationStep{{cmd: "kubectl get pods -A",
		output: "NAMESPACE    NAME                       READY   STATUS    RESTARTS   AGE\n" +
			"tesouraria   tesouraria-api-7d9f8c6b5   1/1     Running   0          3d\n" +
			"tesouraria   tesouraria-job-5c4b3a2d1   1/1     Running   2          3d\n" +
			"vitrine      vitrine-web-6b5a4c3d2      1/1     Running   0          9d\n"}})[0]
	for _, n := range []string{"tesouraria", "vitrine", "tesouraria-api-7d9f8c6b5"} {
		if wholeWord(r.text, n) {
			t.Errorf("%s em claro: %s", n, r.text)
		}
	}
}

// The matrix negatives as command output (with the hint extension, without a requested type): stay
// with nothing masked.
func TestMatrixNegativesAsOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("matriz grande")
	}
	n, bad := 0, 0
	for _, c := range matrixNegatives {
		for _, d := range matrixLayouts {
			if d.propagated {
				continue
			}
			for _, tr := range transports {
				m := newTest(t)
				m.cfg.InternalDomains = nil
				m.cfg.Terms = nil
				txt := tr.f(d.f(c.cols, c.rows))
				n++
				cs := NewCommands()
				ch := cs.Usage("python3 relatorio.py", nil)
				if r, _ := m.maskD(txt, true, WithExtension("", cs.Result(ch, txt))); r.text != txt {
					bad++
					t.Errorf("%s / %s / %s: mascarou\n%s", c.name, d.name, tr.name, r.text)
				}
			}
		}
	}
	t.Logf("negativos como saída de comando: %d células, %d com máscara", n, bad)
}
