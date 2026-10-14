package mask

import (
	"regexp"
	"strings"
)

// Detectors of credentials in HTTP headers, IBAN, court case numbers and documents that depend on a
// nearby word (voter ID, SUS card, RENAVAM...).

var (
	// court case (CNJ unique numbering): NNNNNNN-DD.AAAA.J.TR.OOOO
	reCNJ = regexp.MustCompile(`\b\d{7}-\d{2}\.\d{4}\.\d\.\d{2}\.\d{4}\b`)
	// alphanumeric CNPJ with punctuation
	reCNPJAlfa = regexp.MustCompile(`\b[A-Za-z0-9]{2}\.[A-Za-z0-9]{3}\.[A-Za-z0-9]{3}/[A-Za-z0-9]{4}-\d{2}\b`)

	// documents that only count with the word nearby
	reVoterID  = regexp.MustCompile(`(?i)(?:t[ií]tulo(?: de eleitor| eleitoral)?|eleitor)\D{0,20}(\d{4}\s?\d{4}\s?\d{4})\b`)
	reCNS      = regexp.MustCompile(`(?i)(?:\bcns\b|cart[aã]o (?:nacional de sa[uú]de|do sus|sus))\D{0,20}(\d{3}\s?\d{4}\s?\d{4}\s?\d{4})\b`)
	reRenavam  = regexp.MustCompile(`(?i)renavam\D{0,20}(\d{9,11})\b`)
	rePassport = regexp.MustCompile(`(?i)(?:passaporte|passport)\W{0,20}([A-Za-z]{2}\d{6})\b`)
	rePlate    = regexp.MustCompile(`(?i)\bplaca\W{0,20}([A-Za-z]{3}-?\d[A-Za-z0-9]\d{2})\b`)
	reChassis  = regexp.MustCompile(`(?i)(?:chassi|\bvin\b)\W{0,20}([A-HJ-NPR-Za-hj-npr-z0-9]{17})\b`)
	reIMEI     = regexp.MustCompile(`(?i)\bimei\D{0,20}(\d{15})\b`)
	// a GUID that is the value of a field ending in "id" (workspace_id: X, "lakehouseId": "X",
	// tenant_id=X, "id": "X"): it identifies a client resource (workspace, tenant, subscription)
	reGUIDDeID = regexp.MustCompile(`(?i)([a-z0-9_.-]*id)["']?\s*[:=]\s*["']?([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\b`)
	// the OAuth application id is not a secret (RFC 6749, 2.2): it goes in the browser login URL
	publicOAuthID = setOf("clientid", "appid", "applicationid")

	// credentials in an HTTP header (curl -v output, HAR, proxy log)
	reAuthHTTP = regexp.MustCompile("(?i)\\b(?:proxy-)?authorization[\"']?\\s*[:=]\\s*[\"']?(?:basic|bearer|digest|token|negotiate|apikey)\\s+([^\\s\"',;]{8,})")
	reCookie   = regexp.MustCompile("(?i)\\b(?:set-)?cookie[\"']?\\s*[:=]\\s*[\"']?([^\\n\"']{6,})")
)

var cookieAttribute = setOf("path", "domain", "expires", "max-age", "samesite", "httponly", "secure", "priority", "partitioned")

// findExtras runs the detectors of this file.
func (m *Masker) findExtras(s, lower string, longNum bool, add func(ini, end int, typ string)) {
	on := func(d string) bool { return !m.cfg.Disabled(d) }
	if on("secret") {
		if strings.Contains(lower, "authorization") {
			for _, ix := range reAuthHTTP.FindAllStringSubmatchIndex(s, -1) {
				if !strings.ContainsAny(s[ix[2]:ix[2]+1], "$<{%*") { // ${TOKEN}, <your-token>: a marker, not a credential
					add(ix[2], ix[3], "secret")
				}
			}
		}
		if strings.Contains(lower, "cookie") {
			for _, ix := range reCookie.FindAllStringSubmatchIndex(s, -1) {
				// each "name=value;": masks the value, except the attributes (Path, Expires...)
				for p := ix[2]; p < ix[3]; {
					q := p
					for q < ix[3] && s[q] != ';' {
						q++
					}
					if eq := strings.IndexByte(s[p:q], '='); eq > 0 {
						name := strings.ToLower(strings.TrimSpace(s[p : p+eq]))
						a, b := p+eq+1, q
						for b > a && s[b-1] == ' ' {
							b--
						}
						if !cookieAttribute[name] && b-a >= 8 {
							add(a, b, "secret")
						}
					}
					p = q + 1
				}
			}
		}
	}
	if on("lawsuit") && longNum && strings.Contains(s, ".") {
		for _, ix := range reCNJ.FindAllStringIndex(s, -1) {
			add(ix[0], ix[1], "lawsuit")
		}
	}
	if on("cnpj") && strings.Contains(s, "/") {
		for _, ix := range reCNPJAlfa.FindAllStringIndex(s, -1) {
			if ValidAlphaCNPJ(s[ix[0]:ix[1]]) {
				add(ix[0], ix[1], "cnpj")
			}
		}
	}
	if on("iban") {
		findIBAN(s, add)
	}
	if on("nfe") && longNum && len(s) >= 44 {
		findDFeKey(s, add)
	}
	if on("pix") && strings.Contains(lower, "br.gov.bcb.pix") {
		findBRCode(s, add)
	}
	if on("boleto") && longNum && len(s) >= 44 {
		findBoletos(s, add)
	}
	if on("tracking") && longNum {
		findTrackingCodes(s, add)
	}
	if on("rg") && (strings.Contains(lower, "rg") || strings.Contains(lower, "r.g.") || strings.Contains(lower, "identidade") ||
		strings.Contains(lower, "registro geral")) {
		findLabeledRG(s, add)
	}
	if on("certificate") && longNum && len(s) >= 32 {
		findCertificates(s, add)
	}
	if on("sensitive") {
		if hasCIDTrigger(lower) {
			findCID(s, add)
		}
		findSensitive(s, lower, add)
	}
	if !on("doc") {
		return
	}
	with := func(re *regexp.Regexp, ok func(string) bool) {
		for _, ix := range re.FindAllStringSubmatchIndex(s, -1) {
			if ok == nil || ok(s[ix[2]:ix[3]]) {
				add(ix[2], ix[3], "doc")
			}
		}
	}
	has := func(ps ...string) bool {
		for _, p := range ps {
			if strings.Contains(lower, p) {
				return true
			}
		}
		return false
	}
	if strings.Contains(lower, "id") && strings.Count(s, "-") >= 4 {
		for _, ix := range reGUIDDeID.FindAllStringSubmatchIndex(s, -1) {
			if !publicOAuthID[strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(s[ix[2]:ix[3]]))] {
				add(ix[4], ix[5], "doc")
			}
		}
	}
	if longNum && has("eleitor", "titulo", "título") {
		with(reVoterID, ValidVoterID)
	}
	if longNum && has("cns", "sus", "saude", "saúde") {
		with(reCNS, ValidCNS)
	}
	if longNum && has("renavam") {
		with(reRenavam, ValidRenavam)
	}
	if has("passaporte", "passport") {
		with(rePassport, nil)
	}
	if has("placa") {
		with(rePlate, nil)
	}
	if has("chassi", "vin") {
		with(reChassis, hasDigit)
	}
	if longNum && has("imei") {
		with(reIMEI, ValidLuhn)
	}
}

// findIBAN looks, without regex, for IBAN-shaped words (AA99 + letters and digits, with or without
// spaces every 4) and checks mod 97.
func findIBAN(s string, add func(ini, end int, typ string)) {
	capital := func(b byte) bool { return b >= 'A' && b <= 'Z' }
	dig := func(b byte) bool { return b >= '0' && b <= '9' }
	for i := 0; i+15 <= len(s); i++ {
		if !capital(s[i]) || !capital(s[i+1]) || !dig(s[i+2]) || !dig(s[i+3]) || (i > 0 && isAlnum(s[i-1])) {
			continue
		}
		// joins up to 34 characters, accepting one space between groups
		j, n := i, 0
		for j < len(s) && n < 34 {
			if capital(s[j]) || dig(s[j]) {
				n++
				j++
			} else if s[j] == ' ' && j+1 < len(s) && (capital(s[j+1]) || dig(s[j+1])) && n%4 == 0 {
				j++
			} else {
				break
			}
		}
		if j < len(s) && isAlnum(s[j]) {
			continue
		}
		// the IBAN may end before the end of what was joined (text glued right after)
		for end := j; end-i >= 15; end-- {
			if s[end-1] == ' ' {
				continue
			}
			if ValidIBAN(s[i:end]) {
				add(i, end, "iban")
				i = end
				break
			}
			if !strings.Contains(s[i:end], " ") {
				break // without spaces, only the whole word counts
			}
		}
	}
}
