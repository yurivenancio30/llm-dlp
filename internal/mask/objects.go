package mask

import (
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Objects: names of internal resources (server, database, schema, table, column, procedure, index,
// user, namespace, service, bucket, queue), recognized by the structure of the content (see
// docs/structures.md). This file is the common base: types, pseudonym, learning with brakes and
// on/off per type. The readers of each format deliver their findings through m.readers.

// ObjectPrefixes: entity type -> pseudonym prefix.
var ObjectPrefixes = map[string]string{"server": "HOST", "database": "DB", "schema": "SCH", "table": "T",
	"column": "C", "procedure": "PROC", "index": "IDX", "user": "USR", "namespace": "NS",
	"service": "SVC", "bucket": "BKT", "queue": "TOP",
	"repository": "REPO", "organization": "ORG", "package": "PKG", "folder": "DIR", "cloud_account": "ACC"}

// defaultPropagates: types that, once learned, are masked also outside the structural position.
// Column and index are not: names like user_id and created_at exist in all code.
var defaultPropagates = map[string]bool{"server": true, "database": true, "schema": true, "table": true,
	"procedure": true, "user": true, "namespace": true, "service": true, "bucket": true, "queue": true,
	"repository": true, "organization": true, "package": true, "folder": true, "cloud_account": true}

const objTypePrefix = "obj." // the internal type of an object: "obj.table"

func isObject(typ string) bool { return strings.HasPrefix(typ, objTypePrefix) }

// ObjFinding: an object name found by a structure reader.
type ObjFinding struct {
	Start, End int
	Ent        string // entity type (ObjectPrefixes key)
	Rule       string // rule name, for counting and for the "2 different rules" evidence
	Strong     bool   // unambiguous position: teaches by itself
}

// Reader looks for object names in s, by structure.
type Reader struct {
	Name   string
	Find   func(s string, add func(ObjFinding))
	Public func(v string) bool // the format's vocabulary: never masked nor learned
}

// objMask / objPropagates: liga/desliga por tipo (config "objetos").
func (m *Masker) objMask(ent string) bool {
	o := m.cfg.Objects
	if !o.Enabled {
		return false
	}
	if v, ok := o.Mask[ent]; ok {
		return v
	}
	return true
}

func (m *Masker) objPropagates(ent string) bool {
	if !m.objMask(ent) {
		return false
	}
	if v, ok := m.cfg.Objects.Propagate[ent]; ok {
		return v
	}
	return defaultPropagates[ent]
}

// entSQL: case-insensitive types: the SQL ones (unquoted identifiers are case-insensitive in every
// dialect) and server (a DNS name is case-insensitive: "sqlprd01" and "SQLPRD01" are the same
// machine). In the other types (path, bucket, queue...) the spelling counts as it is:
// "/dados/Relatorios" and "/dados/relatorios" are two folders.
var entSQL = map[string]bool{"database": true, "schema": true, "table": true, "column": true, "procedure": true,
	"index": true, "server": true}

// unquoted: the name without the brackets, quotes or backticks around it.
func unquoted(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		switch {
		case v[0] == '[' && v[len(v)-1] == ']', v[0] == '"' && v[len(v)-1] == '"', v[0] == '`' && v[len(v)-1] == '`':
			v = v[1 : len(v)-1]
		}
	}
	return v
}

// normObj: the form of the name that identifies the object (unquoted; lowercase in the SQL types).
func normObj(ent, v string) string {
	v = unquoted(v)
	if entSQL[ent] {
		return strings.ToLower(v)
	}
	return v
}

// canonObj: the key of the name in the memory and in vistos.json. "o:" = caseless (SQL), "O:" =
// exact.
func canonObj(ent, v string) string {
	if entSQL[ent] {
		return "o:" + strings.ToLower(unquoted(v))
	}
	return "O:" + unquoted(v)
}

// pseudoObject: the type prefix + the ID of the key's HMAC (the same length as the other types).
// Stable across conversations and restarts. A lowercase name gets the prefix in lowercase
// ("t_..."), to match the style of the text; an uppercase one, the prefix in uppercase. A
// mixed-case name (Pedido_Item) gets the prefix with only the first letter capitalized and the
// letters of the ID in the case the HMAC of the spelling itself says (with the key, like the rest
// of the pseudonym: an unkeyed hash would let a guessed name be checked by the case): the same SQL
// name in two spellings in the same text ("ContAB" and "ContAb", which are the same object) goes
// out with two pseudonyms, and the return gives each spelling back as it was.
func (m *Masker) pseudoObject(ent, real string) string {
	pfx := ObjectPrefixes[ent]
	if pfx == "" {
		pfx = "OBJ"
	}
	id := m.p.ID("objeto", hmacDomain(ent)+"\x00"+normObj(ent, real))
	switch {
	case strings.ToLower(real) == real:
		pfx = strings.ToLower(pfx)
	case strings.ToUpper(real) == real:
	default:
		pfx = pfx[:1] + strings.ToLower(pfx[1:])
		h := m.p.Bits("caixa", real, 8)
		b := []byte(id)
		upper := false
		for i := range b {
			if b[i] >= 'a' && b[i] <= 'z' && h[i]&1 == 1 {
				b[i] -= 32
				upper = true
			}
		}
		for i := 0; !upper && i < len(b); i++ { // at least one: not to be confused with the uppercase one
			if b[i] >= 'a' && b[i] <= 'z' {
				b[i] -= 32
				upper = true
			}
		}
		id = string(b)
	}
	ps := pfx + "_" + id
	registerPseudo(ps)
	return ps
}

// rePseudoObj: the SHAPE of an object pseudonym. The shape alone is not enough to skip a name
// ("t_customer" is a real table with that look): see isPseudoObj.
var rePseudoObj = regexp.MustCompile(`^(?i:host|db|sch|t|c|proc|idx|usr|ns|svc|bkt|top|repo|org|pkg|dir|acc|obj)_[a-zA-Z2-7]{8}$`)

// object pseudonyms generated in this process (lowercase): only those are skipped.
var (
	generatedPseudos sync.Map
	nPseudos         atomic.Int64
)

const maxGeneratedPseudos = 500_000

func registerPseudo(ps string) {
	k := strings.ToLower(ps)
	if _, ok := generatedPseudos.LoadOrStore(k, true); !ok && nPseudos.Add(1) > maxGeneratedPseudos {
		generatedPseudos.Clear()
		nPseudos.Store(0)
	}
}

// isPseudoObj: is v an object pseudonym generated by us (in any case)?
func isPseudoObj(v string) bool {
	if !rePseudoObj.MatchString(v) {
		return false
	}
	_, ok := generatedPseudos.Load(strings.ToLower(v))
	return ok
}

// identifierShape: the brake that tells a resource name from a common word. Only names like this
// are learned and propagated: they have "_", a digit, a dot, a hyphen between parts or mixed case
// ("tb_pedido", "srv01", "app.config", "svc-pedidos", "contaCorrente"). Versions, UUIDs and hashes
// stay out: they have digits but are not names.
func identifierShape(v string) bool {
	if len(v) < 3 || len(v) > 128 || hasDigit(v) && reVersionOrHash.MatchString(v) {
		return false
	}
	letter := false
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			letter = true
		}
	}
	if !letter {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == '_' || c == '.' || c >= '0' && c <= '9':
			return true
		case c == '-' && i > 0 && i+1 < len(v) && isAlnum(v[i-1]) && isAlnum(v[i+1]):
			return true
		case c >= 'A' && c <= 'Z' && i > 0 && v[i-1] >= 'a' && v[i-1] <= 'z':
			return true
		}
	}
	return false
}

// version (semver, the official semver.org regex without inner anchors), UUID and hexadecimal hash
var reVersionOrHash = regexp.MustCompile(`^(?:v?(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:\.(?:0|[1-9]\d*))?(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|[0-9a-fA-F]{16,})$`)

// weakSet: weak evidence, RAM only: name -> different rules in which it was seen. Two different
// rules count as strong evidence.
type weakSet struct {
	mu      sync.Mutex
	rules   map[string]int // rule name -> bit
	seenSet map[string]uint64
}

const maxWeak = 200_000

func (f *weakSet) markIt(name, rule string) (twoOf bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rules == nil {
		f.rules, f.seenSet = map[string]int{}, map[string]uint64{}
	}
	b, ok := f.rules[rule]
	if !ok {
		if len(f.rules) >= 64 {
			b = 63
		} else {
			b = len(f.rules)
			f.rules[rule] = b
		}
	}
	if len(f.seenSet) >= maxWeak {
		f.seenSet = map[string]uint64{}
	}
	f.seenSet[name] |= 1 << uint(b)
	v := f.seenSet[name]
	return v&(v-1) != 0 // more than one bit
}

// learnObj applies the brakes and, if it passes, remembers the name (RAM and, hash only,
// vistos.json).
func (m *Masker) learnObj(o ObjFinding, real string, public func(string) bool) {
	v := strings.Trim(strings.TrimSpace(real), "[]\"`")
	if len(v) < 4 || !m.objPropagates(o.Ent) || !identifierShape(v) || isPseudoObj(v) {
		return
	}
	if public != nil && public(v) {
		return
	}
	if ent, ok := m.learnedEnt(v); ok {
		o.Ent = ent // already learned: keeps the type (and the pseudonym) of the first time
	}
	if !o.Strong && !m.weakSet.markIt(canonObj(o.Ent, v), o.Rule) {
		return // weak evidence: masks in place, but does not teach
	}
	m.known.learnIt(objTypePrefix+o.Ent, v)
	if m.seenSet != nil {
		m.seenSet.MarkObj(m.objID(canonObj(o.Ent, v)), o.Ent, today())
	}
}

// learnedEnt: the type with which name v was already learned (in RAM or in vistos.json), if it was.
func (m *Masker) learnedEnt(v string) (string, bool) {
	v = unquoted(v)
	low := strings.ToLower(v)
	c := m.known
	c.mu.RLock()
	tp, ok := c.canon["O:"+v]
	if !ok {
		tp, ok = c.canon["o:"+low]
		ok = ok && entSQL[strings.TrimPrefix(tp, objTypePrefix)]
	}
	c.mu.RUnlock()
	if ok && isObject(tp) {
		return strings.TrimPrefix(tp, objTypePrefix), true
	}
	if m.seenSet == nil || !m.seenSet.HasObj() {
		return "", false
	}
	ent, seen, ok := m.seenSet.Obj(m.objID("O:" + v))
	if !ok {
		ent, seen, ok = m.seenSet.Obj(m.objID("o:" + low))
		ok = ok && entSQL[ent]
	}
	return ent, ok && today()-seen <= objValidity
}

// serversAsHost: a server found by structure (the host of a connection string, of a YAML) that is a
// hostname of the internal domain gets the type "host", the same the hostname detector gives it
// outside the structure. This way the same server has ONE pseudonym everywhere (before:
// host_x7k2... inside the URL and x7k2....invalid loose in the text, and the model saw two).
func (m *Masker) serversAsHost(out []Finding) {
	if len(m.cfg.InternalDomains) == 0 {
		return
	}
	for i, a := range out {
		if a.Type != objTypePrefix+"server" {
			continue
		}
		h := strings.ToLower(a.Real)
		if ix := reHost.FindStringIndex(h); ix != nil && ix[0] == 0 && ix[1] == len(h) && m.internal(h) && !strings.HasSuffix(h, ".invalid") {
			out[i].Type = "host"
		}
	}
}

// unifyObjects: the same real name gets ONE type (and therefore one pseudonym) in the text, even
// when different rules find it with different types ("dir_..." on one line and "svc_..." on another
// for the same folder). The type with which the name was already learned wins; if it was not, the
// one of the first occurrence in the text (in the same piece, the type order, as in applyT).
func (m *Masker) unifyObjects(out []Finding) {
	m.serversAsHost(out)
	var ix []int
	for i, a := range out {
		if isObject(a.Type) {
			ix = append(ix, i)
		}
	}
	if len(ix) < 2 && (len(ix) == 0 || m.known.generation() == 0 && (m.seenSet == nil || !m.seenSet.HasObj())) {
		return
	}
	sort.SliceStable(ix, func(i, j int) bool {
		a, b := out[ix[i]], out[ix[j]]
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		return a.Type < b.Type
	})
	choice := map[string]string{}
	for _, i := range ix {
		k := strings.ToLower(unquoted(out[i].Real))
		t, ok := choice[k]
		if !ok {
			t = out[i].Type
			if ent, ok := m.learnedEnt(out[i].Real); ok && m.objMask(ent) {
				t = objTypePrefix + ent
			}
			choice[k] = t
		}
		out[i].Type = t
	}
}

// objID: the hash with which the name is kept in vistos.json (the type goes in the value).
func (m *Masker) objID(key string) string { return m.p.ID("visto", key) }

// today: days since 1970 (vistos.json keeps dates like this). A variable for the tests.
var today = func() int { return int(time.Now().Unix() / 86400) }

// objValidity: a name not seen for longer than this stops being propagated.
const objValidity = 90

// findObjects runs the readers on s, returns the findings (to mask) and teaches.
func (m *Masker) findObjects(s string, learn bool, add func(ini, end int, typ string)) {
	if !m.cfg.Objects.Enabled || len(m.readers) == 0 {
		return
	}
	ct := textContext(s)
	if len(s) < parallelReaders || runtime.GOMAXPROCS(0) < 2 {
		for _, l := range m.readers {
			l.Find(s, func(o ObjFinding) { m.applyFinding(l, s, ct, o, learn, add) })
		}
		return
	}
	// large text: the readers (which only read s) run in parallel; the findings are applied and
	// learned afterwards, in reader order, as if they had run one after the other
	res := make([][]ObjFinding, len(m.readers))
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < min(runtime.GOMAXPROCS(0), len(m.readers)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= len(m.readers) {
					return
				}
				m.readers[i].Find(s, func(o ObjFinding) { res[i] = append(res[i], o) })
			}
		}()
	}
	wg.Wait()
	for i, l := range m.readers {
		for _, o := range res[i] {
			m.applyFinding(l, s, ct, o, learn, add)
		}
	}
}

// parallelReaders: from this size on, the readers run in parallel (a variable for the tests).
var parallelReaders = 64 << 10

// runReader: one reader on s, with the common brakes (type turned off, pseudonym, vocabulary).
func (m *Masker) runReader(l Reader, s string, learn bool, add func(ini, end int, typ string)) {
	ct := textContext(s)
	l.Find(s, func(o ObjFinding) { m.applyFinding(l, s, ct, o, learn, add) })
}

// ctxText: what applies to the whole text, measured once before the readers: the public software it
// runs as an image (textSoftware).
type ctxText struct {
	software map[string]bool
}

func textContext(s string) ctxText {
	return ctxText{software: textSoftware(s)}
}

// applyFinding: a finding of reader l, with the common brakes. ct: the text context.
func (m *Masker) applyFinding(l Reader, s string, ct ctxText, o ObjFinding, learn bool, add func(ini, end int, typ string)) {
	if o.Start < 0 || o.End > len(s) || o.End <= o.Start || !m.objMask(o.Ent) {
		return
	}
	v := s[o.Start:o.End]
	if isPseudoObj(v) || (l.Public != nil && l.Public(strings.Trim(v, "[]\"`"))) || vocabularyFinding(l, o, strings.Trim(v, "[]\"`'"), ct.software) {
		return
	}
	add(o.Start, o.End, objTypePrefix+o.Ent)
	if learn {
		m.learnObj(o, v, l.Public)
	}
	if hookFound != nil {
		hookFound(l.Name, o, v)
	}
}

// hookFound: only for the measurements (measurement_test.go): each finding of each reader.
var hookFound func(reader string, o ObjFinding, v string)

// objTokens calls fn for each candidate to a learned name in s: [A-Za-z_][A-Za-z0-9_$#]* (with
// accented letters) and "-..." pieces in the middle (the same as the regex
// `[\p{L}_][\p{L}\d_$#]*(?:-[\p{L}\d_$#]+)*`, without the cost of a regex over the whole text).
func objTokens(s string, fn func(a, b int)) {
	ident := func(c byte) bool { return isAlnum(c) || c == '_' || c == '$' || c == '#' }
	for i := 0; i < len(s); {
		c := s[i]
		n0 := 1
		if c >= 0x80 {
			if n0 = utf8Letter(s, i); n0 == 0 { // an accented letter also starts a name
				i++
				continue
			}
		} else if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_') {
			i++
			continue
		}
		j := i + n0
		for {
			for j < len(s) {
				if ident(s[j]) {
					j++
				} else if n := utf8Letter(s, j); n > 0 {
					j += n
				} else {
					break
				}
			}
			if j+1 < len(s) && s[j] == '-' && ident(s[j+1]) {
				j++
				continue
			}
			break
		}
		fn(i, j)
		if escapeIn(s, i) && j > i+1 && (isAlnum(s[i+1]) && !(s[i+1] >= '0' && s[i+1] <= '9') || s[i+1] == '_') {
			fn(i+1, j) // "\nt_x" in escaped text: the name may start after the escape
		}
		i = j
	}
}

// escapeIn: is s[i] the letter of a \n, \t or \r escape (escaped JSON text)?
func escapeIn(s string, i int) bool {
	return i > 0 && s[i-1] == '\\' && (s[i] == 'n' || s[i] == 't' || s[i] == 'r') && (i < 2 || s[i-2] != '\\')
}

// findKnownObjects: learned names (in RAM or, hash only, in vistos.json) that appear in s, in any
// case and outside any structure.
func (m *Masker) findKnownObjects(s string, add func(ini, end int, typ string)) {
	c := m.known
	c.mu.RLock()
	nRAM := c.nObj
	c.mu.RUnlock()
	disk := m.seenSet != nil && m.seenSet.HasObj()
	if nRAM == 0 && !disk {
		return
	}
	d := today()
	objTokens(s, func(a0, b0 int) {
		if a0 > 0 && (s[a0-1] == '-' || isAlnum(s[a0-1])) && !escapeIn(s, a0-1) {
			return
		}
		// a dotted name ("top_x.eventos", "app.config"): the token stops at the dot, so it also
		// tries the forms with the following pieces, from the longest to the shortest
		ends := dottedShapes(s, a0, b0)
		for k := len(ends) - 1; k >= 0; k-- {
			if m.knownObjIn(s, a0, ends[k], c, disk, d, add) {
				return
			}
		}
	})
}

// dottedShapes: the possible ends of a name that starts at s[a:b] and continues with ".piece" (up
// to 3 more pieces): [b, end with 1 piece, end with 2...].
func dottedShapes(s string, a, b int) []int {
	ends := []int{b}
	ident := func(c byte) bool { return isAlnum(c) || c == '_' || c == '$' || c == '#' }
	for n := 0; n < 3 && b+1 < len(s) && s[b] == '.' && ident(s[b+1]); n++ {
		e := b + 1
		for e < len(s) && (ident(s[e]) || s[e] == '-' && e+1 < len(s) && ident(s[e+1])) {
			e++
		}
		ends = append(ends, e)
		b = e
	}
	return ends
}

// knownObjIn: is s[a:b] a learned name (RAM or vistos.json)? If so, delivers the finding.
func (m *Masker) knownObjIn(s string, a, b int, c *knownSet, disk bool, d int, add func(ini, end int, typ string)) bool {
	ix := [2]int{a, b}
	v := s[ix[0]:ix[1]]
	if !identifierShape(v) {
		return false
	}
	c.mu.RLock()
	tp, ok := c.canon["O:"+v] // grafia exata
	if !ok {
		tp, ok = c.canon["o:"+strings.ToLower(v)] // SQL name, in any case
		ok = ok && entSQL[strings.TrimPrefix(tp, objTypePrefix)]
	}
	c.mu.RUnlock()
	if ok {
		if ent := strings.TrimPrefix(tp, objTypePrefix); m.objPropagates(ent) {
			add(ix[0], ix[1], tp)
		}
		return true
	}
	if !disk {
		return false
	}
	key := "O:" + v
	ent, seen, ok := m.seenSet.Obj(m.objID(key))
	if !ok {
		key = "o:" + strings.ToLower(v)
		ent, seen, ok = m.seenSet.Obj(m.objID(key))
		ok = ok && entSQL[ent]
	}
	if !ok || d-seen > objValidity || !m.objPropagates(ent) {
		return false
	}
	add(ix[0], ix[1], objTypePrefix+ent)
	c.learnIt(objTypePrefix+ent, v) // goes back to the memory
	m.seenSet.MarkObj(m.objID(key), ent, d)
	return true
}

// UseReaders swaps the structure readers (the tests use their own readers).
func (m *Masker) UseReaders(ls []Reader) { m.readers = ls }
