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

// The OpenAI format (Chat Completions), Copilot's with its own provider, xAI's and Gemini's
// compatible endpoint: nothing real reaches the API, the response comes back real (whole and in
// streaming) and the file the agent writes comes out with the real value.

const (
	oaEmail = "joao.silva@empresa-ficticia.com.br"
	oaIP    = "10.42.7.15"
)

var rePseEmail = regexp.MustCompile(`p\.[a-z2-7]{8}@d[a-z2-7]{4}\.invalid`)

type oaRequest struct {
	path, auth string
	body       []byte
}

// buildOpenAI: proxy with the "xai" and "gemini" providers pointing to a fake API.
func buildOpenAI(t *testing.T, resp func(p oaRequest, w http.ResponseWriter)) (*httptest.Server, *[]oaRequest) {
	t.Helper()
	cfg := config.Default()
	cfg.InternalDomains = []string{"empresa-ficticia"}
	m, err := mask.NewMasker(cfg, testKey, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	requests := &[]oaRequest{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		p := oaRequest{r.URL.RequestURI(), r.Header.Get("authorization"), b}
		*requests = append(*requests, p)
		resp(p, w)
	}))
	anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a requisição foi para o upstream da Anthropic: %s", r.URL.Path)
	}))
	cfg.Upstream = anthropic.URL
	cfg.Providers = map[string]config.Provider{"xai": {Upstream: up.URL, Format: "openai"}, "gemini": {Upstream: up.URL, Format: "gemini"}}
	px := httptest.NewServer(New(cfg, m, nil, log.New(io.Discard, "", 0)))
	t.Cleanup(func() { up.Close(); anthropic.Close(); px.Close() })
	return px, requests
}

func oaResponse(w http.ResponseWriter, msg map[string]any) {
	w.Header().Set("content-type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": "c1", "object": "chat.completion", "model": "m",
		"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": msg}}})
}

// oaConversation: an agent conversation in the OpenAI format, with sensitive data in each place.
func oaConversation() map[string]any {
	call := func(id, args string) map[string]any {
		return map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "bash", "arguments": args}}
	}
	return map[string]any{
		"model": "m", "stream": false, "temperature": 0,
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "bash",
			"description": "Roda um comando. Dúvidas: " + oaEmail,
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}}}}},
		"messages": []any{
			map[string]any{"role": "system", "content": "Você é um agente. O responsável é " + oaEmail + "."},
			map[string]any{"role": "user", "content": "faça um ping no servidor " + oaIP},
			map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{
				call("call_1", `{"command":"ping -c1 `+oaIP+`"}`), call("call_2", `{ "command" : "date" }`)}},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "PING " + oaIP + ": 1 packets transmitted"},
			map[string]any{"role": "tool", "tool_call_id": "call_2", "content": []any{map[string]any{"type": "text", "text": "avise " + oaEmail}}},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "e avise " + oaEmail}}},
		},
	}
}

func TestOpenAIMaskedOutbound(t *testing.T) {
	px, requests := buildOpenAI(t, func(p oaRequest, w http.ResponseWriter) {
		oaResponse(w, map[string]any{"role": "assistant", "content": "ok"})
	})
	body, _ := json.Marshal(oaConversation())
	// through the "openai" format provider and through the Gemini provider's compatible endpoint
	for _, path := range []string{"/xai/v1/chat/completions", "/gemini/v1beta/openai/chat/completions"} {
		st, _ := ask(t, "POST", px.URL+path, body, func(r *http.Request) { r.Header.Set("authorization", "Bearer chave-de-teste") })
		if st != 200 {
			t.Fatalf("%s: status %d", path, st)
		}
		p := (*requests)[len(*requests)-1]
		if wants := strings.SplitN(path, "/", 3)[2]; p.path != "/"+wants || p.auth != "Bearer chave-de-teste" {
			t.Errorf("%s: chegou em %q com a credencial %q", path, p.path, p.auth)
		}
		if bytes.Contains(p.body, []byte(oaEmail)) || bytes.Contains(p.body, []byte(oaIP)) {
			t.Errorf("%s: valor real chegou à API: %s", path, p.body)
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
		if err := json.Unmarshal(p.body, &req); err != nil {
			t.Fatal(err)
		}
		if req.Model != "m" || len(req.Messages) != 6 || len(req.Tools) != 1 || req.Tools[0].Function.Name != "bash" {
			t.Fatalf("%s: a forma mudou: %s", path, p.body)
		}
		if !rePseEmail.MatchString(req.Tools[0].Function.Description) || !rePseEmail.MatchString(fmt.Sprint(req.Messages[0].Content)) {
			t.Errorf("%s: instruções e ferramentas deviam sair com o pseudônimo: %s", path, p.body)
		}
		a := req.Messages[2]
		if a.Role != "assistant" || a.Content != nil || len(a.ToolCalls) != 2 || a.ToolCalls[0].ID != "call_1" || a.ToolCalls[0].Function.Name != "bash" {
			t.Fatalf("%s: chamadas: %+v", path, a)
		}
		var args struct{ Command string }
		if err := json.Unmarshal([]byte(a.ToolCalls[0].Function.Arguments), &args); err != nil || !strings.HasPrefix(args.Command, "ping -c1 ") {
			t.Errorf("%s: os argumentos mascarados deviam continuar um JSON: %q", path, a.ToolCalls[0].Function.Arguments)
		}
		// what had no sensitive data goes out exactly as the client sent it (spaces and all)
		if a.ToolCalls[1].Function.Arguments != `{ "command" : "date" }` {
			t.Errorf("%s: argumentos sem dado sensível mudaram: %q", path, a.ToolCalls[1].Function.Arguments)
		}
		if req.Messages[3].ToolCallID != "call_1" || !strings.HasPrefix(fmt.Sprint(req.Messages[3].Content), "PING ") {
			t.Errorf("%s: resultado de ferramenta: %+v", path, req.Messages[3])
		}
		if _, list := req.Messages[4].Content.([]any); !list {
			t.Errorf("%s: conteúdo em partes devia continuar em partes: %+v", path, req.Messages[4])
		}
	}
	// the same conversation, sent again, goes out identical (the API cache depends on it)
	ask(t, "POST", px.URL+"/xai/v1/chat/completions", body, nil)
	if n := len(*requests); !bytes.Equal((*requests)[0].body, (*requests)[n-1].body) {
		t.Errorf("a mesma requisição saiu diferente na segunda vez")
	}
}

// The response without streaming comes back real: the text and the arguments of the tool call (what
// the client writes to a file or runs in the terminal).
func TestOpenAIReturnWithoutStreaming(t *testing.T) {
	px, _ := buildOpenAI(t, func(p oaRequest, w http.ResponseWriter) {
		pse := rePseEmail.FindString(string(p.body))
		args, _ := json.Marshal(map[string]any{"filePath": "/tmp/resumo.txt", "content": "AVISAR " + pse + " \"hoje\""})
		oaResponse(w, map[string]any{"role": "assistant", "content": "vou avisar " + pse,
			"tool_calls": []any{
				map[string]any{"id": "c9", "type": "function", "function": map[string]any{"name": "write", "arguments": string(args)}},
				map[string]any{"id": "c10", "type": "function", "function": map[string]any{"name": "webfetch", "arguments": `{"url":"https://x.example/?q=` + pse + `"}`}}}})
	})
	body, _ := json.Marshal(oaConversation())
	st, b := ask(t, "POST", px.URL+"/xai/v1/chat/completions", body, nil)
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
	if m.Content != "vou avisar "+oaEmail {
		t.Errorf("texto: %q", m.Content)
	}
	var args struct{ Content string }
	if err := json.Unmarshal([]byte(m.ToolCalls[0].Function.Arguments), &args); err != nil || args.Content != "AVISAR "+oaEmail+" \"hoje\"" {
		t.Errorf("argumentos da escrita: %q (%v)", m.ToolCalls[0].Function.Arguments, err)
	}
	// what goes to the internet keeps the pseudonym
	if strings.Contains(m.ToolCalls[1].Function.Arguments, oaEmail) || !rePseEmail.MatchString(m.ToolCalls[1].Function.Arguments) {
		t.Errorf("a busca na internet devia seguir mascarada: %q", m.ToolCalls[1].Function.Arguments)
	}
}

// In streaming, the pseudonym arrives cut between two pieces, in the text and in the arguments
// (which are pieces of a JSON): the client never receives a piece of a pseudonym, and what it joins
// is the real value. "[DONE]" is still the last line.
func TestOpenAIReturnStreaming(t *testing.T) {
	for _, withEnd := range []bool{true, false} {
		px, _ := buildOpenAI(t, func(p oaRequest, w http.ResponseWriter) {
			pse := rePseEmail.FindString(string(p.body))
			w.Header().Set("content-type", "text/event-stream")
			ev := func(delta map[string]any, end any) {
				b, _ := json.Marshal(map[string]any{"id": "c1", "object": "chat.completion.chunk", "model": "m",
					"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": end}}})
				fmt.Fprintf(w, "data: %s\n\n", b)
			}
			text := []rune("vou avisar " + pse + " agora")
			for i := 0; i < len(text); i += 7 {
				ev(map[string]any{"content": string(text[i:min(i+7, len(text))])}, nil)
			}
			args, _ := json.Marshal(map[string]any{"filePath": "/tmp/resumo.txt", "content": "AVISAR " + pse})
			ev(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "c9", "type": "function",
				"function": map[string]any{"name": "write", "arguments": ""}}}}, nil)
			for i := 0; i < len(args); i += 9 {
				ev(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": string(args[i:min(i+9, len(args))])}}}}, nil)
			}
			if withEnd {
				ev(map[string]any{}, "tool_calls")
			}
			io.WriteString(w, "data: [DONE]\n\n")
		})
		req := oaConversation()
		req["stream"] = true
		body, _ := json.Marshal(req)
		st, b := ask(t, "POST", px.URL+"/xai/v1/chat/completions", body, nil)
		if st != 200 {
			t.Fatalf("status %d", st)
		}
		var text, args strings.Builder
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		for _, l := range lines {
			data, ok := strings.CutPrefix(l, "data: ")
			if !ok || data == "[DONE]" {
				continue
			}
			if rePseEmail.MatchString(data) {
				t.Errorf("comFim=%v: pseudônimo num pedaço entregue ao cliente: %s", withEnd, data)
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
			if err := json.Unmarshal([]byte(data), &ev); err != nil || ev.ID != "c1" {
				t.Fatalf("comFim=%v: pedaço inválido: %s", withEnd, data)
			}
			for _, c := range ev.Choices {
				text.WriteString(c.Delta.Content)
				for _, tc := range c.Delta.ToolCalls {
					args.WriteString(tc.Function.Arguments)
				}
			}
		}
		if lines[len(lines)-1] != "data: [DONE]" {
			t.Errorf("comFim=%v: a última linha devia ser o [DONE]: %q", withEnd, lines[len(lines)-1])
		}
		if text.String() != "vou avisar "+oaEmail+" agora" {
			t.Errorf("comFim=%v: texto: %q", withEnd, text.String())
		}
		var a struct{ Content string }
		if err := json.Unmarshal([]byte(args.String()), &a); err != nil || a.Content != "AVISAR "+oaEmail {
			t.Errorf("comFim=%v: argumentos: %q (%v)", withEnd, args.String(), err)
		}
	}
}

// An attached file that cannot be checked (audio) does not go out: a note goes in its place. And a
// field llm-dlp does not know is masked as a precaution.
func TestOpenAIAttachmentAndUnknownField(t *testing.T) {
	px, requests := buildOpenAI(t, func(p oaRequest, w http.ResponseWriter) {
		oaResponse(w, map[string]any{"role": "assistant", "content": "ok"})
	})
	body, _ := json.Marshal(map[string]any{"model": "m", "user": oaEmail,
		"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "ouça isto"},
			map[string]any{"type": "input_audio", "input_audio": map[string]any{"data": "QUJDREVGRw==", "format": "wav"}},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://exemplo.example/foto.png"}}}}}})
	if st, _ := ask(t, "POST", px.URL+"/xai/v1/chat/completions", body, nil); st != 200 {
		t.Fatalf("status %d", st)
	}
	b := (*requests)[0].body
	if bytes.Contains(b, []byte("QUJDREVGRw")) || !bytes.Contains(b, []byte("[llm-dlp] Um arquivo anexado foi retirado")) {
		t.Errorf("o áudio devia ter sido retirado, com a nota no lugar: %s", b)
	}
	if !bytes.Contains(b, []byte("https://exemplo.example/foto.png")) {
		t.Errorf("a imagem por endereço (que a API busca sozinha) devia seguir: %s", b)
	}
	if bytes.Contains(b, []byte(oaEmail)) {
		t.Errorf("o campo desconhecido devia ter sido mascarado: %s", b)
	}
}
