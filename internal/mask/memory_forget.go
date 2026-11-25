package mask

import "sort"

// Forgetting (llm-dlp forget): a value learned by mistake stops being applied by the accumulated
// memory. On disk there is only the hash, so the value given is hashed again under every type it
// could have been learned with, and the entry becomes a tombstone: it is no longer applied and is
// not learned again, and a merge with another process's file does not bring it back (see
// mergeFromDisk). The readers still mask it where the structure proves it is a name: forgetting
// takes away the spreading, never the proof. What already went out stays frozen (sent.log).

// forgotten: the type of a tombstone.
const forgotten = "forgotten"

// Forget turns the entries of each value into tombstones and returns how many entries there were.
func (m *Masker) Forget(values []string) int {
	if m.seenSet == nil {
		return 0
	}
	ents := make([]string, 0, len(ObjectPrefixes)+1)
	for e := range ObjectPrefixes {
		ents = append(ents, e)
	}
	ents = append(ents, "object")
	types := make([]string, 0, len(contextual))
	for t := range contextual {
		types = append(types, t)
	}
	sort.Strings(ents)
	sort.Strings(types)
	var ids []string
	for _, v := range values {
		if v == "" {
			continue
		}
		for _, e := range ents {
			ids = append(ids, m.objID(canonObj(e, v)))
		}
		for _, t := range types {
			ids = append(ids, m.p.ID("visto", canonicalKey(t, v)))
		}
	}
	return m.seenSet.Forget(ids)
}

// Forget turns the entries in ids into tombstones; returns how many it found.
func (v *Seen) Forget(ids []string) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	n := 0
	for _, id := range ids {
		if t, ok := v.ids[id]; ok && t != forgotten {
			v.untally(t)
			v.ids[id], v.dirty = forgotten, true
			n++
		}
	}
	return n
}

// untally takes an entry out of the counters (the reverse of tally, without the length filter).
func (v *Seen) untally(t string) {
	v.n[baseType(t)]--
	if len(t) > 4 && t[:4] == "obj." {
		v.nObj--
	}
}

// Forgotten: the entry was forgotten (a tombstone): the value is not learned again.
func (v *Seen) Forgotten(id string) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.ids[id] == forgotten
}
