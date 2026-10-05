package mask

import (
	"strings"
)

// Nome e sigla da empresa (os termos cadastrados) embutidos em identificadores.

// normTermo: minúsculas, sem separadores ("Acme_Corp" -> "acmecorp").
func normTermo(v string) string {
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c == '_' || c == '-' || c == '.' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b.WriteByte(c)
	}
	return b.String()
}

func identPonto(c byte) bool { return ehAlnum(c) || c == '_' || c == '-' || c == '.' }

// acharTermos: identificador que CONTÉM um termo cadastrado como pedaço ("acme_pedidos",
// "dbAcmeVendas"). O termo sozinho fica com o detector de termos.
func acharTermos(s string, ts []string, add func(ObjAchado)) {
	for _, t := range ts {
		c0 := t[0]
		for i := 0; i+len(t) <= len(s); i++ {
			if s[i]|0x20 != c0 || !strings.EqualFold(s[i:i+len(t)], t) {
				continue
			}
			a, b := i, i+len(t)
			for a > 0 && identPonto(s[a-1]) {
				a--
			}
			for b < len(s) && identPonto(s[b]) {
				b++
			}
			for a < b && (s[a] == '.' || s[a] == '-') {
				a++
			}
			for b > a && (s[b-1] == '.' || s[b-1] == '-') {
				b--
			}
			i = max(i, b-1)
			id := s[a:b]
			if len(id) <= len(t) || !caraDeIdentificador(id) || !temPedaco(id, t) || ehPseudoObj(id) {
				continue
			}
			add(ObjAchado{a, b, entPosicao(s, a), "termo-embutido", true})
		}
	}
}

// temPedaco: o termo t (normalizado) é uma sequência de pedaços inteiros de id.
func temPedaco(id, t string) bool {
	var ps [16][2]int
	n, ok := pedacosChave(id, &ps)
	if !ok {
		n = len(ps)
	}
	for i := 0; i < n; i++ {
		k := 0
		for j := i; j < n && k < len(t); j++ {
			p := id[ps[j][0]:ps[j][1]]
			if k+len(p) > len(t) || !strings.EqualFold(p, t[k:k+len(p)]) {
				break
			}
			k += len(p)
		}
		if k == len(t) {
			return true
		}
	}
	return false
}

// entPosicao: a entidade indicada pelo que vem antes de s[a] na linha (host de URL, valor de
// chave conhecida, nome depois de FROM/JOIN/DATABASE...); sem posição, serviço.
func entPosicao(s string, a int) string {
	k := a
	for k > 0 && a-k < 8 && (s[k-1] == ' ' || s[k-1] == '\t' || s[k-1] == '"' || s[k-1] == '\'' || s[k-1] == '`') {
		k--
	}
	if k >= 3 && s[k-3:k] == "://" || k > 0 && s[k-1] == '@' && k == a {
		return "servidor"
	}
	if k > 0 && (s[k-1] == ':' || s[k-1] == '=') {
		e := k - 1
		for e > 0 && (s[e-1] == ' ' || s[e-1] == '"' || s[e-1] == '\'') {
			e--
		}
		b := e
		for b > 0 && e-b < 64 && (ehAlnum(s[b-1]) || s[b-1] == '_' || s[b-1] == '.' || s[b-1] == '-') {
			b--
		}
		if ent, _ := entChave(strings.TrimLeft(s[b:e], "-")); ent != "" {
			return ent
		}
		return "servico"
	}
	b := k
	for b > 0 && k-b < 10 && letraD(s[b-1]) {
		b--
	}
	switch strings.ToUpper(s[b:k]) {
	case "FROM", "JOIN", "INTO", "UPDATE", "TABLE", "VIEW":
		return "tabela"
	case "DATABASE", "USE":
		return "database"
	case "SCHEMA":
		return "schema"
	case "PROCEDURE", "PROC", "CALL", "EXEC", "EXECUTE", "FUNCTION":
		return "procedure"
	}
	return "servico"
}
