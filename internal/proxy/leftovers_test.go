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

// The same person written in different cases comes back each as it was: the report the model writes
// with "MARIA DA SILVA" does not write "Maria da Silva" (and an Edit with old_string in uppercase
// still matches the file). Case taken from the shape of 7 writes that came back different in a real
// session (the names here are made up).
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
			t.Errorf("%q went out in plain text", real)
		}
	}
	if back != text {
		t.Errorf("the way back changed the case:\n want %q\n got  %q", text, back)
	}
}

// buildCaseEcho: the proxy with a registered term ("sgp", the acronym of a made-up company) and a
// fake API that answers with the response resp builds from the body it received.
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

// lastReceivedEntry: the input (already masked) of the last tool call the API received.
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

// The same name in different case spellings (the company acronym "SgP", "sgp" and "SGP"; the login
// "etl_carga" and "ETL_CARGA"; the SQL table in uppercase in one SELECT and in lowercase in
// another) comes back each as it was in the model's writing: the command and the file it writes
// with the pseudonyms it saw come out equal to the original. Before, the term and the login had ONE
// pseudonym for all spellings and the return always brought the first one ("echo SgP sgp SGP" came
// back as "echo SgP SgP SgP"). Case taken from the shape of 21 writes that came back different,
// only by case, in a replay of a real session (the names here are made up).
func TestWritingReturnsEachSpellingCase(t *testing.T) {
	var bodies [][]byte
	// the model repeats the last tool call with the pseudonyms it saw
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
				t.Errorf("%q went out in plain text", real)
			}
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(back), &got); err != nil {
			t.Fatalf("invalid input: %q", back)
		}
		for k, v := range in {
			if got[k] != v {
				t.Errorf("the writing came back with another case (%s):\n wants %q\n got   %q", k, v, got[k])
			}
		}
	}
}

// The model rewrites the case of the acronym's pseudonym, which only appeared in uppercase ("SGP"):
// in lowercase in a path, with an initial capital in a sentence, all in uppercase. None of these
// forms is left as a pseudonym, and all go back to the acronym as it entered (a model that changes
// the case of a pseudonym does not invent a "/dados/sgp" folder that does not exist), like object
// names.
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
		t.Fatalf("premise: the acronym was not masked: %s", c)
	}
	const wants = "ls /dados/SGP/ && echo SGP SGP"
	if cmd := inputCommand(t, back); cmd != wants {
		t.Errorf("the pseudonym in another case did not go back to the acronym as it came in:\n wants %q\n got   %q", wants, cmd)
	}
}

// The other side: a column that is a common word ("status", "valor") does not mask the larger word
// that contains it ("user_status" in code): only an identifier-shaped name counts by piece.
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
			t.Errorf("%q was masked: a common word does not count by piece", p)
		}
	}
}

// A name the conversation already knows does not go out in the clear in a new command output, even
// inside a larger word (the lakehouse with the table name, the file with a prefix); neither does a
// GUID that is the value of an id field (workspace_id, "id"). And the return brings the real value:
// the model that repeats the pseudonym returns to disk the name that was there. Cases taken from
// the shape of what was left in a real Microsoft Fabric session (the data here is made up).
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
			// the API repeats, as text, the tool result it received (with the pseudonyms)
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
				t.Errorf("%q went out in plain text", c.real)
			}
			if text != c.output {
				t.Errorf("the way back did not bring the original:\n wants %q\n got   %q", c.output, text)
			}
		})
	}
}
