package mask

import (
	"strings"
)

// Internal packages: go.mod module, Maven/Gradle groupId and npm scope (see docs/structures.md,
// section Repositories, packages and paths).

func findPackages(s string, add func(ObjFinding)) {
	if strings.Contains(s, "module ") {
		for i := strings.Index(s, "module "); i >= 0; {
			if i == 0 || s[i-1] == '\n' {
				goModIn(s, i+7, add)
			}
			j := strings.Index(s[i+7:], "module ")
			if j < 0 {
				break
			}
			i += 7 + j
		}
	}
	if strings.Contains(s, "<groupId>") {
		for i := strings.Index(s, "<groupId>"); i >= 0; {
			a := i + 9
			if e := strings.Index(s[a:min(len(s), a+200)], "</groupId>"); e > 0 {
				groupIn(s, a, a+e, add)
			}
			j := strings.Index(s[i+9:], "<groupId>")
			if j < 0 {
				break
			}
			i += 9 + j
		}
	}
	if strings.Contains(s, "group") {
		for i := strings.Index(s, "group"); i >= 0; {
			gradleIn(s, i, add)
			j := strings.Index(s[i+5:], "group")
			if j < 0 {
				break
			}
			i += 5 + j
		}
	}
	if strings.Contains(s, "\"@") && strings.Contains(s, "\"name\"") {
		for i := strings.Index(s, "\"name\""); i >= 0; {
			npmIn(s, i+6, add)
			j := strings.Index(s[i+6:], "\"name\"")
			if j < 0 {
				break
			}
			i += 6 + j
		}
	}
}

// module host/org/x (go.mod)
func goModIn(s string, a int, add func(ObjFinding)) {
	for a < len(s) && s[a] == ' ' {
		a++
	}
	if a < len(s) && s[a] == '"' {
		a++
	}
	b := a
	for b < len(s) && (isAlnum(s[b]) || strings.IndexByte("._~/-", s[b]) >= 0) {
		b++
	}
	r := b
	if r < len(s) && s[r] == '"' {
		r++
	}
	for r < len(s) && (s[r] == ' ' || s[r] == '\t' || s[r] == '\r') {
		r++
	}
	if b == a || r < len(s) && s[r] != '\n' && !strings.HasPrefix(s[r:], "//") {
		return // "module " in the middle of a sentence
	}
	sgms := strings.Split(s[a:b], "/")
	x := a
	if strings.IndexByte(sgms[0], '.') >= 0 {
		h := strings.ToLower(sgms[0])
		if publicCodeHosts[h] {
			return
		}
		if internalHost(h) {
			add(ObjFinding{a, a + len(sgms[0]), "servidor", "pacote", true})
		}
		x += len(sgms[0]) + 1
		sgms = sgms[1:]
	}
	for _, sg := range sgms {
		if simpleName(sg) && !publicDev(sg) && !(len(sg) >= 2 && sg[0] == 'v' && strings.Trim(sg[1:], "0123456789") == "") {
			add(ObjFinding{x, x + len(sg), "pacote", "pacote", true})
		}
		x += len(sg) + 1
	}
}

// Maven/Gradle groupId: the pieces after the reversed TLD ("com.empresa.vendas" -> empresa,
// vendas), outside public prefixes.
func groupIn(s string, a, b int, add func(ObjFinding)) {
	v := s[a:b]
	if !simpleName(v) || strings.IndexByte(v, '.') < 0 {
		return
	}
	l := strings.ToLower(v)
	for _, p := range publicGroups {
		if strings.HasPrefix(l, p) && (len(l) == len(p) || p[len(p)-1] == '.' || p[len(p)-1] == '-' || l[len(p)] == '.') {
			return
		}
	}
	ps := strings.Split(v, ".")
	x := a
	start := true // reversed TLD at the start: "br.com.", "com.", "org."
	for _, p := range ps {
		tld := start && (publicTLDs[strings.ToLower(p)] || len(p) == 2)
		start = tld
		if !tld && len(p) >= 2 && reSimpleIdent.MatchString(p) && !publicDev(p) {
			add(ObjFinding{x, x + len(p), "pacote", "pacote", true})
		}
		x += len(p) + 1
	}
}

// group = 'com.empresa' / group "com.empresa" (build.gradle, at the start of the line)
func gradleIn(s string, i int, add func(ObjFinding)) {
	j := i
	for j > 0 && (s[j-1] == ' ' || s[j-1] == '\t') {
		j--
	}
	if j > 0 && s[j-1] != '\n' {
		return
	}
	p := i + 5
	for p < len(s) && s[p] == ' ' {
		p++
	}
	if p < len(s) && s[p] == '=' {
		p++
		for p < len(s) && s[p] == ' ' {
			p++
		}
	}
	if p == i+5 || p >= len(s) || s[p] != '\'' && s[p] != '"' {
		return
	}
	q := s[p]
	e := strings.IndexByte(s[p+1:min(len(s), p+200)], q)
	if e <= 0 {
		return
	}
	groupIn(s, p+1, p+1+e, add)
}

// "name": "@escopo/pacote" (package.json)
func npmIn(s string, p int, add func(ObjFinding)) {
	for p < len(s) && (s[p] == ' ' || s[p] == '\t') {
		p++
	}
	if p >= len(s) || s[p] != ':' {
		return
	}
	p++
	for p < len(s) && (s[p] == ' ' || s[p] == '\t') {
		p++
	}
	if p+2 >= len(s) || s[p] != '"' || s[p+1] != '@' {
		return
	}
	a := p + 2
	e := strings.IndexByte(s[a:min(len(s), a+214)], '"')
	if e <= 0 {
		return
	}
	v := s[a : a+e]
	k := strings.IndexByte(v, '/')
	if k <= 0 || k == len(v)-1 || !simpleName(v[:k]) || !simpleName(v[k+1:]) {
		return
	}
	if publicNpmScopes[strings.ToLower(v[:k])] {
		return
	}
	add(ObjFinding{a, a + k, "organizacao", "pacote", true})
	add(ObjFinding{a + k + 1, a + e, "pacote", "pacote", true})
}
