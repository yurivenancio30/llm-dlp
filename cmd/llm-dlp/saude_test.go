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

// naPorta sobe h numa porta local e devolve a configuração que aponta para ela, com a pasta do
// llm-dlp num diretório temporário.
func naPorta(t *testing.T, h http.Handler) config.Config {
	t.Helper()
	t.Setenv("LLM_DLP_HOME", t.TempDir())
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	_, porta, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	cfg := config.Padrao()
	cfg.Porta, _ = strconv.Atoi(porta)
	return cfg
}

// Outro programa que pegue a porta e responda "sou o llm-dlp" não passa: falta a prova, que só
// quem lê a chave calcula. Sem isso ele receberia a conversa sem máscara e a credencial da API.
func TestSaudeExigeProva(t *testing.T) {
	impostor := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "servico": "llm-dlp", "versao": "9.9.9",
			"prova": strings.Repeat("0", 64)})
	})
	cfg := naPorta(t, impostor)
	if info, ok := saude(cfg); ok || !info.SemProva {
		t.Fatalf("o impostor passou: ok=%v semProva=%v", ok, info.SemProva)
	}

	// o llm-dlp de verdade, com a chave da mesma pasta, passa
	var h http.Handler
	cfg = naPorta(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	chave, err := mask.Chave(config.Caminho("chave"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := mask.NovoMasker(cfg, chave, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h = proxy.Novo(cfg, m, nil, log.New(io.Discard, "", 0)).ComChave(chave)
	if info, ok := saude(cfg); !ok || info.SemProva {
		t.Fatalf("o llm-dlp com a chave certa não passou: ok=%v semProva=%v", ok, info.SemProva)
	}

	// o mesmo llm-dlp com outra chave (o de outro usuário) não passa
	h = proxy.Novo(cfg, m, nil, log.New(io.Discard, "", 0)).ComChave([]byte("a chave de outro usuario........"))
	if info, ok := saude(cfg); ok || !info.SemProva {
		t.Fatalf("passou com a chave de outro: ok=%v", ok)
	}

	// versão anterior, que não sabe provar
	h = proxy.Novo(cfg, m, nil, log.New(io.Discard, "", 0))
	if info, ok := saude(cfg); ok || !info.SemProva {
		t.Fatalf("passou sem prova: ok=%v", ok)
	}
}

// Dizer "estou em emergência" só vale se o arquivo que o administrador grava disser o mesmo.
func TestSaudeEmergenciaSoComOArquivo(t *testing.T) {
	antes := dirEmergencia
	dirEmergencia = t.TempDir()
	defer func() { dirEmergencia = antes }()
	cfg := naPorta(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "servico": "llm-dlp", "modo": "emergencia"})
	}))
	if info, ok := saude(cfg); ok || !info.SemProva {
		t.Fatalf("emergência sem o arquivo do administrador passou: ok=%v", ok)
	}
	b, _ := json.Marshal(estadoEmergencia{Ate: time.Now().Add(time.Hour)})
	if err := os.WriteFile(filepath.Join(dirEmergencia, "emergencia.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := saude(cfg); !ok {
		t.Fatal("emergência ligada pelo administrador devia passar")
	}
}
