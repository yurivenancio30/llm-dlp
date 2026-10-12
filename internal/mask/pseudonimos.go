package mask

import (
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Troca dos achados por pseudônimos.

// Troca dos achados por pseudônimos.

// aplicar resolve sobreposições (fica o trecho mais longo) e troca por pseudônimos.
func (m *Masker) aplicar(s string, achados []Achado) (string, []Entrada) {
	t, e, _ := m.aplicarT(s, achados)
	return t, e
}

// aplicarT é o aplicar que devolve também os trechos trocados (ver memoria_enviados.go).
func (m *Masker) aplicarT(s string, achados []Achado) (string, []Entrada, []trecho) {
	if len(achados) == 0 {
		return s, nil, nil
	}
	sort.Slice(achados, func(i, j int) bool {
		a, b := achados[i], achados[j]
		if a.Ini != b.Ini {
			return a.Ini < b.Ini
		}
		if a.Fim-a.Ini != b.Fim-b.Ini {
			return a.Fim-a.Ini > b.Fim-b.Ini
		}
		// mesmo trecho com dois tipos: sempre a mesma escolha (o pseudônimo não pode variar).
		// Fica o detector de dado pessoal; entre objetos, a ordem do tipo.
		if ehObjeto(a.Tipo) != ehObjeto(b.Tipo) {
			return !ehObjeto(a.Tipo)
		}
		return a.Tipo < b.Tipo
	})
	var b strings.Builder
	var entradas []Entrada
	var ts []trecho
	pos := 0
	for _, a := range achados {
		if a.Ini < pos {
			continue
		}
		ps := m.Pseudonimo(a.Tipo, a.Real)
		b.WriteString(s[pos:a.Ini])
		b.WriteString(ps)
		entradas = entradaDe(entradas, ps, a.Real, a.Tipo)
		ts = append(ts, trecho{a.Ini, a.Fim, a.Tipo, ps})
		pos = a.Fim
	}
	b.WriteString(s[pos:])
	return b.String(), entradas, ts
}

// entradaDe acrescenta a entrada de um trecho trocado. As outras caixas do pseudônimo, que o
// modelo pode escrever ("T_abc", "t_abc", "T_ABC"), não entram aqui: NovaTabela as acrescenta
// depois de todas as grafias que apareceram (ver variantesCaixa).
func entradaDe(entradas []Entrada, ps, real, tipo string) []Entrada {
	entradas = append(entradas, Entrada{ps, real, tipo})
	if tipo == "email" { // o modelo às vezes cita só o domínio: "dxxxx.invalid" volta a ser o domínio real
		if i, j := strings.LastIndexByte(ps, '@'), strings.LastIndexByte(real, '@'); i >= 0 && j >= 0 {
			entradas = append(entradas, Entrada{ps[i+1:], real[j+1:], "dominio"})
		}
	}
	return entradas
}

// remontar refaz o texto enviado antes a partir dos trechos guardados: sai igual, byte a
// byte, mesmo que hoje se saiba mais (ver memoria_enviados.go). Trecho inválido: não remonta.
func remontar(s string, ts []trecho) (string, []Entrada, bool) {
	var b strings.Builder
	var entradas []Entrada
	pos := 0
	for _, t := range ts {
		if t.Ini < pos || t.Fim <= t.Ini || t.Fim > len(s) || t.Pseudo == "" || t.Tipo == "" {
			return "", nil, false
		}
		b.WriteString(s[pos:t.Ini])
		b.WriteString(t.Pseudo)
		entradas = entradaDe(entradas, t.Pseudo, s[t.Ini:t.Fim], t.Tipo)
		pos = t.Fim
	}
	b.WriteString(s[pos:])
	return b.String(), entradas, true
}

// Pseudonimo devolve o pseudônimo estável de um valor real de um tipo.
func (m *Masker) Pseudonimo(tipo, real string) string {
	if ehObjeto(tipo) {
		return m.pseudoObjeto(strings.TrimPrefix(tipo, prefTipoObj), real)
	}
	switch tipo {
	case "email":
		e := strings.ToLower(real)
		at := strings.LastIndex(e, "@")
		pid := ""
		if m.pessoas != nil {
			pid = m.pessoas.PID(m.p, "email", e)
		}
		if pid == "" {
			pid = m.p.ID("email", e)
		}
		// formato curto de propósito: medido, "pessoa.X@dom-YYYYYYYY.invalid" custava ~16% mais
		// tokens que o e-mail real; este custa ~1,5% (o domínio só precisa distinguir domínios)
		return "p." + pid + "@d" + m.p.ID("dominio", e[at+1:])[:4] + ".invalid"
	case "nome":
		pid := ""
		if m.pessoas != nil {
			pid = m.pessoas.PID(m.p, "nome", NormNome(real))
		}
		if pid == "" {
			pid = m.p.ID("nome", NormNome(real))
		}
		// a mesma pessoa, com a caixa da grafia ("MARIA SILVA", "maria silva"): a volta devolve
		// cada grafia como estava (ver variantesNome)
		switch caixaNome(real) {
		case 'A':
			return "PESSOA " + strings.ToUpper(pid)
		case 'a':
			return "pessoa " + pid
		}
		return "Pessoa " + pid
	case "ip":
		a := netip.MustParseAddr(real).As4()
		pref := fmt.Sprintf("%d.%d.%d", a[0], a[1], a[2])
		h := m.p.Bits("rede24", pref, 3)
		return fmt.Sprintf("%d.%d.%d.%d", 240|(h[0]&0x0f), h[1], h[2], a[3])
	case "host":
		h := strings.ToLower(real)
		papel := strings.SplitN(h, ".", 2)[0]
		for _, p := range m.cfg.PapeisHost {
			if p == papel {
				return papel + "-" + m.p.ID("host", h)[:4] + ".invalid"
			}
		}
		return "h" + m.p.ID("host", h) + ".invalid"
	case "segredo":
		return "SEGREDO-" + m.p.ID("segredo", real)
	case "usuario":
		// código de uma pessoa importada: mesmo id do nome e do e-mail dela
		pid := ""
		if m.pessoas != nil {
			pid = m.pessoas.PID(m.p, "codigo", NormNome(real))
		}
		if pid == "" {
			pid = m.p.ID("usuario", NormNome(real))
		}
		return pseudoRotulo("USUARIO", pid, real)
	}
	if tiposNumericos[tipo] {
		if tipo == "cnpj" && CNPJAlfaValido(real) { // o formato novo tem letras: não vale só os dígitos
			return "CNPJ-" + m.p.ID(tipo, NormNome(real))
		}
		return strings.ToUpper(tipo) + "-" + m.p.ID(tipo, canonNum(real))
	}
	rot := strings.TrimPrefix(strings.TrimPrefix(tipo, "x:"), "t:")
	return pseudoRotulo(strings.ToUpper(rot), m.p.ID(tipo, NormNome(real)), real)
}

// tiposNumericos: o pseudônimo vem só dos dígitos (canonNum).
var tiposNumericos = conj("cpf", "cnpj", "pis", "cnh", "telefone", "cep", "cartao", "rg", "conta")

// pseudoComCaixa: o pseudônimo do tipo segue a caixa da grafia real, e a volta acrescenta as
// outras caixas dele (variantesCaixa): os objetos (pseudoObjeto) e os tipos com rótulo cuja chave
// não tem caixa (pseudoRotulo: usuário, termo cadastrado, padrão extra, endereço...). Pessoa tem
// as próprias formas (variantesNome); e-mail, host, IP, segredo e números, não.
func pseudoComCaixa(tipo string) bool {
	if ehObjeto(tipo) {
		return true
	}
	switch tipo {
	case "email", "nome", "ip", "host", "segredo", "dominio":
		return false
	}
	return !tiposNumericos[tipo]
}

// sepPseudo: o separador entre o prefixo e o id do pseudônimo ("T_abc", "EMPRESA-abc").
func sepPseudo(tipo string) byte {
	if ehObjeto(tipo) {
		return '_'
	}
	return '-'
}

// pseudoRotulo: rótulo + id, na caixa da grafia, como o nome de pessoa: o mesmo valor escrito
// "SGP", "sgp" e "SgP" (a chave do id não tem caixa) sai "EMPRESA-x", "empresa-x" e "Empresa-x",
// e a volta devolve cada grafia como estava. Sem letras, a forma de sempre (rótulo em maiúsculas).
// O id fica em minúsculas em todas: as três formas custam os mesmos tokens.
func pseudoRotulo(rot, id, real string) string {
	ps := formaRotulo(rot, id, real)
	if ps[:len(rot)] != rot { // len(ps) > len(rot): ps = rot na caixa da grafia + "-" + id
		registrarPseudo(ps) // as formas novas não têm a cara que rePseudoPronto conhece (ver ehPseudoRotulo)
	}
	return ps
}

// formaRotulo: o pseudônimo de pseudoRotulo, sem registrar.
func formaRotulo(rot, id, real string) string {
	switch caixaDe(real) {
	case 'a':
		rot = strings.ToLower(rot)
	case 'M':
		rot = capitalizar(rot)
	}
	return rot + "-" + id
}

// rotuloPrincipal: ps (rot + "-" + id) é a forma que formaRotulo dá para a grafia real? Sem
// montar a string (NovaTabela pergunta isso de cada entrada).
func rotuloPrincipal(rot, id, real string) bool {
	if id != strings.ToLower(id) {
		return false
	}
	switch caixaDe(real) {
	case 'a':
		return rot == strings.ToLower(rot)
	case 'M':
		r, n := utf8.DecodeRuneInString(rot)
		return unicode.ToUpper(r) == r && rot[n:] == strings.ToLower(rot[n:])
	}
	return rot == strings.ToUpper(rot)
}

// caixaDe: 'A' (só maiúsculas), 'a' (só minúsculas), 'M' (as duas) ou 0 (nenhuma letra com
// caixa), sem alocar.
func caixaDe(s string) byte {
	maius, minus := false, false
	for _, r := range s {
		if r < utf8.RuneSelf {
			maius = maius || 'A' <= r && r <= 'Z'
			minus = minus || 'a' <= r && r <= 'z'
			continue
		}
		maius = maius || unicode.ToLower(r) != r
		minus = minus || unicode.ToUpper(r) != r
	}
	switch {
	case maius && minus:
		return 'M'
	case maius:
		return 'A'
	case minus:
		return 'a'
	}
	return 0
}

// capitalizar: a primeira letra em maiúscula, o resto em minúsculas.
func capitalizar(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + strings.ToLower(s[n:])
}

// rePseudoRotulo: a FORMA de um pseudônimo de rótulo em qualquer caixa. Só a forma não basta
// ("maria-fernanda" tem essa cara): ver ehPseudoRotulo.
var rePseudoRotulo = regexp.MustCompile(`^[A-Za-z:]+-[a-zA-Z2-7]{8}$`)

// ehPseudoRotulo: v é um pseudônimo de rótulo em minúsculas ou com só a primeira maiúscula gerado
// por nós (a forma em maiúsculas, a de sempre, rePseudoPronto já reconhece)?
func ehPseudoRotulo(v string) bool {
	if !rePseudoRotulo.MatchString(v) {
		return false
	}
	_, ok := pseudosGerados.Load(strings.ToLower(v))
	return ok
}

// variantesCaixa: as outras caixas do pseudônimo de um objeto ou de um tipo com rótulo, que o
// modelo pode escrever ("T_abc", "t_abc", "T_ABC", "Empresa-x"). A variante volta ao valor real
// como ele entrou: o modelo que troca a caixa de um pseudônimo não inventa uma grafia nova
// ("/dados/SGP" e "/dados/sgp" são duas pastas). Cada forma que uma grafia gera já está em m
// quando a grafia apareceu (e então a variante não entra); a toda em maiúsculas, que nenhuma
// grafia gera, volta à grafia em maiúsculas quando ela apareceu. m: o de:para já montado.
func variantesCaixa(ps, real, tipo string, m map[string]string) []Entrada {
	i := strings.LastIndexByte(ps, sepPseudo(tipo))
	if i <= 0 {
		return nil
	}
	pref := ps[:i]
	alts := [4]string{strings.ToUpper(pref) + ps[i:], strings.ToLower(ps), strings.ToUpper(ps)}
	if !ehObjeto(tipo) {
		alts[3] = capitalizar(pref) + ps[i:] // "Empresa-x": a forma da grafia com caixa mista
	}
	var out []Entrada
	for k, alt := range alts {
		if alt == "" || alt == ps {
			continue
		}
		r := real
		if k == 2 { // "T_ABC": a grafia do "T_abc" (em maiúsculas), se apareceu
			if g, ok := m[strings.ToUpper(pref)+strings.ToLower(ps[i:])]; ok && mesmoValor(tipo, g, real) {
				r = g
			}
		}
		out = append(out, Entrada{alt, r, tipo})
	}
	return out
}

// caixaNome: 'A' para nome todo em maiúsculas, 'a' para todo em minúsculas, 'M' para o resto.
func caixaNome(s string) byte {
	switch {
	case strings.ToUpper(s) == s && strings.ToLower(s) != s:
		return 'A'
	case strings.ToLower(s) == s && strings.ToUpper(s) != s:
		return 'a'
	}
	return 'M'
}

// conectivosNome: as palavras que ficam minúsculas num nome com as iniciais maiúsculas.
var conectivosNome = conj("da", "das", "de", "do", "dos", "e", "del", "di", "du", "van", "von")

// variantesNome: as outras caixas do pseudônimo de uma pessoa (o modelo pode reescrever a
// caixa, como faz com os nomes de objeto), com o nome real na mesma caixa.
func variantesNome(ps, real string) []Entrada {
	i := strings.IndexByte(ps, ' ')
	if i < 0 {
		return nil
	}
	pid := strings.ToLower(ps[i+1:])
	iniciais := strings.Fields(strings.ToLower(real))
	for k, w := range iniciais {
		if k == 0 || !conectivosNome[w] {
			r := []rune(w)
			r[0] = []rune(strings.ToUpper(string(r[0])))[0]
			iniciais[k] = string(r)
		}
	}
	var out []Entrada
	for _, v := range []Entrada{
		{"PESSOA " + strings.ToUpper(pid), strings.ToUpper(real), "nome"},
		{"pessoa " + pid, strings.ToLower(real), "nome"},
		{"Pessoa " + pid, strings.Join(iniciais, " "), "nome"},
	} {
		if v.Pseudo != ps {
			out = append(out, v)
		}
	}
	return out
}

func (m *Masker) emailLiberado(e string) bool {
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
	for _, d := range m.cfg.DominiosEmailLiberados {
		d = strings.ToLower(d)
		if dom == d || strings.HasSuffix(dom, "."+d) {
			return true
		}
	}
	return false
}
