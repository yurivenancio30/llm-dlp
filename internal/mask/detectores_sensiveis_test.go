package mask

import (
	"encoding/base64"
	"strings"
	"testing"
)

// Dados sensíveis da LGPD (art. 5º, II) no meio do texto. Cada caso: o que tem de sumir; e as
// frases em que a mesma palavra não fala de ninguém, que têm de ficar como estão.
func TestSensiveisNoTexto(t *testing.T) {
	m := novoTeste(t)
	fmr := base64.StdEncoding.EncodeToString(append([]byte("FMR\x00 20\x00"), make([]byte, 30)...))
	casos := []struct {
		s    string
		some []string
	}{
		{"o paciente é evangélico e filiado ao PSOL desde 2019", []string{"evangélico", "PSOL"}},
		{"ela se declara parda e é bissexual", []string{"parda", "bissexual"}},
		{"mulher negra, 34 anos, vítima parda", []string{"negra", "parda"}},
		{"raça/cor: Preta; religião: Umbanda", []string{"Preta", "Umbanda"}},
		{"identidade de gênero: mulher trans", []string{"mulher trans"}},
		{"ele vota no PT e a esposa é eleitora do Novo", []string{"PT ", "Novo"}},
		{"é testemunha de Jeová e membro da Assembleia de Deus", []string{"Jeová", "Assembleia"}},
		{"se converteu ao judaísmo; é ateu desde jovem", []string{"judaísmo", "ateu"}},
		{"sindicalizado no Sindicato dos Metalúrgicos do ABC desde 2010", []string{"Metalúrgicos", "ABC"}},
		{"filiação partidária: PSOL", []string{"PSOL"}},
		{"template " + fmr, []string{fmr[:12]}},
	}
	for _, c := range casos {
		out, ents := m.Mascarar(c.s)
		for _, x := range c.some {
			if strings.Contains(out, x) {
				t.Errorf("%q: ficou %q\n   saiu %q", c.s, x, out)
			}
		}
		if volta := NovaTabela(ents).Desmascarar(out, false); volta != c.s {
			t.Errorf("volta de %q:\n   %q", c.s, volta)
		}
	}
	for _, s := range []string{
		"a tela é preta e o botão branco e preto", "a igreja católica do bairro fica na praça",
		"o PT do documento (português) e o PL 2630", "cor: branca; tema escuro", "João Batista chegou",
		"o novo sistema e a rede nova", "o servidor amarelo no painel", "membro do time de dados",
		"ssh-keygen -lf: fingerprint: SHA256:abc", "Rk1SA é só texto",
	} {
		if out, _ := m.Mascarar(s); out != s {
			t.Errorf("mascarou o que não é dado sensível: %q -> %q", s, out)
		}
	}
}

func TestRotulosSensiveis(t *testing.T) {
	for _, r := range []string{"identidade_genero", "filiacao_partidaria", "opiniao_politica", "conviccao_religiosa",
		"vida_sexual", "impressao_digital", "raca_cor", "Race", "comorbidades"} {
		if c := classeRotulo(r); c != "sensivel" {
			t.Errorf("%s: %q", r, c)
		}
	}
	// filiação sozinha continua sendo o nome dos pais; identidade, o RG
	if classeRotulo("filiacao") != "nome" || classeRotulo("identidade") != "rg" {
		t.Errorf("filiacao %q, identidade %q", classeRotulo("filiacao"), classeRotulo("identidade"))
	}
}
