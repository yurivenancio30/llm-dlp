package mask

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Tests of the examination of large text in pieces: each datum in every cut position.

// Large text is examined in pieces (see detectLarge). These tests guarantee that cutting lets
// nothing through: each data type is placed in EVERY position relative to the cuts, and the result
// has to be identical to that of examining the whole text.

// testSizes shrinks the pieces so the test can sweep every position quickly.
func testSizes(t *testing.T, min, piece, margin int) {
	a, b, c := largeMin, largePiece, largeMargin
	largeMin, largePiece, largeMargin = min, piece, margin
	t.Cleanup(func() { largeMin, largePiece, largeMargin = a, b, c })
}

type cutCase struct{ text, real string }

func cutCases() []cutCase {
	cpf := genCPF("529982247")
	cpfWithout := strings.NewReplacer(".", "", "-", "").Replace(cpf)
	pis := genEleven("1201234567", ValidPIS)
	cnh := genEleven("123456789", ValidCNH)
	shortPEM := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA7bq2Xk9mQ2vLp7wZt4nBc6HfG1aE5uQiOk3Jd9sLq2mXv8Rt\nQ2vLp7wZt4nBc6HfG1aE5uQiOk3Jd9sLq2mXv8RtMIIEpAIBAAKCAQEA7bq2Xk9m\n-----END RSA PRIVATE KEY-----"
	longPEM := "-----BEGIN RSA PRIVATE KEY-----\n" + strings.Repeat("MIIEpAIBAAKCAQEA7bq2Xk9mQ2vLp7wZt4nBc6HfG1aE5uQiOk3Jd9sLq2mXv8Rt\n", 9) + "-----END RSA PRIVATE KEY-----"
	return []cutCase{
		{"owner: joao.silva@empresa-ficticia.com.br ok", "joao.silva@empresa-ficticia.com.br"},
		{"conectando em 10.42.7.15:3306", "10.42.7.15"},
		{"host mysql-dh.empresa-ficticia.intra caiu", "mysql-dh.empresa-ficticia.intra"},
		{"cliente " + cpf + " ativo", cpf},
		{"cpf: " + cpfWithout, cpfWithout},
		{"empresa 11.222.333/0001-81 ok", "11.222.333/0001-81"},
		{"ligue (11) 98765-4321 amanhã", "98765-4321"},
		{"whatsapp: +55 11 98765-4321 ok", "98765-4321"},
		{"CEP 01310-100 ok", "01310-100"},
		{"cartão 4111 1111 1111 1111 ok", "4111 1111 1111 1111"},
		{"RG: 12.345.678-9 ok", "12.345.678-9"},
		{"PIS " + pis + " ok", pis},
		{"CNH " + cnh + " ok", cnh},
		{"agência 1234-5 conta 123456-7 ok", "123456-7"},
		{"chave pix 123e4567-e89b-42d3-a456-426614174000 ok", "123e4567-e89b-42d3-a456-426614174000"},
		{"mora na Rua das Flores, 123 - centro", "Rua das Flores, 123"},
		{"data de nascimento: 12/03/1985 ok", "12/03/1985"},
		{"o owner é JOAO CARLOS SILVA hoje", "JOAO CARLOS SILVA"},
		{"steward Ana Paula Souza ok", "Ana Paula Souza"},
		{"usuario MAT123456 travou", "MAT123456"},
		{"o Projeto Fenix atrasou", "Projeto Fenix"},
		{"token=ghp_k3Jd9sLq2mXv8RtY7wPz4nBc6HfG1aE5uQiO ok", "ghp_k3Jd9sLq2mXv8RtY7wPz4nBc6HfG1aE5uQiO"},
		{"DB_PASSWORD=Ficticia@2024 ok", "Ficticia@2024"},
		{"a senha do banco é Ficticia@2024 ok", "Ficticia@2024"},
		{"senha: Ficticia@2024 ok", "Ficticia@2024"},
		{`"password": "admin123" ok`, "admin123"},
		{"mysql -u root -pFicticia@2024 -h db1", "Ficticia@2024"},
		{"mysql://app:Ficticia2024x@db1:3306/base ok", "Ficticia2024x"},
		{"sshpass -p 'Ficticia@2024' ssh x", "Ficticia@2024"},
		{"clientSecret: aB3~xY9.kL2-mN8qR5tU1wZ4 ok", "aB3~xY9.kL2-mN8qR5tU1wZ4"},
		{"segredo em base64: " + "cGFzc3dvcmQ6IGdocF9rM0pkOXNMcTJtWHY4UnRZN3dQejRuQmM2SGZHMWFFNXVRaU8=" + " ok", "cGFzc3dvcmQ6IGdocF9rM0pkOXNMcTJtWHY4UnRZN3dQejRuQmM2SGZHMWFFNXVRaU8="},
		{shortPEM, "MIIEpAIBAAKCAQEA7bq2Xk9mQ2vLp7wZt4nBc6HfG1aE5uQiOk3Jd9sLq2mXv8Rt"},
		{longPEM, "MIIEpAIBAAKCAQEA7bq2Xk9mQ2vLp7wZt4nBc6HfG1aE5uQiOk3Jd9sLq2mXv8Rt"},
	}
}

func (m *Masker) resetMemory() {
	m.known = newKnown()
	m.mu.Lock()
	m.memo, m.stale, m.bytes = map[[32]byte]result{}, nil, 0
	m.mu.Unlock()
}

// compares the examination in pieces with that of the whole text; returns "" if equal. With
// require, the real value also cannot appear in the result.
func compareCut(m *Masker, s, real string, require bool) string {
	m.resetMemory()
	wants, _ := m.apply(s, m.detectWhole(s))
	m.resetMemory()
	got, _ := m.apply(s, m.Detect(s))
	if strings.Contains(got, real) && (require || !strings.Contains(wants, real)) {
		return "VAZOU " + real
	}
	if got != wants {
		i := 0
		for i < len(got) && i < len(wants) && got[i] == wants[i] {
			i++
		}
		a, b := max(0, i-50), min(len(wants), i+70)
		return fmt.Sprintf("differs at %d\n whole:  %q\n pieces: %q", i, wants[a:b], got[a:min(len(got), b)])
	}
	return ""
}

func scanCuts(t *testing.T, cases []cutCase, require bool, step, total int, build func(c cutCase, padding string, pos int) string) {
	paddings := []string{"linha comum de log, tudo ok\n", "palavra solta ", "x"}
	type task struct {
		c   cutCase
		e   string
		pos int
	}
	queue := make(chan task, 256)
	var errs atomic.Int64
	var n atomic.Int64
	var wg sync.WaitGroup
	// the Maskers are created one at a time: the creation of the gitleaks detector cannot be done
	// in parallel (its use, once created, can)
	ms := make([]*Masker, runtime.GOMAXPROCS(0))
	for i := range ms {
		ms[i] = newTest(t)
	}
	for _, m := range ms {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tf := range queue {
				n.Add(1)
				if msg := compareCut(m, build(tf.c, tf.e, tf.pos), tf.c.real, require); msg != "" && errs.Add(1) <= 12 {
					t.Errorf("case %.40q, filler %q, position %d: %s", tf.c.text, tf.e, tf.pos, msg)
				}
			}
		}()
	}
	for _, c := range cases {
		for _, e := range paddings {
			for pos := 0; pos < total; pos += step {
				queue <- task{c, e, pos}
			}
		}
	}
	close(queue)
	wg.Wait()
	t.Logf("%d texts compared, %d differences", n.Load(), errs.Load())
}

// sep: what separates the datum from the padding. With padding without line breaks the separator is
// a space: this way the cut (which prefers line breaks) falls at a fixed position and the datum
// slides over it. With a line break, it tests the cut that looks for the break.
func sep(e string) string {
	if strings.Contains(e, "\n") {
		return "\n"
	}
	return " "
}

func fill(e string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(e, n/len(e)+1)[:n]
}

// The sensitive datum slides byte by byte over two whole cuts.
func TestCutsAllPositions(t *testing.T) {
	testSizes(t, 1000, 600, 200)
	step := 31 // sample, so the normal suite is fast; with LLM_DLP_STRESS=1, byte by byte
	if os.Getenv("LLM_DLP_STRESS") != "" {
		step = 1
	}
	scanCuts(t, cutCases(), true, step, 1500, func(c cutCase, e string, pos int) string {
		return fill(e, pos) + sep(e) + c.text + sep(e) + fill(e, 2400-pos)
	})
}

// The value is taught (with the keyword) at one end of the text and appears loose in every
// position: it has to be masked in both places, as in the examination of the whole text.
func TestCutsLooseKnownValue(t *testing.T) {
	testSizes(t, 1000, 600, 200)
	step := 53
	if os.Getenv("LLM_DLP_STRESS") != "" {
		step = 3
	}
	// values that are remembered (those that depend on the word next to them), in the learned
	// spelling
	cpfWithout := strings.NewReplacer(".", "", "-", "").Replace(genCPF("529982247"))
	cases := []cutCase{
		{"RG: 12.345.678-9 ok", "12.345.678-9"},
		{"cpf: " + cpfWithout + " ok", cpfWithout},
		{"CEP 01310-100 ok", "01310-100"},
		{"ligue (11) 98765-4321 amanhã", "(11) 98765-4321"},
		{"chave pix 123e4567-e89b-42d3-a456-426614174000 ok", "123e4567-e89b-42d3-a456-426614174000"},
		{"data de nascimento: 12/03/1985 ok", "12/03/1985"},
		{"token=ghp_k3Jd9sLq2mXv8RtY7wPz4nBc6HfG1aE5uQiO ok", "ghp_k3Jd9sLq2mXv8RtY7wPz4nBc6HfG1aE5uQiO"},
		{"DB_PASSWORD=Ficticia@2024 ok", "Ficticia@2024"},
		{"a senha do banco é Ficticia@2024 ok", "Ficticia@2024"},
		{"mysql://app:Ficticia2024x@db1:3306/base ok", "Ficticia2024x"},
		{"clientSecret: aB3~xY9.kL2-mN8qR5tU1wZ4 ok", "aB3~xY9.kL2-mN8qR5tU1wZ4"},
	}
	for _, teachesAtEnd := range []bool{false, true} {
		scanCuts(t, cases, true, step, 1500, func(c cutCase, e string, pos int) string {
			looseOne := fill(e, pos) + " " + c.real + " " + fill(e, 2400-pos)
			if teachesAtEnd {
				return looseOne + sep(e) + c.text + sep(e)
			}
			return c.text + sep(e) + looseOne
		})
	}
}

// With the real sizes (48 KB pieces, 8 KB margin), by sampling positions.
func TestCutsRealSize(t *testing.T) {
	if os.Getenv("LLM_DLP_STRESS") == "" {
		t.Skip("slow (minutes): set LLM_DLP_STRESS=1 to run")
	}
	step := 211
	key4096 := "-----BEGIN RSA PRIVATE KEY-----\n" + strings.Repeat("MIIEpAIBAAKCAQEA7bq2Xk9mQ2vLp7wZt4nBc6HfG1aE5uQiOk3Jd9sLq2mXv8Rt\n", 50) + "-----END RSA PRIVATE KEY-----"
	key16k := "-----BEGIN RSA PRIVATE KEY-----\n" + strings.Repeat("MIIEpAIBAAKCAQEA7bq2Xk9mQ2vLp7wZt4nBc6HfG1aE5uQiOk3Jd9sLq2mXv8Rt\n", 200) + "-----END RSA PRIVATE KEY-----"
	cases := []cutCase{
		{key4096, "MIIEpAIBAAKCAQEA7bq2Xk9mQ2vLp7wZt4nBc6HfG1aE5uQiOk3Jd9sLq2mXv8Rt"},
		{key16k, "MIIEpAIBAAKCAQEA7bq2Xk9mQ2vLp7wZt4nBc6HfG1aE5uQiOk3Jd9sLq2mXv8Rt"}, // larger than the margin
		{"DB_PASSWORD=Ficticia@2024 ok", "Ficticia@2024"},
		{"RG: 12.345.678-9 ok", "12.345.678-9"},
		{"password=" + strings.Repeat("Zq9!", 5000) + " ok", strings.Repeat("Zq9!", 50)}, // a 20 KB value: larger than the margin
		{"password=" + strings.Repeat("Zq9a", 5000) + " ok", strings.Repeat("Zq9a", 50)},
		{"token=" + strings.Repeat("ghp_k3Jd9sLq2mXv8RtY7wPz4nBc6HfG1aE5uQiO.", 400) + " ok", "ghp_k3Jd9sLq2mXv8RtY7wPz4nBc6HfG1aE5uQiO"},
		{"mysql://app:" + strings.Repeat("Zq9!", 5000) + "@db1:3306/base ok", strings.Repeat("Zq9!", 50)},
	}
	m := newTest(t)
	n := 0
	for _, c := range cases {
		for _, e := range []string{"linha comum de log, tudo ok\n", "x"} {
			for pos := largePiece - len(c.text) - 300; pos < largePiece+600; pos += step {
				if pos < 0 {
					continue
				}
				s := fill(e, pos) + sep(e) + c.text + sep(e) + fill(e, largeMin+largePiece-pos)
				n++
				if msg := compareCut(m, s, c.real, true); msg != "" {
					t.Fatalf("case %.30q, filler %q, position %d: %s", c.text, e, pos, msg)
				}
			}
		}
	}
	t.Logf("%d texts compared", n)
}

// The test above only counts if it catches a badly made cut. Without a margin, it has to report a
// difference.
func TestCutsWithoutMarginFail(t *testing.T) {
	testSizes(t, 1000, 600, 0)
	m := newTest(t)
	dif := 0
	for _, c := range cutCases()[:26] {
		for pos := 540; pos < 610; pos += 2 {
			s := fill("x", pos) + " " + c.text + " " + fill("x", 2400-pos)
			if compareCut(m, s, c.real, true) != "" {
				dif++
			}
		}
	}
	t.Logf("without margin: %d texts with a leak or a difference", dif)
	if dif == 0 {
		t.Fatal("the cuts test does not detect a cut without margin")
	}
}

// Large text is examined in parallel pieces. The result has to be the same as that of examining the
// whole text at once, including for what sits on top of a cut.
func TestLargeEqualsWhole(t *testing.T) {
	whole := func(m *Masker, s string) string {
		txt, _ := m.apply(s, m.detectWhole(s))
		return txt
	}
	pem := "-----BEGIN RSA PRIVATE KEY-----\n" + strings.Repeat("MIIEpAIBAAKCAQEA7bq2Xk9mQ2vLp7wZt4nBc6HfG1aE5uQiOk3Jd9sLq2mXv8Rt\n", 48) + "-----END RSA PRIVATE KEY-----\n"
	cases := map[string]func() string{
		"variado": func() string {
			var b strings.Builder
			for i := 0; b.Len() < 600<<10; i++ {
				b.WriteString(variedText(i))
			}
			return b.String()
		},
		"chave privada em cima de cada corte": func() string {
			var b strings.Builder
			for b.Len() < 400<<10 {
				b.WriteString(strings.Repeat("linha comum de log, status=200 ok\n", (largePiece-len(pem)/2-b.Len()%largePiece)/34+1))
				b.WriteString(pem)
			}
			return b.String()
		},
		"valor ensinado no fim, solto no começo": func() string {
			return "erro ao logar com Ficticia@2024 e doc 12.345.678-9\n" + strings.Repeat("linha comum de log, status=200 ok\n", 9000) +
				"DB_PASSWORD=Ficticia@2024\nRG: 12.345.678-9\n"
		},
		"sem quebra de linha, com acentos": func() string {
			var b strings.Builder
			for i := 0; b.Len() < 300<<10; i++ {
				fmt.Fprintf(&b, "ação %d é ótima; contato fulano%d@empresa-ficticia.com.br, cpf %s, senha: Çé%06d!x; ", i, i, genCPF(fmt.Sprintf("%09d", 100000000+i)), i)
			}
			return b.String()
		},
	}
	for name, generates := range cases {
		s := generates()
		if len(s) <= largeMin {
			t.Fatalf("%s: text too small for the test (%d)", name, len(s))
		}
		wants := whole(newTest(t), s)
		got, _ := newTest(t).Mask(s)
		if got != wants {
			i := 0
			for i < len(got) && i < len(wants) && got[i] == wants[i] {
				i++
			}
			a, b := max(0, i-60), min(len(wants), i+60)
			t.Errorf("%s: differs at position %d of %d\n whole:  %q\n pieces: %q", name, i, len(s), wants[a:b], got[a:min(len(got), b)])
		}
		for _, real := range []string{"Ficticia@2024", "12.345.678-9", "MIIEpAIBAAKCAQEA7bq2Xk9mQ2vLp7wZt4nBc6"} {
			if strings.Contains(s, real) && strings.Contains(got, real) {
				t.Errorf("%s: %q got through", name, real)
			}
		}
	}
}
