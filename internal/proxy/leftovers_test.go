package proxy

import (
	"encoding/json"
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

// A mesma pessoa escrita em caixas diferentes volta cada uma como estava: o relatório que o
// modelo escreve com "MARIA DA SILVA" não grava "Maria da Silva" (e um Edit com old_string em
// maiúsculas ainda casa com o arquivo). Caso tirado da forma de 7 escritas que voltavam
// diferentes numa sessão real (os nomes aqui são inventados).
func TestPersonReturnsEachSpellingCase(t *testing.T) {
	const text = `[{"nome_cliente": "Maria da Silva", "cidade": "Recife"},` + "\n" +
		` {"nome_cliente": "MARIA DA SILVA", "cidade": "Recife"},` + "\n" +
		` {"nome_cliente": "maria da silva", "cidade": "Recife"},` + "\n" +
		` {"nome_cliente": "João Pereira Santos", "cidade": "Natal"},` + "\n" +
		` {"nome_cliente": "JOÃO PEREIRA SANTOS", "cidade": "Natal"}]`
	var bodies [][]byte
	px := buildMem(t, t.TempDir(), &bodies, func(body []byte, w http.ResponseWriter) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.Unmarshal(body, &req)
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"type": "message", "role": "assistant",
			"content": []any{map[string]any{"type": "text", "text": req.Messages[len(req.Messages)-1].Content}}})
	})
	back, _ := sendRead(t, px, false, []any{msg("user", text)})
	body := string(bodies[len(bodies)-1])
	for _, real := range []string{"Maria da Silva", "MARIA DA SILVA", "maria da silva", "João Pereira Santos", "JOÃO PEREIRA SANTOS"} {
		if strings.Contains(body, real) {
			t.Errorf("%q saiu em claro", real)
		}
	}
	if back != text {
		t.Errorf("a volta mudou a caixa:\n quer %q\n veio %q", text, back)
	}
}

// buildCaseEcho: o proxy com um termo cadastrado ("sgp", a sigla de uma empresa inventada) e
// uma API falsa que responde com a resposta que resp monta a partir do corpo que recebeu.
func buildCaseEcho(t *testing.T, bodies *[][]byte, resp fakeResp) *httptest.Server {
	t.Helper()
	cfg := config.Default()
	cfg.Terms = []config.Term{{Label: "empresa", Values: []string{"sgp"}}}
	m, err := mask.NewMasker(cfg, []byte("0123456789abcdef0123456789abcdef"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*bodies = append(*bodies, b)
		resp(b, w)
	}))
	cfg.Upstream = up.URL
	px := httptest.NewServer(New(cfg, m, nil, log.New(io.Discard, "", 0)))
	t.Cleanup(func() { up.Close(); px.Close() })
	return px
}

// lastReceivedEntry: a entrada (já mascarada) da última chamada de ferramenta que a API recebeu.
func lastReceivedEntry(body []byte) string {
	var req struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	json.Unmarshal(body, &req)
	var in json.RawMessage
	for _, mm := range req.Messages {
		var bs []map[string]json.RawMessage
		if json.Unmarshal(mm.Content, &bs) == nil {
			for _, b := range bs {
				if string(b["type"]) == `"tool_use"` {
					in = b["input"]
				}
			}
		}
	}
	return string(in)
}

// O mesmo nome em grafias de caixa diferentes (a sigla da empresa "SgP", "sgp" e "SGP"; o login
// "etl_carga" e "ETL_CARGA"; a tabela de SQL em maiúsculas num SELECT e em minúsculas noutro) volta
// cada um como estava na escrita do modelo: o comando e o arquivo que ele escreve com os
// pseudônimos que viu saem iguais ao original. Antes, o termo e o login tinham UM pseudônimo para
// todas as grafias e a volta trazia sempre a primeira ("echo SgP sgp SGP" voltava "echo SgP SgP
// SgP"). Caso tirado da forma de 21 escritas que voltavam diferentes, só pela caixa, numa
// repetição de sessão real (os nomes aqui são inventados).
func TestWritingReturnsEachSpellingCase(t *testing.T) {
	var bodies [][]byte
	// o modelo repete a última chamada de ferramenta com os pseudônimos que viu
	px := buildCaseEcho(t, &bodies, func(body []byte, w http.ResponseWriter) {
		w.Header().Set("content-type", "text/event-stream")
		sse(w, "", lastReceivedEntry(body), "", 5)
	})
	cmd := `echo SgP sgp SGP && psql -U etl_carga -c "select valor_bruto from vendas.propostas_ativas" && ` +
		`psql -U ETL_CARGA -c "SELECT VALOR_BRUTO FROM VENDAS.PROPOSTAS_ATIVAS"`
	fileName := "-- carga da SgP (sgp_carga.sql)\n" +
		"SELECT p.ID, p.Valor_Bruto FROM VENDAS.PROPOSTAS_ATIVAS p;\n" +
		"select valor_bruto from vendas.propostas_ativas; -- SGP\n"
	users := `[{"login": "etl_carga", "ativo": true}, {"login": "ETL_CARGA", "ativo": false}]`
	for _, in := range []map[string]any{
		{"command": cmd},
		{"file_path": "/tmp/carga.sql", "content": fileName},
	} {
		usage := map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": in}
		_, back := sendRead(t, px, true, []any{
			msg("user", "a tabela VENDAS.PROPOSTAS_ATIVAS da SgP: SELECT ID, Valor_Bruto FROM VENDAS.PROPOSTAS_ATIVAS"),
			msg("assistant", "vou ver os logins"),
			msg("user", "os logins: "+users),
			msg("assistant", []any{usage})})
		body := string(bodies[len(bodies)-1])
		for _, real := range []string{"SgP", "sgp", "SGP", "etl_carga", "ETL_CARGA", "propostas_ativas", "PROPOSTAS_ATIVAS"} {
			if strings.Contains(body, real) {
				t.Errorf("%q saiu em claro", real)
			}
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(back), &got); err != nil {
			t.Fatalf("entrada inválida: %q", back)
		}
		for k, v := range in {
			if got[k] != v {
				t.Errorf("a escrita voltou com outra caixa (%s):\n quer %q\n veio %q", k, v, got[k])
			}
		}
	}
}

// O modelo reescreve a caixa do pseudônimo da sigla, que só apareceu em maiúsculas ("SGP"): em
// minúsculas num caminho, com a inicial maiúscula numa frase, toda em maiúsculas. Nenhuma dessas
// formas sobra como pseudônimo, e todas voltam à sigla como ela entrou (o modelo que troca a caixa
// do pseudônimo não inventa uma pasta "/dados/sgp" que não existe), como os nomes de objeto.
func TestWritingWithOtherPseudonymCase(t *testing.T) {
	var bodies [][]byte
	reCompany := regexp.MustCompile(`\bEMPRESA-[a-z2-7]{8}\b`)
	px := buildCaseEcho(t, &bodies, func(body []byte, w http.ResponseWriter) {
		emp := reCompany.FindString(string(body))
		cmd := "ls /dados/" + strings.ToLower(emp) + "/ && echo " + "Empresa" + strings.TrimPrefix(emp, "EMPRESA") + " " + strings.ToUpper(emp)
		arg, _ := json.Marshal(map[string]string{"command": cmd})
		w.Header().Set("content-type", "text/event-stream")
		sse(w, "", string(arg), "", 4)
	})
	_, back := sendRead(t, px, true, []any{msg("user", "a sigla SGP aparece no relatório")})
	if c := string(bodies[0]); !reCompany.MatchString(c) {
		t.Fatalf("premissa: a sigla não foi mascarada: %s", c)
	}
	const wants = "ls /dados/SGP/ && echo SGP SGP"
	if cmd := inputCommand(t, back); cmd != wants {
		t.Errorf("o pseudônimo em outra caixa não voltou à sigla como ela entrou:\n quer %q\n veio %q", wants, cmd)
	}
}

// O lado de lá: uma coluna que é palavra comum ("status", "valor") não mascara a palavra maior
// que a contém ("user_status" no código): só nome com cara de identificador vale por pedaço.
func TestCommonWordDoesNotMaskLargerWord(t *testing.T) {
	var bodies [][]byte
	px := buildMem(t, t.TempDir(), &bodies, func(body []byte, w http.ResponseWriter) {
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"type": "message", "role": "assistant",
			"content": []any{map[string]any{"type": "text", "text": "ok"}}})
	})
	usage := map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{"command": "python3 consulta.py"}}
	sendRead(t, px, false, []any{msg("user", "veja as vendas"), msg("assistant", []any{usage}),
		msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "t1",
			"content": "SELECT status, valor FROM dw.VENDA2024 WHERE ano = 2024"}}),
		msg("assistant", "vou corrigir o código"),
		msg("user", "no código está if user_status == 1: total_valor += 1")})
	body := string(bodies[len(bodies)-1])
	for _, p := range []string{"user_status", "total_valor"} {
		if !strings.Contains(body, p) {
			t.Errorf("%q foi mascarado: palavra comum não vale por pedaço", p)
		}
	}
}

// Um nome que a conversa já conhece não sai em claro numa saída de comando nova, mesmo dentro de
// uma palavra maior (o lakehouse com o nome da tabela, o arquivo com prefixo); um GUID que é o
// valor de um campo de id (workspace_id, "id") também não. E a volta traz o valor real: o modelo
// que repete o pseudônimo devolve ao disco o nome que estava lá. Casos tirados da forma do que
// sobrava numa sessão real do Microsoft Fabric (os dados aqui são inventados).
func TestKnownNameNotLeftOver(t *testing.T) {
	const sql = "SELECT v.id, v.valor FROM dw.VENDA2024 v JOIN dw.CLIENTES_PF c ON c.id = v.cli WHERE v.ano = 2024"
	const guid = "3fa85f64-5717-4562-b3fc-2c963f66afa6"
	for _, c := range []struct{ name, output, real string }{
		{"json: nome maior com _", `{"value": [{"displayName": "LH_BRONZE_VENDA2024", "type": "Lakehouse"}]}`, "LH_BRONZE_VENDA2024"},
		{"json: nome inteiro", `{"displayName": "CLIENTES_PF", "type": "Table"}`, "CLIENTES_PF"},
		{"texto: nome inteiro", "tabela CLIENTES_PF carregada com 10 linhas", "CLIENTES_PF"},
		{"arquivo com prefixo", "T01_VENDA2024.sql\nT02_CARGA.sql", "T01_VENDA2024"},
		{"nome de várias partes no meio", "DIM_CLIENTES_PF_HIST ok", "DIM_CLIENTES_PF_HIST"},
		{"schema$tabela", `{"name": "DW$VENDA2024_HIST"}`, "VENDA2024_HIST"},
		{"guid depois de _id", "workspace_id: " + guid + "\nstatus: ok", guid},
		{"guid em json", `{"id": "` + guid + `", "displayName": "x"}`, guid},
	} {
		t.Run(c.name, func(t *testing.T) {
			var bodies [][]byte
			// a API repete, como texto, o resultado de ferramenta que recebeu (com os pseudônimos)
			px := buildMem(t, t.TempDir(), &bodies, func(body []byte, w http.ResponseWriter) {
				var req struct {
					Messages []struct {
						Content []struct {
							Type    string `json:"type"`
							Content string `json:"content"`
						} `json:"content"`
					} `json:"messages"`
				}
				json.Unmarshal(body, &req)
				echo := ""
				if n := len(req.Messages); n > 0 && len(req.Messages[n-1].Content) > 0 {
					echo = req.Messages[n-1].Content[0].Content
				}
				w.Header().Set("content-type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{"type": "message", "role": "assistant",
					"content": []any{map[string]any{"type": "text", "text": echo}}})
			})
			usage := func(id, cmd string) map[string]any {
				return map[string]any{"type": "tool_use", "id": id, "name": "Bash", "input": map[string]any{"command": cmd}}
			}
			res := func(id, s string) map[string]any {
				return map[string]any{"type": "tool_result", "tool_use_id": id, "content": s}
			}
			text, _ := sendRead(t, px, false, []any{msg("user", "veja as vendas"),
				msg("assistant", []any{usage("t1", "python3 consulta.py")}),
				msg("user", []any{res("t1", sql)}),
				msg("assistant", []any{usage("t2", "python3 listar.py")}),
				msg("user", []any{res("t2", c.output)})})
			if body := string(bodies[len(bodies)-1]); strings.Contains(body, c.real) {
				t.Errorf("%q saiu em claro", c.real)
			}
			if text != c.output {
				t.Errorf("a volta não trouxe o original:\n quer %q\n veio %q", c.output, text)
			}
		})
	}
}
