package mask

import (
	"strings"
	"testing"
)

// Saídas soltas de script e de shell. Os nomes são fictícios.

// mostrar: o texto mascarado com cada pseudônimo trocado por «tipo» (para ler a falha).
func show(m *Masker, s string) string {
	out, ents := m.Mask(s)
	return showOut(out, ents)
}

func showOut(out string, ents []Entry) string {
	for _, e := range ents {
		out = strings.ReplaceAll(out, e.Pseudo, "«"+strings.TrimPrefix(e.Type, "obj.")+"»")
	}
	return out
}

// passo: uma chamada de ferramenta da conversa (o comando e o que ele devolveu).
type step struct{ cmd, output string }

// checkCommand: mascara a saída do último passo com a dica do seu comando (os passos antes
// dele passam pelos Commands, como no proxy) e conta os nomes mascarados.
func checkCommand(t *testing.T, m *Masker, label string, ps []step, names []string) (ok, tot int, out, seen string) {
	t.Helper()
	cs := NewCommands()
	var hint string
	for _, p := range ps {
		hint = cs.Hint(p.cmd, p.output)
	}
	lastPos := ps[len(ps)-1].output
	out, ents := m.NewBatch().MaskHint(lastPos, false, Position{1}, hint)
	for _, n := range names {
		a, b := strings.Count(lastPos, n), strings.Count(out, n)
		tot += a
		ok += a - b
	}
	if back := NewTable(ents).Unmask(out, false); back != lastPos {
		t.Errorf("%s: a volta não reproduz o original", label)
	}
	return ok, tot, out, showOut(out, ents)
}

var loadHdr = step{"head -3 dados/carga_contab.csv", "cd_conta,nm_tabela,nm_schema\n1001,t_lanc_diario,fin_contab\n1002,t_saldo_mes,fin_fiscal\n"}

// O comando tipa as colunas do resultado
var commandCases = []struct {
	name  string
	ps    []step
	names []string
}{
	{"psql -At (sem cabeçalho)", []step{{`psql -h srv-dw -At -c "select table_schema, table_name from information_schema.tables where table_type = 'BASE TABLE'"`,
		"fin_contab|t_lanc_diario\nfin_contab|t_saldo_mes\nfin_fiscal|t_nota_entrada\n"}},
		[]string{"fin_contab", "fin_fiscal", "t_lanc_diario", "t_saldo_mes", "t_nota_entrada"}},
	{"alias no SELECT", []step{{`snow sql -q "select table_schema as s, table_name as t from dwprd.information_schema.tables"`,
		"+------------+----------------+\n| S          | T              |\n|------------+----------------|\n| FIN_CONTAB | T_LANC_DIARIO  |\n| FIN_FISCAL | T_NOTA_ENTRADA |\n+------------+----------------+\n"}},
		[]string{"FIN_CONTAB", "FIN_FISCAL", "T_LANC_DIARIO", "T_NOTA_ENTRADA"}},
	{"name do catálogo (sqlcmd)", []step{{`sqlcmd -S srv-erp -Q "SET NOCOUNT OFF; select name from sys.tables order by name"`,
		"name\n--------------------\nt_lanc_diario\nt_saldo_mes\n\n(2 rows affected)\n"}},
		[]string{"t_lanc_diario", "t_saldo_mes"}},
	{"fetchall sem cabeçalho", []step{{`python -c "cur.execute('select table_schema, table_name from information_schema.tables'); print(cur.fetchall())"`,
		"[('fin_contab', 't_lanc_diario'), ('fin_fiscal', 't_nota_entrada')]\n"}},
		[]string{"fin_contab", "fin_fiscal", "t_lanc_diario", "t_nota_entrada"}},
	{"SHOW SCHEMAS", []step{{`snow sql -q "show schemas in database dwprd"`,
		"created_on                    | name        | is_default | database_name | owner\n------------------------------+-------------+------------+---------------+---------\n2026-01-02 10:00:00.000 -0300 | fin_contab  | N          | dwprd         | sysadmin\n2026-01-02 10:00:00.000 -0300 | fin_fiscal  | N          | dwprd         | sysadmin\n"}},
		[]string{"fin_contab", "fin_fiscal"}},
	{"cut sobre arquivo conhecido", []step{loadHdr, {"cut -d, -f2 dados/carga_contab.csv | tail -n +2 | sort -u", "t_lanc_diario\nt_saldo_mes\n"}},
		[]string{"t_lanc_diario", "t_saldo_mes"}},
	{"awk | sort | uniq -c", []step{loadHdr, {"awk -F, 'NR>1 {print $3}' dados/carga_contab.csv | sort | uniq -c", "      1 fin_contab\n      1 fin_fiscal\n"}},
		[]string{"fin_contab", "fin_fiscal"}},
	{"cut depois do Read", []step{{"/home/ana/proj/dados/carga_contab.csv", "     1→cd_conta;nm_tabela;nm_schema\n     2→1001;t_lanc_diario;fin_contab\n"},
		{"cut -d';' -f2,3 /home/ana/proj/dados/carga_contab.csv | tail -n +2", "t_lanc_diario;fin_contab\nt_saldo_mes;fin_fiscal\n"}},
		[]string{"t_lanc_diario", "t_saldo_mes", "fin_contab", "fin_fiscal"}},
	{"kubectl get pods", []step{{"kubectl get pods -n ns-fin", "NAME                                READY   STATUS    RESTARTS   AGE\nsvc-cobranca-lote-7d9f8c6b5-x2x9k   1/1     Running   0          2d\nsvc-conciliacao-6c8d7b9f4-k8j2m     1/1     Running   3          5d\n"}},
		[]string{"svc-cobranca-lote-7d9f8c6b5-x2x9k", "svc-conciliacao-6c8d7b9f4-k8j2m"}},
	{"helm list", []step{{"helm list -n ns-fin", "NAME            \tNAMESPACE    \tREVISION\tUPDATED                                \tSTATUS  \tCHART          \tAPP VERSION\n" +
		"cobranca-lote   \tns-fin       \t4       \t2026-10-01 10:00:00.123 -0300 -03\tdeployed\tcobranca-1.2.0\t1.2.0\n"}},
		[]string{"cobranca-lote"}},
	{"docker ps", []step{{"docker ps --format 'table {{.ID}}\\t{{.Status}}\\t{{.Names}}'", "CONTAINER ID   STATUS          NAMES\n3f2a1b0c9d8e   Up 2 hours      conciliacao_worker_1\n9e8d7c6b5a4f   Up 2 hours      cobranca_api_1\n"}},
		[]string{"conciliacao_worker_1", "cobranca_api_1"}},
	{"<tipo> list", []step{{"gcloud sql instances list --project prj-fin", "NAME            DATABASE_VERSION  LOCATION       TIER\npg-fin-prd01    POSTGRES_15       us-east1-b     db-custom-2\npg-fin-hml01    POSTGRES_15       us-east1-b     db-custom-2\n"}},
		[]string{"pg-fin-prd01", "pg-fin-hml01"}},
}

func TestCommandTypesOutput(t *testing.T) {
	for _, c := range commandCases {
		m := newTest(t)
		ok, tot, _, seen := checkCommand(t, m, c.name, c.ps, c.names)
		if tot == 0 || ok != tot {
			t.Errorf("%s: %d/%d\n%s", c.name, ok, tot, seen)
		}
		// sem o comando, a mesma saída não basta: é a dica que tipa
		m2 := newTest(t)
		lastPos := c.ps[len(c.ps)-1].output
		o2, _ := m2.Mask(lastPos)
		without := 0
		for _, n := range c.names {
			without += strings.Count(lastPos, n) - strings.Count(o2, n)
		}
		t.Logf("%s: %d/%d com o comando, %d sem", c.name, ok, tot, without)
		if without == tot {
			t.Errorf("%s: sem o comando já mascarava tudo (o caso não testa a dica)", c.name)
		}
	}
}

// a dica vem do comando, não do texto: a mesma saída com outro comando não muda
func TestCommandWithoutHintDoesNotType(t *testing.T) {
	output := "t_lanc_diario\nt_saldo_mes\nt_nota_entrada\n"
	for _, cmd := range []string{"cat notas.txt", "echo ok", ""} {
		m := newTest(t)
		_, _, out, seen := checkCommand(t, m, cmd, []step{{cmd, output}}, nil)
		if out != output {
			t.Errorf("%q: mascarou sem dica\n%s", cmd, seen)
		}
	}
}

// listagens comuns e consultas sem coluna de nome: a dica não acrescenta nada (o que os outros
// leitores já mascaram nessas saídas não vem ao caso aqui)
func TestCommandNegatives(t *testing.T) {
	cases := []step{
		{"ls -la", "total 16\ndrwxr-xr-x 2 ana ana 4096 Oct  5 10:11 relatorio_mensal\n-rw-r--r-- 1 ana ana  220 Oct  5 10:11 notas_reuniao.txt\n"},
		{"ls", "build_cache\nnotas_reuniao.txt\nrelatorio_mensal\n"},
		{"ps aux | grep python", "ana  412  0.0  0.1 25300 7216 pts/0 S+ 10:11 0:00 python carga_diaria.py\n"},
		{"git log --oneline", "a1b2c3d fix: ajusta leitura do cabecalho\ne4f5a6b feat: novo relatorio_mensal\n"},
		{"pip list", "Package            Version\n------------------ -----------\ncharset-normalizer 3.3.2\npython-dateutil    2.9.0.post0\ntyping_extensions  4.12.2\n"},
		{"docker network ls", "NETWORK ID     NAME      DRIVER    SCOPE\n1a2b3c4d5e6f   bridge    bridge    local\n2b3c4d5e6f7a   host      host      local\n"},
		{"kubectl get ns", "NAME              STATUS   AGE\ndefault           Active   90d\nkube-system       Active   90d\nkube-public       Active   90d\nkube-node-lease   Active   90d\n"},
		{"conda env list", "# conda environments:\n#\nbase                  *  /opt/conda\nanalise_dados            /opt/conda/envs/analise_dados\n"},
		{"npm ls", "app@1.0.0 /home/ana/app\n├── express@4.19.2\n└── lodash@4.17.21\n"},
		{"systemctl list-units --type=service", "  UNIT                 LOAD   ACTIVE SUB     DESCRIPTION\n  ssh.service          loaded active running OpenBSD Secure Shell server\n  cron.service         loaded active running Regular background program processing daemon\n"},
		{`psql -At -c "select count(*), max(dt_carga) from fin_contab.t_lanc_diario"`, "1200|2026-10-05\n"},
		{`psql -c "select status, count(*) from cargas group by status"`, " status | count\n--------+-------\n ok     |  1180\n erro   |    20\n(2 rows)\n"},
		{"cut -d: -f1 /etc/passwd", "root\ndaemon\nbin\n"},
	}
	for _, c := range cases {
		for _, tr := range transports {
			m, without := newTest(t), newTest(t)
			txt := tr.f(c.output)
			_, _, out, seen := checkCommand(t, m, c.cmd, []step{{c.cmd, txt}}, nil)
			if o2, _ := without.Mask(txt); out != o2 {
				t.Errorf("%s / %s: a dica mascarou\n%s", c.cmd, tr.name, seen)
			}
		}
	}
}

func TestCommandHint(t *testing.T) {
	cs := NewCommands()
	for _, c := range []struct{ cmd, wants string }{
		{`psql -c "select table_schema, table_name from information_schema.tables"`, "F|schema,tabela|table_name:tabela,table_schema:schema|"},
		{`snow sql -q "SHOW TABLES IN SCHEMA fin"`, "F||name:tabela|tabela"},
		{"kubectl get svc -A", "f||name:servico,names:servico|servico"},
		{"aws s3api list-buckets --output table", "f||name:bucket,names:bucket|bucket"},
		{"ls -la", ""},
		{"ps aux", ""},

		{"git log --oneline", ""},
		{`psql -c "select * from information_schema.tables"`, ""},
	} {
		if d := cs.Hint(c.cmd, ""); d != c.wants {
			t.Errorf("%q: dica %q, queria %q", c.cmd, d, c.wants)
		}
	}
}

// Name qualificado depois de palavra de tipo, em qualquer frase
var qualifiedTypeCases = []struct{ name, text string }{
	{"rótulo pt", "tabela fin_contab.t_lanc_diario: 1200 linhas\n"},
	{"dbt", "10:01:02  1 of 3 OK created sql table model fin_contab.t_lanc_diario ....... [SUCCESS 1 in 2.10s]\n"},
	{"loading", "Loading table dwprd_fin.fin_contab.t_lanc_diario\n"},
	{"erro citado", "Object 'DWPRD_FIN.FIN_CONTAB.T_LANC_DIARIO' does not exist or not authorized.\n"},
	{"conexão pt", "conectando em srv-dw-fin01:5432/dwprd_fin como svc_carga_fin\n"},
	{"conexão en", "connecting to srv-dw-fin01:5432/dwprd_fin as svc_carga_fin\n"},
}

func TestQualifiedType(t *testing.T) {
	names := []string{"fin_contab", "t_lanc_diario", "dwprd_fin", "FIN_CONTAB", "T_LANC_DIARIO", "DWPRD_FIN", "srv-dw-fin01", "svc_carga_fin"}
	for _, c := range qualifiedTypeCases {
		for _, tr := range transports {
			m := newTest(t)
			txt := tr.f(c.text)
			ok, tot := checkMasked(t, m, c.name+"/"+tr.name, txt, names)
			if tot == 0 || ok != tot {
				t.Errorf("%s / %s: %d/%d\n%s", c.name, tr.name, ok, tot, show(m, txt))
			}
		}
	}
	// o tipo de cada parte
	m := newTest(t)
	s := "Loading table dwprd_fin.fin_contab.t_lanc_diario\n"
	for real, wants := range map[string]string{"dwprd_fin": "obj.database", "fin_contab": "obj.schema", "t_lanc_diario": "obj.tabela"} {
		if tp := typeOf(m, s, real); tp != wants {
			t.Errorf("%s: tipo %q, queria %q", real, tp, wants)
		}
	}
}

func TestQualifiedTypeNegatives(t *testing.T) {
	for _, s := range []string{
		"see the table in os.path for details\n",
		"a tabela vendas.csv foi carregada\n",
		"the schema is documented at docs.python.org\n",
		"connecting to localhost:8080/api\n",
		"table of contents: intro.md\n",
		"a tabela foi carregada em 2.5 segundos\n",
		"index i.e. the first one\n",
	} {
		m := newTest(t)
		m.cfg.InternalDomains = nil
		if out, _ := m.Mask(s); out != s {
			t.Errorf("mascarou %q\n-> %q", s, show(m, s))
		}
	}
}

// Lista homogênea; os nomes ensinados antes por um SQL
const sqlTeaches = "select * from fin_contab.t_lanc_diario d join fin_contab.t_saldo_mes s on s.cd = d.cd join fin_contab.t_plano_contas p on p.cd = d.cd\n"

var listCases = []struct{ name, text string }{
	{"coluna solta", "t_lanc_diario\nt_saldo_mes\nt_razao_aux\n"},
	{"marcadores", "- t_lanc_diario\n- t_saldo_mes\n- t_razao_aux\n"},
	{"sort | uniq -c", "     12 t_lanc_diario\n      7 t_saldo_mes\n      3 t_razao_aux\n"},
	{"value_counts", "t_lanc_diario    12\nt_saldo_mes       7\nt_razao_aux       3\n"},
	{"laço com ==", "== t_lanc_diario ==\nlinhas: 10\n== t_saldo_mes ==\nlinhas: 3\n== t_razao_aux ==\nlinhas: 7\n"},
	{"vírgulas", "tabelas carregadas: t_lanc_diario, t_saldo_mes, t_razao_aux\n"},
	{"json.dumps", `["t_lanc_diario", "t_saldo_mes", "t_razao_aux", "t_centro_custo"]` + "\n"},
	{"qualificado com schema conhecido", "copiado para fin_contab.t_razao_aux ontem\n"},
}

func TestHomogeneousList(t *testing.T) {
	for _, c := range listCases {
		for _, tr := range transports {
			m := newTest(t)
			m.Mask(sqlTeaches)
			txt := tr.f(c.text)
			ok, tot := checkMasked(t, m, c.name+"/"+tr.name, txt, []string{"t_razao_aux", "t_centro_custo"})
			if tot == 0 || ok != tot {
				t.Errorf("%s / %s: %d/%d\n%s", c.name, tr.name, ok, tot, show(m, txt))
			}
		}
	}
	// o item novo é aprendido: depois aparece sozinho e é mascarado
	m := newTest(t)
	m.Mask(sqlTeaches)
	m.Mask("t_lanc_diario\nt_saldo_mes\nt_razao_aux\n")
	if out, _ := m.Mask("a t_razao_aux tem 3 linhas"); strings.Contains(out, "t_razao_aux") {
		t.Errorf("item da lista não foi aprendido: %s", out)
	}
	if tp := typeOf(m, "t_razao_aux", "t_razao_aux"); tp != "obj.tabela" {
		t.Errorf("tipo %q", tp)
	}
}

func TestHomogeneousListNegatives(t *testing.T) {
	for _, s := range []string{
		"t_lanc_diario\nfoo_bar\nbaz_qux\nqux_1\n",                    // menos da metade conhecida
		"carregar(t_lanc_diario, t_saldo_mes, x_train)\n",             // argumentos de função
		"t_lanc_diario\nt_saldo_mes\nresumo\npronto\n",                // sem cara de identificador
		"t_lanc_diario, t_saldo_mes, relatorio_final.pdf\n",           // arquivo
		"      12 build_cache\n       7 notas_reuniao\n       3 ok\n", // nada conhecido
		"fin_contab.read_csv(arquivo)\n",                              // chamada
	} {
		m := newTest(t)
		m.cfg.InternalDomains = nil
		m.Mask(sqlTeaches)
		_, ents := m.Mask(s)
		for _, e := range ents {
			for _, stays := range []string{"foo_bar", "baz_qux", "qux_1", "x_train", "resumo", "pronto", "relatorio_final", "build_cache", "notas_reuniao", "read_csv"} {
				if e.Real == stays {
					t.Errorf("%q: mascarou %s\n-> %q", s, stays, show(m, s))
				}
			}
		}
	}
}

// Um nome aprendido é mascarado dentro de qualquer forma solta
func TestPropagationLooseForms(t *testing.T) {
	shapes := []struct{ name, text string }{
		{"f-string", `print(f"carregando fin_contab.t_lanc_diario em {destino}")` + "\n" + `log.info(f"t_saldo_mes: {n} linhas")` + "\n"},
		{"log", "2026-10-05T10:00:00Z INFO job=carga tabela_alvo=t_lanc_diario status=ok\n"},
		{"célula de planilha", "B2=t_lanc_diario\nC2=t_saldo_mes\n"},
		{"echo com rótulo", "Tabela atual -> t_lanc_diario\nprocessando t_saldo_mes...\n"},
		{"texto de docx/pdf", "A tabela t_lanc_diario é atualizada diariamente a partir da t_saldo_mes, no schema fin_contab.\n"},
		{"kubectl logs", "I1005 10:00:00.123 main.go:42] gravando 1200 linhas em fin_contab.t_lanc_diario\n"},
	}
	for _, f := range shapes {
		for _, tr := range transports {
			m := newTest(t)
			m.Mask(sqlTeaches)
			txt := tr.f(f.text)
			ok, tot := checkMasked(t, m, f.name+"/"+tr.name, txt, []string{"t_lanc_diario", "t_saldo_mes", "fin_contab"})
			if tot == 0 || ok != tot {
				t.Errorf("%s / %s: %d/%d\n%s", f.name, tr.name, ok, tot, show(m, txt))
			}
		}
	}
}
