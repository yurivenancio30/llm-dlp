package mask

import (
	"strings"
)

// Company name and acronym (the registered terms) embedded in identifiers.

// normTerm: lowercase, no separators ("Acme_Corp" -> "acmecorp").
func normTerm(v string) string {
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

func identDot(c byte) bool { return isAlnum(c) || c == '_' || c == '-' || c == '.' }

// findTerms: identifier that CONTAINS a registered term as a piece ("acme_pedidos",
// "dbAcmeVendas"). The term alone is left to the terms detector.
func findTerms(s string, ts []string, add func(ObjFinding)) {
	for _, t := range ts {
		c0 := t[0]
		for i := 0; i+len(t) <= len(s); i++ {
			if s[i]|0x20 != c0 || !strings.EqualFold(s[i:i+len(t)], t) {
				continue
			}
			a, b := i, i+len(t)
			for a > 0 && identDot(s[a-1]) {
				a--
			}
			for b < len(s) && identDot(s[b]) {
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
			if len(id) <= len(t) || !identifierShape(id) || !hasPiece(id, t) || isPseudoObj(id) {
				continue
			}
			add(ObjFinding{a, b, entPosition(s, a), "termo-embutido", true})
		}
	}
}

// hasPiece: the term t (normalized) is a sequence of whole pieces of id.
func hasPiece(id, t string) bool {
	var ps [16][2]int
	n, ok := keyPieces(id, &ps)
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

// entPosition: the entity indicated by what comes before s[a] on the line (URL host, value of a
// known key, name after FROM/JOIN/DATABASE...); with no position, service.
func entPosition(s string, a int) string {
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
		for b > 0 && e-b < 64 && (isAlnum(s[b-1]) || s[b-1] == '_' || s[b-1] == '.' || s[b-1] == '-') {
			b--
		}
		if ent, _ := entKey(strings.TrimLeft(s[b:e], "-")); ent != "" {
			return ent
		}
		return "servico"
	}
	b := k
	for b > 0 && k-b < 10 && letterD(s[b-1]) {
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
