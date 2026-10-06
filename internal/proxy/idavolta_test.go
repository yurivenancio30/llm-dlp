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

// Um valor mascarado seguido de pontuação ou marcação ("`User ID=x`", "(User ID=x)",
// "**User ID=x**"): a pontuação fica fora do pseudônimo, para o modelo vê-la, e o que ele
// escreve de volta numa ferramenta (Write) chega ao disco igual ao original, com e sem
// streaming, com o pseudônimo partido em qualquer ponto entre dois pedaços. Antes, a crase e
// o ")" entravam no valor: a crase sumia da vista do modelo (e voltava em dobro quando ele a
// fechava) e um valor com ")" era descartado e ia em claro para a API.
func TestIdaEVoltaValorSeguidoDePontuacao(t *testing.T) {
	u := "svc_" + "relatorio" // valor fictício, montado por partes
	for _, depois := range []string{"`", "\"", "'", ")", "]", "}", ">", "**", "`;", "`\n", ""} {
		original := "Initial Catalog=vendas_x9;User ID=" + u + depois + " fim"
		if depois == "" {
			original = "Initial Catalog=vendas_x9;User ID=" + u
		}
		for _, stream := range []bool{true, false} {
			for _, pedaco := range []int{1, 2, 3, 5, 7, 9} {
				if !stream && pedaco > 1 {
					continue
				}
				var visto string
				_, px := montar(t, func(w http.ResponseWriter, r *http.Request) {
					b, _ := io.ReadAll(r.Body)
					var v struct {
						Messages []struct{ Content string } `json:"messages"`
					}
					json.Unmarshal(b, &v)
					visto = v.Messages[0].Content
					// o modelo escreve no arquivo exatamente o que viu
					arg, _ := json.Marshal(map[string]string{"file_path": "/tmp/x.md", "content": visto})
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
					for i := 0; i < len(s); i += pedaco {
						ev(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": s[i:min(len(s), i+pedaco)]}})
					}
					ev(map[string]any{"type": "content_block_stop", "index": 0})
				})
				corpo, _ := json.Marshal(map[string]any{"model": "x", "max_tokens": 1, "stream": stream,
					"messages": []any{map[string]any{"role": "user", "content": original}}})
				resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(corpo))
				if err != nil {
					t.Fatal(err)
				}
				var entrada string
				if stream {
					sc := bufio.NewScanner(resp.Body)
					for sc.Scan() {
						if l := sc.Text(); strings.HasPrefix(l, "data: ") {
							var e map[string]any
							json.Unmarshal([]byte(l[6:]), &e)
							if d, ok := e["delta"].(map[string]any); ok {
								entrada += d["partial_json"].(string)
							}
						}
					}
				} else {
					var r struct {
						Content []struct{ Input json.RawMessage }
					}
					json.NewDecoder(resp.Body).Decode(&r)
					if len(r.Content) > 0 {
						entrada = string(r.Content[0].Input)
					}
				}
				resp.Body.Close()
				var in map[string]string
				if err := json.Unmarshal([]byte(entrada), &in); err != nil {
					t.Fatalf("depois=%q stream=%v pedaço=%d: entrada inválida", depois, stream, pedaco)
				}
				caso := fmt.Sprintf("depois=%q stream=%v pedaço=%d", depois, stream, pedaco)
				if strings.Contains(visto, u) {
					t.Errorf("%s: o valor foi em claro para a API", caso)
				}
				if depois != "" && !strings.Contains(visto, depois+" fim") {
					t.Errorf("%s: a pontuação depois do valor sumiu da vista do modelo", caso)
				}
				if in["content"] != original {
					t.Errorf("%s: o arquivo gravado difere do original", caso)
				}
			}
		}
	}
}

// Nome de tabela mascarado na ida: o comando que o modelo escreve com o pseudônimo (em
// qualquer caixa do prefixo) chega à ferramenta local com o nome real.
func TestIdaEVoltaNomeDeTabela(t *testing.T) {
	re := regexp.MustCompile(`\b[Tt]_[a-z2-7]{8}\b`)
	for _, caixa := range []string{"igual", "outra"} {
		var pseudo string
		_, px := montar(t, func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			pseudo = re.FindString(string(b))
			p := pseudo
			if caixa == "outra" && p != "" {
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
		corpo, _ := json.Marshal(map[string]any{"model": "x", "max_tokens": 1, "stream": true,
			"messages": []any{map[string]any{"role": "user", "content": "SELECT vl_x FROM financeiro_x.tb_pedido_x9 WHERE id > 0"}}})
		resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(corpo))
		if err != nil {
			t.Fatal(err)
		}
		var entrada string
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if l := sc.Text(); strings.HasPrefix(l, "data: ") {
				var e map[string]any
				json.Unmarshal([]byte(l[6:]), &e)
				if d, ok := e["delta"].(map[string]any); ok {
					entrada += d["partial_json"].(string)
				}
			}
		}
		resp.Body.Close()
		if pseudo == "" {
			t.Fatalf("a tabela não foi mascarada na ida")
		}
		if !strings.Contains(entrada, "FROM tb_pedido_x9") {
			t.Errorf("caixa %s: o comando não voltou com o nome real", caixa)
		}
	}
}

// Item 4: a volta aceita o pseudônimo de objeto em qualquer caixa (inclusive todo em
// maiúsculas), e uma pasta volta com a grafia exata dela.
func TestIdaEVoltaObjetoQualquerCaixa(t *testing.T) {
	re := regexp.MustCompile(`\b(?i:t|bkt)_[a-zA-Z2-7]{8}\b`)
	for _, c := range []struct{ msg, real, comando string }{
		{"SELECT vl_x FROM financeiro_x.tb_pedido_x9 WHERE id > 0 AND a = 1", "tb_pedido_x9", "psql -c 'SELECT 1 FROM %s'"},
		{"aws s3 ls s3://Bkt-Dados-x1/hoje/", "Bkt-Dados-x1", "aws s3 ls s3://%s/"},
	} {
		for _, caixa := range []string{"igual", "maiusc", "minusc"} {
			var pseudos []string
			_, px := montar(t, func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				pseudos = re.FindAllString(string(b), -1)
				p := ""
				for _, x := range pseudos {
					p = x // o último é o do nome procurado
				}
				switch caixa {
				case "maiusc":
					p = strings.ToUpper(p)
				case "minusc":
					p = strings.ToLower(p)
				}
				arg, _ := json.Marshal(map[string]string{"command": fmt.Sprintf(c.comando, p)})
				w.Header().Set("content-type", "text/event-stream")
				ev := func(x any) { j, _ := json.Marshal(x); fmt.Fprintf(w, "event: x\ndata: %s\n\n", j) }
				ev(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "t", "name": "Bash", "input": map[string]any{}}})
				s := string(arg)
				for i := 0; i < len(s); i += 4 {
					ev(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": s[i:min(len(s), i+4)]}})
				}
				ev(map[string]any{"type": "content_block_stop", "index": 0})
			})
			corpo, _ := json.Marshal(map[string]any{"model": "x", "max_tokens": 1, "stream": true,
				"messages": []any{map[string]any{"role": "user", "content": c.msg}}})
			resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(corpo))
			if err != nil {
				t.Fatal(err)
			}
			var entrada string
			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				if l := sc.Text(); strings.HasPrefix(l, "data: ") {
					var e map[string]any
					json.Unmarshal([]byte(l[6:]), &e)
					if d, ok := e["delta"].(map[string]any); ok {
						entrada += d["partial_json"].(string)
					}
				}
			}
			resp.Body.Close()
			if len(pseudos) == 0 {
				t.Fatalf("%s: não foi mascarado na ida", c.real)
			}
			if !strings.Contains(entrada, c.real) {
				t.Errorf("%s, caixa %s: o comando não voltou com o nome real", c.real, caixa)
			}
		}
	}
}
