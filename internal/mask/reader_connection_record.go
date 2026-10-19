package mask

import (
	"regexp"
	"strconv"
	"strings"
)

// Connection record written by a person (an inventory, a ticket, a wiki page): a block of
// "Label: value" lines, between blank lines, with a server label and a port label whose value is a
// port number. It is a person's connection string ("Host: srv01\nPorta: 1433\nDatabase: dw"), so
// the value of a connection label is the resource even when it is a single plain word, as in
// "host=srv01 port=1433 dbname=dw". Several values on the line (" / ", ", ") and the lines indented
// under an empty label ("DataBase:\n    dw\n    stage") are items of the label's type. Without the
// port, "Server: nginx" and "Host: localhost" stay prose labels or headers (reader_key_value.go).

// Portuguese labels of a connection, besides the keys of connection strings (connectionKeys)
var recordLabels = map[string]string{"servidor": "server", "instancia": "server", "banco": "database",
	"bancodedados": "database", "basededados": "database", "esquema": "schema", "usuario": "user", "login": "user"}

var (
	reRecordLine = regexp.MustCompile(`^[ \t]*(?:[-*•][ \t]+)?(?:\*\*)?([A-Za-zÀ-ÿ][A-Za-zÀ-ÿ0-9 _.-]{0,30}?)(?:\*\*)?[ \t]*:(?:\*\*)?(?:[ \t]+(.*?))?[ \t\r]*$`)
	reRecordPort = regexp.MustCompile(`(?im)^[ \t]*(?:[-*•][ \t]+)?(?:\*\*)?port[a]?(?:\*\*)?[ \t]*:(?:\*\*)?[ \t]*\d{1,5}[ \t\r]*$`)
	reRecordSep  = regexp.MustCompile(`[ \t]+/[ \t]+|,[ \t]+|;[ \t]*`)
	accentsPT    = strings.NewReplacer("á", "a", "à", "a", "ã", "a", "â", "a", "é", "e", "ê", "e", "í", "i", "ó", "o",
		"ô", "o", "õ", "o", "ú", "u", "ç", "c")
)

type lineSpan struct{ a, b int }

func findConnectionRecords(s string, add func(ObjFinding)) {
	if !strings.Contains(s, "\n") || !reRecordPort.MatchString(s) {
		return
	}
	var block []lineSpan
	for a := 0; a <= len(s); {
		b := strings.IndexByte(s[a:], '\n')
		if b < 0 {
			b = len(s)
		} else {
			b += a
		}
		if strings.TrimSpace(s[a:b]) == "" || len(block) == 200 {
			connectionRecord(s, block, add)
			block = block[:0]
		}
		if strings.TrimSpace(s[a:b]) != "" {
			block = append(block, lineSpan{a, b})
		}
		a = b + 1
	}
	connectionRecord(s, block, add)
}

// recordLabel: the label in lowercase, without spaces, "_", "-", "." and accents ("Banco de dados"
// -> bancodedados, "Usuário" -> usuario).
func recordLabel(l string) string {
	l = accentsPT.Replace(strings.ToLower(l))
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '_' || r == '-' || r == '.' {
			return -1
		}
		return r
	}, l)
}

func connectionRecord(s string, lines []lineSpan, add func(ObjFinding)) {
	if len(lines) < 3 {
		return
	}
	type label struct {
		line   int
		ent    string
		va, vb int
	}
	var labels []label
	n, port, server := 0, false, false
	for i, l := range lines {
		m := reRecordLine.FindStringSubmatchIndex(s[l.a:l.b])
		if m == nil {
			continue
		}
		n++
		va, vb := l.b, l.b
		if m[4] >= 0 {
			va, vb = l.a+m[4], l.a+m[5]
		}
		k := recordLabel(s[l.a+m[2] : l.a+m[3]])
		if k == "port" || k == "porta" {
			if p, err := strconv.Atoi(s[va:vb]); err == nil && p > 0 && p < 65536 {
				port = true
			}
			continue
		}
		ent, ok := connectionKeys[k]
		if !ok {
			ent = recordLabels[k]
		}
		if ent == "" {
			continue
		}
		server = server || ent == "server"
		labels = append(labels, label{i, ent, va, vb})
	}
	if n < 3 || !port || !server {
		return
	}
	for _, x := range labels {
		if x.va < x.vb {
			recordValues(s, x.va, x.vb, x.ent, add)
			continue
		}
		// empty label: the lines indented under it are its items
		in := indentOf(s, lines[x.line])
		for _, l := range lines[x.line+1:] {
			li := indentOf(s, l)
			if li <= in {
				break
			}
			a := l.a + li
			if a+1 < l.b && strings.IndexByte("-*•", s[a]) >= 0 && s[a+1] == ' ' {
				a += 2
			}
			recordValues(s, a, l.b, x.ent, add)
		}
	}
}

func indentOf(s string, l lineSpan) int {
	i := l.a
	for i < l.b && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i - l.a
}

// recordValues: each item of s[a:b] (separated by " / ", ", " or ";") that is one name; an item
// with a space is prose ("veja abaixo") and stays.
func recordValues(s string, a, b int, ent string, add func(ObjFinding)) {
	cuts := append(reRecordSep.FindAllStringIndex(s[a:b], -1), []int{b - a, b - a})
	x := a
	for _, c := range cuts {
		ia, ib := x, a+c[0]
		x = a + c[1]
		for ia < ib && (s[ia] == '`' || s[ia] == '"' || s[ia] == '\'' || s[ia] == ' ') {
			ia++
		}
		for ib > ia && (s[ib-1] == '`' || s[ib-1] == '"' || s[ib-1] == '\'' || s[ib-1] == ' ' || s[ib-1] == '\r' || s[ib-1] == '.') {
			ib--
		}
		v := s[ia:ib]
		if v == "" || strings.ContainsAny(v, " \t") || !resourceValue(v, ent) || publicConnection(v) {
			continue
		}
		if ent == "server" {
			addServer(s, ia, ib, "connection-record", add)
		} else {
			addParts(s, ia, ib, ent, "connection-record", add)
		}
	}
}
