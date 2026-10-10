package mask

import (
	"strings"
)

// Pacotes internos: módulo do go.mod, groupId do Maven/Gradle e escopo do npm (ver
// docs/estruturas.md, seção Repositórios, pacotes e caminhos).

func acharPacotes(s string, add func(ObjAchado)) {
	if strings.Contains(s, "module ") {
		for i := strings.Index(s, "module "); i >= 0; {
			if i == 0 || s[i-1] == '\n' {
				goModEm(s, i+7, add)
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
				grupoEm(s, a, a+e, add)
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
			gradleEm(s, i, add)
			j := strings.Index(s[i+5:], "group")
			if j < 0 {
				break
			}
			i += 5 + j
		}
	}
	if strings.Contains(s, "\"@") && strings.Contains(s, "\"name\"") {
		for i := strings.Index(s, "\"name\""); i >= 0; {
			npmEm(s, i+6, add)
			j := strings.Index(s[i+6:], "\"name\"")
			if j < 0 {
				break
			}
			i += 6 + j
		}
	}
}

// module host/org/x (go.mod)
func goModEm(s string, a int, add func(ObjAchado)) {
	for a < len(s) && s[a] == ' ' {
		a++
	}
	if a < len(s) && s[a] == '"' {
		a++
	}
	b := a
	for b < len(s) && (ehAlnum(s[b]) || strings.IndexByte("._~/-", s[b]) >= 0) {
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
		return // "module " no meio de uma frase
	}
	segs := strings.Split(s[a:b], "/")
	x := a
	if strings.IndexByte(segs[0], '.') >= 0 {
		h := strings.ToLower(segs[0])
		if hostsCodigoPublico[h] {
			return
		}
		if hostInterno(h) {
			add(ObjAchado{a, a + len(segs[0]), "servidor", "pacote", true})
		}
		x += len(segs[0]) + 1
		segs = segs[1:]
	}
	for _, sg := range segs {
		if nomeSimples(sg) && !publicoDev(sg) && !(len(sg) >= 2 && sg[0] == 'v' && strings.Trim(sg[1:], "0123456789") == "") {
			add(ObjAchado{x, x + len(sg), "pacote", "pacote", true})
		}
		x += len(sg) + 1
	}
}

// groupId do Maven/Gradle: os pedaços depois do TLD invertido ("com.empresa.vendas" ->
// empresa, vendas), fora dos prefixos públicos.
func grupoEm(s string, a, b int, add func(ObjAchado)) {
	v := s[a:b]
	if !nomeSimples(v) || strings.IndexByte(v, '.') < 0 {
		return
	}
	l := strings.ToLower(v)
	for _, p := range gruposPublicos {
		if strings.HasPrefix(l, p) && (len(l) == len(p) || p[len(p)-1] == '.' || p[len(p)-1] == '-' || l[len(p)] == '.') {
			return
		}
	}
	ps := strings.Split(v, ".")
	x := a
	inicio := true // TLD invertido no começo: "br.com.", "com.", "org."
	for _, p := range ps {
		tld := inicio && (tldsPublicos[strings.ToLower(p)] || len(p) == 2)
		inicio = tld
		if !tld && len(p) >= 2 && reIdentSimples.MatchString(p) && !publicoDev(p) {
			add(ObjAchado{x, x + len(p), "pacote", "pacote", true})
		}
		x += len(p) + 1
	}
}

// group = 'com.empresa' / group "com.empresa" (build.gradle, no começo da linha)
func gradleEm(s string, i int, add func(ObjAchado)) {
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
	grupoEm(s, p+1, p+1+e, add)
}

// "name": "@escopo/pacote" (package.json)
func npmEm(s string, p int, add func(ObjAchado)) {
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
	if k <= 0 || k == len(v)-1 || !nomeSimples(v[:k]) || !nomeSimples(v[k+1:]) {
		return
	}
	if escoposNpmPublicos[strings.ToLower(v[:k])] {
		return
	}
	add(ObjAchado{a, a + k, "organizacao", "pacote", true})
	add(ObjAchado{a + k + 1, a + e, "pacote", "pacote", true})
}
