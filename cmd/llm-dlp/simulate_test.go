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

// almostFourThousand: a text of a little under 4000 bytes, full of short e-mails.
func almostFourThousand() string {
	var b strings.Builder
	for i := 0; b.Len() < 3900; i++ {
		b.WriteString("contato: u" + strings.Repeat("x", i%7) + "@ab.co\n")
	}
	return b.String()
}

// outputOf runs f and returns what it wrote to standard output.
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

// "llm-dlp simulate" also replays a subagent's file (subagents/agent-*.jsonl), in which every
// message is a side chain (isSidechain). In the session file, side chains are still skipped. A file
// with nothing to replay warns, instead of showing zeros that look like "all good".
func TestSimulateReadsSubagent(t *testing.T) {
	t.Setenv("LLM_DLP_HOME", t.TempDir())
	cfg := config.Default()
	key, err := mask.Key(config.Path(config.KeyFile))
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
		{"subagente", conversation(true), "2 simulated requests"},
		{"sessão com subagente no meio", append(conversation(false), conversation(true)...), "2 simulated requests"},
		{"sem mensagem", []string{`{"type":"summary","summary":"x"}`}, "WARNING: no request replayed"},
		// below 4000 bytes, but above that after masking (the pseudonym of a short e-mail is longer
		// than it): the fake API's echo is cut and does not count as a difference
		{"texto que cresce ao mascarar", []string{line("user", false, almostFourThousand())}, "response came back different from the original: 0"},
	} {
		file := filepath.Join(dir, strings.ReplaceAll(c.name, " ", "_")+".jsonl")
		if err := os.WriteFile(file, []byte(strings.Join(c.lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		output := outputOf(t, func() error { return simulateSession(file, cfg, m, 100, 600<<10) })
		if !strings.Contains(output, c.wants) {
			t.Errorf("%s: expected %q in:\n%s", c.name, c.wants, output)
		}
	}
}
