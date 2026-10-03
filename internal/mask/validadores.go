package mask

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Validadores de dígito verificador (CPF, CNPJ, PIS, CNH, cartão, título, CNS, RENAVAM, IBAN).

func soDigitos(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func digitos(s string) []int {
	d := make([]int, 0, len(s))
	for _, r := range s {
		d = append(d, int(r-'0'))
	}
	return d
}

func todosIguais(d []int) bool {
	for _, x := range d[1:] {
		if x != d[0] {
			return false
		}
	}
	return true
}

func CPFValido(s string) bool {
	d := digitos(soDigitos(s))
	if len(d) != 11 || todosIguais(d) {
		return false
	}
	for _, i := range []int{9, 10} {
		soma := 0
		for k := 0; k < i; k++ {
			soma += d[k] * (i + 1 - k)
		}
		if (soma*10%11)%10 != d[i] {
			return false
		}
	}
	return true
}

func CNPJValido(s string) bool {
	d := digitos(soDigitos(s))
	if len(d) != 14 || todosIguais(d) {
		return false
	}
	pesos := [][]int{{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}, {6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}}
	for j, n := range []int{12, 13} {
		soma := 0
		for k := 0; k < n; k++ {
			soma += d[k] * pesos[j][k]
		}
		r := soma % 11
		dv := 0
		if r >= 2 {
			dv = 11 - r
		}
		if dv != d[n] {
			return false
		}
	}
	return true
}

// PISValido valida PIS/PASEP/NIS (11 dígitos).
func PISValido(s string) bool {
	d := digitos(soDigitos(s))
	if len(d) != 11 || todosIguais(d) {
		return false
	}
	pesos := []int{3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	soma := 0
	for k, p := range pesos {
		soma += d[k] * p
	}
	r := 11 - soma%11
	if r >= 10 {
		r = 0
	}
	return r == d[10]
}

// CNHValida valida o número de registro da CNH (11 dígitos).
func CNHValida(s string) bool {
	d := digitos(soDigitos(s))
	if len(d) != 11 || todosIguais(d) {
		return false
	}
	soma, desc := 0, 0
	for k := 0; k < 9; k++ {
		soma += d[k] * (9 - k)
	}
	dv1 := soma % 11
	if dv1 >= 10 {
		dv1, desc = 0, 2
	}
	soma = 0
	for k := 0; k < 9; k++ {
		soma += d[k] * (1 + k)
	}
	// mesmo algoritmo da validate-docbr: resto menos o desconto, corrigido para 0..10
	dv2 := soma%11 - desc
	if dv2 < 0 {
		dv2 += 11
	}
	if dv2 >= 10 {
		dv2 = 0
	}
	return dv1 == d[9] && dv2 == d[10]
}

// LuhnValido valida números de cartão (13 a 19 dígitos).
func LuhnValido(s string) bool {
	d := digitos(soDigitos(s))
	if len(d) < 13 || len(d) > 19 || todosIguais(d) {
		return false
	}
	soma := 0
	for i := len(d) - 1; i >= 0; i-- {
		x := d[i]
		if (len(d)-1-i)%2 == 1 {
			x *= 2
			if x > 9 {
				x -= 9
			}
		}
		soma += x
	}
	return soma%10 == 0
}

// NormNome: maiúsculas, sem acento, espaços colapsados ("João  da Silva" -> "JOAO DA SILVA").
func NormNome(s string) string {
	t := norm.NFD.String(s)
	var b strings.Builder
	espaco := false
	for _, r := range t {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsSpace(r) {
			espaco = true
			continue
		}
		if espaco && b.Len() > 0 {
			b.WriteByte(' ')
		}
		espaco = false
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}

// Documentos e identificadores além dos básicos. As regras de validação seguem as mesmas
// fontes dos demais (validate-docbr para os documentos brasileiros; ISO 13616 para IBAN;
// Luhn para IMEI). A lista de tipos veio do que as ferramentas de mercado cobrem (Amazon
// Macie, Microsoft Presidio, Google Cloud DLP) e nós não cobríamos.

// TituloValido: título de eleitor (12 dígitos, os dois últimos verificadores).
func TituloValido(s string) bool {
	d := digitos(soDigitos(s))
	if len(d) != 12 || todosIguais(d) {
		return false
	}
	uf := d[8]*10 + d[9]
	if uf < 1 || uf > 28 {
		return false
	}
	soma := 0
	for i := 0; i < 8; i++ {
		soma += d[i] * (i + 2)
	}
	dv1 := soma % 11
	if dv1 == 10 {
		dv1 = 0
	} else if dv1 == 0 && (uf == 1 || uf == 2) {
		dv1 = 1
	}
	dv2 := (d[8]*7 + d[9]*8 + dv1*9) % 11
	if dv2 == 10 {
		dv2 = 0
	} else if dv2 == 0 && (uf == 1 || uf == 2) {
		dv2 = 1
	}
	return d[10] == dv1 && d[11] == dv2
}

// CNSValido: Cartão Nacional de Saúde (15 dígitos; soma ponderada múltipla de 11).
func CNSValido(s string) bool {
	d := digitos(soDigitos(s))
	if len(d) != 15 || todosIguais(d) || !(d[0] == 1 || d[0] == 2 || d[0] >= 7) {
		return false
	}
	soma := 0
	for i, v := range d {
		soma += v * (15 - i)
	}
	return soma%11 == 0
}

// RenavamValido: 11 dígitos, o último verificador.
func RenavamValido(s string) bool {
	t := soDigitos(s)
	for len(t) < 11 && len(t) >= 9 {
		t = "0" + t // registros antigos têm 9 ou 10 dígitos
	}
	d := digitos(t)
	if len(d) != 11 || todosIguais(d) {
		return false
	}
	pesos := []int{3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	soma := 0
	for i, p := range pesos {
		soma += d[i] * p
	}
	dv := soma * 10 % 11
	if dv == 10 {
		dv = 0
	}
	return dv == d[10]
}

// CNPJAlfaValido: CNPJ no formato novo, com letras nas 12 primeiras posições (vigente a
// partir de julho de 2026). A conta é a mesma do CNPJ numérico, com cada caractere valendo
// o seu código ASCII menos 48.
func CNPJAlfaValido(s string) bool {
	var v []int
	letras := false
	for _, c := range strings.ToUpper(s) {
		switch {
		case c >= '0' && c <= '9':
			v = append(v, int(c-'0'))
		case c >= 'A' && c <= 'Z':
			v = append(v, int(c-'0'))
			letras = true
		case c == '.' || c == '/' || c == '-':
		default:
			return false
		}
	}
	if len(v) != 14 || !letras || v[12] > 9 || v[13] > 9 {
		return false
	}
	pesos := [][]int{{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}, {6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}}
	for j, n := range []int{12, 13} {
		soma := 0
		for k := 0; k < n; k++ {
			soma += v[k] * pesos[j][k]
		}
		dv := 0
		if r := soma % 11; r >= 2 {
			dv = 11 - r
		}
		if dv != v[n] {
			return false
		}
	}
	return true
}

// IBANValido: conta bancária internacional (2 letras do país, 2 verificadores, até 30
// letras ou dígitos; mod 97 = 1).
func IBANValido(s string) bool {
	s = strings.ToUpper(strings.ReplaceAll(s, " ", ""))
	if len(s) < 15 || len(s) > 34 {
		return false
	}
	resto := 0
	for _, c := range s[4:] + s[:4] {
		switch {
		case c >= '0' && c <= '9':
			resto = (resto*10 + int(c-'0')) % 97
		case c >= 'A' && c <= 'Z':
			resto = (resto*100 + int(c-'A') + 10) % 97
		default:
			return false
		}
	}
	return resto == 1
}
