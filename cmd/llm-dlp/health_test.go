package main

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
	"github.com/yurivenancio30/llm-dlp/internal/proxy"
)

// onPort starts h on a local port and returns the configuration pointing to it, with the llm-dlp
// folder in a temporary directory.
func onPort(t *testing.T, h http.Handler) config.Config {
	t.Helper()
	t.Setenv("LLM_DLP_HOME", t.TempDir())
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	cfg := config.Default()
	cfg.Port, _ = strconv.Atoi(port)
	return cfg
}

// Another program that takes the port and answers "I am llm-dlp" does not pass: it lacks the proof,
// which only whoever reads the key computes. Without it, that program would receive the
// conversation without masking and the API credential.
func TestHealthRequiresProof(t *testing.T) {
	impostor := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "servico": "llm-dlp", "versao": "9.9.9",
			"prova": strings.Repeat("0", 64)})
	})
	cfg := onPort(t, impostor)
	if info, ok := health(cfg); ok || !info.WithoutProof {
		t.Fatalf("o impostor passou: ok=%v semProva=%v", ok, info.WithoutProof)
	}

	// the real llm-dlp, with the key of the same folder, passes
	var h http.Handler
	cfg = onPort(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	key, err := mask.Key(config.Path("chave"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := mask.NewMasker(cfg, key, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h = proxy.New(cfg, m, nil, log.New(io.Discard, "", 0)).WithKey(key)
	if info, ok := health(cfg); !ok || info.WithoutProof {
		t.Fatalf("o llm-dlp com a chave certa não passou: ok=%v semProva=%v", ok, info.WithoutProof)
	}

	// the same llm-dlp with another key (another user's) does not pass
	h = proxy.New(cfg, m, nil, log.New(io.Discard, "", 0)).WithKey([]byte("a chave de outro usuario........"))
	if info, ok := health(cfg); ok || !info.WithoutProof {
		t.Fatalf("passou com a chave de outro: ok=%v", ok)
	}

	// earlier version, which does not know how to prove
	h = proxy.New(cfg, m, nil, log.New(io.Discard, "", 0))
	if info, ok := health(cfg); ok || !info.WithoutProof {
		t.Fatalf("passou sem prova: ok=%v", ok)
	}
}

// Saying "I am in emergency" only counts if the file the administrator writes says the same.
func TestHealthEmergencyOnlyWithFile(t *testing.T) {
	before := dirEmergencia
	dirEmergencia = t.TempDir()
	defer func() { dirEmergencia = before }()
	cfg := onPort(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "servico": "llm-dlp", "modo": "emergencia"})
	}))
	if info, ok := health(cfg); ok || !info.WithoutProof {
		t.Fatalf("emergência sem o arquivo do administrador passou: ok=%v", ok)
	}
	b, _ := json.Marshal(emergencyState{Until: time.Now().Add(time.Hour)})
	if err := os.WriteFile(filepath.Join(dirEmergencia, "emergencia.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := health(cfg); !ok {
		t.Fatal("emergência ligada pelo administrador devia passar")
	}
}

// The default configuration produces no warning; each field that removes protection, or sends the
// messages elsewhere, produces its own.
func TestReducedProtection(t *testing.T) {
	t.Setenv("LLM_DLP_HOME", "")
	if a := reducedProtection(config.Default()); len(a) != 0 {
		t.Fatalf("a configuração padrão não devia avisar: %v", a)
	}
	cases := map[string]func(*config.Config){
		"atacante.example":     func(c *config.Config) { c.Upstream = "https://atacante.example/v1" },
		"http://api.anthropic": func(c *config.Config) { c.Upstream = "http://api.anthropic.com" },
		"provedor gemini": func(c *config.Config) {
			c.Providers = map[string]config.Provider{"gemini": {Upstream: "https://atacante.example", Format: "gemini"}}
		},
		"fail_closed":     func(c *config.Config) { c.FailClosed = false },
		"ocr.mode":        func(c *config.Config) { c.OCR.Mode = "allow" },
		"objects.enabled": func(c *config.Config) { c.Objects.Enabled = false },
		"secret, email":   func(c *config.Config) { c.DisabledDetectors = []string{"secret", "email"} },
	}
	for part, f := range cases {
		c := config.Default()
		f(&c)
		a := reducedProtection(c)
		if len(a) != 1 || !strings.Contains(a[0], part) {
			t.Errorf("esperava um aviso com %q, veio %v", part, a)
		}
	}
	t.Setenv("LLM_DLP_HOME", "/tmp/outra-pasta")
	if a := reducedProtection(config.Default()); len(a) != 1 || !strings.Contains(a[0], "LLM_DLP_HOME") {
		t.Errorf("LLM_DLP_HOME devia avisar: %v", a)
	}
}
