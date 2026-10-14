package mask

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Seen: the same values, kept on disk only as hashes, so they apply after a restart.

// Seen keeps on disk, only as hashes (HMAC with the key), the contextual values already masked and
// the type of each one.
type Seen struct {
	path  string
	mu    sync.RWMutex
	ids   map[string]string // id -> tipo
	size  map[int]bool      // lengths of the stored passwords (filter: only tests pieces of these lengths)
	n     map[string]int    // how many of each type
	nObj  int               // how many object names
	dirty bool
	// noSize: there are passwords stored without the length (old file)
	noSize    bool
	saved     time.Time
	scheduled bool
	// disk: the file as this process left it (read or written). If it changed, another process
	// wrote it (the new version during a swap, see update; a command): what it learned is merged
	// before writing, and one write does not erase the other.
	disk fileFingerprint
}

type fileFingerprint struct {
	mod  time.Time
	size int64
}

func fingerprintOf(path string) fileFingerprint {
	st, err := os.Stat(path)
	if err != nil {
		return fileFingerprint{}
	}
	return fileFingerprint{st.ModTime(), st.Size()}
}

// writeSwapping writes b to a temporary file of this process and swaps it for the file (two
// processes writing at the same time do not mix one temporary file with the other). Returns the
// fingerprint of what it wrote (the rename changes neither date nor size).
func writeSwapping(path string, b []byte) (fileFingerprint, error) {
	tmp := path + ".tmp-" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		os.Remove(tmp)
		return fileFingerprint{}, err
	}
	imp := fingerprintOf(tmp)
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fileFingerprint{}, err
	}
	return imp, nil
}

// sizePrefix: the lengths go in the same file, as keys "tam:13"
const sizePrefix = "tam:"

var saveInterval = 5 * time.Second

func (v *Seen) HasSize(n int) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.noSize || v.size[n]
}

func (v *Seen) MarkSize(n int) {
	v.mu.Lock()
	if !v.size[n] {
		v.size[n], v.dirty = true, true
		v.ids[sizePrefix+strconv.Itoa(n)] = "tam"
	}
	v.mu.Unlock()
}

func LoadSeen(path string) (*Seen, error) {
	v := &Seen{path: path, ids: map[string]string{}, n: map[string]int{}, size: map[int]bool{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return nil, err
	}
	v.disk = fingerprintOf(path)
	if json.Unmarshal(b, &v.ids) != nil {
		v.ids = map[string]string{} // old format (list): start over; the values are learned again
	}
	for id, t := range v.ids {
		v.tally(id, t)
	}
	// a file from before lengths were stored: without the filter (tests every piece)
	v.noSize = v.n["segredo"] > 0 && len(v.size) == 0
	return v, nil
}

// tally puts into the counters and the length filter an entry that is already in v.ids.
func (v *Seen) tally(id, t string) {
	if n, err := strconv.Atoi(strings.TrimPrefix(id, sizePrefix)); err == nil && strings.HasPrefix(id, sizePrefix) {
		v.size[n] = true
		return
	}
	v.n[baseType(t)]++
	if strings.HasPrefix(t, "obj.") {
		v.nObj++
	}
}

// mergeFromDisk (with v.mu locked): if another process wrote the file after this one, brings what
// only it learned. An entry this process pruned (ceiling) may come back this way; the next prune
// removes it again.
func (v *Seen) mergeFromDisk() {
	if fingerprintOf(v.path) == v.disk {
		return
	}
	b, err := os.ReadFile(v.path)
	if err != nil {
		return
	}
	var other map[string]string
	if json.Unmarshal(b, &other) != nil {
		return
	}
	for id, t := range other {
		if _, ok := v.ids[id]; !ok {
			v.ids[id] = t
			v.tally(id, t)
		}
	}
}

func (v *Seen) IsEmpty() bool { v.mu.RLock(); defer v.mu.RUnlock(); return len(v.ids) == 0 }

func (v *Seen) Type(id string) (string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	t, ok := v.ids[id]
	return t, ok
}

// Has says whether some value of this type is stored.
func (v *Seen) Has(typ string) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.n[typ] > 0
}

// Objects (see objects.go): the value keeps the type and two dates, in days since 1970:
// "obj.tabela|<learned>|<last seen>". The ceiling is separate, so object names do not push out
// passwords and documents.
const maxObjects = 500_000

func baseType(t string) string {
	if i := strings.IndexByte(t, '|'); i >= 0 {
		return t[:i]
	}
	return t
}

func (v *Seen) HasObj() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.nObj > 0
}

// Obj returns the entity type and the day the name was last seen.
func (v *Seen) Obj(id string) (ent string, seen int, ok bool) {
	v.mu.RLock()
	t, ok := v.ids[id]
	v.mu.RUnlock()
	if !ok {
		return "", 0, false
	}
	ent, _, seen, ok = readObj(t)
	return ent, seen, ok
}

func readObj(t string) (ent string, learned, seen int, ok bool) {
	p := strings.Split(t, "|")
	if len(p) != 3 || !strings.HasPrefix(p[0], "obj.") {
		return "", 0, 0, false
	}
	a, e1 := strconv.Atoi(p[1])
	b, e2 := strconv.Atoi(p[2])
	return p[0][4:], a, b, e1 == nil && e2 == nil
}

// MarkObj stores the name (or updates the day it was seen, at most once a day).
func (v *Seen) MarkObj(id, ent string, day int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if t, ok := v.ids[id]; ok {
		e, a, seen, ok := readObj(t)
		if !ok || seen >= day {
			return
		}
		v.ids[id], v.dirty = "obj."+e+"|"+strconv.Itoa(a)+"|"+strconv.Itoa(day), true
		return
	}
	if v.nObj >= maxObjects {
		v.pruneObj()
	}
	v.ids[id], v.dirty = "obj."+ent+"|"+strconv.Itoa(day)+"|"+strconv.Itoa(day), true
	v.n["obj."+ent]++
	v.nObj++
}

// pruneObj removes the tenth of the object names seen longest ago (with v.mu locked).
func (v *Seen) pruneObj() {
	days := make([]int, 0, v.nObj)
	for _, t := range v.ids {
		if _, _, d, ok := readObj(t); ok {
			days = append(days, d)
		}
	}
	sort.Ints(days)
	cut := days[len(days)/10]
	for id, t := range v.ids {
		if e, _, d, ok := readObj(t); ok && d <= cut {
			delete(v.ids, id)
			v.n["obj."+e]--
			v.nObj--
		}
	}
}

// maxSeen: ceiling of the stored hashes (~40 bytes each). Past it, a tenth goes, at random; those
// values go back to depending on the word next to them until they are seen again.
const maxSeen = 500_000

func (v *Seen) Mark(id, typ string) {
	v.mu.Lock()
	if _, ok := v.ids[id]; !ok {
		if len(v.ids)-v.nObj >= maxSeen {
			newOne, n := make(map[string]string, maxSeen), map[string]int{}
			for k, t := range v.ids { // the order of a Go map is random
				if strings.HasPrefix(k, sizePrefix) || strings.HasPrefix(t, "obj.") {
					newOne[k] = t
					continue
				}
				if len(newOne) >= maxSeen*9/10 {
					continue
				}
				newOne[k] = t
				n[t]++
			}
			for k, c := range v.n {
				if strings.HasPrefix(k, "obj.") {
					n[k] = c
				}
			}
			v.ids, v.n = newOne, n
		}
		v.ids[id], v.dirty = typ, true
		v.n[typ]++
	}
	v.mu.Unlock()
}

// SaveIfDirty writes the file if there was something new since the last write. With many hashes the
// write is costly (≈0.3 s with 250 thousand), so it happens at most once every saveInterval; what
// was left pending is written by a timer.
func (v *Seen) SaveIfDirty() error {
	v.mu.Lock()
	if !v.dirty {
		v.mu.Unlock()
		return nil
	}
	if missingOne := saveInterval - time.Since(v.saved); missingOne > 0 {
		if !v.scheduled {
			v.scheduled = true
			time.AfterFunc(missingOne, func() {
				v.mu.Lock()
				v.scheduled = false
				v.mu.Unlock()
				v.SaveIfDirty()
			})
		}
		v.mu.Unlock()
		return nil
	}
	v.saved = time.Now()
	v.mergeFromDisk()
	b, _ := json.Marshal(v.ids)
	v.dirty = false
	v.mu.Unlock()
	imp, err := writeSwapping(v.path, b)
	if err != nil {
		return err
	}
	v.mu.Lock()
	v.disk = imp
	v.mu.Unlock()
	return nil
}

// SaveNow writes what is pending right away, without waiting for the interval (when the proxy
// exits).
func (v *Seen) SaveNow() error {
	v.mu.Lock()
	v.saved = time.Time{}
	v.mu.Unlock()
	return v.SaveIfDirty()
}
