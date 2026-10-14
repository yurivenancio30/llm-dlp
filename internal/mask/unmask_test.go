package mask

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

func TestRoundTrip(t *testing.T) {
	m := newTest(t)
	s := "Owner: joao.silva@empresa-ficticia.com.br\nHost: mysql-dh.empresa-ficticia.intra (10.42.7.15)\n" +
		"JOAO CARLOS SILVA, cpf " + genCPF("529982247") + ", tel (11) 98765-4321, token=ghp_k3Jd9sLq2mXv8RtY7wPz4nBc6HfG1aE5uQiO"
	masked, ents := m.Mask(s)
	for _, real := range []string{"joao.silva", "mysql-dh", "10.42.7.15", "JOAO CARLOS", "98765", "ghp_"} {
		if strings.Contains(masked, real) {
			t.Errorf("vazou %q: %s", real, masked)
		}
	}
	if back := NewTable(ents).Unmask(masked, false); back != s {
		t.Errorf("ida e volta não bateu:\n%s\n---\n%s", s, back)
	}
}

// Streaming: the masked text is cut at EVERY position (2 chunks) and into random chunks; what comes
// out has to be exactly the real text. Repeats with several keys, because each key generates
// different pseudonyms (and different coincidences).
func TestStreamCut(t *testing.T) {
	real := "a pessoa joao.silva@empresa-ficticia.com.br (JOAO CARLOS SILVA) usa 10.42.7.15 e 10.42.7.200/24; ok 2"
	for c := 0; c < 50; c++ {
		key := []byte(fmt.Sprintf("%032d", c))
		ps, _ := LoadPeople(t.TempDir() + "/p.json")
		ps.Import(NewPseudo(key), "João Carlos Silva", "joao.silva@empresa-ficticia.com.br", "")
		mc, _ := NewMasker(config.Default(), key, ps, nil)
		masked, ents := mc.Mask(real)
		tab := NewTable(ents)
		for cut := 0; cut <= len(masked); cut++ {
			f := tab.NewStream(false)
			out := f.Push(masked[:cut]) + f.Push(masked[cut:]) + f.Close()
			if out != real {
				t.Fatalf("chave %d, corte em %d: %q", c, cut, out)
			}
		}
	}
	m := newTest(t)
	masked, ents := m.Mask(real)
	tab := NewTable(ents)
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		f := tab.NewStream(false)
		var b strings.Builder
		for j := 0; j < len(masked); {
			n := 1 + r.Intn(7)
			if j+n > len(masked) {
				n = len(masked) - j
			}
			b.WriteString(f.Push(masked[j : j+n]))
			j += n
		}
		b.WriteString(f.Close())
		if b.String() != real {
			t.Fatalf("pedaços aleatórios: %q", b.String())
		}
	}
}

func TestStreamJSON(t *testing.T) {
	m := newTest(t)
	masked, ents := m.Mask(`grep "joao.silva@empresa-ficticia.com.br" a.txt`)
	tab := NewTable(ents)
	f := tab.NewStream(true)
	out := f.Push(`{"command":"`+masked[:10]) + f.Push(masked[10:]+`"}`) + f.Close()
	if !strings.Contains(out, "joao.silva@empresa-ficticia.com.br") {
		t.Fatalf("json: %s", out)
	}
}

// A labeled type (registered term, user code: the id key has no case) in three case spellings: one
// pseudonym per spelling, and the way back returns each one as it was. The variant (the model
// rewrote the pseudonym's case) goes back to a spelling that appeared, the uppercase one for the
// all-uppercase pseudonym, and never takes the place of another: "sao paulo" has the same id as
// "SÃO PAULO" and stays as it was.
func TestLabelWithSpellingCase(t *testing.T) {
	m := newTest(t) // termo cadastrado: "Projeto Fenix"
	s := "o Projeto Fenix (PROJETO FENIX no ERP, projeto fenix no git) e o login u1001 / U1001"
	masked, ents := m.Mask(s)
	if strings.Contains(strings.ToLower(masked), "fenix") {
		t.Fatalf("premissa: o termo não foi mascarado: %s", masked)
	}
	tab := NewTable(ents)
	if back := tab.Unmask(masked, false); back != s {
		t.Errorf("a volta mudou a caixa:\n quer %q\n veio %q\n mascarado %q", s, back, masked)
	}
	ps := m.Pseudonym("t:projeto", "PROJETO FENIX")
	id := strings.TrimPrefix(ps, "PROJETO-")
	if ps == id || m.Pseudonym("t:projeto", "projeto fenix") != "projeto-"+id || m.Pseudonym("t:projeto", "Projeto Fenix") != "Projeto-"+id {
		t.Fatalf("formas do pseudônimo: %s %s %s", ps, m.Pseudonym("t:projeto", "projeto fenix"), m.Pseudonym("t:projeto", "Projeto Fenix"))
	}
	if v := tab.Unmask("PROJETO-"+strings.ToUpper(id), false); v != "PROJETO FENIX" {
		t.Errorf("variante em maiúsculas: %q", v)
	}
	if _, ok := fieldValue("nome", "projeto-"+id); ok {
		t.Errorf("o pseudônimo em minúsculas foi tratado como valor de campo")
	}

	sp, spAccent := "sao paulo", "SÃO PAULO"
	pA, pa := m.Pseudonym("t:cidade", spAccent), m.Pseudonym("t:cidade", sp)
	tab = NewTable([]Entry{{pA, spAccent, "t:cidade"}, {pa, sp, "t:cidade"}})
	for ps, wants := range map[string]string{pA: spAccent, pa: sp, strings.ToUpper(pA): spAccent, "Cidade" + strings.TrimPrefix(pA, "CIDADE"): spAccent} {
		if v := tab.Unmask(ps, false); v != wants {
			t.Errorf("%s: quer %q, veio %q", ps, wants, v)
		}
	}
}

// Collision: two reals with the same pseudonym are never unmasked (it would mean picking one).
func TestCollisionDoesNotUnmask(t *testing.T) {
	tab := NewTable([]Entry{
		{"pessoa.aaaa@dom-x.invalid", "a@empresa.com", "email"},
		{"pessoa.aaaa@dom-x.invalid", "b@empresa.com", "email"},
		{"250.1.2.15", "10.0.0.15", "ip"},
		{"250.1.2.16", "10.9.9.16", "ip"}, // mesma sub-rede falsa, sub-redes reais diferentes
		{"pessoa.bbbb@dom-x.invalid", "c@empresa.com", "email"},
	})
	got := tab.Unmask("x pessoa.aaaa@dom-x.invalid y 250.1.2.15 z 250.1.2.99 w pessoa.bbbb@dom-x.invalid", false)
	want := "x pessoa.aaaa@dom-x.invalid y 250.1.2.15 z 250.1.2.99 w c@empresa.com"
	if got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
	if tab.Conflicts != 2 || strings.Join(tab.Collided(), " ") != "250.1.2.x pessoa.aaaa@dom-x.invalid" {
		t.Errorf("conflitos = %d %v, esperado 2", tab.Conflicts, tab.Collided())
	}
}

// The people registry gives the same id to the full name and to the first + last name: it is the
// same person, not a collision. The way back keeps the first spelling seen.
func TestSamePersonNamesDoNotCollide(t *testing.T) {
	tab := NewTable([]Entry{
		{"Pessoa abcdefgh", "Maria Fernanda Silva", "nome"},
		{"Pessoa abcdefgh", "Maria Silva", "nome"},
	})
	if got := tab.Unmask("fale com Pessoa abcdefgh", false); got != "fale com Maria Fernanda Silva" || tab.Conflicts != 0 {
		t.Errorf("%q, conflitos %d", got, tab.Conflicts)
	}
}

func TestConfigNoUnmask(t *testing.T) {
	c := config.Default()
	if !c.NoUnmask("WebFetch") || !c.NoUnmask("WebSearch") || c.NoUnmask("Bash") {
		t.Error("padrão de ferramentas sem desmascarar incorreto")
	}
}

// Unmask with many distinct pseudonyms (a CSV of e-mails in the conversation) has to stay fast and
// correct.
func TestUnmaskWithManyPseudonyms(t *testing.T) {
	m := newTest(t)
	var real strings.Builder
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&real, "linha %d: fulano%d@empresa-ficticia.com.br em 10.%d.%d.%d\n", i, i, i%5, i%8, i%251) // 40 sub-redes
	}
	masked, ents := m.Mask(real.String())
	if strings.Contains(masked, "fulano") {
		t.Fatal("e-mail passou")
	}
	t0 := time.Now()
	tab := NewTable(ents)
	back := tab.Unmask(masked, false)
	if d := time.Since(t0); d > 3*time.Second {
		t.Errorf("montar a tabela e desmascarar %d KB com %d pseudônimos levou %s", len(masked)>>10, len(tab.m), d)
	}
	if back != real.String() {
		t.Error("ida e volta não bateu")
	}
	f := tab.NewStream(false)
	var b strings.Builder
	for i := 0; i < len(masked); i += 11 {
		b.WriteString(f.Push(masked[i:min(i+11, len(masked))]))
	}
	b.WriteString(f.Close())
	if b.String() != real.String() {
		t.Error("ida e volta em streaming não bateu")
	}
}

// The model sometimes cites only the domain of the masked e-mail: it also has to go back to the
// real one.
func TestEmailDomainComesBack(t *testing.T) {
	m := newTest(t)
	masked, ents := m.Mask("o e-mail é joao.silva@empresa-ficticia.com.br")
	dom := masked[strings.LastIndexByte(masked, '@')+1:]
	if back := NewTable(ents).Unmask("o domínio "+dom+" não aparece em vazamentos", false); !strings.Contains(back, "empresa-ficticia.com.br") {
		t.Fatalf("domínio sozinho não voltou: %q", back)
	}
}

// Our pseudonym left over on the way back (from another request, cut, in a case no variant covers)
// and an IP in the fake range go to NotUndone; text with the same shape that was not generated by
// llm-dlp does not.
func TestNotUndone(t *testing.T) {
	m := newTest(t)
	_, otherOne := m.Mask("cpf 529.982.247-25")
	_, thisOne := m.Mask("servidor 10.42.7.15")
	tab := NewTable(thisOne)
	foreign := otherOne[0].Pseudo
	back := tab.Unmask("o "+foreign+" no PR-features, t_abcdefgh e 245.1.2.3; "+thisOne[0].Pseudo, false)
	nd := tab.NotUndone()
	if len(nd) != 2 || nd[0] != foreign || nd[1] != "245.1.2.3" {
		t.Errorf("não desfeitos: %q (volta %q)", nd, back)
	}
	if strings.Contains(back, thisOne[0].Pseudo) {
		t.Errorf("o desta requisição não voltou: %q", back)
	}
}
