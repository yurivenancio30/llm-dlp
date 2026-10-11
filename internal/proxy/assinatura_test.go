package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// As assinaturas: o Gemini CLI com a conta Google (Code Assist) e o Codex com a conta do ChatGPT
// (formato Responses). Nada real chega à API, o envelope e os ids não mudam, e a resposta volta
// real, em pedaços e inteira.

type pedidoAss struct {
	caminho, auth string
	corpo         []byte
}

// montarAssinaturas: proxy com os provedores "codeassist" e "chatgpt" apontando para uma API falsa.
func montarAssinaturas(t *testing.T, resp func(p pedidoAss, w http.ResponseWriter)) (*httptest.Server, *[]pedidoAss, *Proxy) {
	t.Helper()
	cfg := config.Padrao()
	cfg.DominiosInternos = []string{"empresa-ficticia"}
	m, err := mask.NovoMasker(cfg, chaveTeste, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pedidos := &[]pedidoAss{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		p := pedidoAss{r.URL.RequestURI(), r.Header.Get("authorization"), b}
		*pedidos = append(*pedidos, p)
		resp(p, w)
	}))
	anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a requisição foi para o upstream da Anthropic: %s", r.URL.Path)
	}))
	cfg.Upstream = anthropic.URL
	cfg.Provedores = map[string]config.Provedor{"codeassist": {Upstream: up.URL, Formato: "codeassist"},
		"chatgpt": {Upstream: up.URL + "/backend-api/codex", Formato: "responses"}}
	p := Novo(cfg, m, nil, log.New(io.Discard, "", 0))
	px := httptest.NewServer(p)
	t.Cleanup(func() { up.Close(); anthropic.Close(); px.Close() })
	return px, pedidos, p
}

func sseEventos(w http.ResponseWriter, eventos ...map[string]any) {
	w.Header().Set("content-type", "text/event-stream")
	for _, ev := range eventos {
		b, _ := json.Marshal(ev)
		if t, ok := ev["type"].(string); ok {
			fmt.Fprintf(w, "event: %s\n", t)
		}
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
}

// ---- Gemini CLI com a conta Google ------------------------------------------------------------

func codeAssistReq() []byte {
	b, _ := json.Marshal(map[string]any{"model": "gemini-2.5-pro", "project": "projeto-123", "user_prompt_id": "upi-1",
		"request": map[string]any{
			"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": "o dono é " + emailOA}}},
			"contents": []any{conteudoG("user", parte("text", "faça ping em "+ipOA)),
				conteudoG("model", parte("functionCall", map[string]any{"name": "run_shell_command", "args": map[string]any{"command": "ping " + ipOA}})),
				conteudoG("user", parte("functionResponse", map[string]any{"name": "run_shell_command", "response": map[string]any{"output": "ok " + emailOA}}))},
			"session_id": "8f14e45f-ceea-467a-9ad7-0b5d1a2c3e4f"}})
	return b
}

func TestCodeAssistIdaEVolta(t *testing.T) {
	px, pedidos, p := montarAssinaturas(t, func(pd pedidoAss, w http.ResponseWriter) {
		pse := rePseEmail.FindString(string(pd.corpo))
		if strings.Contains(pd.caminho, "streamGenerateContent") {
			// o pseudônimo chega cortado entre dois pedaços, como o Code Assist manda
			sseEventos(w, map[string]any{"response": map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model",
				"parts": []any{map[string]any{"text": "avise " + pse[:6]}}}}}}, "traceId": "tr-1"},
				map[string]any{"response": map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"role": "model",
					"parts": []any{map[string]any{"text": pse[6:] + " hoje"}}}}}}, "traceId": "tr-1"})
			return
		}
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"traceId": "tr-2", "response": map[string]any{"candidates": []any{map[string]any{
			"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": "avise " + pse},
				map[string]any{"functionCall": map[string]any{"name": "write_file", "args": map[string]any{"content": pse}}}}}}}}})
	})
	_ = p
	st, b := pedir(t, "POST", px.URL+"/codeassist/v1internal:streamGenerateContent?alt=sse", codeAssistReq(), func(r *http.Request) { r.Header.Set("authorization", "Bearer token-google") })
	if st != 200 {
		t.Fatalf("status %d", st)
	}
	pd := (*pedidos)[0]
	if pd.caminho != "/v1internal:streamGenerateContent?alt=sse" || pd.auth != "Bearer token-google" {
		t.Errorf("chegou em %q com %q", pd.caminho, pd.auth)
	}
	if bytes.Contains(pd.corpo, []byte(emailOA)) || bytes.Contains(pd.corpo, []byte(ipOA)) {
		t.Errorf("valor real chegou à API: %s", pd.corpo)
	}
	var env struct {
		Model, Project string
		UserPromptID   string `json:"user_prompt_id"`
		Request        struct {
			SessionID string `json:"session_id"`
		}
	}
	json.Unmarshal(pd.corpo, &env)
	if env.Model != "gemini-2.5-pro" || env.Project != "projeto-123" || env.UserPromptID != "upi-1" || env.Request.SessionID != "8f14e45f-ceea-467a-9ad7-0b5d1a2c3e4f" {
		t.Errorf("o envelope mudou: %+v", env)
	}
	var texto strings.Builder
	for _, l := range strings.Split(string(b), "\n") {
		if d, ok := strings.CutPrefix(l, "data: "); ok {
			var ev struct {
				TraceID  string
				Response struct {
					Candidates []struct {
						Content struct{ Parts []struct{ Text string } }
					}
				}
			}
			json.Unmarshal([]byte(d), &ev)
			for _, c := range ev.Response.Candidates {
				for _, pt := range c.Content.Parts {
					texto.WriteString(pt.Text)
				}
			}
		}
	}
	if texto.String() != "avise "+emailOA+" hoje" || rePseEmail.Match(b) {
		t.Errorf("streaming: %q", texto.String())
	}

	_, b = pedir(t, "POST", px.URL+"/codeassist/v1internal:generateContent", codeAssistReq(), nil)
	if !bytes.Contains(b, []byte(`"avise `+emailOA+`"`)) || !bytes.Contains(b, []byte(`"content":"`+emailOA+`"`)) || !bytes.Contains(b, []byte(`"traceId":"tr-2"`)) {
		t.Errorf("sem streaming: %s", b)
	}
}

// Os métodos da conta (login, cota, experimentos) passam como vieram: não levam conversa, e o
// projeto e os ids não podem mudar.
func TestCodeAssistMetodosDaContaPassamComoVieram(t *testing.T) {
	px, pedidos, _ := montarAssinaturas(t, func(pd pedidoAss, w http.ResponseWriter) {
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"currentTier":{"id":"free-tier"}}`))
	})
	corpo := []byte(`{"cloudaicompanionProject":"projeto-empresa-ficticia-intra","metadata":{"ideType":"IDE_UNSPECIFIED","platform":"LINUX_AMD64","pluginType":"GEMINI"}}`)
	for _, metodo := range []string{"loadCodeAssist", "onboardUser", "retrieveUserQuota"} {
		if st, _ := pedir(t, "POST", px.URL+"/codeassist/v1internal:"+metodo, corpo, nil); st != 200 {
			t.Fatalf("%s: status %d", metodo, st)
		}
		if got := (*pedidos)[len(*pedidos)-1].corpo; !bytes.Equal(got, corpo) {
			t.Errorf("%s: o corpo mudou:\n %s\n %s", metodo, corpo, got)
		}
	}
}

// ---- Codex (Responses) ------------------------------------------------------------------------

const gramaticaPatch = "start: begin_patch hunk+ end_patch\nbegin_patch: \"*** Begin Patch\" LF"

func responsesReq(stream bool) []byte {
	b, _ := json.Marshal(map[string]any{"model": "gpt-5-codex", "stream": stream, "store": false,
		"prompt_cache_key": "0f3c2b1a-9e8d-4c7b-a6f5-e4d3c2b1a090", "instructions": "Você é o Codex. Dono: " + emailOA,
		"tools": []any{
			map[string]any{"type": "function", "name": "shell", "description": "roda comandos", "strict": false,
				"parameters": map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "array"}}}},
			map[string]any{"type": "custom", "name": "apply_patch", "description": "edita arquivos",
				"format": map[string]any{"type": "grammar", "syntax": "lark", "definition": gramaticaPatch}}},
		"input": []any{
			map[string]any{"type": "message", "role": "developer", "content": []any{map[string]any{"type": "input_text", "text": "cwd do cliente " + emailOA}}},
			map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "corrija o host " + ipOA}}},
			map[string]any{"type": "reasoning", "summary": []any{}, "encrypted_content": "gAAAAB-opaco"},
			map[string]any{"type": "function_call", "name": "shell", "arguments": `{"command":["bash","-lc","ping ` + ipOA + `"]}`, "call_id": "c1"},
			map[string]any{"type": "function_call_output", "call_id": "c1", "output": "PING " + ipOA + " ok, avisar " + emailOA},
			map[string]any{"type": "function_call", "name": "shell", "arguments": `{ "command" : ["date"] }`, "call_id": "c2"},
			map[string]any{"type": "function_call_output", "call_id": "c2", "output": "hoje"},
			map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n+host = " + ipOA + "\n*** End Patch", "call_id": "c3"},
			map[string]any{"type": "custom_tool_call_output", "call_id": "c3", "output": "Success"},
		}})
	return b
}

func TestResponsesIdaMascarada(t *testing.T) {
	px, pedidos, _ := montarAssinaturas(t, func(pd pedidoAss, w http.ResponseWriter) {
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"output":[]}`))
	})
	st, _ := pedir(t, "POST", px.URL+"/chatgpt/responses", responsesReq(false), func(r *http.Request) { r.Header.Set("authorization", "Bearer token-chatgpt") })
	if st != 200 {
		t.Fatalf("status %d", st)
	}
	pd := (*pedidos)[0]
	if pd.caminho != "/backend-api/codex/responses" || pd.auth != "Bearer token-chatgpt" {
		t.Errorf("chegou em %q com %q", pd.caminho, pd.auth)
	}
	if bytes.Contains(pd.corpo, []byte(emailOA)) || bytes.Contains(pd.corpo, []byte(ipOA)) {
		t.Fatalf("valor real chegou à API: %s", pd.corpo)
	}
	var req struct {
		Model, Instructions string
		PromptCacheKey      string `json:"prompt_cache_key"`
		Tools               []map[string]any
		Input               []map[string]any
	}
	if err := json.Unmarshal(pd.corpo, &req); err != nil {
		t.Fatal(err)
	}
	if req.Model != "gpt-5-codex" || req.PromptCacheKey != "0f3c2b1a-9e8d-4c7b-a6f5-e4d3c2b1a090" || len(req.Input) != 9 || len(req.Tools) != 2 {
		t.Fatalf("a forma mudou: %s", pd.corpo)
	}
	if !rePseEmail.MatchString(req.Instructions) {
		t.Errorf("instruções: %q", req.Instructions)
	}
	if f, _ := req.Tools[1]["format"].(map[string]any); f["definition"] != gramaticaPatch || req.Tools[1]["name"] != "apply_patch" {
		t.Errorf("a gramática do apply_patch mudou: %v", req.Tools[1])
	}
	if req.Input[2]["encrypted_content"] != "gAAAAB-opaco" {
		t.Errorf("o raciocínio mudou: %v", req.Input[2])
	}
	var args struct{ Command []string }
	if err := json.Unmarshal([]byte(req.Input[3]["arguments"].(string)), &args); err != nil || len(args.Command) != 3 || !strings.HasPrefix(args.Command[2], "ping ") {
		t.Errorf("argumentos mascarados deviam continuar um JSON: %v", req.Input[3]["arguments"])
	}
	if req.Input[5]["arguments"] != `{ "command" : ["date"] }` {
		t.Errorf("argumentos sem dado sensível mudaram: %v", req.Input[5]["arguments"])
	}
	if in, _ := req.Input[7]["input"].(string); !strings.HasPrefix(in, "*** Begin Patch\n+host = ") || strings.Contains(in, ipOA) {
		t.Errorf("patch: %q", in)
	}
	if out, _ := req.Input[4]["output"].(string); !strings.HasPrefix(out, "PING ") {
		t.Errorf("resultado: %v", req.Input[4]["output"])
	}
	// a mesma requisição de novo sai idêntica (o cache da API depende disso)
	pedir(t, "POST", px.URL+"/chatgpt/responses", responsesReq(false), nil)
	if n := len(*pedidos); !bytes.Equal((*pedidos)[0].corpo, (*pedidos)[n-1].corpo) {
		t.Errorf("a mesma requisição saiu diferente na segunda vez")
	}
}

// Em streaming, o texto, os argumentos e o patch chegam em pedaços, com o pseudônimo cortado; o
// cliente nunca recebe um pedaço de pseudônimo, e tanto os pedaços juntos quanto os eventos com o
// item inteiro trazem o valor real. Uma ferramenta de internet segue com o pseudônimo.
func TestResponsesVoltaStreaming(t *testing.T) {
	px, _, _ := montarAssinaturas(t, func(pd pedidoAss, w http.ResponseWriter) {
		pse := rePseEmail.FindString(string(pd.corpo))
		texto := "avise " + pse + " agora"
		args := `{"command":["bash","-lc","echo ` + pse + `"]}`
		patch := "*** Begin Patch\n+dono = " + pse + "\n*** End Patch"
		var evs []map[string]any
		add := func(ev map[string]any) { evs = append(evs, ev) }
		add(map[string]any{"type": "response.created", "response": map[string]any{"id": "r1"}})
		add(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"type": "message", "id": "m1", "role": "assistant", "content": []any{}}})
		for i := 0; i < len(texto); i += 5 {
			add(map[string]any{"type": "response.output_text.delta", "item_id": "m1", "output_index": 0, "content_index": 0, "delta": texto[i:min(i+5, len(texto))]})
		}
		add(map[string]any{"type": "response.output_text.done", "item_id": "m1", "output_index": 0, "content_index": 0, "text": texto})
		itemMsg := map[string]any{"type": "message", "id": "m1", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": texto}}}
		add(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": itemMsg})
		add(map[string]any{"type": "response.output_item.added", "output_index": 1, "item": map[string]any{"type": "function_call", "id": "f1", "name": "shell", "call_id": "c9", "arguments": ""}})
		for i := 0; i < len(args); i += 7 {
			add(map[string]any{"type": "response.function_call_arguments.delta", "item_id": "f1", "output_index": 1, "delta": args[i:min(i+7, len(args))]})
		}
		add(map[string]any{"type": "response.function_call_arguments.done", "item_id": "f1", "output_index": 1, "arguments": args})
		itemFc := map[string]any{"type": "function_call", "id": "f1", "name": "shell", "call_id": "c9", "arguments": args}
		add(map[string]any{"type": "response.output_item.done", "output_index": 1, "item": itemFc})
		add(map[string]any{"type": "response.output_item.added", "output_index": 2, "item": map[string]any{"type": "custom_tool_call", "id": "p1", "name": "apply_patch", "call_id": "c10", "input": ""}})
		for i := 0; i < len(patch); i += 6 {
			add(map[string]any{"type": "response.custom_tool_call_input.delta", "item_id": "p1", "output_index": 2, "delta": patch[i:min(i+6, len(patch))]})
		}
		add(map[string]any{"type": "response.custom_tool_call_input.done", "item_id": "p1", "output_index": 2, "input": patch})
		itemP := map[string]any{"type": "custom_tool_call", "id": "p1", "name": "apply_patch", "call_id": "c10", "input": patch}
		add(map[string]any{"type": "response.output_item.done", "output_index": 2, "item": itemP})
		web := `{"url":"https://x.example/?q=` + pse + `"}`
		add(map[string]any{"type": "response.output_item.added", "output_index": 3, "item": map[string]any{"type": "function_call", "id": "w1", "name": "webfetch", "call_id": "c11", "arguments": ""}})
		add(map[string]any{"type": "response.function_call_arguments.delta", "item_id": "w1", "output_index": 3, "delta": web})
		add(map[string]any{"type": "response.function_call_arguments.done", "item_id": "w1", "output_index": 3, "arguments": web})
		add(map[string]any{"type": "response.completed", "response": map[string]any{"id": "r1", "output": []any{
			map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": texto}}},
			map[string]any{"type": "function_call", "name": "shell", "arguments": args},
			map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": patch}}}})
		sseEventos(w, evs...)
	})
	st, b := pedir(t, "POST", px.URL+"/chatgpt/responses", responsesReq(true), nil)
	if st != 200 {
		t.Fatalf("status %d", st)
	}
	var texto, args, patch, web strings.Builder
	var dones []string
	nomes := 0
	for _, bloco := range strings.Split(strings.TrimSpace(string(b)), "\n\n") {
		var nome, dados string
		for _, l := range strings.Split(bloco, "\n") {
			if n, ok := strings.CutPrefix(l, "event: "); ok {
				nome = n
			} else if d, ok := strings.CutPrefix(l, "data: "); ok {
				dados = d
			}
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(dados), &ev); err != nil {
			t.Fatalf("evento inválido: %q", bloco)
		}
		if nome != ev["type"] {
			t.Errorf("o nome do evento (%q) não bate com o tipo (%v)", nome, ev["type"])
		}
		nomes++
		d, _ := ev["delta"].(string)
		switch ev["type"] {
		case "response.output_text.delta":
			texto.WriteString(d)
		case "response.function_call_arguments.delta":
			if ev["item_id"] == "w1" {
				web.WriteString(d)
			} else {
				args.WriteString(d)
			}
		case "response.custom_tool_call_input.delta":
			patch.WriteString(d)
		default:
			if ev["item_id"] != "w1" && !strings.Contains(dados, "webfetch") {
				dones = append(dones, dados)
			}
		}
		if ev["item_id"] != "w1" && !strings.Contains(dados, "webfetch") && rePseEmail.MatchString(dados) {
			t.Errorf("pseudônimo entregue ao cliente: %s", dados)
		}
	}
	if texto.String() != "avise "+emailOA+" agora" {
		t.Errorf("texto: %q", texto.String())
	}
	var a struct{ Command []string }
	if err := json.Unmarshal([]byte(args.String()), &a); err != nil || a.Command[2] != "echo "+emailOA {
		t.Errorf("argumentos: %q (%v)", args.String(), err)
	}
	if patch.String() != "*** Begin Patch\n+dono = "+emailOA+"\n*** End Patch" {
		t.Errorf("patch: %q", patch.String())
	}
	if !rePseEmail.MatchString(web.String()) {
		t.Errorf("a busca na internet devia seguir com o pseudônimo: %q", web.String())
	}
	reais := 0
	for _, d := range dones {
		reais += strings.Count(d, emailOA)
	}
	// text.done, item da mensagem, arguments.done, item da chamada, input.done, item do patch e os 3
	// itens do response.completed
	if reais != 9 {
		t.Errorf("os eventos com o item inteiro deviam trazer o valor real 9 vezes, vieram %d", reais)
	}
}

// Sem streaming, o texto e as entradas das ferramentas voltam reais.
func TestResponsesVoltaSemStreaming(t *testing.T) {
	px, _, _ := montarAssinaturas(t, func(pd pedidoAss, w http.ResponseWriter) {
		pse := rePseEmail.FindString(string(pd.corpo))
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "r1", "output": []any{
			map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": "pensando em " + pse}}},
			map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "avise " + pse}}},
			map[string]any{"type": "function_call", "name": "shell", "arguments": `{"command":["echo","` + pse + `"]}`},
			map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "+dono = " + pse}}})
	})
	_, b := pedir(t, "POST", px.URL+"/chatgpt/responses", responsesReq(false), nil)
	if strings.Count(string(b), emailOA) != 3 {
		t.Errorf("devia voltar o valor real 3 vezes (texto, argumentos, patch): %s", b)
	}
	if !bytes.Contains(b, []byte("pensando em p.")) {
		t.Errorf("o resumo do raciocínio não muda: %s", b)
	}
}

// O formato vem do provedor, e o caminho decide quando o provedor atende mais de um: /responses é
// sempre Responses (a chave da OpenAI e a assinatura do ChatGPT), /chat/completions é sempre Chat
// Completions, e os métodos da conta do Code Assist passam como vieram.
func TestDestinoPorCaminho(t *testing.T) {
	cfg := config.Padrao()
	p := &Proxy{cfg: cfg}
	for caminho, quer := range map[string]string{
		"/v1/messages": "anthropic",
		"/gemini/v1beta/models/m:streamGenerateContent":  "gemini",
		"/gemini/v1beta/openai/chat/completions":         "openai",
		"/openai/v1/chat/completions":                    "openai",
		"/openai/v1/responses":                           "responses",
		"/chatgpt/responses":                             "responses",
		"/xai/v1/chat/completions":                       "openai",
		"/codeassist/v1internal:streamGenerateContent":   "codeassist",
		"/codeassist/v1internal:countTokens":             "codeassist",
		"/codeassist/v1internal:loadCodeAssist":          formatoConta,
		"/codeassist/v1internal:recordCodeAssistMetrics": "codeassist",
	} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8787"+caminho, nil)
		if veio, _, _ := p.destino(r); veio != quer {
			t.Errorf("%s: formato %q, esperado %q", caminho, veio, quer)
		}
	}
}
