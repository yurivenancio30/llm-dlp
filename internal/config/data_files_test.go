package config

import (
	"os"
	"testing"
)

// The data files with the names up to 0.9 are copied to the new names once, and stay where they were
// (a version up to 0.9 still reads them). A new name that already exists is never overwritten.
func TestDataPathCopiesOldFile(t *testing.T) {
	t.Setenv("LLM_DLP_HOME", t.TempDir())
	os.WriteFile(Path("chave"), []byte("the-old-key"), 0o600)
	os.WriteFile(Path("vistos.json"), []byte(`{"a":"cpf"}`), 0o600)
	os.WriteFile(Path(PeopleFile), []byte(`{"new":true}`), 0o600)
	os.WriteFile(Path("pessoas.json"), []byte(`{"old":true}`), 0o600)

	for name, wants := range map[string]string{KeyFile: "the-old-key", SeenFile: `{"a":"cpf"}`, PeopleFile: `{"new":true}`} {
		p, err := DataPath(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if b, _ := os.ReadFile(p); string(b) != wants {
			t.Errorf("%s: %q, wants %q", name, b, wants)
		}
		if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v (%v)", name, st.Mode(), err)
		}
	}
	for _, old := range []string{"chave", "vistos.json", "pessoas.json"} {
		if _, err := os.Stat(Path(old)); err != nil {
			t.Errorf("the old %s should stay: %v", old, err)
		}
	}
	// nothing old: the new path, without creating anything
	t.Setenv("LLM_DLP_HOME", t.TempDir())
	if p, err := DataPath(KeyFile); err != nil || p != Path(KeyFile) {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := os.Stat(Path(KeyFile)); err == nil {
		t.Error("created a key file out of nothing")
	}
}
