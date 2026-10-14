package mask

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Check-digit validators (CPF, CNPJ, PIS, CNH, card, voter ID, CNS, RENAVAM, IBAN).

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
	// the order number (positions 9 to 12) starts at 0001, the head office: 0000 is not a real CNPJ
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

// ValidPIS validates PIS/PASEP/NIS (11 digits).
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

// ValidCNH validates the CNH registration number (11 digits).
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
	// same algorithm as validate-docbr: remainder minus the discount, corrected to 0..10
	dv2 := sum%11 - desc
	if dv2 < 0 {
		dv2 += 11
	}
	if dv2 >= 10 {
		dv2 = 0
	}
	return dv1 == d[9] && dv2 == d[10]
}

// ValidLuhn validates card numbers (13 to 19 digits).
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

// NormName: uppercase, unaccented, collapsed spaces ("João da Silva" -> "JOAO DA SILVA").
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

// Documents and identifiers beyond the basic ones. The validation rules follow the same sources as
// the others (validate-docbr for the Brazilian documents; ISO 13616 for IBAN; Luhn for IMEI). The
// list of types came from what market tools cover (Amazon Macie, Microsoft Presidio, Google Cloud
// DLP) and we did not.

// ValidVoterID: voter ID (12 digits, the last two are check digits).
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

// ValidCNS: National Health Card (15 digits; weighted sum multiple of 11).
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

// ValidRenavam: 11 digits, the last one is the check digit.
func ValidRenavam(s string) bool {
	t := digitsOnly(s)
	for len(t) < 11 && len(t) >= 9 {
		t = "0" + t // old records have 9 or 10 digits
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

// ValidAlphaCNPJ: CNPJ in the new format, with letters in the first 12 positions (in force from
// July 2026). The calculation is the same as the numeric CNPJ, with each character worth its ASCII
// code minus 48.
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

// ValidIBAN: international bank account (2 country letters, 2 check digits, up to 30 letters or
// digits; mod 97 = 1).
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
