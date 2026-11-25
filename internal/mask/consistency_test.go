package mask

import "testing"

// A value a reader masked in one new data text and that goes out in the clear in another new data
// text of the same request is counted (the model would see the pseudonym and the real value); the
// assistant's text and a word glued to another do not count. A masked object name that is a
// dictionary word is counted apart.
func TestConsistencyOfRequest(t *testing.T) {
	m := newTest(t)
	b := m.NewBatch()
	pos := func(i byte) Position { var p Position; p[0] = i; return p }
	var ents []Entry
	add := func(_ string, e []Entry) { ents = append(ents, e...) }
	add(b.MaskHint("CREATE TABLE billing (id INT, valor NUMBER);", false, pos(1), ""))
	add(b.MaskHintProse("o billing caiu de novo hoje cedo", false, pos(2), ""))
	add(b.MaskHintProse("só o billings_old e o xbilling ficaram", false, pos(3), ""))
	add(b.MaskWritten("o billing é a tabela que caiu", false, pos(4), ""))
	c := b.Check(ents)
	if c.InClear["table"] != 1 {
		t.Errorf("in the clear elsewhere: %v (wants table 1)", c.InClear)
	}
	if c.Commons["table"] != 1 {
		t.Errorf("common words masked: %v (wants table 1)", c.Commons)
	}

	// only the assistant brings it back in the clear: nothing to count
	b2 := m.NewBatch()
	ents = nil
	add(b2.MaskHint("CREATE TABLE tb_pedido_x9 (id INT);", false, pos(1), ""))
	add(b2.MaskWritten("a tb_pedido_x9 caiu", false, pos(2), ""))
	if c := b2.Check(ents); len(c.InClear) != 0 {
		t.Errorf("assistant text counted: %v", c.InClear)
	}
}
