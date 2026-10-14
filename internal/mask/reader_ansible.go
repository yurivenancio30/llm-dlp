package mask

import (
	"regexp"
	"strings"
)

// Ansible: inventário INI (o YAML fica com o leitor de configuração estruturada).

var (
	reINISection = regexp.MustCompile(`^\[([A-Za-z0-9_.\-]+)(:(?:vars|children))?\][ \t]*$`)
	reINIRange   = regexp.MustCompile(`^[A-Za-z0-9_.\-]*\[[0-9a-z]+:[0-9a-z]+\][A-Za-z0-9_.\-]*$`)
)

func findAnsibleINI(s string, add func(ObjFinding)) {
	if strings.IndexByte(s, '[') < 0 {
		return
	}
	strong := strings.Contains(s, "ansible_") || strings.Contains(s, ":children]") || strings.Contains(s, ":vars]")
	if !strong && !strings.Contains(s, "]\n") {
		return
	}
	section := "" // "" = fora de seção; "h" = hosts; "v" = vars/children
	for _, ln := range wrapLines(s) {
		a, b := ln[0], ln[1]
		if a >= b {
			continue
		}
		l := s[a:b]
		if l[0] == '[' {
			m := reINISection.FindStringSubmatch(l)
			switch {
			case m == nil:
				section = ""
			case m[2] != "":
				section = "v"
			default:
				section = "h"
			}
			continue
		}
		if l[0] == '#' || l[0] == ';' || l[0] == ' ' || l[0] == '\t' {
			if l[0] == ' ' || l[0] == '\t' {
				section = "" // linha indentada: não é inventário
			}
			continue
		}
		if section == "" {
			continue
		}
		fields := strings.Fields(l)
		if len(fields) == 0 {
			continue
		}
		pos := a
		ok := true
		for k, c := range fields {
			if k == 0 && section == "h" && strings.IndexByte(c, '=') >= 0 || k > 0 && strings.IndexByte(c, '=') <= 0 {
				ok = false
				break
			}
		}
		if section == "v" && !strings.Contains(fields[0], "=") {
			continue
		}
		if !ok {
			continue
		}
		for k, c := range fields {
			x := pos + strings.Index(s[pos:b], c)
			pos = x + len(c)
			if k == 0 && section == "h" {
				y := x + len(c)
				if i := strings.LastIndexByte(c, ':'); i > 0 && allDigits(c[i+1:]) {
					y = x + i
				}
				h := s[x:y]
				if strong || strings.ContainsAny(h, ".0123456789") {
					addINIHostName(s, x, y, strong, add)
				}
				continue
			}
			eq := strings.IndexByte(c, '=')
			key, va := c[:eq], x+eq+1
			vb := x + len(c)
			if vb-va >= 2 && (s[va] == '"' || s[va] == '\'') && s[vb-1] == s[va] {
				va, vb = va+1, vb-1
			}
			switch key {
			case "ansible_host":
				addName(s, va, vb, "servidor", "ansible", true, add)
			case "ansible_user":
				addName(s, va, vb, "usuario", "ansible", true, add)
			}
		}
	}
}

// addINIHostName: host do inventário; a faixa www[01:50].x é mascarada como unidade.
func addINIHostName(s string, a, b int, strong bool, add func(ObjFinding)) {
	v := s[a:b]
	if strings.ContainsAny(v, "[]") {
		if reINIRange.MatchString(v) {
			add(ObjFinding{a, b, "servidor", "ansible", strong})
		}
		return
	}
	addName(s, a, b, "servidor", "ansible", strong, add)
}

// ---------------------------------------------------------------------------------------
// Bicep
