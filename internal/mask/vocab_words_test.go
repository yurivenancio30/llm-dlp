package mask

import (
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// The list only accepts a word of letters; a compound name or one with a digit is never a common
// word.
func TestCommonWord(t *testing.T) {
	for _, v := range []string{"conta", "CONTA", "Tabela", "linhagem", "\"status\"", "[relatório]"} {
		if !commonWord(v) {
			t.Errorf("%s should be a common word", v)
		}
	}
	for _, v := range []string{"tb_conta", "conta2", "tag_name", "FIN.CONTA", "vendas-sul", "x"} {
		if commonWord(v) {
			t.Errorf("%s is not a common word by itself", v)
		}
	}
}

// A registered term always wins: the name the model wrote before is not its own if it contains a
// term, and a common word that is a term does not stay in plain text.
func TestTermBeatsModelAndCommonWord(t *testing.T) {
	cfg := config.Default()
	cfg.Terms = []config.Term{{Label: "empresa", Values: []string{"Azul"}}}
	m, _ := NewMasker(cfg, testKey, nil, nil)
	l := m.NewBatch()
	l.UsePublic(map[string]bool{"tb_azul_pedidos": true, "tb_vendas_ok": true})
	if l.fromModel("tb_azul_pedidos") {
		t.Error("name with a registered term was treated as the model's knowledge")
	}
	if !l.fromModel("tb_vendas_ok") {
		t.Error("name without a term, written by the model, should be the model's")
	}
	if m.freeCommon("azul") || !m.freeCommon("conta") {
		t.Error("a registered term should win over the common words list")
	}
}
