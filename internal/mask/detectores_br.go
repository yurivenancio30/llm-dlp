package mask

import (
	"regexp"
	"strings"
)

// Mais documentos brasileiros conferidos pelo próprio dado: boleto e conta de consumo, código de
// rastreio, matrícula de certidão do registro civil e código da CID-10.

// reBoleto: linha digitável do boleto de cobrança, 47 dígitos em 5 campos
// (AAABC.CCCCX DDDDD.DDDDDY EEEEE.EEEEEZ K UUUUVVVVVVVVVV; Convenção da Cobrança da FEBRABAN,
// Anexo V, 2.3.4).
var reBoleto = regexp.MustCompile(`\b\d{5}\.?\d{5}\s*\d{5}\.?\d{6}\s*\d{5}\.?\d{6}\s*\d\s*\d{14}\b`)

// reBillPayment: linha digitável de arrecadação (conta de luz, água, telefone, tributo, GRU):
// 4 blocos de 11 dígitos, cada um com o seu DV (48 dígitos; começa por 8, o produto).
var reBillPayment = regexp.MustCompile(`\b8\d{10}[-\s]?\d\s*\d{11}[-\s]?\d\s*\d{11}[-\s]?\d\s*\d{11}[-\s]?\d\b`)

// reBarcode: o código de barras dos dois, 44 dígitos.
var reBarcode = regexp.MustCompile(`\b\d{44}\b`)

// checkDigitMod10: módulo 10 da FEBRABAN: pesos 2 e 1 da direita para a esquerda, somando os algarismos
// de cada produto; DV = 10 - resto (resto 0: DV 0).
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

// sumMod11: pesos 2 a 9 da direita para a esquerda (cada caractere vale o código ASCII menos 48).
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

// ValidBoletoBarcode: código de barras do boleto de cobrança (44 dígitos). Moeda 9 (real) ou 0
// na posição 4; o DV geral, na posição 5, pelo módulo 11 sobre as outras 43 (11 - resto; 0 ou
// maior que 9 vira 1: manual de código de barras da CAIXA, Anexo I, que segue a FEBRABAN).
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

// ValidBoleto: a linha digitável de 47 dígitos. Os três primeiros campos com o DV do módulo 10,
// e o código de barras remontado com o DV geral certo.
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

// billPaymentCheckDigit: o 3º dígito diz o módulo (6 ou 7: 10; 8 ou 9: 11). No 11, resto 0 ou 1 dá DV 0 e
// resto 10 dá DV 1 (FEBRABAN, Layout Padrão de Arrecadação/Recebimento com Utilização do Código
// de Barras, cálculo do DAC).
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

// ValidBillPaymentBarcode: produto 8, identificador de valor de 6 a 9 e o DV geral na posição 4.
func ValidBillPaymentBarcode(c string) bool {
	if len(c) != 44 || c[0] != '8' || c[2] < '6' || digitsOnly(c) != c {
		return false
	}
	return billPaymentCheckDigit(c[:3]+c[4:], c[2] >= '8') == int(c[3]-'0')
}

// ValidBillPayment: a linha de 48 dígitos; cada bloco de 11 com o seu DV, no mesmo módulo do DV
// geral (Tesouro Nacional, código de barras da GRU).
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

// reTracking: identificador de objeto postal (norma S10 da UPU, usada pelos Correios): 2 letras
// do serviço, 8 dígitos, o DV e as 2 letras do país; impresso também em grupos de 3.
var reTracking = regexp.MustCompile(`\b[A-Z]{2} ?\d{3} ?\d{3} ?\d{3} ?[A-Z]{2}\b`)

// ValidTracking: o DV pela S10 (5.4): pesos 8 6 4 2 3 5 9 7, 11 - resto do módulo 11; 10 vira 0
// e 11 vira 5.
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
			add(ix[0], ix[1], "rastreio")
		}
	}
}

// reCertificate: matrícula das certidões do registro civil (nascimento, casamento, óbito), 32
// dígitos: serventia(6) acervo(2) serviço(2) ano(4) tipo de livro(1) livro(5) folha(3) termo(7)
// DV(2) (Provimento CNJ 149/2023, art. 473; guia do SIRC). Impressa em grupos, com espaço ou ponto.
var reCertificate = regexp.MustCompile(`\b\d{6}[ .]?\d{2}[ .]?\d{2}[ .]?\d{4}[ .]?\d[ .]?\d{5}[ .]?\d{3}[ .]?\d{7}[ .-]?\d{2}\b`)

// ValidCertificateNumber: o serviço é sempre 55 (registro civil das pessoas naturais), o ano é
// de quatro dígitos e o tipo de livro não é 0. O DV não é conferido: o cálculo é do sistema do CNJ
// (SIRC) e não foi publicado.
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
			add(ix[0], ix[1], "certidao")
		}
	}
}

// RG: cada estado define o número do seu registro geral (Lei 7.116/1983), e nenhum instituto de
// identificação publica a regra do dígito verificador; a Carteira de Identidade Nacional usa o
// CPF (Decreto 10.977/2022), que tem regra própria. Então o RG se reconhece pelo rótulo: depois
// de "RG", "registro geral", "cédula de identidade" ou "carteira de identidade", o número de
// qualquer estado, com ou sem pontuação, com a UF na frente ("MG-12.345.678") e o DV que for
// (dígito ou X). O órgão emissor depois ("SSP/DF") fica.
var reLabeledRG = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:rg|r\.g\.|registro\s+geral|c[eé]dula\s+de\s+identidade|carteira\s+de\s+identidade)(?:\s*(?:n[º°o]\.?|n[uú]mero))?\s*[:=]?\s*((?:(?-i:[A-Z]{2})[-\s]?)?\d[\d.\-]{3,14}[\dxX])`)

func findLabeledRG(s string, add func(ini, end int, typ string)) {
	for _, ix := range reLabeledRG.FindAllStringSubmatchIndex(s, -1) {
		v := s[ix[2]:ix[3]]
		if len(digitsOnly(v)) >= 5 && digitEdge(s, ix[2], ix[3]) && (ix[3] == len(s) || !isAlnum(s[ix[3]])) {
			add(ix[2], ix[3], "rg")
		}
	}
}

// CID-10 (OMS; DATASUS): letra, 2 dígitos e a subcategoria (1 dígito, com ou sem ponto). Diagnóstico
// é dado de saúde, sensível (LGPD, art. 5º, II). Só vale logo depois de "CID" ou "diagnóstico":
// sozinho, "A10" é qualquer coisa. Pega a lista ("CID F32.1, F41.0 e Z73").
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
			add(p, p+len(c), "sensivel")
			p += len(c)
			sep := reCIDSep.FindString(s[p:])
			if sep == "" {
				break
			}
			p += len(sep)
		}
	}
}

// hasCIDTrigger: pré-filtro de findCID.
func hasCIDTrigger(lower string) bool {
	return strings.Contains(lower, "cid") || strings.Contains(lower, "diagn")
}
