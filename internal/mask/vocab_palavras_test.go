package mask

import (
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// A lista só aceita uma palavra de letras; nome composto ou com dígito nunca é palavra comum.
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

// Term cadastrado sempre vence: o nome que o modelo escreveu antes não é dele se contém um
// termo, e palavra comum que é termo não fica em claro.
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
