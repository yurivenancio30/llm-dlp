package mask

import (
	"regexp"
	"strings"
)

// Detectores de credencial em cabeçalho HTTP, IBAN, processo judicial e documentos que
// dependem da palavra por perto (título de eleitor, cartão SUS, RENAVAM...).

var (
	// processo judicial (numeração única do CNJ): NNNNNNN-DD.AAAA.J.TR.OOOO
	reCNJ = regexp.MustCompile(`\b\d{7}-\d{2}\.\d{4}\.\d\.\d{2}\.\d{4}\b`)
	// CNPJ alfanumérico com a pontuação
	reCNPJAlfa = regexp.MustCompile(`\b[A-Za-z0-9]{2}\.[A-Za-z0-9]{3}\.[A-Za-z0-9]{3}/[A-Za-z0-9]{4}-\d{2}\b`)

	// documentos que só contam com a palavra por perto
	reVoterID  = regexp.MustCompile(`(?i)(?:t[ií]tulo(?: de eleitor| eleitoral)?|eleitor)\D{0,20}(\d{4}\s?\d{4}\s?\d{4})\b`)
	reCNS      = regexp.MustCompile(`(?i)(?:\bcns\b|cart[aã]o (?:nacional de sa[uú]de|do sus|sus))\D{0,20}(\d{3}\s?\d{4}\s?\d{4}\s?\d{4})\b`)
	reRenavam  = regexp.MustCompile(`(?i)renavam\D{0,20}(\d{9,11})\b`)
	rePassport = regexp.MustCompile(`(?i)(?:passaporte|passport)\W{0,20}([A-Za-z]{2}\d{6})\b`)
	rePlate    = regexp.MustCompile(`(?i)\bplaca\W{0,20}([A-Za-z]{3}-?\d[A-Za-z0-9]\d{2})\b`)
	reChassis  = regexp.MustCompile(`(?i)(?:chassi|\bvin\b)\W{0,20}([A-HJ-NPR-Za-hj-npr-z0-9]{17})\b`)
	reIMEI     = regexp.MustCompile(`(?i)\bimei\D{0,20}(\d{15})\b`)
	// GUID que é o valor de um campo terminado em "id" (workspace_id: X, "lakehouseId": "X",
	// tenant_id=X, "id": "X"): identifica um recurso do cliente (workspace, tenant, assinatura)
	reGUIDDeID = regexp.MustCompile(`(?i)([a-z0-9_.-]*id)["']?\s*[:=]\s*["']?([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\b`)
	// o id do aplicativo OAuth não é segredo (RFC 6749, 2.2): vai na URL de login do navegador
	publicOAuthID = setOf("clientid", "appid", "applicationid")

	// credenciais em cabeçalho HTTP (saída de curl -v, HAR, log de proxy)
	reAuthHTTP = regexp.MustCompile("(?i)\\b(?:proxy-)?authorization[\"']?\\s*[:=]\\s*[\"']?(?:basic|bearer|digest|token|negotiate|apikey)\\s+([^\\s\"',;]{8,})")
	reCookie   = regexp.MustCompile("(?i)\\b(?:set-)?cookie[\"']?\\s*[:=]\\s*[\"']?([^\\n\"']{6,})")
)

var cookieAttribute = setOf("path", "domain", "expires", "max-age", "samesite", "httponly", "secure", "priority", "partitioned")

// findExtras roda os detectores deste arquivo.
func (m *Masker) findExtras(s, lower string, longNum bool, add func(ini, end int, typ string)) {
	on := func(d string) bool { return !m.cfg.Disabled(d) }
	if on("segredo") {
		if strings.Contains(lower, "authorization") {
			for _, ix := range reAuthHTTP.FindAllStringSubmatchIndex(s, -1) {
				if !strings.ContainsAny(s[ix[2]:ix[2]+1], "$<{%*") { // ${TOKEN}, <seu-token>: marcador, não credencial
					add(ix[2], ix[3], "segredo")
				}
			}
		}
		if strings.Contains(lower, "cookie") {
			for _, ix := range reCookie.FindAllStringSubmatchIndex(s, -1) {
				// cada "nome=valor;": mascara o valor, menos os atributos (Path, Expires...)
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
							add(a, b, "segredo")
						}
					}
					p = q + 1
				}
			}
		}
	}
	if on("processo") && longNum && strings.Contains(s, ".") {
		for _, ix := range reCNJ.FindAllStringIndex(s, -1) {
			add(ix[0], ix[1], "processo")
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
	if on("rastreio") && longNum {
		findTrackingCodes(s, add)
	}
	if on("rg") && (strings.Contains(lower, "rg") || strings.Contains(lower, "r.g.") || strings.Contains(lower, "identidade") ||
		strings.Contains(lower, "registro geral")) {
		findLabeledRG(s, add)
	}
	if on("certidao") && longNum && len(s) >= 32 {
		findCertificates(s, add)
	}
	if on("sensivel") {
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

// findIBAN procura, sem regex, palavras com cara de IBAN (AA99 + letras e dígitos, com ou
// sem espaços a cada 4) e confere o mod 97.
func findIBAN(s string, add func(ini, end int, typ string)) {
	capital := func(b byte) bool { return b >= 'A' && b <= 'Z' }
	dig := func(b byte) bool { return b >= '0' && b <= '9' }
	for i := 0; i+15 <= len(s); i++ {
		if !capital(s[i]) || !capital(s[i+1]) || !dig(s[i+2]) || !dig(s[i+3]) || (i > 0 && isAlnum(s[i-1])) {
			continue
		}
		// junta até 34 caracteres, aceitando um espaço entre grupos
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
		// o IBAN pode acabar antes do fim do que foi juntado (texto colado logo depois)
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
				break // sem espaços, só vale a palavra inteira
			}
		}
	}
}
