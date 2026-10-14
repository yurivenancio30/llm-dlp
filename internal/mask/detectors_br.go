package mask

import (
	"regexp"
	"strings"
)

// More Brazilian documents checked by the data itself: bank slip and utility bill, postal tracking
// code, civil registry certificate number and ICD-10 code.

// reBoleto: typed line of the collection bank slip, 47 digits in 5 fields (AAABC.CCCCX DDDDD.DDDDDY
// EEEEE.EEEEEZ K UUUUVVVVVVVVVV; FEBRABAN Collection Convention, Annex V, 2.3.4).
var reBoleto = regexp.MustCompile(`\b\d{5}\.?\d{5}\s*\d{5}\.?\d{6}\s*\d{5}\.?\d{6}\s*\d\s*\d{14}\b`)

// reBillPayment: typed line of a bill payment (electricity, water, telephone, tax, GRU): 4 blocks
// of 11 digits, each with its check digit (48 digits; starts with 8, the product).
var reBillPayment = regexp.MustCompile(`\b8\d{10}[-\s]?\d\s*\d{11}[-\s]?\d\s*\d{11}[-\s]?\d\s*\d{11}[-\s]?\d\b`)

// reBarcode: the barcode of both, 44 digits.
var reBarcode = regexp.MustCompile(`\b\d{44}\b`)

// checkDigitMod10: FEBRABAN modulo 10: weights 2 and 1 from right to left, adding the digits of
// each product; check digit = 10 - remainder (remainder 0: check digit 0).
func checkDigitMod10(s string) int {
	sum, p := 0, 2
	for i := len(s) - 1; i >= 0; i-- {
		x := int(s[i]-'0') * p
		sum += x/10 + x%10
		p = 3 - p
	}
	if r := sum % 10; r != 0 {
		return 10 - r
	}
	return 0
}

// sumMod11: weights 2 to 9 from right to left (each character is worth its ASCII code minus 48).
func sumMod11(s string) int {
	sum, p := 0, 2
	for i := len(s) - 1; i >= 0; i-- {
		sum += int(s[i]-'0') * p
		if p++; p > 9 {
			p = 2
		}
	}
	return sum
}

// ValidBoletoBarcode: barcode of the collection bank slip (44 digits). Currency 9 (real) or 0 at
// position 4; the general check digit, at position 5, by modulo 11 over the other 43 (11 -
// remainder; 0 or greater than 9 becomes 1: CAIXA barcode manual, Annex I, which follows FEBRABAN).
func ValidBoletoBarcode(c string) bool {
	if len(c) != 44 || c[3] != '9' && c[3] != '0' || digitsOnly(c) != c {
		return false
	}
	dv := 11 - sumMod11(c[:4]+c[5:])%11
	if dv == 0 || dv > 9 {
		dv = 1
	}
	return int(c[4]-'0') == dv
}

// ValidBoleto: the 47-digit typed line. The first three fields with the modulo 10 check digit, and
// the rebuilt barcode with the right general check digit.
func ValidBoleto(s string) bool {
	d := digitsOnly(s)
	if len(d) != 47 {
		return false
	}
	for _, c := range [][2]int{{0, 9}, {10, 20}, {21, 31}} {
		if checkDigitMod10(d[c[0]:c[1]]) != int(d[c[1]]-'0') {
			return false
		}
	}
	return ValidBoletoBarcode(d[:4] + d[32:33] + d[33:47] + d[4:9] + d[10:20] + d[21:31])
}

// billPaymentCheckDigit: the 3rd digit gives the modulus (6 or 7: 10; 8 or 9: 11). With 11,
// remainder 0 or 1 gives check digit 0 and remainder 10 gives check digit 1 (FEBRABAN, Standard
// Layout for Bill Payment/Receipt with Barcode, DAC calculation).
func billPaymentCheckDigit(s string, mod11 bool) int {
	if !mod11 {
		return checkDigitMod10(s)
	}
	switch r := sumMod11(s) % 11; r {
	case 0, 1:
		return 0
	case 10:
		return 1
	default:
		return 11 - r
	}
}

// ValidBillPaymentBarcode: product 8, value identifier from 6 to 9 and the general check digit at
// position 4.
func ValidBillPaymentBarcode(c string) bool {
	if len(c) != 44 || c[0] != '8' || c[2] < '6' || digitsOnly(c) != c {
		return false
	}
	return billPaymentCheckDigit(c[:3]+c[4:], c[2] >= '8') == int(c[3]-'0')
}

// ValidBillPayment: the 48-digit line; each block of 11 with its check digit, in the same modulus
// as the general check digit (National Treasury, GRU barcode).
func ValidBillPayment(s string) bool {
	d := digitsOnly(s)
	if len(d) != 48 || d[0] != '8' || d[2] < '6' {
		return false
	}
	c := ""
	for b := 0; b < 4; b++ {
		block := d[b*12 : b*12+11]
		if billPaymentCheckDigit(block, d[2] >= '8') != int(d[b*12+11]-'0') {
			return false
		}
		c += block
	}
	return ValidBillPaymentBarcode(c)
}

func findBoletos(s string, add func(ini, end int, typ string)) {
	for _, ix := range reBoleto.FindAllStringIndex(s, -1) {
		if ValidBoleto(s[ix[0]:ix[1]]) {
			add(ix[0], ix[1], "boleto")
		}
	}
	for _, ix := range reBillPayment.FindAllStringIndex(s, -1) {
		if ValidBillPayment(s[ix[0]:ix[1]]) {
			add(ix[0], ix[1], "boleto")
		}
	}
	for _, ix := range reBarcode.FindAllStringIndex(s, -1) {
		if v := s[ix[0]:ix[1]]; ValidBoletoBarcode(v) || ValidBillPaymentBarcode(v) {
			add(ix[0], ix[1], "boleto")
		}
	}
}

// reTracking: postal item identifier (UPU S10 standard, used by Correios): 2 service letters, 8
// digits, the check digit and the 2 country letters; also printed in groups of 3.
var reTracking = regexp.MustCompile(`\b[A-Z]{2} ?\d{3} ?\d{3} ?\d{3} ?[A-Z]{2}\b`)

// ValidTracking: the check digit by S10 (5.4): weights 8 6 4 2 3 5 9 7, 11 - remainder of modulo
// 11; 10 becomes 0 and 11 becomes 5.
func ValidTracking(s string) bool {
	d := digitsOnly(s)
	if len(d) != 9 {
		return false
	}
	sum := 0
	for i, p := range []int{8, 6, 4, 2, 3, 5, 9, 7} {
		sum += int(d[i]-'0') * p
	}
	dv := 11 - sum%11
	switch dv {
	case 10:
		dv = 0
	case 11:
		dv = 5
	}
	return int(d[8]-'0') == dv
}

func findTrackingCodes(s string, add func(ini, end int, typ string)) {
	for _, ix := range reTracking.FindAllStringIndex(s, -1) {
		if ValidTracking(s[ix[0]:ix[1]]) {
			add(ix[0], ix[1], "tracking")
		}
	}
}

// reCertificate: number of the civil registry certificates (birth, marriage, death), 32 digits:
// office(6) collection(2) service(2) year(4) book type(1) book(5) page(3) entry(7) check digits(2)
// (CNJ Provision 149/2023, art. 473; SIRC guide). Printed in groups, with spaces or dots.
var reCertificate = regexp.MustCompile(`\b\d{6}[ .]?\d{2}[ .]?\d{2}[ .]?\d{4}[ .]?\d[ .]?\d{5}[ .]?\d{3}[ .]?\d{7}[ .-]?\d{2}\b`)

// ValidCertificateNumber: the service is always 55 (civil registry of natural persons), the year
// has four digits and the book type is not 0. The check digit is not verified: the calculation
// belongs to the CNJ system (SIRC) and was not published.
func ValidCertificateNumber(s string) bool {
	d := digitsOnly(s)
	if len(d) != 32 || d[8:10] != "55" || d[14] == '0' {
		return false
	}
	return d[10:14] >= "1800" && d[10:14] <= "2099"
}

func findCertificates(s string, add func(ini, end int, typ string)) {
	for _, ix := range reCertificate.FindAllStringIndex(s, -1) {
		if ValidCertificateNumber(s[ix[0]:ix[1]]) {
			add(ix[0], ix[1], "certificate")
		}
	}
}

// RG: each state defines the number of its general registry (Law 7.116/1983), and no identification
// institute publishes the check-digit rule; the National Identity Card uses the CPF (Decree
// 10.977/2022), which has its own rule. So the RG is recognized by the label: after "RG", "registro
// geral", "cédula de identidade" or "carteira de identidade", the number of any state, with or
// without punctuation, with the state in front ("MG-12.345.678") and whatever check digit (digit or
// X). The issuing body after it ("SSP/DF") stays.
var reLabeledRG = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:rg|r\.g\.|registro\s+geral|c[eé]dula\s+de\s+identidade|carteira\s+de\s+identidade)(?:\s*(?:n[º°o]\.?|n[uú]mero))?\s*[:=]?\s*((?:(?-i:[A-Z]{2})[-\s]?)?\d[\d.\-]{3,14}[\dxX])`)

func findLabeledRG(s string, add func(ini, end int, typ string)) {
	for _, ix := range reLabeledRG.FindAllStringSubmatchIndex(s, -1) {
		v := s[ix[2]:ix[3]]
		if len(digitsOnly(v)) >= 5 && digitEdge(s, ix[2], ix[3]) && (ix[3] == len(s) || !isAlnum(s[ix[3]])) {
			add(ix[2], ix[3], "rg")
		}
	}
}

// ICD-10 (WHO; DATASUS): a letter, 2 digits and the subcategory (1 digit, with or without a dot). A
// diagnosis is health data, sensitive (LGPD, art. 5, II). It only counts right after "CID" or
// "diagnóstico": alone, "A10" is anything. It takes the list ("CID F32.1, F41.0 e Z73").
var (
	reCIDGatilho = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:cids?(?:[- ]?10)?|diagn[oó]stico|diagnosis)["']?(?:\s*(?:[:=]|n[º°o]\.?))?\s*["']?`)
	reCIDCodigo  = regexp.MustCompile(`^[A-Za-z]\d{2}(?:\.?\d)?`)
	reCIDSep     = regexp.MustCompile(`^(?:\s*[,;/+]\s*|\s+(?:e|and|ou|or)\s+)`)
)

func findCID(s string, add func(ini, end int, typ string)) {
	for _, ix := range reCIDGatilho.FindAllStringIndex(s, -1) {
		p := ix[1]
		for n := 0; n < 10; n++ {
			c := reCIDCodigo.FindString(s[p:])
			if c == "" || p+len(c) < len(s) && (isAlnum(s[p+len(c)]) || s[p+len(c)] == '_') {
				break
			}
			add(p, p+len(c), "sensitive")
			p += len(c)
			sep := reCIDSep.FindString(s[p:])
			if sep == "" {
				break
			}
			p += len(sep)
		}
	}
}

// hasCIDTrigger: prefilter of findCID.
func hasCIDTrigger(lower string) bool {
	return strings.Contains(lower, "cid") || strings.Contains(lower, "diagn")
}
