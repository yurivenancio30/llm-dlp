package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// The local port: who can talk to it, who reads the last conversation and the identity proof.

var testKey = []byte("0123456789abcdef0123456789abcdef")

// buildLocal: proxy with the key on. Also returns how many requests reached the API and the log.
func buildLocal(t *testing.T, withKey bool) (*httptest.Server, *int, *safeLog) {
	t.Helper()
	cfg := config.Default()
	m, err := mask.NewMasker(cfg, testKey, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		io.Copy(io.Discard, r.Body)
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`))
	}))
	cfg.Upstream = up.URL
	lg := &safeLog{}
	p := New(cfg, m, nil, log.New(lg, "", 0))
	if withKey {
		p.WithKey(testKey)
	}
	px := httptest.NewServer(p)
	t.Cleanup(func() { up.Close(); px.Close() })
	return px, &n, lg
}

func ask(t *testing.T, method, url string, body []byte, f func(*http.Request)) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		f(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// A page in the browser reaches the local port: through its own domain pointing to 127.0.0.1 (DNS
// rebinding; the Host is its domain) or by sending the request from another site (the Origin is its
// own). Neither is served, and nothing goes to the API.
func TestServesOnlyLocalProgram(t *testing.T) {
	cases := []struct {
		host, origin, site string
		local              bool
	}{
		{"127.0.0.1:8787", "", "", true},
		{"localhost:8787", "", "", true},
		{"[::1]:8787", "", "", true},
		{"127.0.0.1", "", "", true},
		{"127.0.0.1:8787", "http://localhost:3000", "", true},
		{"127.0.0.1:8787", "vscode-webview://abc", "", true},
		{"atacante.example:8787", "", "", false},
		{"127.0.0.1.atacante.example", "", "", false},
		{"127.0.0.1:8787", "https://atacante.example", "", false},
		{"127.0.0.1:8787", "http://atacante.example:8787", "", false},
		{"127.0.0.1:8787", "null", "", true},
		{"127.0.0.1:8787", "", "cross-site", true},
		{"", "", "", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "http://x/", nil)
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.site != "" {
			r.Header.Set("Sec-Fetch-Site", c.site)
		}
		if Local(r) != c.local {
			t.Errorf("Host %q Origin %q Sec-Fetch-Site %q: local=%v, expected %v", c.host, c.origin, c.site, !c.local, c.local)
		}
	}

	px, arrived, lg := buildLocal(t, true)
	body := []byte(`{"model":"x","messages":[{"role":"user","content":"o owner é ` + email + `"}]}`)
	for _, f := range []func(*http.Request){
		func(r *http.Request) { r.Host = "atacante.example:8787" },
		func(r *http.Request) { r.Header.Set("Origin", "https://atacante.example") },
	} {
		for _, path := range []string{"/v1/messages", "/__llm-dlp/saude", "/__llm-dlp/ultima"} {
			if st, _ := ask(t, "POST", px.URL+path, body, f); st != http.StatusForbidden {
				t.Errorf("%s from outside: status %d, expected 403", path, st)
			}
		}
	}
	if *arrived != 0 {
		t.Errorf("request from outside reached the API %d time(s)", *arrived)
	}
	if !strings.Contains(lg.String(), "REFUSED") {
		t.Errorf("the refusal should be in the log: %s", lg.String())
	}
	if st, _ := ask(t, "POST", px.URL+"/v1/messages", body, nil); st != 200 || *arrived != 1 {
		t.Errorf("the local program should be served: status %d, reached=%d", st, *arrived)
	}
}

// The last conversation (masked) only goes out to whoever reads the key: the "llm-dlp last"
// command. Another user of the machine, without the key, neither reads it nor uses the proxy to
// check guesses.
func TestLastOnlyWithProof(t *testing.T) {
	px, _, _ := buildLocal(t, true)
	body, _ := json.Marshal(map[string]any{"model": "x", "messages": []any{msg("user", strings.Repeat("texto da conversa ", 200))}})
	if st, _ := ask(t, "POST", px.URL+"/v1/messages", body, nil); st != 200 {
		t.Fatalf("status %d", st)
	}
	for name, proof := range map[string]string{"sem prova": "", "prova errada": Proof([]byte("outra chave, de outro usuario...."), "ultima"),
		"prova de outro uso": Proof(testKey, "saude\x00"+strings.Repeat("ab", 16))} {
		st, b := ask(t, "GET", px.URL+"/__llm-dlp/ultima", nil, func(r *http.Request) { r.Header.Set(ProofHeader, proof) })
		if st != http.StatusForbidden || bytes.Contains(b, []byte("texto da conversa")) {
			t.Errorf("%s: status %d, body %.80s", name, st, b)
		}
	}
	st, b := ask(t, "GET", px.URL+"/__llm-dlp/ultima", nil, func(r *http.Request) { r.Header.Set(ProofHeader, Proof(testKey, "ultima")) })
	if st != 200 || !bytes.Contains(b, []byte("texto da conversa")) {
		t.Errorf("with the proof: status %d, body %.80s", st, b)
	}

	// without the key on, nobody can read it
	noKey, _, _ := buildLocal(t, false)
	if st, _ := ask(t, "GET", noKey.URL+"/__llm-dlp/ultima", nil, func(r *http.Request) { r.Header.Set(ProofHeader, Proof(nil, "ultima")) }); st != http.StatusForbidden {
		t.Errorf("without a key: status %d, expected 403", st)
	}
}

// llm-dlp proves it is itself by answering a fresh challenge; it only signs hexadecimal challenges,
// and the proof of a challenge does not serve to read the last conversation.
func TestHealthProvesIdentity(t *testing.T) {
	px, _, _ := buildLocal(t, true)
	challenge := strings.Repeat("0f", 16)
	_, b := ask(t, "GET", px.URL+"/__llm-dlp/saude?desafio="+challenge, nil, nil)
	var s struct {
		OK    bool
		Proof string `json:"prova"`
	}
	json.Unmarshal(b, &s)
	if !s.OK || !ProofMatches(testKey, "saude\x00"+challenge, s.Proof) {
		t.Fatalf("wrong proof: %s", b)
	}
	if ProofMatches([]byte("outra chave, de outro usuario...."), "saude\x00"+challenge, s.Proof) || ProofMatches(nil, "saude\x00"+challenge, s.Proof) {
		t.Error("the proof matched with another key")
	}
	for _, d := range []string{"", "ultima", "curto", strings.Repeat("zz", 16), strings.Repeat("0f", 40)} {
		_, b := ask(t, "GET", px.URL+"/__llm-dlp/saude?desafio="+d, nil, nil)
		if bytes.Contains(b, []byte(`"prova"`)) {
			t.Errorf("signed an invalid challenge %q: %s", d, b)
		}
	}
}

// The request path goes to the log without control characters: a line break in the path does not
// forge another line.
func TestLogWithoutForgedLine(t *testing.T) {
	px, _, lg := buildLocal(t, true)
	ask(t, "GET", px.URL+"/v1/x%0aPOST%20/v1/messages%20-%3E%20200%20%7C%20linha%20forjada", nil, nil)
	lines := strings.Split(strings.TrimSpace(lg.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "/v1/x?POST") {
		t.Errorf("the log should have a single line, with the clean path: %q", lines)
	}
}
