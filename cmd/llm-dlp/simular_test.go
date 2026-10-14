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

// almostFourThousand: um texto de pouco menos de 4000 bytes, cheio de e-mails curtos.
func almostFourThousand() string {
	var b strings.Builder
	for i := 0; b.Len() < 3900; i++ {
		b.WriteString("contato: u" + strings.Repeat("x", i%7) + "@ab.co\n")
	}
	return b.String()
}

// outputOf roda f e devolve o que ela escreveu na saída padrão.
func outputOf(t *testing.T, f func() error) string {
	t.Helper()
	stale := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	errF := f()
	w.Close()
	os.Stdout = stale
	b, _ := io.ReadAll(r)
	if errF != nil {
		t.Fatal(errF)
	}
	return string(b)
}

// "llm-dlp simulate" repete também o arquivo de um subagente (subagents/agent-*.jsonl), em que toda
// mensagem é lateral (isSidechain). No arquivo da sessão, as laterais continuam puladas. Um arquivo
// sem nada para repetir avisa, em vez de mostrar zeros que parecem "tudo certo".
func TestSimulateReadsSubagent(t *testing.T) {
	t.Setenv("LLM_DLP_HOME", t.TempDir())
	cfg := config.Default()
	key, err := mask.Key(config.Path("chave"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := mask.NewMasker(cfg, key, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	line := func(hostRole string, lateral bool, content any) string {
		b, _ := json.Marshal(map[string]any{"type": hostRole, "isSidechain": lateral,
			"message": map[string]any{"role": hostRole, "content": content}})
		return string(b)
	}
	conversation := func(lateral bool) []string {
		return []string{
			line("user", lateral, "liste a pasta da aplicação"),
			line("assistant", lateral, []any{map[string]any{"type": "tool_use", "id": "t1", "name": "Bash",
				"input": map[string]any{"command": "ls /srv/app"}}}),
			line("user", lateral, []any{map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "config.yaml"}}),
		}
	}
	dir := t.TempDir()
	for _, c := range []struct {
		name  string
		lines []string
		wants string
	}{
		{"subagente", conversation(true), "2 requisições simuladas"},
		{"sessão com subagente no meio", append(conversation(false), conversation(true)...), "2 requisições simuladas"},
		{"sem mensagem", []string{`{"type":"summary","summary":"x"}`}, "ATENÇÃO: nenhuma requisição"},
		// abaixo de 4000 bytes, mas acima disso depois de mascarado (o pseudônimo de um e-mail
		// curto é maior que ele): o eco da API falsa é cortado e não conta como diferença
		{"texto que cresce ao mascarar", []string{line("user", false, almostFourThousand())}, "resposta voltou diferente do original: 0"},
	} {
		file := filepath.Join(dir, strings.ReplaceAll(c.name, " ", "_")+".jsonl")
		if err := os.WriteFile(file, []byte(strings.Join(c.lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		output := outputOf(t, func() error { return simulateSession(file, cfg, m, 100, 600<<10) })
		if !strings.Contains(output, c.wants) {
			t.Errorf("%s: esperava %q em:\n%s", c.name, c.wants, output)
		}
	}
}
