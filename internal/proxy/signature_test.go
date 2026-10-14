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

// The subscriptions: Gemini CLI with the Google account (Code Assist) and Codex with the ChatGPT
// account (Responses format). Nothing real reaches the API, the envelope and the ids do not change,
// and the response comes back real, in pieces and whole.

type assistRequest struct {
	path, auth string
	body       []byte
}

// buildSignatures: proxy with the "codeassist" and "chatgpt" providers pointing to a fake API.
func buildSignatures(t *testing.T, resp func(p assistRequest, w http.ResponseWriter)) (*httptest.Server, *[]assistRequest, *Proxy) {
	t.Helper()
	cfg := config.Default()
	cfg.InternalDomains = []string{"empresa-ficticia"}
	m, err := mask.NewMasker(cfg, testKey, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	requests := &[]assistRequest{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		p := assistRequest{r.URL.RequestURI(), r.Header.Get("authorization"), b}
		*requests = append(*requests, p)
		resp(p, w)
	}))
	anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the request went to the Anthropic upstream: %s", r.URL.Path)
	}))
	cfg.Upstream = anthropic.URL
	cfg.Providers = map[string]config.Provider{"codeassist": {Upstream: up.URL, Format: "codeassist"},
		"chatgpt": {Upstream: up.URL + "/backend-api/codex", Format: "responses"}}
	p := New(cfg, m, nil, log.New(io.Discard, "", 0))
	px := httptest.NewServer(p)
	t.Cleanup(func() { up.Close(); anthropic.Close(); px.Close() })
	return px, requests, p
}

func sseEvents(w http.ResponseWriter, events ...map[string]any) {
	w.Header().Set("content-type", "text/event-stream")
	for _, ev := range events {
		b, _ := json.Marshal(ev)
		if t, ok := ev["type"].(string); ok {
			fmt.Fprintf(w, "event: %s\n", t)
		}
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
}

// ---- Gemini CLI with the Google account
// ----------------------------------------------------------

func codeAssistReq() []byte {
	b, _ := json.Marshal(map[string]any{"model": "gemini-2.5-pro", "project": "projeto-123", "user_prompt_id": "upi-1",
		"request": map[string]any{
			"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": "o dono é " + oaEmail}}},
			"contents": []any{contentG("user", part("text", "faça ping em "+oaIP)),
				contentG("model", part("functionCall", map[string]any{"name": "run_shell_command", "args": map[string]any{"command": "ping " + oaIP}})),
				contentG("user", part("functionResponse", map[string]any{"name": "run_shell_command", "response": map[string]any{"output": "ok " + oaEmail}}))},
			"session_id": "8f14e45f-ceea-467a-9ad7-0b5d1a2c3e4f"}})
	return b
}

func TestCodeAssistRoundTrip(t *testing.T) {
	px, requests, p := buildSignatures(t, func(pd assistRequest, w http.ResponseWriter) {
		pse := rePseEmail.FindString(string(pd.body))
		if strings.Contains(pd.path, "streamGenerateContent") {
			// the pseudonym arrives cut between two pieces, as Code Assist sends it
			sseEvents(w, map[string]any{"response": map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model",
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
	st, b := ask(t, "POST", px.URL+"/codeassist/v1internal:streamGenerateContent?alt=sse", codeAssistReq(), func(r *http.Request) { r.Header.Set("authorization", "Bearer token-google") })
	if st != 200 {
		t.Fatalf("status %d", st)
	}
	pd := (*requests)[0]
	if pd.path != "/v1internal:streamGenerateContent?alt=sse" || pd.auth != "Bearer token-google" {
		t.Errorf("arrived at %q with %q", pd.path, pd.auth)
	}
	if bytes.Contains(pd.body, []byte(oaEmail)) || bytes.Contains(pd.body, []byte(oaIP)) {
		t.Errorf("real value reached the API: %s", pd.body)
	}
	var env struct {
		Model, Project string
		UserPromptID   string `json:"user_prompt_id"`
		Request        struct {
			SessionID string `json:"session_id"`
		}
	}
	json.Unmarshal(pd.body, &env)
	if env.Model != "gemini-2.5-pro" || env.Project != "projeto-123" || env.UserPromptID != "upi-1" || env.Request.SessionID != "8f14e45f-ceea-467a-9ad7-0b5d1a2c3e4f" {
		t.Errorf("the envelope changed: %+v", env)
	}
	var text strings.Builder
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
					text.WriteString(pt.Text)
				}
			}
		}
	}
	if text.String() != "avise "+oaEmail+" hoje" || rePseEmail.Match(b) {
		t.Errorf("streaming: %q", text.String())
	}

	_, b = ask(t, "POST", px.URL+"/codeassist/v1internal:generateContent", codeAssistReq(), nil)
	if !bytes.Contains(b, []byte(`"avise `+oaEmail+`"`)) || !bytes.Contains(b, []byte(`"content":"`+oaEmail+`"`)) || !bytes.Contains(b, []byte(`"traceId":"tr-2"`)) {
		t.Errorf("without streaming: %s", b)
	}
}

// The account methods (login, quota, experiments) pass as they came: they carry no conversation,
// and the project and the ids cannot change.
func TestCodeAssistAccountMethodsPassAsIs(t *testing.T) {
	px, requests, _ := buildSignatures(t, func(pd assistRequest, w http.ResponseWriter) {
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"currentTier":{"id":"free-tier"}}`))
	})
	body := []byte(`{"cloudaicompanionProject":"projeto-empresa-ficticia-intra","metadata":{"ideType":"IDE_UNSPECIFIED","platform":"LINUX_AMD64","pluginType":"GEMINI"}}`)
	for _, method := range []string{"loadCodeAssist", "onboardUser", "retrieveUserQuota"} {
		if st, _ := ask(t, "POST", px.URL+"/codeassist/v1internal:"+method, body, nil); st != 200 {
			t.Fatalf("%s: status %d", method, st)
		}
		if got := (*requests)[len(*requests)-1].body; !bytes.Equal(got, body) {
			t.Errorf("%s: the body changed:\n %s\n %s", method, body, got)
		}
	}
}

// ---- Codex (Responses) ------------------------------------------------------------------------

const patchGrammar = "start: begin_patch hunk+ end_patch\nbegin_patch: \"*** Begin Patch\" LF"

func responsesReq(stream bool) []byte {
	b, _ := json.Marshal(map[string]any{"model": "gpt-5-codex", "stream": stream, "store": false,
		"prompt_cache_key": "0f3c2b1a-9e8d-4c7b-a6f5-e4d3c2b1a090", "instructions": "Você é o Codex. Dono: " + oaEmail,
		"tools": []any{
			map[string]any{"type": "function", "name": "shell", "description": "roda comandos", "strict": false,
				"parameters": map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "array"}}}},
			map[string]any{"type": "custom", "name": "apply_patch", "description": "edita arquivos",
				"format": map[string]any{"type": "grammar", "syntax": "lark", "definition": patchGrammar}}},
		"input": []any{
			map[string]any{"type": "message", "role": "developer", "content": []any{map[string]any{"type": "input_text", "text": "cwd do cliente " + oaEmail}}},
			map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "corrija o host " + oaIP}}},
			map[string]any{"type": "reasoning", "summary": []any{}, "encrypted_content": "gAAAAB-opaco"},
			map[string]any{"type": "function_call", "name": "shell", "arguments": `{"command":["bash","-lc","ping ` + oaIP + `"]}`, "call_id": "c1"},
			map[string]any{"type": "function_call_output", "call_id": "c1", "output": "PING " + oaIP + " ok, avisar " + oaEmail},
			map[string]any{"type": "function_call", "name": "shell", "arguments": `{ "command" : ["date"] }`, "call_id": "c2"},
			map[string]any{"type": "function_call_output", "call_id": "c2", "output": "hoje"},
			map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n+host = " + oaIP + "\n*** End Patch", "call_id": "c3"},
			map[string]any{"type": "custom_tool_call_output", "call_id": "c3", "output": "Success"},
		}})
	return b
}

func TestResponsesMaskedOutbound(t *testing.T) {
	px, requests, _ := buildSignatures(t, func(pd assistRequest, w http.ResponseWriter) {
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"output":[]}`))
	})
	st, _ := ask(t, "POST", px.URL+"/chatgpt/responses", responsesReq(false), func(r *http.Request) { r.Header.Set("authorization", "Bearer token-chatgpt") })
	if st != 200 {
		t.Fatalf("status %d", st)
	}
	pd := (*requests)[0]
	if pd.path != "/backend-api/codex/responses" || pd.auth != "Bearer token-chatgpt" {
		t.Errorf("arrived at %q with %q", pd.path, pd.auth)
	}
	if bytes.Contains(pd.body, []byte(oaEmail)) || bytes.Contains(pd.body, []byte(oaIP)) {
		t.Fatalf("real value reached the API: %s", pd.body)
	}
	var req struct {
		Model, Instructions string
		PromptCacheKey      string `json:"prompt_cache_key"`
		Tools               []map[string]any
		Input               []map[string]any
	}
	if err := json.Unmarshal(pd.body, &req); err != nil {
		t.Fatal(err)
	}
	if req.Model != "gpt-5-codex" || req.PromptCacheKey != "0f3c2b1a-9e8d-4c7b-a6f5-e4d3c2b1a090" || len(req.Input) != 9 || len(req.Tools) != 2 {
		t.Fatalf("the shape changed: %s", pd.body)
	}
	if !rePseEmail.MatchString(req.Instructions) {
		t.Errorf("instructions: %q", req.Instructions)
	}
	if f, _ := req.Tools[1]["format"].(map[string]any); f["definition"] != patchGrammar || req.Tools[1]["name"] != "apply_patch" {
		t.Errorf("the apply_patch grammar changed: %v", req.Tools[1])
	}
	if req.Input[2]["encrypted_content"] != "gAAAAB-opaco" {
		t.Errorf("the reasoning changed: %v", req.Input[2])
	}
	var args struct{ Command []string }
	if err := json.Unmarshal([]byte(req.Input[3]["arguments"].(string)), &args); err != nil || len(args.Command) != 3 || !strings.HasPrefix(args.Command[2], "ping ") {
		t.Errorf("masked arguments should still be JSON: %v", req.Input[3]["arguments"])
	}
	if req.Input[5]["arguments"] != `{ "command" : ["date"] }` {
		t.Errorf("arguments without sensitive data changed: %v", req.Input[5]["arguments"])
	}
	if in, _ := req.Input[7]["input"].(string); !strings.HasPrefix(in, "*** Begin Patch\n+host = ") || strings.Contains(in, oaIP) {
		t.Errorf("patch: %q", in)
	}
	if out, _ := req.Input[4]["output"].(string); !strings.HasPrefix(out, "PING ") {
		t.Errorf("result: %v", req.Input[4]["output"])
	}
	// the same request again goes out identical (the API cache depends on it)
	ask(t, "POST", px.URL+"/chatgpt/responses", responsesReq(false), nil)
	if n := len(*requests); !bytes.Equal((*requests)[0].body, (*requests)[n-1].body) {
		t.Errorf("the same request went out differently the second time")
	}
}

// In streaming, the text, the arguments and the patch arrive in pieces, with the pseudonym cut; the
// client never receives a piece of a pseudonym, and both the joined pieces and the events with the
// whole item carry the real value. An internet tool keeps the pseudonym.
func TestResponsesReturnStreaming(t *testing.T) {
	px, _, _ := buildSignatures(t, func(pd assistRequest, w http.ResponseWriter) {
		pse := rePseEmail.FindString(string(pd.body))
		text := "avise " + pse + " agora"
		args := `{"command":["bash","-lc","echo ` + pse + `"]}`
		patch := "*** Begin Patch\n+dono = " + pse + "\n*** End Patch"
		var evs []map[string]any
		add := func(ev map[string]any) { evs = append(evs, ev) }
		add(map[string]any{"type": "response.created", "response": map[string]any{"id": "r1"}})
		add(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"type": "message", "id": "m1", "role": "assistant", "content": []any{}}})
		for i := 0; i < len(text); i += 5 {
			add(map[string]any{"type": "response.output_text.delta", "item_id": "m1", "output_index": 0, "content_index": 0, "delta": text[i:min(i+5, len(text))]})
		}
		add(map[string]any{"type": "response.output_text.done", "item_id": "m1", "output_index": 0, "content_index": 0, "text": text})
		itemMsg := map[string]any{"type": "message", "id": "m1", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text}}}
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
			map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": text}}},
			map[string]any{"type": "function_call", "name": "shell", "arguments": args},
			map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": patch}}}})
		sseEvents(w, evs...)
	})
	st, b := ask(t, "POST", px.URL+"/chatgpt/responses", responsesReq(true), nil)
	if st != 200 {
		t.Fatalf("status %d", st)
	}
	var text, args, patch, web strings.Builder
	var dones []string
	names := 0
	for _, block := range strings.Split(strings.TrimSpace(string(b)), "\n\n") {
		var name, data string
		for _, l := range strings.Split(block, "\n") {
			if n, ok := strings.CutPrefix(l, "event: "); ok {
				name = n
			} else if d, ok := strings.CutPrefix(l, "data: "); ok {
				data = d
			}
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			t.Fatalf("invalid event: %q", block)
		}
		if name != ev["type"] {
			t.Errorf("the event name (%q) does not match the type (%v)", name, ev["type"])
		}
		names++
		d, _ := ev["delta"].(string)
		switch ev["type"] {
		case "response.output_text.delta":
			text.WriteString(d)
		case "response.function_call_arguments.delta":
			if ev["item_id"] == "w1" {
				web.WriteString(d)
			} else {
				args.WriteString(d)
			}
		case "response.custom_tool_call_input.delta":
			patch.WriteString(d)
		default:
			if ev["item_id"] != "w1" && !strings.Contains(data, "webfetch") {
				dones = append(dones, data)
			}
		}
		if ev["item_id"] != "w1" && !strings.Contains(data, "webfetch") && rePseEmail.MatchString(data) {
			t.Errorf("pseudonym delivered to the client: %s", data)
		}
	}
	if text.String() != "avise "+oaEmail+" agora" {
		t.Errorf("text: %q", text.String())
	}
	var a struct{ Command []string }
	if err := json.Unmarshal([]byte(args.String()), &a); err != nil || a.Command[2] != "echo "+oaEmail {
		t.Errorf("argumentos: %q (%v)", args.String(), err)
	}
	if patch.String() != "*** Begin Patch\n+dono = "+oaEmail+"\n*** End Patch" {
		t.Errorf("patch: %q", patch.String())
	}
	if !rePseEmail.MatchString(web.String()) {
		t.Errorf("the internet search should go on with the pseudonym: %q", web.String())
	}
	reals := 0
	for _, d := range dones {
		reals += strings.Count(d, oaEmail)
	}
	// text.done, message item, arguments.done, call item, input.done, patch item and the 3 items of
	// response.completed
	if reals != 9 {
		t.Errorf("the events with the whole item should bring the real value 9 times, they brought %d", reals)
	}
}

// Without streaming, the text and the tool inputs come back real.
func TestResponsesReturnWithoutStreaming(t *testing.T) {
	px, _, _ := buildSignatures(t, func(pd assistRequest, w http.ResponseWriter) {
		pse := rePseEmail.FindString(string(pd.body))
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "r1", "output": []any{
			map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": "pensando em " + pse}}},
			map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "avise " + pse}}},
			map[string]any{"type": "function_call", "name": "shell", "arguments": `{"command":["echo","` + pse + `"]}`},
			map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "+dono = " + pse}}})
	})
	_, b := ask(t, "POST", px.URL+"/chatgpt/responses", responsesReq(false), nil)
	if strings.Count(string(b), oaEmail) != 3 {
		t.Errorf("the real value should come back 3 times (text, arguments, patch): %s", b)
	}
	if !bytes.Contains(b, []byte("pensando em p.")) {
		t.Errorf("the reasoning summary does not change: %s", b)
	}
}

// The format comes from the provider, and the path decides when the provider serves more than one:
// /responses is always Responses (the OpenAI key and the ChatGPT subscription), /chat/completions
// is always Chat Completions, and the Code Assist account methods pass as they came.
func TestDestinationByPath(t *testing.T) {
	cfg := config.Default()
	p := &Proxy{cfg: cfg}
	for path, wants := range map[string]string{
		"/v1/messages": "anthropic",
		"/gemini/v1beta/models/m:streamGenerateContent":  "gemini",
		"/gemini/v1beta/openai/chat/completions":         "openai",
		"/openai/v1/chat/completions":                    "openai",
		"/openai/v1/responses":                           "responses",
		"/chatgpt/responses":                             "responses",
		"/xai/v1/chat/completions":                       "openai",
		"/codeassist/v1internal:streamGenerateContent":   "codeassist",
		"/codeassist/v1internal:countTokens":             "codeassist",
		"/codeassist/v1internal:loadCodeAssist":          accountFormat,
		"/codeassist/v1internal:recordCodeAssistMetrics": "codeassist",
	} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8787"+path, nil)
		if came, _, _ := p.destination(r); came != wants {
			t.Errorf("%s: format %q, expected %q", path, came, wants)
		}
	}
}
