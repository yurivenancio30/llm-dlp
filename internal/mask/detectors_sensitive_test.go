package mask

import (
	"encoding/base64"
	"strings"
	"testing"
)

// LGPD sensitive data (art. 5, II) in the middle of the text. Each case: what has to disappear; and
// the sentences where the same word talks about nobody, which have to stay as they are.
func TestSensitiveInText(t *testing.T) {
	m := newTest(t)
	fmr := base64.StdEncoding.EncodeToString(append([]byte("FMR\x00 20\x00"), make([]byte, 30)...))
	cases := []struct {
		s    string
		gone []string
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
	for _, c := range cases {
		out, ents := m.Mask(c.s)
		for _, x := range c.gone {
			if strings.Contains(out, x) {
				t.Errorf("%q: kept %q\n   out %q", c.s, x, out)
			}
		}
		if back := NewTable(ents).Unmask(out, false); back != c.s {
			t.Errorf("way back of %q:\n   %q", c.s, back)
		}
	}
	for _, s := range []string{
		"a tela é preta e o botão branco e preto", "a igreja católica do bairro fica na praça",
		"o PT do documento (português) e o PL 2630", "cor: branca; tema escuro", "João Batista chegou",
		"o novo sistema e a rede nova", "o servidor amarelo no painel", "membro do time de dados",
		"ssh-keygen -lf: fingerprint: SHA256:abc", "Rk1SA é só texto",
	} {
		if out, _ := m.Mask(s); out != s {
			t.Errorf("masked what is not sensitive data: %q -> %q", s, out)
		}
	}
}

func TestSensitiveLabels(t *testing.T) {
	for _, r := range []string{"identidade_genero", "filiacao_partidaria", "opiniao_politica", "conviccao_religiosa",
		"vida_sexual", "impressao_digital", "raca_cor", "Race", "comorbidades"} {
		if c := labelClass(r); c != "sensitive" {
			t.Errorf("%s: %q", r, c)
		}
	}
	// "filiação" alone is still the name of the parents; "identidade", the RG
	if labelClass("filiacao") != "name" || labelClass("identidade") != "rg" {
		t.Errorf("filiacao %q, identidade %q", labelClass("filiacao"), labelClass("identidade"))
	}
}
