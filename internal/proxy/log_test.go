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

// A linha de cada requisição no log diz quantos valores de cada tipo foram mascarados e quais
// pseudônimos voltaram ao cliente sem virar o valor real. Nenhum valor real vai para o log.
func TestLogPorTipoENaoDesfeitos(t *testing.T) {
	cfg := config.Padrao()
	m, err := mask.NovoMasker(cfg, []byte("0123456789abcdef0123456789abcdef"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	reCPF := regexp.MustCompile(`CPF-[a-z2-7]{8}`)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		// o modelo repete o pseudônimo (volta ao valor real) e escreve um IP da faixa falsa que
		// não veio desta conversa (fica: não desfeito)
		w.Write([]byte(`{"type":"message","role":"assistant","content":[{"type":"text","text":"o ` +
			reCPF.FindString(string(b)) + ` usa 245.1.2.3"}]}`))
	}))
	cfg.Upstream = up.URL
	lg := &logSeguro{}
	px := httptest.NewServer(Novo(cfg, m, nil, log.New(lg, "", 0)))
	defer func() { up.Close(); px.Close() }()
	corpo := `{"model":"x","max_tokens":10,"messages":[{"role":"user","content":"cpf 529.982.247-25 no 10.42.7.15"}]}`
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(corpo)))
	if err != nil {
		t.Fatal(err)
	}
	volta, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(volta), "529.982.247-25") {
		t.Fatalf("resposta: %s", volta)
	}
	l := lg.String()
	if !strings.Contains(l, "| tipos: cpf 1, ip 1") || !strings.Contains(l, "| NÃO DESFEITOS 1: 245.1.2.3") ||
		strings.Contains(l, "529.982") || strings.Contains(l, "10.42.7") {
		t.Errorf("log: %s", l)
	}
}
