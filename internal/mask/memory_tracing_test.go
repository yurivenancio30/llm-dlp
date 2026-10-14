package mask

import (
	"strings"
	"testing"
)

// The accumulated knowledge (learned in previous texts, or from before a restart) follows the
// tracing rules: a learned common word does not come back in every text, an identifier-shaped name
// comes back, public vocabulary never.
func TestAccumulatedKnowledgeFollowsRules(t *testing.T) {
	m := newTest(t)
	m.Mask("SELECT id FROM erro JOIN tb_pedido_cli ON 1=1 JOIN linhagem ON 1=1;")
	out, _ := m.Mask("deu erro na linhagem; olhei a tb_pedido_cli e a SNOWFLAKE.ACCOUNT_USAGE.TAG_REFERENCES")
	for _, w := range []string{"erro", "linhagem", "ACCOUNT_USAGE"} {
		if !strings.Contains(out, w) {
			t.Errorf("%q should not be swapped by accumulated knowledge: %s", w, out)
		}
	}
	if strings.Contains(out, "tb_pedido_cli") {
		t.Errorf("a learned name that looks like an identifier should come back masked: %s", out)
	}
}

// SQL cut without ";" (grep, head or log output) followed by something else: the statement ends
// where the line before is complete and the next one is a loose word; file names of a listing do
// not become table.column. SQL formatted over several lines is still read.
func TestCutSQLDoesNotSwallowListing(t *testing.T) {
	m := newTest(t)
	out, _ := m.Mask("SELECT a FROM t WHERE x = 1\nlinhagem.sql\nconfere.py\nrelatorio-novo.html\n")
	for _, w := range []string{"linhagem.sql", "confere.py", "relatorio-novo.html"} {
		if !strings.Contains(out, w) {
			t.Errorf("%s from the listing was read as SQL: %s", w, out)
		}
	}
	out, _ = m.Mask("SELECT\n  t.col_aa1,\n  t.col_bb2\nFROM\n  dw.tb_pedido_zz t\nWHERE t.col_aa1 = 1\n")
	for _, w := range []string{"col_aa1", "col_bb2", "tb_pedido_zz"} {
		if strings.Contains(out, w) {
			t.Errorf("multi-line SQL stopped being read (%s in plain text): %s", w, out)
		}
	}
}
