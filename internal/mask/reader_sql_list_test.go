package mask

import (
	"regexp"
	"strings"
	"testing"
)

// A statement without ";" in one item of a list ends there: the words of the next items are not
// read as columns. A real statement split over lines keeps being read whole.
func TestSQLEndsAtListItem(t *testing.T) {
	reTok := func(w string) *regexp.Regexp {
		return regexp.MustCompile(`(^|[^\pL\pN_])` + regexp.QuoteMeta(w) + `($|[^\pL\pN_])`)
	}
	for _, c := range []struct {
		name, text string
		gone, stay []string
	}{
		{"numbered list", "9. rode SELECT id FROM tb_pedido_hx\n10. minha carteira: abc\n11. cpf do cliente\n",
			[]string{"tb_pedido_hx"}, []string{"minha", "carteira", "cpf", "cliente"}},
		{"bullets", "- confira com SELECT id FROM tb_pedido_hx\n- minha carteira nova\n• cpf do cliente\n",
			[]string{"tb_pedido_hx"}, []string{"minha", "carteira", "cpf", "cliente"}},
		{"star on the next line", "SELECT\n  *\nFROM tb_pedido_hx\nWHERE col_valor_hx > 1\n",
			[]string{"tb_pedido_hx", "col_valor_hx"}, nil},
		{"negative number in a column", "SELECT col_valor_hx\n  -1 AS col_ajuste_hx\nFROM tb_pedido_hx\n",
			[]string{"tb_pedido_hx", "col_valor_hx"}, nil},
		{"a list of statements", "1. SELECT col_valor_hx FROM tb_pedido_hx\n2. SELECT col_data_hx FROM tb_cliente_hx\n",
			[]string{"tb_pedido_hx", "tb_cliente_hx", "col_valor_hx", "col_data_hx"}, nil},
	} {
		out, _ := newTest(t).Mask(c.text)
		for _, w := range c.gone {
			if reTok(w).MatchString(out) {
				t.Errorf("%s: %q went through: %q", c.name, w, out)
			}
		}
		for _, w := range c.stay {
			if !strings.Contains(out, w) {
				t.Errorf("%s: %q was masked: %q", c.name, w, out)
			}
		}
	}
}
