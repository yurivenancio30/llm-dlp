package proxy

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// A mesma pessoa escrita em caixas diferentes volta cada uma como estava: o relatório que o
// modelo escreve com "MARIA DA SILVA" não grava "Maria da Silva" (e um Edit com old_string em
// maiúsculas ainda casa com o arquivo). Caso tirado da forma de 7 escritas que voltavam
// diferentes numa sessão real (os nomes aqui são inventados).
func TestPessoaVoltaComACaixaDeCadaGrafia(t *testing.T) {
	const texto = `[{"nome_cliente": "Maria da Silva", "cidade": "Recife"},` + "\n" +
		` {"nome_cliente": "MARIA DA SILVA", "cidade": "Recife"},` + "\n" +
		` {"nome_cliente": "maria da silva", "cidade": "Recife"},` + "\n" +
		` {"nome_cliente": "João Pereira Santos", "cidade": "Natal"},` + "\n" +
		` {"nome_cliente": "JOÃO PEREIRA SANTOS", "cidade": "Natal"}]`
	var corpos [][]byte
	px := montarMem(t, t.TempDir(), &corpos, func(corpo []byte, w http.ResponseWriter) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.Unmarshal(corpo, &req)
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"type": "message", "role": "assistant",
			"content": []any{map[string]any{"type": "text", "text": req.Messages[len(req.Messages)-1].Content}}})
	})
	volta, _ := enviarLer(t, px, false, []any{msg("user", texto)})
	corpo := string(corpos[len(corpos)-1])
	for _, real := range []string{"Maria da Silva", "MARIA DA SILVA", "maria da silva", "João Pereira Santos", "JOÃO PEREIRA SANTOS"} {
		if strings.Contains(corpo, real) {
			t.Errorf("%q saiu em claro", real)
		}
	}
	if volta != texto {
		t.Errorf("a volta mudou a caixa:\n quer %q\n veio %q", texto, volta)
	}
}

// O lado de lá: uma coluna que é palavra comum ("status", "valor") não mascara a palavra maior
// que a contém ("user_status" no código): só nome com cara de identificador vale por pedaço.
func TestPalavraComumNaoMascaraPalavraMaior(t *testing.T) {
	var corpos [][]byte
	px := montarMem(t, t.TempDir(), &corpos, func(corpo []byte, w http.ResponseWriter) {
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"type": "message", "role": "assistant",
			"content": []any{map[string]any{"type": "text", "text": "ok"}}})
	})
	uso := map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{"command": "python3 consulta.py"}}
	enviarLer(t, px, false, []any{msg("user", "veja as vendas"), msg("assistant", []any{uso}),
		msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "t1",
			"content": "SELECT status, valor FROM dw.VENDA2024 WHERE ano = 2024"}}),
		msg("assistant", "vou corrigir o código"),
		msg("user", "no código está if user_status == 1: total_valor += 1")})
	corpo := string(corpos[len(corpos)-1])
	for _, p := range []string{"user_status", "total_valor"} {
		if !strings.Contains(corpo, p) {
			t.Errorf("%q foi mascarado: palavra comum não vale por pedaço", p)
		}
	}
}

// Um nome que a conversa já conhece não sai em claro numa saída de comando nova, mesmo dentro de
// uma palavra maior (o lakehouse com o nome da tabela, o arquivo com prefixo); um GUID que é o
// valor de um campo de id (workspace_id, "id") também não. E a volta traz o valor real: o modelo
// que repete o pseudônimo devolve ao disco o nome que estava lá. Casos tirados da forma do que
// sobrava numa sessão real do Microsoft Fabric (os dados aqui são inventados).
func TestNomeConhecidoNaoSobra(t *testing.T) {
	const sql = "SELECT v.id, v.valor FROM dw.VENDA2024 v JOIN dw.CLIENTES_PF c ON c.id = v.cli WHERE v.ano = 2024"
	const guid = "3fa85f64-5717-4562-b3fc-2c963f66afa6"
	for _, c := range []struct{ nome, saida, real string }{
		{"json: nome maior com _", `{"value": [{"displayName": "LH_BRONZE_VENDA2024", "type": "Lakehouse"}]}`, "LH_BRONZE_VENDA2024"},
		{"json: nome inteiro", `{"displayName": "CLIENTES_PF", "type": "Table"}`, "CLIENTES_PF"},
		{"texto: nome inteiro", "tabela CLIENTES_PF carregada com 10 linhas", "CLIENTES_PF"},
		{"arquivo com prefixo", "T01_VENDA2024.sql\nT02_CARGA.sql", "T01_VENDA2024"},
		{"nome de várias partes no meio", "DIM_CLIENTES_PF_HIST ok", "DIM_CLIENTES_PF_HIST"},
		{"schema$tabela", `{"name": "DW$VENDA2024_HIST"}`, "VENDA2024_HIST"},
		{"guid depois de _id", "workspace_id: " + guid + "\nstatus: ok", guid},
		{"guid em json", `{"id": "` + guid + `", "displayName": "x"}`, guid},
	} {
		t.Run(c.nome, func(t *testing.T) {
			var corpos [][]byte
			// a API repete, como texto, o resultado de ferramenta que recebeu (com os pseudônimos)
			px := montarMem(t, t.TempDir(), &corpos, func(corpo []byte, w http.ResponseWriter) {
				var req struct {
					Messages []struct {
						Content []struct {
							Type    string `json:"type"`
							Content string `json:"content"`
						} `json:"content"`
					} `json:"messages"`
				}
				json.Unmarshal(corpo, &req)
				eco := ""
				if n := len(req.Messages); n > 0 && len(req.Messages[n-1].Content) > 0 {
					eco = req.Messages[n-1].Content[0].Content
				}
				w.Header().Set("content-type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{"type": "message", "role": "assistant",
					"content": []any{map[string]any{"type": "text", "text": eco}}})
			})
			uso := func(id, cmd string) map[string]any {
				return map[string]any{"type": "tool_use", "id": id, "name": "Bash", "input": map[string]any{"command": cmd}}
			}
			res := func(id, s string) map[string]any {
				return map[string]any{"type": "tool_result", "tool_use_id": id, "content": s}
			}
			texto, _ := enviarLer(t, px, false, []any{msg("user", "veja as vendas"),
				msg("assistant", []any{uso("t1", "python3 consulta.py")}),
				msg("user", []any{res("t1", sql)}),
				msg("assistant", []any{uso("t2", "python3 listar.py")}),
				msg("user", []any{res("t2", c.saida)})})
			if corpo := string(corpos[len(corpos)-1]); strings.Contains(corpo, c.real) {
				t.Errorf("%q saiu em claro", c.real)
			}
			if texto != c.saida {
				t.Errorf("a volta não trouxe o original:\n quer %q\n veio %q", c.saida, texto)
			}
		})
	}
}
