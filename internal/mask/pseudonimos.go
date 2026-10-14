package mask

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Swap dos achados por pseudônimos.

// Swap dos achados por pseudônimos.

// aplicar resolve sobreposições (fica o trecho mais longo) e troca por pseudônimos.
func (m *Masker) apply(s string, findings []Finding) (string, []Entry) {
	t, e, _ := m.applyT(s, findings)
	return t, e
}

// applyT é o aplicar que devolve também os trechos trocados (ver memoria_enviados.go).
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
		// mesmo trecho com dois tipos: sempre a mesma escolha (o pseudônimo não pode variar).
		// Fica o detector de dado pessoal; entre objetos, a ordem do tipo.
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

// entryOf acrescenta a entrada de um trecho trocado. As outras caixas do pseudônimo, que o
// modelo pode escrever ("T_abc", "t_abc", "T_ABC"), não entram aqui: NewTable as acrescenta
// depois de todas as grafias que apareceram (ver caseVariants).
func entryOf(entries []Entry, ps, real, typ string) []Entry {
	entries = append(entries, Entry{ps, real, typ})
	if typ == "email" { // o modelo às vezes cita só o domínio: "dxxxx.invalid" volta a ser o domínio real
		if i, j := strings.LastIndexByte(ps, '@'), strings.LastIndexByte(real, '@'); i >= 0 && j >= 0 {
			entries = append(entries, Entry{ps[i+1:], real[j+1:], "dominio"})
		}
	}
	return entries
}

// remontar refaz o texto enviado antes a partir dos trechos guardados: sai igual, byte a
// byte, mesmo que hoje se saiba mais (ver memoria_enviados.go). Trecho inválido: não remonta.
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

// Pseudonym devolve o pseudônimo estável de um valor real de um tipo. Todo pseudônimo fica
// registrado: um que sobre na volta, sem desmascarar, é com certeza nosso (ver NotUndone).
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
		// formato curto de propósito: medido, "pessoa.X@dom-YYYYYYYY.invalid" custava ~16% mais
		// tokens que o e-mail real; este custa ~1,5% (o domínio só precisa distinguir domínios)
		return "p." + pid + "@d" + m.p.ID("dominio", e[at+1:])[:4] + ".invalid"
	case "nome":
		pid := ""
		if m.people != nil {
			pid = m.people.PID(m.p, "nome", NormName(real))
		}
		if pid == "" {
			pid = m.p.ID("nome", NormName(real))
		}
		// a mesma pessoa, com a caixa da grafia ("MARIA SILVA", "maria silva"): a volta devolve
		// cada grafia como estava (ver nameVariants)
		switch nameCase(real) {
		case 'A':
			return "PESSOA " + strings.ToUpper(pid)
		case 'a':
			return "pessoa " + pid
		}
		return "Pessoa " + pid
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
	case "segredo":
		return "SEGREDO-" + m.p.ID("segredo", real)
	case "usuario":
		// código de uma pessoa importada: mesmo id do nome e do e-mail dela
		pid := ""
		if m.people != nil {
			pid = m.people.PID(m.p, "codigo", NormName(real))
		}
		if pid == "" {
			pid = m.p.ID("usuario", NormName(real))
		}
		return pseudoLabel("USUARIO", pid, real)
	}
	if numericTypes[typ] {
		if typ == "cnpj" && ValidAlphaCNPJ(real) { // o formato novo tem letras: não vale só os dígitos
			return "CNPJ-" + m.p.ID(typ, NormName(real))
		}
		return strings.ToUpper(typ) + "-" + m.p.ID(typ, canonNum(real))
	}
	lbl := strings.TrimPrefix(strings.TrimPrefix(typ, "x:"), "t:")
	return pseudoLabel(strings.ToUpper(lbl), m.p.ID(typ, NormName(real)), real)
}

// numericTypes: o pseudônimo vem só dos dígitos (canonNum).
var numericTypes = setOf("cpf", "cnpj", "pis", "cnh", "telefone", "cep", "cartao", "rg", "conta")

// pseudoWithCase: o pseudônimo do tipo segue a caixa da grafia real, e a volta acrescenta as
// outras caixas dele (caseVariants): os objetos (pseudoObject) e os tipos com rótulo cuja chave
// não tem caixa (pseudoLabel: usuário, termo cadastrado, padrão extra, endereço...). Pessoa tem
// as próprias formas (nameVariants); e-mail, host, IP, segredo e números, não.
func pseudoWithCase(typ string) bool {
	if isObject(typ) {
		return true
	}
	switch typ {
	case "email", "nome", "ip", "host", "segredo", "dominio":
		return false
	}
	return !numericTypes[typ]
}

// pseudoSep: o separador entre o prefixo e o id do pseudônimo ("T_abc", "EMPRESA-abc").
func pseudoSep(typ string) byte {
	if isObject(typ) {
		return '_'
	}
	return '-'
}

// pseudoLabel: rótulo + id, na caixa da grafia, como o nome de pessoa: o mesmo valor escrito
// "SGP", "sgp" e "SgP" (a chave do id não tem caixa) sai "EMPRESA-x", "empresa-x" e "Empresa-x",
// e a volta devolve cada grafia como estava. Sem letras, a forma de sempre (rótulo em maiúsculas).
// O id fica em minúsculas em todas: as três formas custam os mesmos tokens.
func pseudoLabel(lbl, id, real string) string {
	ps := labelShape(lbl, id, real)
	if ps[:len(lbl)] != lbl { // len(ps) > len(rot): ps = rot na caixa da grafia + "-" + id
		registerPseudo(ps) // as formas novas não têm a cara que reReadyPseudo conhece (ver isPseudoLabel)
	}
	return ps
}

// labelShape: o pseudônimo de pseudoLabel, sem registrar.
func labelShape(lbl, id, real string) string {
	switch caseOf(real) {
	case 'a':
		lbl = strings.ToLower(lbl)
	case 'M':
		lbl = capitalize(lbl)
	}
	return lbl + "-" + id
}

// primaryLabel: ps (rot + "-" + id) é a forma que labelShape dá para a grafia real? Sem
// montar a string (NewTable pergunta isso de cada entrada).
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

// caseOf: 'A' (só maiúsculas), 'a' (só minúsculas), 'M' (as duas) ou 0 (nenhuma letra com
// caixa), sem alocar.
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

// capitalizar: a primeira letra em maiúscula, o resto em minúsculas.
func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + strings.ToLower(s[n:])
}

// rePseudoLabel: a FORMA de um pseudônimo de rótulo em qualquer caixa. Só a forma não basta
// ("maria-fernanda" tem essa cara): ver isPseudoLabel.
var rePseudoLabel = regexp.MustCompile(`^[A-Za-z:]+-[a-zA-Z2-7]{8}$`)

// isPseudoLabel: v é um pseudônimo de rótulo em minúsculas ou com só a primeira maiúscula gerado
// por nós (a forma em maiúsculas, a de sempre, reReadyPseudo já reconhece)?
func isPseudoLabel(v string) bool {
	if !rePseudoLabel.MatchString(v) {
		return false
	}
	_, ok := generatedPseudos.Load(strings.ToLower(v))
	return ok
}

// caseVariants: as outras caixas do pseudônimo de um objeto ou de um tipo com rótulo, que o
// modelo pode escrever ("T_abc", "t_abc", "T_ABC", "Empresa-x"). A variante volta ao valor real
// como ele entrou: o modelo que troca a caixa de um pseudônimo não inventa uma grafia nova
// ("/dados/SGP" e "/dados/sgp" são duas pastas). Cada forma que uma grafia gera já está em m
// quando a grafia apareceu (e então a variante não entra); a toda em maiúsculas, que nenhuma
// grafia gera, volta à grafia em maiúsculas quando ela apareceu. m: o de:para já montado.
func caseVariants(ps, real, typ string, m map[string]string) []Entry {
	i := strings.LastIndexByte(ps, pseudoSep(typ))
	if i <= 0 {
		return nil
	}
	pfx := ps[:i]
	alts := [4]string{strings.ToUpper(pfx) + ps[i:], strings.ToLower(ps), strings.ToUpper(ps)}
	if !isObject(typ) {
		alts[3] = capitalize(pfx) + ps[i:] // "Empresa-x": a forma da grafia com caixa mista
	}
	var out []Entry
	for k, alt := range alts {
		if alt == "" || alt == ps {
			continue
		}
		r := real
		if k == 2 { // "T_ABC": a grafia do "T_abc" (em maiúsculas), se apareceu
			if g, ok := m[strings.ToUpper(pfx)+strings.ToLower(ps[i:])]; ok && sameValue(typ, g, real) {
				r = g
			}
		}
		out = append(out, Entry{alt, r, typ})
	}
	return out
}

// nameCase: 'A' para nome todo em maiúsculas, 'a' para todo em minúsculas, 'M' para o resto.
func nameCase(s string) byte {
	switch {
	case strings.ToUpper(s) == s && strings.ToLower(s) != s:
		return 'A'
	case strings.ToLower(s) == s && strings.ToUpper(s) != s:
		return 'a'
	}
	return 'M'
}

// nameConnectives: as palavras que ficam minúsculas num nome com as iniciais maiúsculas.
var nameConnectives = setOf("da", "das", "de", "do", "dos", "e", "del", "di", "du", "van", "von")

// nameVariants: as outras caixas do pseudônimo de uma pessoa (o modelo pode reescrever a
// caixa, como faz com os nomes de objeto), com o nome real na mesma caixa.
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
		{"PESSOA " + strings.ToUpper(pid), strings.ToUpper(real), "nome"},
		{"pessoa " + pid, strings.ToLower(real), "nome"},
		{"Pessoa " + pid, strings.Join(initials, " "), "nome"},
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
