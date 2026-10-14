package mask

import (
	"sort"
	"strings"
)

// Conversation memory (see docs/structures.md): the names decided in ALL the texts of a request
// apply to its other texts not yet sent. A name a reader or a decider recognized in a text (the
// namespace inventory, the header of a CSV) is also masked where it appears loose (in prose, in an
// output without structure), as a whole word.
//
// The memory is recomputed on every request from the request's own texts (the decisions of each one
// are in the memo): nothing is kept per conversation, and in a new conversation it starts empty. It
// is a step after the memo: the memo key does not change, and the text already sent (frozen) goes
// out the same.
//
// Who wrote it: an assistant text the proxy unmasked (recorded in RecordResponse) comes back as the
// API sent it: exactly the pieces the proxy translated become pseudonyms again, and the memory does
// not apply to the rest (the model never saw the real name; if it wrote the word, it is a common
// word). The translated words enter the memory (rule "translated", with the type of the pseudonym).

// memName: a name of the memory, indexed by its first word.
type memName struct {
	name     string
	off      int // where the first word starts inside the name
	ent      string
	caseless bool // SQL type (or server): matches in any case
}

type memory struct {
	exact    map[string][]memName // first word -> names (from largest to smallest)
	caseless map[string][]memName // first word in lowercase -> names
	n        int
	has      map[string]bool // keys already in the memory (add)
	// lengths of the first words (bit n = length n; bit 63 = 63 or more): a word of another length
	// is not even looked up
	exactSize, caselessSize uint64
}

func bitSize(n int) uint64 { return 1 << min(n, 63) }

// memMin: the shortest name the memory applies outside the text where it was decided (a column "a"
// of a SELECT cannot mask every letter "a" of the conversation).
const memMin = 3

// isWord: a byte that is part of a word (the word boundaries of the memory). A UTF-8 byte counts as
// a letter: "ação" is a single word.
func isWord(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// newMemory: the decisions, in order (the first decision of a key defines the type).
func (m *Masker) newMemory(decs []Decision) *memory {
	mm := &memory{exact: map[string][]memName{}, caseless: map[string][]memName{}}
	seen := map[string]bool{}
	for _, d := range decs {
		if d.Generic || len(d.Name) < memMin || publicGeneral(d.Name) || m.provenSoftware(d.Name) {
			continue
		}
		k := d.Key()
		if seen[k] {
			continue
		}
		seen[k] = true
		if mm.has == nil {
			mm.has = map[string]bool{}
		}
		mm.has[k] = true
		if !m.objMask(d.Ent) || isPseudoObj(d.Name) {
			continue
		}
		i := 0
		for i < len(d.Name) && !isWord(d.Name[i]) {
			i++
		}
		j := i
		letter := false
		for j < len(d.Name) && isWord(d.Name[j]) {
			if c := d.Name[j]; c < '0' || c > '9' {
				letter = true
			}
			j++
		}
		if i == j || !letter && j-i == len(d.Name) {
			continue // without a word, or only a number
		}
		nm := memName{name: d.Name, off: i, ent: d.Ent, caseless: entSQL[d.Ent]}
		if nm.caseless {
			t := strings.ToLower(d.Name[i:j])
			mm.caseless[t] = append(mm.caseless[t], nm)
			mm.caselessSize |= bitSize(j - i)
		} else {
			mm.exact[d.Name[i:j]] = append(mm.exact[d.Name[i:j]], nm)
			mm.exactSize |= bitSize(j - i)
		}
		mm.n++
	}
	if mm.n == 0 {
		return nil
	}
	for _, idx := range []map[string][]memName{mm.exact, mm.caseless} {
		for _, ns := range idx {
			sort.SliceStable(ns, func(a, b int) bool { return len(ns[a].name) > len(ns[b].name) })
		}
	}
	return mm
}

// matchIt: does name nm occupy s from start (whole word)?
func (nm memName) matchIt(s string, ini int) bool {
	end := ini + len(nm.name)
	if ini < 0 || end > len(s) {
		return false
	}
	if nm.caseless {
		if !strings.EqualFold(s[ini:end], nm.name) {
			return false
		}
	} else if s[ini:end] != nm.name {
		return false
	}
	if isWord(nm.name[0]) && ini > 0 && isWord(s[ini-1]) {
		return false
	}
	return !(isWord(nm.name[len(nm.name)-1]) && end < len(s) && isWord(s[end]))
}

// scan: the pieces of s where the memory matches, outside the pieces already swapped (ts, in
// order). One pass: each word of s is one lookup in the index.
func (mm *memory) scan(s string, ts []snippet) []snippet { return mm.scanF(s, ts, memFilter{}) }

// scanF: like scan, with the text brakes (memory_conversation_brakes.go).
func (mm *memory) scanF(s string, ts []snippet, f memFilter) []snippet {
	var out []snippet
	var buf []byte
	k := 0
	for i := 0; i < len(s); {
		if !isWord(s[i]) {
			i++
			continue
		}
		j := i + 1
		for j < len(s) && isWord(s[j]) {
			j++
		}
		var best *memName
		ini := 0
		attempt := func(ns []memName) {
			for x := range ns {
				nm := &ns[x]
				if best != nil && len(nm.name) <= len(best.name) {
					return
				}
				if a := i - nm.off; nm.matchIt(s, a) {
					best, ini = nm, a
					return
				}
			}
		}
		if mm.exactSize&bitSize(j-i) != 0 {
			attempt(mm.exact[s[i:j]])
		}
		if mm.caselessSize&bitSize(j-i) != 0 {
			buf = buf[:0]
			for x := i; x < j; x++ {
				c := s[x]
				if c >= 'A' && c <= 'Z' {
					c += 32
				}
				buf = append(buf, c)
			}
			attempt(mm.caseless[string(buf)])
		}
		end := 0
		if best != nil {
			end = ini + len(best.name)
		} else if nm := mm.piece(s[i:j]); nm != nil {
			// the name is a piece of a larger word ("LH_BRONZE_VENDA2024"): the whole word is
			// masked, with its type
			best, ini, end = nm, i, j
		} else {
			i = j
			continue
		}
		for k < len(ts) && ts[k].End <= ini {
			k++
		}
		free := (k >= len(ts) || ts[k].Start >= end) && (len(out) == 0 || out[len(out)-1].End <= ini)
		if free && !f.skips(s, ini, end, best) {
			out = append(out, snippet{Start: ini, End: end, Type: objTypePrefix + best.ent})
			i = end
			continue
		}
		i = j
	}
	return out
}

// piece: the memory name that is a sequence of parts of p (a word with "_"), identifier-shaped, if
// any; the longest. "VENDA2024" matches in "LH_BRONZE_VENDA2024" and in "T01_VENDA2024";
// "CLIENTES_PF" in "DIM_CLIENTES_PF_HIST". A common word ("status" in "user_status") stays out:
// only a name with "_", a digit or mixed case.
func (mm *memory) piece(p string) *memName {
	if strings.IndexByte(p, '_') < 0 || len(p) > 128 {
		return nil
	}
	var cuts []int // start of each part and, at the end, len(p)+1
	cuts = append(cuts, 0)
	for x := 0; x < len(p); x++ {
		if p[x] == '_' {
			cuts = append(cuts, x+1)
		}
	}
	cuts = append(cuts, len(p)+1)
	var best *memName
	for a := 0; a < len(cuts)-1; a++ {
		for b := a + 1; b < len(cuts); b++ {
			ini, end := cuts[a], cuts[b]-1
			if ini == 0 && end == len(p) || end-ini < memMin || best != nil && end-ini <= len(best.name) {
				continue
			}
			c := p[ini:end]
			if c[0] == '_' || c[len(c)-1] == '_' {
				continue
			}
			find := func(ns []memName, equal func(string) bool) {
				for x := range ns {
					if nm := &ns[x]; nm.off == 0 && equal(nm.name) && identifierShape(nm.name) {
						best = nm
						return
					}
				}
			}
			if mm.exactSize&bitSize(len(c)) != 0 {
				find(mm.exact[c], func(n string) bool { return n == c })
			}
			if mm.caselessSize&bitSize(len(c)) != 0 {
				find(mm.caseless[strings.ToLower(c)], func(n string) bool { return strings.EqualFold(n, c) })
			}
		}
	}
	return best
}

// withMemory: the result of s with the conversation memory applied (the new pieces enter the
// entries and the snippets, for the return path and for freezing).
func (l *Batch) withMemory(s string, r result, hint string) result {
	if l.mem == nil {
		l.mem = &memory{exact: map[string][]memName{}, caseless: map[string][]memName{}}
	}
	f := filterOf(hint)
	f.output = !l.prose && !l.writing
	extra := l.mem.scanF(s, r.snippets, f)
	if !l.writing && fileSource(l.currentSource) {
		// a column proven in one source does not contaminate ANOTHER file read (the file's
		// vocabulary is its own: "status" in app.js is not the column of schema.sql); the
		// conversation and outputs without a file (kubectl logs, git branch) receive the contagion.
		// A resource name (table, schema, database...) proven in this request applies in any file
		// (provenance).
		k := extra[:0]
		for _, x := range extra {
			if v := s[x.Start:x.End]; identifierShape(v) || l.provenAtSource(v) || !columnOrIndex(strings.TrimPrefix(x.Type, objTypePrefix)) {
				k = append(k, x)
			}
		}
		extra = k
	}
	extra = l.noCommonsInProse(s, extra)
	if !l.writing && documentSource(l.currentSource) {
		extra = l.noCommonsInDocument(s, extra)
	}
	ts := l.joinSnippets(r.snippets, extra)
	l.currentProv = map[string]bool{}
	for _, d := range r.decidedSet {
		if d.Rule != "anchor" {
			l.currentProv[strings.ToLower(d.Name)] = true
		}
	}
	ts = l.noPublics(s, ts)
	ts2 := l.anchorPositions(s, ts, f)
	if newOnes := l.learnAnchor(s, ts, ts2); len(newOnes) > 0 {
		// the deduced value applies to the other occurrences of this text (rule 2)
		ts2 = l.joinSnippets(ts2, l.noCommonsInProse(s, l.mem.scanF(s, ts2, f)))
		r.decidedSet = append(append([]Decision{}, r.decidedSet...), newOnes...)
		extra = append(extra, snippet{})
	}
	if len(extra) == 0 && len(ts2) == len(r.snippets) && !typeChanged(ts2, r.snippets) {
		return r
	}
	for i := range ts2 {
		if ts2[i].Pseudo == "" {
			ts2[i].Pseudo = l.m.Pseudonym(ts2[i].Type, s[ts2[i].Start:ts2[i].End])
		}
	}
	ts2 = noOverlapLong(l.noPublics(s, ts2)) // a conflict is resolved; never discard everything
	text, entries, ok := rebuild(s, ts2)
	if !ok {
		return r
	}
	return result{text: text, entries: entries, snippets: ts2, gen: r.gen, noLearn: r.noLearn, decidedSet: r.decidedSet}
}

// noCommonsInProse: in prose (message text), the pieces the conversation memory brought that are
// common words come out: there it is the word (vocab_words.go). Those a reader decided in the text
// itself do not go through here.
func (l *Batch) noCommonsInProse(s string, xs []snippet) []snippet {
	if !l.prose {
		return xs
	}
	out := xs[:0:0]
	for _, x := range xs {
		if !l.m.freeCommon(s[x.Start:x.End]) {
			out = append(out, x)
		}
	}
	return out
}

// noCommonsInDocument: in a documentation file (README, agent prompt, notes), the text outside the
// code blocks is prose, like the user message: the common word the memory brought stays a word.
// Inside a code block (``` or ~~~), provenance applies as in any code.
func (l *Batch) noCommonsInDocument(s string, xs []snippet) []snippet {
	if len(xs) == 0 {
		return xs
	}
	fences := codeBlocks(s)
	out := xs[:0:0]
	for _, x := range xs {
		if !insideOf(fences, x.Start) && l.m.freeCommon(s[x.Start:x.End]) {
			continue
		}
		out = append(out, x)
	}
	return out
}

// codeBlocks: the pieces of s between Markdown code fences (``` or ~~~ at the start of the line,
// after the Read line number, if any). A fence without closing goes to the end.
func codeBlocks(s string) [][2]int {
	var out [][2]int
	opened := -1
	for ini := 0; ini < len(s); {
		end := strings.IndexByte(s[ini:], '\n')
		if end < 0 {
			end = len(s)
		} else {
			end += ini
		}
		c := lineContent(s, ini, end)
		if strings.HasPrefix(s[c:end], "```") || strings.HasPrefix(s[c:end], "~~~") {
			if opened < 0 {
				opened = ini
			} else {
				out = append(out, [2]int{opened, end})
				opened = -1
			}
		}
		ini = end + 1
	}
	if opened >= 0 {
		out = append(out, [2]int{opened, len(s)})
	}
	return out
}

// lineContent: where the content of line s[start:end] starts, after the indentation and the line
// number Claude Code's Read puts (" 12→" or "12\t").
func lineContent(s string, ini, end int) int {
	i := ini
	for i < end && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	j := i
	for j < end && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j > i {
		if strings.HasPrefix(s[j:end], "→") {
			return j + len("→")
		}
		if j < end && s[j] == '\t' {
			return j + 1
		}
	}
	for i < end && i-ini < 8 && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

func insideOf(fs [][2]int, p int) bool {
	for _, f := range fs {
		if p >= f[0] && p < f[1] {
			return true
		}
	}
	return false
}

// typeChanged: some piece changed type (anchor by position corrected the generic one).
func typeChanged(a, b []snippet) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i].Type != b[i].Type || a[i].Pseudo != b[i].Pseudo {
			return true
		}
	}
	return false
}

// joinSnippets: the memo pieces and the memory pieces, in order (without pseudonyms in the new
// ones).
func (l *Batch) joinSnippets(base, extra []snippet) []snippet {
	r := struct{ snippets []snippet }{base}
	ts := make([]snippet, 0, len(r.snippets)+len(extra))
	a, b := 0, 0
	for a < len(r.snippets) || b < len(extra) {
		if b >= len(extra) || a < len(r.snippets) && r.snippets[a].Start < extra[b].Start {
			ts = append(ts, r.snippets[a])
			a++
		} else {
			ts = append(ts, extra[b])
			b++
		}
	}
	return ts
}

// Memory builds the conversation memory with the names decided in the texts of the request (after
// Warm, when every new text is already in the memo) and in the translated pieces of the assistant
// texts (extras). Internet content does not enter (it is masked, but does not teach).
func (l *Batch) Memory(items []BatchItem, extras []Decision) {
	itSources := make([]string, len(items))
	for i := range items {
		itSources[i], _ = SplitSource(items[i].Hint)
	}
	items = noSources(items)
	defer func() { l.recordSources(items, itSources) }()
	l.buildMemory(l.itemDecisions(items), extras)
}

// itemDecisions: the decisions of each text (from the frozen one, from the memo or recomputed
// once).
func (l *Batch) itemDecisions(items []BatchItem) [][]Decision {
	m := l.m
	decs := make([][]Decision, len(items))
	var missingCount []warmItem
	var iMissing []int
	for i, it := range items {
		if it.FromWeb || len(it.S) < 4 {
			continue
		}
		if r, ok := m.frozen(it.Pos, it.S); ok && r.decidedSet != nil {
			decs[i] = r.decidedSet
			continue
		}
		if r, ok := m.inMemo(it.S, it.Hint); ok {
			// the result goes straight to assembly (MaskHint), without computing the hash again
			l.pre[it.Pos] = r
			if r.decidedSet != nil {
				decs[i] = r.decidedSet
				continue
			}
		}
		// a text that went out before a restart (enviados.log only has the snippets) or that left
		// the memo: its decisions are recomputed once
		missingCount = append(missingCount, warmItem{it.S, true, it.Hint})
		iMissing = append(iMissing, i)
	}
	if len(missingCount) > 0 {
		m.warm(missingCount)
		for _, i := range iMissing {
			it := items[i]
			decs[i], _ = m.decidedMemo(it.S, it.Hint)
			if decs[i] == nil {
				// it left the memo while the others were being computed (a conversation larger than
				// the memo): compute again, without depending on the memo
				r, _ := m.maskD(it.S, true, it.Hint)
				decs[i] = r.decidedSet
			}
			m.mu.Lock()
			if r, ok := m.frz[it.Pos]; ok && r.decidedSet == nil {
				r.decidedSet = decs[i]
				if r.decidedSet == nil {
					r.decidedSet = []Decision{}
				}
				m.frz[it.Pos] = r
			}
			m.mu.Unlock()
		}
	}
	return decs
}

// DecidedByText: for priority, the names each text decided by itself (readers and deciders of that
// text; never what came from the conversation memory or from accumulated knowledge), per text. A
// text without computed decisions (from the internet) stays out.
func (l *Batch) DecidedByText(items []BatchItem) map[string][]string {
	items = noSources(items)
	decs := l.itemDecisions(items)
	out := make(map[string][]string, len(items))
	for i, it := range items {
		if decs[i] == nil {
			continue
		}
		ns := out[it.S]
		if ns == nil {
			ns = []string{}
		}
		for _, d := range decs[i] {
			ns = append(ns, d.Name)
		}
		out[it.S] = ns
	}
	return out
}

// joinDecisions: the decisions of all texts, each text repeated only once.
func joinDecisions(decs [][]Decision) []Decision {
	var allOf []Decision
	seenOnes := map[*Decision]bool{} // the same repeated text brings the same decisions (from the memo)
	for _, d := range decs {
		if len(d) == 0 || seenOnes[&d[0]] {
			continue
		}
		seenOnes[&d[0]] = true
		allOf = append(allOf, d...)
	}
	return allOf
}

func (l *Batch) buildMemory(decs [][]Decision, extras []Decision) {
	m := l.m
	allOf := append(joinDecisions(decs), extras...)
	if len(allOf) > 0 {
		if len(l.publicSet) > 0 {
			k := allOf[:0:0]
			for _, d := range allOf {
				if !l.fromModel(d.Name) {
					k = append(k, d)
				}
			}
			allOf = k
		}
		l.mem = m.newMemory(allOf)
		for _, d := range allOf {
			if d.Rule == "anchor" {
				l.markDeduced(d.Name)
			}
		}
	}
}

// decidedMemo: the decisions of s in the memo (nil, false: s is not in the memo).
func (m *Masker) decidedMemo(s, hint string) ([]Decision, bool) {
	r, ok := m.inMemo(s, hint)
	if !ok || r.decidedSet == nil {
		return nil, false
	}
	return r.decidedSet, true
}

// inMemo: the result of s in the memo, as maskKE would return it (without detecting again).
func (m *Masker) inMemo(s, hint string) (result, bool) {
	k := memoKey(s, hint)
	m.mu.Lock()
	r, ok := m.memo[k]
	if !ok {
		if r, ok = m.stale[k]; ok {
			r = m.store(k, r)
		}
	}
	m.mu.Unlock()
	if !ok {
		return r, false
	}
	// a value learned after the result was computed that appears in s: redo it (as in maskKE; once
	// checked, the result is marked with the current generation)
	if g := m.known.generation(); r.gen != g {
		if m.known.containsSince(s, r.gen) {
			return r, false
		}
		r.gen = g
		m.mu.Lock()
		r = m.store(k, r)
		m.mu.Unlock()
	}
	return r, true
}

// Written: is des an assistant text unmasked here (is there a record)? Returns the decisions of the
// translated pieces (rule "translated", with the type of the pseudonym), for the memory.
func (l *Batch) Written(des string) ([]Decision, bool) {
	if len(des) < 4 {
		return nil, false
	}
	e, ok := l.m.writtenOf(des)
	if !ok {
		return nil, false
	}
	return translatedDecisions(des, e.ts), true
}

func translatedDecisions(des string, ts []snippet) []Decision {
	out := []Decision{}
	for _, t := range ts {
		if !isObject(t.Type) {
			continue
		}
		v := des[t.Start:t.End]
		out = append(out, Decision{Name: v, Ent: strings.TrimPrefix(t.Type, objTypePrefix), Rule: "translated", Generic: isGeneric(v)})
	}
	return out
}

// MaskHintProse: like MaskHint, for the text the user wrote in the message (not tool output): a
// common word that arrives by contagion stays a word; one a reader decides right there (a pasted
// DDL, kubectl -n) stays a name (vocab_words.go).
func (l *Batch) MaskHintProse(s string, fromWeb bool, pos Position, hint string) (string, []Entry) {
	l.prose = true
	defer func() { l.prose = false }()
	return l.MaskHint(s, fromWeb, pos, hint)
}

// MaskProse: like MaskWritten, for a text block of the response (not tool input): a common word
// decided elsewhere stays a word (vocab_words.go).
func (l *Batch) MaskProse(des string, fromWeb bool, pos Position, hint string) (string, []Entry) {
	l.prose = true
	defer func() { l.prose = false }()
	return l.MaskWritten(des, fromWeb, pos, hint)
}

// MaskWritten: like MaskHint, for an assistant text. With a record (see RecordResponse), what the
// API sent goes out: only the translated pieces become pseudonyms again. Without a record (restart
// without enviados.log, another process), it is a text like the others.
func (l *Batch) MaskWritten(des string, fromWeb bool, pos Position, hint string) (string, []Entry) {
	if len(des) < 4 {
		return des, nil
	}
	if r, ok := l.m.frozen(pos, des); ok {
		return r.text, r.entries
	}
	if e, ok := l.m.writtenOf(des); ok && !fromWeb {
		if text, entries, valid := rebuild(des, e.ts); valid {
			r := result{text: text, entries: entries, snippets: e.ts, decidedSet: translatedDecisions(des, e.ts)}
			if _, already := l.outputs[pos]; !already {
				l.outputs[pos] = r
			}
			return r.text, r.entries
		}
	}
	if !fromWeb {
		return l.MaskContagion(des, fromWeb, pos) // rule 1: the assistant is not a source
	}
	return l.MaskHint(des, fromWeb, pos, hint)
}
