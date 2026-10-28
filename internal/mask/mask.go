package mask

import (
	"crypto/sha256"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
)

// Mask: the entry point. It memoizes results, because Claude Code resends the whole conversation
// with every message.

// Mask and the result memory (Claude Code resends the whole conversation with every message).

const memoMax = 64 << 20 // (approximate) ceiling of the memoized text, adding both generations

// Mask swaps the sensitive data of s for pseudonyms and returns the entries used. Results are
// memoized: Claude Code resends the whole conversation with every message. The proxy uses a Batch,
// which also freezes what went out, by position (see memory_sent.go).
func (m *Masker) Mask(s string) (string, []Entry) {
	r, _ := m.maskIt(s, true)
	return r.text, r.entries
}

func (m *Masker) maskIt(s string, learn bool) (result, [32]byte) {
	return m.maskD(s, learn, "")
}

// maskD: with the hint of the command that produced the text (see call_command.go; "" = no hint).
func (m *Masker) maskD(s string, learn bool, hint string) (result, [32]byte) {
	if len(s) < 4 {
		return result{text: s}, [32]byte{}
	}
	k := memoKey(s, hint)
	return m.maskKE(s, k, learn, readHint(hint), hint), k
}

// memoKey: the key of the text in the result memory (with the hint, if any: the same text may come
// out of different commands).
func memoKey(s, hint string) [32]byte {
	if hint == "" {
		return sha256.Sum256([]byte(s))
	}
	h := sha256.New()
	h.Write([]byte("dica\x00" + hint + "\x00"))
	h.Write([]byte(s))
	var k [32]byte
	copy(k[:], h.Sum(nil))
	return k
}

// maskK: with the text hash already computed (k).
func (m *Masker) maskK(s string, k [32]byte, learn bool, d *outputHint) result {
	return m.maskKE(s, k, learn, d, "")
}

// maskKE: with the raw hint (its extension goes to the deciders).
func (m *Masker) maskKE(s string, k [32]byte, learn bool, d *outputHint, hint string) result {
	m.mu.Lock()
	r, ok := m.memo[k]
	if !ok {
		if r, ok = m.stale[k]; ok {
			r = m.store(k, r) // still in use: goes back to the current generation of the memo
		}
	}
	m.mu.Unlock()
	g := m.known.generation()
	if ok && !(learn && r.noLearn) {
		// Valid, unless it contains a value learned AFTERWARDS (by another text, for example).
		if r.gen == g {
			return r
		}
		if !m.known.containsSince(s, r.gen) {
			r.gen = g // checked up to here: next time it does not check again
			m.mu.Lock()
			r = m.store(k, r)
			m.mu.Unlock()
			return r
		}
	}
	_, ext := hintParts(hint)
	findings, decidedSet := m.detectDE(s, learn, d, ext)
	m.recordSoftware(s)

	// Up to which generation is this result valid? If nothing was learned during detection, up to
	// g. If something was learned (by this text or by another, in parallel), it checks: is every
	// new value that appears in s already among the findings? Then it is valid up to the current
	// generation. If some was left out (learned by another text after this one had already been
	// scanned), it stays marked with g, and the next lookup redoes it.
	if g1 := m.known.generation(); g1 != g {
		covered := make(map[[2]int]bool, len(findings))
		for _, a := range findings {
			covered[[2]int{a.Start, a.End}] = true
		}
		missingOne := false
		m.known.scan(s, g, func(ini, end int, _ string) bool {
			missingOne = !covered[[2]int{ini, end}]
			return !missingOne
		})
		if !missingOne {
			g = g1
		}
	}
	text, entries, ts := m.applyT(s, findings)
	if decidedSet == nil {
		decidedSet = []Decision{} // computed, without decisions (nil = unknown: see Batch.Memory)
	}

	m.mu.Lock()
	r = m.store(k, result{text, entries, ts, g, !learn, decidedSet})
	m.mu.Unlock()
	return r
}

// posID: how a text at a position is kept on disk (HMAC of the position key).
func (m *Masker) posID(kc Position) string { return m.p.raw("enviado", string(kc[:]), 16) }

// UseSent turns on the on-disk record of what already went out (only the proxy uses it).
// fingerprint identifies the configuration and the version: if they change, the old records are no
// longer valid.
func (m *Masker) UseSent(path, fingerprint string) error {
	e, err := LoadSent(path, m.p.raw("impressao", fingerprint, 16))
	if err != nil {
		return err
	}
	m.sentSet = e
	return nil
}

// Position identifies a text at an exact point of the conversation: a hash of everything that comes
// before it in the request and of itself (the proxy computes it). Freezing only applies to the same
// Position: the exact resend of that point of the conversation goes out the same; the same text in
// another conversation, or in a new message, is masked with what is known now.
type Position [32]byte

// frozen: the result the text went out with at this position, if it already went out (from memory,
// or from enviados.log after a restart).
func (m *Masker) frozen(kc Position, s string) (result, bool) {
	m.mu.Lock()
	r, ok := m.frz[kc]
	if !ok {
		if r, ok = m.frzOld[kc]; ok {
			m.storeFrozen(kc, r)
		}
	}
	m.mu.Unlock()
	if ok || m.sentSet == nil {
		return r, ok
	}
	ts, foundIt := m.sentSet.Lookup(m.posID(kc))
	if !foundIt {
		return r, false
	}
	text, entries, valid := rebuild(s, ts)
	if !valid {
		return r, false
	}
	// after a restart, what the text taught is in RAM no more, and a text that already went out is
	// not detected again: the software proofs (memory_software.go) and what its e-mails teach
	// (memory_email.go) are recorded now, once per position
	m.recordSoftware(s)
	for _, e := range entries {
		if e.Type == "email" {
			m.learnEmail(e.Real)
		}
	}
	r = result{text: text, entries: entries, snippets: ts}
	m.mu.Lock()
	m.storeFrozen(kc, r)
	m.mu.Unlock()
	return r, true
}

// storeFrozen: how to store, for the frozen ones (with m.mu locked).
func (m *Masker) storeFrozen(kc Position, r result) {
	if _, ok := m.frz[kc]; !ok {
		if m.frzBytes > memoMax/2 {
			m.frzOld, m.frz, m.frzBytes = m.frz, map[Position]result{}, 0
		}
		m.frzBytes += len(r.text) + 96*len(r.entries) + 64
	}
	m.frz[kc] = r
}

// Batch: the texts of a request. After the request goes out, Freeze fixes the result of each one at
// its position.
type Batch struct {
	m       *Masker
	outputs map[Position]result // what went out at each position
	mem     *memory             // the conversation memory (see memory_conversation.go); nil = empty
	pre     map[Position]result // the memo result of each new text, already looked up in Memory
	// tracing (see memory_tracing.go)
	deducedSet    map[string]bool            // values marked by deduction: they do not serve as seeds
	sources       map[string]map[string]bool // value (lowercase) -> sources where it had direct proof
	currentSource string                     // the source of the text being assembled (empty: no command)
	currentProv   map[string]bool            // decided with proof in the text being assembled itself
	writing       bool                       // assembling assistant text (receives all the contagion)
	prose         bool                       // assembling message text, from the user or the response (vocab_words.go)
	publicSet     map[string]bool            // priority: the model wrote it before any data
}

func (m *Masker) NewBatch() *Batch {
	return &Batch{m: m, outputs: map[Position]result{}, pre: map[Position]result{}}
}

// BatchItem: a text of the request, where it is and whether it came from the internet.
type BatchItem struct {
	S       string
	FromWeb bool
	Pos     Position
	Hint    string // what the command that produced the text says about it (Commands.Hint)
}

// Mask: a text that already went out at this position goes out the same (rewriting would protect
// nothing and would rewrite the conversation in the API cache). fromWeb = internet content
// (WebFetch/WebSearch result): it is masked, but nothing of it is remembered.
func (l *Batch) Mask(s string, fromWeb bool, pos Position) (string, []Entry) {
	return l.MaskHint(s, fromWeb, pos, "")
}

// MaskHint: like Mask, with the hint of the command that produced the text.
func (l *Batch) MaskHint(s string, fromWeb bool, pos Position, hint string) (string, []Entry) {
	l.currentSource, hint = SplitSource(hint)
	if l.currentSource == SystemSource {
		return l.MaskContagion(s, fromWeb, pos)
	}
	if len(s) < 4 {
		return s, nil
	}
	if r, ok := l.m.frozen(pos, s); ok {
		return r.text, r.entries
	}
	r, ok := l.pre[pos]
	if !ok || r.noLearn && !fromWeb {
		r, _ = l.m.maskD(s, !fromWeb, hint)
	}
	r = l.withMemory(s, r, hint)
	if _, already := l.outputs[pos]; !already {
		l.outputs[pos] = r
	}
	return r.text, r.entries
}

// Warm masks, before assembly, the texts of the request that have not gone out yet, so that every
// value learned in it already applies when the texts are assembled in order.
func (l *Batch) Warm(items []BatchItem) {
	items = noSources(items)
	pend := make([]warmItem, 0, len(items))
	for _, it := range items {
		if len(it.S) < 4 {
			continue
		}
		if _, ok := l.m.frozen(it.Pos, it.S); ok {
			continue
		}
		pend = append(pend, warmItem{it.S, !it.FromWeb, it.Hint})
	}
	l.m.warm(pend)
}

// Freeze: the request went out. The result of each text stays fixed at its position (and goes to
// disk).
func (l *Batch) Freeze() {
	m := l.m
	newSet := make([]Position, 0, len(l.outputs))
	m.mu.Lock()
	for kc, r := range l.outputs {
		if _, ok := m.frz[kc]; ok {
			continue
		}
		m.storeFrozen(kc, r)
		newSet = append(newSet, kc)
	}
	m.mu.Unlock()
	if m.sentSet != nil {
		for _, kc := range newSet {
			m.sentSet.Record(m.posID(kc), l.outputs[kc].snippets)
		}
	}
}

// Persist writes to disk what was learned and not yet written (new people).
func (m *Masker) Persist() {
	if m.people != nil {
		m.people.SaveIfDirty()
	}
	if m.sentSet != nil {
		m.sentSet.Save()
	}
}

// Warm masks in parallel the texts that are not in the memo yet. It is for the moment when a lot of
// new text arrives at once (first message after a restart, resumed session): instead of one text at
// a time, it uses every core. The final result is the same, because whoever assembles the request
// calls Mask in order right after (and Mask rechecks the text not yet sent against values learned
// in the meantime).
func (m *Masker) Warm(texts []string) {
	items := make([]warmItem, len(texts))
	for i, t := range texts {
		items[i] = warmItem{t, true, ""}
	}
	m.warm(items)
}

type warmItem struct {
	s     string
	learn bool
	hint  string
}

func (m *Masker) warm(items []warmItem) {
	var pend []warmItem
	total := 0
	seen := map[[32]byte]bool{}
	m.mu.Lock()
	for _, it := range items {
		if len(it.s) < 4 {
			continue
		}
		k := memoKey(it.s, it.hint)
		_, a := m.memo[k]
		_, b := m.stale[k]
		if !a && !b && !seen[k] {
			seen[k] = true
			pend = append(pend, it)
			total += len(it.s)
		}
	}
	m.mu.Unlock()
	if len(pend) < 2 || total < warmMin {
		// little text: one at a time (but before assembly, so all learning applies in it)
		for _, it := range pend {
			m.maskD(it.s, it.learn, it.hint)
		}
		return
	}
	// the largest first, so the tasks stay balanced
	sort.Slice(pend, func(i, j int) bool { return len(pend[i].s) > len(pend[j].s) })
	n := runtime.GOMAXPROCS(0)
	if n > len(pend) {
		n = len(pend)
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < n; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= len(pend) {
					return
				}
				m.maskD(pend[i].s, pend[i].learn, pend[i].hint)
			}
		}()
	}
	wg.Wait()
}

// warmMin: below this much new text, parallelism does not pay off
const warmMin = 48 << 10

// store puts the result in the memo (with m.mu locked) and returns it. The memo has two
// generations: when the current one fills up, it becomes the "old" one and another empty one
// starts; what is still in use is brought back on lookup, and the rest disappears when the old one
// is dropped. This way RAM has a ceiling and there is no moment when everything is forgotten at
// once.
func (m *Masker) store(k [32]byte, r result) result {
	if _, ok := m.memo[k]; !ok {
		if m.bytes > memoMax/2 {
			m.stale, m.memo, m.bytes = m.memo, map[[32]byte]result{}, 0
		}
		m.bytes += len(r.text) + 96*len(r.entries) + 64
	}
	m.memo[k] = r
	return r
}
