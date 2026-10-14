package mask

import (
	"path/filepath"
	"testing"
)

// Two processes with the same registry (the old and new versions during a swap, or import-people
// with the proxy running): each write merges what the other wrote, and neither erases the other.
// Before, the last write kept only what its own process knew.

func TestSeenFromTwoProcessesKept(t *testing.T) {
	p := filepath.Join(t.TempDir(), "seen.json")
	a, err := LoadSeen(p)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := LoadSeen(p)
	a.Mark("id-a", "cpf")
	b.Mark("id-b", "email")
	b.MarkObj("id-obj", "table", 20000)
	if err := a.SaveNow(); err != nil {
		t.Fatal(err)
	}
	if err := b.SaveNow(); err != nil {
		t.Fatal(err)
	}
	a.Mark("id-a2", "cpf")
	if err := a.SaveNow(); err != nil {
		t.Fatal(err)
	}
	c, _ := LoadSeen(p)
	for _, id := range []string{"id-a", "id-a2", "id-b", "id-obj"} {
		if _, ok := c.Type(id); !ok {
			t.Errorf("%s disappeared from the file", id)
		}
	}
	// what came from the other process also counts in the memory of whoever merged
	if !a.Has("email") || !a.HasObj() {
		t.Errorf("a merged the file but does not count what came from it: email=%v obj=%v", a.Has("email"), a.HasObj())
	}
}

func TestPeopleFromTwoProcessesKept(t *testing.T) {
	p := filepath.Join(t.TempDir(), "people.json")
	ps := NewPseudo(testKey)
	a, err := LoadPeople(p)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := LoadPeople(p)
	a.Import(ps, "João Carlos Silva", "joao.silva@empresa-ficticia.com.br", "U1001")
	b.Import(ps, "Ana Paula Souza", "", "U2001")
	if err := a.SaveNow(); err != nil {
		t.Fatal(err)
	}
	if err := b.SaveNow(); err != nil {
		t.Fatal(err)
	}
	// the same registry made in a single process (each name goes in in more than one form)
	together, _ := LoadPeople(filepath.Join(t.TempDir(), "x.json"))
	together.Import(ps, "João Carlos Silva", "joao.silva@empresa-ficticia.com.br", "U1001")
	together.Import(ps, "Ana Paula Souza", "", "U2001")
	c, _ := LoadPeople(p)
	if c.Total() != together.Total() {
		t.Fatalf("the written registry has %d names, expected %d", c.Total(), together.Total())
	}
	if !c.IsCode(ps, "U1001") || !c.IsCode(ps, "U2001") {
		t.Errorf("the codes of both people should be in the registry")
	}
	if b.Total() != together.Total() || !b.IsCode(ps, "U1001") {
		t.Errorf("b merged the file but does not recognize who came from it (total %d)", b.Total())
	}
}
