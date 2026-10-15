package mask

import (
	"crypto/sha256"
	"strings"
	"testing"
)

// bip39Phrase builds the recovery phrase of an entropy in one of the lists (BIP-39: entropy plus
// the first len/32 bits of its SHA-256, in groups of 11 bits).
func bip39Phrase(t *testing.T, entropy []byte, list map[string]int) string {
	t.Helper()
	words := make([]string, len(list))
	for w, i := range list {
		words[i] = w
	}
	h := sha256.Sum256(entropy)
	var bits []byte
	for _, b := range entropy {
		for i := 7; i >= 0; i-- {
			bits = append(bits, b>>i&1)
		}
	}
	for i := 0; i < len(entropy)*8/32; i++ {
		bits = append(bits, h[i/8]>>(7-i%8)&1)
	}
	var out []string
	for i := 0; i < len(bits); i += 11 {
		v := 0
		for _, b := range bits[i : i+11] {
			v = v<<1 | int(b)
		}
		out = append(out, words[v])
	}
	return strings.Join(out, " ")
}

func TestBIP39Lists(t *testing.T) {
	for i, l := range bip39Lists {
		if len(l) != 2048 {
			t.Fatalf("list %d: %d words, want 2048", i, len(l))
		}
		for w := range l {
			if len(w) < 3 || len(w) > 8 {
				t.Errorf("list %d: word %q outside 3 to 8 letters (findRecoveryPhrase skips it)", i, w)
			}
		}
	}
}

func TestAuthData(t *testing.T) {
	// BIP-39 test vectors (bitcoin/bips, bip-0039, "Test vectors"), and a Portuguese phrase built here
	vectorEN12 := "legal winner thank year wave sausage worth useful legal winner thank yellow"
	vectorEN24 := "letter advice cage absurd amount doctor acoustic avoid letter advice cage absurd amount doctor acoustic avoid letter advice cage absurd amount doctor acoustic bless"
	phrasePT := bip39Phrase(t, []byte("dezesseis bytes!"), bip39Lists[1])
	takes := map[string]string{
		"senha: 4821":              "4821",
		"senha do cartão: 4821":    "4821",
		"a senha é 4821":           "4821",
		"SENHA_BANCO=4821":         "4821",
		"pwd: 48213":               "48213",
		"PIN: 4821":                "4821",
		"pin do cartão 4821":       "4821",
		"my PIN is 4821":           "4821",
		"CVV: 123":                 "123",
		"cvv 123":                  "123",
		"CVC2=0456":                "0456",
		"código de segurança: 123": "123",
		"código de segurança enviado por SMS: 482913": "482913",
		"código de verificação: 482913":               "482913",
		"seu código de acesso é 482913":               "482913",
		"verification code: 48291375":                 "48291375",
		"OTP: 482913":                                 "482913",
		"2FA: 482913":                                 "482913",
		"token: 482913":                               "482913",
		"token SMS 482913":                            "482913",
		"recovery codes: 9f3k-2m8x 7p1q-4z6w":         "7p1q-4z6w",
		"códigos de backup: 9f3k2m8x, 7p1q4z6w":       "9f3k2m8x",
		"resposta secreta: Rex":                       "Rex",
		"security answer: Fluffy the cat.":            "Fluffy the cat",
		"frase de recuperação: gato mesa sol":         "gato mesa sol",
		"seed phrase = " + vectorEN12:                 "sausage",
		// by the data itself, without a label
		vectorEN12:                       "sausage",
		"minha carteira:\n" + vectorEN24: "bless",
		"1. legal 2. winner 3. thank 4. year 5. wave 6. sausage 7. worth 8. useful 9. legal 10. winner 11. thank 12. yellow": "sausage",
		"anotei: " + phrasePT + ".": strings.Fields(phrasePT)[5],
	}
	for text, real := range takes {
		if out, _ := newTest(t).Mask(text); strings.Contains(out, real) {
			t.Errorf("authentication data went through: %q -> %q", text, out)
		}
	}
	// the value after a label ends at the end of the sentence, a ";" or a "|": what comes next stays
	for _, c := range []struct{ text, gone, keep string }{
		{"frase de recuperação: gato mesa sol; SELECT id FROM t", "mesa", "; SELECT id FROM"},
		{"resposta secreta: Rex. Depois rodei o deploy", "Rex", ". Depois rodei o deploy"},
		{"seed phrase: gato mesa sol | status ok", "mesa", "| status ok"},
		{"recovery codes: 9f3k-2m8x; build ok", "9f3k", "; build ok"},
	} {
		if out, _ := newTest(t).Mask(c.text); strings.Contains(out, c.gone) || !strings.Contains(out, c.keep) {
			t.Errorf("value after the label cut wrong: %q -> %q", c.text, out)
		}
	}
	clean := []string{
		"password_min_length: 8",
		"PASSWORD_MAX_LENGTH=1024",
		"senha_tentativas: 3",
		"password_expiry_days: 9999",
		"max_tokens: 4096",
		"tokens: 123456",
		"input tokens: 1234",
		"pin 13 do GPIO",
		"PIN de 4 dígitos",
		"a senha de 2024 expirou",
		"CID 1234 do pedido",
		"o CVV fica no verso do cartão",
		"a resposta secreta deve ser fácil de lembrar",
		"guarde a frase de recuperação num lugar seguro",
		"the code is at line 4821",
		// 12 words of the list in a row, but the checksum does not match: only a label would catch it
		"abandon ability able about above absent absorb abstract absurd abuse access accident",
	}
	for _, text := range clean {
		if out, ents := newTest(t).Mask(text); len(ents) > 0 {
			t.Errorf("false positive: %q -> %q", text, out)
		}
	}
	// the list sentence above must really fail the checksum, or the case says nothing
	var idx []int
	for _, w := range strings.Fields(clean[len(clean)-1]) {
		idx = append(idx, bip39Lists[0][w])
	}
	if bip39Checksum(idx) {
		t.Fatal("the negative phrase has a valid checksum: pick other words")
	}
	if !bip39Checksum(func() []int {
		var v []int
		for _, w := range strings.Fields(vectorEN12) {
			v = append(v, bip39Lists[0][w])
		}
		return v
	}()) {
		t.Fatal("official test vector failed the checksum")
	}
}
