package mask

import (
	"path/filepath"
	"testing"
)

// Dois processos com o mesmo registro (a versão antiga e a nova durante uma troca, ou o
// importar-pessoas com o proxy no ar): cada gravação junta o que o outro gravou, e nenhuma
// apaga a outra. Antes, a última gravação ficava só com o que o seu processo sabia.

func TestVistosDeDoisProcessosNaoSeApagam(t *testing.T) {
	p := filepath.Join(t.TempDir(), "vistos.json")
	a, err := CarregarVistos(p)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := CarregarVistos(p)
	a.Marcar("id-a", "cpf")
	b.Marcar("id-b", "email")
	b.MarcarObj("id-obj", "tabela", 20000)
	if err := a.SalvarAgora(); err != nil {
		t.Fatal(err)
	}
	if err := b.SalvarAgora(); err != nil {
		t.Fatal(err)
	}
	a.Marcar("id-a2", "cpf")
	if err := a.SalvarAgora(); err != nil {
		t.Fatal(err)
	}
	c, _ := CarregarVistos(p)
	for _, id := range []string{"id-a", "id-a2", "id-b", "id-obj"} {
		if _, ok := c.Tipo(id); !ok {
			t.Errorf("%s sumiu do arquivo", id)
		}
	}
	// o que veio do outro processo vale também na memória de quem juntou
	if !a.Tem("email") || !a.TemObj() {
		t.Errorf("a juntou o arquivo mas não conta o que veio dele: email=%v obj=%v", a.Tem("email"), a.TemObj())
	}
}

func TestPessoasDeDoisProcessosNaoSeApagam(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pessoas.json")
	ps := NovoPseudo(chaveTeste)
	a, err := CarregarPessoas(p)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := CarregarPessoas(p)
	a.Importar(ps, "João Carlos Silva", "joao.silva@empresa-ficticia.com.br", "U1001")
	b.Importar(ps, "Ana Paula Souza", "", "U2001")
	if err := a.SalvarAgora(); err != nil {
		t.Fatal(err)
	}
	if err := b.SalvarAgora(); err != nil {
		t.Fatal(err)
	}
	// o mesmo registro feito num processo só (cada nome entra em mais de uma forma)
	junto, _ := CarregarPessoas(filepath.Join(t.TempDir(), "x.json"))
	junto.Importar(ps, "João Carlos Silva", "joao.silva@empresa-ficticia.com.br", "U1001")
	junto.Importar(ps, "Ana Paula Souza", "", "U2001")
	c, _ := CarregarPessoas(p)
	if c.Total() != junto.Total() {
		t.Fatalf("o registro gravado tem %d nomes, esperado %d", c.Total(), junto.Total())
	}
	if !c.EhCodigo(ps, "U1001") || !c.EhCodigo(ps, "U2001") {
		t.Errorf("os códigos das duas pessoas deviam estar no registro")
	}
	if b.Total() != junto.Total() || !b.EhCodigo(ps, "U1001") {
		t.Errorf("b juntou o arquivo mas não reconhece quem veio dele (total %d)", b.Total())
	}
}
