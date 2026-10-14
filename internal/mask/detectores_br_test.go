package mask

import (
	"fmt"
	"strings"
	"testing"
)

// Os exemplos dos manuais: módulo 10 e 11 da FEBRABAN (DAC de 01230067896: 3 e 0), o código de
// barras do Anexo I do manual da CAIXA (DV geral 4) e o identificador da norma S10 da UPU
// (473124829).
func TestValidadoresBR(t *testing.T) {
	if dvMod10("01230067896") != 3 || dvArrecadacao("01230067896", true) != 0 {
		t.Errorf("DAC da FEBRABAN: %d %d", dvMod10("01230067896"), dvArrecadacao("01230067896", true))
	}
	cod := "1049" + "4" + "3242" + "0000032112" + "0055077222133347777777771"
	if !CodBarrasBoletoValido(cod) || CodBarrasBoletoValido(cod[:4]+"5"+cod[5:]) {
		t.Errorf("código de barras do manual da CAIXA")
	}
	if !BoletoValido(linhaDoBoleto(cod)) || BoletoValido(strings.Replace(linhaDoBoleto(cod), "77777", "77776", 1)) {
		t.Errorf("linha digitável: %s", linhaDoBoleto(cod))
	}
	arr := linhaArrecadacao("836" + "00000001234" + "5004" + "8100500123456789012345678")
	if !ArrecadacaoValida(arr) || ArrecadacaoValida(arr[:5]+"9"+arr[6:]) {
		t.Errorf("arrecadação: %s", arr)
	}
	if !RastreioValido("RR473124829BR") || RastreioValido("RR473124828BR") {
		t.Errorf("S10")
	}
	if !MatriculaCertidaoValida("10453901552013100012021000012312") || MatriculaCertidaoValida("10453901542013100012021000012312") {
		t.Errorf("matrícula de certidão")
	}
}

// linhaDoBoleto monta a linha digitável (5 campos) de um código de barras de cobrança.
func linhaDoBoleto(c string) string {
	c1, c2, c3 := c[0:4]+c[19:24], c[24:34], c[34:44]
	return fmt.Sprintf("%s.%s%d %s.%s%d %s.%s%d %s %s", c1[:5], c1[5:], dvMod10(c1), c2[:5], c2[5:], dvMod10(c2),
		c3[:5], c3[5:], dvMod10(c3), c[4:5], c[5:19])
}

// linhaArrecadacao: com 43 dígitos (sem o DV geral) monta o código e a linha de 48 em 4 blocos.
func linhaArrecadacao(sem string) string {
	mod11 := sem[2] >= '8'
	c := sem[:3] + fmt.Sprint(dvArrecadacao(sem, mod11)) + sem[3:]
	var bs []string
	for b := 0; b < 4; b++ {
		bloco := c[b*11 : b*11+11]
		bs = append(bs, fmt.Sprintf("%s-%d", bloco, dvArrecadacao(bloco, mod11)))
	}
	return strings.Join(bs, " ")
}

func TestDetectaBR(t *testing.T) {
	m := novoTeste(t)
	cod := "1049" + "4" + "3242" + "0000032112" + "0055077222133347777777771"
	casos := []struct{ s, some string }{
		{"pague o boleto " + linhaDoBoleto(cod) + " até sexta", "32420000032112"},
		{"codigo_barras: " + cod, cod},
		{"conta de luz " + linhaArrecadacao("836"+"00000001234"+"5004"+"8100500123456789012345678"), "12345004810"},
		{"rastreio RR473124829BR e RR 473 124 829 BR", "473124829"},
		{"matrícula 104539 01 55 2013 1 00012 021 0000123 12", "0000123"},
		{"atestado com CID F32.1, F41.0 e Z73 por 15 dias", "F41.0"},
		{`{"cid": "F32.1", "cid10": "Z73"}`, "Z73"},
	}
	for _, c := range casos {
		out, ents := m.Mascarar(c.s)
		if strings.Contains(out, c.some) {
			t.Errorf("%q: ficou %q\n   saiu %q", c.s, c.some, out)
		}
		if volta := NovaTabela(ents).Desmascarar(out, false); volta != c.s {
			t.Errorf("volta de %q:\n   %q", c.s, volta)
		}
	}
	// RG de qualquer estado, pelo rótulo
	for _, c := range []struct{ s, some string }{
		{"RG: MG-12.345.678 expedido em BH", "345.678"},
		{"RG nº 1234567 SSP/DF", "1234567"},
		{"carteira de identidade 12.345.678-90 (IFP/RJ)", "345.678"},
		{"registro geral 9876543210", "9876543210"},
		{"R.G. 12.345.678-X", "345.678"},
	} {
		out, ents := m.Mascarar(c.s)
		if strings.Contains(out, c.some) {
			t.Errorf("%q: ficou %q\n   saiu %q", c.s, c.some, out)
		}
		if volta := NovaTabela(ents).Desmascarar(out, false); volta != c.s {
			t.Errorf("volta de %q:\n   %q", c.s, volta)
		}
	}
	for _, s := range []string{
		"rg 2024", "o cargo 12345 da tabela", "identidade visual 2024 v12345",
		"rastreio errado RR473124828BR", "matrícula 104539 01 54 2013 1 00012 021 0000123 12",
		"cid do e-mail: cid:image001.png", "o diagnóstico ficou para depois", "CID-10 é a classificação",
	} {
		if out, _ := m.Mascarar(s); out != s {
			t.Errorf("mascarou o que não é dado: %q -> %q", s, out)
		}
	}
}
