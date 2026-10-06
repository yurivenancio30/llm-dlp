package mask

import (
	"strings"
	"testing"
)

// Regra léxica para código e configuração (seção C, teste T7). Nomes inventados, vários de
// palavra comum.

// decidido: o nome v está entre as decisões do texto s, com o tipo ent (e a regra, se dada).
func decidido(m *Masker, s, v, ent, regra string) bool {
	for _, d := range m.Decididos(s, "") {
		if d.Nome == v && d.Ent == ent && (regra == "" || d.Regra == regra) {
			return true
		}
	}
	return false
}

func TestLexicaT7(t *testing.T) {
	m := novoTeste(t)
	s := `namespace = "payments"`
	confere(t, m, s, []string{"payments"}, []string{"namespace = "})
	if !decidido(m, s, "payments", "namespace", "") {
		t.Errorf("payments não ficou nas decisões do texto: %+v", m.Decididos(s, ""))
	}
	for _, s := range []string{
		`payments = load()`,              // identificador de código
		`# payments do dia`,              // comentário é prosa
		`msg = "payments failed"`,        // literal com frase
		"payments = billing\n",           // atribuição de código (sem seção de INI)
		"# namespace payments\n",         // comentário com palavra de tipo
		`log.info("namespace payments")`, // frase dentro do literal
	} {
		if out, ents := m.Mascarar(s); len(ents) > 0 {
			t.Errorf("mascarou a mais: %q -> %q", s, out)
		}
	}
}

// A pista da chave, em qualquer formato: o valor inteiro de palavra comum vira nome do tipo da
// chave.
func TestLexicaPistaDaChave(t *testing.T) {
	m := novoTeste(t)
	for _, c := range []struct{ s, nome, ent string }{
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
		confere(t, m, c.s, []string{c.nome}, nil)
		if !decidido(m, c.s, c.nome, c.ent, "") {
			t.Errorf("%q: %s não ficou decidido como %s: %+v", c.s, c.nome, c.ent, m.Decididos(c.s, ""))
		}
	}
	// sem cabeçalho de seção, ou com código no meio, continua sendo atribuição de código
	for _, s := range []string{
		"table_name = pedidos\n",
		"[banco]\nprint(x)\ntable_name = pedidos\n",
		"x = [1]\ntable_name = pedidos\n",
		"[1]\ntable_name = pedidos\n",
	} {
		if out, ents := m.Mascarar(s); len(ents) > 0 {
			t.Errorf("mascarou a mais: %q -> %q", s, out)
		}
	}
}

// Âncora: numa lista de literais em que um item já é nome, os outros são nomes do mesmo tipo.
func TestLexicaAncora(t *testing.T) {
	m := novoTeste(t)
	// a âncora achada no mesmo texto
	s := "db_host = \"pgsrv-01\"\nalvos = [\"pgsrv-01\", \"billing\", \"checkout\"]\n"
	confere(t, m, s, []string{"billing", "checkout"}, []string{"alvos"})
	if !decidido(m, s, "billing", "servidor", "léxica-âncora") {
		t.Errorf("billing: %+v", m.Decididos(s, ""))
	}
	// a âncora aprendida antes (outro texto), em lista de Go e em lista YAML
	m.Mascarar("conectando em host=pgsrv-02 porta 5432")
	confere(t, m, `alvos := []string{"ledger", "pgsrv-02"}`, []string{"ledger"}, nil)
	confere(t, m, "alvos:\n  - pgsrv-02\n  - reports\n  - \"audit\"\n", []string{"reports", "audit"}, nil)
	confere(t, m, "[\n  \"pgsrv-02\",\n  \"payroll\",\n]", []string{"payroll"}, nil)

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
		out, _ := m.Mascarar(s)
		for _, w := range []string{"billing", "checkout", "admin", "rodar", "testar"} {
			if strings.Contains(s, w) && !strings.Contains(out, w) {
				t.Errorf("%s mascarado a mais: %q -> %q", w, s, out)
			}
		}
	}
	// pseudônimo traduzido não vira âncora de si mesmo; âncoras de tipos diferentes desistem
	m.Mascarar(`bucket = "bkt-01"`)
	s = `xs = ["pgsrv-02", "bkt-01", "billing"]`
	if out, _ := m.Mascarar(s); !strings.Contains(out, "billing") {
		t.Errorf("âncoras de tipos diferentes: %q", out)
	}
}

func TestLexicaCabecalhoINI(t *testing.T) {
	for v, quer := range map[string]bool{
		"[db]": true, "[tool:pytest]": true, `[remote "origin"]`: true, "[a.b-c]": true,
		"[1]": false, "[x for x in y]": false, "[]": false, `[a "b]`: false, "[a, b]": false,
	} {
		if cabecalhoINI(v) != quer {
			t.Errorf("cabecalhoINI(%q) = %v", v, !quer)
		}
	}
}
