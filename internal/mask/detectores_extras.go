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
	reTitulo     = regexp.MustCompile(`(?i)(?:t[ií]tulo(?: de eleitor| eleitoral)?|eleitor)\D{0,20}(\d{4}\s?\d{4}\s?\d{4})\b`)
	reCNS        = regexp.MustCompile(`(?i)(?:\bcns\b|cart[aã]o (?:nacional de sa[uú]de|do sus|sus))\D{0,20}(\d{3}\s?\d{4}\s?\d{4}\s?\d{4})\b`)
	reRenavam    = regexp.MustCompile(`(?i)renavam\D{0,20}(\d{9,11})\b`)
	rePassaporte = regexp.MustCompile(`(?i)(?:passaporte|passport)\W{0,20}([A-Za-z]{2}\d{6})\b`)
	rePlaca      = regexp.MustCompile(`(?i)\bplaca\W{0,20}([A-Za-z]{3}-?\d[A-Za-z0-9]\d{2})\b`)
	reChassi     = regexp.MustCompile(`(?i)(?:chassi|\bvin\b)\W{0,20}([A-HJ-NPR-Za-hj-npr-z0-9]{17})\b`)
	reIMEI       = regexp.MustCompile(`(?i)\bimei\D{0,20}(\d{15})\b`)

	// credenciais em cabeçalho HTTP (saída de curl -v, HAR, log de proxy)
	reAuthHTTP = regexp.MustCompile("(?i)\\b(?:proxy-)?authorization[\"']?\\s*[:=]\\s*[\"']?(?:basic|bearer|digest|token|negotiate|apikey)\\s+([^\\s\"',;]{8,})")
	reCookie   = regexp.MustCompile("(?i)\\b(?:set-)?cookie[\"']?\\s*[:=]\\s*[\"']?([^\\n\"']{6,})")
)

var atributoCookie = conj("path", "domain", "expires", "max-age", "samesite", "httponly", "secure", "priority", "partitioned")

// acharExtras roda os detectores deste arquivo.
func (m *Masker) acharExtras(s, baixo string, numLongo bool, add func(ini, fim int, tipo string)) {
	on := func(d string) bool { return !m.cfg.Desligado(d) }
	if on("segredo") {
		if strings.Contains(baixo, "authorization") {
			for _, ix := range reAuthHTTP.FindAllStringSubmatchIndex(s, -1) {
				if !strings.ContainsAny(s[ix[2]:ix[2]+1], "$<{%*") { // ${TOKEN}, <seu-token>: marcador, não credencial
					add(ix[2], ix[3], "segredo")
				}
			}
		}
		if strings.Contains(baixo, "cookie") {
			for _, ix := range reCookie.FindAllStringSubmatchIndex(s, -1) {
				// cada "nome=valor;": mascara o valor, menos os atributos (Path, Expires...)
				for p := ix[2]; p < ix[3]; {
					q := p
					for q < ix[3] && s[q] != ';' {
						q++
					}
					if eq := strings.IndexByte(s[p:q], '='); eq > 0 {
						nome := strings.ToLower(strings.TrimSpace(s[p : p+eq]))
						a, b := p+eq+1, q
						for b > a && s[b-1] == ' ' {
							b--
						}
						if !atributoCookie[nome] && b-a >= 8 {
							add(a, b, "segredo")
						}
					}
					p = q + 1
				}
			}
		}
	}
	if on("processo") && numLongo && strings.Contains(s, ".") {
		for _, ix := range reCNJ.FindAllStringIndex(s, -1) {
			add(ix[0], ix[1], "processo")
		}
	}
	if on("cnpj") && strings.Contains(s, "/") {
		for _, ix := range reCNPJAlfa.FindAllStringIndex(s, -1) {
			if CNPJAlfaValido(s[ix[0]:ix[1]]) {
				add(ix[0], ix[1], "cnpj")
			}
		}
	}
	if on("iban") {
		acharIBAN(s, add)
	}
	if !on("doc") {
		return
	}
	com := func(re *regexp.Regexp, ok func(string) bool) {
		for _, ix := range re.FindAllStringSubmatchIndex(s, -1) {
			if ok == nil || ok(s[ix[2]:ix[3]]) {
				add(ix[2], ix[3], "doc")
			}
		}
	}
	tem := func(ps ...string) bool {
		for _, p := range ps {
			if strings.Contains(baixo, p) {
				return true
			}
		}
		return false
	}
	if numLongo && tem("eleitor", "titulo", "título") {
		com(reTitulo, TituloValido)
	}
	if numLongo && tem("cns", "sus", "saude", "saúde") {
		com(reCNS, CNSValido)
	}
	if numLongo && tem("renavam") {
		com(reRenavam, RenavamValido)
	}
	if tem("passaporte", "passport") {
		com(rePassaporte, nil)
	}
	if tem("placa") {
		com(rePlaca, nil)
	}
	if tem("chassi", "vin") {
		com(reChassi, temDigito)
	}
	if numLongo && tem("imei") {
		com(reIMEI, LuhnValido)
	}
}

// acharIBAN procura, sem regex, palavras com cara de IBAN (AA99 + letras e dígitos, com ou
// sem espaços a cada 4) e confere o mod 97.
func acharIBAN(s string, add func(ini, fim int, tipo string)) {
	maiuscula := func(b byte) bool { return b >= 'A' && b <= 'Z' }
	dig := func(b byte) bool { return b >= '0' && b <= '9' }
	for i := 0; i+15 <= len(s); i++ {
		if !maiuscula(s[i]) || !maiuscula(s[i+1]) || !dig(s[i+2]) || !dig(s[i+3]) || (i > 0 && ehAlnum(s[i-1])) {
			continue
		}
		// junta até 34 caracteres, aceitando um espaço entre grupos
		j, n := i, 0
		for j < len(s) && n < 34 {
			if maiuscula(s[j]) || dig(s[j]) {
				n++
				j++
			} else if s[j] == ' ' && j+1 < len(s) && (maiuscula(s[j+1]) || dig(s[j+1])) && n%4 == 0 {
				j++
			} else {
				break
			}
		}
		if j < len(s) && ehAlnum(s[j]) {
			continue
		}
		// o IBAN pode acabar antes do fim do que foi juntado (texto colado logo depois)
		for fim := j; fim-i >= 15; fim-- {
			if s[fim-1] == ' ' {
				continue
			}
			if IBANValido(s[i:fim]) {
				add(i, fim, "iban")
				i = fim
				break
			}
			if !strings.Contains(s[i:fim], " ") {
				break // sem espaços, só vale a palavra inteira
			}
		}
	}
}
