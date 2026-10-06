package mask

import "testing"

// Seção B: o leitor de tabela em qualquer desenho. Os nomes de ferramenta são só de onde a forma
// foi copiada; a regra lê a forma.

var nomesTabela = []string{"fin_contab", "lanc_diario", "plano_contas", "vl_lancto", "cd_conta"}

var desenhosTabela = []struct{ nome, texto string }{
	{"pandas print(df)", `  table_schema    table_name column_name
0   fin_contab   lanc_diario   vl_lancto
1   fin_contab  plano_contas    cd_conta
`},
	{"pandas truncado", `     table_schema    table_name column_name
0      fin_contab   lanc_diario   vl_lancto
..            ...           ...         ...
41     fin_contab  plano_contas    cd_conta

[42 rows x 3 columns]
`},
	{"polars", `shape: (2, 3)
┌──────────────┬──────────────┬─────────────┐
│ table_schema ┆ table_name   ┆ column_name │
│ ---          ┆ ---          ┆ ---         │
│ str          ┆ str          ┆ str         │
╞══════════════╪══════════════╪═════════════╡
│ fin_contab   ┆ lanc_diario  ┆ vl_lancto   │
│ fin_contab   ┆ plano_contas ┆ cd_conta    │
└──────────────┴──────────────┴─────────────┘
`},
	{"duckdb", `┌──────────────┬──────────────┬─────────────┐
│ table_schema │  table_name  │ column_name │
│   varchar    │   varchar    │   varchar   │
├──────────────┼──────────────┼─────────────┤
│ fin_contab   │ lanc_diario  │ vl_lancto   │
│ fin_contab   │ plano_contas │ cd_conta    │
└──────────────┴──────────────┴─────────────┘
`},
	{"R data.frame", `  table_schema   table_name column_name
1   fin_contab  lanc_diario   vl_lancto
2   fin_contab plano_contas    cd_conta
`},
	{"R tibble", `# A tibble: 2 × 3
  table_schema table_name   column_name
  <chr>        <chr>        <chr>
1 fin_contab   lanc_diario  vl_lancto
2 fin_contab   plano_contas cd_conta
`},
	{"HTML", `<table><thead><tr><th>table_schema</th><th>table_name</th><th>column_name</th></tr></thead>
<tbody><tr><td>fin_contab</td><td>lanc_diario</td><td>vl_lancto</td></tr>
<tr><td>fin_contab</td><td>plano_contas</td><td>cd_conta</td></tr></tbody></table>
`},
	{"tuplas por linha", `('table_schema', 'table_name', 'column_name')
('fin_contab', 'lanc_diario', 'vl_lancto')
('fin_contab', 'plano_contas', 'cd_conta')
`},
	{"listas por linha", `['table_schema', 'table_name', 'column_name']
['fin_contab', 'lanc_diario', 'vl_lancto']
['fin_contab', 'plano_contas', 'cd_conta']
`},
	{"lista de tuplas", `[('table_schema', 'table_name', 'column_name'), ('fin_contab', 'lanc_diario', 'vl_lancto'), ('fin_contab', 'plano_contas', 'cd_conta')]
`},
	{"lista de listas JSON", `[["table_schema","table_name","column_name"],["fin_contab","lanc_diario","vl_lancto"],["fin_contab","plano_contas","cd_conta"]]
`},
	{"psql expandido", `-[ RECORD 1 ]+-------------
table_schema | fin_contab
table_name   | lanc_diario
column_name  | vl_lancto
-[ RECORD 2 ]+-------------
table_schema | fin_contab
table_name   | plano_contas
column_name  | cd_conta
`},
	{"CSV ponto e vírgula", "table_schema;table_name;column_name\nfin_contab;lanc_diario;vl_lancto\nfin_contab;plano_contas;cd_conta\n"},
	{"TSV", "table_schema\ttable_name\tcolumn_name\nfin_contab\tlanc_diario\tvl_lancto\nfin_contab\tplano_contas\tcd_conta\n"},
	{"CSV com aspas", `"urn","table_schema","table_name","column_name"
"urn:li:dataset:(urn:li:dataPlatform:postgres,erp.fin_contab.lanc_diario,PROD)","fin_contab","lanc_diario","vl_lancto"
"urn:li:dataset:(urn:li:dataPlatform:postgres,erp.fin_contab.plano_contas,PROD)","fin_contab","plano_contas","cd_conta"
`},
	{"largura fixa sem traços", `TABLE_SCHEMA    TABLE_NAME      COLUMN_NAME
fin_contab      lanc_diario     vl_lancto
fin_contab      plano_contas    cd_conta
`},
	{"DESCRIBE", `+-----------+-------------+------+-----+
| Field     | Type        | Null | Key |
+-----------+-------------+------+-----+
| vl_lancto | decimal(12,2) | NO   |     |
| cd_conta  | varchar(20) | YES  | MUL |
+-----------+-------------+------+-----+
`},
	{"DESC TABLE name/type", `name      | type          | kind   | null?
----------+---------------+--------+------
VL_LANCTO | NUMBER(12,2)  | COLUMN | N
CD_CONTA  | VARCHAR(20)   | COLUMN | Y
`},
}

func TestTabelaDesenhos(t *testing.T) {
	for _, d := range desenhosTabela {
		for _, tr := range transportes {
			m := novoTeste(t)
			txt := tr.f(d.texto)
			ok, tot := conferirMascarado(t, m, d.nome+"/"+tr.nome, txt, append(nomesTabela, "VL_LANCTO", "CD_CONTA"))
			if ok != tot {
				out, _ := m.Mascarar(txt)
				t.Errorf("%s / %s: %d/%d\n%s", d.nome, tr.nome, ok, tot, out)
			}
		}
	}
}

func TestTabelaListagens(t *testing.T) {
	casos := []struct{ nome, texto string }{
		{"kubectl get -A", `NAMESPACE        NAME                                READY   STATUS    RESTARTS   AGE
ns-financeiro    svc-cobranca-lote-7d9f8c6b5-x2x9k   1/1     Running   0          2d
ns-financeiro    svc-conciliacao-6c8d7b9f4-k8j2m     1/1     Running   3          5d
`},
		{"helm list", "NAME            \tNAMESPACE    \tREVISION\tUPDATED                                \tSTATUS  \tCHART          \tAPP VERSION\n" +
			"cobranca-lote   \tns-financeiro\t4       \t2026-10-01 10:00:00.123 -0300 -03\tdeployed\tcobranca-1.2.0\t1.2.0\n"},
		{"aws table", `-----------------------------------------------
|                 ListBuckets                 |
+---------------------------------------------+
||                  Buckets                  ||
|+------------------------+------------------+|
||      CreationDate      |      Name        ||
|+------------------------+------------------+|
||  2024-01-01T00:00:00Z  |  bkt-relat-fin   ||
||  2024-02-01T00:00:00Z  |  bkt-notas-fisc  ||
|+------------------------+------------------+|
`},
		{"aws text", "BUCKETS\t2024-01-01T00:00:00.000Z\tbkt-relat-fin\nBUCKETS\t2024-02-01T00:00:00.000Z\tbkt-notas-fisc\n"},
	}
	// a coluna NAME sem tipo no cabeçalho fica para o tipo dado pelo comando (seção F)
	nomes := []string{"ns-financeiro", "bkt-relat-fin", "bkt-notas-fisc"}
	for _, c := range casos {
		m := novoTeste(t)
		ok, tot := conferirMascarado(t, m, c.nome, c.texto, nomes)
		if ok != tot {
			out, _ := m.Mascarar(c.texto)
			t.Errorf("%s: %d/%d\n%s", c.nome, ok, tot, out)
		}
	}
}

// negativos (item 10): nada mascarado
var negativosTabela = []struct{ nome, texto string }{
	{"ls -l", `total 16
drwxr-xr-x 2 root root 4096 Oct  5 10:11 bin
-rw-r--r-- 1 root root  220 Oct  5 10:11 notas_reuniao.txt
-rw-r--r-- 1 root root 3771 Oct  5 10:11 build_cache.tar
`},
	{"df -h", `Filesystem      Size  Used Avail Use% Mounted on
/dev/sda1        98G   41G   52G  45% /
tmpfs           3.9G     0  3.9G   0% /dev/shm
`},
	{"pip list", `Package            Version
------------------ -----------
charset-normalizer 3.3.2
python-dateutil    2.9.0.post0
typing_extensions  4.12.2
`},
	{"git log --oneline", `a1b2c3d fix: ajusta leitura do cabecalho
e4f5a6b feat: novo relatorio_mensal
0c9d8e7 chore: atualiza dependencias
`},
	{"ps aux", `USER         PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND
root           1  0.0  0.1 167736 11544 ?        Ss   Oct05   0:03 /sbin/init
root         412  0.0  0.0  25300  7216 ?        Ss   Oct05   0:00 /lib/systemd/systemd-udevd
`},
	{"benchmark", `name           time/op
FrioDenso-32   21.2ms ± 3%
FrioComum-32   8.87ms ± 2%
`},
	{"DataFrame de números", `          a         b         c
0  0.496714 -0.138264  0.647689
1  1.523030 -0.234153 -0.234137
2  1.579213  0.767435 -0.469474
`},
	{"prosa", `Rodei os testes de novo hoje cedo
e o resultado mudou bastante desde ontem
quando o cache_local ainda estava ligado
`},
}

func TestTabelaNegativos(t *testing.T) {
	for _, c := range negativosTabela {
		for _, tr := range transportes {
			m := novoTeste(t)
			m.cfg.DominiosInternos = nil
			m.cfg.Termos = nil
			txt := tr.f(c.texto)
			if out, _ := m.Mascarar(txt); out != txt {
				t.Errorf("%s / %s: mascarou\n%s", c.nome, tr.nome, out)
			}
		}
	}
}

func TestEhTipoDado(t *testing.T) {
	for _, v := range []string{"varchar(20)", "NUMBER(12,2)", "datetime64[ns]", "<chr>", "Int64", "string", "i64", "StringType()", "decimal(10,2)"} {
		if !ehTipoDado(v) {
			t.Errorf("%q deveria ser tipo de dado", v)
		}
	}
	for _, v := range []string{"lanc_diario", "fin_contab", "Running", "ClusterIP"} {
		if ehTipoDado(v) {
			t.Errorf("%q não é tipo de dado", v)
		}
	}
}
