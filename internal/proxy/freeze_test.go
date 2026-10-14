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
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Proxy with vistos.json and enviados.log in a temporary directory (never in ~/.config). Each body
// that reaches the "API" goes to *bodies.
func buildDisk(t *testing.T, dir string, bodies *[][]byte) *httptest.Server {
	t.Helper()
	cfg := config.Default()
	key := []byte("0123456789abcdef0123456789abcdef")
	vs, err := mask.LoadSeen(dir + "/vistos.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := mask.NewMasker(cfg, key, nil, vs)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.UseSent(dir+"/enviados.log", "teste"); err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*bodies = append(*bodies, b)
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`))
	}))
	cfg.Upstream = up.URL
	px := httptest.NewServer(New(cfg, m, vs, log.New(io.Discard, "", 0)))
	t.Cleanup(func() { up.Close(); px.Close() })
	return px
}

func send(t *testing.T, px *httptest.Server, msgs []any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"model": "x", "max_tokens": 10, "messages": msgs})
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

// messages: each message exactly as it went out (bytes of the body sent to the API)
func messages(t *testing.T, body []byte) []string {
	t.Helper()
	var v struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(v.Messages))
	for i, m := range v.Messages {
		out[i] = string(m)
	}
	return out
}

func msg(hostRole string, content any) map[string]any {
	return map[string]any{"role": hostRole, "content": content}
}

// password built from parts (see the comment in mask/memory_sent_test.go)
var weakTest = "gira" + "ssol"

func urlWith(s string) string { return "mysql://app:" + s + "@db1:3306/base" }

// (c) and (d): two consecutive requests with the same history, with a value learned in the middle
// (in a short text, at the end of the conversation), go out with the history byte for byte
// identical; and the same after a proxy restart.
func TestHistorySameWhenLearning(t *testing.T) {
	dir := t.TempDir()
	var bodies [][]byte
	px := buildDisk(t, dir, &bodies)
	s := weakTest
	conversation := []any{msg("user", "tentei "+s+" no login e não entrou")}
	send(t, px, conversation)
	conversation = append(conversation, msg("assistant", "vou olhar a conexão"), msg("user", "a url é "+urlWith(s)))
	send(t, px, conversation) // learns s, in a short text at the end
	conversation = append(conversation, msg("assistant", "entendi"), msg("user", "testa "+s+" de novo"))
	send(t, px, conversation)

	r := [][]string{messages(t, bodies[0]), messages(t, bodies[1]), messages(t, bodies[2])}
	for i := 1; i < 3; i++ {
		for j := range r[i-1] {
			if r[i][j] != r[i-1][j] {
				t.Fatalf("request %d changed message %d of the history:\nbefore: %s\nafter:  %s", i+1, j, r[i-1][j], r[i][j])
			}
		}
	}
	if !strings.Contains(r[2][0], s) {
		t.Fatalf("premise: the first message went out without the value (it was already masked)")
	}
	if strings.Contains(r[1][2], s) || strings.Contains(r[2][4], s) {
		t.Errorf("new text did not use the learned value: %s | %s", r[1][2], r[2][4])
	}

	// the SAME text of the first message, now in a new message: goes out masked
	firstOne := "tentei " + s + " no login e não entrou"
	conversation = append(conversation, msg("assistant", "certo"), msg("user", firstOne))
	send(t, px, conversation)
	r3 := messages(t, bodies[3])
	if r3[0] != r[2][0] {
		t.Fatalf("the resend of the first message changed")
	}
	if strings.Contains(r3[6], s) {
		t.Errorf("the same text in a new message came out with the old mask: %s", r3[6])
	}
	// e numa conversa nova
	send(t, px, []any{msg("user", "outra conversa"), msg("assistant", "ok"), msg("user", firstOne)})
	if got := messages(t, bodies[4]); strings.Contains(got[2], s) {
		t.Errorf("the same text in a new conversation came out with the old mask: %s", got[2])
	}
	conversation = conversation[:5]
	r[2] = messages(t, bodies[2])

	// restart: empty memo, same directory
	var bodies2 [][]byte
	px2 := buildDisk(t, dir, &bodies2)
	send(t, px2, conversation)
	if got := messages(t, bodies2[0]); strings.Join(got, "\n") != strings.Join(r[2], "\n") {
		t.Fatalf("after the restart the history changed:\nbefore: %v\nafter:  %v", r[2], got)
	}
	// and new text after the restart stays masked (value remembered in vistos.json)
	conversation = append(conversation, msg("assistant", "ok"), msg("user", "e agora "+s+"?"))
	send(t, px2, conversation)
	if got := messages(t, bodies2[1]); strings.Contains(got[6], s) {
		t.Errorf("after the restart, new text came out with the value: %s", got[6])
	}
}

// (b) The example password in a URL inside a WebFetch result is masked there, but is not remembered
// (neither in RAM nor in vistos.json).
func TestWebFetchResultDoesNotTeach(t *testing.T) {
	dir := t.TempDir()
	var bodies [][]byte
	px := buildDisk(t, dir, &bodies)
	ex := "change" + "me"
	loose := "então " + ex + " é só um exemplo da doc?"
	conversation := []any{
		msg("user", "como conecto no mysql?"),
		msg("assistant", []any{map[string]any{"type": "tool_use", "id": "w1", "name": "WebFetch",
			"input": map[string]any{"url": "https://docs.example.com/mysql", "prompt": "como conectar"}}}),
		msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "w1",
			"content": "Exemplo: " + urlWith(ex)}}),
		msg("assistant", "a documentação mostra uma URL de exemplo"),
		msg("user", loose),
	}
	send(t, px, conversation)
	r := messages(t, bodies[0])
	if strings.Contains(r[2], ex) {
		t.Errorf("in the WebFetch result, the example password should come out masked: %s", r[2])
	}
	if !strings.Contains(r[4], ex) {
		t.Errorf("value coming from the web was remembered: %s", r[4])
	}
	var bodies2 [][]byte
	px2 := buildDisk(t, dir, &bodies2) // restart: only what is on disk
	send(t, px2, []any{msg("user", "outra conversa: "+ex+" aparece aqui")})
	if r := messages(t, bodies2[0]); !strings.Contains(r[0], ex) {
		t.Errorf("value coming from the web was written to seen.json: %s", r[0])
	}
}

// (e) A claude.ai connector tool (runs outside the machine) receives the pseudonym, like WebFetch;
// a local tool receives the real value.
func TestConnectorDoesNotUnmask(t *testing.T) {
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
		for i, name := range []string{"mcp__claude_ai_Gmail__search_threads", "Bash"} {
			arg, _ := json.Marshal(map[string]string{"query": "from:" + pseudoEmail})
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
	if pseudoEmail == "" || strings.Contains(byIdx[0], email) || !strings.Contains(byIdx[0], pseudoEmail) {
		t.Errorf("the connector received the real value (or nothing): %s", byIdx[0])
	}
	if !strings.Contains(byIdx[1], email) {
		t.Errorf("Bash should receive the real value: %s", byIdx[1])
	}
}

// The cache marker moves with every message: it cannot change the position of the texts that come
// after it.
func TestCacheControlKeepsPosition(t *testing.T) {
	var bodies [][]byte
	px := buildDisk(t, t.TempDir(), &bodies)
	s := weakTest
	txt := func(t string, cc bool) map[string]any {
		b := map[string]any{"type": "text", "text": t}
		if cc {
			b["cache_control"] = map[string]any{"type": "ephemeral"}
		}
		return b
	}
	send(t, px, []any{msg("user", []any{txt("contexto inicial", true), txt("tentei "+s+" e nada", false)})})
	send(t, px, []any{msg("user", []any{txt("contexto inicial", false), txt("tentei "+s+" e nada", false)}),
		msg("assistant", []any{txt("ok", false)}), msg("user", []any{txt("a url é "+urlWith(s), true)})})
	var a, b []map[string]any
	json.Unmarshal([]byte(messages(t, bodies[0])[0]), &struct{ Content *[]map[string]any }{&a})
	json.Unmarshal([]byte(messages(t, bodies[1])[0]), &struct{ Content *[]map[string]any }{&b})
	if len(a) != 2 || len(b) != 2 || a[1]["text"] != b[1]["text"] {
		t.Errorf("the cache_control in an earlier block changed the mask of the next text")
	}
}
