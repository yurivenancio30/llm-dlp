package proxy

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Trilha B (mask/chamada.go): o proxy liga a mensagem do usuário, o tool_use e o tool_result
// em ordem, e a dica de cada um leva o que se sabe da chamada.

func montarComMasker(t *testing.T, corpos *[][]byte) (*httptest.Server, *mask.Masker) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Padrao()
	vs, err := mask.CarregarVistos(dir + "/vistos.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := mask.NovoMasker(cfg, []byte("0123456789abcdef0123456789abcdef"), nil, vs)
	if err != nil {
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
	return px, m
}

func chamadaBash(id, cmd string) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": "Bash", "input": map[string]any{"command": cmd}}
}

func resultado(id, saida string) map[string]any {
	return map[string]any{"type": "tool_result", "tool_use_id": id, "content": []any{map[string]any{"type": "text", "text": saida}}}
}

func TestProxyInventarioEEco(t *testing.T) {
	var corpos [][]byte
	px, m := montarComMasker(t, &corpos)
	ps := m.Pseudonimo("obj.namespace", "cobranca")
	tab := mask.NovaTabela([]mask.Entrada{{Pseudo: ps, Real: "cobranca", Tipo: "obj.namespace"}})
	cmdLogs := m.RegistrarResposta("kubectl logs -n "+ps+" deploy/web --tail 5", tab)
	enviar(t, px, []any{
		msg("user", "<system-reminder>lembrete qualquer</system-reminder>lista os namespaces do cluster"),
		msg("assistant", []any{chamadaBash("c1", "kubectl get ns")}),
		msg("user", []any{resultado("c1", "NAME         STATUS   AGE\npagamentos   Active   12d\ncobranca     Active   40d\nvitrine      Active   3d\n")}),
		msg("assistant", []any{chamadaBash("c2", "curl -s https://h.exemplo/api/ambientes/vitrine")}),
		msg("user", []any{resultado("c2", `{"ok": true}`)}),
		msg("assistant", []any{chamadaBash("c3", cmdLogs)}),
		msg("user", []any{resultado("c3", "conectado ao cobranca\npronto\n")}),
		msg("assistant", []any{chamadaBash("c4", "ls")}),
		msg("user", []any{resultado("c4", "docs\ninternal\nscripts\n")}),
	})
	r := mensagens(t, corpos[0])
	for _, n := range []string{"pagamentos", "cobranca", "vitrine"} {
		if strings.Contains(r[2], n) {
			t.Errorf("%s em claro no inventário", n)
		}
	}
	if strings.Contains(r[3], "vitrine") {
		t.Errorf("eco em claro no tool_use: %s", r[3])
	}
	if strings.Contains(r[6], "cobranca") {
		t.Errorf("palavra traduzida em claro na saída: %s", r[6])
	}
	if !strings.Contains(r[8], "docs") || !strings.Contains(r[8], "internal") {
		t.Errorf("ls de outro turno mascarado: %s", r[8])
	}
}
