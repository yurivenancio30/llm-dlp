package mask

import (
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Values already masked once are recognized later anywhere (memory in RAM).

// Known values: everything masked by a detector that depends on CONTEXT (the word "rg" nearby,
// "password:" before the password...) needs to be recognized later anywhere. Otherwise this
// happens:
//
//	you:     "RG: 12.345.678-9"            -> masked (it has the word RG)
//	model:   "o documento RG-abc está ok"  -> you see "o documento 12.345.678-9 está ok"
//	next message: the history comes back with "o documento 12.345.678-9 está ok"
//	              -> without the word RG nearby, the number would pass unmasked
//
// Two memories:
//   - in RAM, the exact value (literal search, catches any format already seen);
//   - on disk, only the hash with the key (to survive restarts without keeping the value).

var contextual = map[string]bool{"secret": true, "rg": true, "cnh": true, "pis": true, "cep": true, "account": true,
	"pix": true, "birthdate": true, "phone": true, "card": true, "cpf": true, "cnpj": true,
	"user": true, "doc": true}

// forEachCode calls fn for each "word" of s that can be a user or document code: from 3 to 40
// letters, digits, "_", "." or "-", starting with a letter or digit.

func forEachCode(s string, fn func(ini, end int)) {
	for i := 0; i < len(s); {
		if !isAlnum(s[i]) {
			i++
			continue
		}
		j := i + 1
		for j < len(s) && (isAlnum(s[j]) || s[j] == '_' || s[j] == '.' || s[j] == '-') {
			j++
		}
		end := j
		for end > i && !isAlnum(s[end-1]) {
			end-- // end-of-sentence punctuation is not part of it
		}
		if n := end - i; n >= 3 && n <= 40 {
			fn(i, end)
		}
		i = j
	}
}

// canonicalKey: form of the value used to compare regardless of type and punctuation
// ("12.345.678-9" and "123456789" are the same number).
func canonicalKey(typ, real string) string {
	if isObject(typ) {
		return canonObj(strings.TrimPrefix(typ, objTypePrefix), real)
	}
	switch typ {
	case "secret":
		return "s:" + real
	case "pix":
		return "u:" + strings.ToLower(real)
	case "user", "doc":
		return "c:" + strings.ToUpper(real)
	case "name":
		return "p:" + NormName(real)
	}
	return "n:" + canonNum(real)
}

func canonNum(v string) string {
	d := digitsOnly(v)
	if strings.HasSuffix(strings.ToLower(v), "x") {
		d += "x"
	}
	return d
}

// knownSet is the memory in RAM. The search in the text does not walk the list of values: it uses
// an index by the start of each value (the first 4 bytes), as multi-pattern search engines do
// (prefix + confirmation). The cost depends on the size of the text, not on how many values there
// are. It has a ceiling: past maxKnown, the oldest half leaves RAM (its hash stays on disk).
type knownSet struct {
	mu    sync.RWMutex
	base  int               // how many values already left RAM (keeps the "generation" increasing)
	reals []string          // in order of arrival; generation of the value = base + index
	typ   map[string]string // exact value -> type
	seq   map[string]int    // exact value -> generation in which it was learned
	canon map[string]string // canonical key -> type
	// canonical key -> generation in which it was learned. It serves to redo a memoized text in
	// which the value appeared in ANOTHER spelling (RG without dots, code in lowercase).
	canonSeq map[string]int
	nTypes   map[string]int
	nObj     int // how many object names (see objects.go)
	// first 4 bytes -> LENGTHS of the values that start like this. With the start and the length,
	// the piece of text is looked up directly in seq (one map lookup). Keeping the list of values
	// here would be slow when thousands start the same way (RGs "12.3...", CPFs in sequence).
	pfx  map[uint32][]int32
	pair [1 << 16 / 64]uint64 // quick filter: is there any value starting with these 2 bytes?
}

const maxKnown = 50_000

func newKnown() *knownSet {
	c := &knownSet{}
	c.reset()
	return c
}

func (c *knownSet) reset() {
	c.reals = nil
	c.typ, c.seq, c.canon, c.canonSeq = map[string]string{}, map[string]int{}, map[string]string{}, map[string]int{}
	c.nTypes, c.pfx, c.nObj = map[string]int{}, map[uint32][]int32{}, 0
	c.pair = [1 << 16 / 64]uint64{}
}

func (c *knownSet) generation() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.base + len(c.reals)
}

func (c *knownSet) insert(typ, real string) {
	c.typ[real] = typ
	c.seq[real] = c.base + len(c.reals)
	c.canon[canonicalKey(typ, real)] = typ
	c.canonSeq[canonicalKey(typ, real)] = c.base + len(c.reals)
	c.reals = append(c.reals, real)
	c.nTypes[typ]++
	if isObject(typ) {
		c.nObj++
	}
	k := uint32(real[0])<<24 | uint32(real[1])<<16 | uint32(real[2])<<8 | uint32(real[3])
	has := false
	for _, n := range c.pfx[k] {
		if int(n) == len(real) {
			has = true
			break
		}
	}
	if !has {
		c.pfx[k] = append(c.pfx[k], int32(len(real)))
	}
	c.pair[k>>22] |= 1 << (k >> 16 & 63)
}

func (c *knownSet) learnIt(typ, real string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.typ[real]; ok {
		return false
	}
	if len(c.reals) >= maxKnown { // ceiling: the most recent half stays, in new maps
		stayOnes := append([]string(nil), c.reals[len(c.reals)/2:]...)
		types := make([]string, len(stayOnes))
		for i, v := range stayOnes {
			types[i] = c.typ[v]
		}
		c.base += len(c.reals) - len(stayOnes)
		c.reset()
		for i, v := range stayOnes {
			c.insert(types[i], v)
		}
	}
	c.insert(typ, real)
	return true
}

// scan calls fn for each occurrence, in s, of a value learned from generation since. fn returns
// false to stop.
func (c *knownSet) scan(s string, since int, fn func(ini, end int, typ string) bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.reals) == 0 {
		return
	}
	for i := 0; i+4 <= len(s); i++ {
		k2 := uint32(s[i])<<8 | uint32(s[i+1])
		if c.pair[k2>>6]&(1<<(k2&63)) == 0 {
			continue
		}
		for _, n := range c.pfx[k2<<16|uint32(s[i+2])<<8|uint32(s[i+3])] {
			end := i + int(n)
			if end > len(s) {
				continue
			}
			v := s[i:end]
			g, ok := c.seq[v]
			if !ok || g < since {
				continue
			}
			if isAlnum(v[0]) && i > 0 && isAlnum(s[i-1]) {
				continue // piece of another word/number
			}
			if isAlnum(v[len(v)-1]) && end < len(s) && isAlnum(s[end]) {
				continue
			}
			if tp := c.typ[v]; isObject(tp) && (i > 0 && (isIdent(s[i-1]) || utf8LetterBefore(s, i)) || end < len(s) && (isIdent(s[end]) || utf8Letter(s, end) > 0)) {
				continue // an object name only counts whole ("tb_x" is not a piece of "tb_x_hist")
			}
			if !fn(i, end, c.typ[v]) {
				return
			}
		}
	}
}

// containsSince says whether s contains some value learned from generation g (or whether it can no
// longer be known, because values of that time left RAM).
func (c *knownSet) containsSince(s string, g int) bool {
	c.mu.RLock()
	lost := g < c.base
	c.mu.RUnlock()
	if lost {
		return true
	}
	foundIt := false
	c.scan(s, g, func(int, int, string) bool { foundIt = true; return false })
	if foundIt {
		return true
	}
	// the same thing in another spelling: the number without punctuation, the code in another case
	c.mu.RLock()
	defer c.mu.RUnlock()
	newOne := func(key string) bool { gen, ok := c.canonSeq[key]; return ok && gen >= g }
	for _, ix := range reNumTok.FindAllStringIndex(s, -1) {
		if v := s[ix[0]:ix[1]]; len(digitsOnly(v)) >= 8 && newOne("n:"+canonNum(v)) {
			return true
		}
	}
	for _, ix := range rePix.FindAllStringIndex(s, -1) {
		if newOne("u:" + strings.ToLower(s[ix[0]:ix[1]])) {
			return true
		}
	}
	forEachCode(s, func(ini, end int) {
		if !foundIt && newOne("c:"+strings.ToUpper(s[ini:end])) {
			foundIt = true
		}
	})
	if !foundIt && c.nObj > 0 {
		objTokens(s, func(a, b int) {
			for _, f := range dottedShapes(s, a, b) { // also with a dot ("top_x.eventos")
				if v := s[a:f]; !foundIt && (newOne("O:"+v) || newOne("o:"+strings.ToLower(v))) {
					foundIt = true
				}
			}
		})
		if foundIt {
			return true
		}
	}
	return foundIt
}

func hasDigit(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			return true
		}
	}
	return false
}

// isIdent: a character that continues an identifier (letter, digit, _ $ # -).
func isIdent(b byte) bool { return isAlnum(b) || b == '_' || b == '$' || b == '#' || b == '-' }

// utf8Letter: if s[i] starts a non-ASCII letter (á, ç, ñ...), its length in bytes; otherwise 0.
func utf8Letter(s string, i int) int {
	if s[i] < 0x80 {
		return 0
	}
	r, n := utf8.DecodeRuneInString(s[i:])
	if r == utf8.RuneError || !unicode.IsLetter(r) {
		return 0
	}
	return n
}

// utf8LetterBefore: is the character that ends at s[i-1] a non-ASCII letter?
func utf8LetterBefore(s string, i int) bool {
	if i == 0 || s[i-1] < 0x80 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return r != utf8.RuneError && unicode.IsLetter(r)
}

func isAlnum(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// lineWindow limits the search for the start/end of the line around an occurrence. Without a limit,
// on a single long line (minified JSON, API response, HTML) each occurrence walks the whole line
// and the cost becomes quadratic.
const lineWindow = 1000

// lineStartJ: start of the line of s[i], or the start of the window if the line is longer.
func lineStartJ(s string, i int) int {
	a := max(0, i-lineWindow)
	return a + strings.LastIndexByte(s[a:i], '\n') + 1
}

// lineEndJ: end of the line of s[i] (the '\n' or len(s)), or the end of the window.
func lineEndJ(s string, i int) int {
	z := min(len(s), i+lineWindow)
	if k := strings.IndexByte(s[i:z], '\n'); k >= 0 {
		return i + k
	}
	return z
}

// separators a password is usually glued to; maxCuts limits the work per word
const (
	passwordSep = "=&:/?@|#+"
	maxCuts     = 17
)

var (
	reNumTok   = regexp.MustCompile(`\d[\d.\-/]*[\dxX]`)
	reTokenSeg = regexp.MustCompile("[^\\s\"'`,;()\\[\\]{}<>]{8,}")
)

// findKnown appends to out the occurrences, in s, of values already known.
func (m *Masker) findKnown(s string, longNum bool, add0 func(ini, end int, typ string)) {
	c := m.known
	add := func(ini, end int, typ string) {
		if isObject(typ) {
			if !m.objPropagates(strings.TrimPrefix(typ, objTypePrefix)) {
				return // type turned off after being learned
			}
			// the accumulated knowledge (RAM, vistos.json) follows the tracing rules: a common word
			// does not spread through it, public vocabulary is never applied and neither is a public
			// software with proof (a name learned wrongly before does not come back in every text;
			// see memory_tracing.go and memory_software.go)
			if v := s[ini:end]; !identifierShape(v) || publicGeneral(v) || m.provenSoftware(v) {
				return
			}
		}
		// a learned domain only counts whole: "acme.io" is not the end of "acme.io.br"
		if typ == "domain" && end+1 < len(s) && s[end] == '.' && isAlnum(s[end+1]) {
			return
		}
		add0(ini, end, typ)
	}
	m.findKnownObjects(s, add)

	// 1) values in RAM, by the exact text (catches any format already seen)
	c.scan(s, 0, func(ini, end int, typ string) bool { add(ini, end, typ); return true })

	// Finding by hash: the real value is here in the text, so it goes back to the memory. This way
	// the search by exact text (1) starts catching it glued to something else too.
	gotIt := func(ini, end int, tp string) {
		add(ini, end, tp)
		if end-ini >= 4 {
			c.learnIt(tp, s[ini:end])
		}
	}

	// 2) candidates by the canonical form: the same number with other punctuation, and values
	//    learned before a restart (on disk there is only the hash)
	hasHash := m.seenSet != nil && !m.seenSet.IsEmpty()
	c.mu.RLock()
	hasCanon := len(c.canon) > 0
	nSeg := c.nTypes["secret"]
	c.mu.RUnlock()
	if !hasHash && !hasCanon {
		return
	}
	look := func(key string) (string, bool) {
		c.mu.RLock()
		tp, ok := c.canon[key]
		c.mu.RUnlock()
		if ok {
			return tp, true
		}
		if hasHash {
			return m.seenSet.Type(m.p.ID("visto", key))
		}
		return "", false
	}
	if longNum || hasHash || hasCanon {
		for _, ix := range reNumTok.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			// only long numbers: a short learned number (branch "4321-0") cannot make every "43210"
			// of the text become sensitive data. Short ones stay with the literal search.
			if !digitEdge(s, ix[0], ix[1]) || len(digitsOnly(v)) < 8 {
				continue
			}
			if tp, ok := look("n:" + canonNum(v)); ok {
				gotIt(ix[0], ix[1], tp)
			}
		}
		for _, ix := range reTel.FindAllStringIndex(s, -1) { // telephones with spaces/parentheses
			if tp, ok := look("n:" + canonNum(s[ix[0]:ix[1]])); ok && digitEdge(s, ix[0], ix[1]) {
				gotIt(ix[0], ix[1], tp)
			}
		}
	}
	if strings.Contains(s, "-") {
		for _, ix := range rePix.FindAllStringIndex(s, -1) {
			if tp, ok := look("u:" + strings.ToLower(s[ix[0]:ix[1]])); ok {
				gotIt(ix[0], ix[1], tp)
			}
		}
	}
	// learned user codes and documents (in any case, and after a restart)
	c.mu.RLock()
	nCode := c.nTypes["user"] + c.nTypes["doc"]
	c.mu.RUnlock()
	if nCode > 0 || (hasHash && (m.seenSet.Has("user") || m.seenSet.Has("doc"))) {
		forEachCode(s, func(ini, end int) {
			v := s[ini:end]
			if !hasDigit(v) || len(v) < 5 {
				return
			}
			if tp, ok := look("c:" + strings.ToUpper(v)); ok {
				gotIt(ini, end, tp)
			}
		})
	}
	// secrets from before a restart: only the hash exists, so it tests each long "word"
	if hasHash && (m.seenSet.Has("secret") || nSeg > 0) {
		for _, ix := range reTokenSeg.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			c.mu.RLock()
			_, alreadyKnown := c.typ[v]
			c.mu.RUnlock()
			if alreadyKnown {
				continue
			}
			if m.seenSet.HasSize(len(v)) {
				if tp, ok := m.seenSet.Type(m.p.ID("visto", "s:"+v)); ok {
					gotIt(ix[0], ix[1], tp)
					continue
				}
			}
			// Password glued to something else ("k=Senha@1&b=2", "user:Senha@1@host"): with the
			// hash one can only ask about an exact piece, so it tests the pieces between
			// separators. The password can contain separators, so all intervals (from one cut to
			// another), and not only the minimal pieces.
			cuts := []int{0}
			for i := 0; i < len(v) && len(cuts) < maxCuts; i++ {
				if strings.IndexByte(passwordSep, v[i]) >= 0 {
					cuts = append(cuts, i, i+1) // before and after the separator
				}
			}
			if len(cuts) == 1 {
				continue
			}
			cuts = append(cuts, len(v))
		pieces:
			for a := 0; a < len(cuts); a++ {
				for b := len(cuts) - 1; b > a; b-- {
					ini, end := cuts[a], cuts[b]
					// only pieces of the length of some known password: avoids hashing almost all
					// of them (computing the hash is the expensive part)
					if end-ini < 8 || (ini == 0 && end == len(v)) || !m.seenSet.HasSize(end-ini) {
						continue
					}
					if tp, ok := m.seenSet.Type(m.p.ID("visto", "s:"+v[ini:end])); ok {
						gotIt(ix[0]+ini, ix[0]+end, tp)
						break pieces
					}
				}
			}
		}
	}
}

// learnIt records a contextual value in RAM and (hash only) on disk.
func (m *Masker) learnIt(typ, real string) {
	if typ == "email" {
		m.learnEmail(real) // the organization's domain and the person's name (memory_email.go)
		return
	}
	if typ == "name" {
		// A name found by the field label ("NOM_CLIENTE"): the person enters the registry, as in
		// import-people, and starts being recognized anywhere and in any spelling.
		if m.people != nil && len(strings.Fields(real)) >= 2 && len(real) <= 80 && m.people.PID(m.p, "name", NormName(real)) == "" {
			m.people.Import(m.p, real, "", "")
			m.known.learnIt("name", real) // makes the texts already memoized with this name be redone
		}
		return
	}
	if !contextual[typ] || len(real) < 4 {
		return
	}
	// a short code or one without a digit ("jsilva", "ana") is only masked together with the label:
	// remembering it would make every equal word become sensitive data
	if (typ == "user" || typ == "doc") && (len(real) < 5 || !hasDigit(real)) {
		return
	}
	if m.known.learnIt(typ, real) && m.seenSet != nil {
		if typ == "secret" {
			m.seenSet.MarkSize(len(real))
		}
		m.seenSet.Mark(m.p.ID("visto", canonicalKey(typ, real)), typ)
	}
}
