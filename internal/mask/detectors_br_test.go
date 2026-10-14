package mask

import (
	"fmt"
	"strings"
	"testing"
)

// The manuals' examples: FEBRABAN modulo 10 and 11 (DAC of 01230067896: 3 and 0), the barcode from
// Annex I of the CAIXA manual (general check digit 4) and the identifier of the UPU S10 standard
// (473124829).
func TestValidatorsBR(t *testing.T) {
	if checkDigitMod10("01230067896") != 3 || billPaymentCheckDigit("01230067896", true) != 0 {
		t.Errorf("FEBRABAN check digit: %d %d", checkDigitMod10("01230067896"), billPaymentCheckDigit("01230067896", true))
	}
	code := "1049" + "4" + "3242" + "0000032112" + "0055077222133347777777771"
	if !ValidBoletoBarcode(code) || ValidBoletoBarcode(code[:4]+"5"+code[5:]) {
		t.Errorf("barcode from CAIXA's manual")
	}
	if !ValidBoleto(boletoLine(code)) || ValidBoleto(strings.Replace(boletoLine(code), "77777", "77776", 1)) {
		t.Errorf("typeable line: %s", boletoLine(code))
	}
	arr := billPaymentLine("836" + "00000001234" + "5004" + "8100500123456789012345678")
	if !ValidBillPayment(arr) || ValidBillPayment(arr[:5]+"9"+arr[6:]) {
		t.Errorf("collection slip: %s", arr)
	}
	if !ValidTracking("RR473124829BR") || ValidTracking("RR473124828BR") {
		t.Errorf("S10")
	}
	if !ValidCertificateNumber("10453901552013100012021000012312") || ValidCertificateNumber("10453901542013100012021000012312") {
		t.Errorf("certificate registration number")
	}
}

// boletoLine builds the typed line (5 fields) of a collection barcode.
func boletoLine(c string) string {
	c1, c2, c3 := c[0:4]+c[19:24], c[24:34], c[34:44]
	return fmt.Sprintf("%s.%s%d %s.%s%d %s.%s%d %s %s", c1[:5], c1[5:], checkDigitMod10(c1), c2[:5], c2[5:], checkDigitMod10(c2),
		c3[:5], c3[5:], checkDigitMod10(c3), c[4:5], c[5:19])
}

// billPaymentLine: with 43 digits (without the general check digit) builds the code and the
// 48-digit line in 4 blocks.
func billPaymentLine(without string) string {
	mod11 := without[2] >= '8'
	c := without[:3] + fmt.Sprint(billPaymentCheckDigit(without, mod11)) + without[3:]
	var bs []string
	for b := 0; b < 4; b++ {
		block := c[b*11 : b*11+11]
		bs = append(bs, fmt.Sprintf("%s-%d", block, billPaymentCheckDigit(block, mod11)))
	}
	return strings.Join(bs, " ")
}

func TestDetectsBR(t *testing.T) {
	m := newTest(t)
	code := "1049" + "4" + "3242" + "0000032112" + "0055077222133347777777771"
	cases := []struct{ s, gone string }{
		{"pague o boleto " + boletoLine(code) + " até sexta", "32420000032112"},
		{"codigo_barras: " + code, code},
		{"conta de luz " + billPaymentLine("836"+"00000001234"+"5004"+"8100500123456789012345678"), "12345004810"},
		{"rastreio RR473124829BR e RR 473 124 829 BR", "473124829"},
		{"matrícula 104539 01 55 2013 1 00012 021 0000123 12", "0000123"},
		{"atestado com CID F32.1, F41.0 e Z73 por 15 dias", "F41.0"},
		{`{"cid": "F32.1", "cid10": "Z73"}`, "Z73"},
	}
	for _, c := range cases {
		out, ents := m.Mask(c.s)
		if strings.Contains(out, c.gone) {
			t.Errorf("%q: kept %q\n   out %q", c.s, c.gone, out)
		}
		if back := NewTable(ents).Unmask(out, false); back != c.s {
			t.Errorf("way back of %q:\n   %q", c.s, back)
		}
	}
	// RG of any state, by the label
	for _, c := range []struct{ s, gone string }{
		{"RG: MG-12.345.678 expedido em BH", "345.678"},
		{"RG nº 1234567 SSP/DF", "1234567"},
		{"carteira de identidade 12.345.678-90 (IFP/RJ)", "345.678"},
		{"registro geral 9876543210", "9876543210"},
		{"R.G. 12.345.678-X", "345.678"},
	} {
		out, ents := m.Mask(c.s)
		if strings.Contains(out, c.gone) {
			t.Errorf("%q: kept %q\n   out %q", c.s, c.gone, out)
		}
		if back := NewTable(ents).Unmask(out, false); back != c.s {
			t.Errorf("way back of %q:\n   %q", c.s, back)
		}
	}
	for _, s := range []string{
		"rg 2024", "o cargo 12345 da tabela", "identidade visual 2024 v12345",
		"rastreio errado RR473124828BR", "matrícula 104539 01 54 2013 1 00012 021 0000123 12",
		"cid do e-mail: cid:image001.png", "o diagnóstico ficou para depois", "CID-10 é a classificação",
	} {
		if out, _ := m.Mask(s); out != s {
			t.Errorf("masked what is not data: %q -> %q", s, out)
		}
	}
}
