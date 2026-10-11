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

// A porta local: quem pode falar com ela, quem lê a última conversa e a prova de identidade.

var chaveTeste = []byte("0123456789abcdef0123456789abcdef")

// montarLocal: proxy com a chave ligada. Devolve também quantas requisições chegaram à API e o log.
func montarLocal(t *testing.T, comChave bool) (*httptest.Server, *int, *logSeguro) {
	t.Helper()
	cfg := config.Padrao()
	m, err := mask.NovoMasker(cfg, chaveTeste, nil, nil)
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
	lg := &logSeguro{}
	p := Novo(cfg, m, nil, log.New(lg, "", 0))
	if comChave {
		p.ComChave(chaveTeste)
	}
	px := httptest.NewServer(p)
	t.Cleanup(func() { up.Close(); px.Close() })
	return px, &n, lg
}

func pedir(t *testing.T, metodo, url string, corpo []byte, f func(*http.Request)) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(metodo, url, bytes.NewReader(corpo))
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

// Uma página no navegador chega à porta local: pelo próprio domínio apontando para 127.0.0.1
// (DNS rebinding; o Host é o domínio dela) ou mandando a requisição de outro site (a Origin é a
// dela). Nenhuma das duas é atendida, e nada vai para a API.
func TestSoAtendeProgramaLocal(t *testing.T) {
	casos := []struct {
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
	for _, c := range casos {
		r := httptest.NewRequest("GET", "http://x/", nil)
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.site != "" {
			r.Header.Set("Sec-Fetch-Site", c.site)
		}
		if Local(r) != c.local {
			t.Errorf("Host %q Origin %q Sec-Fetch-Site %q: local=%v, esperado %v", c.host, c.origin, c.site, !c.local, c.local)
		}
	}

	px, chegou, lg := montarLocal(t, true)
	corpo := []byte(`{"model":"x","messages":[{"role":"user","content":"o owner é ` + email + `"}]}`)
	for _, f := range []func(*http.Request){
		func(r *http.Request) { r.Host = "atacante.example:8787" },
		func(r *http.Request) { r.Header.Set("Origin", "https://atacante.example") },
	} {
		for _, caminho := range []string{"/v1/messages", "/__llm-dlp/saude", "/__llm-dlp/ultima"} {
			if st, _ := pedir(t, "POST", px.URL+caminho, corpo, f); st != http.StatusForbidden {
				t.Errorf("%s de fora: status %d, esperado 403", caminho, st)
			}
		}
	}
	if *chegou != 0 {
		t.Errorf("requisição de fora chegou à API %d vez(es)", *chegou)
	}
	if !strings.Contains(lg.String(), "RECUSADO") {
		t.Errorf("a recusa devia estar no log: %s", lg.String())
	}
	if st, _ := pedir(t, "POST", px.URL+"/v1/messages", corpo, nil); st != 200 || *chegou != 1 {
		t.Errorf("o programa local devia ser atendido: status %d, chegou=%d", st, *chegou)
	}
}

// A última conversa (mascarada) só sai para quem lê a chave: o comando "llm-dlp ultima". Outro
// usuário da máquina, sem a chave, não lê nem usa o proxy para conferir palpites.
func TestUltimaSoComAProva(t *testing.T) {
	px, _, _ := montarLocal(t, true)
	corpo, _ := json.Marshal(map[string]any{"model": "x", "messages": []any{msg("user", strings.Repeat("texto da conversa ", 200))}})
	if st, _ := pedir(t, "POST", px.URL+"/v1/messages", corpo, nil); st != 200 {
		t.Fatalf("status %d", st)
	}
	for nome, prova := range map[string]string{"sem prova": "", "prova errada": Prova([]byte("outra chave, de outro usuario...."), "ultima"),
		"prova de outro uso": Prova(chaveTeste, "saude\x00"+strings.Repeat("ab", 16))} {
		st, b := pedir(t, "GET", px.URL+"/__llm-dlp/ultima", nil, func(r *http.Request) { r.Header.Set(CabecalhoProva, prova) })
		if st != http.StatusForbidden || bytes.Contains(b, []byte("texto da conversa")) {
			t.Errorf("%s: status %d, corpo %.80s", nome, st, b)
		}
	}
	st, b := pedir(t, "GET", px.URL+"/__llm-dlp/ultima", nil, func(r *http.Request) { r.Header.Set(CabecalhoProva, Prova(chaveTeste, "ultima")) })
	if st != 200 || !bytes.Contains(b, []byte("texto da conversa")) {
		t.Errorf("com a prova: status %d, corpo %.80s", st, b)
	}

	// sem chave ligada, não há quem possa ler
	semChave, _, _ := montarLocal(t, false)
	if st, _ := pedir(t, "GET", semChave.URL+"/__llm-dlp/ultima", nil, func(r *http.Request) { r.Header.Set(CabecalhoProva, Prova(nil, "ultima")) }); st != http.StatusForbidden {
		t.Errorf("sem chave: status %d, esperado 403", st)
	}
}

// O llm-dlp prova que é ele respondendo a um desafio novo; só assina desafio em hexadecimal, e a
// prova de um desafio não serve para ler a última conversa.
func TestSaudeProvaQuemE(t *testing.T) {
	px, _, _ := montarLocal(t, true)
	desafio := strings.Repeat("0f", 16)
	_, b := pedir(t, "GET", px.URL+"/__llm-dlp/saude?desafio="+desafio, nil, nil)
	var s struct {
		OK    bool
		Prova string
	}
	json.Unmarshal(b, &s)
	if !s.OK || !ProvaConfere(chaveTeste, "saude\x00"+desafio, s.Prova) {
		t.Fatalf("prova errada: %s", b)
	}
	if ProvaConfere([]byte("outra chave, de outro usuario...."), "saude\x00"+desafio, s.Prova) || ProvaConfere(nil, "saude\x00"+desafio, s.Prova) {
		t.Error("a prova conferiu com outra chave")
	}
	for _, d := range []string{"", "ultima", "curto", strings.Repeat("zz", 16), strings.Repeat("0f", 40)} {
		_, b := pedir(t, "GET", px.URL+"/__llm-dlp/saude?desafio="+d, nil, nil)
		if bytes.Contains(b, []byte(`"prova"`)) {
			t.Errorf("assinou um desafio inválido %q: %s", d, b)
		}
	}
}

// O caminho da requisição vai para o log sem caracteres de controle: uma quebra de linha no
// caminho não forja outra linha.
func TestLogSemLinhaForjada(t *testing.T) {
	px, _, lg := montarLocal(t, true)
	pedir(t, "GET", px.URL+"/v1/x%0aPOST%20/v1/messages%20-%3E%20200%20%7C%20linha%20forjada", nil, nil)
	linhas := strings.Split(strings.TrimSpace(lg.String()), "\n")
	if len(linhas) != 1 || !strings.Contains(linhas[0], "/v1/x?POST") {
		t.Errorf("o log devia ter uma linha só, com o caminho limpo: %q", linhas)
	}
}
