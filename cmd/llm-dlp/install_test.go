package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The lock adds only what is its own, keeps the rest and is removed without a trace.
func TestLockRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "managed-settings.json")
	orig := `{"permissions": {"deny": ["Bash(rm -rf *)"]}, "env": {"HTTPS_PROXY": "http://proxy:3128"}, "outra": true}`
	os.WriteFile(p, []byte(orig), 0o644)
	if _, err := applyLock(p, 8787, false); err != nil {
		t.Fatal(err)
	}
	if _, err := applyLock(p, 8787, false); err != nil { // again: does not duplicate
		t.Fatal(err)
	}
	var s map[string]any
	b, _ := os.ReadFile(p)
	json.Unmarshal(b, &s)
	if !reflect.DeepEqual(s["allowedProviders"], []any{"customEndpoint"}) {
		t.Errorf("allowedProviders: %v", s["allowedProviders"])
	}
	env := s["env"].(map[string]any)
	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:8787" || env["HTTPS_PROXY"] != "http://proxy:3128" {
		t.Errorf("env: %v", env)
	}
	if n := len(s["permissions"].(map[string]any)["deny"].([]any)); n != 1+len(denyLLMDLP) {
		t.Errorf("deny duplicated or missing: %d rules", n)
	}
	if _, err := applyLock(p, 8787, true); err != nil {
		t.Fatal(err)
	}
	var after, before map[string]any
	b, _ = os.ReadFile(p)
	json.Unmarshal(b, &after)
	json.Unmarshal([]byte(orig), &before)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("removing did not go back to the original:\n before %v\n after  %v", before, after)
	}
	if bs, _ := filepath.Glob(p + ".bak-*"); len(bs) != 3 {
		t.Errorf("expected 3 distinct backups, found %d", len(bs))
	}
}
