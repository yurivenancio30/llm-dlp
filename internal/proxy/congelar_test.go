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
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Proxy com vistos.json e enviados.log num diretório temporário (nunca em ~/.config).
// Cada corpo que chega à "API" vai para *corpos.
func montarDisco(t *testing.T, dir string, corpos *[][]byte) *httptest.Server {
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
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`))
	}))
	cfg.Upstream = up.URL
	px := httptest.NewServer(Novo(cfg, m, vs, log.New(io.Discard, "", 0)))
	t.Cleanup(func() { up.Close(); px.Close() })
	return px
}

func enviar(t *testing.T, px *httptest.Server, msgs []any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"model": "x", "max_tokens": 10, "messages": msgs})
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

// mensagens: cada mensagem exatamente como saiu (bytes do corpo enviado à API)
func mensagens(t *testing.T, corpo []byte) []string {
	t.Helper()
	var v struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(corpo, &v); err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(v.Messages))
	for i, m := range v.Messages {
		out[i] = string(m)
	}
	return out
}

func msg(papel string, conteudo any) map[string]any {
	return map[string]any{"role": papel, "content": conteudo}
}

// senha montada por partes (ver o comentário em mask/congelar_test.go)
var fracaTeste = "gira" + "ssol"

func urlCom(s string) string { return "mysql://app:" + s + "@db1:3306/base" }

// (c) e (d): duas requisições seguidas com o mesmo histórico, com um valor aprendido no
// meio (num texto curto, no fim da conversa), saem com o histórico byte a byte idêntico;
// e o mesmo depois de um reinício do proxy.
func TestHistoricoIgualQuandoAprende(t *testing.T) {
	dir := t.TempDir()
	var corpos [][]byte
	px := montarDisco(t, dir, &corpos)
	s := fracaTeste
	conversa := []any{msg("user", "tentei "+s+" no login e não entrou")}
	enviar(t, px, conversa)
	conversa = append(conversa, msg("assistant", "vou olhar a conexão"), msg("user", "a url é "+urlCom(s)))
	enviar(t, px, conversa) // aprende s, num texto curto no fim
	conversa = append(conversa, msg("assistant", "entendi"), msg("user", "testa "+s+" de novo"))
	enviar(t, px, conversa)

	r := [][]string{mensagens(t, corpos[0]), mensagens(t, corpos[1]), mensagens(t, corpos[2])}
	for i := 1; i < 3; i++ {
		for j := range r[i-1] {
			if r[i][j] != r[i-1][j] {
				t.Fatalf("requisição %d mudou a mensagem %d do histórico:\nantes:  %s\ndepois: %s", i+1, j, r[i-1][j], r[i][j])
			}
		}
	}
	if !strings.Contains(r[2][0], s) {
		t.Fatalf("premissa: a primeira mensagem saiu sem o valor (já era mascarado)")
	}
	if strings.Contains(r[1][2], s) || strings.Contains(r[2][4], s) {
		t.Errorf("texto novo não usou o valor aprendido: %s | %s", r[1][2], r[2][4])
	}

	// o MESMO texto da primeira mensagem, agora numa mensagem nova: sai mascarado
	primeira := "tentei " + s + " no login e não entrou"
	conversa = append(conversa, msg("assistant", "certo"), msg("user", primeira))
	enviar(t, px, conversa)
	r3 := mensagens(t, corpos[3])
	if r3[0] != r[2][0] {
		t.Fatalf("o reenvio da primeira mensagem mudou")
	}
	if strings.Contains(r3[6], s) {
		t.Errorf("o mesmo texto numa mensagem nova saiu com a máscara antiga: %s", r3[6])
	}
	// e numa conversa nova
	enviar(t, px, []any{msg("user", "outra conversa"), msg("assistant", "ok"), msg("user", primeira)})
	if got := mensagens(t, corpos[4]); strings.Contains(got[2], s) {
		t.Errorf("o mesmo texto numa conversa nova saiu com a máscara antiga: %s", got[2])
	}
	conversa = conversa[:5]
	r[2] = mensagens(t, corpos[2])

	// reinício: memo vazio, mesmo diretório
	var corpos2 [][]byte
	px2 := montarDisco(t, dir, &corpos2)
	enviar(t, px2, conversa)
	if got := mensagens(t, corpos2[0]); strings.Join(got, "\n") != strings.Join(r[2], "\n") {
		t.Fatalf("depois do reinício o histórico mudou:\nantes:  %v\ndepois: %v", r[2], got)
	}
	// e texto novo depois do reinício continua mascarado (valor lembrado em vistos.json)
	conversa = append(conversa, msg("assistant", "ok"), msg("user", "e agora "+s+"?"))
	enviar(t, px2, conversa)
	if got := mensagens(t, corpos2[1]); strings.Contains(got[6], s) {
		t.Errorf("após reinício, texto novo saiu com o valor: %s", got[6])
	}
}

// (b) A senha de exemplo numa URL dentro do resultado de um WebFetch é mascarada ali, mas não
// é lembrada (nem em RAM nem em vistos.json).
func TestResultadoDeWebFetchNaoEnsina(t *testing.T) {
	dir := t.TempDir()
	var corpos [][]byte
	px := montarDisco(t, dir, &corpos)
	ex := "change" + "me"
	solta := "então " + ex + " é só um exemplo da doc?"
	conversa := []any{
		msg("user", "como conecto no mysql?"),
		msg("assistant", []any{map[string]any{"type": "tool_use", "id": "w1", "name": "WebFetch",
			"input": map[string]any{"url": "https://docs.example.com/mysql", "prompt": "como conectar"}}}),
		msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "w1",
			"content": "Exemplo: " + urlCom(ex)}}),
		msg("assistant", "a documentação mostra uma URL de exemplo"),
		msg("user", solta),
	}
	enviar(t, px, conversa)
	r := mensagens(t, corpos[0])
	if strings.Contains(r[2], ex) {
		t.Errorf("no resultado do WebFetch, a senha de exemplo deveria sair mascarada: %s", r[2])
	}
	if !strings.Contains(r[4], ex) {
		t.Errorf("valor vindo da web foi lembrado: %s", r[4])
	}
	var corpos2 [][]byte
	px2 := montarDisco(t, dir, &corpos2) // reinício: só o que está em disco
	enviar(t, px2, []any{msg("user", "outra conversa: "+ex+" aparece aqui")})
	if r := mensagens(t, corpos2[0]); !strings.Contains(r[0], ex) {
		t.Errorf("valor vindo da web foi gravado em vistos.json: %s", r[0])
	}
}

// (e) Ferramenta de conector do claude.ai (roda fora da máquina) recebe o pseudônimo, como
// WebFetch; ferramenta local recebe o real.
func TestConectorNaoDesmascara(t *testing.T) {
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
		for i, nome := range []string{"mcp__claude_ai_Gmail__search_threads", "Bash"} {
			arg, _ := json.Marshal(map[string]string{"query": "from:" + pseudoEmail})
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
	if pseudoEmail == "" || strings.Contains(porIdx[0], email) || !strings.Contains(porIdx[0], pseudoEmail) {
		t.Errorf("o conector recebeu o real (ou nada): %s", porIdx[0])
	}
	if !strings.Contains(porIdx[1], email) {
		t.Errorf("Bash deveria receber o real: %s", porIdx[1])
	}
}

// O marcador de cache muda de lugar a cada mensagem: não pode mudar a posição dos textos
// que vêm depois dele.
func TestCacheControlNaoMudaPosicao(t *testing.T) {
	var corpos [][]byte
	px := montarDisco(t, t.TempDir(), &corpos)
	s := fracaTeste
	txt := func(t string, cc bool) map[string]any {
		b := map[string]any{"type": "text", "text": t}
		if cc {
			b["cache_control"] = map[string]any{"type": "ephemeral"}
		}
		return b
	}
	enviar(t, px, []any{msg("user", []any{txt("contexto inicial", true), txt("tentei "+s+" e nada", false)})})
	enviar(t, px, []any{msg("user", []any{txt("contexto inicial", false), txt("tentei "+s+" e nada", false)}),
		msg("assistant", []any{txt("ok", false)}), msg("user", []any{txt("a url é "+urlCom(s), true)})})
	var a, b []map[string]any
	json.Unmarshal([]byte(mensagens(t, corpos[0])[0]), &struct{ Content *[]map[string]any }{&a})
	json.Unmarshal([]byte(mensagens(t, corpos[1])[0]), &struct{ Content *[]map[string]any }{&b})
	if len(a) != 2 || len(b) != 2 || a[1]["text"] != b[1]["text"] {
		t.Errorf("o cache_control num bloco anterior mudou a máscara do texto seguinte")
	}
}
