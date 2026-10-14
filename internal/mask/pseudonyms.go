package mask

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Swapping the findings for pseudonyms.

// Swapping the findings for pseudonyms.

// apply resolves overlaps (the longest piece stays) and swaps for pseudonyms.
func (m *Masker) apply(s string, findings []Finding) (string, []Entry) {
	t, e, _ := m.applyT(s, findings)
	return t, e
}

// applyT is apply that also returns the swapped pieces (see memory_sent.go).
func (m *Masker) applyT(s string, findings []Finding) (string, []Entry, []snippet) {
	if len(findings) == 0 {
		return s, nil, nil
	}
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		if a.End-a.Start != b.End-b.Start {
			return a.End-a.Start > b.End-b.Start
		}
		// the same piece with two types: always the same choice (the pseudonym cannot vary). The
		// personal data detector wins; among objects, the type order.
		if isObject(a.Type) != isObject(b.Type) {
			return !isObject(a.Type)
		}
		return a.Type < b.Type
	})
	var b strings.Builder
	var entries []Entry
	var ts []snippet
	pos := 0
	for _, a := range findings {
		if a.Start < pos {
			continue
		}
		ps := m.Pseudonym(a.Type, a.Real)
		b.WriteString(s[pos:a.Start])
		b.WriteString(ps)
		entries = entryOf(entries, ps, a.Real, a.Type)
		ts = append(ts, snippet{a.Start, a.End, a.Type, ps})
		pos = a.End
	}
	b.WriteString(s[pos:])
	return b.String(), entries, ts
}

// entryOf adds the entry of a swapped piece. The other cases of the pseudonym, which the model may
// write ("T_abc", "t_abc", "T_ABC"), do not enter here: NewTable adds them after all the spellings
// that appeared (see caseVariants).
func entryOf(entries []Entry, ps, real, typ string) []Entry {
	entries = append(entries, Entry{ps, real, typ})
	if typ == "email" { // the model sometimes cites only the domain: "dxxxx.invalid" goes back to the real domain
		if i, j := strings.LastIndexByte(ps, '@'), strings.LastIndexByte(real, '@'); i >= 0 && j >= 0 {
			entries = append(entries, Entry{ps[i+1:], real[j+1:], "domain"})
		}
	}
	return entries
}

// rebuild redoes the text sent before from the stored pieces: it goes out the same, byte for byte,
// even if more is known today (see memory_sent.go). Invalid piece: does not rebuild.
func rebuild(s string, ts []snippet) (string, []Entry, bool) {
	var b strings.Builder
	var entries []Entry
	pos := 0
	for _, t := range ts {
		if t.Start < pos || t.End <= t.Start || t.End > len(s) || t.Pseudo == "" || t.Type == "" {
			return "", nil, false
		}
		b.WriteString(s[pos:t.Start])
		b.WriteString(t.Pseudo)
		entries = entryOf(entries, t.Pseudo, s[t.Start:t.End], t.Type)
		pos = t.End
	}
	b.WriteString(s[pos:])
	return b.String(), entries, true
}

// Pseudonym returns the stable pseudonym of a real value of a type. Every pseudonym is registered:
// one left over on the return path, without unmasking, is surely ours (see NotUndone).
func (m *Masker) Pseudonym(typ, real string) string {
	ps := m.pseudonym(typ, real)
	registerPseudo(ps)
	return ps
}

func (m *Masker) pseudonym(typ, real string) string {
	if isObject(typ) {
		return m.pseudoObject(strings.TrimPrefix(typ, objTypePrefix), real)
	}
	switch typ {
	case "email":
		e := strings.ToLower(real)
		at := strings.LastIndex(e, "@")
		pid := ""
		if m.people != nil {
			pid = m.people.PID(m.p, "email", e)
		}
		if pid == "" {
			pid = m.p.ID("email", e)
		}
		// short format on purpose: measured, "pessoa.X@dom-YYYYYYYY.invalid" cost ~16% more tokens
		// than the real e-mail; this one costs ~1.5% (the domain only needs to tell domains apart)
		return "p." + pid + "@d" + m.p.ID("dominio", e[at+1:])[:4] + ".invalid"
	case "name":
		pid := ""
		if m.people != nil {
			pid = m.people.PID(m.p, "name", NormName(real))
		}
		if pid == "" {
			pid = m.p.ID("nome", NormName(real))
		}
		// the same person, with the case of the spelling ("MARIA SILVA", "maria silva"): the return
		// gives each spelling back as it was (see nameVariants)
		switch nameCase(real) {
		case 'A':
			return "PERSON " + strings.ToUpper(pid)
		case 'a':
			return "person " + pid
		}
		return "Person " + pid
	case "ip":
		return m.pseudoIP(real)
	case "host":
		h := strings.ToLower(real)
		hostRole := strings.SplitN(h, ".", 2)[0]
		for _, p := range m.cfg.HostRoles {
			if p == hostRole {
				return hostRole + "-" + m.p.ID("host", h)[:4] + ".invalid"
			}
		}
		return "h" + m.p.ID("host", h) + ".invalid"
	case "secret":
		return "SECRET-" + m.p.ID(hmacDomain(typ), real)
	case "user":
		// code of an imported person: the same id as their name and e-mail
		pid := ""
		if m.people != nil {
			pid = m.people.PID(m.p, "code", NormName(real))
		}
		if pid == "" {
			pid = m.p.ID(hmacDomain(typ), NormName(real))
		}
		return pseudoLabel("USER", pid, real)
	}
	if numericTypes[typ] {
		if typ == "cnpj" && ValidAlphaCNPJ(real) { // the new format has letters: the digits alone are not enough
			return "CNPJ-" + m.p.ID(hmacDomain(typ), NormName(real))
		}
		return strings.ToUpper(typ) + "-" + m.p.ID(hmacDomain(typ), canonNum(real))
	}
	lbl := strings.TrimPrefix(strings.TrimPrefix(typ, "x:"), "t:")
	return pseudoLabel(strings.ToUpper(lbl), m.p.ID(hmacDomain(typ), NormName(real)), real)
}

// numericTypes: the pseudonym comes only from the digits (canonNum).
var numericTypes = setOf("cpf", "cnpj", "pis", "cnh", "phone", "cep", "card", "rg", "account")

// pseudoWithCase: the type's pseudonym follows the case of the real spelling, and the return adds
// its other cases (caseVariants): objects (pseudoObject) and labeled types whose key has no case
// (pseudoLabel: user, registered term, extra pattern, address...). A person has its own forms
// (nameVariants); e-mail, host, IP, secret and numbers do not.
func pseudoWithCase(typ string) bool {
	if isObject(typ) {
		return true
	}
	switch typ {
	case "email", "name", "ip", "host", "secret", "domain":
		return false
	}
	return !numericTypes[typ]
}

// pseudoSep: the separator between the prefix and the id of the pseudonym ("T_abc", "EMPRESA-abc").
func pseudoSep(typ string) byte {
	if isObject(typ) {
		return '_'
	}
	return '-'
}

// pseudoLabel: label + id, in the case of the spelling, like a person's name: the same value
// written "SGP", "sgp" and "SgP" (the id key has no case) goes out as "EMPRESA-x", "empresa-x" and
// "Empresa-x", and the return gives each spelling back as it was. Without letters, the usual form
// (label in uppercase). The id stays lowercase in all of them: the three forms cost the same
// tokens.
func pseudoLabel(lbl, id, real string) string {
	ps := labelShape(lbl, id, real)
	if ps[:len(lbl)] != lbl { // len(ps) > len(lbl): ps = lbl in the case of the spelling + "-" + id
		registerPseudo(ps) // the new forms do not have the look reReadyPseudo knows (see isPseudoLabel)
	}
	return ps
}

// labelShape: the pseudonym of pseudoLabel, without registering it.
func labelShape(lbl, id, real string) string {
	switch caseOf(real) {
	case 'a':
		lbl = strings.ToLower(lbl)
	case 'M':
		lbl = capitalize(lbl)
	}
	return lbl + "-" + id
}

// primaryLabel: is ps (lbl + "-" + id) the form labelShape gives for the real spelling? Without
// building the string (NewTable asks this of every entry).
func primaryLabel(lbl, id, real string) bool {
	if id != strings.ToLower(id) {
		return false
	}
	switch caseOf(real) {
	case 'a':
		return lbl == strings.ToLower(lbl)
	case 'M':
		r, n := utf8.DecodeRuneInString(lbl)
		return unicode.ToUpper(r) == r && lbl[n:] == strings.ToLower(lbl[n:])
	}
	return lbl == strings.ToUpper(lbl)
}

// caseOf: 'A' (uppercase only), 'a' (lowercase only), 'M' (both) or 0 (no letter with case),
// without allocating.
func caseOf(s string) byte {
	upper, lowerSeen := false, false
	for _, r := range s {
		if r < utf8.RuneSelf {
			upper = upper || 'A' <= r && r <= 'Z'
			lowerSeen = lowerSeen || 'a' <= r && r <= 'z'
			continue
		}
		upper = upper || unicode.ToLower(r) != r
		lowerSeen = lowerSeen || unicode.ToUpper(r) != r
	}
	switch {
	case upper && lowerSeen:
		return 'M'
	case upper:
		return 'A'
	case lowerSeen:
		return 'a'
	}
	return 0
}

// capitalize: the first letter in uppercase, the rest in lowercase.
func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + strings.ToLower(s[n:])
}

// rePseudoLabel: the SHAPE of a label pseudonym in any case. The shape alone is not enough
// ("maria-fernanda" has that look): see isPseudoLabel.
var rePseudoLabel = regexp.MustCompile(`^[A-Za-z:]+-[a-zA-Z2-7]{8}$`)

// isPseudoLabel: is v a label pseudonym in lowercase or with only the first letter capitalized,
// generated by us (the uppercase form, the usual one, reReadyPseudo already recognizes)?
func isPseudoLabel(v string) bool {
	if !rePseudoLabel.MatchString(v) {
		return false
	}
	_, ok := generatedPseudos.Load(strings.ToLower(v))
	return ok
}

// caseVariants: the other cases of the pseudonym of an object or of a labeled type, which the model
// may write ("T_abc", "t_abc", "T_ABC", "Empresa-x"). The variant goes back to the real value as it
// entered: a model that changes the case of a pseudonym does not invent a new spelling
// ("/dados/SGP" and "/dados/sgp" are two folders). Each form a spelling generates is already in m
// when the spelling appeared (and then the variant does not enter); the all-uppercase one, which no
// spelling generates, goes back to the uppercase spelling when it appeared. m: the mapping already
// built.
func caseVariants(ps, real, typ string, m map[string]string) []Entry {
	i := strings.LastIndexByte(ps, pseudoSep(typ))
	if i <= 0 {
		return nil
	}
	pfx := ps[:i]
	alts := [4]string{strings.ToUpper(pfx) + ps[i:], strings.ToLower(ps), strings.ToUpper(ps)}
	if !isObject(typ) {
		alts[3] = capitalize(pfx) + ps[i:] // "Empresa-x": the form of the mixed-case spelling
	}
	var out []Entry
	for k, alt := range alts {
		if alt == "" || alt == ps {
			continue
		}
		r := real
		if k == 2 { // "T_ABC": the spelling of "T_abc" (in uppercase), if it appeared
			if g, ok := m[strings.ToUpper(pfx)+strings.ToLower(ps[i:])]; ok && sameValue(typ, g, real) {
				r = g
			}
		}
		out = append(out, Entry{alt, r, typ})
	}
	return out
}

// nameCase: 'A' for a name all in uppercase, 'a' for all in lowercase, 'M' for the rest.
func nameCase(s string) byte {
	switch {
	case strings.ToUpper(s) == s && strings.ToLower(s) != s:
		return 'A'
	case strings.ToLower(s) == s && strings.ToUpper(s) != s:
		return 'a'
	}
	return 'M'
}

// nameConnectives: the words that stay lowercase in a name with capital initials.
var nameConnectives = setOf("da", "das", "de", "do", "dos", "e", "del", "di", "du", "van", "von")

// nameVariants: the other cases of a person's pseudonym (the model may rewrite the case, as it does
// with object names), with the real name in the same case.
func nameVariants(ps, real string) []Entry {
	i := strings.IndexByte(ps, ' ')
	if i < 0 {
		return nil
	}
	pid := strings.ToLower(ps[i+1:])
	initials := strings.Fields(strings.ToLower(real))
	for k, w := range initials {
		if k == 0 || !nameConnectives[w] {
			r := []rune(w)
			r[0] = []rune(strings.ToUpper(string(r[0])))[0]
			initials[k] = string(r)
		}
	}
	var out []Entry
	for _, v := range []Entry{
		{"PERSON " + strings.ToUpper(pid), strings.ToUpper(real), "name"},
		{"person " + pid, strings.ToLower(real), "name"},
		{"Person " + pid, strings.Join(initials, " "), "name"},
	} {
		if v.Pseudo != ps {
			out = append(out, v)
		}
	}
	return out
}

func (m *Masker) allowedEmail(e string) bool {
	e = strings.ToLower(e)
	dom := e[strings.LastIndex(e, "@")+1:]
	if strings.HasSuffix(dom, ".invalid") {
		return true
	}
	for _, x := range m.cfg.EmailsLiberados {
		if strings.ToLower(x) == e {
			return true
		}
	}
	for _, d := range m.cfg.AllowedEmailDomains {
		d = strings.ToLower(d)
		if dom == d || strings.HasSuffix(dom, "."+d) {
			return true
		}
	}
	return false
}
