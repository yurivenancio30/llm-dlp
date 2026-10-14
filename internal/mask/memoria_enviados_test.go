package mask

import (
	"os"
	"strings"
	"testing"
)

// Senhas e rótulos montados por partes: o texto do teste não deve ensinar nada a um
// llm-dlp que esteja no caminho de quem edita o código.
var weakOnes = []string{"change" + "me", "ad" + "min", "gira" + "ssol"}

// ensina: a senha numa URL de conexão (onde até a só de letras é detectada)
// testCPF: CPF fictício válido
var testCPF = "529.982" + ".247-25"

// posições de teste (no proxy, vêm do hash da conversa até o texto)
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

// (a) Senha fraca, só de letras ou com cara de exemplo, dita pelo usuário ou lida de um
// arquivo local, é lembrada: aparece depois sem rótulo e é mascarada.
func TestWeakPasswordIsRemembered(t *testing.T) {
	for _, originV := range []string{"o usuário digitou: " + teaches("%s"), "# .env lido do disco\nDATABASE_URL=" + teaches("%s") + "\n"} {
		m := newTest(t)
		for _, s := range weakOnes {
			m.Mask(strings.Replace(originV, "%s", s, 1))
			if out, _ := m.Mask("tentei " + s + " e falhou"); strings.Contains(out, s) {
				t.Errorf("senha fraca não foi lembrada (origem %q): %q", originV[:8], out)
			}
		}
	}
}

// (b) A mesma palavra, como senha de exemplo numa URL de um resultado de WebFetch: é mascarada
// ali, mas não é lembrada nem gravada em vistos.json.
func TestWebContentDoesNotTeach(t *testing.T) {
	dir := t.TempDir()
	m, vs := diskMasker(t, dir)
	ex := weakOnes[0]
	page := "Exemplo da documentação: mysql://app:" + ex + "@db.example.com:3306/base"
	out, _ := m.NewBatch().Mask(page, true, posA)
	if strings.Contains(out, ex) {
		t.Fatalf("na própria página, a senha de exemplo deveria ser mascarada: %q", out)
	}
	loose := "a palavra " + ex + " aparece em outro texto"
	if out, _ := m.Mask(loose); out != loose {
		t.Errorf("valor vindo da web foi lembrado: %q", out)
	}
	if _, ok := vs.Type(m.p.ID("visto", "s:"+ex)); ok {
		t.Errorf("valor vindo da web foi gravado em vistos.json")
	}
	vs.SaveIfDirty()
	m2, _ := diskMasker(t, dir)
	if out, _ := m2.Mask(loose); out != loose {
		t.Errorf("após reinício, valor vindo da web é mascarado: %q", out)
	}
	// o mesmo texto vindo de fonte local ensina normalmente
	m.Mask(page)
	if out, _ := m.Mask(loose); out == loose {
		t.Errorf("o mesmo texto, de fonte local, deveria ensinar")
	}
}

// (c) Text que já saiu sai igual depois, mesmo que um valor dele seja aprendido mais
// tarde, e também depois de um reinício (memo vazio). Text novo usa o valor aprendido.
func TestSentTextStaysFrozen(t *testing.T) {
	dir := t.TempDir()
	m, vs := diskMasker(t, dir)
	s := weakOnes[2]
	old := "tentei " + s + " no login e não entrou; CPF " + testCPF
	l := m.NewBatch()
	sent, _ := l.Mask(old, false, posA)
	if !strings.Contains(sent, s) {
		t.Skip("premissa mudou: a palavra já é mascarada sem rótulo")
	}
	l.Freeze()

	m.Mask(teaches(s)) // aprende depois
	if out, _ := m.NewBatch().Mask(old, false, posA); out != sent {
		t.Fatalf("texto já enviado mudou depois de aprender um valor:\nantes:  %q\ndepois: %q", sent, out)
	}
	if out, _ := m.Mask("de novo: " + s); strings.Contains(out, s) {
		t.Fatalf("texto novo não usou o valor aprendido: %q", out)
	}
	// o MESMO texto, noutra posição (outra conversa, mensagem nova): não herda a máscara antiga
	if out, _ := m.NewBatch().Mask(old, false, posB); strings.Contains(out, s) {
		t.Fatalf("o mesmo texto noutra posição saiu com a máscara antiga: %q", out)
	}

	m.Persist()
	vs.SaveIfDirty()
	m2, _ := diskMasker(t, dir) // reinício
	out, ents := m2.NewBatch().Mask(old, false, posA)
	if out != sent {
		t.Fatalf("após reinício, o texto enviado mudou:\nantes:  %q\ndepois: %q", sent, out)
	}
	if len(ents) == 0 || ents[0].Real != testCPF {
		t.Errorf("após reinício, faltou a entrada para desmascarar a resposta: %+v", ents)
	}
	if out, _ := m2.Mask("outra vez: " + s); strings.Contains(out, s) {
		t.Errorf("após reinício, texto novo não usou o valor aprendido: %q", out)
	}
	if b, _ := os.ReadFile(dir + "/enviados.log"); strings.Contains(string(b), s) || strings.Contains(string(b), testCPF) {
		t.Errorf("valor real gravado em enviados.log")
	}
}

// Configuração ou versão diferente: os registros antigos não valem (o texto é mascarado
// com as regras atuais).
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
		t.Errorf("registro de outra configuração foi usado: %q", out)
	}
}

// Line corrompida ou trecho fora do texto: não remonta, mascara de novo.
func TestSentCorrupted(t *testing.T) {
	dir := t.TempDir()
	m, _ := diskMasker(t, dir)
	text := "CPF " + testCPF
	m.sentSet.Record(m.posID(posA), []snippet{{4, 99, "cpf", "CPF-x"}})
	if out, _ := m.NewBatch().Mask(text, false, posA); strings.Contains(out, text[4:]) {
		t.Errorf("trecho inválido foi usado: %q", out)
	}
	m.Persist()
	f, _ := os.OpenFile(dir+"/enviados.log", os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("{quebrado\n")
	f.Close()
	if _, err := LoadSent(dir+"/enviados.log", m.sentSet.fingerprint); err != nil {
		t.Fatal(err)
	}
}

// O congelamento é por posição: o mesmo texto noutra posição, depois de um reinício, também
// é mascarado com o que se sabe agora.
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
		t.Errorf("reenvio na mesma posição mudou depois do reinício")
	}
	if out, _ := m2.NewBatch().Mask(old, false, posB); strings.Contains(out, s) {
		t.Errorf("noutra posição, depois do reinício, saiu com a máscara antiga: %q", out)
	}
}
