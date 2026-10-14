package mask

import (
	"strings"
	"testing"
)

// Rule léxica para código e configuração. Names inventados, vários de
// palavra comum.

// decidido: o nome v está entre as decisões do texto s, com o tipo ent (e a regra, se dada).
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
		`payments = load()`,              // identificador de código
		`# payments do dia`,              // comentário é prosa
		`msg = "payments failed"`,        // literal com frase
		"payments = billing\n",           // atribuição de código (sem seção de INI)
		"# namespace payments\n",         // comentário com palavra de tipo
		`log.info("namespace payments")`, // frase dentro do literal
	} {
		if out, ents := m.Mask(s); len(ents) > 0 {
			t.Errorf("mascarou a mais: %q -> %q", s, out)
		}
	}
}

// A pista da chave, em qualquer formato: o valor inteiro de palavra comum vira nome do tipo da
// chave.
func TestLexicalKeyClue(t *testing.T) {
	m := newTest(t)
	for _, c := range []struct{ s, name, ent string }{
		{`namespace = 'payments'`, "payments", "namespace"},
		{`opts := Options{Namespace: "payments"}`, "payments", "namespace"},
		{"metadata:\n  namespace: payments\n  name: web\n", "payments", "namespace"},
		{"NAMESPACE=payments\n", "payments", "namespace"},
		{`table_name = "pedidos"`, "pedidos", "tabela"},
		{`{"bucket": "billing", "region": "us-east-1"}`, "billing", "bucket"},
		{`resource "aws_s3_bucket" "b" { bucket = "billing" }`, "billing", "bucket"},
		{"queue = \"checkout\"\nretries = 3\n", "checkout", "fila"},
		// seção de INI: "chave = valor" com espaços vale dentro do bloco do cabeçalho
		{"[banco]\nhost = pgsrv-01\ntable_name = pedidos\n", "pedidos", "tabela"},
		{"; config\n[fila]\n# comentário\nqueue = checkout\n", "checkout", "fila"},
	} {
		check(t, m, c.s, []string{c.name}, nil)
		if !decided(m, c.s, c.name, c.ent, "") {
			t.Errorf("%q: %s não ficou decidido como %s: %+v", c.s, c.name, c.ent, m.Decided(c.s, ""))
		}
	}
	// sem cabeçalho de seção, ou com código no meio, continua sendo atribuição de código
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

// Âncora: numa lista de literais em que um item já é nome, os outros são nomes do mesmo tipo.
func TestLexicalAnchor(t *testing.T) {
	m := newTest(t)
	// a âncora achada no mesmo texto
	s := "db_host = \"pgsrv-01\"\nalvos = [\"pgsrv-01\", \"billing\", \"checkout\"]\n"
	check(t, m, s, []string{"billing", "checkout"}, []string{"alvos"})
	if !decided(m, s, "billing", "servidor", "léxica-âncora") {
		t.Errorf("billing: %+v", m.Decided(s, ""))
	}
	// a âncora aprendida antes (outro texto), em lista de Go e em lista YAML
	m.Mask("conectando em host=pgsrv-02 porta 5432")
	check(t, m, `alvos := []string{"ledger", "pgsrv-02"}`, []string{"ledger"}, nil)
	check(t, m, "alvos:\n  - pgsrv-02\n  - reports\n  - \"audit\"\n", []string{"reports", "audit"}, nil)
	check(t, m, "[\n  \"pgsrv-02\",\n  \"payroll\",\n]", []string{"payroll"}, nil)

	for _, s := range []string{
		`alvos = ["billing", "checkout"]`,             // sem âncora
		`conectar("pgsrv-02", "admin")`,               // argumentos de chamada: tipos misturados
		`par = ("pgsrv-02", "billing")`,               // tupla
		`alvos = ["pgsrv-02", "deu erro", "billing"]`, // um item é frase: não é lista de nomes
		`alvos = ["pgsrv-02", "--force", "billing"]`,  // opção no meio: argv
		`alvos = ["pgsrv-02", 3, "billing"]`,          // número no meio
		"Passos:\n- rodar\n- testar\n",                // marcadores sem âncora
		"- pgsrv-02\n- billing\n",                     // marcadores sem a linha \"chave:\" em cima
		"alvos:\n  - pgsrv-02\n  - name: billing\n",   // item que é mapa
		`__all__ = ["pgsrv-02", "billing"]`,           // identificadores de código (Python)
	} {
		out, _ := m.Mask(s)
		for _, w := range []string{"billing", "checkout", "admin", "rodar", "testar"} {
			if strings.Contains(s, w) && !strings.Contains(out, w) {
				t.Errorf("%s mascarado a mais: %q -> %q", w, s, out)
			}
		}
	}
	// pseudônimo traduzido não vira âncora de si mesmo; âncoras de tipos diferentes desistem
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
