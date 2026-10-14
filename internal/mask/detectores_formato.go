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

// Detectores por formato: e-mail, IP, hostname, CPF, CNPJ, telefone, cartão, tokens (gitleaks)...

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
	fakeNet = netip.MustParsePrefix("240.0.0.0/4") // faixa reservada (RFC 1112): só pseudônimos
)

// inURLAuthority: a posição i está na autoridade de uma URL ("esquema://usuario:senha@host"),
// antes de qualquer "/", "?" ou "#". Ali, "senha@host" não é e-mail: é a senha e o servidor,
// que os leitores de conexão e de senha mascaram cada um com o seu tipo. Um e-mail no caminho ou
// nos parâmetros da URL (?email=...) continua sendo e-mail.
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

// numberEdge: como digitEdge, mas também rejeita "1.2.3.4.5" (versões, OIDs).
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

// contexto: a palavra aparece nos ~30 caracteres antes do trecho.
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

// wordContext: como contexto, mas exige a palavra inteira (evita "rg" dentro de "cargo").
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

// detectBase: todos os detectores, um por um.

// janelas do gitleaks (ver Detect)
const (
	leaksWindow   = 64 << 10
	leaksLeftover = 8 << 10
)

// detectBase roda os detectores sobre s, sem a memória de valores conhecidos.
func (m *Masker) detectBase(s string) []Finding {
	var out []Finding
	add := func(ini, end int, typ string) {
		out = append(out, Finding{ini, end, typ, s[ini:end]})
	}
	// Pré-filtros baratos: cada detector só roda se o texto tiver o "ingrediente" dele.
	// Sem isso, ~15 regex varrem todo texto (≈6 ms a cada 5 KB); com isso, quase nada roda
	// em texto comum.
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

	// a própria chave do llm-dlp nunca pode sair
	for i := 0; ; {
		j := strings.Index(s[i:], m.keyB64)
		if j < 0 {
			break
		}
		add(i+j, i+j+len(m.keyB64), "segredo")
		i += j + len(m.keyB64)
	}

	if on("segredo") {
		// O gitleaks fica desproporcionalmente lento em textos grandes (acima de ~128 KB o
		// tempo cresce mais que o tamanho). Por isso o texto vai em janelas, em paralelo, com
		// sobra entre elas para não cortar ao meio um segredo de várias linhas (chave privada
		// PEM). Com um bloco PEM maior que a sobra, vai inteiro.
		type window struct{ base, end int }
		var js []window
		if len(s) <= leaksWindow || longBlock(s, leaksLeftover) {
			js = []window{{0, len(s)}}
		} else {
			for base := 0; ; {
				end := min(len(s), base+leaksWindow)
				for end < len(s) && end > base && s[end]&0xC0 == 0x80 {
					end-- // não corta um caractere UTF-8 ao meio
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
					add(f.base+k+j, f.base+k+j+len(f.value), "segredo")
					k += j + len(f.value)
					foundIt = true
				}
				if !foundIt {
					// segredo achado DEPOIS de decodificar (ex.: base64 de um Secret do Kubernetes):
					// ele não aparece literal no texto; cobre o trecho codificado naquela posição,
					// inteiro (mesmo a parte que ficou fora desta janela)
					if ini, f2, ok := encodedSnippet(part, f.line, f.col); ok {
						a, b := f.base+ini, f.base+f2
						for a > 0 && isB64(s[a-1]) {
							a--
						}
						for b < len(s) && isB64(s[b]) {
							b++
						}
						add(a, b, "segredo")
					}
				}
			}
		}
	}
	if on("segredo") {
		m.findPasswords(s, lower, add)
	}
	if on("campo") {
		m.findFields(s, add)
		if strings.Contains(lower, "insert into") {
			m.findInsert(s, lower, add)
		}
		if strings.Contains(s, "</") {
			m.findXML(s, add)
		}
	}
	m.findExtras(s, lower, longNum, add)
	if on("usuario") && m.people != nil && m.people.HasCodes() {
		forEachCode(s, func(ini, end int) {
			if m.people.IsCode(m.p, s[ini:end]) {
				add(ini, end, "usuario")
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
	if on("telefone") && longNum {
		for _, ix := range reTel.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			if !digitEdge(s, ix[0], ix[1]) {
				continue
			}
			strong := strings.HasPrefix(v, "+55") || reDDD.MatchString(v) // "(11) 9...": parêntese aberto E fechado
			if strong || (strings.ContainsAny(v, "- ") && anyContext(s, ix[0], "tel", "fone", "celular", "whatsapp", "contato", "cel")) {
				add(ix[0], ix[1], "telefone")
			}
		}
	}
	if on("cep") && has("cep") {
		for _, ix := range reCEP.FindAllStringIndex(s, -1) {
			// a palavra "cep" inteira (não dentro de "exception", "conceito"), e o número não
			// pode ser uma data escrita junta (20261003)
			if digitEdge(s, ix[0], ix[1]) && wordContext(s, ix[0], "cep") && !gluedDate(s[ix[0]:ix[1]]) {
				add(ix[0], ix[1], "cep")
			}
		}
	}
	if on("cartao") && longNum {
		for _, ix := range reCard.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			if !digitEdge(s, ix[0], ix[1]) || !ValidLuhn(v) {
				continue
			}
			if reGrouped.MatchString(v) || anyContext(s, ix[0], "cart", "card", "credito", "crédito", "debito", "débito") {
				add(ix[0], ix[1], "cartao")
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
	if on("conta") && has("ag", "conta", "c/c") {
		for _, re := range []*regexp.Regexp{reAg, reAccount} {
			for _, ix := range re.FindAllStringSubmatchIndex(s, -1) {
				add(ix[2], ix[3], "conta")
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
	if on("endereco") && has("rua", "r.", "av", "travessa", "tv.", "alameda", "al.", "rodovia", "rod.", "estrada", "praça", "praca", "largo") {
		for _, ix := range reEnd.FindAllStringIndex(s, -1) {
			add(ix[0], ix[1], "endereco")
		}
	}
	if on("nascimento") && has("nasc") {
		for _, ix := range reBirth.FindAllStringSubmatchIndex(s, -1) {
			add(ix[2], ix[3], "nascimento")
		}
	}
	if on("nome") && m.people != nil {
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

// encodedSnippet acha, na linha `linha` (0-based) perto da coluna `col` informada pelo
// gitleaks, o trecho base64/hex inteiro que contém essa posição. A coluna do gitleaks tem
// deslocamento de 1–2 caracteres conforme a linha; procurar o trecho inteiro em volta dela
// torna isso irrelevante.
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

// numericProfile: há um número longo (>= 8 dígitos, aceitando . - / ( ) espaço e +)? E algo
// com cara de IPv4 (dígitos com 3 pontos)? Uma passada só, sem regex.
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

// digitEdge: o trecho não está colado a outros dígitos (é um número inteiro, não um pedaço).
func digitEdge(s string, i, j int) bool {
	return (i == 0 || !isDig(s[i-1])) && (j >= len(s) || !isDig(s[j]))
}

// Endereço físico de placa de rede (identifica um aparelho, e por ele uma pessoa)
var reMAC = regexp.MustCompile(`\b(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}\b`)

// gluedDate: 8 dígitos que formam uma data plausível (AAAAMMDD ou DDMMAAAA).
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

// isolatedNumberOnly: s[i:j] é um número inteiro sozinho, não um pedaço de outra coisa: não está
// colado em letra (hash, identificador), nem é a parte decimal ou um trecho de um número
// maior com ponto, vírgula ou traço ("3.14159265358", "v1.31845276035").
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

// gluedDateTime: 14 dígitos no formato AAAAMMDDhhmmss (carimbo de data e hora).
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
