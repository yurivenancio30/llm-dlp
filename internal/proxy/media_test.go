package proxy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Image and PDF that cannot be checked: they do not go out, and the conversation goes on.

// 1x1 PNG
const minimalPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

type safeLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *safeLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *safeLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// buildMedia: proxy with the OCR configuration adjusted by f. Returns the bodies that reach the API
// and the log.
func buildMedia(t *testing.T, f func(*config.Config)) (*httptest.Server, *[][]byte, *safeLog) {
	t.Helper()
	cfg := config.Default()
	f(&cfg)
	m, err := mask.NewMasker(cfg, []byte("0123456789abcdef0123456789abcdef"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	bodies := &[][]byte{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*bodies = append(*bodies, b)
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`))
	}))
	cfg.Upstream = up.URL
	lg := &safeLog{}
	px := httptest.NewServer(New(cfg, m, nil, log.New(lg, "", 0)))
	t.Cleanup(func() { up.Close(); px.Close() })
	return px, bodies, lg
}

// conversationWithFile: a file read whose result is the given block (image or PDF).
func conversationWithFile(block map[string]any) []byte {
	b, _ := json.Marshal(map[string]any{"model": "x", "max_tokens": 10, "messages": []any{
		msg("user", "veja o arquivo"),
		msg("assistant", []any{map[string]any{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{"file_path": "/tmp/tela.png"}}}),
		msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": []any{block}}}),
	}})
	return b
}

func imageBlock(data string) map[string]any {
	return map[string]any{"type": "image", "cache_control": map[string]any{"type": "ephemeral"},
		"source": map[string]any{"type": "base64", "media_type": "image/png", "data": data}}
}

// whatWentOut: the content of the tool result, as the API received it.
func whatWentOut(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var v struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatal(err)
	}
	var blocks []struct {
		Content []map[string]any `json:"content"`
	}
	if err := json.Unmarshal(v.Messages[2].Content, &blocks); err != nil {
		t.Fatal(err)
	}
	return blocks[0].Content
}

func post(t *testing.T, px *httptest.Server, body []byte) int {
	t.Helper()
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

// Without OCR installed, the image does not go out and the request is not refused: a notice with
// the reason and how to install goes in its place. When it was refused, the conversation got stuck
// (the image stays in the history).
func TestImageWithoutOCRDoesNotBlockConversation(t *testing.T) {
	px, bodies, lg := buildMedia(t, func(c *config.Config) {
		c.OCR.Tesseract = filepath.Join(t.TempDir(), "tesseract-que-nao-existe")
	})
	body := conversationWithFile(imageBlock(minimalPNG))
	for i := 0; i < 2; i++ {
		if st := post(t, px, body); st != 200 {
			t.Fatalf("requisição %d recusada: status %d", i, st)
		}
	}
	if len(*bodies) != 2 {
		t.Fatalf("a API devia receber as 2 requisições, recebeu %d", len(*bodies))
	}
	for _, c := range *bodies {
		if bytes.Contains(c, []byte(minimalPNG)) || bytes.Contains(c, []byte(`"image"`)) {
			t.Fatalf("a imagem sem verificação saiu: %s", c)
		}
	}
	blocks := whatWentOut(t, (*bodies)[0])
	if len(blocks) != 1 || blocks[0]["type"] != "text" || blocks[0]["cache_control"] == nil {
		t.Fatalf("devia sair um bloco de texto, com o marcador de cache da imagem: %v", blocks)
	}
	warning, _ := blocks[0]["text"].(string)
	for _, part := range []string{"[llm-dlp] An image was removed", "OCR is not installed", "tesseract"} {
		if !strings.Contains(warning, part) {
			t.Errorf("o aviso devia ter %q: %s", part, warning)
		}
	}
	if !bytes.Equal((*bodies)[0], (*bodies)[1]) {
		t.Error("o reenvio devia sair igual (cache da API)")
	}
	if n := strings.Count(lg.String(), "WARNING"); n != 1 {
		t.Errorf("o motivo devia ir uma vez para o log, foi %d:\n%s", n, lg.String())
	}
}

// Unreadable file: also removed, and the notice neither says to install what is already installed
// nor carries the error detail (which stays in the log).
func TestUnreadableImageIsRemoved(t *testing.T) {
	px, bodies, lg := buildMedia(t, func(c *config.Config) {})
	junk := base64.StdEncoding.EncodeToString([]byte("isto não é uma imagem"))
	if st := post(t, px, conversationWithFile(imageBlock(junk))); st != 200 {
		t.Fatalf("recusada: status %d", st)
	}
	warning, _ := whatWentOut(t, (*bodies)[0])[0]["text"].(string)
	if !strings.Contains(warning, "reading the file failed") || strings.Contains(warning, "is not installed") || strings.Contains(warning, "unreadable") {
		t.Errorf("aviso: %s", warning)
	}
	if bytes.Contains((*bodies)[0], []byte(junk)) {
		t.Error("o arquivo ilegível saiu")
	}
	if !strings.Contains(lg.String(), "unreadable image") {
		t.Errorf("o detalhe devia estar no log: %s", lg.String())
	}
}

// ocr.mode = block: image and PDF do not go out, and the conversation goes on.
func TestMediaBlockedByConfig(t *testing.T) {
	px, bodies, _ := buildMedia(t, func(c *config.Config) { c.OCR.Mode = "block" })
	pdf := map[string]any{"type": "document", "source": map[string]any{"type": "base64", "media_type": "application/pdf", "data": minimalPNG}}
	if st := post(t, px, conversationWithFile(pdf)); st != 200 {
		t.Fatalf("recusada: status %d", st)
	}
	warning, _ := whatWentOut(t, (*bodies)[0])[0]["text"].(string)
	if !strings.Contains(warning, "[llm-dlp] A PDF was removed") || !strings.Contains(warning, "ocr.mode = block") {
		t.Errorf("aviso: %s", warning)
	}
	if bytes.Contains((*bodies)[0], []byte(minimalPNG)) {
		t.Error("o PDF bloqueado saiu")
	}
}

// The API error goes to the log with the type and the message (that is what tells one 429 from
// another).
func TestAPIErrorInLog(t *testing.T) {
	lg := &safeLog{}
	cfg := config.Default()
	m, err := mask.NewMasker(cfg, []byte("0123456789abcdef0123456789abcdef"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(429)
		w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"This request would exceed\n your limit"}}`))
	}))
	cfg.Upstream = up.URL
	px := httptest.NewServer(New(cfg, m, nil, log.New(lg, "", 0)))
	defer func() { up.Close(); px.Close() }()
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", strings.NewReader(`{"model":"x","messages":[{"role":"user","content":"oi, tudo bem?"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 429 || !bytes.Contains(b, []byte("rate_limit_error")) {
		t.Fatalf("o erro devia chegar ao cliente como veio: %d %s", resp.StatusCode, b)
	}
	if !strings.Contains(lg.String(), "-> 429") || !strings.Contains(lg.String(), "| API: rate_limit_error: This request would exceed your limit") {
		t.Errorf("log: %s", lg.String())
	}
}
