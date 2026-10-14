package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// SetFields changes only the fields it gets: what the file had, including fields this program does
// not know and old names, stays (the old names under the new ones), and the previous file is kept.
func TestSetFieldsKeepsTheRest(t *testing.T) {
	t.Setenv("LLM_DLP_HOME", t.TempDir())
	orig := []byte(oldConfig)
	if err := os.WriteFile(Path("config.json"), orig, 0o600); err != nil {
		t.Fatal(err)
	}
	bak, err := SetFields(map[string]any{"internal_domains": []string{"acme", "nova"}})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(bak); string(b) != string(orig) {
		t.Errorf("the copy %s is not the previous file", bak)
	}
	var m map[string]any
	b, _ := os.ReadFile(Path("config.json"))
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["campo_que_nao_existe"] != float64(1) {
		t.Errorf("a field this program does not know was lost:\n%s", b)
	}
	if d, _ := json.Marshal(m["internal_domains"]); string(d) != `["acme","nova"]` {
		t.Errorf("internal_domains = %s", d)
	}
	if _, has := m["dominios_internos"]; has {
		t.Errorf("the old name stayed next to the new one:\n%s", b)
	}
	var c Config
	if err := Read(b, &c); err != nil {
		t.Fatal(err)
	}
	want := expectedFromOld()
	want.InternalDomains = []string{"acme", "nova"}
	if c.Port != want.Port || len(c.Terms) != 1 || c.Terms[0].Values[1] != "Acme" || !c.Objects.PublicIP || c.FailClosed {
		t.Errorf("the other fields changed: %+v", c)
	}
	// no file yet: SetFields creates it, without a copy
	t.Setenv("LLM_DLP_HOME", filepath.Join(t.TempDir(), "new"))
	if bak, err := SetFields(map[string]any{"terms": []Term{{Label: "name", Values: []string{"X"}}}}); err != nil || bak != "" {
		t.Fatalf("new file: %q %v", bak, err)
	}
}
