package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// Os nomes em português de antes da 0.9.0 levam a um comando que existe (na ajuda, ou entre os
// internos), e a ajuda não mostra nenhum deles.
func TestOldCommandNames(t *testing.T) {
	help := helpText()
	for stale, newOne := range oldNames {
		if !strings.Contains(help, "llm-dlp "+newOne) && !strings.Contains(help, newOne+",") && !strings.HasSuffix(strings.TrimSpace(help), newOne) {
			t.Errorf("%s -> %s: o nome novo não está na ajuda", stale, newOne)
		}
		if strings.Contains(help, "llm-dlp "+stale+" ") || strings.Contains(help, "llm-dlp "+stale+"\n") {
			t.Errorf("a ajuda ainda mostra o nome antigo %q", stale)
		}
	}
}

func readHooks(t *testing.T, target string) string {
	t.Helper()
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	json.Unmarshal(b, &s)
	h, _ := json.Marshal(s["hooks"])
	return string(h)
}

// install troca os hooks antigos pelos novos (sem duplicar e sem mexer nos de outros
// programas); update para uma versão anterior põe os antigos de volta; uninstall tira os dois.
func TestHooksNewAndOldNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home) // uninstall usa a home do usuário: nunca o ~/.claude de verdade
	t.Setenv("LLM_DLP_HOME", filepath.Join(home, "cfg"))
	bin := filepath.Join(home, ".local", "bin", "llm-dlp")
	target := filepath.Join(home, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(target), 0o700)
	before := `{"hooks": {
	  "SessionStart": [{"hooks": [{"type": "command", "command": "` + bin + ` garantir", "timeout": 20}]},
	                   {"hooks": [{"type": "command", "command": "outro-programa iniciar"}]}],
	  "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "` + bin + ` verificar", "timeout": 20}]}]}}`
	os.WriteFile(target, []byte(before), 0o600)
	cfg := config.Default()

	if _, err := applyClaude(home, bin, cfg); err != nil {
		t.Fatal(err)
	}
	h := readHooks(t, target)
	if strings.Contains(h, " garantir") || strings.Contains(h, " verificar") {
		t.Errorf("install deixou hooks antigos: %s", h)
	}
	if strings.Count(h, bin+" ensure") != 1 || strings.Count(h, bin+" check") != 1 || !strings.Contains(h, "outro-programa") {
		t.Errorf("install: esperava ensure e check uma vez cada, e o hook do outro programa: %s", h)
	}
	if _, err := applyClaude(home, bin, cfg); err != nil {
		t.Fatal(err)
	}
	if h2 := readHooks(t, target); strings.Count(h2, bin+" ensure") != 1 {
		t.Errorf("install de novo duplicou: %s", h2)
	}

	if changed, err := hooksForVersion(home, bin, "0.8.0"); err != nil || !changed {
		t.Fatalf("voltar para a 0.8.0: mudou=%v err=%v", changed, err)
	}
	h = readHooks(t, target)
	if !strings.Contains(h, bin+" garantir") || !strings.Contains(h, bin+" verificar") || strings.Contains(h, " ensure") {
		t.Errorf("voltar para a 0.8.0 devia pôr os nomes antigos: %s", h)
	}
	if changed, _ := hooksForVersion(home, bin, "0.8.0"); changed {
		t.Errorf("já estava com os nomes da 0.8.0: não devia mexer")
	}

	if err := uninstall(nil); err != nil {
		t.Fatal(err)
	}
	h = readHooks(t, target)
	if strings.Contains(h, "llm-dlp") || !strings.Contains(h, "outro-programa") {
		t.Errorf("uninstall: devia tirar só os do llm-dlp: %s", h)
	}
}
