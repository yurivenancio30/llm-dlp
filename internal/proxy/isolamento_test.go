package proxy

import (
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

// O suporte a outras APIs (Gemini, OpenAI) não muda nada no caminho do Claude Code: a mesma
// requisição sai byte a byte igual com ou sem os provedores, o que é só desses formatos não vale
// para a Anthropic, e nenhuma requisição ou resposta malformada desses formatos derruba o proxy.

// montarClaude: proxy com a API da Anthropic falsa (resp escreve a resposta) e os provedores dados.
func montarClaude(t *testing.T, provs map[string]config.Provedor, resp func(corpo []byte, w http.ResponseWriter)) (*httptest.Server, *[][]byte) {
	t.Helper()
	cfg := config.Padrao()
	cfg.DominiosInternos = []string{"empresa-ficticia"}
	cfg.Provedores = provs
	m, err := mask.NovoMasker(cfg, chaveTeste, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	corpos := &[][]byte{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*corpos = append(*corpos, b)
		resp(b, w)
	}))
	cfg.Upstream = up.URL
	px := httptest.NewServer(Novo(cfg, m, nil, log.New(io.Discard, "", 0)))
	t.Cleanup(func() { up.Close(); px.Close() })
	return px, corpos
}

func respostaClaude(w http.ResponseWriter, conteudo ...any) {
	w.Header().Set("content-type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"type": "message", "role": "assistant", "content": conteudo})
}

// conversaClaude: uma conversa do Claude Code com dado sensível nos lugares em que o código dos
// outros formatos poderia encostar: um campo "format" na entrada de uma ferramenta, uma
// ferramenta chamada web_fetch (nome de ferramenta de internet no Gemini CLI) e um schema.
func conversaClaude() []byte {
	b, _ := json.Marshal(map[string]any{"model": "claude", "max_tokens": 100,
		"system": []any{map[string]any{"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude."}},
		"tools": []any{map[string]any{"name": "web_fetch", "description": "busca interna",
			"input_schema": map[string]any{"type": "object", "properties": map[string]any{"url": map[string]any{"type": "string", "format": "uri"}}}}},
		"messages": []any{
			msg("user", "exporte os contatos de "+emailOA),
			msg("assistant", []any{map[string]any{"type": "tool_use", "id": "t1", "name": "exportar",
				"input": map[string]any{"format": emailOA, "mimeType": ipOA}}}),
			msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "ok, enviado a " + emailOA}}),
		}})
	return b
}

func TestClaudeIgualComOuSemOutrasAPIs(t *testing.T) {
	semNada := func(_ []byte, w http.ResponseWriter) { respostaClaude(w, map[string]any{"type": "text", "text": "ok"}) }
	sem, corposSem := montarClaude(t, nil, semNada)
	com, corposCom := montarClaude(t, config.ProvedoresPadrao(), semNada)
	for _, px := range []*httptest.Server{sem, com} {
		if st, _ := pedir(t, "POST", px.URL+"/v1/messages", conversaClaude(), nil); st != 200 {
			t.Fatalf("status %d", st)
		}
	}
	a, b := (*corposSem)[0], (*corposCom)[0]
	if !bytes.Equal(a, b) {
		t.Errorf("a requisição do Claude mudou com os provedores ligados:\n sem: %s\n com: %s", a, b)
	}
	// campos e ferramentas que só os outros formatos deixam intactos continuam mascarados no Claude
	if bytes.Contains(a, []byte(emailOA)) || bytes.Contains(a, []byte(ipOA)) {
		t.Errorf("valor real chegou à API da Anthropic: %s", a)
	}
}

// Na volta do Claude, web_fetch é uma ferramenta como outra qualquer (a entrada volta real); só
// WebFetch, a do Claude Code, segue com os pseudônimos.
func TestClaudeVoltaSemAsFerramentasDosOutros(t *testing.T) {
	px, _ := montarClaude(t, config.ProvedoresPadrao(), func(corpo []byte, w http.ResponseWriter) {
		pse := rePseEmail.FindString(string(corpo))
		respostaClaude(w,
			map[string]any{"type": "tool_use", "id": "a", "name": "web_fetch", "input": map[string]any{"q": pse}},
			map[string]any{"type": "tool_use", "id": "b", "name": "WebFetch", "input": map[string]any{"q": pse}})
	})
	_, b := pedir(t, "POST", px.URL+"/v1/messages", conversaClaude(), nil)
	var resp struct {
		Content []struct {
			Name  string
			Input map[string]string
		}
	}
	if err := json.Unmarshal(b, &resp); err != nil || len(resp.Content) != 2 {
		t.Fatalf("resposta: %s", b)
	}
	if resp.Content[0].Input["q"] != emailOA {
		t.Errorf("web_fetch no Claude devia voltar com o valor real: %q", resp.Content[0].Input["q"])
	}
	if !rePseEmail.MatchString(resp.Content[1].Input["q"]) {
		t.Errorf("WebFetch devia seguir com o pseudônimo: %q", resp.Content[1].Input["q"])
	}
}

// variacoes: cópias de v com um nó trocado por um valor estranho, um nó e um valor de cada vez.
func variacoes(t *testing.T, v any) [][]byte {
	t.Helper()
	orig, _ := json.Marshal(v)
	var caminhos [][]any
	var andar func(x any, cam []any)
	andar = func(x any, cam []any) {
		caminhos = append(caminhos, append([]any(nil), cam...))
		switch y := x.(type) {
		case map[string]any:
			for _, k := range chaves(y) {
				andar(y[k], append(cam, k))
			}
		case []any:
			for i := range y {
				andar(y[i], append(cam, i))
			}
		}
	}
	var raiz any
	json.Unmarshal(orig, &raiz)
	andar(raiz, nil)
	var out [][]byte
	for _, cam := range caminhos[1:] {
		for _, estranho := range []any{nil, 5, "x", []any{}, map[string]any{}, []any{"x", 5, nil}} {
			var c any
			json.Unmarshal(orig, &c)
			x := c
			for _, p := range cam[:len(cam)-1] {
				if k, ok := p.(string); ok {
					x = x.(map[string]any)[k]
				} else {
					x = x.([]any)[p.(int)]
				}
			}
			if k, ok := cam[len(cam)-1].(string); ok {
				x.(map[string]any)[k] = estranho
			} else {
				x.([]any)[cam[len(cam)-1].(int)] = estranho
			}
			b, _ := json.Marshal(c)
			out = append(out, b)
		}
	}
	return out
}

// Requisições e respostas do Gemini e da OpenAI com qualquer nó trocado por um valor estranho:
// o proxy nunca cai (responde sempre), e o Claude continua funcionando depois.
func TestOutrasAPIsMalformadasNaoDerrubamOClaude(t *testing.T) {
	cfg := config.Padrao()
	m, err := mask.NovoMasker(cfg, chaveTeste, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var respostaAtual []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/v1/messages" {
			respostaClaude(w, map[string]any{"type": "text", "text": "ok"})
			return
		}
		w.Header().Set("content-type", "application/json")
		w.Write(respostaAtual)
	}))
	cfg.Upstream = up.URL
	cfg.Provedores = map[string]config.Provedor{"gemini": {Upstream: up.URL, Formato: "gemini"}, "xai": {Upstream: up.URL, Formato: "openai"}}
	p := Novo(cfg, m, nil, log.New(io.Discard, "", 0))
	px := httptest.NewServer(p)
	t.Cleanup(func() { up.Close(); px.Close() })

	gem := map[string]any{"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": "sistema " + emailOA}}},
		"tools": []any{map[string]any{"functionDeclarations": []any{map[string]any{"name": "f", "parameters": map[string]any{"type": "OBJECT"}}}}},
		"contents": []any{conteudoG("user", parte("text", "oi "+emailOA)),
			conteudoG("model", parte("functionCall", map[string]any{"name": "f", "args": map[string]any{"a": ipOA}})),
			conteudoG("user", parte("functionResponse", map[string]any{"name": "f", "response": map[string]any{"r": ipOA}}),
				parte("inlineData", map[string]any{"mimeType": "audio/wav", "data": "AAAA"}))}}
	oa := conversaOA()
	oa["messages"] = append(oa["messages"].([]any), map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,AAAA"}}}})
	pse := p.m.Pseudonimo("email", emailOA)
	respGem := map[string]any{"candidates": []any{map[string]any{"index": 0, "finishReason": "STOP", "content": map[string]any{"role": "model",
		"parts": []any{map[string]any{"text": "oi " + pse}, map[string]any{"functionCall": map[string]any{"name": "f", "args": map[string]any{"a": pse}}}}}}}}
	respOA := map[string]any{"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "oi " + pse,
		"tool_calls": []any{map[string]any{"id": "c", "function": map[string]any{"name": "f", "arguments": `{"a":"` + pse + `"}`}}}}}}}

	casos := []struct {
		caminho     string
		req, resp   any
		semStreamOK bool
	}{
		{"/gemini/v1beta/models/m:generateContent", gem, respGem, true},
		{"/xai/v1/chat/completions", oa, respOA, true},
	}
	n := 0
	for _, c := range casos {
		boa, _ := json.Marshal(c.resp)
		reqs := variacoes(t, c.req)
		resps := variacoes(t, c.resp)
		for i := 0; i < max(len(reqs), len(resps)); i++ {
			req, _ := json.Marshal(c.req)
			respostaAtual = boa
			if i < len(reqs) {
				req = reqs[i]
			}
			if i < len(resps) {
				respostaAtual = resps[i]
			}
			resp, err := http.Post(px.URL+c.caminho, "application/json", bytes.NewReader(req))
			if err != nil {
				t.Fatalf("%s: o proxy caiu com\n req: %s\n resp: %s\n erro: %v", c.caminho, req, respostaAtual, err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			n++
		}
	}
	if st, _ := pedir(t, "POST", px.URL+"/v1/messages", conversaClaude(), nil); st != 200 {
		t.Errorf("o Claude parou de funcionar depois das requisições malformadas: status %d", st)
	}
	t.Logf("%d requisições e respostas malformadas, nenhuma derrubou o proxy", n)
}

// O streaming malformado (pedaços que não são JSON, campos com o tipo errado, corte no meio)
// também nunca derruba o proxy.
func TestOutrasAPIsStreamingMalformado(t *testing.T) {
	cfg := config.Padrao()
	m, err := mask.NovoMasker(cfg, chaveTeste, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := Novo(cfg, m, nil, log.New(io.Discard, "", 0))
	pse := p.m.Pseudonimo("email", emailOA)
	tab := mask.NovaTabela([]mask.Entrada{{Pseudo: pse, Real: emailOA, Tipo: "email"}})
	pedacos := []string{
		`{"candidates":[{"content":{"parts":[{"text":"oi ` + pse[:5] + `"}]}}]}`,
		`{"candidates":[{"index":"x","content":{"parts":[5,null,{"text":5},{"functionCall":"x"}]}}]}`,
		`{"candidates":5}`, `{"choices":[{"delta":{"content":5,"tool_calls":[5,{"function":"x"},{"index":"a","function":{"arguments":7}}]}}]}`,
		`{"choices":[{"delta":null,"finish_reason":"stop"}]}`, `não é json`, `[1,2]`, `null`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"f","arguments":"{\"a\":\"` + pse[:7] + `"}}]}}]}`,
	}
	for _, fim := range []string{"", "data: [DONE]\n\n"} {
		var corpo strings.Builder
		for _, pc := range pedacos {
			fmt.Fprintf(&corpo, "data: %s\n\n", pc)
		}
		corpo.WriteString(fim)
		for nome, f := range map[string]func(http.ResponseWriter, io.Reader, *mask.Tabela) error{
			"gemini": p.desmascararSSEGemini, "openai": p.desmascararSSEOpenAI} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s: o streaming malformado derrubou o proxy: %v", nome, r)
					}
				}()
				f(httptest.NewRecorder(), strings.NewReader(corpo.String()), tab)
			}()
		}
	}
}
