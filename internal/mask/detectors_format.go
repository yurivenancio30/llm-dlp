package mask

import (
	"net/netip"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
	"unicode/utf8"
)

// Detectors by format: e-mail, IP, hostname, CPF, CNPJ, telephone, card, tokens (gitleaks)...

var (
	reEmail   = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)+`)
	reIPv4    = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`)
	reHost    = regexp.MustCompile(`(?i)(?:[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?\.)+[a-z]{2,}`)
	reCPF     = regexp.MustCompile(`\d{3}\.?\d{3}\.?\d{3}-?\d{2}`)
	reCNPJ    = regexp.MustCompile(`\d{2}\.?\d{3}\.?\d{3}/?\d{4}-?\d{2}`)
	reTel     = regexp.MustCompile(`(?:\+55[\s\-]?)?\(?\d{2}\)?[\s\-]?9?\d{4}[\s\-]?\d{4}`)
	reCEP     = regexp.MustCompile(`\d{5}-?\d{3}`)
	reCard    = regexp.MustCompile(`\d(?:[ \-]?\d){12,18}`)
	reRG      = regexp.MustCompile(`\d{1,2}\.?\d{3}\.?\d{3}-?[\dxX]`)
	re11      = regexp.MustCompile(`\d{3}\.?\d{5}\.?\d{2}-?\d|\d{11}`)
	reAg      = regexp.MustCompile(`(?i)\b(?:ag[eê]ncia|ag\.)\s*(?:n[ºo°.]?\s*)?[:=]?\s*(\d{3,5}(?:-[\dxX])?)`)
	reAccount = regexp.MustCompile(`(?i)\b(?:conta(?:\s+corrente|\s+poupan[cç]a)?|c/c)\s*(?:n[ºo°.]?\s*)?[:=]?\s*(\d{4,12}-?[\dxX]?)`)
	rePix     = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	reEnd     = regexp.MustCompile(`(?i)\b(?:rua|r\.|avenida|av\.|travessa|tv\.|alameda|al\.|rodovia|rod\.|estrada|pra[çc]a|largo)\s+[\p{L}0-9 .'\-]{2,60}?,\s*(?:n[ºo°.]?\s*)?\d{1,6}`)
	reBirth   = regexp.MustCompile(`(?i)\b(?:nascimento|nasc\.|data de nasc\w*|dt_?nasc\w*)\s*[:=]?\s*(\d{2}/\d{2}/\d{4}|\d{4}-\d{2}-\d{2})`)
	reDigNu   = regexp.MustCompile(`\d{11}|\d{14}`)

	reDDD     = regexp.MustCompile(`^\(\d{2}\)`)
	reGrouped = regexp.MustCompile(`^\d{4}[ \-]\d{4}[ \-]\d{4}[ \-]\d{1,7}$`)
)

var (
	fakeNet = netip.MustParsePrefix("240.0.0.0/4") // reserved range (RFC 1112): pseudonyms only
)

// inURLAuthority: position i is in the authority of a URL ("scheme://user:password@host"), before
// any "/", "?" or "#". There, "password@host" is not an e-mail: it is the password and the server,
// which the connection and password readers mask each with its type. An e-mail in the path or in
// the parameters of the URL (?email=...) is still an e-mail.
func inURLAuthority(s string, i int) bool {
	ini := i
	for ini > 0 && !blank(s[ini-1]) && !strings.ContainsRune("\"'`<>()[]{},;", rune(s[ini-1])) {
		ini--
	}
	k := strings.LastIndex(s[ini:i], "://")
	return k >= 0 && !strings.ContainsAny(s[ini+k+3:i], "/?#")
}

func (m *Masker) internal(h string) bool {
	for _, d := range m.cfg.InternalDomains {
		if d != "" && strings.Contains(h, strings.ToLower(d)) {
			return true
		}
	}
	return false
}

// --- bordas e contexto -------------------------------------------------------

func isDig(b byte) bool { return b >= '0' && b <= '9' }

// numberEdge: like digitEdge, but also rejects "1.2.3.4.5" (versions, OIDs).
func numberEdge(s string, i, j int) bool {
	if !digitEdge(s, i, j) {
		return false
	}
	if i > 0 && s[i-1] == '.' {
		return false
	}
	if j < len(s)-1 && s[j] == '.' && isDig(s[j+1]) {
		return false
	}
	return true
}

func formatted(v string) bool { return strings.ContainsAny(v, ".-/") }

// contextOf: the word appears in the ~30 characters before the piece.
func contextOf(s string, i int, word string) bool {
	ini := i - 30
	if ini < 0 {
		ini = 0
	}
	for ini > 0 && !utf8.RuneStart(s[ini]) {
		ini--
	}
	return strings.Contains(strings.ToLower(s[ini:i]), word)
}

func anyContext(s string, i int, words ...string) bool {
	for _, p := range words {
		if contextOf(s, i, p) {
			return true
		}
	}
	return false
}

// wordContext: like contextOf, but requires the whole word (avoids "rg" inside "cargo").
func wordContext(s string, i int, word string) bool {
	ini := i - 30
	if ini < 0 {
		ini = 0
	}
	for ini > 0 && !utf8.RuneStart(s[ini]) {
		ini--
	}
	snippet := strings.ToLower(s[ini:i])
	for _, w := range strings.FieldsFunc(snippet, func(r rune) bool { return !unicode.IsLetter(r) }) {
		if w == word {
			return true
		}
	}
	return false
}

// detectBase: all detectors, one by one.

// gitleaks windows (see Detect)
const (
	leaksWindow   = 64 << 10
	leaksLeftover = 8 << 10
)

// detectBase runs the detectors over s, without the memory of known values.
func (m *Masker) detectBase(s string) []Finding {
	var out []Finding
	add := func(ini, end int, typ string) {
		out = append(out, Finding{ini, end, typ, s[ini:end]})
	}
	// Cheap prefilters: each detector only runs if the text has its "ingredient". Without this, ~15
	// regexes scan every text (≈6 ms per 5 KB); with it, almost nothing runs on common text.
	lower := strings.ToLower(s)
	has := func(ps ...string) bool {
		for _, p := range ps {
			if strings.Contains(lower, p) {
				return true
			}
		}
		return false
	}
	longNum, ipLike := numericProfile(s)
	on := func(d string) bool { return !m.cfg.Disabled(d) }

	// llm-dlp's own key can never go out
	for i := 0; ; {
		j := strings.Index(s[i:], m.keyB64)
		if j < 0 {
			break
		}
		add(i+j, i+j+len(m.keyB64), "secret")
		i += j + len(m.keyB64)
	}

	if on("secret") {
		// gitleaks gets disproportionately slow on large texts (above ~128 KB the time grows faster
		// than the size). So the text goes in windows, in parallel, with an overlap between them so
		// a multi-line secret (PEM private key) is not cut in half. With a PEM block larger than
		// the overlap, it goes whole.
		type window struct{ base, end int }
		var js []window
		if len(s) <= leaksWindow || longBlock(s, leaksLeftover) {
			js = []window{{0, len(s)}}
		} else {
			for base := 0; ; {
				end := min(len(s), base+leaksWindow)
				for end < len(s) && end > base && s[end]&0xC0 == 0x80 {
					end-- // does not cut a UTF-8 character in half
				}
				js = append(js, window{base, end})
				if end == len(s) {
					break
				}
				base = end - leaksLeftover
				for base > 0 && s[base]&0xC0 == 0x80 {
					base--
				}
			}
		}
		type secret struct {
			base, line, col int
			value           string
		}
		res := make([][]secret, len(js))
		var next atomic.Int64
		var wg sync.WaitGroup
		for w := 0; w < min(runtime.GOMAXPROCS(0), len(js)); w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					i := int(next.Add(1)) - 1
					if i >= len(js) {
						return
					}
					for _, f := range m.leaks.DetectString(s[js[i].base:js[i].end]) {
						res[i] = append(res[i], secret{js[i].base, f.StartLine, f.StartColumn, f.Secret})
					}
				}
			}()
		}
		wg.Wait()
		for i, r := range res {
			part := s[js[i].base:js[i].end]
			alreadySeen := map[string]bool{}
			for _, f := range r {
				if len(f.value) < 6 || alreadySeen[f.value] {
					continue
				}
				alreadySeen[f.value] = true
				foundIt := false
				for k := 0; ; {
					j := strings.Index(part[k:], f.value)
					if j < 0 {
						break
					}
					add(f.base+k+j, f.base+k+j+len(f.value), "secret")
					k += j + len(f.value)
					foundIt = true
				}
				if !foundIt {
					// a secret found AFTER decoding (e.g. base64 of a Kubernetes Secret): it does
					// not appear literally in the text; covers the encoded piece at that position,
					// whole (even the part that was outside this window)
					if ini, f2, ok := encodedSnippet(part, f.line, f.col); ok {
						a, b := f.base+ini, f.base+f2
						for a > 0 && isB64(s[a-1]) {
							a--
						}
						for b < len(s) && isB64(s[b]) {
							b++
						}
						add(a, b, "secret")
					}
				}
			}
		}
	}
	if on("secret") {
		m.findPasswords(s, lower, add)
	}
	if on("field") {
		m.findFields(s, add)
		if strings.Contains(lower, "insert into") {
			m.findInsert(s, lower, add)
		}
		if strings.Contains(s, "</") {
			m.findXML(s, add)
		}
	}
	m.findExtras(s, lower, longNum, add)
	if on("user") && m.people != nil && m.people.HasCodes() {
		forEachCode(s, func(ini, end int) {
			if m.people.IsCode(m.p, s[ini:end]) {
				add(ini, end, "user")
			}
		})
	}
	if on("mac") && strings.Count(s, ":")+strings.Count(s, "-") >= 5 {
		for _, ix := range reMAC.FindAllStringIndex(s, -1) {
			add(ix[0], ix[1], "mac")
		}
	}
	if on("email") && strings.IndexByte(s, '@') >= 0 {
		for _, ix := range reEmail.FindAllStringIndex(s, -1) {
			if m.allowedEmail(s[ix[0]:ix[1]]) || inURLAuthority(s, ix[0]) {
				continue
			}
			add(ix[0], ix[1], "email")
		}
	}
	if on("ip") && ipLike {
		for _, ix := range reIPv4.FindAllStringIndex(s, -1) {
			if !numberEdge(s, ix[0], ix[1]) {
				continue
			}
			a, err := netip.ParseAddr(s[ix[0]:ix[1]])
			if err != nil || !internalIP(a) {
				continue
			}
			add(ix[0], ix[1], "ip")
		}
	}
	if on("ip") && strings.Contains(lower, ".in-addr.arpa") {
		findReverseV4(s, func(ini, end int) { add(ini, end, "ip") })
	}
	if on("ip") && digitHyphenDigit(s) {
		findHyphenIP(s, func(_, ini, end int, a netip.Addr) {
			if internalIP(a) {
				add(ini, end, "ip")
			}
		})
	}
	if on("host") && len(m.cfg.InternalDomains) > 0 && m.hasInternal(lower) {
		for _, ix := range reHost.FindAllStringIndex(s, -1) {
			h := strings.ToLower(s[ix[0]:ix[1]])
			if (ix[0] > 0 && s[ix[0]-1] == '@') || strings.HasSuffix(h, ".invalid") || !m.internal(h) {
				continue
			}
			add(ix[0], ix[1], "host")
		}
	}
	if on("cnpj") && longNum {
		for _, ix := range reCNPJ.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			if digitEdge(s, ix[0], ix[1]) && ValidCNPJ(v) && (formatted(v) || contextOf(s, ix[0], "cnpj") ||
				(m.cfg.DocumentsWithoutContext && isolatedNumberOnly(s, ix[0], ix[1]) && !gluedDateTime(v))) {
				add(ix[0], ix[1], "cnpj")
			}
		}
	}
	if on("cpf") && longNum {
		for _, ix := range reCPF.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			if digitEdge(s, ix[0], ix[1]) && ValidCPF(v) && (formatted(v) || contextOf(s, ix[0], "cpf") ||
				(m.cfg.DocumentsWithoutContext && isolatedNumberOnly(s, ix[0], ix[1]))) {
				add(ix[0], ix[1], "cpf")
			}
		}
	}
	if on("pis") && longNum {
		for _, ix := range re11.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			if digitEdge(s, ix[0], ix[1]) && ValidPIS(v) && (strings.Count(v, ".") == 2 || anyContext(s, ix[0], "pis", "pasep", "nis", "nit")) {
				add(ix[0], ix[1], "pis")
			}
		}
	}
	if on("cnh") && longNum && has("cnh") {
		for _, ix := range re11.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			if digitEdge(s, ix[0], ix[1]) && ValidCNH(v) && contextOf(s, ix[0], "cnh") {
				add(ix[0], ix[1], "cnh")
			}
		}
	}
	if on("phone") && longNum {
		for _, ix := range reTel.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			if !digitEdge(s, ix[0], ix[1]) {
				continue
			}
			strong := strings.HasPrefix(v, "+55") || reDDD.MatchString(v) // "(11) 9...": parenthesis opened AND closed
			if strong || (strings.ContainsAny(v, "- ") && anyContext(s, ix[0], "tel", "fone", "celular", "whatsapp", "contato", "cel")) {
				add(ix[0], ix[1], "phone")
			}
		}
	}
	if on("cep") && has("cep") {
		for _, ix := range reCEP.FindAllStringIndex(s, -1) {
			// the whole word "cep" (not inside "exception", "conceito"), and the number cannot be a
			// date written together (20261003)
			if digitEdge(s, ix[0], ix[1]) && wordContext(s, ix[0], "cep") && !gluedDate(s[ix[0]:ix[1]]) {
				add(ix[0], ix[1], "cep")
			}
		}
	}
	if on("card") && longNum {
		for _, ix := range reCard.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			if !digitEdge(s, ix[0], ix[1]) || !ValidLuhn(v) {
				continue
			}
			if reGrouped.MatchString(v) || anyContext(s, ix[0], "cart", "card", "credito", "crédito", "debito", "débito") {
				add(ix[0], ix[1], "card")
			}
		}
	}
	if on("rg") && has("rg") {
		for _, ix := range reRG.FindAllStringIndex(s, -1) {
			if digitEdge(s, ix[0], ix[1]) && wordContext(s, ix[0], "rg") {
				add(ix[0], ix[1], "rg")
			}
		}
	}
	if on("account") && has("ag", "conta", "c/c") {
		for _, re := range []*regexp.Regexp{reAg, reAccount} {
			for _, ix := range re.FindAllStringSubmatchIndex(s, -1) {
				add(ix[2], ix[3], "account")
			}
		}
	}
	if on("pix") && has("pix") {
		for _, ix := range rePix.FindAllStringIndex(s, -1) {
			if contextOf(s, ix[0], "pix") {
				add(ix[0], ix[1], "pix")
			}
		}
	}
	if on("address") && has("rua", "r.", "av", "travessa", "tv.", "alameda", "al.", "rodovia", "rod.", "estrada", "praça", "praca", "largo") {
		for _, ix := range reEnd.FindAllStringIndex(s, -1) {
			add(ix[0], ix[1], "address")
		}
	}
	if on("birthdate") && has("nasc") {
		for _, ix := range reBirth.FindAllStringSubmatchIndex(s, -1) {
			add(ix[2], ix[3], "birthdate")
		}
	}
	if on("name") && m.people != nil {
		for _, a := range m.people.FindNames(s, m.p) {
			out = append(out, a)
		}
	}
	for i, re := range m.extras {
		for _, ix := range re.FindAllStringIndex(s, -1) {
			add(ix[0], ix[1], "x:"+m.extraLabel[i])
		}
	}
	if m.terms != nil {
		for _, ix := range m.terms.FindAllStringIndex(s, -1) {
			add(ix[0], ix[1], "t:"+m.termLabel[strings.ToLower(s[ix[0]:ix[1]])])
		}
	}
	return out
}

func (m *Masker) hasInternal(lower string) bool { return m.internal(lower) }

func isB64(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=' || c == '-' || c == '_'
}

// encodedSnippet finds, on line `line` (0-based) near the column `col` reported by gitleaks, the
// whole base64/hex piece containing that position. The gitleaks column is off by 1–2 characters
// depending on the line; looking for the whole piece around it makes that irrelevant.
func encodedSnippet(s string, line, col int) (int, int, bool) {
	ini := 0
	for l := 0; l < line; l++ {
		k := strings.IndexByte(s[ini:], '\n')
		if k < 0 {
			return 0, 0, false
		}
		ini += k + 1
	}
	lineEnd := len(s)
	if k := strings.IndexByte(s[ini:], '\n'); k >= 0 {
		lineEnd = ini + k
	}
	for _, p := range []int{ini + col - 1, ini + col - 2, ini + col} {
		if p < ini || p >= lineEnd || !isB64(s[p]) {
			continue
		}
		a, b := p, p
		for a > ini && isB64(s[a-1]) {
			a--
		}
		for b < lineEnd && isB64(s[b]) {
			b++
		}
		if b-a >= 16 {
			return a, b, true
		}
	}
	return 0, 0, false
}

// numericProfile: is there a long number (>= 8 digits, accepting . - / ( ) space and +)? And
// something IPv4-shaped (digits with 3 dots)? A single pass, without regex.
func numericProfile(s string) (longNum, ipLike bool) {
	dig, dots, ipDigits := 0, 0, 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			dig++
			ipDigits++
		case c == '.':
			if ipDigits > 0 {
				dots++
			}
			ipDigits = 0
		case c == '-' || c == '/' || c == '(' || c == ')' || c == ' ' || c == '+':
			ipDigits, dots = 0, 0
		default:
			dig, ipDigits, dots = 0, 0, 0
		}
		if dig >= 8 {
			longNum = true
		}
		if dots >= 3 && ipDigits > 0 {
			ipLike = true
		}
		if longNum && ipLike {
			return
		}
	}
	return
}

// digitEdge: the piece is not glued to other digits (it is a whole number, not a piece).
func digitEdge(s string, i, j int) bool {
	return (i == 0 || !isDig(s[i-1])) && (j >= len(s) || !isDig(s[j]))
}

// Physical address of a network card (identifies a device, and through it a person)
var reMAC = regexp.MustCompile(`\b(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}\b`)

// gluedDate: 8 digits that form a plausible date (YYYYMMDD or DDMMYYYY).
func gluedDate(v string) bool {
	if len(v) != 8 {
		return false
	}
	n := func(a, b int) int {
		x := 0
		for _, c := range v[a:b] {
			if c < '0' || c > '9' {
				return -1
			}
			x = x*10 + int(c-'0')
		}
		return x
	}
	ok := func(year, month, day int) bool {
		return year >= 1900 && year <= 2100 && month >= 1 && month <= 12 && day >= 1 && day <= 31
	}
	return ok(n(0, 4), n(4, 6), n(6, 8)) || ok(n(4, 8), n(2, 4), n(0, 2))
}

// isolatedNumberOnly: s[i:j] is a whole number on its own, not a piece of something else: it is not
// glued to a letter (hash, identifier), nor is it the decimal part or a piece of a larger number
// with a dot, comma or dash ("3.14159265358", "v1.31845276035").
func isolatedNumberOnly(s string, i, j int) bool {
	if i > 0 {
		a := s[i-1]
		if isAlnum(a) || a == '_' {
			return false
		}
		if (a == '.' || a == ',' || a == '-') && i > 1 && isAlnum(s[i-2]) {
			return false
		}
	}
	if j < len(s) {
		b := s[j]
		if isAlnum(b) || b == '_' {
			return false
		}
		if (b == '.' || b == ',' || b == '-') && j+1 < len(s) && isDig(s[j+1]) {
			return false
		}
	}
	return true
}

// gluedDateTime: 14 digits in the YYYYMMDDhhmmss format (date and time stamp).
func gluedDateTime(v string) bool {
	d := digitsOnly(v)
	if len(d) != 14 || !gluedDate(d[:8]) {
		return false
	}
	n := func(a, b int) int {
		x := 0
		for _, c := range d[a:b] {
			x = x*10 + int(c-'0')
		}
		return x
	}
	return n(8, 10) < 24 && n(10, 12) < 60 && n(12, 14) < 60
}
