package mask

import (
	"strings"
	"testing"
)

// Seção F: saídas soltas de script e de shell. Os nomes são fictícios.

// mostrar: o texto mascarado com cada pseudônimo trocado por «tipo» (para ler a falha).
func mostrar(m *Masker, s string) string {
	out, ents := m.Mascarar(s)
	return mostrarOut(out, ents)
}

func mostrarOut(out string, ents []Entrada) string {
	for _, e := range ents {
		out = strings.ReplaceAll(out, e.Pseudo, "«"+strings.TrimPrefix(e.Tipo, "obj.")+"»")
	}
	return out
}

// passo: uma chamada de ferramenta da conversa (o comando e o que ele devolveu).
type passo struct{ cmd, saida string }

// conferirComando: mascara a saída do último passo com a dica do seu comando (os passos antes
// dele passam pelos Comandos, como no proxy) e conta os nomes mascarados.
func conferirComando(t *testing.T, m *Masker, rotulo string, ps []passo, nomes []string) (ok, tot int, out, visto string) {
	t.Helper()
	cs := NovosComandos()
	var dica string
	for _, p := range ps {
		dica = cs.Dica(p.cmd, p.saida)
	}
	ult := ps[len(ps)-1].saida
	out, ents := m.NovoLote().MascararDica(ult, false, Posicao{1}, dica)
	for _, n := range nomes {
		a, b := strings.Count(ult, n), strings.Count(out, n)
		tot += a
		ok += a - b
	}
	if volta := NovaTabela(ents).Desmascarar(out, false); volta != ult {
		t.Errorf("%s: a volta não reproduz o original", rotulo)
	}
	return ok, tot, out, mostrarOut(out, ents)
}

var cabCarga = passo{"head -3 dados/carga_contab.csv", "cd_conta,nm_tabela,nm_schema\n1001,t_lanc_diario,fin_contab\n1002,t_saldo_mes,fin_fiscal\n"}

// item 24: o comando tipa as colunas do resultado
var casosComando = []struct {
	nome  string
	ps    []passo
	nomes []string
}{
	{"psql -At (sem cabeçalho)", []passo{{`psql -h srv-dw -At -c "select table_schema, table_name from information_schema.tables where table_type = 'BASE TABLE'"`,
		"fin_contab|t_lanc_diario\nfin_contab|t_saldo_mes\nfin_fiscal|t_nota_entrada\n"}},
		[]string{"fin_contab", "fin_fiscal", "t_lanc_diario", "t_saldo_mes", "t_nota_entrada"}},
	{"alias no SELECT", []passo{{`snow sql -q "select table_schema as s, table_name as t from dwprd.information_schema.tables"`,
		"+------------+----------------+\n| S          | T              |\n|------------+----------------|\n| FIN_CONTAB | T_LANC_DIARIO  |\n| FIN_FISCAL | T_NOTA_ENTRADA |\n+------------+----------------+\n"}},
		[]string{"FIN_CONTAB", "FIN_FISCAL", "T_LANC_DIARIO", "T_NOTA_ENTRADA"}},
	{"name do catálogo (sqlcmd)", []passo{{`sqlcmd -S srv-erp -Q "SET NOCOUNT OFF; select name from sys.tables order by name"`,
		"name\n--------------------\nt_lanc_diario\nt_saldo_mes\n\n(2 rows affected)\n"}},
		[]string{"t_lanc_diario", "t_saldo_mes"}},
	{"fetchall sem cabeçalho", []passo{{`python -c "cur.execute('select table_schema, table_name from information_schema.tables'); print(cur.fetchall())"`,
		"[('fin_contab', 't_lanc_diario'), ('fin_fiscal', 't_nota_entrada')]\n"}},
		[]string{"fin_contab", "fin_fiscal", "t_lanc_diario", "t_nota_entrada"}},
	{"SHOW SCHEMAS", []passo{{`snow sql -q "show schemas in database dwprd"`,
		"created_on                    | name        | is_default | database_name | owner\n------------------------------+-------------+------------+---------------+---------\n2026-01-02 10:00:00.000 -0300 | fin_contab  | N          | dwprd         | sysadmin\n2026-01-02 10:00:00.000 -0300 | fin_fiscal  | N          | dwprd         | sysadmin\n"}},
		[]string{"fin_contab", "fin_fiscal"}},
	{"cut sobre arquivo conhecido", []passo{cabCarga, {"cut -d, -f2 dados/carga_contab.csv | tail -n +2 | sort -u", "t_lanc_diario\nt_saldo_mes\n"}},
		[]string{"t_lanc_diario", "t_saldo_mes"}},
	{"awk | sort | uniq -c", []passo{cabCarga, {"awk -F, 'NR>1 {print $3}' dados/carga_contab.csv | sort | uniq -c", "      1 fin_contab\n      1 fin_fiscal\n"}},
		[]string{"fin_contab", "fin_fiscal"}},
	{"cut depois do Read", []passo{{"/home/ana/proj/dados/carga_contab.csv", "     1→cd_conta;nm_tabela;nm_schema\n     2→1001;t_lanc_diario;fin_contab\n"},
		{"cut -d';' -f2,3 /home/ana/proj/dados/carga_contab.csv | tail -n +2", "t_lanc_diario;fin_contab\nt_saldo_mes;fin_fiscal\n"}},
		[]string{"t_lanc_diario", "t_saldo_mes", "fin_contab", "fin_fiscal"}},
	{"kubectl get pods", []passo{{"kubectl get pods -n ns-fin", "NAME                                READY   STATUS    RESTARTS   AGE\nsvc-cobranca-lote-7d9f8c6b5-x2x9k   1/1     Running   0          2d\nsvc-conciliacao-6c8d7b9f4-k8j2m     1/1     Running   3          5d\n"}},
		[]string{"svc-cobranca-lote-7d9f8c6b5-x2x9k", "svc-conciliacao-6c8d7b9f4-k8j2m"}},
	{"helm list", []passo{{"helm list -n ns-fin", "NAME            \tNAMESPACE    \tREVISION\tUPDATED                                \tSTATUS  \tCHART          \tAPP VERSION\n" +
		"cobranca-lote   \tns-fin       \t4       \t2026-10-01 10:00:00.123 -0300 -03\tdeployed\tcobranca-1.2.0\t1.2.0\n"}},
		[]string{"cobranca-lote"}},
	{"docker ps", []passo{{"docker ps --format 'table {{.ID}}\\t{{.Status}}\\t{{.Names}}'", "CONTAINER ID   STATUS          NAMES\n3f2a1b0c9d8e   Up 2 hours      conciliacao_worker_1\n9e8d7c6b5a4f   Up 2 hours      cobranca_api_1\n"}},
		[]string{"conciliacao_worker_1", "cobranca_api_1"}},
	{"<tipo> list", []passo{{"gcloud sql instances list --project prj-fin", "NAME            DATABASE_VERSION  LOCATION       TIER\npg-fin-prd01    POSTGRES_15       us-east1-b     db-custom-2\npg-fin-hml01    POSTGRES_15       us-east1-b     db-custom-2\n"}},
		[]string{"pg-fin-prd01", "pg-fin-hml01"}},
}

func TestComandoTipaSaida(t *testing.T) {
	for _, c := range casosComando {
		m := novoTeste(t)
		ok, tot, _, visto := conferirComando(t, m, c.nome, c.ps, c.nomes)
		if tot == 0 || ok != tot {
			t.Errorf("%s: %d/%d\n%s", c.nome, ok, tot, visto)
		}
		// sem o comando, a mesma saída não basta: é a dica que tipa
		m2 := novoTeste(t)
		ult := c.ps[len(c.ps)-1].saida
		o2, _ := m2.Mascarar(ult)
		sem := 0
		for _, n := range c.nomes {
			sem += strings.Count(ult, n) - strings.Count(o2, n)
		}
		t.Logf("%s: %d/%d com o comando, %d sem", c.nome, ok, tot, sem)
		if sem == tot {
			t.Errorf("%s: sem o comando já mascarava tudo (o caso não testa a dica)", c.nome)
		}
	}
}

// a dica vem do comando, não do texto: a mesma saída com outro comando não muda
func TestComandoSemDicaNaoTipa(t *testing.T) {
	saida := "t_lanc_diario\nt_saldo_mes\nt_nota_entrada\n"
	for _, cmd := range []string{"cat notas.txt", "echo ok", ""} {
		m := novoTeste(t)
		_, _, out, visto := conferirComando(t, m, cmd, []passo{{cmd, saida}}, nil)
		if out != saida {
			t.Errorf("%q: mascarou sem dica\n%s", cmd, visto)
		}
	}
}

// listagens comuns e consultas sem coluna de nome: a dica não acrescenta nada (o que os outros
// leitores já mascaram nessas saídas não vem ao caso aqui)
func TestComandoNegativos(t *testing.T) {
	casos := []passo{
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
	for _, c := range casos {
		for _, tr := range transportes {
			m, sem := novoTeste(t), novoTeste(t)
			txt := tr.f(c.saida)
			_, _, out, visto := conferirComando(t, m, c.cmd, []passo{{c.cmd, txt}}, nil)
			if o2, _ := sem.Mascarar(txt); out != o2 {
				t.Errorf("%s / %s: a dica mascarou\n%s", c.cmd, tr.nome, visto)
			}
		}
	}
}

func TestDicaDoComando(t *testing.T) {
	cs := NovosComandos()
	for _, c := range []struct{ cmd, quer string }{
		{`psql -c "select table_schema, table_name from information_schema.tables"`, "F|schema,tabela|table_name:tabela,table_schema:schema|"},
		{`snow sql -q "SHOW TABLES IN SCHEMA fin"`, "F||name:tabela|tabela"},
		{"kubectl get svc -A", "f||name:servico,names:servico|servico"},
		{"aws s3api list-buckets --output table", "f||name:bucket,names:bucket|bucket"},
		{"ls -la", ""},
		{"ps aux", ""},

		{"git log --oneline", ""},
		{`psql -c "select * from information_schema.tables"`, ""},
	} {
		if d := cs.Dica(c.cmd, ""); d != c.quer {
			t.Errorf("%q: dica %q, queria %q", c.cmd, d, c.quer)
		}
	}
}

// item 25: nome qualificado depois de palavra de tipo, em qualquer frase
var casosTipoQualificado = []struct{ nome, texto string }{
	{"rótulo pt", "tabela fin_contab.t_lanc_diario: 1200 linhas\n"},
	{"dbt", "10:01:02  1 of 3 OK created sql table model fin_contab.t_lanc_diario ....... [SUCCESS 1 in 2.10s]\n"},
	{"loading", "Loading table dwprd_fin.fin_contab.t_lanc_diario\n"},
	{"erro citado", "Object 'DWPRD_FIN.FIN_CONTAB.T_LANC_DIARIO' does not exist or not authorized.\n"},
	{"conexão pt", "conectando em srv-dw-fin01:5432/dwprd_fin como svc_carga_fin\n"},
	{"conexão en", "connecting to srv-dw-fin01:5432/dwprd_fin as svc_carga_fin\n"},
}

func TestTipoQualificado(t *testing.T) {
	nomes := []string{"fin_contab", "t_lanc_diario", "dwprd_fin", "FIN_CONTAB", "T_LANC_DIARIO", "DWPRD_FIN", "srv-dw-fin01", "svc_carga_fin"}
	for _, c := range casosTipoQualificado {
		for _, tr := range transportes {
			m := novoTeste(t)
			txt := tr.f(c.texto)
			ok, tot := conferirMascarado(t, m, c.nome+"/"+tr.nome, txt, nomes)
			if tot == 0 || ok != tot {
				t.Errorf("%s / %s: %d/%d\n%s", c.nome, tr.nome, ok, tot, mostrar(m, txt))
			}
		}
	}
	// o tipo de cada parte
	m := novoTeste(t)
	s := "Loading table dwprd_fin.fin_contab.t_lanc_diario\n"
	for real, quer := range map[string]string{"dwprd_fin": "obj.database", "fin_contab": "obj.schema", "t_lanc_diario": "obj.tabela"} {
		if tp := tipoDe(m, s, real); tp != quer {
			t.Errorf("%s: tipo %q, queria %q", real, tp, quer)
		}
	}
}

func TestTipoQualificadoNegativos(t *testing.T) {
	for _, s := range []string{
		"see the table in os.path for details\n",
		"a tabela vendas.csv foi carregada\n",
		"the schema is documented at docs.python.org\n",
		"connecting to localhost:8080/api\n",
		"table of contents: intro.md\n",
		"a tabela foi carregada em 2.5 segundos\n",
		"index i.e. the first one\n",
	} {
		m := novoTeste(t)
		m.cfg.DominiosInternos = nil
		if out, _ := m.Mascarar(s); out != s {
			t.Errorf("mascarou %q\n-> %q", s, mostrar(m, s))
		}
	}
}

// item 26: lista homogênea; os nomes ensinados antes por um SQL
const sqlEnsina = "select * from fin_contab.t_lanc_diario d join fin_contab.t_saldo_mes s on s.cd = d.cd join fin_contab.t_plano_contas p on p.cd = d.cd\n"

var casosLista = []struct{ nome, texto string }{
	{"coluna solta", "t_lanc_diario\nt_saldo_mes\nt_razao_aux\n"},
	{"marcadores", "- t_lanc_diario\n- t_saldo_mes\n- t_razao_aux\n"},
	{"sort | uniq -c", "     12 t_lanc_diario\n      7 t_saldo_mes\n      3 t_razao_aux\n"},
	{"value_counts", "t_lanc_diario    12\nt_saldo_mes       7\nt_razao_aux       3\n"},
	{"laço com ==", "== t_lanc_diario ==\nlinhas: 10\n== t_saldo_mes ==\nlinhas: 3\n== t_razao_aux ==\nlinhas: 7\n"},
	{"vírgulas", "tabelas carregadas: t_lanc_diario, t_saldo_mes, t_razao_aux\n"},
	{"json.dumps", `["t_lanc_diario", "t_saldo_mes", "t_razao_aux", "t_centro_custo"]` + "\n"},
	{"qualificado com schema conhecido", "copiado para fin_contab.t_razao_aux ontem\n"},
}

func TestListaHomogenea(t *testing.T) {
	for _, c := range casosLista {
		for _, tr := range transportes {
			m := novoTeste(t)
			m.Mascarar(sqlEnsina)
			txt := tr.f(c.texto)
			ok, tot := conferirMascarado(t, m, c.nome+"/"+tr.nome, txt, []string{"t_razao_aux", "t_centro_custo"})
			if tot == 0 || ok != tot {
				t.Errorf("%s / %s: %d/%d\n%s", c.nome, tr.nome, ok, tot, mostrar(m, txt))
			}
		}
	}
	// o item novo é aprendido: depois aparece sozinho e é mascarado
	m := novoTeste(t)
	m.Mascarar(sqlEnsina)
	m.Mascarar("t_lanc_diario\nt_saldo_mes\nt_razao_aux\n")
	if out, _ := m.Mascarar("a t_razao_aux tem 3 linhas"); strings.Contains(out, "t_razao_aux") {
		t.Errorf("item da lista não foi aprendido: %s", out)
	}
	if tp := tipoDe(m, "t_razao_aux", "t_razao_aux"); tp != "obj.tabela" {
		t.Errorf("tipo %q", tp)
	}
}

func TestListaHomogeneaNegativos(t *testing.T) {
	for _, s := range []string{
		"t_lanc_diario\nfoo_bar\nbaz_qux\nqux_1\n",                    // menos da metade conhecida
		"carregar(t_lanc_diario, t_saldo_mes, x_train)\n",             // argumentos de função
		"t_lanc_diario\nt_saldo_mes\nresumo\npronto\n",                // sem cara de identificador
		"t_lanc_diario, t_saldo_mes, relatorio_final.pdf\n",           // arquivo
		"      12 build_cache\n       7 notas_reuniao\n       3 ok\n", // nada conhecido
		"fin_contab.read_csv(arquivo)\n",                              // chamada
	} {
		m := novoTeste(t)
		m.cfg.DominiosInternos = nil
		m.Mascarar(sqlEnsina)
		_, ents := m.Mascarar(s)
		for _, e := range ents {
			for _, fica := range []string{"foo_bar", "baz_qux", "qux_1", "x_train", "resumo", "pronto", "relatorio_final", "build_cache", "notas_reuniao", "read_csv"} {
				if e.Real == fica {
					t.Errorf("%q: mascarou %s\n-> %q", s, fica, mostrar(m, s))
				}
			}
		}
	}
}

// item 27: um nome aprendido é mascarado dentro de qualquer forma solta
func TestPropagacaoFormasSoltas(t *testing.T) {
	formas := []struct{ nome, texto string }{
		{"f-string", `print(f"carregando fin_contab.t_lanc_diario em {destino}")` + "\n" + `log.info(f"t_saldo_mes: {n} linhas")` + "\n"},
		{"log", "2026-10-05T10:00:00Z INFO job=carga tabela_alvo=t_lanc_diario status=ok\n"},
		{"célula de planilha", "B2=t_lanc_diario\nC2=t_saldo_mes\n"},
		{"echo com rótulo", "Tabela atual -> t_lanc_diario\nprocessando t_saldo_mes...\n"},
		{"texto de docx/pdf", "A tabela t_lanc_diario é atualizada diariamente a partir da t_saldo_mes, no schema fin_contab.\n"},
		{"kubectl logs", "I1005 10:00:00.123 main.go:42] gravando 1200 linhas em fin_contab.t_lanc_diario\n"},
	}
	for _, f := range formas {
		for _, tr := range transportes {
			m := novoTeste(t)
			m.Mascarar(sqlEnsina)
			txt := tr.f(f.texto)
			ok, tot := conferirMascarado(t, m, f.nome+"/"+tr.nome, txt, []string{"t_lanc_diario", "t_saldo_mes", "fin_contab"})
			if tot == 0 || ok != tot {
				t.Errorf("%s / %s: %d/%d\n%s", f.nome, tr.nome, ok, tot, mostrar(m, txt))
			}
		}
	}
}
