package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
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

// Memória da conversa e quem escreveu, de ponta a ponta. Names inventados,
// de palavra comum, montados por partes.

var (
	nsWord  = "pag" + "amentos"
	rePseNS = regexp.MustCompile(`\bns_[a-z2-7]{8}\b`)
	// o inventário: o leitor de linha de comando decide o namespace
	inventory = "kubectl logs -n " + nsWord + " deploy/web --tail 10"
)

// resposta da API falsa, montada a partir do corpo que ela recebeu
type fakeResp func(body []byte, w http.ResponseWriter)

// buildMem: proxy com vistos e enviados.log em dir (o mesmo dir = o mesmo disco depois de um
// "reinício"). Cada corpo que chega à API vai para *corpos.
func buildMem(t *testing.T, dir string, bodies *[][]byte, resp fakeResp) *httptest.Server {
	t.Helper()
	cfg := config.Default()
	key := []byte("0123456789abcdef0123456789abcdef")
	vs, err := mask.LoadSeen(dir + "/vistos.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := mask.NewMasker(cfg, key, nil, vs)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.UseSent(dir+"/enviados.log", "teste"); err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*bodies = append(*bodies, b)
		resp(b, w)
	}))
	cfg.Upstream = up.URL
	px := httptest.NewServer(New(cfg, m, vs, log.New(io.Discard, "", 0)))
	t.Cleanup(func() { up.Close(); px.Close() })
	return px
}

// respText: um bloco de texto (com o pseudônimo do namespace no lugar de %s), em streaming
// picotado ou em JSON.
func respText(stream bool, template string) fakeResp {
	return func(body []byte, w http.ResponseWriter) {
		text := strings.ReplaceAll(template, "%s", rePseNS.FindString(string(body)))
		if stream {
			w.Header().Set("content-type", "text/event-stream")
			sse(w, text, `{"command":"true"}`, "pensando", 3)
			return
		}
		w.Header().Set("content-type", "application/json")
		b, _ := json.Marshal(map[string]any{"type": "message", "role": "assistant",
			"content": []any{map[string]any{"type": "text", "text": text}}})
		w.Write(b)
	}
}

// commandResp: uma chamada de ferramenta com o comando (o pseudônimo no lugar de %s).
func commandResp(stream bool, template string) fakeResp {
	return func(body []byte, w http.ResponseWriter) {
		cmd := strings.ReplaceAll(template, "%s", rePseNS.FindString(string(body)))
		arg, _ := json.Marshal(map[string]string{"command": cmd})
		if stream {
			w.Header().Set("content-type", "text/event-stream")
			sse(w, "vou rodar", string(arg), "pensando", 4)
			return
		}
		w.Header().Set("content-type", "application/json")
		b, _ := json.Marshal(map[string]any{"type": "message", "role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "t9", "name": "Bash", "input": json.RawMessage(arg)}}})
		w.Write(b)
	}
}

// sendRead: manda a conversa e devolve o que o Claude Code recebeu: o texto e a entrada da
// ferramenta (desmascarados).
func sendRead(t *testing.T, px *httptest.Server, stream bool, msgs []any) (text, entry string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"model": "x", "max_tokens": 10, "stream": stream, "messages": msgs})
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if !stream {
		var r struct {
			Content []struct {
				Type, Text string
				Input      json.RawMessage
			}
		}
		json.NewDecoder(resp.Body).Decode(&r)
		for _, c := range r.Content {
			text += c.Text
			if c.Type == "tool_use" {
				entry = string(c.Input)
			}
		}
		return
	}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		l := sc.Text()
		if !strings.HasPrefix(l, "data: ") {
			continue
		}
		var e map[string]any
		json.Unmarshal([]byte(l[6:]), &e)
		if d, ok := e["delta"].(map[string]any); ok {
			if s, ok := d["text"].(string); ok {
				text += s
			}
			if s, ok := d["partial_json"].(string); ok {
				entry += s
			}
		}
	}
	return
}

func inputCommand(t *testing.T, entry string) string {
	t.Helper()
	var in map[string]string
	if err := json.Unmarshal([]byte(entry), &in); err != nil {
		t.Fatalf("entrada inválida: %q", entry)
	}
	return in["command"]
}

// O modelo usa o pseudônimo num comando, o proxy traduz para o nome; quando a chamada
// volta no histórico, ela sai com o pseudônimo, a palavra traduzida entra na memória e a saída
// seguinte com a palavra sai mascarada (numa conversa sem o inventário: só a tradução ensina).
func TestMemoryTranslatedInCommand(t *testing.T) {
	for _, stream := range []bool{true, false} {
		var bodies [][]byte
		px := buildMem(t, t.TempDir(), &bodies, commandResp(stream, "echo %s pronto"))
		_, entry := sendRead(t, px, stream, []any{msg("user", inventory)})
		cmd := inputCommand(t, entry)
		ps := rePseNS.FindString(string(bodies[0]))
		if ps == "" || cmd != "echo "+nsWord+" pronto" {
			t.Fatalf("stream=%v: premissa: pseudônimo %q, comando %q", stream, ps, cmd)
		}
		output := nsWord + " pronto\n"
		conversation := []any{
			msg("user", "roda o eco"),
			msg("assistant", []any{map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{"command": cmd}}}),
			msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": output}}),
		}
		sendRead(t, px, stream, conversation)
		ms := messages(t, bodies[1])
		if !strings.Contains(ms[1], "echo "+ps+" pronto") {
			t.Errorf("stream=%v: a chamada não voltou como a API mandou: %s", stream, ms[1])
		}
		if strings.Contains(ms[2], nsWord) || !strings.Contains(ms[2], ps) {
			t.Errorf("stream=%v: a saída com a palavra traduzida saiu em claro: %s", stream, ms[2])
		}

		// sem registro (outro processo): comportamento atual, a palavra comum passa
		var othersSet [][]byte
		px2 := buildMem(t, t.TempDir(), &othersSet, respText(false, "ok"))
		sendRead(t, px2, false, conversation)
		if ms := messages(t, othersSet[0]); !strings.Contains(ms[2], nsWord) {
			t.Errorf("stream=%v: sem registro, a palavra comum foi mascarada: %s", stream, ms[2])
		}
	}
}

// Resposta com um pseudônimo traduzido e a mesma palavra escrita pelo próprio modelo no
// mesmo bloco. Ao voltar no histórico, só a traduzida vira pseudônimo; na mensagem nova do
// usuário a palavra comum fica como palavra (o contágio não leva palavra comum para a prosa:
// mask/vocab_words.go). Vale também depois de um reinício (o registro está no
// enviados.log, só com HMAC e pseudônimos).
func TestMemoryWhoWroteEndToEnd(t *testing.T) {
	template := "o namespace %s tem logs; " + nsWord + " tambem e uma palavra comum" // ASCII: o picote de 3 bytes não parte um caractere
	for _, stream := range []bool{true, false} {
		for _, restartN := range []bool{false, true} {
			dir := t.TempDir()
			var bodies [][]byte
			px := buildMem(t, dir, &bodies, respText(stream, template))
			text, _ := sendRead(t, px, stream, []any{msg("user", inventory)})
			ps := rePseNS.FindString(string(bodies[0]))
			original := strings.ReplaceAll(template, "%s", ps)
			if ps == "" || text != strings.ReplaceAll(template, "%s", nsWord) {
				t.Fatalf("premissa: %q %q", ps, text)
			}
			if restartN {
				px.Close()
				px = buildMem(t, dir, &bodies, respText(stream, "ok"))
			}
			conversation := []any{msg("user", inventory), msg("assistant", []any{
				map[string]any{"type": "thinking", "thinking": "pensando", "signature": "S"},
				map[string]any{"type": "text", "text": text}}),
				msg("user", "e o "+nsWord+" agora?")}
			sendRead(t, px, stream, conversation)
			ms := messages(t, bodies[len(bodies)-1])
			var a struct {
				Content []struct{ Type, Text, Thinking string }
			}
			json.Unmarshal([]byte(ms[1]), &a)
			tc := map[bool]string{true: "stream", false: "json"}[stream] + map[bool]string{true: " depois do reinício", false: ""}[restartN]
			if len(a.Content) != 2 || a.Content[1].Text != original || a.Content[0].Thinking != "pensando" {
				t.Errorf("%s: o texto do assistente não voltou como a API mandou:\n%s\nesperado %q", tc, ms[1], original)
			}
			if !strings.Contains(ms[2], nsWord) {
				t.Errorf("%s: palavra comum trocada na mensagem nova do usuário: %s", tc, ms[2])
			}
			if strings.Contains(ms[0], nsWord) {
				t.Errorf("%s: o inventário saiu em claro: %s", tc, ms[0])
			}
		}
	}
}

// Outra conversa, sem o inventário, não mascara a palavra comum (no mesmo processo).
func TestMemoryOtherConversation(t *testing.T) {
	var bodies [][]byte
	px := buildMem(t, t.TempDir(), &bodies, respText(false, "ok"))
	sendRead(t, px, false, []any{msg("user", inventory),
		msg("assistant", []any{map[string]any{"type": "tool_use", "id": "t5", "name": "Bash", "input": map[string]any{"command": "kubectl logs deploy/web --tail 5"}}}),
		msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "t5", "content": "o " + nsWord + " caiu"}})})
	if ms := messages(t, bodies[0]); strings.Contains(ms[2], nsWord) {
		t.Fatalf("premissa: com o inventário, a palavra devia sair mascarada na saída: %s", ms[2])
	}
	sendRead(t, px, false, []any{msg("user", "o "+nsWord+" caiu")})
	if ms := messages(t, bodies[1]); !strings.Contains(ms[0], nsWord) {
		t.Fatalf("outra conversa: a palavra comum foi mascarada: %s", ms[0])
	}
}

// O que já foi enviado continua idêntico, byte a byte, depois que a memória passa a
// conhecer o nome; os textos novos do mesmo pedido usam a memória.
func TestMemoryDoesNotRewritePastEndToEnd(t *testing.T) {
	var bodies [][]byte
	px := buildMem(t, t.TempDir(), &bodies, respText(false, "entendi"))
	oldOne := "o " + nsWord + " caiu ontem"
	conversation := []any{msg("user", oldOne)}
	text, _ := sendRead(t, px, false, conversation)
	conversation = append(conversation, msg("assistant", text), msg("user", inventory+"\ne o "+nsWord+" hoje?"))
	sendRead(t, px, false, conversation)
	conversation = append(conversation, msg("assistant", "certo"), msg("user", "de novo: "+nsWord))
	sendRead(t, px, false, conversation)
	r := [][]string{messages(t, bodies[0]), messages(t, bodies[1]), messages(t, bodies[2])}
	if r[1][0] != r[0][0] || r[2][0] != r[0][0] || !strings.Contains(r[2][0], nsWord) {
		t.Fatalf("texto enviado mudou:\n%s\n%s\n%s", r[0][0], r[1][0], r[2][0])
	}
	for j := range r[1] {
		if r[2][j] != r[1][j] {
			t.Fatalf("reenvio mudou a mensagem %d:\n%s\n%s", j, r[1][j], r[2][j])
		}
	}
	// o inventário colado no texto novo continua nome (o leitor decide ali); a palavra comum na
	// prosa fica como palavra (mask/vocab_words.go)
	if strings.Contains(r[1][2], "-n "+nsWord) || !strings.Contains(r[2][4], nsWord) {
		t.Fatalf("texto novo: %s | %s", r[1][2], r[2][4])
	}
}
