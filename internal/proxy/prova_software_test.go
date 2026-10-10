package proxy

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Prova de software (mask/memoria_software.go): o nome padrão de um software público, decidido
// como banco numa DSN, só deixa de ser espalhado pela conversa quando a própria conversa prova
// que é o software (instalado, importado, rodado). Sem prova, ou com nome que é palavra real,
// nome inventado do cliente ou definido no projeto, continua mascarado em todo lugar.
func TestProvaSoftware(t *testing.T) {
	dsn := func(banco string) [2]string {
		return [2]string{"grep DB_URL .env", "DB_URL=postgresql://svc:x@pg-hx-prd:5432/" + banco + "\n"}
	}
	casos := []struct {
		nome    string
		palavra string
		pares   [][2]string
		frase   string
		claro   bool // a palavra fica em claro na frase final do usuário
	}{
		{"datahub sem prova", "datahub", [][2]string{dsn("datahub")}, "e a doc do datahub fala o que?", false},
		{"datahub importado", "datahub", [][2]string{dsn("datahub"), {"head -1 emit.py", "from datahub.emitter.rest_emitter import DatahubRestEmitter\n"}},
			"e a doc do datahub fala o que?", true},
		{"airflow rodado", "airflow", [][2]string{dsn("airflow"), {"airflow dags list | head -3", "dag_id | fileloc\n"}},
			"a UI do airflow não sobe", true},
		{"codinome que é palavra real, importado", "polaris", [][2]string{dsn("polaris"), {"head -1 app.py", "import polaris\n"}},
			"o polaris caiu de novo", false},
		{"pacote inventado do cliente, instalado", "vendashx", [][2]string{dsn("vendashx"), {"pip install vendashx", "Successfully installed vendashx-1.0\n"}},
			"o vendashx caiu de novo", false},
		{"definido no projeto (sombreamento)", "superset", [][2]string{dsn("superset"), {"cat pyproject.toml", "[project]\nname = \"superset\"\n"},
			{"head -1 main.py", "import superset\n"}}, "o superset caiu de novo", false},
	}
	for _, c := range casos {
		var corpos [][]byte
		px := rtProxy(t, t.TempDir(), &corpos, func(b []byte, w http.ResponseWriter) { rtTexto("ok", w) })
		msgs := rtTurnos("o serviço caiu", c.pares...)
		msgs = append(msgs, map[string]any{"role": "user", "content": c.frase})
		rtEnviar(t, px, msgs)
		enviado := string(corpos[len(corpos)-1])
		if rtTok(enviado, "pg-hx-prd") != 0 {
			t.Errorf("%s: o host do cliente vazou", c.nome)
		}
		// a DSN sempre mascara o banco; a frase final só fica em claro com prova
		if claro := rtTok(ultimaMensagem(t, corpos[len(corpos)-1]), c.palavra) > 0; claro != c.claro {
			t.Errorf("%s: %q em claro na frase final = %v, queria %v", c.nome, c.palavra, claro, c.claro)
		}
	}
}

// ultimaMensagem: o conteúdo da última mensagem do corpo enviado à API.
func ultimaMensagem(t *testing.T, corpo []byte) string {
	t.Helper()
	var r struct{ Messages []struct{ Content any } }
	if err := json.Unmarshal(corpo, &r); err != nil || len(r.Messages) == 0 {
		t.Fatal("corpo sem mensagens")
	}
	b, _ := json.Marshal(r.Messages[len(r.Messages)-1].Content)
	return string(b)
}

// Origem por caminho: o arquivo lido de uma dependência pública é código do fornecedor; os nomes
// dele são mascarados ali, mas não ensinam a memória da conversa. O arquivo do projeto ensina.
func TestOrigemPorCaminho(t *testing.T) {
	for _, c := range []struct {
		nome, caminho, tabela string
		claro                 bool
	}{
		{"dependência pública", "/home/ana/.venv/lib/python3.11/site-packages/datahub/ingestion/source/sql/mysql.py", "metadata_aspect_v2", true},
		{"projeto do cliente", "/home/ana/seguradora-hx/etl/consulta.py", "sinistros_hx", false},
		{"pacote interno instalado", "/home/ana/.venv/lib/python3.11/site-packages/vendashx_core/consulta.py", "sinistros_hx", false},
	} {
		var corpos [][]byte
		px := rtProxy(t, t.TempDir(), &corpos, func(b []byte, w http.ResponseWriter) { rtTexto("ok", w) })
		msgs := []any{
			map[string]any{"role": "user", "content": "olha isso"},
			map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "r1", "name": "Read", "input": map[string]any{"file_path": c.caminho}}}},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "r1",
				"content": "     1\tQUERY = \"SELECT urn, aspect FROM " + c.tabela + " WHERE version = 0\"\n     2\tdef ler(conn):\n"}}},
			map[string]any{"role": "user", "content": "por que a " + c.tabela + " cresce tanto?"},
		}
		rtEnviar(t, px, msgs)
		if claro := rtTok(ultimaMensagem(t, corpos[len(corpos)-1]), c.tabela) > 0; claro != c.claro {
			t.Errorf("%s: %q em claro na frase = %v, queria %v", c.nome, c.tabela, claro, c.claro)
		}
	}
}
