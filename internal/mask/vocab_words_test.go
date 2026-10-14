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
			t.Errorf("%s devia ser palavra comum", v)
		}
	}
	for _, v := range []string{"tb_conta", "conta2", "tag_name", "FIN.CONTA", "vendas-sul", "x"} {
		if commonWord(v) {
			t.Errorf("%s não é uma palavra comum sozinha", v)
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
		t.Error("nome com termo cadastrado foi tratado como conhecimento do modelo")
	}
	if !l.fromModel("tb_vendas_ok") {
		t.Error("nome sem termo, escrito pelo modelo, devia ser dele")
	}
	if m.freeCommon("azul") || !m.freeCommon("conta") {
		t.Error("termo cadastrado devia vencer a lista de palavras comuns")
	}
}
