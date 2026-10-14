package mask

import (
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// Detect: joins the detectors. Common text is examined whole; large text, in parallel pieces.

// Detect: common text is examined whole; large text, in parallel pieces.

// Detect returns the sensitive pieces of s (without resolving overlaps).
func (m *Masker) Detect(s string) []Finding { return m.detect(s, true) }

// detect with learn=false remembers nothing of what it finds (internet content): it only masks
// where it appears.
func (m *Masker) detect(s string, learn bool) []Finding { return m.detectD(s, learn, nil) }

// detectD: with the hint of the command that produced the text (nil = no hint).
func (m *Masker) detectD(s string, learn bool, d *outputHint) []Finding {
	out, _ := m.detectDE(s, learn, d, "")
	return out
}

// detectDE: also with the hint extension (for the deciders); returns the decided names.
func (m *Masker) detectDE(s string, learn bool, d *outputHint, ext string) ([]Finding, []Decision) {
	if len(s) > largeMin && !longBlock(s, largeMargin) {
		return m.detectLargeE(s, learn, d, ext)
	}
	return m.detectWholeE(s, learn, d, ext)
}

// Large text is examined in pieces, in parallel. Each piece is examined together with a margin of
// the neighboring text on both sides, and only the findings that START inside the piece count: this
// way, what sits on top of a cut (a multi-line private key, a number with the keyword right before
// it) is seen whole by one of the two sides.
//
// The cuts and the edges of the windows always fall on whitespace: a "word" (a password, a token, a
// base64 block, however large) is never split. This way, only data with spaces in the middle AND
// larger than the margin could sit on top of a cut; the only one of that kind is the "-----BEGIN
// ... -----END" block, and text with a block larger than the margin is examined whole, at once.
//
// They are variables only so the tests can use small sizes.
var (
	largeMin    = 96 << 10 // above this, in pieces
	largePiece  = 48 << 10
	largeMargin = 8 << 10 // larger than the multi-line sensitive data (RSA 4096 key ≈ 3 KB)
)

// longBlock: is there a "-----BEGIN" block whose "-----END" is more than limit bytes away (or
// missing)?
func longBlock(s string, limitN int) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], "-----BEGIN")
		if j < 0 {
			return false
		}
		i += j + 10
		k := strings.Index(s[i:], "-----END")
		if k < 0 || k+40 > limitN {
			return true
		}
	}
}

// detectWhole examines s at once.
func (m *Masker) detectWhole(s string) []Finding { return m.detectWholeA(s, true) }

func (m *Masker) detectWholeA(s string, learn bool) []Finding {
	return m.detectWholeD(s, learn, nil)
}

func (m *Masker) detectWholeD(s string, learn bool, d *outputHint) []Finding {
	out, _ := m.detectWholeE(s, learn, d, "")
	return out
}

func (m *Masker) detectWholeE(s string, learn bool, d *outputHint, ext string) ([]Finding, []Decision) {
	out := m.detectBase(s)
	out = append(out, m.findStructure(s, learn, out, d)...)
	newSet, dec := m.runDeciders(s, out, learn, ext)
	dec = append(findingDecisions(out), dec...) // the readers' ones (before the deciders' findings)
	out = append(out, newSet...)
	// Values that depend on context are remembered and recognized later anywhere (see
	// memory_known.go): without this, they would leak when the model repeats them without the
	// keyword nearby and the history is resent.
	for _, a := range out {
		if learn {
			m.learnIt(a.Type, a.Real)
		}
	}
	longNum, _ := numericProfile(s)
	m.findKnown(s, longNum, func(ini, end int, typ string) {
		out = append(out, Finding{ini, end, typ, s[ini:end]})
	})
	m.unifyObjects(out)
	return out, dec
}

func blank(b byte) bool { return b == ' ' || b == '\n' || b == '\t' || b == '\r' }

// detectLarge examines s in parallel pieces (see the comment above).
func (m *Masker) detectLarge(s string, learn bool, d *outputHint) []Finding {
	out, _ := m.detectLargeE(s, learn, d, "")
	return out
}

func (m *Masker) detectLargeE(s string, learn bool, d *outputHint, ext string) ([]Finding, []Decision) {
	type piece struct{ ini, end, jStart, jEnd int } // miolo [ini,fim) e janela [jStart,jEnd)
	// after: first position >= i that comes right after whitespace (or the end)
	after := func(i int) int {
		for i < len(s) && !(i > 0 && blank(s[i-1])) {
			i++
		}
		return i
	}
	// before: last position <= i that comes right after whitespace (or the start)
	before := func(i int) int {
		for i > 0 && !blank(s[i-1]) {
			i--
		}
		return i
	}
	var ps []piece
	for ini := 0; ini < len(s); {
		end := ini + largePiece
		if end >= len(s) {
			end = len(s)
		} else if k := strings.LastIndexByte(s[ini:end], '\n'); k > largePiece/2 {
			end = ini + k + 1 // preferably, at a line break
		} else {
			end = after(end) // otherwise, at the next space: a "word" is never cut
		}
		ps = append(ps, piece{ini, end, before(max(0, ini-largeMargin)), after(min(len(s), end+largeMargin))})
		ini = end
	}
	if len(ps) < 2 {
		return m.detectWholeE(s, learn, d, ext)
	}
	runIt := func(f func(window string, add func(ini, end int, typ string))) []Finding {
		res := make([][]Finding, len(ps))
		var next atomic.Int64
		var wg sync.WaitGroup
		for w := 0; w < min(runtime.GOMAXPROCS(0), len(ps)); w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					i := int(next.Add(1)) - 1
					if i >= len(ps) {
						return
					}
					p := ps[i]
					f(s[p.jStart:p.jEnd], func(ini, end int, typ string) {
						if a := p.jStart + ini; a >= p.ini && a < p.end {
							res[i] = append(res[i], Finding{a, p.jStart + end, typ, s[a : p.jStart+end]})
						}
					})
				}
			}()
		}
		wg.Wait()
		var out []Finding
		for _, r := range res {
			out = append(out, r...)
		}
		return out
	}
	out := runIt(func(window string, add func(ini, end int, typ string)) {
		for _, a := range m.detectBase(window) {
			add(a.Start, a.End, a.Type)
		}
	})
	// structure (objects) and tables: on the whole text (a structure may span more than one piece)
	out = append(out, m.findStructure(s, learn, out, d)...)
	newSet, dec := m.runDeciders(s, out, learn, ext)
	dec = append(findingDecisions(out), dec...) // the readers' ones (before the deciders' findings)
	out = append(out, newSet...)
	// first it learns EVERYTHING, then looks for the known values in the whole text: a value taught
	// at the end of the text is recognized at the start too
	for _, a := range out {
		if learn {
			m.learnIt(a.Type, a.Real)
		}
	}
	out = append(out, runIt(func(window string, add func(ini, end int, typ string)) {
		longNum, _ := numericProfile(window)
		m.findKnown(window, longNum, add)
	})...)
	m.unifyObjects(out)
	return out, dec
}

// findStructure: the structure and table readers, on the text without the transport (see
// normalization.go), with the findings back at the positions of the original, and on the original
// when normalization removed something that carries a name. base: the detectors' findings (for
// namesInLine). d: the hint of the command that produced the text (call_command.go), or nil. After
// the readers, the homogeneous lists (objects_lists.go) use what is already known to type the
// missing items.
func (m *Masker) findStructure(s string, learn bool, base []Finding, d *outputHint) []Finding {
	readIt := func(s string, base []Finding) []Finding {
		var out []Finding
		add := func(ini, end int, typ string) { out = append(out, Finding{ini, end, typ, s[ini:end]}) }
		m.findObjects(s, learn, add)
		if d != nil && m.cfg.Objects.Enabled {
			m.runReader(Reader{Name: "comando", Public: publicHint,
				Find: func(s string, add func(ObjFinding)) { findWithHint(s, d, add) }}, s, learn, add)
		}
		m.findLists(s, learn, add)
		if !m.cfg.Disabled("campo") {
			m.findTables(s, add)
			out = append(out, namesInLine(s, append(base[:len(base):len(base)], out...))...)
		}
		return out
	}
	n, ok := normalize(s)
	if !ok {
		return readIt(s, base)
	}
	var out []Finding
	if n.alsoRaw {
		out = readIt(s, base)
	}
	var bt []Finding // the detectors' findings, at positions of the clean text
	for _, a := range base {
		if la, lb, ok := n.cleaned(s, a.Start, a.End); ok {
			bt = append(bt, Finding{la, lb, a.Type, a.Real})
		}
	}
	for _, a := range readIt(n.t, bt) {
		if oa, ob, ok := n.original(s, a.Start, a.End); ok {
			out = append(out, Finding{oa, ob, a.Type, s[oa:ob]})
		}
	}
	return out
}
