package mask

import (
	"strings"
	"testing"
)

// Lexical rule for code and configuration. Made-up names, several of them common words.

// decided: the name v is among the decisions of text s, with type ent (and the rule, if given).
func decided(m *Masker, s, v, ent, rule string) bool {
	for _, d := range m.Decided(s, "") {
		if d.Name == v && d.Ent == ent && (rule == "" || d.Rule == rule) {
			return true
		}
	}
	return false
}

func TestLexicalT7(t *testing.T) {
	m := newTest(t)
	s := `namespace = "payments"`
	check(t, m, s, []string{"payments"}, []string{"namespace = "})
	if !decided(m, s, "payments", "namespace", "") {
		t.Errorf("payments não ficou nas decisões do texto: %+v", m.Decided(s, ""))
	}
	for _, s := range []string{
		`payments = load()`,              // code identifier
		`# payments do dia`,              // a comment is prose
		`msg = "payments failed"`,        // literal with a sentence
		"payments = billing\n",           // code assignment (without an INI section)
		"# namespace payments\n",         // comment with a type word
		`log.info("namespace payments")`, // a sentence inside the literal
	} {
		if out, ents := m.Mask(s); len(ents) > 0 {
			t.Errorf("mascarou a mais: %q -> %q", s, out)
		}
	}
}

// The key clue, in any format: the whole common-word value becomes a name of the key's type.
func TestLexicalKeyClue(t *testing.T) {
	m := newTest(t)
	for _, c := range []struct{ s, name, ent string }{
		{`namespace = 'payments'`, "payments", "namespace"},
		{`opts := Options{Namespace: "payments"}`, "payments", "namespace"},
		{"metadata:\n  namespace: payments\n  name: web\n", "payments", "namespace"},
		{"NAMESPACE=payments\n", "payments", "namespace"},
		{`table_name = "pedidos"`, "pedidos", "table"},
		{`{"bucket": "billing", "region": "us-east-1"}`, "billing", "bucket"},
		{`resource "aws_s3_bucket" "b" { bucket = "billing" }`, "billing", "bucket"},
		{"queue = \"checkout\"\nretries = 3\n", "checkout", "queue"},
		// INI section: "key = value" with spaces counts inside the block of the header
		{"[banco]\nhost = pgsrv-01\ntable_name = pedidos\n", "pedidos", "table"},
		{"; config\n[fila]\n# comentário\nqueue = checkout\n", "checkout", "queue"},
	} {
		check(t, m, c.s, []string{c.name}, nil)
		if !decided(m, c.s, c.name, c.ent, "") {
			t.Errorf("%q: %s não ficou decidido como %s: %+v", c.s, c.name, c.ent, m.Decided(c.s, ""))
		}
	}
	// without a section header, or with code in the middle, it is still a code assignment
	for _, s := range []string{
		"table_name = pedidos\n",
		"[banco]\nprint(x)\ntable_name = pedidos\n",
		"x = [1]\ntable_name = pedidos\n",
		"[1]\ntable_name = pedidos\n",
	} {
		if out, ents := m.Mask(s); len(ents) > 0 {
			t.Errorf("mascarou a mais: %q -> %q", s, out)
		}
	}
}

// Anchor: in a list of literals in which one item is already a name, the others are names of the
// same type.
func TestLexicalAnchor(t *testing.T) {
	m := newTest(t)
	// the anchor found in the same text
	s := "db_host = \"pgsrv-01\"\nalvos = [\"pgsrv-01\", \"billing\", \"checkout\"]\n"
	check(t, m, s, []string{"billing", "checkout"}, []string{"alvos"})
	if !decided(m, s, "billing", "server", "léxica-âncora") {
		t.Errorf("billing: %+v", m.Decided(s, ""))
	}
	// the anchor learned before (another text), in a Go list and in a YAML list
	m.Mask("conectando em host=pgsrv-02 porta 5432")
	check(t, m, `alvos := []string{"ledger", "pgsrv-02"}`, []string{"ledger"}, nil)
	check(t, m, "alvos:\n  - pgsrv-02\n  - reports\n  - \"audit\"\n", []string{"reports", "audit"}, nil)
	check(t, m, "[\n  \"pgsrv-02\",\n  \"payroll\",\n]", []string{"payroll"}, nil)

	for _, s := range []string{
		`alvos = ["billing", "checkout"]`,             // without an anchor
		`conectar("pgsrv-02", "admin")`,               // call arguments: mixed types
		`par = ("pgsrv-02", "billing")`,               // tupla
		`alvos = ["pgsrv-02", "deu erro", "billing"]`, // an item is a sentence: not a list of names
		`alvos = ["pgsrv-02", "--force", "billing"]`,  // an option in the middle: argv
		`alvos = ["pgsrv-02", 3, "billing"]`,          // a number in the middle
		"Passos:\n- rodar\n- testar\n",                // bullets without an anchor
		"- pgsrv-02\n- billing\n",                     // bullets without the \"key:\" line above
		"alvos:\n  - pgsrv-02\n  - name: billing\n",   // an item that is a map
		`__all__ = ["pgsrv-02", "billing"]`,           // code identifiers (Python)
	} {
		out, _ := m.Mask(s)
		for _, w := range []string{"billing", "checkout", "admin", "rodar", "testar"} {
			if strings.Contains(s, w) && !strings.Contains(out, w) {
				t.Errorf("%s mascarado a mais: %q -> %q", w, s, out)
			}
		}
	}
	// a translated pseudonym does not become an anchor of itself; anchors of different types give
	// up
	m.Mask(`bucket = "bkt-01"`)
	s = `xs = ["pgsrv-02", "bkt-01", "billing"]`
	if out, _ := m.Mask(s); !strings.Contains(out, "billing") {
		t.Errorf("âncoras de tipos diferentes: %q", out)
	}
}

func TestLexicalINIHeader(t *testing.T) {
	for v, wants := range map[string]bool{
		"[db]": true, "[tool:pytest]": true, `[remote "origin"]`: true, "[a.b-c]": true,
		"[1]": false, "[x for x in y]": false, "[]": false, `[a "b]`: false, "[a, b]": false,
	} {
		if iniHeader(v) != wants {
			t.Errorf("cabecalhoINI(%q) = %v", v, !wants)
		}
	}
}
