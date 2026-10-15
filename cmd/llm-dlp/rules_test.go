package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// An installation from before the newer rules gets them on update/reload: only the missing ones,
// keeping the user's own rules and the rest of settings.json; a settings.json without llm-dlp is not
// touched.
func TestCompleteDenyRules(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(p), 0o700)
	old := `{
  "env": {"ANTHROPIC_BASE_URL": "http://127.0.0.1:8787", "OUTRA": "x"},
  "hooks": {"UserPromptSubmit": [{"hooks": [{"type": "command", "command": "/h/.local/bin/llm-dlp check"}]}]},
  "permissions": {"allow": ["Bash(ls*)"], "deny": ["Read(~/.config/llm-dlp/**)", "Edit(~/.config/llm-dlp/**)", "Bash(*llm-dlp emergencia*)", "Bash(rm -rf*)"]},
  "model": "opus"
}`
	os.WriteFile(p, []byte(old), 0o600)
	added, err := completeDenyRules(home)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Bash(*.config/llm-dlp*)", "Bash(*llm-dlp emergency*)", "Bash(*managed-settings.json*)"}
	if !reflect.DeepEqual(added, want) {
		t.Fatalf("added %v, want %v", added, want)
	}
	b, _ := os.ReadFile(p)
	var s map[string]any
	json.Unmarshal(b, &s)
	if len(missingDeny(denyOf(s))) != 0 || !strings.Contains(string(b), `"Bash(rm -rf*)"`) ||
		!strings.Contains(string(b), `"OUTRA"`) || !strings.Contains(string(b), `"Bash(ls*)"`) || s["model"] != "opus" {
		t.Errorf("settings.json after completing:\n%s", b)
	}
	if again, err := completeDenyRules(home); err != nil || len(again) != 0 {
		t.Errorf("second time: %v %v", again, err)
	}
	// without the llm-dlp hooks, nothing changes
	other := `{"permissions": {"deny": ["Bash(rm -rf*)"]}}`
	os.WriteFile(p, []byte(other), 0o600)
	if added, err := completeDenyRules(home); err != nil || len(added) != 0 {
		t.Errorf("settings.json without llm-dlp: %v %v", added, err)
	}
	if b, _ := os.ReadFile(p); string(b) != other {
		t.Errorf("settings.json without llm-dlp changed:\n%s", b)
	}
}

// The lock is only read: what it lacks is reported, and no lock means nothing to report.
func TestLockMissingRules(t *testing.T) {
	before := lockFile
	defer func() { lockFile = before }()
	lockFile = filepath.Join(t.TempDir(), "managed-settings.json")
	if m := lockMissingRules(); m != nil {
		t.Errorf("no lock: %v", m)
	}
	os.WriteFile(lockFile, []byte(`{"allowedProviders": ["customEndpoint"], "env": {"ANTHROPIC_BASE_URL": "http://127.0.0.1:8787"},
  "permissions": {"deny": ["Read(~/.config/llm-dlp/**)", "Edit(~/.config/llm-dlp/**)", "Bash(*llm-dlp emergencia*)", "Bash(*managed-settings.json*)"]}}`), 0o644)
	if m := lockMissingRules(); !reflect.DeepEqual(m, []string{"Bash(*.config/llm-dlp*)", "Bash(*llm-dlp emergency*)"}) {
		t.Errorf("lock of an older version: %v", m)
	}
	if _, err := applyLock(lockFile, 8787, false); err != nil {
		t.Fatal(err)
	}
	if m := lockMissingRules(); len(m) != 0 {
		t.Errorf("after install-lock: %v", m)
	}
}

// /dev/null is a character device, but nobody answers there: no question, only the command.
func TestStdinTerminalDevNull(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	old := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = old }()
	if stdinTerminal() {
		t.Error("stdin from /dev/null taken as a terminal")
	}
}
