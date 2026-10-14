package mask

import (
	"crypto/sha256"
	"strings"
	"testing"
)

// Conversation memory (memory_conversation.go): made-up names, built from parts.

var (
	nsCommon  = "pag" + "amentos" // common-word namespace
	tabCommon = "fat" + "uras"    // common-word table
)

func testPos(s string) Position { return sha256.Sum256([]byte("pos\x00" + s)) }

// batch as the proxy uses it: warms, builds the memory and masks in order
func batchWith(m *Masker, texts ...string) (*Batch, []string, []Entry) {
	l := m.NewBatch()
	items := make([]BatchItem, len(texts))
	for i, s := range texts {
		items[i] = BatchItem{S: s, Pos: testPos(s)}
	}
	l.Warm(items)
	l.Memory(items, nil)
	var outs []string
	var ents []Entry
	for _, it := range items {
		o, e := l.MaskHint(it.S, false, it.Pos, "")
		outs = append(outs, o)
		ents = append(ents, e...)
	}
	return l, outs, ents
}

func TestMemoryWholeWordAndCase(t *testing.T) {
	m := newTest(t)
	inv := "kubectl logs -n " + nsCommon + " deploy/web --tail 10"
	sql := "SELECT valor FROM " + tabCommon + " WHERE id = 1 AND x = 2"
	prose := "o " + nsCommon + " caiu; " + strings.ToUpper(nsCommon) + " é outro; " + nsCommon + "X não; " +
		"e a tabela " + strings.ToUpper(tabCommon) + " cresceu, " + tabCommon + "_old não"
	_, outs, ents := batchWith(m, inv, sql, prose)
	p := outs[2]
	if strings.Contains(p, " "+nsCommon+" ") {
		t.Errorf("namespace solto ficou em claro: %s", p)
	}
	if !strings.Contains(p, strings.ToUpper(nsCommon)+" é") {
		t.Errorf("namespace é exato: a grafia em maiúsculas é outra palavra: %s", p)
	}
	if !strings.Contains(p, nsCommon+"X") || !strings.Contains(p, tabCommon+"_old") {
		t.Errorf("palavra inteira: pedaço de outra palavra foi trocado: %s", p)
	}
	if strings.Contains(p, strings.ToUpper(tabCommon)) {
		t.Errorf("tabela é sem caixa: a grafia em maiúsculas devia sair mascarada: %s", p)
	}
	if v := NewTable(ents).Unmask(p, false); v != prose {
		t.Fatalf("ida e volta: %q", v)
	}
}

// Without the inventory in the request (new conversation), the common word stays (at the batch
// level).
func TestMemoryOnlyInRequest(t *testing.T) {
	m := newTest(t)
	batchWith(m, "kubectl logs -n "+nsCommon+" deploy/web --tail 10", "o "+nsCommon+" caiu")
	_, outs, _ := batchWith(m, "o "+nsCommon+" caiu de novo")
	if !strings.Contains(outs[0], nsCommon) {
		t.Fatalf("outra conversa: a palavra comum foi mascarada: %s", outs[0])
	}
}

// A generic word (public reference) is masked where it was decided, but does not enter the memory
// (D4).
func TestMemoryGenericOut(t *testing.T) {
	prev := publicRef
	defer func() { publicRef = prev }()
	publicRef = map[string]bool{nsCommon: true}
	m := newTest(t)
	_, outs, _ := batchWith(m, "kubectl logs -n "+nsCommon+" deploy/web --tail 10", "o "+nsCommon+" caiu")
	if !strings.Contains(outs[1], nsCommon) {
		t.Fatalf("genérica entrou na memória: %s", outs[1])
	}
}

// Text already sent (frozen) goes out the same, even if the memory now knows the name.
func TestMemoryDoesNotRewritePast(t *testing.T) {
	m := newTest(t)
	stale := "o " + nsCommon + " caiu ontem"
	l1, o1, _ := batchWith(m, stale)
	l1.Freeze()
	inv := "kubectl logs -n " + nsCommon + " deploy/web --tail 10"
	l2, o2, _ := batchWith(m, stale, inv, "e o "+nsCommon+" hoje")
	if o2[0] != o1[0] || !strings.Contains(o2[0], nsCommon) {
		t.Fatalf("texto enviado mudou: %q -> %q", o1[0], o2[0])
	}
	if strings.Contains(o2[2], nsCommon) {
		t.Fatalf("texto novo sem a memória: %s", o2[2])
	}
	// freezing writes the text with the memory applied: the resend goes out the same
	l2.Freeze()
	_, o3, _ := batchWith(m, stale, inv, "e o "+nsCommon+" hoje")
	for i := range o3 {
		if o3[i] != o2[i] {
			t.Fatalf("reenvio mudou o texto %d: %q -> %q", i, o2[i], o3[i])
		}
	}
}

// Who wrote it: the assistant text comes back as the API sent it; only the translated piece is a
// pseudonym, the same word written by the model stays (at the batch level); the translated word
// enters the memory.
func TestMemoryWhoWrote(t *testing.T) {
	m := newTest(t)
	inv := "kubectl logs -n " + nsCommon + " deploy/web --tail 10"
	_, outs, ents := batchWith(m, inv)
	tab := NewTable(ents)
	ps := ""
	for _, e := range ents {
		if e.Real == nsCommon && primaryCase(e.Pseudo, e.Real, e.Type) {
			ps = e.Pseudo
		}
	}
	if ps == "" || strings.Contains(outs[0], nsCommon) {
		t.Fatalf("premissa: o inventário não mascarou: %s", outs[0])
	}
	orig := "o namespace " + ps + " tem o serviço " + nsCommon + " do modelo"
	des := m.RecordResponse(orig, tab)
	l := m.NewBatch()
	d, ok := l.Written(des)
	if !ok || len(d) != 1 || d[0].Name != nsCommon || d[0].Ent != "namespace" || d[0].Rule != "traduzida" {
		t.Fatalf("decisões traduzidas: %+v %v", d, ok)
	}
	output := "logs de " + nsCommon + " ok"
	items := []BatchItem{{S: output, Pos: testPos(output)}}
	l.Warm(items)
	l.Memory(items, d)
	o, _ := l.MaskWritten(des, false, testPos(des), "")
	if o != orig {
		t.Fatalf("texto do assistente: %q, esperado %q", o, orig)
	}
	if only, _ := l.MaskHint(output, false, testPos(output), ""); strings.Contains(only, nsCommon) {
		t.Fatalf("palavra traduzida não entrou na memória: %s", only)
	}
}

// The who-wrote-it record survives a restart (enviados.log, only HMAC and pseudonyms).
func TestWhoWroteOnDisk(t *testing.T) {
	dir := t.TempDir()
	m := newTest(t)
	if err := m.UseSent(dir+"/e.log", "x"); err != nil {
		t.Fatal(err)
	}
	_, ents := m.Mask("kubectl logs -n " + nsCommon + " deploy/web --tail 10")
	tab := NewTable(ents)
	ps := ""
	for _, e := range ents {
		if e.Real == nsCommon && primaryCase(e.Pseudo, e.Real, e.Type) {
			ps = e.Pseudo
		}
	}
	orig := "veja " + ps + " e " + nsCommon
	des := m.RecordResponse(orig, tab)
	m.Persist()
	m2 := newTest(t)
	if err := m2.UseSent(dir+"/e.log", "x"); err != nil {
		t.Fatal(err)
	}
	if o, ok := m2.OriginalOf(des); !ok || o != orig {
		t.Fatalf("depois do reinício: %q %v", o, ok)
	}
	tr, ok := m2.Translated(des)
	if !ok || len(tr) != 1 || tr[0].Type != objTypePrefix+"namespace" {
		t.Fatalf("Traduzidas depois do reinício: %+v", tr)
	}
}

// The memory scans each text once (one index lookup per word): the cost grows with the text size,
// not with the number of names.
func BenchmarkMemoryScan1MB(b *testing.B) {
	m := newTest(b)
	var decs []Decision
	for i := 0; i < 2000; i++ {
		decs = append(decs, Decision{Name: "nome" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + string(rune('a'+i/26%26)), Ent: []string{"namespace", "tabela"}[i%2]})
	}
	mm := m.newMemory(decs)
	line := "o servico nomexab respondeu em 12ms para Tabela NOMEXXBC; outra palavra comum aqui e ali\n"
	s := strings.Repeat(line, (1<<20)/len(line))
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mm.scan(s, nil)
	}
}

// The comment marker only counts outside quotes; "--x" glued to a letter is a command option and
// "#3" is numbering.
func TestInComment(t *testing.T) {
	cases := map[string]bool{
		"# carrega as faturas":                    true,
		"select 1 -- faturas":                     true,
		"x = 1 // faturas":                        true,
		`print("a # b") # faturas`:                true,
		"pg_dump --schema=faturas":                false,
		`rotulo = "lote #3: faturas"`:             false,
		`msg = "total // faturas"`:                false,
		"ver o item #3 faturas":                   false,
		"SELECT '--' || faturas FROM t":           false,
		"echo 'a -- b' faturas":                   false,
		"# comentário com 'aspas' antes: faturas": true,
	}
	for s, wants := range cases {
		if got := inComment(s, strings.LastIndex(s, "faturas")); got != wants {
			t.Errorf("%q: emComentario=%v, esperado %v", s, got, wants)
		}
	}
}

// Provenance: in data (tool output, file read), the resource name the request proved stays a name
// inside a comment; in the user message, the pasted comment stays out of the memory's reach, as
// before.
func TestProvenanceInDataComment(t *testing.T) {
	m := newTest(t)
	sql := "SELECT valor FROM " + tabCommon + " WHERE id = 1 AND x = 2"
	fileName := "def carregar():\n    # carrega as " + tabCommon + " do dia\n    return 1\n"
	l, outs, _ := batchWith(m, sql, fileName)
	if strings.Contains(outs[1], tabCommon) {
		t.Fatalf("o nome provado no SQL ficou em claro no comentário do arquivo: %s", outs[1])
	}
	glued := "olha isto:\n# carrega as " + tabCommon + " do dia"
	if o, _ := l.MaskHintProse(glued, false, testPos(glued), ""); !strings.Contains(o, tabCommon) {
		t.Fatalf("na mensagem do usuário o comentário devia ficar como antes: %s", o)
	}
}

// A documentation file (README, agent prompt) is prose outside the code blocks: the common word the
// memory brings stays a word; in the code block and in a code file, provenance applies.
func TestDocumentAndProseOutsideCode(t *testing.T) {
	tab := "pro" + "postas" // common-word table (it is in the list)
	if !commonWord(tab) {
		t.Fatalf("premissa: %q devia ser palavra comum", tab)
	}
	m := newTest(t)
	sql := "SELECT valor FROM " + tab + " WHERE id = 1 AND x = 2"
	doc := "# Guia\n\nO time revisa as " + tab + " toda semana.\n\n```sql\nselect * from " + tab + " -- as " + tab + " do dia\n```\n"
	codeValue := "def carregar():\n    # carrega as " + tab + " do dia\n    return 1\n"
	items := []BatchItem{
		{S: sql, Pos: testPos(sql)},
		{S: doc, Pos: testPos(doc), Hint: WithSource("readme.md", "")},
		{S: codeValue, Pos: testPos(codeValue), Hint: WithSource("carga.py", "")},
	}
	l := m.NewBatch()
	l.Warm(items)
	l.Memory(items, nil)
	var outs []string
	for _, it := range items {
		o, _ := l.MaskHint(it.S, false, it.Pos, it.Hint)
		outs = append(outs, o)
	}
	if !strings.Contains(outs[1], "revisa as "+tab+" toda") {
		t.Errorf("na prosa do README a palavra comum devia ficar: %s", outs[1])
	}
	if strings.Contains(outs[1], "from "+tab) || strings.Contains(outs[1], "-- as "+tab) {
		t.Errorf("no bloco de código do README o nome devia ser mascarado: %s", outs[1])
	}
	if strings.Contains(outs[2], tab) {
		t.Errorf("no arquivo de código o nome devia ser mascarado: %s", outs[2])
	}
}
