package mask

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// Troca dos achados por pseudônimos.

// Troca dos achados por pseudônimos.

// aplicar resolve sobreposições (fica o trecho mais longo) e troca por pseudônimos.
func (m *Masker) aplicar(s string, achados []Achado) (string, []Entrada) {
	if len(achados) == 0 {
		return s, nil
	}
	sort.Slice(achados, func(i, j int) bool {
		if achados[i].Ini != achados[j].Ini {
			return achados[i].Ini < achados[j].Ini
		}
		return achados[i].Fim-achados[i].Ini > achados[j].Fim-achados[j].Ini
	})
	var b strings.Builder
	var entradas []Entrada
	pos := 0
	for _, a := range achados {
		if a.Ini < pos {
			continue
		}
		ps := m.Pseudonimo(a.Tipo, a.Real)
		b.WriteString(s[pos:a.Ini])
		b.WriteString(ps)
		entradas = append(entradas, Entrada{ps, a.Real, a.Tipo})
		pos = a.Fim
	}
	b.WriteString(s[pos:])
	return b.String(), entradas
}

// Pseudonimo devolve o pseudônimo estável de um valor real de um tipo.
func (m *Masker) Pseudonimo(tipo, real string) string {
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
		if m.pessoas != nil {
			if pid := m.pessoas.PID(m.p, "codigo", NormNome(real)); pid != "" {
				return "USUARIO-" + pid
			}
		}
		return "USUARIO-" + m.p.ID("usuario", NormNome(real))
	case "cpf", "cnpj", "pis", "cnh", "telefone", "cep", "cartao", "rg", "conta":
		if tipo == "cnpj" && CNPJAlfaValido(real) { // o formato novo tem letras: não vale só os dígitos
			return "CNPJ-" + m.p.ID(tipo, NormNome(real))
		}
		return strings.ToUpper(tipo) + "-" + m.p.ID(tipo, canonNum(real))
	default:
		rot := strings.TrimPrefix(strings.TrimPrefix(tipo, "x:"), "t:")
		return strings.ToUpper(rot) + "-" + m.p.ID(tipo, NormNome(real))
	}
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
