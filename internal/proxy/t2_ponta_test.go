package proxy

import (
	"regexp"
	"strings"
	"testing"
)

// T2 de ponta a ponta, com a memória da conversa de verdade: o pedido, o inventário e, numa
// mensagem nova do usuário, o mesmo nome solto em 6 formatos. Alvo: 0 em claro.

func seisFormatosP(n string) []string {
	return []string{
		"o " + n + " parou de responder hoje cedo",
		"(" + n + ", 3)",
		"['" + n + "', 'outro']",
		`{"nome": "` + n + `", "total": 3}`,
		`print(f"processando {"` + n + `"}")`,
		n + " 3",
	}
}

type areaT2P struct {
	nome    string
	usuario string
	cmd     string
	saida   string
	nomes   []string // os nomes (palavra comum) que a saída traz
	tipo    string   // o tipo esperado ("" = qualquer)
}

var areasT2P = []areaT2P{
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


func TestT2PontaAPonta(t *testing.T) {
	total, claro := 0, 0
	for _, ar := range areasT2P {
		var corpos [][]byte
		px, _ := montarComMasker(t, &corpos)
		pedido := ar.usuario
		if pedido == "" {
			pedido = "vamos ver o ambiente"
		}
		var fs []string
		for _, n := range ar.nomes {
			fs = append(fs, seisFormatosP(n)...)
		}
		enviar(t, px, []any{
			msg("user", pedido),
			msg("assistant", []any{chamadaBash("c1", ar.cmd)}),
			msg("user", []any{resultado("c1", ar.saida)}),
			msg("assistant", "Vi o inventário."),
			msg("user", strings.Join(fs, "\n")),
		})
		r := mensagens(t, corpos[len(corpos)-1])
		for _, n := range ar.nomes {
			re := regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(n) + `($|[^A-Za-z0-9_])`)
			total += 6
			if k := len(re.FindAllString(r[4], -1)); k > 0 {
				claro += k
				t.Errorf("%s: %s em claro %d vez(es) nos 6 formatos", ar.nome, n, k)
			}
			if re.MatchString(r[2]) {
				t.Errorf("%s: %s em claro no inventário", ar.nome, n)
			}
		}
	}
	t.Logf("T2: %d de %d ocorrências em claro depois do inventário", claro, total)
}
