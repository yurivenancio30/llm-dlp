package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Rastreamento (mask/rastreamento.go) de ponta a ponta, com nomes inventados.

var rePsRT = regexp.MustCompile(`(?i)\b(host|db|sch|t|c|proc|idx|usr|ns|svc|bkt|top|repo|org|pkg|dir|acc|obj)_[a-z2-7]{8}\b`)

// desescapar: no JSON, a quebra de linha é "\n" (a palavra do começo de uma linha ficaria colada
// no "n" e não seria contada).
var desescapar = strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\"`, `"`, `\\`, `\`)

// rtTok: ocorrências de w como palavra inteira ("_" conta como letra), no texto desescapado.
func rtTok(s, w string) int {
	s = desescapar.Replace(s)
	return len(regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_])`+regexp.QuoteMeta(w)+`(?:$|[^A-Za-z0-9_])`).FindAllStringIndex(s, -1))
}

// rtProxy: proxy com estado em dir (reabrir o mesmo dir é um reinício) e upstream falso.
func rtProxy(t *testing.T, dir string, corpos *[][]byte, resp func([]byte, http.ResponseWriter)) *httptest.Server {
	cfg := config.Padrao()
	vs, _ := mask.CarregarVistos(dir + "/vistos.json")
	m, err := mask.NovoMasker(cfg, []byte("0123456789abcdef0123456789abcdef"), nil, vs)
	if err != nil {
		t.Fatal(err)
	}
	m.UsarEnviados(dir+"/enviados.log", "teste")
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*corpos = append(*corpos, b)
		resp(b, w)
	}))
	cfg.Upstream = up.URL
	px := httptest.NewServer(Novo(cfg, m, vs, log.New(io.Discard, "", 0)))
	t.Cleanup(func() { up.Close(); px.Close() })
	return px
}

func rtTexto(texto string, w http.ResponseWriter) {
	w.Header().Set("content-type", "application/json")
	b, _ := json.Marshal(map[string]any{"type": "message", "role": "assistant",
		"content": []any{map[string]any{"type": "text", "text": texto}}})
	w.Write(b)
}

var rtSeq int

// rtTurnos: user, depois pares (comando Bash, saída).
func rtTurnos(pedido string, pares ...[2]string) []any {
	msgs := []any{map[string]any{"role": "user", "content": pedido}}
	for i, p := range pares {
		rtSeq++
		id := fmt.Sprintf("c%d", rtSeq) // único entre chamadas (o proxy liga resultado a comando pelo id)
		_ = i
		msgs = append(msgs,
			map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": id, "name": "Bash", "input": map[string]any{"command": p[0]}}}},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": p[1]}}})
	}
	return msgs
}

func rtEnviar(t *testing.T, px *httptest.Server, msgs []any) string {
	t.Helper()
	corpo, _ := json.Marshal(map[string]any{"model": "x", "max_tokens": 10, "messages": msgs})
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(corpo))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var r struct{ Content []struct{ Text string } }
	json.Unmarshal(out, &r)
	if len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

// O modelo cita os códigos que recebeu (um em minúsculas) e escreve SQL de sistema; o cliente
// guarda a resposta traduzida e reenvia; 3 turnos, cada um com o proxy reiniciado, e por fim o
// texto do modelo alterado (não bate com o registro). Nunca nome real para a API, nunca código
// para o cliente, nunca público trocado.
func TestRastreamentoIdaEVolta(t *testing.T) {
	segredos := []string{"pagto-core-prd", "billing-v2", "NUM_CPF_TITULAR", "DES_ENDERECO_RES", "tb_pedido_cli"}
	publicos := []string{"OBJECT_DEPENDENCIES", "REFERENCED_OBJECT_NAME", "NIVEL", "kube-system"}
	dir := t.TempDir()
	modelo := func(b []byte, w http.ResponseWriter) {
		vis := map[string]bool{}
		var l []string
		for _, c := range rePsRT.FindAllString(string(b), -1) {
			if !vis[c] {
				vis[c] = true
				l = append(l, c)
			}
		}
		txt := "Vi estes: " + strings.Join(l, ", ")
		if len(l) > 0 {
			txt += "; e em minúsculas: " + strings.ToLower(l[0])
		}
		rtTexto(txt+"\n```sql\nSELECT 1 AS NIVEL, REFERENCED_OBJECT_NAME FROM SNOWFLAKE.ACCOUNT_USAGE.OBJECT_DEPENDENCIES;\n```", w)
	}
	hist := rtTurnos("o que tem no cluster e no catálogo?",
		[2]string{"kubectl get ns", "NAME             STATUS   AGE\nkube-system      Active   400d\npagto-core-prd   Active   210d\nbilling-v2       Active   180d\n"},
		[2]string{"head -4 catalogo.csv", "plataforma,objeto,coluna,tipo\nmssql,erp.dbo.tb_pedido_cli,NUM_CPF_TITULAR,VARCHAR(14)\noracle,CAD.TB_PESSOA,DES_ENDERECO_RES,VARCHAR(60)\npostgres,app.public.tb_user,cod_x_y,INT\n"})
	conferir := func(rot string, hist []any) string {
		var corpos [][]byte
		px := rtProxy(t, dir, &corpos, modelo)
		txt := rtEnviar(t, px, hist)
		api := string(corpos[0])
		o, _ := json.Marshal(hist)
		for _, s := range segredos {
			if rtTok(api, s) > 0 {
				t.Errorf("%s: %s foi em claro para a API", rot, s)
			}
		}
		for _, p := range publicos {
			if rtTok(string(o), p) > rtTok(api, p) {
				t.Errorf("%s: público %s trocado", rot, p)
			}
		}
		if n := len(rePsRT.FindAllString(txt, -1)); n > 0 {
			t.Errorf("%s: %d código(s) chegaram ao cliente: %s", rot, n, txt)
		}
		return txt
	}
	for i := 1; i <= 3; i++ {
		txt := conferir(fmt.Sprintf("turno %d", i), hist)
		hist = append(hist, map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": txt}}},
			map[string]any{"role": "user", "content": "continua"})
	}
	alt := make([]any, len(hist))
	for i, m := range hist {
		alt[i] = m
		mm := m.(map[string]any)
		if c, ok := mm["content"].([]any); ok && mm["role"] == "assistant" {
			if b, ok := c[0].(map[string]any); ok && b["type"] == "text" {
				alt[i] = map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": b["text"].(string) + " (editado)"}}}
			}
		}
	}
	conferir("texto do modelo alterado", alt)
}

// O arquivo já foi visto com cabeçalho; depois o mesmo arquivo aparece em comandos compostos
// (uniq -c, grep com prefixo), em linhas com pipe e num dicionário Python de uma linha, com
// nomes que o head não mostrou. A decisão é do valor, a dedução vale na mesma fonte.
func TestRastreamentoFormatos(t *testing.T) {
	vistas := []string{"NUM_CPF_TITULAR", "DES_ENDERECO_RES", "cod_raca_cor", "QTD_TENT_LOGIN"}
	novas := []string{"NUM_CEL_CLI", "eml_cli_princ", "DTA_NSC_CLI", "NOM_MAE", "VLR_RENDA_MENSAL", "END_RES_CLI", "NR_CNPJ_EMPRESA", "COD_CGC_EMP"}
	tipos := []string{"VARCHAR(11)", "\"NUMBER(18,2)\"", "INT", "TEXT"}
	plat := []string{"mssql", "oracle", "postgres", "DB2"}
	obj := []string{"VENDAS.dbo.TB_ALFA", "FIN.TB_BETA", "app.public.tb_gama", "FIN.TB_DELTA"}
	head := "plataforma,objeto,coluna,tipo\n"
	for i, c := range vistas {
		head += plat[i] + "," + obj[i] + "," + c + "," + tipos[i] + "\n"
	}
	todas := append(append([]string{}, vistas...), novas...)
	comp, pipe, dict := "", "", "{"
	for i, c := range todas {
		comp += fmt.Sprintf("      %d %s\n", i+1, c)
	}
	comp += "---\n"
	for i, c := range todas {
		comp += "catalogo_colunas.csv:" + plat[i%4] + "," + obj[i%4] + "," + c + "," + tipos[i%4] + "\n"
		pipe += plat[i%4] + " |" + obj[i%4] + " |" + c + " |40 |.*CPF.*\n"
		dict += fmt.Sprintf("'%s': %d, ", c, i)
	}
	dict += "}\n"
	var corpos [][]byte
	px := rtProxy(t, t.TempDir(), &corpos, func(b []byte, w http.ResponseWriter) { rtTexto("ok", w) })
	rtEnviar(t, px, rtTurnos("me mostra as colunas que caem nas regras",
		[2]string{"head -5 catalogo_colunas.csv", head},
		[2]string{"awk -F, 'NR>1{print $3}' catalogo_colunas.csv | sort | uniq -c; echo ---; grep -iE 'cpf|end' catalogo_colunas.csv /dev/null", comp},
		[2]string{"python3 - <<'EOF'\nimport csv\nrows=list(csv.reader(open('catalogo_colunas.csv')))\nEOF", pipe},
		[2]string{"python3 - <<'EOF'\nimport csv, collections\nc=collections.Counter(r[2] for r in csv.reader(open('catalogo_colunas.csv')))\nEOF", dict}))
	var api struct{ Messages []map[string]any }
	json.Unmarshal(corpos[0], &api)
	for i, m := range api.Messages {
		b, _ := json.Marshal(m)
		if !strings.Contains(string(b), "tool_result") {
			continue
		}
		for _, c := range todas {
			if rtTok(string(b), c) > 0 {
				t.Errorf("mensagem %d: coluna %s em claro", i, c)
			}
		}
	}
}

// A dedução só vale com sementes provadas na mesma fonte: uma lista de pacotes de outro arquivo,
// em que alguns nomes coincidem com tabelas já vistas, não vira lista de tabelas.
func TestRastreamentoDeducaoMesmaFonte(t *testing.T) {
	sql := "CREATE TABLE celery (id INT);\nCREATE TABLE redis_jobs (id INT);\nSELECT * FROM celery JOIN kombu ON 1=1;\n"
	pacotes := "apache-airflow==2.9.1\ncelery==5.3.6\nkombu==5.3.4\nflask-appbuilder==4.4.1\nmarshmallow-sqlalchemy==0.28.2\nsqlalchemy-utils==0.41.2\n"
	var corpos [][]byte
	px := rtProxy(t, t.TempDir(), &corpos, func(b []byte, w http.ResponseWriter) { rtTexto("ok", w) })
	rtEnviar(t, px, rtTurnos("analisa o projeto",
		[2]string{"cat sql/schema.sql", sql},
		[2]string{"cat airflow/constraints.txt", pacotes}))
	var api struct{ Messages []map[string]any }
	json.Unmarshal(corpos[0], &api)
	b, _ := json.Marshal(api.Messages[len(api.Messages)-1])
	for _, p := range []string{"apache-airflow", "flask-appbuilder", "marshmallow-sqlalchemy", "sqlalchemy-utils"} {
		if rtTok(string(b), p) == 0 {
			t.Errorf("pacote %s virou nome do cliente por dedução de outra fonte: %s", p, b)
		}
	}
}

// O modelo escreve SQL sobre as visões de sistema; os nomes do cliente que ele cita vieram de
// uma saída anterior. Na ida seguinte, o vocabulário público chega intacto e os nomes do cliente
// continuam mascarados.
func TestRastreamentoSQLDoAssistente(t *testing.T) {
	sqlLin := "WITH RECURSIVE deps AS (\n  SELECT 1 AS NIVEL, REFERENCED_DATABASE AS OBJECT_DATABASE, REFERENCED_OBJECT_NAME AS OBJECT_NAME\n  FROM SNOWFLAKE.ACCOUNT_USAGE.OBJECT_DEPENDENCIES\n  WHERE REFERENCING_DATABASE = 'DW_COMERCIAL_PRD' AND REFERENCING_SCHEMA = 'MART_VENDAS' AND REFERENCING_OBJECT_NAME = 'VW_FATURAMENTO_DIA'\n)\nSELECT NIVEL, OBJECT_NAME, t.TAG_NAME, t.TAG_VALUE FROM deps c LEFT JOIN SNOWFLAKE.ACCOUNT_USAGE.TAG_REFERENCES t ON t.OBJECT_NAME = c.OBJECT_NAME;\n"
	msgs := rtTurnos("De onde a view VW_FATURAMENTO_DIA herda a tag de owner?",
		[2]string{"snow sql -q \"SHOW VIEWS LIKE 'VW_FATURAMENTO_DIA' IN ACCOUNT\"", "+---------------------+--------------------+---------------+\n| name                | database_name      | schema_name   |\n|---------------------+--------------------+---------------|\n| VW_FATURAMENTO_DIA  | DW_COMERCIAL_PRD   | MART_VENDAS   |\n+---------------------+--------------------+---------------+\n"})
	msgs = append(msgs, map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "Esta consulta sobe a cadeia:\n```sql\n" + sqlLin + "```"}}},
		map[string]any{"role": "user", "content": "deu erro na linha 3"})
	var corpos [][]byte
	px := rtProxy(t, t.TempDir(), &corpos, func(b []byte, w http.ResponseWriter) { rtTexto("ok", w) })
	rtEnviar(t, px, msgs)
	api := string(corpos[0])
	for _, p := range []string{"OBJECT_DEPENDENCIES", "REFERENCED_DATABASE", "REFERENCED_OBJECT_NAME", "REFERENCING_DATABASE", "REFERENCING_SCHEMA", "REFERENCING_OBJECT_NAME", "TAG_REFERENCES", "TAG_NAME", "TAG_VALUE", "OBJECT_DATABASE", "OBJECT_NAME", "NIVEL", "deps"} {
		if rtTok(sqlLin, p) > rtTok(api, p) {
			t.Errorf("público %s trocado no SQL do modelo", p)
		}
	}
	for _, s := range []string{"DW_COMERCIAL_PRD", "MART_VENDAS", "VW_FATURAMENTO_DIA"} {
		if rtTok(api, s) > 0 {
			t.Errorf("%s foi em claro para a API", s)
		}
	}
}

// DESC/SHOW de um objeto de catálogo de sistema lista estrutura pública (as colunas de
// SNOWFLAKE.ACCOUNT_USAGE.TAG_REFERENCES são documentação); o DESC de uma tabela do cliente
// continua mascarando as colunas.
func TestRastreamentoCatalogoDeSistema(t *testing.T) {
	var corpos [][]byte
	px := rtProxy(t, t.TempDir(), &corpos, func(b []byte, w http.ResponseWriter) { rtTexto("ok", w) })
	rtEnviar(t, px, rtTurnos("de onde vem a tag de owner?",
		[2]string{`snow sql -q "DESC VIEW SNOWFLAKE.ACCOUNT_USAGE.TAG_REFERENCES"`, "name             type    kind\nTAG_NAME         VARCHAR COLUMN\nTAG_VALUE        VARCHAR COLUMN\nAPPLY_METHOD     VARCHAR COLUMN\nOBJECT_DATABASE  VARCHAR COLUMN\n"},
		[2]string{`snow sql -q "DESC TABLE DW_PRD.MART.TB_CLI_PJ"`, "name             type    kind\nNUM_CNPJ_MATRIZ  VARCHAR COLUMN\nDES_RAZAO_SOC    VARCHAR COLUMN\nDTA_ABERTURA     DATE    COLUMN\n"}))
	api := string(corpos[0])
	for _, p := range []string{"TAG_NAME", "TAG_VALUE", "APPLY_METHOD", "OBJECT_DATABASE"} {
		if rtTok(api, p) == 0 {
			t.Errorf("coluna pública %s (catálogo de sistema) foi mascarada", p)
		}
	}
	for _, s := range []string{"NUM_CNPJ_MATRIZ", "DES_RAZAO_SOC", "DTA_ABERTURA"} {
		if rtTok(api, s) > 0 {
			t.Errorf("coluna do cliente %s foi em claro", s)
		}
	}
}
