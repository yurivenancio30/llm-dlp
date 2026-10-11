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

// O formato da OpenAI (Chat Completions), o do Copilot com provedor próprio, da xAI e do
// endereço compatível do Gemini: nada real chega à API, a resposta volta real (inteira e em
// streaming) e o arquivo que o agente escreve sai com o valor real.

const (
	emailOA = "joao.silva@empresa-ficticia.com.br"
	ipOA    = "10.42.7.15"
)

var rePseEmail = regexp.MustCompile(`p\.[a-z2-7]{8}@d[a-z2-7]{4}\.invalid`)

type pedidoOA struct {
	caminho, auth string
	corpo         []byte
}

// montarOpenAI: proxy com os provedores "xai" e "gemini" apontando para uma API falsa.
func montarOpenAI(t *testing.T, resp func(p pedidoOA, w http.ResponseWriter)) (*httptest.Server, *[]pedidoOA) {
	t.Helper()
	cfg := config.Padrao()
	cfg.DominiosInternos = []string{"empresa-ficticia"}
	m, err := mask.NovoMasker(cfg, chaveTeste, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pedidos := &[]pedidoOA{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		p := pedidoOA{r.URL.RequestURI(), r.Header.Get("authorization"), b}
		*pedidos = append(*pedidos, p)
		resp(p, w)
	}))
	anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a requisição foi para o upstream da Anthropic: %s", r.URL.Path)
	}))
	cfg.Upstream = anthropic.URL
	cfg.Provedores = map[string]config.Provedor{"xai": {Upstream: up.URL, Formato: "openai"}, "gemini": {Upstream: up.URL, Formato: "gemini"}}
	px := httptest.NewServer(Novo(cfg, m, nil, log.New(io.Discard, "", 0)))
	t.Cleanup(func() { up.Close(); anthropic.Close(); px.Close() })
	return px, pedidos
}

func respostaOA(w http.ResponseWriter, msg map[string]any) {
	w.Header().Set("content-type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": "c1", "object": "chat.completion", "model": "m",
		"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": msg}}})
}

// conversaOA: uma conversa de agente no formato da OpenAI, com dado sensível em cada lugar.
func conversaOA() map[string]any {
	chamada := func(id, args string) map[string]any {
		return map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "bash", "arguments": args}}
	}
	return map[string]any{
		"model": "m", "stream": false, "temperature": 0,
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "bash",
			"description": "Roda um comando. Dúvidas: " + emailOA,
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}}}}},
		"messages": []any{
			map[string]any{"role": "system", "content": "Você é um agente. O responsável é " + emailOA + "."},
			map[string]any{"role": "user", "content": "faça um ping no servidor " + ipOA},
			map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{
				chamada("call_1", `{"command":"ping -c1 `+ipOA+`"}`), chamada("call_2", `{ "command" : "date" }`)}},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "PING " + ipOA + ": 1 packets transmitted"},
			map[string]any{"role": "tool", "tool_call_id": "call_2", "content": []any{map[string]any{"type": "text", "text": "avise " + emailOA}}},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "e avise " + emailOA}}},
		},
	}
}

func TestOpenAIIdaMascarada(t *testing.T) {
	px, pedidos := montarOpenAI(t, func(p pedidoOA, w http.ResponseWriter) {
		respostaOA(w, map[string]any{"role": "assistant", "content": "ok"})
	})
	corpo, _ := json.Marshal(conversaOA())
	// pelo provedor de formato "openai" e pelo endereço compatível do provedor do Gemini
	for _, caminho := range []string{"/xai/v1/chat/completions", "/gemini/v1beta/openai/chat/completions"} {
		st, _ := pedir(t, "POST", px.URL+caminho, corpo, func(r *http.Request) { r.Header.Set("authorization", "Bearer chave-de-teste") })
		if st != 200 {
			t.Fatalf("%s: status %d", caminho, st)
		}
		p := (*pedidos)[len(*pedidos)-1]
		if quer := strings.SplitN(caminho, "/", 3)[2]; p.caminho != "/"+quer || p.auth != "Bearer chave-de-teste" {
			t.Errorf("%s: chegou em %q com a credencial %q", caminho, p.caminho, p.auth)
		}
		if bytes.Contains(p.corpo, []byte(emailOA)) || bytes.Contains(p.corpo, []byte(ipOA)) {
			t.Errorf("%s: valor real chegou à API: %s", caminho, p.corpo)
		}
		var req struct {
			Model string
			Tools []struct {
				Function struct{ Name, Description string }
			}
			Messages []struct {
				Role       string
				Content    any
				ToolCallID string `json:"tool_call_id"`
				ToolCalls  []struct {
					ID       string
					Function struct{ Name, Arguments string }
				} `json:"tool_calls"`
			}
		}
		if err := json.Unmarshal(p.corpo, &req); err != nil {
			t.Fatal(err)
		}
		if req.Model != "m" || len(req.Messages) != 6 || len(req.Tools) != 1 || req.Tools[0].Function.Name != "bash" {
			t.Fatalf("%s: a forma mudou: %s", caminho, p.corpo)
		}
		if !rePseEmail.MatchString(req.Tools[0].Function.Description) || !rePseEmail.MatchString(fmt.Sprint(req.Messages[0].Content)) {
			t.Errorf("%s: instruções e ferramentas deviam sair com o pseudônimo: %s", caminho, p.corpo)
		}
		a := req.Messages[2]
		if a.Role != "assistant" || a.Content != nil || len(a.ToolCalls) != 2 || a.ToolCalls[0].ID != "call_1" || a.ToolCalls[0].Function.Name != "bash" {
			t.Fatalf("%s: chamadas: %+v", caminho, a)
		}
		var args struct{ Command string }
		if err := json.Unmarshal([]byte(a.ToolCalls[0].Function.Arguments), &args); err != nil || !strings.HasPrefix(args.Command, "ping -c1 ") {
			t.Errorf("%s: os argumentos mascarados deviam continuar um JSON: %q", caminho, a.ToolCalls[0].Function.Arguments)
		}
		// o que não tinha dado sensível sai exatamente como o cliente mandou (espaços e tudo)
		if a.ToolCalls[1].Function.Arguments != `{ "command" : "date" }` {
			t.Errorf("%s: argumentos sem dado sensível mudaram: %q", caminho, a.ToolCalls[1].Function.Arguments)
		}
		if req.Messages[3].ToolCallID != "call_1" || !strings.HasPrefix(fmt.Sprint(req.Messages[3].Content), "PING ") {
			t.Errorf("%s: resultado de ferramenta: %+v", caminho, req.Messages[3])
		}
		if _, lista := req.Messages[4].Content.([]any); !lista {
			t.Errorf("%s: conteúdo em partes devia continuar em partes: %+v", caminho, req.Messages[4])
		}
	}
	// a mesma conversa, mandada de novo, sai idêntica (o cache da API depende disso)
	pedir(t, "POST", px.URL+"/xai/v1/chat/completions", corpo, nil)
	if n := len(*pedidos); !bytes.Equal((*pedidos)[0].corpo, (*pedidos)[n-1].corpo) {
		t.Errorf("a mesma requisição saiu diferente na segunda vez")
	}
}

// A resposta sem streaming volta real: o texto e os argumentos da chamada de ferramenta (o que o
// cliente grava num arquivo ou roda no terminal).
func TestOpenAIVoltaSemStreaming(t *testing.T) {
	px, _ := montarOpenAI(t, func(p pedidoOA, w http.ResponseWriter) {
		pse := rePseEmail.FindString(string(p.corpo))
		args, _ := json.Marshal(map[string]any{"filePath": "/tmp/resumo.txt", "content": "AVISAR " + pse + " \"hoje\""})
		respostaOA(w, map[string]any{"role": "assistant", "content": "vou avisar " + pse,
			"tool_calls": []any{
				map[string]any{"id": "c9", "type": "function", "function": map[string]any{"name": "write", "arguments": string(args)}},
				map[string]any{"id": "c10", "type": "function", "function": map[string]any{"name": "webfetch", "arguments": `{"url":"https://x.example/?q=` + pse + `"}`}}}})
	})
	corpo, _ := json.Marshal(conversaOA())
	st, b := pedir(t, "POST", px.URL+"/xai/v1/chat/completions", corpo, nil)
	var resp struct {
		Choices []struct {
			Message struct {
				Content   string
				ToolCalls []struct {
					Function struct{ Name, Arguments string }
				} `json:"tool_calls"`
			}
		}
	}
	if err := json.Unmarshal(b, &resp); err != nil || st != 200 || len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 2 {
		t.Fatalf("status %d: %s", st, b)
	}
	m := resp.Choices[0].Message
	if m.Content != "vou avisar "+emailOA {
		t.Errorf("texto: %q", m.Content)
	}
	var args struct{ Content string }
	if err := json.Unmarshal([]byte(m.ToolCalls[0].Function.Arguments), &args); err != nil || args.Content != "AVISAR "+emailOA+" \"hoje\"" {
		t.Errorf("argumentos da escrita: %q (%v)", m.ToolCalls[0].Function.Arguments, err)
	}
	// o que vai para a internet segue com o pseudônimo
	if strings.Contains(m.ToolCalls[1].Function.Arguments, emailOA) || !rePseEmail.MatchString(m.ToolCalls[1].Function.Arguments) {
		t.Errorf("a busca na internet devia seguir mascarada: %q", m.ToolCalls[1].Function.Arguments)
	}
}

// Em streaming, o pseudônimo chega cortado entre dois pedaços, no texto e nos argumentos (que
// são pedaços de um JSON): o cliente nunca recebe um pedaço de pseudônimo, e o que ele junta é o
// valor real. O "[DONE]" continua sendo a última linha.
func TestOpenAIVoltaStreaming(t *testing.T) {
	for _, comFim := range []bool{true, false} {
		px, _ := montarOpenAI(t, func(p pedidoOA, w http.ResponseWriter) {
			pse := rePseEmail.FindString(string(p.corpo))
			w.Header().Set("content-type", "text/event-stream")
			ev := func(delta map[string]any, fim any) {
				b, _ := json.Marshal(map[string]any{"id": "c1", "object": "chat.completion.chunk", "model": "m",
					"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": fim}}})
				fmt.Fprintf(w, "data: %s\n\n", b)
			}
			texto := []rune("vou avisar " + pse + " agora")
			for i := 0; i < len(texto); i += 7 {
				ev(map[string]any{"content": string(texto[i:min(i+7, len(texto))])}, nil)
			}
			args, _ := json.Marshal(map[string]any{"filePath": "/tmp/resumo.txt", "content": "AVISAR " + pse})
			ev(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "c9", "type": "function",
				"function": map[string]any{"name": "write", "arguments": ""}}}}, nil)
			for i := 0; i < len(args); i += 9 {
				ev(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": string(args[i:min(i+9, len(args))])}}}}, nil)
			}
			if comFim {
				ev(map[string]any{}, "tool_calls")
			}
			io.WriteString(w, "data: [DONE]\n\n")
		})
		req := conversaOA()
		req["stream"] = true
		corpo, _ := json.Marshal(req)
		st, b := pedir(t, "POST", px.URL+"/xai/v1/chat/completions", corpo, nil)
		if st != 200 {
			t.Fatalf("status %d", st)
		}
		var texto, args strings.Builder
		linhas := strings.Split(strings.TrimSpace(string(b)), "\n")
		for _, l := range linhas {
			dados, ok := strings.CutPrefix(l, "data: ")
			if !ok || dados == "[DONE]" {
				continue
			}
			if rePseEmail.MatchString(dados) {
				t.Errorf("comFim=%v: pseudônimo num pedaço entregue ao cliente: %s", comFim, dados)
			}
			var ev struct {
				ID      string
				Choices []struct {
					Delta struct {
						Content   string
						ToolCalls []struct{ Function struct{ Arguments string } } `json:"tool_calls"`
					}
				}
			}
			if err := json.Unmarshal([]byte(dados), &ev); err != nil || ev.ID != "c1" {
				t.Fatalf("comFim=%v: pedaço inválido: %s", comFim, dados)
			}
			for _, c := range ev.Choices {
				texto.WriteString(c.Delta.Content)
				for _, tc := range c.Delta.ToolCalls {
					args.WriteString(tc.Function.Arguments)
				}
			}
		}
		if linhas[len(linhas)-1] != "data: [DONE]" {
			t.Errorf("comFim=%v: a última linha devia ser o [DONE]: %q", comFim, linhas[len(linhas)-1])
		}
		if texto.String() != "vou avisar "+emailOA+" agora" {
			t.Errorf("comFim=%v: texto: %q", comFim, texto.String())
		}
		var a struct{ Content string }
		if err := json.Unmarshal([]byte(args.String()), &a); err != nil || a.Content != "AVISAR "+emailOA {
			t.Errorf("comFim=%v: argumentos: %q (%v)", comFim, args.String(), err)
		}
	}
}

// Arquivo anexado que não dá para verificar (áudio) não sai: vai uma nota no lugar. E um campo
// que o llm-dlp não conhece é mascarado por precaução.
func TestOpenAIAnexoECampoDesconhecido(t *testing.T) {
	px, pedidos := montarOpenAI(t, func(p pedidoOA, w http.ResponseWriter) {
		respostaOA(w, map[string]any{"role": "assistant", "content": "ok"})
	})
	corpo, _ := json.Marshal(map[string]any{"model": "m", "user": emailOA,
		"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "ouça isto"},
			map[string]any{"type": "input_audio", "input_audio": map[string]any{"data": "QUJDREVGRw==", "format": "wav"}},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://exemplo.example/foto.png"}}}}}})
	if st, _ := pedir(t, "POST", px.URL+"/xai/v1/chat/completions", corpo, nil); st != 200 {
		t.Fatalf("status %d", st)
	}
	b := (*pedidos)[0].corpo
	if bytes.Contains(b, []byte("QUJDREVGRw")) || !bytes.Contains(b, []byte("[llm-dlp] Um arquivo anexado foi retirado")) {
		t.Errorf("o áudio devia ter sido retirado, com a nota no lugar: %s", b)
	}
	if !bytes.Contains(b, []byte("https://exemplo.example/foto.png")) {
		t.Errorf("a imagem por endereço (que a API busca sozinha) devia seguir: %s", b)
	}
	if bytes.Contains(b, []byte(emailOA)) {
		t.Errorf("o campo desconhecido devia ter sido mascarado: %s", b)
	}
}
