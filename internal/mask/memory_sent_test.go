package mask

import (
	"os"
	"strings"
	"testing"
)

// Passwords and labels built from parts: the test text must not teach anything to an llm-dlp that
// is in the path of whoever edits the code.
var weakOnes = []string{"change" + "me", "ad" + "min", "gira" + "ssol"}

// teaches: the password in a connection URL (where even a letters-only one is detected) testCPF: a
// valid fictitious CPF
var testCPF = "529.982" + ".247-25"

// test positions (in the proxy, they come from the hash of the conversation up to the text)
var posA, posB = Position{1}, Position{2}

func teaches(s string) string { return "mysql://app:" + s + "@db1:3306/base" }

func diskMasker(t *testing.T, dir string) (*Masker, *Seen) {
	t.Helper()
	vs, err := LoadSeen(dir + "/vistos.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewMasker(newTest(t).cfg, testKey, nil, vs)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.UseSent(dir+"/enviados.log", "teste"); err != nil {
		t.Fatal(err)
	}
	return m, vs
}

// (a) A weak password, letters only or looking like an example, said by the user or read from a
// local file, is remembered: it appears later without a label and is masked.
func TestWeakPasswordIsRemembered(t *testing.T) {
	for _, originV := range []string{"o usuário digitou: " + teaches("%s"), "# .env lido do disco\nDATABASE_URL=" + teaches("%s") + "\n"} {
		m := newTest(t)
		for _, s := range weakOnes {
			m.Mask(strings.Replace(originV, "%s", s, 1))
			if out, _ := m.Mask("tentei " + s + " e falhou"); strings.Contains(out, s) {
				t.Errorf("weak password was not remembered (origin %q): %q", originV[:8], out)
			}
		}
	}
}

// (b) The same word, as an example password in a URL of a WebFetch result: it is masked there, but
// is neither remembered nor written to vistos.json.
func TestWebContentDoesNotTeach(t *testing.T) {
	dir := t.TempDir()
	m, vs := diskMasker(t, dir)
	ex := weakOnes[0]
	page := "Exemplo da documentação: mysql://app:" + ex + "@db.example.com:3306/base"
	out, _ := m.NewBatch().Mask(page, true, posA)
	if strings.Contains(out, ex) {
		t.Fatalf("on the page itself, the example password should be masked: %q", out)
	}
	loose := "a palavra " + ex + " aparece em outro texto"
	if out, _ := m.Mask(loose); out != loose {
		t.Errorf("value coming from the web was remembered: %q", out)
	}
	if _, ok := vs.Type(m.p.ID("visto", "s:"+ex)); ok {
		t.Errorf("value coming from the web was written to seen.json")
	}
	vs.SaveIfDirty()
	m2, _ := diskMasker(t, dir)
	if out, _ := m2.Mask(loose); out != loose {
		t.Errorf("after the restart, a value coming from the web is masked: %q", out)
	}
	// the same text coming from a local source teaches normally
	m.Mask(page)
	if out, _ := m.Mask(loose); out == loose {
		t.Errorf("the same text, from a local source, should teach")
	}
}

// (c) A text that already went out goes out the same later, even if one of its values is learned
// afterwards, and also after a restart (empty memo). A new text uses the learned value.
func TestSentTextStaysFrozen(t *testing.T) {
	dir := t.TempDir()
	m, vs := diskMasker(t, dir)
	s := weakOnes[2]
	old := "tentei " + s + " no login e não entrou; CPF " + testCPF
	l := m.NewBatch()
	sent, _ := l.Mask(old, false, posA)
	if !strings.Contains(sent, s) {
		t.Skip("premise changed: the word is already masked without a label")
	}
	l.Freeze()

	m.Mask(teaches(s)) // learns later
	if out, _ := m.NewBatch().Mask(old, false, posA); out != sent {
		t.Fatalf("text already sent changed after learning a value:\nbefore: %q\nafter:  %q", sent, out)
	}
	if out, _ := m.Mask("de novo: " + s); strings.Contains(out, s) {
		t.Fatalf("new text did not use the learned value: %q", out)
	}
	// the SAME text, at another position (another conversation, new message): does not inherit the
	// old mask
	if out, _ := m.NewBatch().Mask(old, false, posB); strings.Contains(out, s) {
		t.Fatalf("the same text in another position came out with the old mask: %q", out)
	}

	m.Persist()
	vs.SaveIfDirty()
	m2, _ := diskMasker(t, dir) // restart
	out, ents := m2.NewBatch().Mask(old, false, posA)
	if out != sent {
		t.Fatalf("after the restart, the sent text changed:\nbefore: %q\nafter:  %q", sent, out)
	}
	if len(ents) == 0 || ents[0].Real != testCPF {
		t.Errorf("after the restart, the entry to unmask the response was missing: %+v", ents)
	}
	if out, _ := m2.Mask("outra vez: " + s); strings.Contains(out, s) {
		t.Errorf("after the restart, new text did not use the learned value: %q", out)
	}
	if b, _ := os.ReadFile(dir + "/enviados.log"); strings.Contains(string(b), s) || strings.Contains(string(b), testCPF) {
		t.Errorf("real value written to sent.log")
	}
}

// Different configuration or version: the old records are not valid (the text is masked with the
// current rules).
func TestSentFromOtherConfigInvalid(t *testing.T) {
	dir := t.TempDir()
	m, _ := diskMasker(t, dir)
	s := weakOnes[2]
	old := "tentei " + s + " no login"
	l := m.NewBatch()
	l.Mask(old, false, posA)
	l.Freeze()
	m.Mask(teaches(s))
	m.Persist()
	m.seenSet.SaveIfDirty()

	vs, _ := LoadSeen(dir + "/vistos.json")
	m2, _ := NewMasker(newTest(t).cfg, testKey, nil, vs)
	m2.UseSent(dir+"/enviados.log", "outra configuração")
	if out, _ := m2.NewBatch().Mask(old, false, posA); strings.Contains(out, s) {
		t.Errorf("record of another configuration was used: %q", out)
	}
}

// Corrupted line or a piece outside the text: does not rebuild, masks again.
func TestSentCorrupted(t *testing.T) {
	dir := t.TempDir()
	m, _ := diskMasker(t, dir)
	text := "CPF " + testCPF
	m.sentSet.Record(m.posID(posA), []snippet{{4, 99, "cpf", "CPF-x"}})
	if out, _ := m.NewBatch().Mask(text, false, posA); strings.Contains(out, text[4:]) {
		t.Errorf("invalid snippet was used: %q", out)
	}
	m.Persist()
	f, _ := os.OpenFile(dir+"/enviados.log", os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("{quebrado\n")
	f.Close()
	if _, err := LoadSent(dir+"/enviados.log", m.sentSet.fingerprint); err != nil {
		t.Fatal(err)
	}
}

// Freezing is per position: the same text at another position, after a restart, is also masked with
// what is known now.
func TestFrozenOnlySamePositionAfterRestart(t *testing.T) {
	dir := t.TempDir()
	m, vs := diskMasker(t, dir)
	s := weakOnes[2]
	old := "tentei " + s + " no login"
	l := m.NewBatch()
	sent, _ := l.Mask(old, false, posA)
	l.Freeze()
	m.Mask(teaches(s))
	m.Persist()
	vs.SaveIfDirty()
	m2, _ := diskMasker(t, dir)
	if out, _ := m2.NewBatch().Mask(old, false, posA); out != sent {
		t.Errorf("resend in the same position changed after the restart")
	}
	if out, _ := m2.NewBatch().Mask(old, false, posB); strings.Contains(out, s) {
		t.Errorf("in another position, after the restart, it came out with the old mask: %q", out)
	}
}
