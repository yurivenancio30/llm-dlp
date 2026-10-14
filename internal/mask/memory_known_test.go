package mask

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Round trip: the value is masked with context, the model repeats it in a neutral text, the proxy
// unmasks it for the user and, in the next message, the history comes back with the real value
// WITHOUT the context. It has to be masked again (and with the same pseudonym).
func TestRoundTripWithoutContext(t *testing.T) {
	cpf := strings.NewReplacer(".", "", "-", "").Replace(genCPF("529982247"))
	cases := []struct{ name, original, real string }{
		{"rg", "RG: 12.345.678-9", "12.345.678-9"},
		{"cnh", "CNH " + genEleven("123456789", ValidCNH), genEleven("123456789", ValidCNH)},
		{"pis", "PIS " + genEleven("1201234567", ValidPIS), genEleven("1201234567", ValidPIS)},
		{"cep", "CEP 01310-100", "01310-100"},
		{"account", "conta corrente 123456-7", "123456-7"},
		{"agencia", "agência 4321-0", "4321-0"},
		{"pix", "chave pix 123e4567-e89b-42d3-a456-426614174000", "123e4567-e89b-42d3-a456-426614174000"},
		{"birthdate", "data de nascimento: 12/03/1985", "12/03/1985"},
		{"phone", "celular 11 98765-4321", "11 98765-4321"},
		{"card", "cartão 4111111111111111", "4111111111111111"},
		{"cpf", "cpf " + cpf, cpf},
		{"senha", "password: Xk9mP2qL7vT4zR8w", "Xk9mP2qL7vT4zR8w"},
	}
	for _, c := range cases {
		m := newTest(t)
		masked, ents := m.Mask(c.original)
		if strings.Contains(masked, c.real) || len(ents) == 0 {
			t.Errorf("%s: not even the first pass masked: %q", c.name, masked)
			continue
		}
		response := "Anotado: o valor " + ents[0].Pseudo + " foi registrado."
		history := NewTable(ents).Unmask(response, false) // it is what Claude Code keeps
		if !strings.Contains(history, c.real) {
			t.Fatalf("%s: unmasking failed: %q", c.name, history)
		}
		again, _ := m.Mask(history)
		if strings.Contains(again, c.real) {
			t.Errorf("%s: LEAKS on the way back: %q", c.name, again)
		} else if again != response {
			t.Errorf("%s: pseudonym changed on the way back (breaks the cache): %q != %q", c.name, again, response)
		}
	}
}

// After a proxy restart the memory is gone; only the hashes on disk remain. The value repeated
// without context still has to be recognized.
func TestRoundTripAfterRestart(t *testing.T) {
	dir := t.TempDir()
	newOne := func() *Masker {
		vs, _ := LoadSeen(dir + "/vistos.json")
		m, err := NewMasker(newTest(t).cfg, testKey, nil, vs)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	m1 := newOne()
	for _, s := range []string{"RG: 12.345.678-9", "password: Xk9mP2qL7vT4zR8w", "CEP 01310-100", "celular 11 98765-4321"} {
		m1.Mask(s)
	}
	if err := m1.seenSet.SaveIfDirty(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dir + "/vistos.json"); strings.Contains(string(b), "12.345") || strings.Contains(string(b), "Xk9mP2") {
		t.Fatalf("readable value written to disk: %s", b)
	}
	m2 := newOne() // "restarted": empty memory
	for _, real := range []string{"12.345.678-9", "Xk9mP2qL7vT4zR8w", "01310-100", "11 98765-4321"} {
		if out, _ := m2.Mask("o valor " + real + " apareceu de novo"); strings.Contains(out, real) {
			t.Errorf("after the restart, it leaked: %q", out)
		}
	}
}

// The value appeared loose BEFORE being learned: the memoized result of that text has to be redone
// when the value becomes known.
func TestMemoRedoneWhenLearning(t *testing.T) {
	m := newTest(t)
	looseOne := "pedido referente a 12.345.678-9 em análise"
	if out, _ := m.Mask(looseOne); out != looseOne {
		t.Fatalf("without context it should not mask yet: %q", out)
	}
	m.Mask("RG: 12.345.678-9")
	if out, _ := m.Mask(looseOne); strings.Contains(out, "12.345.678-9") {
		t.Errorf("old memo reused after learning the value: %q", out)
	}
}

// Learning a short number cannot make similar numbers become sensitive data, and the same numbering
// with other punctuation is recognized and comes back without conflict.
func TestKnownWithoutFalsePositive(t *testing.T) {
	m := newTest(t)
	m.Mask("agência 4321-0")
	cleaned := "a porta 43210 respondeu em 43210 ms; linha 4321 ok"
	if out, _ := m.Mask(cleaned); out != cleaned {
		t.Errorf("learned short number caused a false positive: %q", out)
	}
	m.Mask("RG: 12.345.678-9")
	masked, ents := m.Mask("cadastro 123456789 e também 12.345.678-9")
	if strings.Contains(masked, "123456789") || strings.Contains(masked, "12.345.678-9") {
		t.Fatalf("another punctuation of the same RG was not masked: %q", masked)
	}
	tab := NewTable(ents)
	if tab.Conflicts != 0 {
		t.Errorf("the same numbering with different punctuation was treated as a collision")
	}
}

// After a restart only the hash exists. When the value is recognized by the hash, it goes back to
// the memory; from then on it is caught also glued to something else.
func TestRecognizedByHashReturnsToMemory(t *testing.T) {
	dir := t.TempDir()
	vs, _ := LoadSeen(dir + "/v.json")
	m, _ := NewMasker(newTest(t).cfg, testKey, nil, vs)
	m.Mask("DB_PASSWORD=Ficticia@2024")
	vs.SaveIfDirty()

	vs2, _ := LoadSeen(dir + "/v.json")
	m2, _ := NewMasker(m.cfg, testKey, nil, vs2) // "restarted": empty memory
	// first appearance after the restart already glued, without a keyword: caught by the pieces
	for _, stuck := range []string{"GET /x?a=1&k=Ficticia@2024&b=2", "mysql://app:Ficticia@2024@db1:3306/x", "v=Ficticia@2024"} {
		vs3, _ := LoadSeen(dir + "/v.json")
		m3, _ := NewMasker(m.cfg, testKey, nil, vs3)
		if out, _ := m3.Mask(stuck); strings.Contains(out, "Ficticia@2024") {
			t.Fatalf("glued, right after the restart, went through: %s", out)
		}
	}
	if out, _ := m2.Mask("erro ao logar com Ficticia@2024 no banco"); strings.Contains(out, "Ficticia@2024") {
		t.Fatalf("loose, after the restart, should be caught by the hash: %s", out)
	}
	// recognized by the hash, goes back to the memory: from then on it is caught in any position
	if out, _ := m2.Mask("xFicticia@2024;y e (Ficticia@2024)"); strings.Count(out, "Ficticia@2024") != 1 {
		t.Fatalf("expected only the occurrence glued to a letter without a mask: %s", out)
	}
}

// The index search has to find exactly what the naive search (value by value) finds.
func TestScanEqualsNaiveSearch(t *testing.T) {
	c := newKnown()
	vals := []string{"Xk9$mQ2vLp7w", "12.345.678-9", "1234-5", "abcd", "abcdef", "Xk9$", "ção-ñ9"}
	for _, v := range vals {
		c.learnIt("secret", v)
	}
	text := "a Xk9$mQ2vLp7w b 12.345.678-9, (1234-5) xabcd abcdef abcd. 912.345.678-9 Xk9$ ção-ñ9 abc 1234-56"
	naive := map[string]bool{}
	for _, v := range vals {
		for i := 0; ; {
			j := strings.Index(text[i:], v)
			if j < 0 {
				break
			}
			ini, end := i+j, i+j+len(v)
			i = ini + 1
			if isAlnum(v[0]) && ini > 0 && isAlnum(text[ini-1]) {
				continue
			}
			if isAlnum(v[len(v)-1]) && end < len(text) && isAlnum(text[end]) {
				continue
			}
			naive[fmt.Sprint(ini, "-", end)] = true
		}
	}
	index := map[string]bool{}
	c.scan(text, 0, func(ini, end int, _ string) bool { index[fmt.Sprint(ini, "-", end)] = true; return true })
	if len(naive) < 7 || fmt.Sprint(naive) != fmt.Sprint(index) {
		t.Fatalf("naive %v\nindex %v", naive, index)
	}
}

// The RAM memory has a ceiling; what leaves it stays recognized by the hash on disk.
func TestKnownHasCap(t *testing.T) {
	vs, _ := LoadSeen(t.TempDir() + "/v.json")
	m, _ := NewMasker(newTest(t).cfg, testKey, nil, vs)
	first := "senha: Zq8wPrimeiroValor01"
	m.Mask(first)
	for i := 0; i < maxKnown+10; i++ {
		m.known.learnIt("secret", fmt.Sprintf("Lixo%08dabc", i))
	}
	if n := len(m.known.reals); n > maxKnown {
		t.Fatalf("RAM without a ceiling: %d values", n)
	}
	if len(m.known.typ) != len(m.known.reals) || len(m.known.seq) != len(m.known.reals) {
		t.Fatalf("maps did not follow the list: %d %d %d", len(m.known.typ), len(m.known.seq), len(m.known.reals))
	}
	if _, ok := m.known.typ["Zq8wPrimeiroValor01"]; ok {
		t.Fatal("the oldest value should have left the RAM")
	}
	if out, _ := m.Mask("erro com Zq8wPrimeiroValor01 no log"); strings.Contains(out, "Zq8wPrimeiroValor01") {
		t.Fatalf("value that left the RAM leaked: %s", out)
	}
	// text memoized before leaving: it can no longer be checked, so it is redone
	if !m.known.containsSince("qualquer", 0) {
		t.Fatal("a generation before the ceiling should force a redo")
	}
}

func TestSeenHasCap(t *testing.T) {
	v, _ := LoadSeen(t.TempDir() + "/v.json")
	for i := 0; i < maxSeen+1000; i++ {
		v.Mark(fmt.Sprint("id", i), "rg")
	}
	if len(v.ids) > maxSeen || v.n["rg"] != len(v.ids) {
		t.Fatalf("hashes without a ceiling: %d (counter %d)", len(v.ids), v.n["rg"])
	}
}

// Item 4 (external review): a value that went out in the clear in a text and was only learned later
// (in another spelling) cannot stay in the clear when the history is resent. The memo of the old
// text has to be redone.
func TestMemoRedoneWhenLearningOtherSpelling(t *testing.T) {
	m := newTest(t)
	old := "linha 7: 123456789;ATIVO"
	if out, _ := m.Mask(old); !strings.Contains(out, "123456789") {
		t.Skip("premise changed: the number is already masked by itself")
	}
	m.Mask("RG: 12.345.678-9`") // learns the same number, with punctuation
	if out, _ := m.Mask(old); strings.Contains(out, "123456789") {
		t.Fatalf("the old text stayed in plain text after the value was learned: %q", out)
	}
}
