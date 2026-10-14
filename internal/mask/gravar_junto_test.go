package mask

import (
	"path/filepath"
	"testing"
)

// Dois processos com o mesmo registro (a versão antiga e a nova durante uma troca, ou o
// importar-pessoas com o proxy no ar): cada gravação junta o que o outro gravou, e nenhuma
// apaga a outra. Antes, a última gravação ficava só com o que o seu processo sabia.

func TestSeenFromTwoProcessesKept(t *testing.T) {
	p := filepath.Join(t.TempDir(), "vistos.json")
	a, err := LoadSeen(p)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := LoadSeen(p)
	a.Mark("id-a", "cpf")
	b.Mark("id-b", "email")
	b.MarkObj("id-obj", "tabela", 20000)
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
			t.Errorf("%s sumiu do arquivo", id)
		}
	}
	// o que veio do outro processo vale também na memória de quem juntou
	if !a.Has("email") || !a.HasObj() {
		t.Errorf("a juntou o arquivo mas não conta o que veio dele: email=%v obj=%v", a.Has("email"), a.HasObj())
	}
}

func TestPeopleFromTwoProcessesKept(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pessoas.json")
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
	// o mesmo registro feito num processo só (cada nome entra em mais de uma forma)
	together, _ := LoadPeople(filepath.Join(t.TempDir(), "x.json"))
	together.Import(ps, "João Carlos Silva", "joao.silva@empresa-ficticia.com.br", "U1001")
	together.Import(ps, "Ana Paula Souza", "", "U2001")
	c, _ := LoadPeople(p)
	if c.Total() != together.Total() {
		t.Fatalf("o registro gravado tem %d nomes, esperado %d", c.Total(), together.Total())
	}
	if !c.IsCode(ps, "U1001") || !c.IsCode(ps, "U2001") {
		t.Errorf("os códigos das duas pessoas deviam estar no registro")
	}
	if b.Total() != together.Total() || !b.IsCode(ps, "U1001") {
		t.Errorf("b juntou o arquivo mas não reconhece quem veio dele (total %d)", b.Total())
	}
}
