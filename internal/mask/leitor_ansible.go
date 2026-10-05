package mask

import (
	"regexp"
	"strings"
)

// Ansible: inventário INI (o YAML fica com o leitor de configuração estruturada).

var (
	reSecaoINI = regexp.MustCompile(`^\[([A-Za-z0-9_.\-]+)(:(?:vars|children))?\][ \t]*$`)
	reFaixaINI = regexp.MustCompile(`^[A-Za-z0-9_.\-]*\[[0-9a-z]+:[0-9a-z]+\][A-Za-z0-9_.\-]*$`)
)

func acharAnsibleINI(s string, add func(ObjAchado)) {
	if strings.IndexByte(s, '[') < 0 {
		return
	}
	forte := strings.Contains(s, "ansible_") || strings.Contains(s, ":children]") || strings.Contains(s, ":vars]")
	if !forte && !strings.Contains(s, "]\n") {
		return
	}
	secao := "" // "" = fora de seção; "h" = hosts; "v" = vars/children
	for _, ln := range quebraLinhas(s) {
		a, b := ln[0], ln[1]
		if a >= b {
			continue
		}
		l := s[a:b]
		if l[0] == '[' {
			m := reSecaoINI.FindStringSubmatch(l)
			switch {
			case m == nil:
				secao = ""
			case m[2] != "":
				secao = "v"
			default:
				secao = "h"
			}
			continue
		}
		if l[0] == '#' || l[0] == ';' || l[0] == ' ' || l[0] == '\t' {
			if l[0] == ' ' || l[0] == '\t' {
				secao = "" // linha indentada: não é inventário
			}
			continue
		}
		if secao == "" {
			continue
		}
		campos := strings.Fields(l)
		if len(campos) == 0 {
			continue
		}
		pos := a
		ok := true
		for k, c := range campos {
			if k == 0 && secao == "h" && strings.IndexByte(c, '=') >= 0 || k > 0 && strings.IndexByte(c, '=') <= 0 {
				ok = false
				break
			}
		}
		if secao == "v" && !strings.Contains(campos[0], "=") {
			continue
		}
		if !ok {
			continue
		}
		for k, c := range campos {
			x := pos + strings.Index(s[pos:b], c)
			pos = x + len(c)
			if k == 0 && secao == "h" {
				y := x + len(c)
				if i := strings.LastIndexByte(c, ':'); i > 0 && todoDigitos(c[i+1:]) {
					y = x + i
				}
				h := s[x:y]
				if forte || strings.ContainsAny(h, ".0123456789") {
					addNomeHostINI(s, x, y, forte, add)
				}
				continue
			}
			eq := strings.IndexByte(c, '=')
			chave, va := c[:eq], x+eq+1
			vb := x + len(c)
			if vb-va >= 2 && (s[va] == '"' || s[va] == '\'') && s[vb-1] == s[va] {
				va, vb = va+1, vb-1
			}
			switch chave {
			case "ansible_host":
				addNome(s, va, vb, "servidor", "ansible", true, add)
			case "ansible_user":
				addNome(s, va, vb, "usuario", "ansible", true, add)
			}
		}
	}
}

// addNomeHostINI: host do inventário; a faixa www[01:50].x é mascarada como unidade.
func addNomeHostINI(s string, a, b int, forte bool, add func(ObjAchado)) {
	v := s[a:b]
	if strings.ContainsAny(v, "[]") {
		if reFaixaINI.MatchString(v) {
			add(ObjAchado{a, b, "servidor", "ansible", forte})
		}
		return
	}
	addNome(s, a, b, "servidor", "ansible", forte, add)
}

// ---------------------------------------------------------------------------------------
// Bicep
