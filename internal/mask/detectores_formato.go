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
	reEmail  = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)+`)
	reIPv4   = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`)
	reHost   = regexp.MustCompile(`(?i)(?:[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?\.)+[a-z]{2,}`)
	reCPF    = regexp.MustCompile(`\d{3}\.?\d{3}\.?\d{3}-?\d{2}`)
	reCNPJ   = regexp.MustCompile(`\d{2}\.?\d{3}\.?\d{3}/?\d{4}-?\d{2}`)
	reTel    = regexp.MustCompile(`(?:\+55[\s\-]?)?\(?\d{2}\)?[\s\-]?9?\d{4}[\s\-]?\d{4}`)
	reCEP    = regexp.MustCompile(`\d{5}-?\d{3}`)
	reCartao = regexp.MustCompile(`\d(?:[ \-]?\d){12,18}`)
	reRG     = regexp.MustCompile(`\d{1,2}\.?\d{3}\.?\d{3}-?[\dxX]`)
	re11     = regexp.MustCompile(`\d{3}\.?\d{5}\.?\d{2}-?\d|\d{11}`)
	reAg     = regexp.MustCompile(`(?i)\b(?:ag[eê]ncia|ag\.)\s*(?:n[ºo°.]?\s*)?[:=]?\s*(\d{3,5}(?:-[\dxX])?)`)
	reConta  = regexp.MustCompile(`(?i)\b(?:conta(?:\s+corrente|\s+poupan[cç]a)?|c/c)\s*(?:n[ºo°.]?\s*)?[:=]?\s*(\d{4,12}-?[\dxX]?)`)
	rePix    = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	reEnd    = regexp.MustCompile(`(?i)\b(?:rua|r\.|avenida|av\.|travessa|tv\.|alameda|al\.|rodovia|rod\.|estrada|pra[çc]a|largo)\s+[\p{L}0-9 .'\-]{2,60}?,\s*(?:n[ºo°.]?\s*)?\d{1,6}`)
	reNasc   = regexp.MustCompile(`(?i)\b(?:nascimento|nasc\.|data de nasc\w*|dt_?nasc\w*)\s*[:=]?\s*(\d{2}/\d{2}/\d{4}|\d{4}-\d{2}-\d{2})`)
	reDigNu  = regexp.MustCompile(`\d{11}|\d{14}`)

	reDDD      = regexp.MustCompile(`^\(\d{2}\)`)
	reAgrupado = regexp.MustCompile(`^\d{4}[ \-]\d{4}[ \-]\d{4}[ \-]\d{1,7}$`)
)

var (
	fakeNet = netip.MustParsePrefix("240.0.0.0/4") // faixa reservada (RFC 1112): só pseudônimos
)

func (m *Masker) interno(h string) bool {
	for _, d := range m.cfg.DominiosInternos {
		if d != "" && strings.Contains(h, strings.ToLower(d)) {
			return true
		}
	}
	return false
}

// --- bordas e contexto -------------------------------------------------------

func ehDig(b byte) bool { return b >= '0' && b <= '9' }

// bordaNum: como bordaDig, mas também rejeita "1.2.3.4.5" (versões, OIDs).
func bordaNum(s string, i, j int) bool {
	if !bordaDig(s, i, j) {
		return false
	}
	if i > 0 && s[i-1] == '.' {
		return false
	}
	if j < len(s)-1 && s[j] == '.' && ehDig(s[j+1]) {
		return false
	}
	return true
}

func formatado(v string) bool { return strings.ContainsAny(v, ".-/") }

// contexto: a palavra aparece nos ~30 caracteres antes do trecho.
func contexto(s string, i int, palavra string) bool {
	ini := i - 30
	if ini < 0 {
		ini = 0
	}
	for ini > 0 && !utf8.RuneStart(s[ini]) {
		ini--
	}
	return strings.Contains(strings.ToLower(s[ini:i]), palavra)
}

func contextoAlgum(s string, i int, palavras ...string) bool {
	for _, p := range palavras {
		if contexto(s, i, p) {
			return true
		}
	}
	return false
}

// contextoPalavra: como contexto, mas exige a palavra inteira (evita "rg" dentro de "cargo").
func contextoPalavra(s string, i int, palavra string) bool {
	ini := i - 30
	if ini < 0 {
		ini = 0
	}
	for ini > 0 && !utf8.RuneStart(s[ini]) {
		ini--
	}
	trecho := strings.ToLower(s[ini:i])
	for _, w := range strings.FieldsFunc(trecho, func(r rune) bool { return !unicode.IsLetter(r) }) {
		if w == palavra {
			return true
		}
	}
	return false
}

// detectarBase: todos os detectores, um por um.

// janelas do gitleaks (ver Detectar)
const (
	janelaLeaks = 64 << 10
	sobraLeaks  = 8 << 10
)

// detectarBase roda os detectores sobre s, sem a memória de valores conhecidos.
func (m *Masker) detectarBase(s string) []Achado {
	var out []Achado
	add := func(ini, fim int, tipo string) {
		out = append(out, Achado{ini, fim, tipo, s[ini:fim]})
	}
	// Pré-filtros baratos: cada detector só roda se o texto tiver o "ingrediente" dele.
	// Sem isso, ~15 regex varrem todo texto (≈6 ms a cada 5 KB); com isso, quase nada roda
	// em texto comum.
	baixo := strings.ToLower(s)
	tem := func(ps ...string) bool {
		for _, p := range ps {
			if strings.Contains(baixo, p) {
				return true
			}
		}
		return false
	}
	numLongo, ipLike := perfilNumerico(s)
	on := func(d string) bool { return !m.cfg.Desligado(d) }

	// a própria chave do llm-dlp nunca pode sair
	for i := 0; ; {
		j := strings.Index(s[i:], m.chaveB64)
		if j < 0 {
			break
		}
		add(i+j, i+j+len(m.chaveB64), "segredo")
		i += j + len(m.chaveB64)
	}

	if on("segredo") {
		// O gitleaks fica desproporcionalmente lento em textos grandes (acima de ~128 KB o
		// tempo cresce mais que o tamanho). Por isso o texto vai em janelas, em paralelo, com
		// sobra entre elas para não cortar ao meio um segredo de várias linhas (chave privada
		// PEM). Com um bloco PEM maior que a sobra, vai inteiro.
		type janela struct{ base, fim int }
		var js []janela
		if len(s) <= janelaLeaks || blocoLongo(s, sobraLeaks) {
			js = []janela{{0, len(s)}}
		} else {
			for base := 0; ; {
				fim := min(len(s), base+janelaLeaks)
				for fim < len(s) && fim > base && s[fim]&0xC0 == 0x80 {
					fim-- // não corta um caractere UTF-8 ao meio
				}
				js = append(js, janela{base, fim})
				if fim == len(s) {
					break
				}
				base = fim - sobraLeaks
				for base > 0 && s[base]&0xC0 == 0x80 {
					base--
				}
			}
		}
		type segredo struct {
			base, linha, col int
			valor            string
		}
		res := make([][]segredo, len(js))
		var prox atomic.Int64
		var wg sync.WaitGroup
		for w := 0; w < min(runtime.GOMAXPROCS(0), len(js)); w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					i := int(prox.Add(1)) - 1
					if i >= len(js) {
						return
					}
					for _, f := range m.leaks.DetectString(s[js[i].base:js[i].fim]) {
						res[i] = append(res[i], segredo{js[i].base, f.StartLine, f.StartColumn, f.Secret})
					}
				}
			}()
		}
		wg.Wait()
		for i, r := range res {
			parte := s[js[i].base:js[i].fim]
			jaVi := map[string]bool{}
			for _, f := range r {
				if len(f.valor) < 6 || jaVi[f.valor] {
					continue
				}
				jaVi[f.valor] = true
				achou := false
				for k := 0; ; {
					j := strings.Index(parte[k:], f.valor)
					if j < 0 {
						break
					}
					add(f.base+k+j, f.base+k+j+len(f.valor), "segredo")
					k += j + len(f.valor)
					achou = true
				}
				if !achou {
					// segredo achado DEPOIS de decodificar (ex.: base64 de um Secret do Kubernetes):
					// ele não aparece literal no texto; cobre o trecho codificado naquela posição,
					// inteiro (mesmo a parte que ficou fora desta janela)
					if ini, f2, ok := trechoCodificado(parte, f.linha, f.col); ok {
						a, b := f.base+ini, f.base+f2
						for a > 0 && ehB64(s[a-1]) {
							a--
						}
						for b < len(s) && ehB64(s[b]) {
							b++
						}
						add(a, b, "segredo")
					}
				}
			}
		}
	}
	if on("segredo") {
		m.acharSenhas(s, baixo, add)
	}
	if on("campo") {
		m.acharCampos(s, add)
		if strings.Contains(baixo, "insert into") {
			m.acharInsert(s, baixo, add)
		}
		if strings.Contains(s, "</") {
			m.acharXML(s, add)
		}
	}
	m.acharExtras(s, baixo, numLongo, add)
	if on("usuario") && m.pessoas != nil && m.pessoas.TemCodigos() {
		paraCadaCodigo(s, func(ini, fim int) {
			if m.pessoas.EhCodigo(m.p, s[ini:fim]) {
				add(ini, fim, "usuario")
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
			if m.emailLiberado(s[ix[0]:ix[1]]) {
				continue
			}
			add(ix[0], ix[1], "email")
		}
	}
	if on("ip") && ipLike {
		for _, ix := range reIPv4.FindAllStringIndex(s, -1) {
			if !bordaNum(s, ix[0], ix[1]) {
				continue
			}
			a, err := netip.ParseAddr(s[ix[0]:ix[1]])
			if err != nil || !a.IsPrivate() || fakeNet.Contains(a) {
				continue
			}
			add(ix[0], ix[1], "ip")
		}
	}
	if on("host") && len(m.cfg.DominiosInternos) > 0 && m.temInterno(baixo) {
		for _, ix := range reHost.FindAllStringIndex(s, -1) {
			h := strings.ToLower(s[ix[0]:ix[1]])
			if (ix[0] > 0 && s[ix[0]-1] == '@') || strings.HasSuffix(h, ".invalid") || !m.interno(h) {
				continue
			}
			add(ix[0], ix[1], "host")
		}
	}
	if on("cnpj") && numLongo {
		for _, ix := range reCNPJ.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			if bordaDig(s, ix[0], ix[1]) && CNPJValido(v) && (formatado(v) || contexto(s, ix[0], "cnpj")) {
				add(ix[0], ix[1], "cnpj")
			}
		}
	}
	if on("cpf") && numLongo {
		for _, ix := range reCPF.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			if bordaDig(s, ix[0], ix[1]) && CPFValido(v) && (formatado(v) || contexto(s, ix[0], "cpf")) {
				add(ix[0], ix[1], "cpf")
			}
		}
	}
	if on("pis") && numLongo {
		for _, ix := range re11.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			if bordaDig(s, ix[0], ix[1]) && PISValido(v) && (strings.Count(v, ".") == 2 || contextoAlgum(s, ix[0], "pis", "pasep", "nis", "nit")) {
				add(ix[0], ix[1], "pis")
			}
		}
	}
	if on("cnh") && numLongo && tem("cnh") {
		for _, ix := range re11.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			if bordaDig(s, ix[0], ix[1]) && CNHValida(v) && contexto(s, ix[0], "cnh") {
				add(ix[0], ix[1], "cnh")
			}
		}
	}
	if on("telefone") && numLongo {
		for _, ix := range reTel.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			if !bordaDig(s, ix[0], ix[1]) {
				continue
			}
			forte := strings.HasPrefix(v, "+55") || reDDD.MatchString(v) // "(11) 9...": parêntese aberto E fechado
			if forte || (strings.ContainsAny(v, "- ") && contextoAlgum(s, ix[0], "tel", "fone", "celular", "whatsapp", "contato", "cel")) {
				add(ix[0], ix[1], "telefone")
			}
		}
	}
	if on("cep") && tem("cep") {
		for _, ix := range reCEP.FindAllStringIndex(s, -1) {
			// a palavra "cep" inteira (não dentro de "exception", "conceito"), e o número não
			// pode ser uma data escrita junta (20261003)
			if bordaDig(s, ix[0], ix[1]) && contextoPalavra(s, ix[0], "cep") && !dataJunta(s[ix[0]:ix[1]]) {
				add(ix[0], ix[1], "cep")
			}
		}
	}
	if on("cartao") && numLongo {
		for _, ix := range reCartao.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			if !bordaDig(s, ix[0], ix[1]) || !LuhnValido(v) {
				continue
			}
			if reAgrupado.MatchString(v) || contextoAlgum(s, ix[0], "cart", "card", "credito", "crédito", "debito", "débito") {
				add(ix[0], ix[1], "cartao")
			}
		}
	}
	if on("rg") && tem("rg") {
		for _, ix := range reRG.FindAllStringIndex(s, -1) {
			if bordaDig(s, ix[0], ix[1]) && contextoPalavra(s, ix[0], "rg") {
				add(ix[0], ix[1], "rg")
			}
		}
	}
	if on("conta") && tem("ag", "conta", "c/c") {
		for _, re := range []*regexp.Regexp{reAg, reConta} {
			for _, ix := range re.FindAllStringSubmatchIndex(s, -1) {
				add(ix[2], ix[3], "conta")
			}
		}
	}
	if on("pix") && tem("pix") {
		for _, ix := range rePix.FindAllStringIndex(s, -1) {
			if contexto(s, ix[0], "pix") {
				add(ix[0], ix[1], "pix")
			}
		}
	}
	if on("endereco") && tem("rua", "r.", "av", "travessa", "tv.", "alameda", "al.", "rodovia", "rod.", "estrada", "praça", "praca", "largo") {
		for _, ix := range reEnd.FindAllStringIndex(s, -1) {
			add(ix[0], ix[1], "endereco")
		}
	}
	if on("nascimento") && tem("nasc") {
		for _, ix := range reNasc.FindAllStringSubmatchIndex(s, -1) {
			add(ix[2], ix[3], "nascimento")
		}
	}
	if on("nome") && m.pessoas != nil {
		for _, a := range m.pessoas.AcharNomes(s, m.p) {
			out = append(out, a)
		}
	}
	for i, re := range m.extras {
		for _, ix := range re.FindAllStringIndex(s, -1) {
			add(ix[0], ix[1], "x:"+m.rotExtra[i])
		}
	}
	if m.termos != nil {
		for _, ix := range m.termos.FindAllStringIndex(s, -1) {
			add(ix[0], ix[1], "t:"+m.rotTermo[strings.ToLower(s[ix[0]:ix[1]])])
		}
	}
	return out
}

func (m *Masker) temInterno(baixo string) bool { return m.interno(baixo) }

func ehB64(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=' || c == '-' || c == '_'
}

// trechoCodificado acha, na linha `linha` (0-based) perto da coluna `col` informada pelo
// gitleaks, o trecho base64/hex inteiro que contém essa posição. A coluna do gitleaks tem
// deslocamento de 1–2 caracteres conforme a linha; procurar o trecho inteiro em volta dela
// torna isso irrelevante.
func trechoCodificado(s string, linha, col int) (int, int, bool) {
	ini := 0
	for l := 0; l < linha; l++ {
		k := strings.IndexByte(s[ini:], '\n')
		if k < 0 {
			return 0, 0, false
		}
		ini += k + 1
	}
	fimLinha := len(s)
	if k := strings.IndexByte(s[ini:], '\n'); k >= 0 {
		fimLinha = ini + k
	}
	for _, p := range []int{ini + col - 1, ini + col - 2, ini + col} {
		if p < ini || p >= fimLinha || !ehB64(s[p]) {
			continue
		}
		a, b := p, p
		for a > ini && ehB64(s[a-1]) {
			a--
		}
		for b < fimLinha && ehB64(s[b]) {
			b++
		}
		if b-a >= 16 {
			return a, b, true
		}
	}
	return 0, 0, false
}

// perfilNumerico: há um número longo (>= 8 dígitos, aceitando . - / ( ) espaço e +)? E algo
// com cara de IPv4 (dígitos com 3 pontos)? Uma passada só, sem regex.
func perfilNumerico(s string) (numLongo, ipLike bool) {
	dig, pontos, digIP := 0, 0, 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			dig++
			digIP++
		case c == '.':
			if digIP > 0 {
				pontos++
			}
			digIP = 0
		case c == '-' || c == '/' || c == '(' || c == ')' || c == ' ' || c == '+':
			digIP, pontos = 0, 0
		default:
			dig, digIP, pontos = 0, 0, 0
		}
		if dig >= 8 {
			numLongo = true
		}
		if pontos >= 3 && digIP > 0 {
			ipLike = true
		}
		if numLongo && ipLike {
			return
		}
	}
	return
}

// bordaDig: o trecho não está colado a outros dígitos (é um número inteiro, não um pedaço).
func bordaDig(s string, i, j int) bool {
	return (i == 0 || !ehDig(s[i-1])) && (j >= len(s) || !ehDig(s[j]))
}

// Endereço físico de placa de rede (identifica um aparelho, e por ele uma pessoa)
var reMAC = regexp.MustCompile(`\b(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}\b`)

// dataJunta: 8 dígitos que formam uma data plausível (AAAAMMDD ou DDMMAAAA).
func dataJunta(v string) bool {
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
	ok := func(ano, mes, dia int) bool {
		return ano >= 1900 && ano <= 2100 && mes >= 1 && mes <= 12 && dia >= 1 && dia <= 31
	}
	return ok(n(0, 4), n(4, 6), n(6, 8)) || ok(n(4, 8), n(2, 4), n(0, 2))
}
