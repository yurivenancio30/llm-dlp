package mask

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Test reader: the real rules come in the following batches. "TABLE x" and "HOST x" are a strong
// position; "tab x" and "ref x" are weak (each one is a rule); "COLUMN x" is a column.
var reStrongTest = regexp.MustCompile(`\b(TABLE|HOST|COLUMN) ([\w.\-]+)`)
var reWeakTest = regexp.MustCompile(`\b(tab|ref) ([\w.\-]+)`)

func testReader() []Reader {
	ent := map[string]string{"TABLE": "table", "HOST": "server", "COLUMN": "column", "tab": "table", "ref": "table"}
	return []Reader{{Name: "teste", Public: func(v string) bool { return strings.EqualFold(v, "dual_x1") },
		Find: func(s string, add func(ObjFinding)) {
			for _, ix := range reStrongTest.FindAllStringSubmatchIndex(s, -1) {
				add(ObjFinding{ix[4], ix[5], ent[s[ix[2]:ix[3]]], "strong", true})
			}
			for _, ix := range reWeakTest.FindAllStringSubmatchIndex(s, -1) {
				add(ObjFinding{ix[4], ix[5], ent[s[ix[2]:ix[3]]], s[ix[2]:ix[3]], false})
			}
		}}}
}

func withReader(t *testing.T) *Masker {
	m := newTest(t)
	m.UseReaders(testReader())
	return m
}

var rePseudoT = regexp.MustCompile(`\b[Tt]_[a-z2-7]{8}\b`)

func TestObjectPseudonym(t *testing.T) {
	m := withReader(t)
	a := m.Pseudonym("obj.table", "tb_pedido_x9")
	if !regexp.MustCompile(`^t_[a-z2-7]{8}$`).MatchString(a) {
		t.Fatalf("format: %q", a)
	}
	if b := m.Pseudonym("obj.table", "TB_PEDIDO_X9"); b != "T"+a[1:] {
		t.Errorf("the same table in uppercase should have the same ID: %q %q", a, b)
	}
	if b := m.Pseudonym("obj.table", "[tb_pedido_x9]"); b[2:] != a[2:] {
		t.Errorf("brackets should not change the ID")
	}
	if b := m.Pseudonym("obj.column", "tb_pedido_x9"); b[2:] == a[2:] {
		t.Errorf("different types should have different IDs")
	}
	// stable across instances (restart) with the same key; another one with another key
	m2, _ := NewMasker(m.cfg, testKey, nil, nil)
	if m2.Pseudonym("obj.table", "tb_pedido_x9") != a {
		t.Errorf("pseudonym changed with another instance of the same key")
	}
	m3, _ := NewMasker(m.cfg, []byte("outra-chave-outra-chave-outra-32"), nil, nil)
	if m3.Pseudonym("obj.table", "tb_pedido_x9") == a {
		t.Errorf("different keys gave the same pseudonym")
	}
}

func TestIdentifierShape(t *testing.T) {
	yes := []string{"tb_pedido", "srv01", "app.config", "svc-pedidos", "contaCorrente", "DB_X"}
	negated := []string{"cliente", "Pedido", "SQL", "guarda", "1.2.3", "v2.10.1", "550e8400-e29b-41d4-a716-446655440000",
		"deadbeefdeadbeef", "ab", "2026"}
	for _, v := range yes {
		if !identifierShape(v) {
			t.Errorf("should look like an identifier: %q", v)
		}
	}
	for _, v := range negated {
		if identifierShape(v) {
			t.Errorf("should not look like an identifier: %q", v)
		}
	}
}

func TestStrongObjectPropagates(t *testing.T) {
	m := withReader(t)
	out, _ := m.Mask("CREATE TABLE tb_pedido_x9 (id int)")
	if strings.Contains(out, "tb_pedido_x9") || !rePseudoT.MatchString(out) {
		t.Fatalf("in position: %q", out)
	}
	for _, s := range []string{"a carga da tb_pedido_x9 falhou", "a carga da TB_PEDIDO_X9 falhou", "x: [\"tb_pedido_x9\"]"} {
		if out, _ := m.Mask(s); strings.Contains(strings.ToLower(out), "tb_pedido_x9") {
			t.Errorf("did not propagate: %q", out)
		}
	}
	// a piece of another name is not the name
	if out, _ := m.Mask("a tb_pedido_x9_hist e a xtb_pedido_x9"); !strings.Contains(out, "tb_pedido_x9_hist") || !strings.Contains(out, "xtb_pedido_x9") {
		t.Errorf("masked a piece of another name: %q", out)
	}
}

func TestWeakObjectTeachesOnlyWithTwoRules(t *testing.T) {
	m := withReader(t)
	if out, _ := m.Mask("veja tab tb_fraca_01 agora"); strings.Contains(out, "tb_fraca_01") {
		t.Fatalf("in the weak position it should mask: %q", out)
	}
	if out, _ := m.Mask("solto: tb_fraca_01"); !strings.Contains(out, "tb_fraca_01") {
		t.Fatalf("weak evidence should not teach: %q", out)
	}
	m.Mask("veja tab tb_fraca_01 de novo") // mesma regra: continua fraco
	if out, _ := m.Mask("solto de novo: tb_fraca_01"); !strings.Contains(out, "tb_fraca_01") {
		t.Fatalf("the same rule twice should not teach: %q", out)
	}
	m.Mask("e ref tb_fraca_01") // outra regra
	if out, _ := m.Mask("agora solto: tb_fraca_01"); strings.Contains(out, "tb_fraca_01") {
		t.Errorf("duas regras diferentes deveriam ensinar: %q", out)
	}
}

func TestSimpleWordAndColumnDoNotPropagate(t *testing.T) {
	m := withReader(t)
	if out, _ := m.Mask("TABLE cliente"); strings.Contains(out, "cliente") {
		t.Fatalf("in position it should mask the plain word: %q", out)
	}
	if out, _ := m.Mask("o cliente ligou"); out != "o cliente ligou" {
		t.Errorf("palavra simples propagou: %q", out)
	}
	if out, _ := m.Mask("COLUMN vl_total_x2"); strings.Contains(out, "vl_total_x2") {
		t.Fatalf("column in position: %q", out)
	}
	if out, _ := m.Mask("soma de vl_total_x2"); !strings.Contains(out, "vl_total_x2") {
		t.Errorf("column should not propagate by default: %q", out)
	}
	m.cfg.Objects.Propagate = map[string]bool{"column": true}
	m.Mask("COLUMN vl_outro_x3")
	if out, _ := m.Mask("soma de vl_outro_x3"); strings.Contains(out, "vl_outro_x3") {
		t.Errorf("with propagate.column on it should propagate: %q", out)
	}
}

func TestPublicVocabularyNotLearned(t *testing.T) {
	m := withReader(t)
	if out, _ := m.Mask("TABLE dual_x1"); !strings.Contains(out, "dual_x1") {
		t.Errorf("public vocabulary was masked: %q", out)
	}
	if out, _ := m.Mask("solto dual_x1"); !strings.Contains(out, "dual_x1") {
		t.Errorf("public vocabulary was learned: %q", out)
	}
}

func TestToggleByType(t *testing.T) {
	m := withReader(t)
	m.Mask("HOST srv-exemplo-01")
	m.cfg.Objects.Mask = map[string]bool{"server": false}
	if out, _ := m.Mask("HOST srv-exemplo-02"); !strings.Contains(out, "srv-exemplo-02") {
		t.Errorf("server turned off was masked in position: %q", out)
	}
	if out, _ := m.Mask("ping srv-exemplo-01 agora"); !strings.Contains(out, "srv-exemplo-01") {
		t.Errorf("server learned before stayed masked after being turned off: %q", out)
	}
	m.cfg.Objects.Mask = nil
	m.cfg.Objects.Enabled = false
	if out, _ := m.Mask("TABLE tb_desligado_01"); !strings.Contains(out, "tb_desligado_01") {
		t.Errorf("objects turned off were masked: %q", out)
	}
}

func TestWebObjectDoesNotTeach(t *testing.T) {
	m := withReader(t)
	if out, _ := m.NewBatch().Mask("doc: TABLE tb_web_01", true, posA); strings.Contains(out, "tb_web_01") {
		t.Fatalf("on the page it should mask: %q", out)
	}
	if out, _ := m.Mask("solto tb_web_01"); !strings.Contains(out, "tb_web_01") {
		t.Errorf("web content taught: %q", out)
	}
}

func TestObjectAfterRestartAndValidity(t *testing.T) {
	dir := t.TempDir()
	vs, _ := LoadSeen(dir + "/vistos.json")
	m, _ := NewMasker(newTest(t).cfg, testKey, nil, vs)
	m.UseReaders(testReader())
	m.Mask("TABLE tb_disco_01")
	vs.SaveIfDirty()
	if b, _ := os.ReadFile(dir + "/vistos.json"); strings.Contains(string(b), "tb_disco") {
		t.Fatalf("name in plain text in seen.json")
	}
	newOne := func() *Masker {
		v, _ := LoadSeen(dir + "/vistos.json")
		m, _ := NewMasker(newTest(t).cfg, testKey, nil, v)
		return m
	}
	if out, _ := newOne().Mask("depois do reinício: TB_DISCO_01"); strings.Contains(strings.ToLower(out), "tb_disco_01") {
		t.Errorf("after the restart it did not mask: %q", out)
	}
	before := today
	defer func() { today = before }()
	today = func() int { return before() + objValidity + 5 }
	if out, _ := newOne().Mask("bem depois: tb_disco_01"); !strings.Contains(out, "tb_disco_01") {
		t.Errorf("name not seen for more than %d days kept propagating: %q", objValidity, out)
	}
}

// Round trip: the pseudonym the model writes, in either case of the prefix, goes back to the real
// name.
func TestRoundTripObject(t *testing.T) {
	m := withReader(t)
	out, ents := m.Mask("SELECT * FROM x; TABLE tb_ida_01")
	p := rePseudoT.FindString(out)
	if p == "" {
		t.Fatalf("no pseudonym: %q", out)
	}
	tab := NewTable(ents)
	for _, written := range []string{p, "T" + p[1:], "t" + p[1:]} {
		if got := tab.Unmask("rode `SELECT 1 FROM "+written+"`;", false); got != "rode `SELECT 1 FROM tb_ida_01`;" {
			t.Errorf("way back of %q: %q", written, got)
		}
	}
	if out2, _ := m.Mask("o modelo escreveu " + p); out2 != "o modelo escreveu "+p {
		t.Errorf("ready pseudonym was masked again: %q", out2)
	}
}

// A name learned later does not change the text that already went out (freezing), but applies to
// new text.
func TestLearnedObjectKeepsWhatWasSent(t *testing.T) {
	m := withReader(t)
	old := "a carga da tb_cong_01 falhou"
	l := m.NewBatch()
	sent, _ := l.Mask(old, false, posA)
	l.Freeze()
	m.Mask("TABLE tb_cong_01")
	if out, _ := m.NewBatch().Mask(old, false, posA); out != sent {
		t.Errorf("the text that already went out changed")
	}
	if out, _ := m.NewBatch().Mask(old, false, posB); strings.Contains(out, "tb_cong_01") {
		t.Errorf("new text did not use the learned name: %q", out)
	}
}

// A name with the shape of a pseudonym (type prefix + 8 letters) is not skipped: only the pseudonym
// we generated ourselves is skipped.
func TestNameShapedLikePseudonymIsMasked(t *testing.T) {
	m := newTest(t)
	for _, n := range []string{"t_transactions", "t_customer", "repo_services", "org_platform", "pkg_payments", "dir_archives", "acc_billings"} {
		out, _ := m.Mask("SELECT id_x FROM " + n + " WHERE a = 1 AND b = 2")
		if strings.Contains(out, n) {
			t.Errorf("SQL: %q stayed in plain text: %q", n, out)
		}
		if out, _ := m.Mask("a carga da " + n + " atrasou"); strings.Contains(out, n) {
			t.Errorf("prose: %q stayed in plain text: %q", n, out)
		}
	}
	// the pseudonym we generated is not masked again
	out, _ := m.Mask("SELECT id_x FROM tb_ja_x1 WHERE a = 1 AND b = 2")
	p := rePseudoT.FindString(out)
	if p == "" {
		t.Fatal("no pseudonym")
	}
	if out2, _ := m.Mask("SELECT id_x FROM " + p + " WHERE a = 1 AND b = 2"); !strings.Contains(out2, p) {
		t.Errorf("our pseudonym was masked again: %q", out2)
	}
}

// Item 4a: case is only ignored in the SQL types; in the others, each spelling is a name.
func TestCaseOnlyInSQLTypes(t *testing.T) {
	m := newTest(t)
	if m.Pseudonym("obj.table", "TB_X1") == "" || m.Pseudonym("obj.table", "TB_X1")[2:] != m.Pseudonym("obj.table", "tb_x1")[2:] {
		t.Errorf("table: the case should be ignored")
	}
	a, b := m.Pseudonym("obj.folder", "Relatorios_x1"), m.Pseudonym("obj.folder", "relatorios_x1")
	if a[strings.IndexByte(a, '_'):] == b[strings.IndexByte(b, '_'):] {
		t.Errorf("folder: different spellings should have different pseudonyms")
	}
	_, ents := m.Mask("aws s3 sync s3://Bkt-Dados-x1/a s3://bkt-dados-x1/b")
	tab := NewTable(ents)
	reals := map[string]bool{}
	for _, e := range ents {
		reals[e.Real] = true
	}
	if !reals["Bkt-Dados-x1"] || !reals["bkt-dados-x1"] || tab.Conflicts > 0 {
		t.Errorf("each spelling has to come back as its own (conflicts=%d)", tab.Conflicts)
	}
}

// Final adjustment 1: server (DNS name) is case-insensitive: learned in lowercase, it is masked in
// uppercase and in mixed case, and comes back with the spelling of the text.
func TestServerWithoutCase(t *testing.T) {
	m := newTest(t)
	m.Mask("DB_HOST=sqlprd01\n")
	for _, g := range []string{"SQLPRD01", "SqlPrd01", "sqlprd01"} {
		out, ents := m.Mask("o servidor " + g + " caiu")
		if strings.Contains(out, g) {
			t.Errorf("%s stayed in plain text: %q", g, out)
		}
		if got := NewTable(ents).Unmask(out, false); got != "o servidor "+g+" caiu" {
			t.Errorf("%s did not come back with its own spelling: %q", g, got)
		}
	}
}
