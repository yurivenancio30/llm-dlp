package proxy

import (
	"bytes"
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

// The line of each request in the log says how many values of each type were masked and which
// pseudonyms came back to the client without becoming the real value. No real value goes to the
// log.
func TestLogByTypeAndNotUndone(t *testing.T) {
	cfg := config.Default()
	m, err := mask.NewMasker(cfg, []byte("0123456789abcdef0123456789abcdef"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	reCPF := regexp.MustCompile(`CPF-[a-z2-7]{8}`)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		// the model repeats the pseudonym (goes back to the real value) and writes an IP of the
		// fake range that did not come from this conversation (stays: not undone)
		w.Write([]byte(`{"type":"message","role":"assistant","content":[{"type":"text","text":"o ` +
			reCPF.FindString(string(b)) + ` usa 245.1.2.3"}]}`))
	}))
	cfg.Upstream = up.URL
	lg := &safeLog{}
	px := httptest.NewServer(New(cfg, m, nil, log.New(lg, "", 0)))
	defer func() { up.Close(); px.Close() }()
	body := `{"model":"x","max_tokens":10,"messages":[{"role":"user","content":"cpf 529.982.247-25 no 10.42.7.15"}]}`
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	back, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(back), "529.982.247-25") {
		t.Fatalf("response: %s", back)
	}
	l := lg.String()
	if !strings.Contains(l, "| types: cpf 1, ip 1") || !strings.Contains(l, "| NOT UNDONE 1: 245.1.2.3") ||
		strings.Contains(l, "529.982") || strings.Contains(l, "10.42.7") {
		t.Errorf("log: %s", l)
	}
}
