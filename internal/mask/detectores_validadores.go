package mask

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Validadores de dígito verificador (CPF, CNPJ, PIS, CNH, cartão, título, CNS, RENAVAM, IBAN).

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func digits(s string) []int {
	d := make([]int, 0, len(s))
	for _, r := range s {
		d = append(d, int(r-'0'))
	}
	return d
}

func allEqual(d []int) bool {
	for _, x := range d[1:] {
		if x != d[0] {
			return false
		}
	}
	return true
}

func ValidCPF(s string) bool {
	d := digits(digitsOnly(s))
	if len(d) != 11 || allEqual(d) {
		return false
	}
	for _, i := range []int{9, 10} {
		sum := 0
		for k := 0; k < i; k++ {
			sum += d[k] * (i + 1 - k)
		}
		if (sum*10%11)%10 != d[i] {
			return false
		}
	}
	return true
}

func ValidCNPJ(s string) bool {
	d := digits(digitsOnly(s))
	// o número de ordem (posições 9 a 12) começa em 0001, a matriz: 0000 não é um CNPJ real
	if len(d) != 14 || allEqual(d) || (d[8] == 0 && d[9] == 0 && d[10] == 0 && d[11] == 0) {
		return false
	}
	weights := [][]int{{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}, {6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}}
	for j, n := range []int{12, 13} {
		sum := 0
		for k := 0; k < n; k++ {
			sum += d[k] * weights[j][k]
		}
		r := sum % 11
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

// ValidPIS valida PIS/PASEP/NIS (11 dígitos).
func ValidPIS(s string) bool {
	d := digits(digitsOnly(s))
	if len(d) != 11 || allEqual(d) {
		return false
	}
	weights := []int{3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	sum := 0
	for k, p := range weights {
		sum += d[k] * p
	}
	r := 11 - sum%11
	if r >= 10 {
		r = 0
	}
	return r == d[10]
}

// ValidCNH valida o número de registro da CNH (11 dígitos).
func ValidCNH(s string) bool {
	d := digits(digitsOnly(s))
	if len(d) != 11 || allEqual(d) {
		return false
	}
	sum, desc := 0, 0
	for k := 0; k < 9; k++ {
		sum += d[k] * (9 - k)
	}
	dv1 := sum % 11
	if dv1 >= 10 {
		dv1, desc = 0, 2
	}
	sum = 0
	for k := 0; k < 9; k++ {
		sum += d[k] * (1 + k)
	}
	// mesmo algoritmo da validate-docbr: resto menos o desconto, corrigido para 0..10
	dv2 := sum%11 - desc
	if dv2 < 0 {
		dv2 += 11
	}
	if dv2 >= 10 {
		dv2 = 0
	}
	return dv1 == d[9] && dv2 == d[10]
}

// ValidLuhn valida números de cartão (13 a 19 dígitos).
func ValidLuhn(s string) bool {
	d := digits(digitsOnly(s))
	if len(d) < 13 || len(d) > 19 || allEqual(d) {
		return false
	}
	sum := 0
	for i := len(d) - 1; i >= 0; i-- {
		x := d[i]
		if (len(d)-1-i)%2 == 1 {
			x *= 2
			if x > 9 {
				x -= 9
			}
		}
		sum += x
	}
	return sum%10 == 0
}

// NormName: maiúsculas, sem acento, espaços colapsados ("João  da Silva" -> "JOAO DA SILVA").
func NormName(s string) string {
	t := norm.NFD.String(s)
	var b strings.Builder
	space := false
	for _, r := range t {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}

// Documentos e identificadores além dos básicos. As regras de validação seguem as mesmas
// fontes dos demais (validate-docbr para os documentos brasileiros; ISO 13616 para IBAN;
// Luhn para IMEI). A lista de tipos veio do que as ferramentas de mercado cobrem (Amazon
// Macie, Microsoft Presidio, Google Cloud DLP) e nós não cobríamos.

// ValidVoterID: título de eleitor (12 dígitos, os dois últimos verificadores).
func ValidVoterID(s string) bool {
	d := digits(digitsOnly(s))
	if len(d) != 12 || allEqual(d) {
		return false
	}
	uf := d[8]*10 + d[9]
	if uf < 1 || uf > 28 {
		return false
	}
	sum := 0
	for i := 0; i < 8; i++ {
		sum += d[i] * (i + 2)
	}
	dv1 := sum % 11
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

// ValidCNS: Cartão Nacional de Saúde (15 dígitos; soma ponderada múltipla de 11).
func ValidCNS(s string) bool {
	d := digits(digitsOnly(s))
	if len(d) != 15 || allEqual(d) || !(d[0] == 1 || d[0] == 2 || d[0] >= 7) {
		return false
	}
	sum := 0
	for i, v := range d {
		sum += v * (15 - i)
	}
	return sum%11 == 0
}

// ValidRenavam: 11 dígitos, o último verificador.
func ValidRenavam(s string) bool {
	t := digitsOnly(s)
	for len(t) < 11 && len(t) >= 9 {
		t = "0" + t // registros antigos têm 9 ou 10 dígitos
	}
	d := digits(t)
	if len(d) != 11 || allEqual(d) {
		return false
	}
	weights := []int{3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	sum := 0
	for i, p := range weights {
		sum += d[i] * p
	}
	dv := sum * 10 % 11
	if dv == 10 {
		dv = 0
	}
	return dv == d[10]
}

// ValidAlphaCNPJ: CNPJ no formato novo, com letras nas 12 primeiras posições (vigente a
// partir de julho de 2026). A conta é a mesma do CNPJ numérico, com cada caractere valendo
// o seu código ASCII menos 48.
func ValidAlphaCNPJ(s string) bool {
	var v []int
	letters := false
	for _, c := range strings.ToUpper(s) {
		switch {
		case c >= '0' && c <= '9':
			v = append(v, int(c-'0'))
		case c >= 'A' && c <= 'Z':
			v = append(v, int(c-'0'))
			letters = true
		case c == '.' || c == '/' || c == '-':
		default:
			return false
		}
	}
	if len(v) != 14 || !letters || v[12] > 9 || v[13] > 9 || (v[8] == 0 && v[9] == 0 && v[10] == 0 && v[11] == 0) {
		return false
	}
	weights := [][]int{{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}, {6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}}
	for j, n := range []int{12, 13} {
		sum := 0
		for k := 0; k < n; k++ {
			sum += v[k] * weights[j][k]
		}
		dv := 0
		if r := sum % 11; r >= 2 {
			dv = 11 - r
		}
		if dv != v[n] {
			return false
		}
	}
	return true
}

// ValidIBAN: conta bancária internacional (2 letras do país, 2 verificadores, até 30
// letras ou dígitos; mod 97 = 1).
func ValidIBAN(s string) bool {
	s = strings.ToUpper(strings.ReplaceAll(s, " ", ""))
	if len(s) < 15 || len(s) > 34 {
		return false
	}
	rest := 0
	for _, c := range s[4:] + s[:4] {
		switch {
		case c >= '0' && c <= '9':
			rest = (rest*10 + int(c-'0')) % 97
		case c >= 'A' && c <= 'Z':
			rest = (rest*100 + int(c-'A') + 10) % 97
		default:
			return false
		}
	}
	return rest == 1
}
