package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// A masked value followed by punctuation or markup ("`User ID=x`", "(User ID=x)", "**User ID=x**"):
// the punctuation stays outside the pseudonym, so the model sees it, and what it writes back in a
// tool (Write) reaches the disk equal to the original, with and without streaming, with the
// pseudonym split at any point between two pieces. Before, the backtick and the ")" went into the
// value: the backtick disappeared from the model's view (and came back doubled when it closed it)
// and a value with ")" was discarded and went to the API in the clear.
func TestRoundTripValueFollowedByPunctuation(t *testing.T) {
	u := "svc_" + "relatorio" // fictitious value, built from parts
	for _, after := range []string{"`", "\"", "'", ")", "]", "}", ">", "**", "`;", "`\n", ""} {
		original := "Initial Catalog=vendas_x9;User ID=" + u + after + " fim"
		if after == "" {
			original = "Initial Catalog=vendas_x9;User ID=" + u
		}
		for _, stream := range []bool{true, false} {
			for _, piece := range []int{1, 2, 3, 5, 7, 9} {
				if !stream && piece > 1 {
					continue
				}
				var seen string
				_, px := build(t, func(w http.ResponseWriter, r *http.Request) {
					b, _ := io.ReadAll(r.Body)
					var v struct {
						Messages []struct{ Content string } `json:"messages"`
					}
					json.Unmarshal(b, &v)
					seen = v.Messages[0].Content
					// the model writes to the file exactly what it saw
					arg, _ := json.Marshal(map[string]string{"file_path": "/tmp/x.md", "content": seen})
					if !stream {
						w.Header().Set("content-type", "application/json")
						j, _ := json.Marshal(map[string]any{"type": "message", "content": []any{map[string]any{
							"type": "tool_use", "id": "t", "name": "Write", "input": json.RawMessage(arg)}}})
						w.Write(j)
						return
					}
					w.Header().Set("content-type", "text/event-stream")
					ev := func(x any) { j, _ := json.Marshal(x); fmt.Fprintf(w, "event: x\ndata: %s\n\n", j) }
					ev(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "t", "name": "Write", "input": map[string]any{}}})
					s := string(arg)
					for i := 0; i < len(s); i += piece {
						ev(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": s[i:min(len(s), i+piece)]}})
					}
					ev(map[string]any{"type": "content_block_stop", "index": 0})
				})
				body, _ := json.Marshal(map[string]any{"model": "x", "max_tokens": 1, "stream": stream,
					"messages": []any{map[string]any{"role": "user", "content": original}}})
				resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				var entry string
				if stream {
					sc := bufio.NewScanner(resp.Body)
					for sc.Scan() {
						if l := sc.Text(); strings.HasPrefix(l, "data: ") {
							var e map[string]any
							json.Unmarshal([]byte(l[6:]), &e)
							if d, ok := e["delta"].(map[string]any); ok {
								entry += d["partial_json"].(string)
							}
						}
					}
				} else {
					var r struct {
						Content []struct{ Input json.RawMessage }
					}
					json.NewDecoder(resp.Body).Decode(&r)
					if len(r.Content) > 0 {
						entry = string(r.Content[0].Input)
					}
				}
				resp.Body.Close()
				var in map[string]string
				if err := json.Unmarshal([]byte(entry), &in); err != nil {
					t.Fatalf("depois=%q stream=%v pedaço=%d: entrada inválida", after, stream, piece)
				}
				tc := fmt.Sprintf("depois=%q stream=%v pedaço=%d", after, stream, piece)
				if strings.Contains(seen, u) {
					t.Errorf("%s: o valor foi em claro para a API", tc)
				}
				if after != "" && !strings.Contains(seen, after+" fim") {
					t.Errorf("%s: a pontuação depois do valor sumiu da vista do modelo", tc)
				}
				if in["content"] != original {
					t.Errorf("%s: o arquivo gravado difere do original", tc)
				}
			}
		}
	}
}

// A table name masked on the way out: the command the model writes with the pseudonym (in any case
// of the prefix) reaches the local tool with the real name.
func TestRoundTripTableName(t *testing.T) {
	re := regexp.MustCompile(`\b[Tt]_[a-z2-7]{8}\b`)
	for _, letterCase := range []string{"igual", "outra"} {
		var pseudo string
		_, px := build(t, func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			pseudo = re.FindString(string(b))
			p := pseudo
			if letterCase == "outra" && p != "" {
				p = strings.ToUpper(p[:1]) + p[1:]
			}
			arg, _ := json.Marshal(map[string]string{"command": `psql -c "SELECT count(*) FROM ` + p + `"`})
			w.Header().Set("content-type", "text/event-stream")
			ev := func(x any) { j, _ := json.Marshal(x); fmt.Fprintf(w, "event: x\ndata: %s\n\n", j) }
			ev(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "t", "name": "Bash", "input": map[string]any{}}})
			s := string(arg)
			for i := 0; i < len(s); i += 5 {
				ev(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": s[i:min(len(s), i+5)]}})
			}
			ev(map[string]any{"type": "content_block_stop", "index": 0})
		})
		body, _ := json.Marshal(map[string]any{"model": "x", "max_tokens": 1, "stream": true,
			"messages": []any{map[string]any{"role": "user", "content": "SELECT vl_x FROM financeiro_x.tb_pedido_x9 WHERE id > 0"}}})
		resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		var entry string
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if l := sc.Text(); strings.HasPrefix(l, "data: ") {
				var e map[string]any
				json.Unmarshal([]byte(l[6:]), &e)
				if d, ok := e["delta"].(map[string]any); ok {
					entry += d["partial_json"].(string)
				}
			}
		}
		resp.Body.Close()
		if pseudo == "" {
			t.Fatalf("a tabela não foi mascarada na ida")
		}
		if !strings.Contains(entry, "FROM tb_pedido_x9") {
			t.Errorf("caixa %s: o comando não voltou com o nome real", letterCase)
		}
	}
}

// The return path accepts the object pseudonym in any case (including all uppercase), and a folder
// comes back with its exact spelling.
func TestRoundTripObjectAnyCase(t *testing.T) {
	re := regexp.MustCompile(`\b(?i:t|bkt)_[a-zA-Z2-7]{8}\b`)
	for _, c := range []struct{ msg, real, command string }{
		{"SELECT vl_x FROM financeiro_x.tb_pedido_x9 WHERE id > 0 AND a = 1", "tb_pedido_x9", "psql -c 'SELECT 1 FROM %s'"},
		{"aws s3 ls s3://Bkt-Dados-x1/hoje/", "Bkt-Dados-x1", "aws s3 ls s3://%s/"},
	} {
		for _, letterCase := range []string{"igual", "maiusc", "minusc"} {
			var pseudos []string
			_, px := build(t, func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				pseudos = re.FindAllString(string(b), -1)
				p := ""
				for _, x := range pseudos {
					p = x // the last one is the one of the name looked for
				}
				switch letterCase {
				case "maiusc":
					p = strings.ToUpper(p)
				case "minusc":
					p = strings.ToLower(p)
				}
				arg, _ := json.Marshal(map[string]string{"command": fmt.Sprintf(c.command, p)})
				w.Header().Set("content-type", "text/event-stream")
				ev := func(x any) { j, _ := json.Marshal(x); fmt.Fprintf(w, "event: x\ndata: %s\n\n", j) }
				ev(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "t", "name": "Bash", "input": map[string]any{}}})
				s := string(arg)
				for i := 0; i < len(s); i += 4 {
					ev(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": s[i:min(len(s), i+4)]}})
				}
				ev(map[string]any{"type": "content_block_stop", "index": 0})
			})
			body, _ := json.Marshal(map[string]any{"model": "x", "max_tokens": 1, "stream": true,
				"messages": []any{map[string]any{"role": "user", "content": c.msg}}})
			resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			var entry string
			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				if l := sc.Text(); strings.HasPrefix(l, "data: ") {
					var e map[string]any
					json.Unmarshal([]byte(l[6:]), &e)
					if d, ok := e["delta"].(map[string]any); ok {
						entry += d["partial_json"].(string)
					}
				}
			}
			resp.Body.Close()
			if len(pseudos) == 0 {
				t.Fatalf("%s: não foi mascarado na ida", c.real)
			}
			if !strings.Contains(entry, c.real) {
				t.Errorf("%s, caixa %s: o comando não voltou com o nome real", c.real, letterCase)
			}
		}
	}
}
