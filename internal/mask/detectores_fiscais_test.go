package mask

import (
	"strings"
	"testing"
)

// chaveDFe monta uma chave de acesso com o DV calculado (módulo 11, pesos 2 a 9 da direita para
// a esquerda, cada caractere valendo o código ASCII menos 48).
func chaveDFe(base43 string) string {
	soma, p := 0, 2
	for i := len(base43) - 1; i >= 0; i-- {
		soma += int(base43[i]-'0') * p
		if p++; p > 9 {
			p = 2
		}
	}
	dv := 0
	if r := soma % 11; r >= 2 {
		dv = 11 - r
	}
	return base43 + string(rune('0'+dv))
}

func TestChaveDFe(t *testing.T) {
	cnpj := soDigitos(gerarValido("112223330001", 14, CNPJValido))
	cpf := strings.NewReplacer(".", "", "-", "").Replace(gerarCPF("529982247"))
	// o exemplo oficial do CNPJ alfanumérico (Receita Federal): 12.ABC.345/01DE-35
	validas := []string{
		chaveDFe("35" + "2609" + cnpj + "55" + "001" + "000012345" + "1" + "48213579"),
		chaveDFe("53" + "2611" + "12ABC34501DE35" + "65" + "001" + "000000077" + "1" + "10203040"),
		chaveDFe("31" + "2610" + "000" + cpf + "55" + "920" + "000000001" + "1" + "55667788"),
	}
	for _, c := range validas {
		if !ChaveDFeValida(c) {
			t.Errorf("chave válida recusada: %s", c)
		}
	}
	v := validas[0]
	invalidas := []string{
		v[:43] + string(rune('0'+(int(v[43]-'0')+1)%10)), // DV errado
		chaveDFe("99" + v[2:43]),                         // UF que não existe
		chaveDFe(v[:4] + "13" + v[6:43]),                 // mês 13
		chaveDFe(v[:6] + "11222333000100" + v[20:43]),    // CNPJ do emitente inválido
		v[:43],
	}
	for _, c := range invalidas {
		if ChaveDFeValida(c) {
			t.Errorf("chave inválida aceita: %s", c)
		}
	}
	m := novoTeste(t)
	grupos := ""
	for i := 0; i < 44; i += 4 {
		grupos += v[i:i+4] + " "
	}
	for _, s := range []string{"chave " + v + " ok", "<infNFe Id=\"NFe" + v + "\" versao=\"4.00\">", "DANFE " + strings.TrimSpace(grupos)} {
		out, ents := m.Mascarar(s)
		if strings.Contains(out, cnpj) || strings.Contains(out, v[20:]) || strings.Contains(out, v[:20]) {
			t.Errorf("chave ficou: %q", out)
		}
		if volta := NovaTabela(ents).Desmascarar(out, false); volta != s {
			t.Errorf("volta: %q", volta)
		}
	}
	// 44 dígitos sem ser chave (DV, UF ou mês não batem) ficam
	if out, _ := m.Mascarar("id " + invalidas[0]); !strings.Contains(out, invalidas[0]) {
		t.Errorf("número que não é chave mascarado: %q", out)
	}
}

// Os exemplos do Manual de Padrões para Iniciação do Pix (BCB): o estático com chave e o
// dinâmico com URL. O nome e a cidade de quem recebe também somem.
func TestBRCode(t *testing.T) {
	exemplos := []string{
		"00020126580014br.gov.bcb.pix0136123e4567-e12b-12d1-a456-4266554400005204000053039865802BR5913Fulano de Tal6008BRASILIA62070503***63041D3D",
		"00020101021226700014br.gov.bcb.pix2548pix.example.com/8b3da2f39a4140d1a91abd93113bd4415204000053039865802BR5913Fulano de Tal6008BRASILIA62070503***630464E4",
	}
	m := novoTeste(t)
	for _, e := range exemplos {
		s := "pague com o código " + e + " até amanhã"
		out, ents := m.Mascarar(s)
		if strings.Contains(out, "Fulano") || strings.Contains(out, "BRASILIA") || strings.Contains(out, "br.gov.bcb.pix") {
			t.Errorf("BR Code ficou: %q", out)
		}
		if !strings.HasPrefix(out, "pague com o código ") || !strings.HasSuffix(out, " até amanhã") {
			t.Errorf("mascarou além do código: %q", out)
		}
		if volta := NovaTabela(ents).Desmascarar(out, false); volta != s {
			t.Errorf("volta: %q", volta)
		}
	}
	// CRC errado ou sem o GUI do Pix: não é um BR Code do Pix
	var achou []string
	add := func(ini, fim int, _ string) { achou = append(achou, "x") }
	acharBRCode(exemplos[0][:len(exemplos[0])-4]+"1D3E", add)
	acharBRCode(strings.Replace(exemplos[0], "br.gov.bcb.pix", "br.gov.bcb.xyz", 1), add)
	if len(achou) != 0 {
		t.Errorf("aceitou BR Code inválido: %d", len(achou))
	}
}
