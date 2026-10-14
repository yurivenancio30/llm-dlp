package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// The Gemini API through the /gemini route: nothing real reaches the API, the response comes back
// real (whole and in streaming) and the history goes out identical from one request to the next.

type geminiRequest struct {
	path, key string
	body      []byte
}

// buildGemini: proxy with the "gemini" provider pointing to a fake API, which answers with resp.
func buildGemini(t *testing.T, f func(*config.Config), resp func(p geminiRequest, w http.ResponseWriter)) (*httptest.Server, *[]geminiRequest) {
	t.Helper()
	cfg := config.Default()
	cfg.InternalDomains = []string{"empresa-ficticia"}
	if f != nil {
		f(&cfg)
	}
	m, err := mask.NewMasker(cfg, testKey, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	requests := &[]geminiRequest{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		p := geminiRequest{r.URL.RequestURI(), r.Header.Get("x-goog-api-key"), b}
		*requests = append(*requests, p)
		resp(p, w)
	}))
	anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a requisição do Gemini foi para o upstream da Anthropic: %s", r.URL.Path)
	}))
	cfg.Upstream = anthropic.URL
	cfg.Providers = map[string]config.Provider{"gemini": {Upstream: up.URL, Format: "gemini"}}
	px := httptest.NewServer(New(cfg, m, nil, log.New(io.Discard, "", 0)))
	t.Cleanup(func() { up.Close(); anthropic.Close(); px.Close() })
	return px, requests
}

func part(k string, v any) map[string]any { return map[string]any{k: v} }

func contentG(hostRole string, parts ...any) map[string]any {
	return map[string]any{"role": hostRole, "parts": parts}
}

const (
	signature = "Q2lRQmxXVnpkR1VnWkdVZ1lYTnphVzVoZEhWeVlRPT0="
	userText  = "o owner " + email + " (cpf " + cpf + ") não acessa " + host
	pathG     = "/v1beta/models/gemini-x:generateContent"
)

func geminiRequestOf() map[string]any {
	return map[string]any{
		"systemInstruction": map[string]any{"parts": []any{part("text", "Você é um agente. Usuário logado: "+email)}},
		"tools": []any{map[string]any{"functionDeclarations": []any{map[string]any{
			"name": "run_shell_command", "description": "Roda um comando. Dúvidas: " + email,
			"parameters": map[string]any{"type": "OBJECT", "required": []any{"command"},
				"properties": map[string]any{"command": map[string]any{"type": "STRING", "description": "o comando"}}}}}}},
		"generationConfig": map[string]any{"temperature": json.Number("0.2"), "responseMimeType": "text/plain"},
		"contents": []any{
			contentG("user", part("text", userText)),
			contentG("model",
				map[string]any{"text": "vou conferir o p.aaaaaaaa", "thought": true},
				map[string]any{"functionCall": map[string]any{"name": "run_shell_command", "args": map[string]any{"command": "ping -c1 " + ip}},
					"thoughtSignature": signature}),
			contentG("user", part("functionResponse", map[string]any{"name": "run_shell_command",
				"response": map[string]any{"output": "PING " + ip + " from " + host, "exitCode": json.Number("0")}})),
		},
	}
}

func postG(t *testing.T, px *httptest.Server, path string, body any) (int, []byte) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", px.URL+"/gemini"+path, bytes.NewReader(b))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-goog-api-key", "chave-de-teste")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

type respG struct {
	Candidates []struct {
		Content struct {
			Parts []map[string]any `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
}

// The request reaches the Gemini API without anything real, on the right path, with the same
// structure; what is not data (reasoning signature, configuration, tool name, schema) does not
// change.
func TestGeminiMaskedOutbound(t *testing.T) {
	px, requests := buildGemini(t, nil, func(p geminiRequest, w http.ResponseWriter) {
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`))
	})
	if st, b := postG(t, px, pathG+"?alt=json", geminiRequestOf()); st != 200 {
		t.Fatalf("status %d: %s", st, b)
	}
	p := (*requests)[0]
	if p.path != pathG+"?alt=json" || p.key != "chave-de-teste" {
		t.Errorf("caminho ou chave não chegaram como vieram: %q %q", p.path, p.key)
	}
	for _, r := range []string{email, ip, host, cpf} {
		if bytes.Contains(p.body, []byte(r)) {
			t.Errorf("vazou %q: %s", r, p.body)
		}
	}
	var wentOut, orig map[string]any
	json.Unmarshal(p.body, &wentOut)
	b, _ := json.Marshal(geminiRequestOf())
	json.Unmarshal(b, &orig)
	if !reflect.DeepEqual(wentOut["generationConfig"], orig["generationConfig"]) {
		t.Errorf("generationConfig mudou: %v", wentOut["generationConfig"])
	}
	fd := wentOut["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)[0].(map[string]any)
	fdOrig := orig["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)[0].(map[string]any)
	if fd["name"] != "run_shell_command" || !reflect.DeepEqual(fd["parameters"], fdOrig["parameters"]) {
		t.Errorf("nome ou schema da ferramenta mudou: %v", fd)
	}
	cs := wentOut["contents"].([]any)
	if len(cs) != 3 {
		t.Fatalf("contents: %d", len(cs))
	}
	model := cs[1].(map[string]any)
	ps := model["parts"].([]any)
	if model["role"] != "model" || len(ps) != 2 {
		t.Fatalf("mensagem do modelo mudou de forma: %v", model)
	}
	if thought := ps[0].(map[string]any); thought["text"] != "vou conferir o p.aaaaaaaa" || thought["thought"] != true {
		t.Errorf("o raciocínio mudou: %v", thought)
	}
	call := ps[1].(map[string]any)
	if call["thoughtSignature"] != signature {
		t.Errorf("a assinatura do raciocínio mudou: %v", call["thoughtSignature"])
	}
	if cmd := call["functionCall"].(map[string]any)["args"].(map[string]any)["command"].(string); !strings.HasPrefix(cmd, "ping -c1 ") {
		t.Errorf("comando: %q", cmd)
	}
	fr := cs[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	output := fr["response"].(map[string]any)
	if fr["name"] != "run_shell_command" || !strings.HasPrefix(output["output"].(string), "PING ") || fmt.Sprint(output["exitCode"]) != "0" {
		t.Errorf("resposta da ferramenta mudou de forma: %v", fr)
	}
}

// The response comes back real: the text and the arguments of the tool call, which the model wrote
// with the pseudonyms it received.
func TestGeminiReturnWithoutStreaming(t *testing.T) {
	px, _ := buildGemini(t, nil, func(p geminiRequest, w http.ResponseWriter) {
		var req map[string]any
		json.Unmarshal(p.body, &req)
		cs := req["contents"].([]any)
		echo := cs[0].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"]
		args := cs[1].(map[string]any)["parts"].([]any)[1].(map[string]any)["functionCall"].(map[string]any)["args"]
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"index": 0, "finishReason": "STOP",
			"content": map[string]any{"role": "model", "parts": []any{
				map[string]any{"text": "pensando em " + fmt.Sprint(echo), "thought": true},
				part("text", echo),
				map[string]any{"functionCall": map[string]any{"name": "run_shell_command", "args": args}, "thoughtSignature": signature}}}}},
			"usageMetadata": map[string]any{"promptTokenCount": 10}})
	})
	st, b := postG(t, px, pathG, geminiRequestOf())
	var r respG
	if err := json.Unmarshal(b, &r); err != nil || st != 200 || len(r.Candidates) != 1 {
		t.Fatalf("status %d: %s", st, b)
	}
	ps := r.Candidates[0].Content.Parts
	if len(ps) != 3 {
		t.Fatalf("partes: %v", ps)
	}
	if s, _ := ps[0]["text"].(string); strings.Contains(s, email) || ps[0]["thought"] != true {
		t.Errorf("o raciocínio não devia ser desmascarado: %v", ps[0])
	}
	if ps[1]["text"] != userText {
		t.Errorf("texto não voltou real:\n%v\n%v", userText, ps[1]["text"])
	}
	fc := ps[2]["functionCall"].(map[string]any)
	if fc["args"].(map[string]any)["command"] != "ping -c1 "+ip || ps[2]["thoughtSignature"] != signature {
		t.Errorf("chamada de ferramenta não voltou real: %v", ps[2])
	}
	if !bytes.Contains(b, []byte(`"promptTokenCount":10`)) {
		t.Errorf("o resto da resposta mudou: %s", b)
	}
}

// In streaming (alt=sse) the text arrives chopped up, with pseudonyms cut between two pieces, and
// the client builds the real text.
func TestGeminiReturnStreaming(t *testing.T) {
	for _, n := range []int{1, 3, 7, 50} {
		px, _ := buildGemini(t, nil, func(p geminiRequest, w http.ResponseWriter) {
			var req map[string]any
			json.Unmarshal(p.body, &req)
			// chopped per letter (the API does not cut an accented letter in half)
			echo := []rune(req["contents"].([]any)[0].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"].(string))
			w.Header().Set("content-type", "text/event-stream")
			for i := 0; i < len(echo); i += n {
				j := min(i+n, len(echo))
				piece := map[string]any{"candidates": []any{map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": []any{part("text", string(echo[i:j]))}}}}}
				if j == len(echo) {
					piece["candidates"].([]any)[0].(map[string]any)["finishReason"] = "STOP"
					piece["usageMetadata"] = map[string]any{"totalTokenCount": 42}
				}
				b, _ := json.Marshal(piece)
				fmt.Fprintf(w, "data: %s\r\n\r\n", b)
			}
		})
		b, _ := json.Marshal(geminiRequestOf())
		resp, err := http.Post(px.URL+"/gemini/v1beta/models/gemini-x:streamGenerateContent?alt=sse", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		end, usage := "", false
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			data, ok := strings.CutPrefix(sc.Text(), "data: ")
			if !ok {
				continue
			}
			var r respG
			if err := json.Unmarshal([]byte(data), &r); err != nil {
				t.Fatalf("pedaço inválido: %s", data)
			}
			usage = usage || strings.Contains(data, `"totalTokenCount":42`)
			for _, c := range r.Candidates {
				for _, p := range c.Content.Parts {
					s, _ := p["text"].(string)
					text.WriteString(s)
				}
				if c.FinishReason != "" {
					end = c.FinishReason
				}
			}
		}
		resp.Body.Close()
		if text.String() != userText || end != "STOP" || !usage {
			t.Errorf("pedaços de %d: fim=%q uso=%v\n%q\n%q", n, end, usage, userText, text.String())
		}
	}
}

// The history goes out byte for byte identical in the next request, even after the conversation
// learns a new value (the API cache depends on it).
func TestGeminiFrozenHistory(t *testing.T) {
	px, requests := buildGemini(t, nil, func(p geminiRequest, w http.ResponseWriter) {
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`))
	})
	r1 := geminiRequestOf()
	postG(t, px, pathG, r1)
	r2 := geminiRequestOf()
	r2["contents"] = append(r2["contents"].([]any),
		contentG("model", part("text", "ok")),
		contentG("user", part("text", "e o servidor "+host+" com a senha "+weakTest+" em "+urlWith(weakTest)+"?")))
	postG(t, px, pathG, r2)
	contents := func(b []byte) []json.RawMessage {
		var v struct {
			Contents []json.RawMessage `json:"contents"`
		}
		json.Unmarshal(b, &v)
		return v.Contents
	}
	a, b := contents((*requests)[0].body), contents((*requests)[1].body)
	if len(a) != 3 || len(b) != 5 {
		t.Fatalf("contents: %d e %d", len(a), len(b))
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			t.Errorf("a mensagem %d mudou no reenvio:\n%s\n%s", i, a[i], b[i])
		}
	}
	if bytes.Contains(b[4], []byte(host)) || bytes.Contains(b[4], []byte(weakTest)) {
		t.Errorf("a mensagem nova vazou: %s", b[4])
	}
}

// An image that cannot be checked and a file of a type llm-dlp does not check do not go out: a text
// part with the notice goes in their place.
func TestGeminiFileWithoutCheck(t *testing.T) {
	px, requests := buildGemini(t, func(c *config.Config) {
		c.OCR.Tesseract = filepath.Join(t.TempDir(), "tesseract-que-nao-existe")
	}, func(p geminiRequest, w http.ResponseWriter) {
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"candidates":[]}`))
	})
	audio := "QVVESU8gREUgVEVTVEUgQVVESU8gREUgVEVTVEU="
	req := map[string]any{"contents": []any{contentG("user",
		part("text", "veja os anexos"),
		part("inlineData", map[string]any{"mimeType": "image/png", "data": minimalPNG}),
		part("inline_data", map[string]any{"mime_type": "audio/mp3", "data": audio}))}}
	if st, b := postG(t, px, pathG, req); st != 200 {
		t.Fatalf("status %d: %s", st, b)
	}
	body := (*requests)[0].body
	if bytes.Contains(body, []byte(minimalPNG)) || bytes.Contains(body, []byte(audio)) || bytes.Contains(body, []byte("inlineData")) {
		t.Fatalf("arquivo sem verificação saiu: %s", body)
	}
	var v struct {
		Contents []struct {
			Parts []map[string]any `json:"parts"`
		} `json:"contents"`
	}
	json.Unmarshal(body, &v)
	ps := v.Contents[0].Parts
	if len(ps) != 3 || ps[0]["text"] != "veja os anexos" {
		t.Fatalf("partes: %v", ps)
	}
	if s, _ := ps[1]["text"].(string); !strings.Contains(s, "An image was removed") {
		t.Errorf("aviso da imagem: %v", ps[1])
	}
	if s, _ := ps[2]["text"].(string); !strings.Contains(s, "can only check images and PDFs") {
		t.Errorf("aviso do áudio: %v", ps[2])
	}
}

// The Gemini error goes to the log with the status and the message; the API key, which may come in
// the address (?key=...), does not go to the log when the API does not answer.
func TestGeminiErrorInLogWithoutKey(t *testing.T) {
	if s := errorSummary([]byte(`{"error":{"code":429,"message":"Quota exceeded for metric","status":"RESOURCE_EXHAUSTED"}}`)); s != " | API: RESOURCE_EXHAUSTED: Quota exceeded for metric" {
		t.Errorf("erro do Gemini: %q", s)
	}
	cfg := config.Default()
	m, _ := mask.NewMasker(cfg, testKey, nil, nil)
	cfg.Providers = map[string]config.Provider{"gemini": {Upstream: "http://127.0.0.1:1", Format: "gemini"}}
	lg := &safeLog{}
	px := httptest.NewServer(New(cfg, m, nil, log.New(lg, "", 0)))
	defer px.Close()
	resp, err := http.Post(px.URL+"/gemini"+pathG+"?key=CHAVE-SECRETA-DA-API", "application/json", strings.NewReader(`{"contents":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || strings.Contains(lg.String(), "CHAVE-SECRETA") || bytes.Contains(b, []byte("CHAVE-SECRETA")) {
		t.Errorf("status %d; a chave apareceu no log ou na resposta:\n%s\n%s", resp.StatusCode, lg.String(), b)
	}
}

// "llm-dlp last" also shows the Gemini conversation: the last masked request is kept (only for
// whoever has the key), and token counting does not take its place.
func TestGeminiLast(t *testing.T) {
	cfg := config.Default()
	cfg.InternalDomains = []string{"empresa-ficticia"}
	m, err := mask.NewMasker(cfg, testKey, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]}}]}`))
	}))
	cfg.Providers = map[string]config.Provider{"gemini": {Upstream: up.URL, Format: "gemini"}}
	px := httptest.NewServer(New(cfg, m, nil, log.New(io.Discard, "", 0)).WithKey(testKey))
	t.Cleanup(func() { up.Close(); px.Close() })

	body := func(text string) []byte {
		b, _ := json.Marshal(map[string]any{"contents": []any{contentG("user", part("text", text))}})
		return b
	}
	conversation := "o contato é joao.silva@empresa-ficticia.com.br. " + strings.Repeat("texto da conversa ", 200)
	for _, p := range []struct{ path, text string }{
		{"/gemini/v1beta/models/m:generateContent", conversation},
		{"/gemini/v1beta/models/m:countTokens", strings.Repeat("contagem de tokens ", 200)},
	} {
		if st, _ := ask(t, "POST", px.URL+p.path, body(p.text), nil); st != 200 {
			t.Fatalf("%s: status %d", p.path, st)
		}
	}
	st, b := ask(t, "GET", px.URL+"/__llm-dlp/ultima", nil, func(r *http.Request) { r.Header.Set(ProofHeader, Proof(testKey, "ultima")) })
	if st != 200 || !bytes.Contains(b, []byte("texto da conversa")) || bytes.Contains(b, []byte("contagem de tokens")) {
		t.Errorf("status %d, corpo %.120s", st, b)
	}
	if bytes.Contains(b, []byte("joao.silva@")) || !bytes.Contains(b, []byte(".invalid")) {
		t.Errorf("a última conversa devia estar mascarada: %.160s", b)
	}
}
