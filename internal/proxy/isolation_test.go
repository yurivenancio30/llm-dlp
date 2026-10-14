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

// Support for other APIs (Gemini, OpenAI) changes nothing in the Claude Code path: the same request
// goes out byte for byte equal with or without the providers, what belongs only to those formats
// does not apply to Anthropic, and no malformed request or response of those formats brings the
// proxy down.

// buildClaude: proxy with the fake Anthropic API (resp writes the response) and the given
// providers.
func buildClaude(t *testing.T, provs map[string]config.Provider, resp func(body []byte, w http.ResponseWriter)) (*httptest.Server, *[][]byte) {
	t.Helper()
	cfg := config.Default()
	cfg.InternalDomains = []string{"empresa-ficticia"}
	cfg.Providers = provs
	m, err := mask.NewMasker(cfg, testKey, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	bodies := &[][]byte{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*bodies = append(*bodies, b)
		resp(b, w)
	}))
	cfg.Upstream = up.URL
	px := httptest.NewServer(New(cfg, m, nil, log.New(io.Discard, "", 0)))
	t.Cleanup(func() { up.Close(); px.Close() })
	return px, bodies
}

func claudeResponse(w http.ResponseWriter, content ...any) {
	w.Header().Set("content-type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"type": "message", "role": "assistant", "content": content})
}

// claudeConversation: a Claude Code conversation with sensitive data in the places where the code
// of the other formats could touch it: a "format" field in a tool input, a tool called web_fetch
// (an internet tool name in Gemini CLI) and a schema.
func claudeConversation() []byte {
	b, _ := json.Marshal(map[string]any{"model": "claude", "max_tokens": 100,
		"system": []any{map[string]any{"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude."}},
		"tools": []any{map[string]any{"name": "web_fetch", "description": "busca interna",
			"input_schema": map[string]any{"type": "object", "properties": map[string]any{"url": map[string]any{"type": "string", "format": "uri"}}}}},
		"messages": []any{
			msg("user", "exporte os contatos de "+oaEmail),
			msg("assistant", []any{map[string]any{"type": "tool_use", "id": "t1", "name": "exportar",
				"input": map[string]any{"format": oaEmail, "mimeType": oaIP}}}),
			msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "ok, enviado a " + oaEmail}}),
		}})
	return b
}

func TestClaudeSameWithOrWithoutOtherAPIs(t *testing.T) {
	nothingAtAll := func(_ []byte, w http.ResponseWriter) { claudeResponse(w, map[string]any{"type": "text", "text": "ok"}) }
	without, bodiesWithout := buildClaude(t, nil, nothingAtAll)
	with, bodiesWith := buildClaude(t, config.DefaultProviders(), nothingAtAll)
	for _, px := range []*httptest.Server{without, with} {
		if st, _ := ask(t, "POST", px.URL+"/v1/messages", claudeConversation(), nil); st != 200 {
			t.Fatalf("status %d", st)
		}
	}
	a, b := (*bodiesWithout)[0], (*bodiesWith)[0]
	if !bytes.Equal(a, b) {
		t.Errorf("the Claude request changed with the providers on:\n without: %s\n with:    %s", a, b)
	}
	// fields and tools that only the other formats leave intact stay masked in Claude
	if bytes.Contains(a, []byte(oaEmail)) || bytes.Contains(a, []byte(oaIP)) {
		t.Errorf("real value reached the Anthropic API: %s", a)
	}
}

// On Claude's return path, web_fetch is a tool like any other (the input comes back real); only
// WebFetch, Claude Code's own, keeps the pseudonyms.
func TestClaudeReturnsWithoutOthersTools(t *testing.T) {
	px, _ := buildClaude(t, config.DefaultProviders(), func(body []byte, w http.ResponseWriter) {
		pse := rePseEmail.FindString(string(body))
		claudeResponse(w,
			map[string]any{"type": "tool_use", "id": "a", "name": "web_fetch", "input": map[string]any{"q": pse}},
			map[string]any{"type": "tool_use", "id": "b", "name": "WebFetch", "input": map[string]any{"q": pse}})
	})
	_, b := ask(t, "POST", px.URL+"/v1/messages", claudeConversation(), nil)
	var resp struct {
		Content []struct {
			Name  string
			Input map[string]string
		}
	}
	if err := json.Unmarshal(b, &resp); err != nil || len(resp.Content) != 2 {
		t.Fatalf("response: %s", b)
	}
	if resp.Content[0].Input["q"] != oaEmail {
		t.Errorf("web_fetch in Claude should come back with the real value: %q", resp.Content[0].Input["q"])
	}
	if !rePseEmail.MatchString(resp.Content[1].Input["q"]) {
		t.Errorf("WebFetch should go on with the pseudonym: %q", resp.Content[1].Input["q"])
	}
}

// variations: copies of v with one node replaced by an odd value, one node and one value at a time.
func variations(t *testing.T, v any) [][]byte {
	t.Helper()
	orig, _ := json.Marshal(v)
	var paths [][]any
	var walkStep func(x any, pth []any)
	walkStep = func(x any, pth []any) {
		paths = append(paths, append([]any(nil), pth...))
		switch y := x.(type) {
		case map[string]any:
			for _, k := range keys(y) {
				walkStep(y[k], append(pth, k))
			}
		case []any:
			for i := range y {
				walkStep(y[i], append(pth, i))
			}
		}
	}
	var rootDir any
	json.Unmarshal(orig, &rootDir)
	walkStep(rootDir, nil)
	var out [][]byte
	for _, pth := range paths[1:] {
		for _, odd := range []any{nil, 5, "x", []any{}, map[string]any{}, []any{"x", 5, nil}} {
			var c any
			json.Unmarshal(orig, &c)
			x := c
			for _, p := range pth[:len(pth)-1] {
				if k, ok := p.(string); ok {
					x = x.(map[string]any)[k]
				} else {
					x = x.([]any)[p.(int)]
				}
			}
			if k, ok := pth[len(pth)-1].(string); ok {
				x.(map[string]any)[k] = odd
			} else {
				x.([]any)[pth[len(pth)-1].(int)] = odd
			}
			b, _ := json.Marshal(c)
			out = append(out, b)
		}
	}
	return out
}

// Gemini and OpenAI requests and responses with any node replaced by an odd value: the proxy never
// goes down (it always answers), and Claude keeps working afterwards.
func TestMalformedOtherAPIsDoNotBreakClaude(t *testing.T) {
	cfg := config.Default()
	m, err := mask.NewMasker(cfg, testKey, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var currentResponse []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/v1/messages" {
			claudeResponse(w, map[string]any{"type": "text", "text": "ok"})
			return
		}
		w.Header().Set("content-type", "application/json")
		w.Write(currentResponse)
	}))
	cfg.Upstream = up.URL
	cfg.Providers = map[string]config.Provider{"gemini": {Upstream: up.URL, Format: "gemini"}, "xai": {Upstream: up.URL, Format: "openai"},
		"codeassist": {Upstream: up.URL, Format: "codeassist"}, "chatgpt": {Upstream: up.URL, Format: "responses"}}
	p := New(cfg, m, nil, log.New(io.Discard, "", 0))
	px := httptest.NewServer(p)
	t.Cleanup(func() { up.Close(); px.Close() })

	gem := map[string]any{"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": "sistema " + oaEmail}}},
		"tools": []any{map[string]any{"functionDeclarations": []any{map[string]any{"name": "f", "parameters": map[string]any{"type": "OBJECT"}}}}},
		"contents": []any{contentG("user", part("text", "oi "+oaEmail)),
			contentG("model", part("functionCall", map[string]any{"name": "f", "args": map[string]any{"a": oaIP}})),
			contentG("user", part("functionResponse", map[string]any{"name": "f", "response": map[string]any{"r": oaIP}}),
				part("inlineData", map[string]any{"mimeType": "audio/wav", "data": "AAAA"}))}}
	oa := oaConversation()
	oa["messages"] = append(oa["messages"].([]any), map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,AAAA"}}}})
	pse := p.m.Pseudonym("email", oaEmail)
	respGem := map[string]any{"candidates": []any{map[string]any{"index": 0, "finishReason": "STOP", "content": map[string]any{"role": "model",
		"parts": []any{map[string]any{"text": "oi " + pse}, map[string]any{"functionCall": map[string]any{"name": "f", "args": map[string]any{"a": pse}}}}}}}}
	respOA := map[string]any{"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "oi " + pse,
		"tool_calls": []any{map[string]any{"id": "c", "function": map[string]any{"name": "f", "arguments": `{"a":"` + pse + `"}`}}}}}}}

	var ca, rs any
	json.Unmarshal(codeAssistReq(), &ca)
	json.Unmarshal(responsesReq(false), &rs)
	respCA := map[string]any{"traceId": "t", "response": respGem}
	respRS := map[string]any{"output": []any{
		map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "oi " + pse}}},
		map[string]any{"type": "function_call", "name": "f", "arguments": `{"a":"` + pse + `"}`},
		map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": pse},
		map[string]any{"type": "local_shell_call", "action": map[string]any{"command": []any{"echo", pse}}}}}
	cases := []struct {
		path       string
		req, resp  any
		noStreamOK bool
	}{
		{"/gemini/v1beta/models/m:generateContent", gem, respGem, true},
		{"/xai/v1/chat/completions", oa, respOA, true},
		{"/codeassist/v1internal:generateContent", ca, respCA, true},
		{"/chatgpt/responses", rs, respRS, true},
	}
	n := 0
	for _, c := range cases {
		good, _ := json.Marshal(c.resp)
		reqs := variations(t, c.req)
		resps := variations(t, c.resp)
		for i := 0; i < max(len(reqs), len(resps)); i++ {
			req, _ := json.Marshal(c.req)
			currentResponse = good
			if i < len(reqs) {
				req = reqs[i]
			}
			if i < len(resps) {
				currentResponse = resps[i]
			}
			resp, err := http.Post(px.URL+c.path, "application/json", bytes.NewReader(req))
			if err != nil {
				t.Fatalf("%s: the proxy crashed with\n req:  %s\n resp: %s\n error: %v", c.path, req, currentResponse, err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			n++
		}
	}
	if st, _ := ask(t, "POST", px.URL+"/v1/messages", claudeConversation(), nil); st != 200 {
		t.Errorf("Claude stopped working after the malformed requests: status %d", st)
	}
	t.Logf("%d malformed requests and responses, none took the proxy down", n)
}

// Malformed streaming (pieces that are not JSON, fields with the wrong type, a cut in the middle)
// also never brings the proxy down.
func TestOtherAPIsMalformedStreaming(t *testing.T) {
	cfg := config.Default()
	m, err := mask.NewMasker(cfg, testKey, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := New(cfg, m, nil, log.New(io.Discard, "", 0))
	pse := p.m.Pseudonym("email", oaEmail)
	tab := mask.NewTable([]mask.Entry{{Pseudo: pse, Real: oaEmail, Type: "email"}})
	pieces := []string{
		`{"candidates":[{"content":{"parts":[{"text":"oi ` + pse[:5] + `"}]}}]}`,
		`{"candidates":[{"index":"x","content":{"parts":[5,null,{"text":5},{"functionCall":"x"}]}}]}`,
		`{"candidates":5}`, `{"choices":[{"delta":{"content":5,"tool_calls":[5,{"function":"x"},{"index":"a","function":{"arguments":7}}]}}]}`,
		`{"choices":[{"delta":null,"finish_reason":"stop"}]}`, `não é json`, `[1,2]`, `null`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"f","arguments":"{\"a\":\"` + pse[:7] + `"}}]}}]}`,
		`{"type":"response.output_text.delta","item_id":5,"delta":"` + pse[:5] + `"}`, `{"type":"response.output_text.done","text":5}`,
		`{"type":"response.function_call_arguments.delta","delta":7}`, `{"type":"response.output_item.done","item":"x"}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","arguments":5,"name":["x"]}}`,
		`{"type":"response.custom_tool_call_input.delta","item_id":"p","delta":"` + pse[:9] + `"}`,
		`{"type":"response.completed","response":{"output":[5,null,{"type":"message","content":"x"},{"type":"local_shell_call","action":7}]}}`,
		`{"type":"response.content_part.done","part":5}`, `{"response":{"candidates":"x"}}`, `{"response":5}`,
	}
	for _, end := range []string{"", "data: [DONE]\n\n"} {
		var body strings.Builder
		for _, pc := range pieces {
			fmt.Fprintf(&body, "data: %s\n\n", pc)
		}
		body.WriteString(end)
		for name, f := range map[string]func(http.ResponseWriter, io.Reader, *mask.Table) error{
			"gemini": p.unmaskSSEGemini, "openai": p.unmaskSSEOpenAI, "responses": p.unmaskSSEResponses} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s: the malformed streaming took the proxy down: %v", name, r)
					}
				}()
				f(httptest.NewRecorder(), strings.NewReader(body.String()), tab)
			}()
		}
	}
}
