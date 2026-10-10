package mask

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"
)

// Valores de referência conhecidos (independentes do código).
func TestValidadoresReferencia(t *testing.T) {
	casos := []struct {
		f     func(string) bool
		v     string
		valid bool
	}{
		{CPFValido, "529.982.247-25", true}, {CPFValido, "529.982.247-26", false}, {CPFValido, "111.111.111-11", false},
		{CNPJValido, "11.222.333/0001-81", true}, {CNPJValido, "11.222.333/0001-82", false},
		{LuhnValido, "4111 1111 1111 1111", true}, {LuhnValido, "4111 1111 1111 1112", false},
		{LuhnValido, "5555555555554444", true}, {LuhnValido, "378282246310005", true},
	}
	for _, c := range casos {
		if c.f(c.v) != c.valid {
			t.Errorf("%s: esperado %v", c.v, c.valid)
		}
	}
}

// Vetores gerados por uma biblioteca externa (validate-docbr). Rode com
// LLM_DLP_VETORES=arquivo (linhas "tipo valido|invalido numero").
func TestValidadoresBibliotecaExterna(t *testing.T) {
	arq := os.Getenv("LLM_DLP_VETORES")
	if arq == "" {
		t.Skip("defina LLM_DLP_VETORES para conferir contra a validate-docbr")
	}
	f, err := os.Open(arq)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fs := map[string]func(string) bool{"cpf": CPFValido, "cnpj": CNPJValido, "pis": PISValido, "cnh": CNHValida}
	erros := map[string]int{}
	total := map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Fields(sc.Text())
		if len(p) != 3 {
			continue
		}
		total[p[0]]++
		if fs[p[0]](p[2]) != (p[1] == "valido") {
			erros[p[0]]++
		}
	}
	for tipo, n := range total {
		t.Logf("%-5s %d vetores, %d divergências", tipo, n, erros[tipo])
		if erros[tipo] > 0 {
			t.Errorf("%s diverge da biblioteca externa em %d de %d", tipo, erros[tipo], n)
		}
	}
}

func TestValidadoresExtras(t *testing.T) {
	// exemplos públicos de documentação
	if !IBANValido("GB82 WEST 1234 5698 7654 32") || !IBANValido("BR1500000000000010932840814P2") || IBANValido("GB82 WEST 1234 5698 7654 33") {
		t.Error("IBAN")
	}
	if !CNPJAlfaValido("12.ABC.345/01DE-35") || CNPJAlfaValido("12.ABC.345/01DE-36") || CNPJAlfaValido("11.222.333/0001-81") {
		t.Error("CNPJ alfanumérico (o numérico é do CNPJValido)")
	}
	tit := gerarValido("1234567801", 12, TituloValido)
	cns := gerarValido("7000000000000", 15, CNSValido)
	ren := gerarValido("6392748451", 11, RenavamValido)
	if tit == "" || cns == "" || ren == "" {
		t.Fatalf("não gerou: título %q cns %q renavam %q", tit, cns, ren)
	}
	if TituloValido("123456780199") && TituloValido("123456780198") && TituloValido("123456780197") {
		t.Error("título aceita qualquer coisa")
	}
}

// Regra oficial além do dígito verificador: o número de ordem do CNPJ começa em 0001.
func TestCNPJOrdemZero(t *testing.T) {
	for dv := 0; dv < 100; dv++ {
		c := fmt.Sprintf("123456780000%02d", dv)
		if CNPJValido(c) {
			t.Fatalf("CNPJ com ordem 0000 aceito: %s", c)
		}
	}
	if !CNPJValido("11.222.333/0001-81") {
		t.Fatal("CNPJ real de exemplo (ordem 0001) recusado")
	}
}
