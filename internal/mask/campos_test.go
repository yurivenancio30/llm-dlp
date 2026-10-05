package mask

import (
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

func TestClasseRotulo(t *testing.T) {
	casos := map[string]string{
		"NOM_CLIENTE": "nome", "nomeCliente": "nome", "Nome da Mãe": "nome", "NOM_DATA_OWNER": "nome", "NOM_STEWARD": "nome",
		"full_name": "nome", "nm_titular": "nome", "CPF": "cpf", "NUM_CPF_CLIENTE": "cpf", "nr_cnpj": "cnpj", "DT_NASCIMENTO": "nascimento",
		"telefone_celular": "telefone", "COD_USU_DATA_OWNER": "usuario", "matricula": "usuario", "login": "usuario", "user_id": "usuario",
		"religiao": "sensivel", "raca_cor": "sensivel", "orientacao_sexual": "sensivel", "tipo_sanguineo": "sensivel", "user_name": "usuario",
		"id_cliente": "usuario", "nome_usuario": "usuario",
		"titulo_eleitor": "doc", "passaporte": "doc", "placa": "doc", "endereco_residencial": "endereco", "CEP": "cep", "num_rg": "rg",
		// nomes de coisas e campos comuns: nada
		"NOM_SISTEMA": "", "table_name": "", "nome": "nomesolto", "name": "nomesolto", "Cliente": "nomecompleto", "CPF/CNPJ": "cpf", "NOM_CATEGORIA": "", "schema_name": "", "TABLE_NAME": "", "user": "",
		"owner": "nomecompleto", "Data Steward": "nomecompleto", "nome_coluna": "", "tipo_usuario": "", "role_name": "", "file_name": "", "display_name": "", "hostname": "",
		"database_name": "", "status": "", "id": "", "tool_name": "", "model_name": "", "ip_address": "", "email_address": "",
		"login_timeout": "", "usuario_erro_critico_pct": "", "user_message_id": "", "df_health": "", "saude": "", "diagnostico": "", "pai": "",
		"cid": "", "cluster_health_check": "", "rds_username": "", "com o nome que o datahub usa": "", "cor": "", "titulo": "", "partido": "",
	}
	for r, quer := range casos {
		if got := classeRotulo(r); got != quer {
			t.Errorf("classeRotulo(%q) = %q, queria %q", r, got, quer)
		}
	}
}

func TestCamposRotulados(t *testing.T) {
	cpfSem := strings.NewReplacer(".", "", "-", "").Replace(gerarCPF("529982247"))
	pega := map[string][]string{
		`{"NOM_CLIENTE": "Maria Aparecida Lopes", "VL_SALDO": 100}`: {"Maria Aparecida Lopes"},
		"nome_mae: Francisca Pereira Lima\nstatus: ativo":           {"Francisca Pereira Lima"},
		"COD_USU_DATA_OWNER=B123456":                                {"B123456"},
		"matricula: 0098765":                                        {"0098765"},
		"religiao: catolica":                                        {"catolica"},
		"tipo_sanguineo: O+":                                        {"O+"},
		"titulo_eleitor: 1234 5678 9012":                            {"1234 5678 9012"},
		"NOM_CLIENTE,NUM_CPF,VL_SALDO\nMaria Aparecida Lopes," + cpfSem + ",10.50\n": {"Maria Aparecida Lopes", cpfSem},
		"NOM_SISTEMA;COD_USU_OWNER;NOM_OWNER\nSISTEMA_X;B123456;Carlos Eduardo Nunes\nSISTEMA_Y;C654321;Paula Regina Dias\n": {
			"B123456", "Carlos Eduardo Nunes", "C654321", "Paula Regina Dias"},
		"| NOM_CLIENTE | DT_NASCIMENTO | PRODUTO |\n|---|---|---|\n| Jose Antonio Reis | 1980-05-17 | CDB |\n": {"Jose Antonio Reis", "1980-05-17"},
		"NOM_CLIENTE\tTELEFONE\nRita de Cassia Melo\t31999998888\n":                                            {"Rita de Cassia Melo", "31999998888"},
		"NOM_SISTEMA,NOM_STEWARD\nSIS_A,Carlos Eduardo Nunes / Paula Regina Dias\n":                            {"Carlos Eduardo Nunes", "Paula Regina Dias"},
		"Nom_tabela;Owner\nTB_X;Carlos Eduardo Nunes\n":                                                        {"Carlos Eduardo Nunes"},
		"placa de rede aa:bb:cc:dd:ee:ff ok":                                                                   {"aa:bb:cc:dd:ee:ff"},
	}
	for texto, reais := range pega {
		out, _ := novoTeste(t).Mascarar(texto)
		for _, r := range reais {
			if strings.Contains(out, r) {
				t.Errorf("passou %q em %q -> %q", r, texto, out)
			}
		}
	}
	// o que NÃO é sensível fica como está (valores, nomes de sistema). Nomes de coluna de
	// cabeçalho, schema/tabela de catálogo e "table_name" são nomes de recursos e são
	// mascarados de propósito (ver TestDesenhoMascaraRecursos).
	fica := map[string][]string{
		"NOM_SISTEMA;COD_USU_OWNER;NOM_OWNER\nSISTEMA_X;B123456;Carlos Eduardo Nunes\n": {"SISTEMA_X"},
		"NOM_CLIENTE,NUM_CPF,VL_SALDO\nMaria Aparecida Lopes,52998224725,10.50\n":       {"10.50"},
		"TABLE_SCHEMA,TABLE_NAME,ROW_COUNT\nGOLD,TB_CLIENTE,1500\n":                     {"1500"},
		`{"name": "Bash", "user": "root", "table_name": "TB_X"}`:                        {"Bash", "root"},
		"name,owner\nTAG_X,SYSADMIN\nTAG_Y,ACCOUNTADMIN\n":                              {"SYSADMIN", "ACCOUNTADMIN"},
		"usuario: postgres\nlogin = admin":                                              {"postgres", "admin"},
	}
	for texto, ficam := range fica {
		out, _ := novoTeste(t).Mascarar(texto)
		for _, r := range ficam {
			if !strings.Contains(out, r) {
				t.Errorf("mascarou %q sem precisar em %q -> %q", r, texto, out)
			}
		}
	}
}

// Nome achado pelo rótulo entra no registro: depois é reconhecido solto, em outra grafia,
// e a ida e volta devolve o que foi escrito.
func TestNomeDeCampoViraPessoaConhecida(t *testing.T) {
	m := novoTeste(t)
	m.Mascarar("NOM_CLIENTE;SALDO\nMaria Aparecida Lopes;10\n")
	out, ents := m.Mascarar("falei com a MARIA APARECIDA LOPES e com Maria Aparecida Lopes ontem")
	if strings.Contains(strings.ToLower(out), "aparecida") {
		t.Fatalf("nome aprendido pelo campo passou solto: %s", out)
	}
	if volta := NovaTabela(ents).Desmascarar(out, false); !strings.Contains(volta, "MARIA APARECIDA LOPES") {
		t.Fatalf("duas grafias do mesmo nome não voltaram: %s", volta)
	}
	// código aprendido pelo campo: reconhecido solto, em qualquer caixa
	m.Mascarar("COD_USU_OWNER=B123456")
	if out, _ := m.Mascarar("o usuário b123456 e B123456 travaram"); strings.Contains(strings.ToUpper(out), "B123456") {
		t.Fatalf("código aprendido passou solto: %s", out)
	}
}

// Código de usuário de pessoa importada: reconhecido em qualquer lugar, com o mesmo id do nome.
func TestCodigoDePessoaImportada(t *testing.T) {
	m := novoTeste(t) // importa "João Carlos Silva" com o código U1001
	out, _ := m.Mascarar("o usuário U1001 (JOAO CARLOS SILVA) abriu o chamado")
	if strings.Contains(out, "U1001") {
		t.Fatalf("código importado passou: %s", out)
	}
	i := strings.Index(out, "USUARIO-")
	if i < 0 || !strings.Contains(out, "Pessoa "+out[i+8:i+16]) {
		t.Errorf("código e nome da mesma pessoa sem o mesmo id: %s", out)
	}
}

// O detector por nome de campo tem que valer para qualquer origem: vários formatos de
// arquivo e várias convenções de nome (português, inglês, camelCase, abreviações). Todos os
// dados aqui são fictícios.
func TestFormatosGenericos(t *testing.T) {
	cpf := strings.NewReplacer(".", "", "-", "").Replace(gerarCPF("529982247"))
	casos := []struct {
		nome, texto string
		reais       []string
	}{
		{"CSV com vírgula", "id,nome_cliente,cpf,saldo\n1,Maria Aparecida Lopes," + cpf + ",10.5\n2,Jose Antonio Reis," + cpf + ",7\n", []string{"Maria Aparecida Lopes", "Jose Antonio Reis", cpf}},
		{"CSV com aspas", "\"Customer Name\",\"Phone Number\",\"Plan\"\n\"Rita de Cassia Melo\",\"31999998888\",\"gold\"\n", []string{"Rita de Cassia Melo", "31999998888"}},
		{"TSV", "customerId\tfullName\tbirthDate\nC0012345\tPaulo Henrique Braga\t1979-03-21\n", []string{"C0012345", "Paulo Henrique Braga", "1979-03-21"}},
		{"ponto e vírgula, cabeçalho com espaços", "Nome do Cliente;CPF/CNPJ;Telefone Celular\nLuiza Fernandes Prado;" + cpf + ";(31) 98888-7777\n", []string{"Luiza Fernandes Prado", cpf}},
		{"markdown", "| Cliente | Data de Nascimento | Produto |\n|:--|:-:|--:|\n| Carla Regina Souto | 12/03/1985 | CDB |\n", []string{"Carla Regina Souto", "12/03/1985"}},
		{"tabela de terminal (mysql)", "+----------------------+-------------+\n| nm_titular           | nr_cpf      |\n+----------------------+-------------+\n| Otavio Luiz Camargo  | " + cpf + " |\n+----------------------+-------------+\n", []string{"Otavio Luiz Camargo", cpf}},
		{"tabela de terminal (psql)", " employee_name    | employee_id | dept\n------------------+-------------+------\n Sandra Maia Lins | E0098765    | TI\n", []string{"Sandra Maia Lins", "E0098765"}},
		{"JSON", `[{"firstName": "Renata", "lastName": "Vasconcelos", "motherName": "Ivone Vasconcelos Lima", "taxId": 1}]`, []string{"Renata", "Vasconcelos", "Ivone Vasconcelos Lima"}},
		{"JSON em linhas", "{\"nome_completo\":\"Davi Lucca Moraes\",\"matricula\":\"0045678\"}\n{\"nome_completo\":\"Helena Castro Diniz\",\"matricula\":\"0045679\"}\n", []string{"Davi Lucca Moraes", "Helena Castro Diniz", "0045678", "0045679"}},
		{"YAML", "cliente:\n  nome_cliente: Bruno Cesar Tavares\n  dt_nascimento: 1990-07-02\n  religiao: espirita\n", []string{"Bruno Cesar Tavares", "1990-07-02", "espirita"}},
		{"chave=valor (log, .env)", "evento=login user_id=884422 nome_usuario=bctavares ok\nCLIENTE_CPF=" + cpf + "\n", []string{"884422", "bctavares", cpf}},
		{"SQL INSERT", "INSERT INTO clientes (id, nome_cliente, num_cpf, limite) VALUES (1, 'Maria Aparecida Lopes', '" + cpf + "', 500), (2, 'Jose Antonio Reis', '" + cpf + "', 90);", []string{"Maria Aparecida Lopes", "Jose Antonio Reis", cpf}},
		{"XML", "<cliente><nomeCliente>Aline Barros Teles</nomeCliente><cpf>" + cpf + "</cpf><produto>CDB</produto></cliente>", []string{"Aline Barros Teles", cpf}},
		{"coluna 'nome' numa tabela de pessoas", "nome;cpf;cidade\nFabio Augusto Leme;" + cpf + ";Belo Horizonte\n", []string{"Fabio Augusto Leme", cpf}},
		{"dois responsáveis na célula", "sistema,responsavel\nSIS_A,Carlos Eduardo Nunes / Paula Regina Dias\n", []string{"Carlos Eduardo Nunes", "Paula Regina Dias"}},
		{"planilha impressa pelo pandas (print(df))", comoPandas([]string{"nome_cliente", "cpf", "saldo"}, [][]string{
			{"Maria Aparecida Lopes", cpf, "10.50"}, {"José Antônio Reis", cpf, "7.00"}, {"Ana Lúcia Prado", cpf, "130.25"}}),
			[]string{"Maria Aparecida Lopes", "José Antônio Reis", "Ana Lúcia Prado", cpf}},
		{"planilha em colunas alinhadas à esquerda", "NOME_CLIENTE           NUM_CPF      PRODUTO\nMaria Aparecida Lopes  " + cpf + "  CDB\nJosé Antônio Reis      " + cpf + "  LCI\n", []string{"Maria Aparecida Lopes", "José Antônio Reis", cpf}},
		{"planilha lida com openpyxl (tuplas)", "('NOM_CLIENTE', 'NUM_CPF', 'SALDO')\n('Maria Aparecida Lopes', '" + cpf + "', 10.5)\n('Jose Antonio Reis', " + cpf + ", 7)\n", []string{"Maria Aparecida Lopes", "Jose Antonio Reis", cpf}},
		{"planilha como lista de listas", "[\"Cliente\", \"Telefone\"]\n[\"Rita de Cassia Melo\", \"31999998888\"]\n", []string{"Rita de Cassia Melo", "31999998888"}},
		{"planilha como dicionários (df.to_dict)", "[{'NOM_CLIENTE': 'Maria Aparecida Lopes', 'NUM_CPF': '" + cpf + "'}, {'NOM_CLIENTE': 'Jose Antonio Reis', 'NUM_CPF': '" + cpf + "'}]", []string{"Maria Aparecida Lopes", "Jose Antonio Reis", cpf}},
		{"arquivo lido pelo Claude Code (número da linha antes de cada linha)", "1\tNOM_CLIENTE;NUM_CPF;PRODUTO\n2\tMaria Aparecida Lopes;" + cpf + ";CDB\n3\tJosé Antônio Reis;" + cpf + ";LCI\n", []string{"Maria Aparecida Lopes", "José Antônio Reis", cpf}},
		{"idem, com seta e alinhado", "     1→NOM_CLIENTE,NUM_CPF\n     2→Maria Aparecida Lopes," + cpf + "\n", []string{"Maria Aparecida Lopes", cpf}},
		{"idem, TSV", "1\tNOM_CLIENTE\tNUM_CPF\n2\tMaria Aparecida Lopes\t" + cpf + "\n", []string{"Maria Aparecida Lopes", cpf}},
		{"idem, planilha impressa", numerar(comoPandas([]string{"nome_cliente", "cpf", "saldo"}, [][]string{{"Maria Aparecida Lopes", cpf, "10.50"}, {"José Antônio Reis", cpf, "7.00"}, {"Ana Lúcia Prado", cpf, "1.25"}})), []string{"Maria Aparecida Lopes", "José Antônio Reis", "Ana Lúcia Prado", cpf}},
		{"TSV em que a primeira coluna é um número (não é número de linha)", "id\tnome_cliente\tcpf\n1\tMaria Aparecida Lopes\t" + cpf + "\n2\tJosé Antônio Reis\t" + cpf + "\n", []string{"Maria Aparecida Lopes", "José Antônio Reis", cpf}},
		{"SQL INSERT lido pelo Claude Code (número de linha)", "1\t-- dump\n2\tINSERT INTO clientes (id, nome_cliente, num_cpf, dt_nascimento) VALUES\n3\t(1, 'Maria Aparecida Lopes', '" + cpf + "', '1998-04-13'),\n4\t(2, 'Jose Antonio Reis', '" + cpf + "', '1984-08-17');\n", []string{"Maria Aparecida Lopes", "Jose Antonio Reis", cpf, "1998-04-13", "1984-08-17"}},
		{"linhas coladas sem cabeçalho, com CPF", "Maria Aparecida Lopes | " + gerarCPF("529982247") + " | maria.3@gmail.com\nJose Antonio Reis | " + gerarCPF("529982247") + " | jose.4@gmail.com\n", []string{"Maria Aparecida Lopes", "Jose Antonio Reis"}},
		{"dado pessoal sensível em tabela", "paciente\ttipo_sanguineo\tdeficiencia\nMarcos Vinicius Paz\tAB-\tauditiva\n", []string{"Marcos Vinicius Paz", "AB-", "auditiva"}},
	}
	for _, c := range casos {
		out, _ := novoTeste(t).Mascarar(c.texto)
		for _, r := range c.reais {
			if strings.Contains(out, r) {
				t.Errorf("%s: passou %q\n   %q", c.nome, r, out)
			}
		}
	}
	// O que não é dado de pessoa fica como está, em qualquer formato.
	intactos := []string{
		"name,type,size\nreport.pdf,file,1024\nimages,dir,4096\n",
		`{"name": "build", "steps": [{"name": "Run tests", "run": "go test ./..."}]}`,
		"name: Deploy to Production\non: push\njobs:\n  build:\n    name: Build and Test\n",
		"| Sistema | Status | Dono do processo |\n|---|---|---|\n| SIS_A | ativo | TI |\n",
		"<produto><nome>CDB Liquidez</nome><tipo>renda fixa</tipo></produto>",
		"func login(user string) error { return nil } // login: ver docs",
		"cliente = buscar_cliente(id)\nresponsavel = None\n",
		// listas de colunas em SQL e em texto não são cabeçalho de tabela
		"select start_time, user_name, role_name, query_type\nfrom account_usage.query_history\nwhere user_name, ROLE_NAME, QUERY_TYPE\n",
		"select\n    start_time,\n    user_name,\n    role_name,\n    WAREHOUSE_NAME,\n    QUERY_TYPE\nfrom t\n",
		"start_time, user_name, role_name,\nQUERY_TYPE, WAREHOUSE_NAME, DATABASE_NAME,\n",
		"`user_id`, `user_name`, `role_name`\n`REQUEST_ID`, `USER_TAGS`, `TOKENS`\n",
		"group by user_name, role_name\norder by total_elapsed, QUERY_TYPE\n",
		// linha de dados de uma tabela não vira cabeçalho, mesmo com nome de campo numa célula
		// data numa coluna de documento não é documento
		"cep,cidade\n2026-10-03,Belo Horizonte\n",
		"num_cpf: 2026-10-03T12:30:00",
		"telefone;obs\n12:30:00;ok\n",
		"   produto  preco   estoque\n0      CDB  10.50       100\n1      LCI   7.00        50\n", // planilha sem coluna de pessoa
		"NAME          STATUS    AGE\nmysql-0       Running   3d\nredis-0       Running   9d\n",   // kubectl get pods
		"('id', 'produto')\n(1, 'CDB')\n",
		"total  used  free\n  16G    8G    8G\n",
	}
	for _, s := range intactos {
		if out, ents := novoTeste(t).Mascarar(s); len(ents) > 0 {
			t.Errorf("mascarou sem precisar:\n   %q\n-> %q", s, out)
		}
	}
}

// O vocabulário de nomes de campo é extensível pelo config, e os quase identificadores são opcionais.
func TestCamposExtrasEOpcionais(t *testing.T) {
	cfg := config.Padrao()
	cfg.CamposExtras = []config.CampoExtra{{Tipo: "usuario", Palavras: []string{"RE", "num_chapa_func"}}, {Tipo: "pessoa", Palavras: []string{"cooperado"}}}
	cfg.DetectoresOpcionais = []string{"quase"}
	ps, _ := CarregarPessoas(t.TempDir() + "/p.json")
	m, err := NovoMasker(cfg, chaveTeste, ps, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := m.Mascarar("RE;NOME_COOPERADO;SEXO;IDADE\n778899;Livia Duarte Pinho;F;34\n")
	for _, r := range []string{"778899", "Livia Duarte Pinho", ";F;", ";34"} {
		if strings.Contains(out, r) {
			t.Errorf("com campos_extras e quase ligado, passou %q: %q", r, out)
		}
	}
	if out, _ := novoTeste(t).Mascarar("RE;NOME_COOPERADO\n778899;Livia Duarte Pinho\n"); !strings.Contains(out, "778899") {
		t.Errorf("sem campos_extras não deveria reconhecer: %q", out)
	}
	if _, err := NovoMasker(config.Config{CamposExtras: []config.CampoExtra{{Tipo: "xpto", Palavras: []string{"a"}}}}, chaveTeste, nil, nil); err == nil {
		t.Error("tipo desconhecido em campos_extras deveria dar erro")
	}
	cols := m.DescreverCampos("RE;NOME_COOPERADO;NOM_SISTEMA\n1;a;b\n")
	if len(cols) != 3 || cols[0].Classe != "usuario" || cols[1].Classe != "nome" || cols[2].Classe != "" {
		t.Errorf("DescreverCampos: %+v", cols)
	}
}

// Item 3 (revisão externa): nomes de coluna de documento genéricos.
func TestRotulosDeDocumento(t *testing.T) {
	for _, r := range []string{"document", "documento", "userDocument", "user_document", "num_documento", "tax_id", "taxpayer", "taxpayer_id", "nr_documento_cliente", "CPF_CNPJ"} {
		if classeRotulo(r) == "" {
			t.Errorf("%q deveria ser reconhecido como documento", r)
		}
	}
	for _, r := range []string{"document_type", "tipo_documento", "documento_url"} {
		if c := classeRotulo(r); c != "" {
			t.Errorf("%q não guarda o número do documento, mas virou %q", r, c)
		}
	}
}

// O desenho mascara nomes de recursos (tabela, coluna de cabeçalho, schema de catálogo) de
// propósito. Nesses textos, o que sai mascarado tem que ser só pseudônimo de objeto: nenhum
// detector de dado pessoal pode disparar.
func TestDesenhoMascaraRecursos(t *testing.T) {
	for _, s := range []string{
		"table_name;row_count\nTB_CLIENTE;1500\nTB_CONTA;90\n",
		"INSERT INTO produtos (id, nome_produto, preco) VALUES (1, 'CDB Liquidez Diaria', 10.5);",
		"SELECT nome_cliente, cpf FROM clientes WHERE dt_nascimento > '1990-01-01'", // nomes de coluna numa consulta
		"| Column | Data Type | Description |\n|---|---|---|\n| REQUEST_ID | VARCHAR | id |\n| USER_ID | NUMBER | Identifier of the user |\n| USER_NAME | VARCHAR | Name of the user |\n| USAGE_TIME | TIMESTAMP | when |\n",
		"coluna;tipo;descricao\nNOM_CLIENTE;VARCHAR;nome\nNUM_CPF;VARCHAR;documento\nVL_SALDO;NUMBER;saldo\n",
		"NOM_SISTEMA;COD_USU_OWNER;NOM_OWNER\nSISTEMA_X;x;y\n",
		"TABLE_SCHEMA,TABLE_NAME,ROW_COUNT\nGOLD,TB_CLIENTE,1500\n",
		`{"name": "Bash", "user": "root", "table_name": "TB_X"}`,
		"SELECT * FROM vendas WHERE id = 12345678901 AND ts > '2026-10-02 18:35:00'",
		"bank_name,bank_code\nBanco X,341\n",
	} {
		_, ents := novoTeste(t).Mascarar(s)
		obj := 0
		for _, e := range ents {
			if strings.HasPrefix(e.Tipo, "obj.") {
				obj++
			} else {
				t.Errorf("detector de dado pessoal disparou (%s) em %q", e.Tipo, s)
			}
		}
		if obj == 0 {
			t.Errorf("nenhum nome de recurso mascarado em %q", s)
		}
	}
}
