package mask

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

// People: registry (hashes only) of names, e-mails and codes of known people.

// People keeps, ONLY AS HASHES (HMAC with the key), the names, e-mails and codes of known people
// and which person each one belongs to. It serves to (1) find a loose name in the text and (2) give
// the same identifier to the name and the e-mail of the same person. No real value stays in the
// file.
type People struct {
	path       string
	mu         sync.RWMutex
	dirty      bool
	saved      time.Time
	codeCache  sync.Map // word -> is it someone's code? (RAM only)
	nCodeCache atomic.Int64
	disk       fileFingerprint // the file as this process left it (see Seen.disk)
	D          peopleData
}

type peopleData struct {
	Names  map[string]string `json:"names"`  // id(name) -> pid
	Emails map[string]string `json:"emails"` // id(email) -> pid
	Codes  map[string]string `json:"codes"`  // id(code) -> pid
	// id(first name): a cheap filter to find names in any spelling ("joão silva", "JOAO SILVA")
	// without testing every word sequence of the text
	FirstNames map[string]bool `json:"first_names"`
	// lengths of the user codes: a cheap filter (only words of these lengths are checked).
	// Empty in old registries: then every word is checked.
	CodeLengths map[int]bool `json:"code_lengths,omitempty"`
}

// UnmarshalJSON also reads the field names of the registry written up to 0.9 (pessoas.json, copied
// to people.json by config.DataPath).
func (d *peopleData) UnmarshalJSON(b []byte) error {
	type plain peopleData
	if err := json.Unmarshal(b, (*plain)(d)); err != nil {
		return err
	}
	var old struct {
		Names       map[string]string `json:"nomes"`
		Codes       map[string]string `json:"codigos"`
		FirstNames  map[string]bool   `json:"primeiros"`
		CodeLengths map[int]bool      `json:"tam_codigos"`
	}
	if err := json.Unmarshal(b, &old); err != nil {
		return err
	}
	if len(d.Names) == 0 && len(old.Names) > 0 {
		d.Names = old.Names
	}
	if len(d.Codes) == 0 && len(old.Codes) > 0 {
		d.Codes = old.Codes
	}
	if len(d.FirstNames) == 0 && len(old.FirstNames) > 0 {
		d.FirstNames = old.FirstNames
	}
	if len(d.CodeLengths) == 0 && len(old.CodeLengths) > 0 {
		d.CodeLengths = old.CodeLengths
	}
	return nil
}

func LoadPeople(path string) (*People, error) {
	ps := &People{path: path}
	ps.D.Names, ps.D.Emails, ps.D.Codes = map[string]string{}, map[string]string{}, map[string]string{}
	ps.D.FirstNames = map[string]bool{}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ps, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &ps.D); err != nil {
		return nil, err
	}
	ps.disk = fingerprintOf(path)
	return ps, nil
}

// Save writes the registry. If another process wrote it after this one (import-people with the
// proxy running; the new version during a swap), it first merges what only the other has: the
// registry only grows, and one write does not erase the other.
func (ps *People) Save() error {
	ps.mu.Lock()
	ps.mergeFromDisk()
	b, _ := json.Marshal(ps.D)
	ps.mu.Unlock()
	imp, err := writeSwapping(ps.path, b)
	if err != nil {
		return err
	}
	ps.mu.Lock()
	ps.disk = imp
	ps.mu.Unlock()
	return nil
}

// mergeFromDisk: with ps.mu locked. An id both have stays with this process's person.
func (ps *People) mergeFromDisk() {
	if fingerprintOf(ps.path) == ps.disk {
		return
	}
	b, err := os.ReadFile(ps.path)
	if err != nil {
		return
	}
	var other People
	if json.Unmarshal(b, &other.D) != nil {
		return
	}
	// no lengths = an old registry, which checks every word: if one of the sides is like that, the
	// result stays without the filter (checking more never lets a code through)
	noFilter := len(ps.D.Codes) > 0 && len(ps.D.CodeLengths) == 0 ||
		len(other.D.Codes) > 0 && len(other.D.CodeLengths) == 0
	join := func(mine *map[string]string, its map[string]string) {
		for id, pid := range its {
			if *mine == nil {
				*mine = map[string]string{}
			}
			if _, ok := (*mine)[id]; !ok {
				(*mine)[id] = pid
			}
		}
	}
	join(&ps.D.Names, other.D.Names)
	join(&ps.D.Emails, other.D.Emails)
	join(&ps.D.Codes, other.D.Codes)
	for id := range other.D.FirstNames {
		if ps.D.FirstNames == nil {
			ps.D.FirstNames = map[string]bool{}
		}
		ps.D.FirstNames[id] = true
	}
	if noFilter {
		ps.D.CodeLengths = nil
	} else {
		for n := range other.D.CodeLengths {
			if ps.D.CodeLengths == nil {
				ps.D.CodeLengths = map[int]bool{}
			}
			ps.D.CodeLengths[n] = true
		}
	}
	ps.codeCache.Clear() // new codes: a "not a code" stored before may no longer hold
	ps.nCodeCache.Store(0)
}

// SaveNow writes what is pending right away, without waiting for the interval (when the proxy
// exits).
func (ps *People) SaveNow() error {
	ps.mu.Lock()
	dirty := ps.dirty
	ps.dirty = false
	ps.mu.Unlock()
	if !dirty {
		return nil
	}
	return ps.Save()
}

// SaveIfDirty writes the registry if someone new came in (at most once every 5 s).
func (ps *People) SaveIfDirty() error {
	ps.mu.Lock()
	if !ps.dirty || time.Since(ps.saved) < saveInterval {
		ps.mu.Unlock()
		return nil
	}
	ps.dirty, ps.saved = false, time.Now()
	ps.mu.Unlock()
	return ps.Save()
}

// IsCode: is v the code of someone in the registry? It keeps the answer per word (the same words
// repeat a lot, and computing the hash is the expensive part).
func (ps *People) IsCode(p *Pseudo, v string) bool {
	// same rule as the import, for registries made before it: a short code or one without a digit
	// is not looked for in the text
	if len(v) < 4 || !hasDigit(v) {
		return false
	}
	ps.mu.RLock()
	outside := len(ps.D.CodeLengths) > 0 && !ps.D.CodeLengths[len(v)]
	ps.mu.RUnlock()
	if outside {
		return false
	}
	if r, ok := ps.codeCache.Load(v); ok {
		return r.(bool)
	}
	r := ps.PID(p, "code", NormName(v)) != ""
	if ps.nCodeCache.Add(1) > 200_000 {
		ps.codeCache.Clear()
		ps.nCodeCache.Store(0)
	}
	ps.codeCache.Store(v, r)
	return r
}

func (ps *People) HasCodes() bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.D.Codes) > 0
}

func (ps *People) Total() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.D.Names)
}

// Import registers a person. Empty values are ignored. Single-word names do not enter detection
// (they are usually teams or systems, not people).
func (ps *People) Import(p *Pseudo, name, email, codeValue string) string {
	name, email, codeValue = NormName(name), strings.ToLower(strings.TrimSpace(email)), NormName(codeValue)
	// a short code or one without a digit ("TBD", "N/A", "SIM") identifies nobody and, looked for
	// in the text, would mask common words
	if len(codeValue) < 4 || !hasDigit(codeValue) {
		codeValue = ""
	}
	if codeValue != "" {
		ps.codeCache.Clear()
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	var ids []struct {
		m  map[string]string
		id string
	}
	if email != "" {
		ids = append(ids, struct {
			m  map[string]string
			id string
		}{ps.D.Emails, p.ID("email", email)})
	}
	if codeValue != "" {
		if ps.D.CodeLengths == nil && len(ps.D.Codes) == 0 {
			ps.D.CodeLengths = map[int]bool{}
		}
		if ps.D.CodeLengths != nil && !ps.D.CodeLengths[len(codeValue)] {
			ps.D.CodeLengths[len(codeValue)], ps.dirty = true, true
		}
		ids = append(ids, struct {
			m  map[string]string
			id string
		}{ps.D.Codes, p.ID("codigo", codeValue)})
	}
	var variants []string
	if parts := strings.Fields(name); len(parts) >= 2 {
		variants = append(variants, name)
		if len(parts) >= 3 {
			variants = append(variants, parts[0]+" "+parts[len(parts)-1])
		}
	}
	for _, v := range variants {
		ids = append(ids, struct {
			m  map[string]string
			id string
		}{ps.D.Names, p.ID("nome", v)})
		if ps.D.FirstNames == nil {
			ps.D.FirstNames = map[string]bool{}
		}
		ps.D.FirstNames[p.ID("primeiro", strings.Fields(v)[0])] = true
	}
	if len(ids) == 0 {
		return ""
	}
	pid := ""
	for _, x := range ids {
		if v, ok := x.m[x.id]; ok {
			pid = v
			break
		}
	}
	if pid == "" {
		pid = ids[0].id
	}
	for _, x := range ids {
		if _, ok := x.m[x.id]; !ok {
			x.m[x.id] = pid
			ps.dirty = true
		}
	}
	return pid
}

// PID returns the identifier of the person who owns the value, or "" if unknown.
func (ps *People) PID(p *Pseudo, typ, normValue string) string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	switch typ {
	case "email":
		return ps.D.Emails[p.ID("email", normValue)]
	case "name":
		return ps.D.Names[p.ID("nome", normValue)]
	case "code":
		return ps.D.Codes[p.ID("codigo", normValue)]
	}
	return ""
}

var connectors = map[string]bool{"da": true, "de": true, "do": true, "das": true, "dos": true, "e": true}

type word struct {
	ini, end  int
	upperCase bool
	connector bool
}

// FindNames looks for sequences of 2 to 6 name-shaped words ("João da Silva", "JOAO SILVA") and
// checks the hash of each one in the registry.
func (ps *People) FindNames(s string, p *Pseudo) []Finding {
	if ps.Total() == 0 {
		return nil
	}
	var ws []word
	ini := -1
	for i, r := range s + " " {
		letter := unicode.IsLetter(r) || ((r == '\'' || r == '-') && ini >= 0)
		if letter && ini < 0 {
			ini = i
		} else if !letter && ini >= 0 {
			w := s[ini:i]
			r0 := []rune(w)[0]
			ws = append(ws, word{ini, i, unicode.IsUpper(r0), connectors[w]})
			ini = -1
		}
	}
	ps.mu.RLock()
	byFirst := len(ps.D.FirstNames) > 0
	ps.mu.RUnlock()
	start := func(i int) bool {
		if !byFirst { // old registry, without the filter: only sequences with capital initials
			return ws[i].upperCase
		}
		if ws[i].end-ws[i].ini < 2 {
			return false
		}
		ps.mu.RLock()
		defer ps.mu.RUnlock()
		return ps.D.FirstNames[p.ID("primeiro", NormName(s[ws[i].ini:ws[i].end]))]
	}
	var out []Finding
	for i := 0; i < len(ws); i++ {
		if !start(i) {
			continue
		}
		foundIt := 0
		for n := 6; n >= 2 && foundIt == 0; n-- {
			j := i + n - 1
			if j >= len(ws) || (!byFirst && !ws[j].upperCase) || !sameLine(s, ws, i, j) {
				continue
			}
			ok := true
			for k := i + 1; k < j && !byFirst; k++ {
				if !ws[k].upperCase && !ws[k].connector {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			if ps.PID(p, "name", NormName(s[ws[i].ini:ws[j].end])) != "" {
				out = append(out, Finding{ws[i].ini, ws[j].end, "name", s[ws[i].ini:ws[j].end]})
				foundIt = n
			}
		}
		if foundIt > 0 {
			i += foundIt - 1
		}
	}
	return out
}

// sameLine: between words i..j there are only single spaces (no line break or punctuation).
func sameLine(s string, ws []word, i, j int) bool {
	for k := i; k < j; k++ {
		gap := s[ws[k].end:ws[k+1].ini]
		if strings.TrimLeft(gap, " \t") != "" {
			return false
		}
	}
	return true
}
