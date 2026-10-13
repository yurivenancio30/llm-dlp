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

// Imagem e PDF que não dá para verificar: não saem, e a conversa continua.

// PNG de 1x1 ponto
const pngMinimo = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

type logSeguro struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logSeguro) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logSeguro) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// montarMidia: proxy com a configuração de OCR ajustada por f. Devolve os corpos que chegam à
// API e o log.
func montarMidia(t *testing.T, f func(*config.Config)) (*httptest.Server, *[][]byte, *logSeguro) {
	t.Helper()
	cfg := config.Padrao()
	f(&cfg)
	m, err := mask.NovoMasker(cfg, []byte("0123456789abcdef0123456789abcdef"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	corpos := &[][]byte{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*corpos = append(*corpos, b)
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`))
	}))
	cfg.Upstream = up.URL
	lg := &logSeguro{}
	px := httptest.NewServer(Novo(cfg, m, nil, log.New(lg, "", 0)))
	t.Cleanup(func() { up.Close(); px.Close() })
	return px, corpos, lg
}

// conversaComArquivo: uma leitura de arquivo cujo resultado é o bloco dado (imagem ou PDF).
func conversaComArquivo(bloco map[string]any) []byte {
	b, _ := json.Marshal(map[string]any{"model": "x", "max_tokens": 10, "messages": []any{
		msg("user", "veja o arquivo"),
		msg("assistant", []any{map[string]any{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{"file_path": "/tmp/tela.png"}}}),
		msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": []any{bloco}}}),
	}})
	return b
}

func blocoImagem(dados string) map[string]any {
	return map[string]any{"type": "image", "cache_control": map[string]any{"type": "ephemeral"},
		"source": map[string]any{"type": "base64", "media_type": "image/png", "data": dados}}
}

// oQueSaiu: o conteúdo do resultado da ferramenta, como a API o recebeu.
func oQueSaiu(t *testing.T, corpo []byte) []map[string]any {
	t.Helper()
	var v struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(corpo, &v); err != nil {
		t.Fatal(err)
	}
	var blocos []struct {
		Content []map[string]any `json:"content"`
	}
	if err := json.Unmarshal(v.Messages[2].Content, &blocos); err != nil {
		t.Fatal(err)
	}
	return blocos[0].Content
}

func postar(t *testing.T, px *httptest.Server, corpo []byte) int {
	t.Helper()
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(corpo))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

// Sem o OCR instalado, a imagem não sai e a requisição não é recusada: no lugar vai um aviso com
// o motivo e como instalar. Recusada, a conversa ficava presa (a imagem fica no histórico).
func TestImagemSemOCRNaoPrendeAConversa(t *testing.T) {
	px, corpos, lg := montarMidia(t, func(c *config.Config) {
		c.OCR.Tesseract = filepath.Join(t.TempDir(), "tesseract-que-nao-existe")
	})
	corpo := conversaComArquivo(blocoImagem(pngMinimo))
	for i := 0; i < 2; i++ {
		if st := postar(t, px, corpo); st != 200 {
			t.Fatalf("requisição %d recusada: status %d", i, st)
		}
	}
	if len(*corpos) != 2 {
		t.Fatalf("a API devia receber as 2 requisições, recebeu %d", len(*corpos))
	}
	for _, c := range *corpos {
		if bytes.Contains(c, []byte(pngMinimo)) || bytes.Contains(c, []byte(`"image"`)) {
			t.Fatalf("a imagem sem verificação saiu: %s", c)
		}
	}
	blocos := oQueSaiu(t, (*corpos)[0])
	if len(blocos) != 1 || blocos[0]["type"] != "text" || blocos[0]["cache_control"] == nil {
		t.Fatalf("devia sair um bloco de texto, com o marcador de cache da imagem: %v", blocos)
	}
	aviso, _ := blocos[0]["text"].(string)
	for _, parte := range []string{"[llm-dlp] Uma imagem foi retirada", "OCR não está instalado", "tesseract"} {
		if !strings.Contains(aviso, parte) {
			t.Errorf("o aviso devia ter %q: %s", parte, aviso)
		}
	}
	if !bytes.Equal((*corpos)[0], (*corpos)[1]) {
		t.Error("o reenvio devia sair igual (cache da API)")
	}
	if n := strings.Count(lg.String(), "AVISO"); n != 1 {
		t.Errorf("o motivo devia ir uma vez para o log, foi %d:\n%s", n, lg.String())
	}
}

// Arquivo ilegível: também é retirado, e o aviso não manda instalar o que já está instalado nem
// leva o detalhe do erro (que fica no log).
func TestImagemIlegivelERetirada(t *testing.T) {
	px, corpos, lg := montarMidia(t, func(c *config.Config) {})
	lixo := base64.StdEncoding.EncodeToString([]byte("isto não é uma imagem"))
	if st := postar(t, px, conversaComArquivo(blocoImagem(lixo))); st != 200 {
		t.Fatalf("recusada: status %d", st)
	}
	aviso, _ := oQueSaiu(t, (*corpos)[0])[0]["text"].(string)
	if !strings.Contains(aviso, "a leitura do arquivo falhou") || strings.Contains(aviso, "não está instalado") || strings.Contains(aviso, "ilegível") {
		t.Errorf("aviso: %s", aviso)
	}
	if bytes.Contains((*corpos)[0], []byte(lixo)) {
		t.Error("o arquivo ilegível saiu")
	}
	if !strings.Contains(lg.String(), "imagem ilegível") {
		t.Errorf("o detalhe devia estar no log: %s", lg.String())
	}
}

// ocr.mode = block: imagem e PDF não saem, e a conversa continua.
func TestMidiaBloqueadaPelaConfiguracao(t *testing.T) {
	px, corpos, _ := montarMidia(t, func(c *config.Config) { c.OCR.Modo = "block" })
	pdf := map[string]any{"type": "document", "source": map[string]any{"type": "base64", "media_type": "application/pdf", "data": pngMinimo}}
	if st := postar(t, px, conversaComArquivo(pdf)); st != 200 {
		t.Fatalf("recusada: status %d", st)
	}
	aviso, _ := oQueSaiu(t, (*corpos)[0])[0]["text"].(string)
	if !strings.Contains(aviso, "[llm-dlp] Um PDF foi retirado") || !strings.Contains(aviso, "ocr.mode = block") {
		t.Errorf("aviso: %s", aviso)
	}
	if bytes.Contains((*corpos)[0], []byte(pngMinimo)) {
		t.Error("o PDF bloqueado saiu")
	}
}

// O erro da API vai para o log com o tipo e a mensagem (é o que distingue um 429 de outro).
func TestErroDaAPINoLog(t *testing.T) {
	lg := &logSeguro{}
	cfg := config.Padrao()
	m, err := mask.NovoMasker(cfg, []byte("0123456789abcdef0123456789abcdef"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(429)
		w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"This request would exceed\n your limit"}}`))
	}))
	cfg.Upstream = up.URL
	px := httptest.NewServer(Novo(cfg, m, nil, log.New(lg, "", 0)))
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
