package mask

import (
	"os"
	"path/filepath"
	"testing"
)

// The people registry written up to 0.9 (field names in Portuguese) is read in full, and saved with
// the current names.
func TestPeopleOldFieldNames(t *testing.T) {
	p := filepath.Join(t.TempDir(), "people.json")
	os.WriteFile(p, []byte(`{"nomes":{"n1":"p1"},"emails":{"e1":"p1"},"codigos":{"c1":"p1"},"primeiros":{"f1":true},"tam_codigos":{"5":true}}`), 0o600)
	ps, err := LoadPeople(p)
	if err != nil {
		t.Fatal(err)
	}
	if ps.D.Names["n1"] != "p1" || ps.D.Emails["e1"] != "p1" || ps.D.Codes["c1"] != "p1" || !ps.D.FirstNames["f1"] || !ps.D.CodeLengths[5] {
		t.Fatalf("read: %+v", ps.D)
	}
	ps.dirty = true
	if err := ps.SaveNow(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if want := `{"names":{"n1":"p1"},"emails":{"e1":"p1"},"codes":{"c1":"p1"},"first_names":{"f1":true},"code_lengths":{"5":true}}`; string(b) != want {
		t.Errorf("saved:\n%s\nwants:\n%s", b, want)
	}
}
