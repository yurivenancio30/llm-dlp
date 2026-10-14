package mask

import (
	"fmt"
	"strings"
	"testing"
)

// Os exemplos dos manuais: módulo 10 e 11 da FEBRABAN (DAC de 01230067896: 3 e 0), o código de
// barras do Anexo I do manual da CAIXA (DV geral 4) e o identificador da norma S10 da UPU
// (473124829).
func TestValidatorsBR(t *testing.T) {
	if checkDigitMod10("01230067896") != 3 || billPaymentCheckDigit("01230067896", true) != 0 {
		t.Errorf("DAC da FEBRABAN: %d %d", checkDigitMod10("01230067896"), billPaymentCheckDigit("01230067896", true))
	}
	code := "1049" + "4" + "3242" + "0000032112" + "0055077222133347777777771"
	if !ValidBoletoBarcode(code) || ValidBoletoBarcode(code[:4]+"5"+code[5:]) {
		t.Errorf("código de barras do manual da CAIXA")
	}
	if !ValidBoleto(boletoLine(code)) || ValidBoleto(strings.Replace(boletoLine(code), "77777", "77776", 1)) {
		t.Errorf("linha digitável: %s", boletoLine(code))
	}
	arr := billPaymentLine("836" + "00000001234" + "5004" + "8100500123456789012345678")
	if !ValidBillPayment(arr) || ValidBillPayment(arr[:5]+"9"+arr[6:]) {
		t.Errorf("arrecadação: %s", arr)
	}
	if !ValidTracking("RR473124829BR") || ValidTracking("RR473124828BR") {
		t.Errorf("S10")
	}
	if !ValidCertificateNumber("10453901552013100012021000012312") || ValidCertificateNumber("10453901542013100012021000012312") {
		t.Errorf("matrícula de certidão")
	}
}

// boletoLine monta a linha digitável (5 campos) de um código de barras de cobrança.
func boletoLine(c string) string {
	c1, c2, c3 := c[0:4]+c[19:24], c[24:34], c[34:44]
	return fmt.Sprintf("%s.%s%d %s.%s%d %s.%s%d %s %s", c1[:5], c1[5:], checkDigitMod10(c1), c2[:5], c2[5:], checkDigitMod10(c2),
		c3[:5], c3[5:], checkDigitMod10(c3), c[4:5], c[5:19])
}

// billPaymentLine: com 43 dígitos (sem o DV geral) monta o código e a linha de 48 em 4 blocos.
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
			t.Errorf("%q: ficou %q\n   saiu %q", c.s, c.gone, out)
		}
		if back := NewTable(ents).Unmask(out, false); back != c.s {
			t.Errorf("volta de %q:\n   %q", c.s, back)
		}
	}
	// RG de qualquer estado, pelo rótulo
	for _, c := range []struct{ s, gone string }{
		{"RG: MG-12.345.678 expedido em BH", "345.678"},
		{"RG nº 1234567 SSP/DF", "1234567"},
		{"carteira de identidade 12.345.678-90 (IFP/RJ)", "345.678"},
		{"registro geral 9876543210", "9876543210"},
		{"R.G. 12.345.678-X", "345.678"},
	} {
		out, ents := m.Mask(c.s)
		if strings.Contains(out, c.gone) {
			t.Errorf("%q: ficou %q\n   saiu %q", c.s, c.gone, out)
		}
		if back := NewTable(ents).Unmask(out, false); back != c.s {
			t.Errorf("volta de %q:\n   %q", c.s, back)
		}
	}
	for _, s := range []string{
		"rg 2024", "o cargo 12345 da tabela", "identidade visual 2024 v12345",
		"rastreio errado RR473124828BR", "matrícula 104539 01 54 2013 1 00012 021 0000123 12",
		"cid do e-mail: cid:image001.png", "o diagnóstico ficou para depois", "CID-10 é a classificação",
	} {
		if out, _ := m.Mask(s); out != s {
			t.Errorf("mascarou o que não é dado: %q -> %q", s, out)
		}
	}
}
