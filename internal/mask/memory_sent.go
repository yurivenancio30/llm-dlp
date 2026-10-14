package mask

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
)

// Sent: what each text already sent to the API carried in place of the real data, so it goes out
// the same on resends, including after a restart.
//
// Claude Code resends the whole conversation with every message, and the API only uses the cache if
// the start of the request is identical to the previous one. If a value learned later changed an
// old text, the whole conversation would be rewritten in the cache, protecting nothing (the old
// text had already gone out with the value in the clear). So a text that went out stays frozen.
//
// The record is per position in the conversation (see Position): only the exact resend stays
// frozen.
//
// On disk there is only: the HMAC (with the key) of the position plus the text and, for each
// swapped piece, where it is in the text, the type and the pseudonym (what the API received). No
// real value.

type snippet struct {
	Start, End   int
	Type, Pseudo string
}

// Sent keeps the records in RAM and appends the new ones to the file (one JSON line each).
type Sent struct {
	path        string
	fingerprint string // config + version: if it changes, the records are no longer valid
	mu          sync.Mutex
	reg         map[string]*sentRecord
	usage       int64
	pend        [][]byte // lines not yet written
	lines       int      // lines in the file (repeated records count)
}

type sentRecord struct {
	ts    []snippet
	usage int64
}

type sentLine struct {
	ID string  `json:"i,omitempty"`
	T  [][]any `json:"t,omitempty"`
	// only on the first line
	Fingerprint string `json:"impressao,omitempty"`
}

// maxSent: ceiling of stored texts. Past it, the tenth used longest ago goes.
var maxSent = 400_000

// LoadSent reads the file. If it is from another configuration or another llm-dlp version, it
// starts over empty: the text is masked with the current rules (the conversation is rewritten in
// the cache once).
func LoadSent(path, fingerprint string) (*Sent, error) {
	e := &Sent{path: path, fingerprint: fingerprint, reg: map[string]*sentRecord{}}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return e, e.rewrite()
	}
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	firstOne, valid := true, false
	for sc.Scan() {
		var l sentLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue // broken line (interrupted write): the text is masked again
		}
		if firstOne {
			firstOne, valid = false, l.Fingerprint == fingerprint
			if !valid {
				break
			}
			continue
		}
		if ts, ok := lineSnippets(l.T); ok && l.ID != "" {
			e.usage++
			e.reg[l.ID] = &sentRecord{ts, e.usage}
			e.lines++
		}
	}
	f.Close()
	if err := sc.Err(); err != nil {
		valid = false
	}
	if !valid {
		e.reg, e.usage = map[string]*sentRecord{}, 0
	}
	if !valid || len(e.reg) > maxSent || e.lines > 2*len(e.reg)+1000 {
		e.prune()
		return e, e.rewrite()
	}
	return e, nil
}

func lineSnippets(t [][]any) ([]snippet, bool) {
	out := make([]snippet, 0, len(t))
	for _, x := range t {
		if len(x) != 4 {
			return nil, false
		}
		ini, ok1 := x[0].(float64)
		end, ok2 := x[1].(float64)
		tp, ok3 := x[2].(string)
		ps, ok4 := x[3].(string)
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return nil, false
		}
		out = append(out, snippet{int(ini), int(end), tp, ps})
	}
	return out, true
}

// Lookup returns the pieces the text was sent with at that position (by the HMAC).
func (e *Sent) Lookup(id string) ([]snippet, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, ok := e.reg[id]
	if !ok {
		return nil, false
	}
	e.usage++
	r.usage = e.usage
	return r.ts, true
}

// Record records a text that just went out (it goes to the file on the next Save).
func (e *Sent) Record(id string, ts []snippet) {
	t := make([][]any, len(ts))
	for i, x := range ts {
		t[i] = []any{x.Start, x.End, x.Type, x.Pseudo}
	}
	b, _ := json.Marshal(sentLine{ID: id, T: t})
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.reg[id]; ok {
		return
	}
	e.usage++
	e.reg[id] = &sentRecord{ts, e.usage}
	e.pend = append(e.pend, b)
}

// Save appends to the file what was recorded since the last time. Past the ceiling, it removes
// those used longest ago and rewrites the file.
func (e *Sent) Save() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.reg) > maxSent {
		e.prune()
		return e.rewrite()
	}
	if len(e.pend) == 0 {
		return nil
	}
	f, err := os.OpenFile(e.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, l := range e.pend {
		w.Write(l)
		w.WriteByte('\n')
	}
	err = w.Flush()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	e.lines += len(e.pend)
	e.pend = nil
	return err
}

// prune leaves at most 90% of the ceiling, keeping the most recently used ones (with e.mu locked or
// before Sent is shared).
func (e *Sent) prune() {
	if len(e.reg) <= maxSent {
		return
	}
	ids := make([]string, 0, len(e.reg))
	for id := range e.reg {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return e.reg[ids[i]].usage > e.reg[ids[j]].usage })
	for _, id := range ids[maxSent*9/10:] {
		delete(e.reg, id)
	}
}

// rewrite writes the whole file (header + records, from oldest to newest).
func (e *Sent) rewrite() error {
	ids := make([]string, 0, len(e.reg))
	for id := range e.reg {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return e.reg[ids[i]].usage < e.reg[ids[j]].usage })
	tmp := e.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	b, _ := json.Marshal(sentLine{Fingerprint: e.fingerprint})
	w.Write(append(b, '\n'))
	for _, id := range ids {
		r := e.reg[id]
		t := make([][]any, len(r.ts))
		for i, x := range r.ts {
			t[i] = []any{x.Start, x.End, x.Type, x.Pseudo}
		}
		b, _ := json.Marshal(sentLine{ID: id, T: t})
		w.Write(append(b, '\n'))
	}
	err = w.Flush()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	e.lines, e.pend = len(ids), nil
	return os.Rename(tmp, e.path)
}
