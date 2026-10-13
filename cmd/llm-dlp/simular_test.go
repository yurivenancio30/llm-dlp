package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// quaseQuatroMil: um texto de pouco menos de 4000 bytes, cheio de e-mails curtos.
func quaseQuatroMil() string {
	var b strings.Builder
	for i := 0; b.Len() < 3900; i++ {
		b.WriteString("contato: u" + strings.Repeat("x", i%7) + "@ab.co\n")
	}
	return b.String()
}

// saidaDe roda f e devolve o que ela escreveu na saída padrão.
func saidaDe(t *testing.T, f func() error) string {
	t.Helper()
	velho := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	errF := f()
	w.Close()
	os.Stdout = velho
	b, _ := io.ReadAll(r)
	if errF != nil {
		t.Fatal(errF)
	}
	return string(b)
}

// "llm-dlp simulate" repete também o arquivo de um subagente (subagents/agent-*.jsonl), em que toda
// mensagem é lateral (isSidechain). No arquivo da sessão, as laterais continuam puladas. Um arquivo
// sem nada para repetir avisa, em vez de mostrar zeros que parecem "tudo certo".
func TestSimularLeSubagente(t *testing.T) {
	t.Setenv("LLM_DLP_HOME", t.TempDir())
	cfg := config.Padrao()
	chave, err := mask.Chave(config.Caminho("chave"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := mask.NovoMasker(cfg, chave, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	linha := func(papel string, lateral bool, conteudo any) string {
		b, _ := json.Marshal(map[string]any{"type": papel, "isSidechain": lateral,
			"message": map[string]any{"role": papel, "content": conteudo}})
		return string(b)
	}
	conversa := func(lateral bool) []string {
		return []string{
			linha("user", lateral, "liste a pasta da aplicação"),
			linha("assistant", lateral, []any{map[string]any{"type": "tool_use", "id": "t1", "name": "Bash",
				"input": map[string]any{"command": "ls /srv/app"}}}),
			linha("user", lateral, []any{map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "config.yaml"}}),
		}
	}
	dir := t.TempDir()
	for _, c := range []struct {
		nome   string
		linhas []string
		quer   string
	}{
		{"subagente", conversa(true), "2 requisições simuladas"},
		{"sessão com subagente no meio", append(conversa(false), conversa(true)...), "2 requisições simuladas"},
		{"sem mensagem", []string{`{"type":"summary","summary":"x"}`}, "ATENÇÃO: nenhuma requisição"},
		// abaixo de 4000 bytes, mas acima disso depois de mascarado (o pseudônimo de um e-mail
		// curto é maior que ele): o eco da API falsa é cortado e não conta como diferença
		{"texto que cresce ao mascarar", []string{linha("user", false, quaseQuatroMil())}, "resposta voltou diferente do original: 0"},
	} {
		arq := filepath.Join(dir, strings.ReplaceAll(c.nome, " ", "_")+".jsonl")
		if err := os.WriteFile(arq, []byte(strings.Join(c.linhas, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		saida := saidaDe(t, func() error { return simularSessao(arq, cfg, m, 100, 600<<10) })
		if !strings.Contains(saida, c.quer) {
			t.Errorf("%s: esperava %q em:\n%s", c.nome, c.quer, saida)
		}
	}
}
