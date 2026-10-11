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
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// A API do Gemini pela rota /gemini: nada real chega à API, a resposta volta real (inteira e em
// streaming) e o histórico sai idêntico de uma requisição para a outra.

type pedidoGemini struct {
	caminho, chave string
	corpo          []byte
}

// montarGemini: proxy com o provedor "gemini" apontando para uma API falsa, que responde com resp.
func montarGemini(t *testing.T, f func(*config.Config), resp func(p pedidoGemini, w http.ResponseWriter)) (*httptest.Server, *[]pedidoGemini) {
	t.Helper()
	cfg := config.Padrao()
	cfg.DominiosInternos = []string{"empresa-ficticia"}
	if f != nil {
		f(&cfg)
	}
	m, err := mask.NovoMasker(cfg, chaveTeste, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pedidos := &[]pedidoGemini{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		p := pedidoGemini{r.URL.RequestURI(), r.Header.Get("x-goog-api-key"), b}
		*pedidos = append(*pedidos, p)
		resp(p, w)
	}))
	anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a requisição do Gemini foi para o upstream da Anthropic: %s", r.URL.Path)
	}))
	cfg.Upstream = anthropic.URL
	cfg.Provedores = map[string]config.Provedor{"gemini": {Upstream: up.URL, Formato: "gemini"}}
	px := httptest.NewServer(Novo(cfg, m, nil, log.New(io.Discard, "", 0)))
	t.Cleanup(func() { up.Close(); anthropic.Close(); px.Close() })
	return px, pedidos
}

func parte(k string, v any) map[string]any { return map[string]any{k: v} }

func conteudoG(papel string, partes ...any) map[string]any {
	return map[string]any{"role": papel, "parts": partes}
}

const (
	assinatura   = "Q2lRQmxXVnpkR1VnWkdVZ1lYTnphVzVoZEhWeVlRPT0="
	textoUsuario = "o owner " + email + " (cpf " + cpf + ") não acessa " + host
	caminhoG     = "/v1beta/models/gemini-x:generateContent"
)

func requisicaoGemini() map[string]any {
	return map[string]any{
		"systemInstruction": map[string]any{"parts": []any{parte("text", "Você é um agente. Usuário logado: "+email)}},
		"tools": []any{map[string]any{"functionDeclarations": []any{map[string]any{
			"name": "run_shell_command", "description": "Roda um comando. Dúvidas: " + email,
			"parameters": map[string]any{"type": "OBJECT", "required": []any{"command"},
				"properties": map[string]any{"command": map[string]any{"type": "STRING", "description": "o comando"}}}}}}},
		"generationConfig": map[string]any{"temperature": json.Number("0.2"), "responseMimeType": "text/plain"},
		"contents": []any{
			conteudoG("user", parte("text", textoUsuario)),
			conteudoG("model",
				map[string]any{"text": "vou conferir o p.aaaaaaaa", "thought": true},
				map[string]any{"functionCall": map[string]any{"name": "run_shell_command", "args": map[string]any{"command": "ping -c1 " + ip}},
					"thoughtSignature": assinatura}),
			conteudoG("user", parte("functionResponse", map[string]any{"name": "run_shell_command",
				"response": map[string]any{"output": "PING " + ip + " from " + host, "exitCode": json.Number("0")}})),
		},
	}
}

func postarG(t *testing.T, px *httptest.Server, caminho string, corpo any) (int, []byte) {
	t.Helper()
	b, _ := json.Marshal(corpo)
	req, _ := http.NewRequest("POST", px.URL+"/gemini"+caminho, bytes.NewReader(b))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-goog-api-key", "chave-de-teste")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

type respG struct {
	Candidates []struct {
		Content struct {
			Parts []map[string]any `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
}

// A requisição chega à API do Gemini sem nada real, no caminho certo, com a mesma estrutura; o
// que não é dado (assinatura do raciocínio, configuração, nome da ferramenta, schema) não muda.
func TestGeminiIdaMascarada(t *testing.T) {
	px, pedidos := montarGemini(t, nil, func(p pedidoGemini, w http.ResponseWriter) {
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`))
	})
	if st, b := postarG(t, px, caminhoG+"?alt=json", requisicaoGemini()); st != 200 {
		t.Fatalf("status %d: %s", st, b)
	}
	p := (*pedidos)[0]
	if p.caminho != caminhoG+"?alt=json" || p.chave != "chave-de-teste" {
		t.Errorf("caminho ou chave não chegaram como vieram: %q %q", p.caminho, p.chave)
	}
	for _, r := range []string{email, ip, host, cpf} {
		if bytes.Contains(p.corpo, []byte(r)) {
			t.Errorf("vazou %q: %s", r, p.corpo)
		}
	}
	var saiu, orig map[string]any
	json.Unmarshal(p.corpo, &saiu)
	b, _ := json.Marshal(requisicaoGemini())
	json.Unmarshal(b, &orig)
	if !reflect.DeepEqual(saiu["generationConfig"], orig["generationConfig"]) {
		t.Errorf("generationConfig mudou: %v", saiu["generationConfig"])
	}
	fd := saiu["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)[0].(map[string]any)
	fdOrig := orig["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)[0].(map[string]any)
	if fd["name"] != "run_shell_command" || !reflect.DeepEqual(fd["parameters"], fdOrig["parameters"]) {
		t.Errorf("nome ou schema da ferramenta mudou: %v", fd)
	}
	cs := saiu["contents"].([]any)
	if len(cs) != 3 {
		t.Fatalf("contents: %d", len(cs))
	}
	modelo := cs[1].(map[string]any)
	ps := modelo["parts"].([]any)
	if modelo["role"] != "model" || len(ps) != 2 {
		t.Fatalf("mensagem do modelo mudou de forma: %v", modelo)
	}
	if pensou := ps[0].(map[string]any); pensou["text"] != "vou conferir o p.aaaaaaaa" || pensou["thought"] != true {
		t.Errorf("o raciocínio mudou: %v", pensou)
	}
	chamada := ps[1].(map[string]any)
	if chamada["thoughtSignature"] != assinatura {
		t.Errorf("a assinatura do raciocínio mudou: %v", chamada["thoughtSignature"])
	}
	if cmd := chamada["functionCall"].(map[string]any)["args"].(map[string]any)["command"].(string); !strings.HasPrefix(cmd, "ping -c1 ") {
		t.Errorf("comando: %q", cmd)
	}
	fr := cs[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	saida := fr["response"].(map[string]any)
	if fr["name"] != "run_shell_command" || !strings.HasPrefix(saida["output"].(string), "PING ") || fmt.Sprint(saida["exitCode"]) != "0" {
		t.Errorf("resposta da ferramenta mudou de forma: %v", fr)
	}
}

// A resposta volta real: o texto e os argumentos da chamada de ferramenta, que o modelo escreveu
// com os pseudônimos que recebeu.
func TestGeminiVoltaSemStreaming(t *testing.T) {
	px, _ := montarGemini(t, nil, func(p pedidoGemini, w http.ResponseWriter) {
		var req map[string]any
		json.Unmarshal(p.corpo, &req)
		cs := req["contents"].([]any)
		eco := cs[0].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"]
		args := cs[1].(map[string]any)["parts"].([]any)[1].(map[string]any)["functionCall"].(map[string]any)["args"]
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"index": 0, "finishReason": "STOP",
			"content": map[string]any{"role": "model", "parts": []any{
				map[string]any{"text": "pensando em " + fmt.Sprint(eco), "thought": true},
				parte("text", eco),
				map[string]any{"functionCall": map[string]any{"name": "run_shell_command", "args": args}, "thoughtSignature": assinatura}}}}},
			"usageMetadata": map[string]any{"promptTokenCount": 10}})
	})
	st, b := postarG(t, px, caminhoG, requisicaoGemini())
	var r respG
	if err := json.Unmarshal(b, &r); err != nil || st != 200 || len(r.Candidates) != 1 {
		t.Fatalf("status %d: %s", st, b)
	}
	ps := r.Candidates[0].Content.Parts
	if len(ps) != 3 {
		t.Fatalf("partes: %v", ps)
	}
	if s, _ := ps[0]["text"].(string); strings.Contains(s, email) || ps[0]["thought"] != true {
		t.Errorf("o raciocínio não devia ser desmascarado: %v", ps[0])
	}
	if ps[1]["text"] != textoUsuario {
		t.Errorf("texto não voltou real:\n%v\n%v", textoUsuario, ps[1]["text"])
	}
	fc := ps[2]["functionCall"].(map[string]any)
	if fc["args"].(map[string]any)["command"] != "ping -c1 "+ip || ps[2]["thoughtSignature"] != assinatura {
		t.Errorf("chamada de ferramenta não voltou real: %v", ps[2])
	}
	if !bytes.Contains(b, []byte(`"promptTokenCount":10`)) {
		t.Errorf("o resto da resposta mudou: %s", b)
	}
}

// Em streaming (alt=sse) o texto chega picotado, com pseudônimos cortados entre dois pedaços, e
// o cliente monta o texto real.
func TestGeminiVoltaStreaming(t *testing.T) {
	for _, n := range []int{1, 3, 7, 50} {
		px, _ := montarGemini(t, nil, func(p pedidoGemini, w http.ResponseWriter) {
			var req map[string]any
			json.Unmarshal(p.corpo, &req)
			// picotado por letra (a API não corta uma letra acentuada ao meio)
			eco := []rune(req["contents"].([]any)[0].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"].(string))
			w.Header().Set("content-type", "text/event-stream")
			for i := 0; i < len(eco); i += n {
				j := min(i+n, len(eco))
				pedaco := map[string]any{"candidates": []any{map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": []any{parte("text", string(eco[i:j]))}}}}}
				if j == len(eco) {
					pedaco["candidates"].([]any)[0].(map[string]any)["finishReason"] = "STOP"
					pedaco["usageMetadata"] = map[string]any{"totalTokenCount": 42}
				}
				b, _ := json.Marshal(pedaco)
				fmt.Fprintf(w, "data: %s\r\n\r\n", b)
			}
		})
		b, _ := json.Marshal(requisicaoGemini())
		resp, err := http.Post(px.URL+"/gemini/v1beta/models/gemini-x:streamGenerateContent?alt=sse", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		var texto strings.Builder
		fim, uso := "", false
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			dados, ok := strings.CutPrefix(sc.Text(), "data: ")
			if !ok {
				continue
			}
			var r respG
			if err := json.Unmarshal([]byte(dados), &r); err != nil {
				t.Fatalf("pedaço inválido: %s", dados)
			}
			uso = uso || strings.Contains(dados, `"totalTokenCount":42`)
			for _, c := range r.Candidates {
				for _, p := range c.Content.Parts {
					s, _ := p["text"].(string)
					texto.WriteString(s)
				}
				if c.FinishReason != "" {
					fim = c.FinishReason
				}
			}
		}
		resp.Body.Close()
		if texto.String() != textoUsuario || fim != "STOP" || !uso {
			t.Errorf("pedaços de %d: fim=%q uso=%v\n%q\n%q", n, fim, uso, textoUsuario, texto.String())
		}
	}
}

// O histórico sai byte a byte idêntico na requisição seguinte, mesmo depois de a conversa
// aprender um valor novo (o cache da API depende disso).
func TestGeminiHistoricoCongelado(t *testing.T) {
	px, pedidos := montarGemini(t, nil, func(p pedidoGemini, w http.ResponseWriter) {
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`))
	})
	r1 := requisicaoGemini()
	postarG(t, px, caminhoG, r1)
	r2 := requisicaoGemini()
	r2["contents"] = append(r2["contents"].([]any),
		conteudoG("model", parte("text", "ok")),
		conteudoG("user", parte("text", "e o servidor "+host+" com a senha "+fracaTeste+" em "+urlCom(fracaTeste)+"?")))
	postarG(t, px, caminhoG, r2)
	contents := func(b []byte) []json.RawMessage {
		var v struct {
			Contents []json.RawMessage `json:"contents"`
		}
		json.Unmarshal(b, &v)
		return v.Contents
	}
	a, b := contents((*pedidos)[0].corpo), contents((*pedidos)[1].corpo)
	if len(a) != 3 || len(b) != 5 {
		t.Fatalf("contents: %d e %d", len(a), len(b))
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			t.Errorf("a mensagem %d mudou no reenvio:\n%s\n%s", i, a[i], b[i])
		}
	}
	if bytes.Contains(b[4], []byte(host)) || bytes.Contains(b[4], []byte(fracaTeste)) {
		t.Errorf("a mensagem nova vazou: %s", b[4])
	}
}

// Imagem que não dá para verificar e arquivo de tipo que o llm-dlp não verifica não saem: no
// lugar vai uma parte de texto com o aviso.
func TestGeminiArquivoSemVerificacao(t *testing.T) {
	px, pedidos := montarGemini(t, func(c *config.Config) {
		c.OCR.Tesseract = filepath.Join(t.TempDir(), "tesseract-que-nao-existe")
	}, func(p pedidoGemini, w http.ResponseWriter) {
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"candidates":[]}`))
	})
	audio := "QVVESU8gREUgVEVTVEUgQVVESU8gREUgVEVTVEU="
	req := map[string]any{"contents": []any{conteudoG("user",
		parte("text", "veja os anexos"),
		parte("inlineData", map[string]any{"mimeType": "image/png", "data": pngMinimo}),
		parte("inline_data", map[string]any{"mime_type": "audio/mp3", "data": audio}))}}
	if st, b := postarG(t, px, caminhoG, req); st != 200 {
		t.Fatalf("status %d: %s", st, b)
	}
	corpo := (*pedidos)[0].corpo
	if bytes.Contains(corpo, []byte(pngMinimo)) || bytes.Contains(corpo, []byte(audio)) || bytes.Contains(corpo, []byte("inlineData")) {
		t.Fatalf("arquivo sem verificação saiu: %s", corpo)
	}
	var v struct {
		Contents []struct {
			Parts []map[string]any `json:"parts"`
		} `json:"contents"`
	}
	json.Unmarshal(corpo, &v)
	ps := v.Contents[0].Parts
	if len(ps) != 3 || ps[0]["text"] != "veja os anexos" {
		t.Fatalf("partes: %v", ps)
	}
	if s, _ := ps[1]["text"].(string); !strings.Contains(s, "Uma imagem foi retirada") {
		t.Errorf("aviso da imagem: %v", ps[1])
	}
	if s, _ := ps[2]["text"].(string); !strings.Contains(s, "só consegue verificar imagem e PDF") {
		t.Errorf("aviso do áudio: %v", ps[2])
	}
}

// O erro do Gemini vai para o log com o status e a mensagem; a chave da API, que pode vir no
// endereço (?key=...), não vai para o log quando a API não responde.
func TestGeminiErroNoLogSemAChave(t *testing.T) {
	if s := resumoDoErro([]byte(`{"error":{"code":429,"message":"Quota exceeded for metric","status":"RESOURCE_EXHAUSTED"}}`)); s != " | API: RESOURCE_EXHAUSTED: Quota exceeded for metric" {
		t.Errorf("erro do Gemini: %q", s)
	}
	cfg := config.Padrao()
	m, _ := mask.NovoMasker(cfg, chaveTeste, nil, nil)
	cfg.Provedores = map[string]config.Provedor{"gemini": {Upstream: "http://127.0.0.1:1", Formato: "gemini"}}
	lg := &logSeguro{}
	px := httptest.NewServer(Novo(cfg, m, nil, log.New(lg, "", 0)))
	defer px.Close()
	resp, err := http.Post(px.URL+"/gemini"+caminhoG+"?key=CHAVE-SECRETA-DA-API", "application/json", strings.NewReader(`{"contents":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || strings.Contains(lg.String(), "CHAVE-SECRETA") || bytes.Contains(b, []byte("CHAVE-SECRETA")) {
		t.Errorf("status %d; a chave apareceu no log ou na resposta:\n%s\n%s", resp.StatusCode, lg.String(), b)
	}
}
