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

// Memória da conversa e quem escreveu, de ponta a ponta (T3, T4, T5, T6). Nomes inventados,
// de palavra comum, montados por partes.

var (
	nsPalavra = "pag" + "amentos"
	rePseNS   = regexp.MustCompile(`\bns_[a-z2-7]{8}\b`)
	// o inventário: o leitor de linha de comando decide o namespace
	inventario = "kubectl logs -n " + nsPalavra + " deploy/web --tail 10"
)

// resposta da API falsa, montada a partir do corpo que ela recebeu
type respFalsa func(corpo []byte, w http.ResponseWriter)

// montarMem: proxy com vistos e enviados.log em dir (o mesmo dir = o mesmo disco depois de um
// "reinício"). Cada corpo que chega à API vai para *corpos.
func montarMem(t *testing.T, dir string, corpos *[][]byte, resp respFalsa) *httptest.Server {
	t.Helper()
	cfg := config.Padrao()
	chave := []byte("0123456789abcdef0123456789abcdef")
	vs, err := mask.CarregarVistos(dir + "/vistos.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := mask.NovoMasker(cfg, chave, nil, vs)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.UsarEnviados(dir+"/enviados.log", "teste"); err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*corpos = append(*corpos, b)
		resp(b, w)
	}))
	cfg.Upstream = up.URL
	px := httptest.NewServer(Novo(cfg, m, vs, log.New(io.Discard, "", 0)))
	t.Cleanup(func() { up.Close(); px.Close() })
	return px
}

// respTexto: um bloco de texto (com o pseudônimo do namespace no lugar de %s), em streaming
// picotado ou em JSON.
func respTexto(stream bool, molde string) respFalsa {
	return func(corpo []byte, w http.ResponseWriter) {
		texto := strings.ReplaceAll(molde, "%s", rePseNS.FindString(string(corpo)))
		if stream {
			w.Header().Set("content-type", "text/event-stream")
			sse(w, texto, `{"command":"true"}`, "pensando", 3)
			return
		}
		w.Header().Set("content-type", "application/json")
		b, _ := json.Marshal(map[string]any{"type": "message", "role": "assistant",
			"content": []any{map[string]any{"type": "text", "text": texto}}})
		w.Write(b)
	}
}

// respComando: uma chamada de ferramenta com o comando (o pseudônimo no lugar de %s).
func respComando(stream bool, molde string) respFalsa {
	return func(corpo []byte, w http.ResponseWriter) {
		cmd := strings.ReplaceAll(molde, "%s", rePseNS.FindString(string(corpo)))
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

// enviarLer: manda a conversa e devolve o que o Claude Code recebeu: o texto e a entrada da
// ferramenta (desmascarados).
func enviarLer(t *testing.T, px *httptest.Server, stream bool, msgs []any) (texto, entrada string) {
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
			texto += c.Text
			if c.Type == "tool_use" {
				entrada = string(c.Input)
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
				texto += s
			}
			if s, ok := d["partial_json"].(string); ok {
				entrada += s
			}
		}
	}
	return
}

func comandoDaEntrada(t *testing.T, entrada string) string {
	t.Helper()
	var in map[string]string
	if err := json.Unmarshal([]byte(entrada), &in); err != nil {
		t.Fatalf("entrada inválida: %q", entrada)
	}
	return in["command"]
}

// T3: o modelo usa o pseudônimo num comando, o proxy traduz para o nome; quando a chamada
// volta no histórico, ela sai com o pseudônimo, a palavra traduzida entra na memória e a saída
// seguinte com a palavra sai mascarada (numa conversa sem o inventário: só a tradução ensina).
func TestMemoriaTraduzidaNoComando(t *testing.T) {
	for _, stream := range []bool{true, false} {
		var corpos [][]byte
		px := montarMem(t, t.TempDir(), &corpos, respComando(stream, "echo %s pronto"))
		_, entrada := enviarLer(t, px, stream, []any{msg("user", inventario)})
		cmd := comandoDaEntrada(t, entrada)
		ps := rePseNS.FindString(string(corpos[0]))
		if ps == "" || cmd != "echo "+nsPalavra+" pronto" {
			t.Fatalf("stream=%v: premissa: pseudônimo %q, comando %q", stream, ps, cmd)
		}
		saida := nsPalavra + " pronto\n"
		conversa := []any{
			msg("user", "roda o eco"),
			msg("assistant", []any{map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{"command": cmd}}}),
			msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": saida}}),
		}
		enviarLer(t, px, stream, conversa)
		ms := mensagens(t, corpos[1])
		if !strings.Contains(ms[1], "echo "+ps+" pronto") {
			t.Errorf("stream=%v: a chamada não voltou como a API mandou: %s", stream, ms[1])
		}
		if strings.Contains(ms[2], nsPalavra) || !strings.Contains(ms[2], ps) {
			t.Errorf("stream=%v: a saída com a palavra traduzida saiu em claro: %s", stream, ms[2])
		}

		// sem registro (outro processo): comportamento atual, a palavra comum passa
		var outros [][]byte
		px2 := montarMem(t, t.TempDir(), &outros, respTexto(false, "ok"))
		enviarLer(t, px2, false, conversa)
		if ms := mensagens(t, outros[0]); !strings.Contains(ms[2], nsPalavra) {
			t.Errorf("stream=%v: sem registro, a palavra comum foi mascarada: %s", stream, ms[2])
		}
	}
}

// T4: resposta com um pseudônimo traduzido e a mesma palavra escrita pelo próprio modelo no
// mesmo bloco. Ao voltar no histórico, só a traduzida vira pseudônimo; na mensagem nova do
// usuário a palavra comum fica como palavra (o contágio não leva palavra comum para a prosa:
// mask/palavras_comuns.go). Vale também depois de um reinício (o registro está no
// enviados.log, só com HMAC e pseudônimos).
func TestMemoriaQuemEscreveuPontaAPonta(t *testing.T) {
	molde := "o namespace %s tem logs; " + nsPalavra + " tambem e uma palavra comum" // ASCII: o picote de 3 bytes não parte um caractere
	for _, stream := range []bool{true, false} {
		for _, reinicio := range []bool{false, true} {
			dir := t.TempDir()
			var corpos [][]byte
			px := montarMem(t, dir, &corpos, respTexto(stream, molde))
			texto, _ := enviarLer(t, px, stream, []any{msg("user", inventario)})
			ps := rePseNS.FindString(string(corpos[0]))
			original := strings.ReplaceAll(molde, "%s", ps)
			if ps == "" || texto != strings.ReplaceAll(molde, "%s", nsPalavra) {
				t.Fatalf("premissa: %q %q", ps, texto)
			}
			if reinicio {
				px.Close()
				px = montarMem(t, dir, &corpos, respTexto(stream, "ok"))
			}
			conversa := []any{msg("user", inventario), msg("assistant", []any{
				map[string]any{"type": "thinking", "thinking": "pensando", "signature": "S"},
				map[string]any{"type": "text", "text": texto}}),
				msg("user", "e o "+nsPalavra+" agora?")}
			enviarLer(t, px, stream, conversa)
			ms := mensagens(t, corpos[len(corpos)-1])
			var a struct {
				Content []struct{ Type, Text, Thinking string }
			}
			json.Unmarshal([]byte(ms[1]), &a)
			caso := map[bool]string{true: "stream", false: "json"}[stream] + map[bool]string{true: " depois do reinício", false: ""}[reinicio]
			if len(a.Content) != 2 || a.Content[1].Text != original || a.Content[0].Thinking != "pensando" {
				t.Errorf("%s: o texto do assistente não voltou como a API mandou:\n%s\nesperado %q", caso, ms[1], original)
			}
			if !strings.Contains(ms[2], nsPalavra) {
				t.Errorf("%s: palavra comum trocada na mensagem nova do usuário: %s", caso, ms[2])
			}
			if strings.Contains(ms[0], nsPalavra) {
				t.Errorf("%s: o inventário saiu em claro: %s", caso, ms[0])
			}
		}
	}
}

// T5: outra conversa, sem o inventário, não mascara a palavra comum (no mesmo processo).
func TestMemoriaOutraConversa(t *testing.T) {
	var corpos [][]byte
	px := montarMem(t, t.TempDir(), &corpos, respTexto(false, "ok"))
	enviarLer(t, px, false, []any{msg("user", inventario),
		msg("assistant", []any{map[string]any{"type": "tool_use", "id": "t5", "name": "Bash", "input": map[string]any{"command": "kubectl logs deploy/web --tail 5"}}}),
		msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "t5", "content": "o " + nsPalavra + " caiu"}})})
	if ms := mensagens(t, corpos[0]); strings.Contains(ms[2], nsPalavra) {
		t.Fatalf("premissa: com o inventário, a palavra devia sair mascarada na saída: %s", ms[2])
	}
	enviarLer(t, px, false, []any{msg("user", "o "+nsPalavra+" caiu")})
	if ms := mensagens(t, corpos[1]); !strings.Contains(ms[0], nsPalavra) {
		t.Fatalf("outra conversa: a palavra comum foi mascarada: %s", ms[0])
	}
}

// T6: o que já foi enviado continua idêntico, byte a byte, depois que a memória passa a
// conhecer o nome; os textos novos do mesmo pedido usam a memória.
func TestMemoriaNaoReescreveOPassadoPontaAPonta(t *testing.T) {
	var corpos [][]byte
	px := montarMem(t, t.TempDir(), &corpos, respTexto(false, "entendi"))
	velha := "o " + nsPalavra + " caiu ontem"
	conversa := []any{msg("user", velha)}
	texto, _ := enviarLer(t, px, false, conversa)
	conversa = append(conversa, msg("assistant", texto), msg("user", inventario+"\ne o "+nsPalavra+" hoje?"))
	enviarLer(t, px, false, conversa)
	conversa = append(conversa, msg("assistant", "certo"), msg("user", "de novo: "+nsPalavra))
	enviarLer(t, px, false, conversa)
	r := [][]string{mensagens(t, corpos[0]), mensagens(t, corpos[1]), mensagens(t, corpos[2])}
	if r[1][0] != r[0][0] || r[2][0] != r[0][0] || !strings.Contains(r[2][0], nsPalavra) {
		t.Fatalf("texto enviado mudou:\n%s\n%s\n%s", r[0][0], r[1][0], r[2][0])
	}
	for j := range r[1] {
		if r[2][j] != r[1][j] {
			t.Fatalf("reenvio mudou a mensagem %d:\n%s\n%s", j, r[1][j], r[2][j])
		}
	}
	// o inventário colado no texto novo continua nome (o leitor decide ali); a palavra comum na
	// prosa fica como palavra (mask/palavras_comuns.go)
	if strings.Contains(r[1][2], "-n "+nsPalavra) || !strings.Contains(r[2][4], nsPalavra) {
		t.Fatalf("texto novo: %s | %s", r[1][2], r[2][4])
	}
}
