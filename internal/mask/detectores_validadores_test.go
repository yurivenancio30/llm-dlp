package mask

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"
)

// Values de referência conhecidos (independentes do código).
func TestValidatorsReference(t *testing.T) {
	cases := []struct {
		f     func(string) bool
		v     string
		valid bool
	}{
		{ValidCPF, "529.982.247-25", true}, {ValidCPF, "529.982.247-26", false}, {ValidCPF, "111.111.111-11", false},
		{ValidCNPJ, "11.222.333/0001-81", true}, {ValidCNPJ, "11.222.333/0001-82", false},
		{ValidLuhn, "4111 1111 1111 1111", true}, {ValidLuhn, "4111 1111 1111 1112", false},
		{ValidLuhn, "5555555555554444", true}, {ValidLuhn, "378282246310005", true},
	}
	for _, c := range cases {
		if c.f(c.v) != c.valid {
			t.Errorf("%s: esperado %v", c.v, c.valid)
		}
	}
}

// Vetores gerados por uma biblioteca externa (validate-docbr). Rode com
// LLM_DLP_VETORES=arquivo (linhas "tipo valido|invalido numero").
func TestValidatorsExternalLibrary(t *testing.T) {
	file := os.Getenv("LLM_DLP_VETORES")
	if file == "" {
		t.Skip("defina LLM_DLP_VETORES para conferir contra a validate-docbr")
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fs := map[string]func(string) bool{"cpf": ValidCPF, "cnpj": ValidCNPJ, "pis": ValidPIS, "cnh": ValidCNH}
	errs := map[string]int{}
	total := map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Fields(sc.Text())
		if len(p) != 3 {
			continue
		}
		total[p[0]]++
		if fs[p[0]](p[2]) != (p[1] == "valido") {
			errs[p[0]]++
		}
	}
	for typ, n := range total {
		t.Logf("%-5s %d vetores, %d divergências", typ, n, errs[typ])
		if errs[typ] > 0 {
			t.Errorf("%s diverge da biblioteca externa em %d de %d", typ, errs[typ], n)
		}
	}
}

func TestValidatorsExtra(t *testing.T) {
	// exemplos públicos de documentação
	if !ValidIBAN("GB82 WEST 1234 5698 7654 32") || !ValidIBAN("BR1500000000000010932840814P2") || ValidIBAN("GB82 WEST 1234 5698 7654 33") {
		t.Error("IBAN")
	}
	if !ValidAlphaCNPJ("12.ABC.345/01DE-35") || ValidAlphaCNPJ("12.ABC.345/01DE-36") || ValidAlphaCNPJ("11.222.333/0001-81") {
		t.Error("CNPJ alfanumérico (o numérico é do CNPJValido)")
	}
	ttl := genValid("1234567801", 12, ValidVoterID)
	cns := genValid("7000000000000", 15, ValidCNS)
	ren := genValid("6392748451", 11, ValidRenavam)
	if ttl == "" || cns == "" || ren == "" {
		t.Fatalf("não gerou: título %q cns %q renavam %q", ttl, cns, ren)
	}
	if ValidVoterID("123456780199") && ValidVoterID("123456780198") && ValidVoterID("123456780197") {
		t.Error("título aceita qualquer coisa")
	}
}

// Rule oficial além do dígito verificador: o número de ordem do CNPJ começa em 0001.
func TestCNPJZeroOrder(t *testing.T) {
	for dv := 0; dv < 100; dv++ {
		c := fmt.Sprintf("123456780000%02d", dv)
		if ValidCNPJ(c) {
			t.Fatalf("CNPJ com ordem 0000 aceito: %s", c)
		}
	}
	if !ValidCNPJ("11.222.333/0001-81") {
		t.Fatal("CNPJ real de exemplo (ordem 0001) recusado")
	}
}
