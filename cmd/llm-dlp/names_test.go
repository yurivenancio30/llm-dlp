package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// The Portuguese names from before 0.9.0 lead to a command that exists (in the help, or among the
// internal ones), and the help shows none of them.
func TestOldCommandNames(t *testing.T) {
	help := helpText()
	for stale, newOne := range oldNames {
		if !strings.Contains(help, "llm-dlp "+newOne) && !strings.Contains(help, newOne+",") && !strings.HasSuffix(strings.TrimSpace(help), newOne) {
			t.Errorf("%s -> %s: the new name is not in the help", stale, newOne)
		}
		if strings.Contains(help, "llm-dlp "+stale+" ") || strings.Contains(help, "llm-dlp "+stale+"\n") {
			t.Errorf("the help still shows the old name %q", stale)
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

// install replaces the old hooks with the new ones (without duplicating and without touching other
// programs' hooks); update to an earlier version puts the old ones back; uninstall removes both.
func TestHooksNewAndOldNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home) // uninstall uses the user's home: never the real ~/.claude
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
		t.Errorf("install left old hooks: %s", h)
	}
	if strings.Count(h, bin+" ensure") != 1 || strings.Count(h, bin+" check") != 1 || !strings.Contains(h, "outro-programa") {
		t.Errorf("install: expected ensure and check once each, and the other program's hook: %s", h)
	}
	if _, err := applyClaude(home, bin, cfg); err != nil {
		t.Fatal(err)
	}
	if h2 := readHooks(t, target); strings.Count(h2, bin+" ensure") != 1 {
		t.Errorf("install again duplicated: %s", h2)
	}

	if changed, err := hooksForVersion(home, bin, "0.8.0"); err != nil || !changed {
		t.Fatalf("going back to 0.8.0: changed=%v err=%v", changed, err)
	}
	h = readHooks(t, target)
	if !strings.Contains(h, bin+" garantir") || !strings.Contains(h, bin+" verificar") || strings.Contains(h, " ensure") {
		t.Errorf("going back to 0.8.0 should put the old names: %s", h)
	}
	if changed, _ := hooksForVersion(home, bin, "0.8.0"); changed {
		t.Errorf("it already had the 0.8.0 names: it should not touch them")
	}

	if err := uninstall(nil); err != nil {
		t.Fatal(err)
	}
	h = readHooks(t, target)
	if strings.Contains(h, "llm-dlp") || !strings.Contains(h, "outro-programa") {
		t.Errorf("uninstall: should remove only llm-dlp's: %s", h)
	}
}
