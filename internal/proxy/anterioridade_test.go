package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Anterioridade (anterioridade.go), com nomes inventados.

func rtAssist(texto string) any {
	return map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": texto}}}
}

func rtAPI(t *testing.T, sistema string, msgs []any) string {
	t.Helper()
	var corpos [][]byte
	px := rtProxy(t, t.TempDir(), &corpos, func(b []byte, w http.ResponseWriter) { rtTexto("ok", w) })
	corpo := map[string]any{"model": "x", "max_tokens": 10, "messages": msgs}
	if sistema != "" {
		corpo["system"] = sistema
	}
	b, _ := json.Marshal(corpo)
	rtEnviarCorpo(t, px, b)
	return string(corpos[0])
}

// O modelo escreve a query com as colunas das views de sistema (conhecimento dele); depois a
// saída as mostra no cabeçalho e o usuário as cita. Ficam legíveis; os valores do cliente não.
func TestAnterioridadeSQLDeSistemaAntesDaSaida(t *testing.T) {
	q := "SELECT TAG_NAME, TAG_VALUE, APPLY_METHOD, OBJECT_NAME FROM SNOWFLAKE.ACCOUNT_USAGE.TAG_REFERENCES WHERE TAG_NAME = 'OWNER'"
	msgs := []any{map[string]any{"role": "user", "content": "quem é o owner das views?"}}
	msgs = append(msgs, rtTurnos("", [2]string{`snow sql -q "` + q + `"`,
		"TAG_NAME | TAG_VALUE      | APPLY_METHOD | OBJECT_NAME\nOWNER    | squad_receita  | INHERITED    | VW_FATURAMENTO_DIA\nOWNER    | squad_credito  | MANUAL       | TB_CLIENTE_PJ\n"})[1:]...)
	msgs = append(msgs, map[string]any{"role": "user", "content": "por que o APPLY_METHOD do TB_CLIENTE_PJ é MANUAL e o TAG_VALUE é squad_credito?"})
	api := rtAPI(t, "", msgs)
	for _, p := range []string{"TAG_NAME", "TAG_VALUE", "APPLY_METHOD", "OBJECT_NAME", "TAG_REFERENCES"} {
		if rtTok(api, p) < rtTok(q, p) {
			t.Errorf("%s (o modelo escreveu antes) foi mascarado", p)
		}
	}
	// (os valores de TAG_VALUE, como squad_receita, não são nome de objeto: lacuna conhecida,
	// anterior à anterioridade)
	for _, s := range []string{"VW_FATURAMENTO_DIA", "TB_CLIENTE_PJ"} {
		if rtTok(api, s) > 0 {
			t.Errorf("%s (do cliente) foi em claro", s)
		}
	}
}

// O nome que o modelo escreve depois de vê-lo num dado (ou que o proxy traduziu) não é dele.
func TestAnterioridadeCopiaNaoEDoModelo(t *testing.T) {
	msgs := rtTurnos("o que tem no cluster?", [2]string{"kubectl get ns", "NAME             STATUS   AGE\nkube-system      Active   400d\npagto-core-prd   Active   210d\n"})
	msgs = append(msgs, rtAssist("O namespace pagto-core-prd tem os pods de pagamento; vou olhar os logs."))
	msgs = append(msgs, rtTurnos("", [2]string{"kubectl logs -n pagto-core-prd deploy/api --tail 2", "ERROR timeout em pagto-core-prd\n"})[1:]...)
	api := rtAPI(t, "", msgs)
	if n := rtTok(api, "pagto-core-prd"); n > 0 {
		t.Errorf("nome que o modelo copiou de um dado foi em claro %d vez(es)", n)
	}
}

// O system (diretório de trabalho, CLAUDE.md) é dado: o nome que o modelo repete dele não é do
// modelo. Se o proxy mascara o nome no system, mascara também quando o modelo o escreve.
func TestAnterioridadeSystemEDado(t *testing.T) {
	sistema := "Working directory: /home/u/trabalho/acme-cobranca-core\nO projeto acme-cobranca-core usa o banco dw_cobranca_prd."
	msgs := []any{map[string]any{"role": "user", "content": "lista os arquivos"}}
	msgs = append(msgs, rtTurnos("", [2]string{"ls /home/u/trabalho/acme-cobranca-core && psql -d dw_cobranca_prd -c 'select 1'", "README.md\nsrc\n"})[1:]...)
	api := rtAPI(t, sistema, msgs)
	var c struct {
		System   any
		Messages []any
	}
	json.Unmarshal([]byte(api), &c)
	sys, _ := json.Marshal(c.System)
	ms, _ := json.Marshal(c.Messages)
	for _, n := range []string{"acme-cobranca-core", "dw_cobranca_prd"} {
		if rtTok(string(sys), n) == 0 && rtTok(string(ms), n) > 0 {
			t.Errorf("%s: mascarado no system, mas em claro no que o modelo escreveu", n)
		}
	}
}

// O modelo instala pacotes pelo nome (conhecimento dele); a lista de dependências que aparece
// depois continua legível.
func TestAnterioridadePacotes(t *testing.T) {
	msgs := rtTurnos("prepara o ambiente", [2]string{"pip install apache-airflow flask-appbuilder marshmallow-sqlalchemy", "Successfully installed apache-airflow-2.9.1 flask-appbuilder-4.4.1 marshmallow-sqlalchemy-0.28.2\n"})
	msgs = append(msgs, rtTurnos("", [2]string{"pip freeze", "apache-airflow==2.9.1\nflask-appbuilder==4.4.1\nmarshmallow-sqlalchemy==0.28.2\n"})[1:]...)
	api := rtAPI(t, "", msgs)
	for _, p := range []string{"apache-airflow", "flask-appbuilder", "marshmallow-sqlalchemy"} {
		if rtTok(api, p) < 3 {
			t.Errorf("pacote %s mascarado", p)
		}
	}
}

// Risco medido (não falha): o modelo chuta um nome genérico antes de ver os dados, e o cliente
// tem um objeto com esse nome. O nome genérico fica legível; o específico, não.
func TestAnterioridadeChuteGenerico(t *testing.T) {
	msgs := rtTurnos("me mostra os clientes", [2]string{`psql -c "SELECT * FROM customers LIMIT 2"`, "ERROR: relation \"customers\" does not exist"})
	msgs = append(msgs, rtTurnos("", [2]string{`psql -c "\dt"`, " Schema |      Name       | Type  \n--------+-----------------+-------\n public | customers       | table\n public | tb_cliente_pj_x | table\n public | tb_contrato_y   | table\n"})[1:]...)
	api := rtAPI(t, "", msgs)
	t.Logf("chute genérico 'customers' em claro: %v (risco aceito); específico em claro: %v",
		rtTok(api, "customers") > 0, rtTok(api, "tb_cliente_pj_x") > 0)
	if rtTok(api, "tb_cliente_pj_x") > 0 || rtTok(api, "tb_contrato_y") > 0 {
		t.Errorf("nome específico do cliente foi em claro")
	}
}

func rtEnviarCorpo(t *testing.T, px *httptest.Server, b []byte) {
	t.Helper()
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

// Resultado de ferramenta do lado da API (dentro da mensagem do modelo) é dado: o nome que vem
// nele não vira conhecimento do modelo.
func TestAnterioridadeResultadoDentroDoModeloEDado(t *testing.T) {
	msgs := []any{map[string]any{"role": "user", "content": "roda o script no arquivo"},
		map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "server_tool_use", "id": "s1", "name": "code_execution", "input": map[string]any{"code": "print(open('dados.csv').read())"}},
			map[string]any{"type": "code_execution_tool_result", "tool_use_id": "s1", "content": map[string]any{"type": "code_execution_result", "stdout": "tabela,coluna\ntb_contrato_zq,num_cpf_socio_zq\n"}},
			map[string]any{"type": "text", "text": "Vi a tb_contrato_zq."}}},
	}
	a := &anterioridade{dados: map[string]bool{}, modelo: map[string]bool{}, traduz: map[string]bool{}}
	a.emDados = true
	a.tudo(msgs[0].(map[string]any)["content"])
	a.emDados = false
	a.tudo(msgs[1].(map[string]any)["content"])
	if a.modelo["tb_contrato_zq"] || a.modelo["num_cpf_socio_zq"] {
		t.Errorf("nome de um resultado de ferramenta da API virou conhecimento do modelo: %v", a.modelo)
	}
	if !a.modelo["code_execution"] {
		t.Errorf("o que o modelo escreveu (a chamada) devia contar: %v", a.modelo)
	}
}

// Uma saída que só MENCIONA a palavra (um changelog público que cita tag_name) não tira a autoria
// do modelo: quando ele a escreve numa query, é dele, mesmo que depois um dado a decida. O nome
// que o usuário digitou continua sendo do cliente (TestRastreamentoSQLDoAssistente).
func TestAnterioridadeMencaoNaoTiraAutoria(t *testing.T) {
	changelog := "## 0.12.1\n\n- #9244: o construtor de tag mudou; TagKey(\"x\", [\"tag_name\"]) não é mais aceito, use TagKey(\"tag_name\").\n"
	q := "SELECT tag_name, tag_value, object_name FROM snowflake.account_usage.tag_references"
	msgs := rtTurnos("o que mudou na versão nova?", [2]string{"cat CHANGELOG.md", changelog})
	msgs = append(msgs, rtTurnos("", [2]string{"snow sql -q \"" + q + "\"", "TAG_NAME | TAG_VALUE | OBJECT_NAME\nOWNER    | squad_x   | TB_CLIENTE_PJ\n"})[1:]...)
	msgs = append(msgs, rtTurnos("", [2]string{"cat consulta.py", "q = \"\"\"\n    SELECT tag_name AS \"TAG_NAME\",\n           object_name AS \"OBJETO\"\n    FROM snowflake.account_usage.tag_references\n\"\"\"\n"})[1:]...)
	api := rtAPI(t, "", msgs)
	if rtTok(api, "tag_name") < rtTok(q, "tag_name")+1 {
		t.Errorf("tag_name (o modelo escreveu antes de um dado decidi-lo) foi mascarado")
	}
	if rtTok(api, "TB_CLIENTE_PJ") > 0 {
		t.Errorf("TB_CLIENTE_PJ (do cliente) foi em claro")
	}
}

// Palavra comum decidida como nome num dado (a tabela CONTA existe) continua nome no dado, mas
// na prosa do modelo é a palavra; nome com cara de identificador continua mascarado na prosa.
func TestPalavraComumNaProsaDoModelo(t *testing.T) {
	ddl := "CREATE TABLE FIN.CONTA (ID INTEGER NOT NULL PRIMARY KEY, NM_TITULAR VARCHAR(10));\nCREATE TABLE FIN.TB_PEDIDO_X9 (ID INTEGER);\n"
	msgs := rtTurnos("cria as tabelas do lab", [2]string{"cat ddl.sql", ddl})
	msgs = append(msgs, rtAssist("Daqui só acesso a conta trial do laboratório; a TB_PEDIDO_X9 ficou vazia."))
	msgs = append(msgs, map[string]any{"role": "user", "content": "ok"})
	api := rtAPI(t, "", msgs)
	if !strings.Contains(api, "acesso a conta trial") {
		t.Errorf("palavra comum trocada na prosa do modelo")
	}
	if rtTok(api, "TB_PEDIDO_X9") > 0 {
		t.Errorf("nome com cara de identificador em claro")
	}
	if strings.Contains(api, ".CONTA (") {
		t.Errorf("CONTA no DDL (onde é nome) devia continuar mascarada")
	}
}

// O mesmo no texto do usuário: a palavra comum que chegou por contágio fica como palavra; no DDL
// que ele cola na mesma mensagem, onde um leitor a decide, continua mascarada.
func TestPalavraComumNaProsaDoUsuario(t *testing.T) {
	ddl := "CREATE TABLE FIN.CONTA (ID INTEGER NOT NULL PRIMARY KEY, NM_TITULAR VARCHAR(10));\n"
	msgs := rtTurnos("cria as tabelas do lab", [2]string{"cat ddl.sql", ddl})
	msgs = append(msgs, rtAssist("Criei."))
	msgs = append(msgs, map[string]any{"role": "user", "content": "em qual conta você rodaria? olha esse DDL:\nCREATE TABLE FIN.CONTA (ID INTEGER, NM_TITULAR VARCHAR(10));"})
	api := rtAPI(t, "", msgs)
	if !strings.Contains(api, "em qual conta você rodaria") {
		t.Errorf("palavra comum trocada no texto do usuário")
	}
	if strings.Contains(api, ".CONTA (") {
		t.Errorf("CONTA no DDL colado (onde é nome) devia continuar mascarada")
	}
}
