package mask

import "strings"

// Vocabulário e regras de domínio comuns aos leitores do lote D (chave-valor, endereços,
// repositórios, pacotes, caminhos, nuvem). Ver docs/estruturas.md.

func sufixoInterno(h string) bool {
	// nome DNS de serviço do Kubernetes (servico.namespace.svc[.cluster.local]): fica com o
	// leitor de Kubernetes, que mascara o serviço e o namespace e deixa o sufixo público
	if l := strings.ToLower(h); strings.HasSuffix(l, ".svc.cluster.local") || strings.HasSuffix(l, ".svc") {
		return false
	}
	for _, suf := range sufixosInternos {
		if len(h) > len(suf) && strings.HasSuffix(h, suf) {
			return true
		}
	}
	return false
}

// dominioPublico: nome com TLD público ou reservado (não é mascarado: domínio do cliente vai em
// dominios_internos e é pego pelo detector de host).
func dominioPublico(h string) bool {
	h = strings.TrimRight(strings.ToLower(h), ".")
	d := strings.LastIndexByte(h, '.')
	if d < 0 || sufixoInterno(h) {
		return false
	}
	t := h[d+1:]
	if tldsReservados[t] || tldsPublicos[t] {
		return true
	}
	return len(t) == 2 && letraD(t[0]) && letraD(t[1])
}

// hostInterno: rótulo único (sem ponto) ou nome com sufixo interno.
func hostInterno(h string) bool {
	h = strings.TrimRight(strings.ToLower(h), ".")
	if len(h) < 2 || publicoDev(h) || ehIPv4Simples(h) {
		return false
	}
	if strings.IndexByte(h, '.') < 0 {
		for i := 0; i < len(h); i++ {
			if !(ehAlnum(h[i]) || h[i] == '-' || h[i] == '_') {
				return false
			}
		}
		return letraD(h[0])
	}
	return sufixoInterno(h)
}

func publicoDev(v string) bool {
	l := strings.ToLower(v)
	return vocabDev[l] || publicoConexao(l) || receptoresCodigo[l]
}

// ---- URLs: host interno, armazenamento, filas e remotos do git -------------------------

func fimURL(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '"', '\'', '`', '<', '>', ')', ']', '}', ',', ';', '|', '\\', '^':
		return true
	}
	return false
}

func acharEnderecos(s string, add func(ObjAchado)) {
	if strings.Contains(s, "://") {
		for i := strings.Index(s, "://"); i >= 0; {
			// a URL termina, no máximo, onde começa o esquema da próxima: URLs coladas sem
			// separador ("https://a/xhttps://b/y") não fazem cada uma varrer o resto da linha
			// (era quadrático). E nunca passa de maxURL bytes.
			lim, prox := min(len(s), i+3+maxURL), -1
			if j := strings.Index(s[i+3:], "://"); j >= 0 {
				prox = i + 3 + j
				lim = min(lim, max(i+3, inicioEsquema(s, prox)))
			}
			urlEm(s[:lim], i, add)
			if prox < 0 {
				break
			}
			i = prox
		}
	}
	acharHostsInternos(s, add)
}

// maxURL: até onde se lê uma URL depois do "://".
const maxURL = 2 * janelaLinha

// inicioEsquema: onde começa o esquema que termina no "://" em s[i].
func inicioEsquema(s string, i int) int {
	a := i
	for a > 0 && i-a < 16 && (ehAlnum(s[a-1]) || s[a-1] == '+' || s[a-1] == '-' || s[a-1] == '.') {
		a--
	}
	return a
}

// urlEm lê a URL cujo "://" está em s[i]. s termina onde a URL pode terminar (ver
// acharEnderecos); o que vem antes de i é lido inteiro.
func urlEm(s string, i int, add func(ObjAchado)) {
	a := inicioEsquema(s, i)
	// "${X:-http://...}": o "-" de ":-" não faz parte do esquema
	for a < i && s[a] == '-' {
		a++
	}
	if a == i || !letraD(s[a]) || a > 0 && (ehAlnum(s[a-1]) || s[a-1] == ':' && !defaultPlaceholder(s, a-1) || s[a-1] == '-' && !defaultPlaceholder(s, a-1)) {
		return
	}
	esq := strings.ToLower(s[a:i])
	if esquemasBanco[esq] || strings.HasPrefix(esq, "jdbc") {
		return
	}
	h := i + 3
	e := h
	for e < len(s) && !fimURL(s[e]) && s[e] != '/' && s[e] != '?' && s[e] != '#' {
		e++
	}
	if e == h {
		return
	}
	ua, ub := -1, -1
	ha := h
	if at := strings.LastIndexByte(s[h:e], '@'); at >= 0 {
		ua, ub = h, h+at
		if c := strings.IndexByte(s[h:h+at], ':'); c >= 0 {
			ub = h + c
		}
		ha = h + at + 1
	}
	hb := e
	if c := strings.IndexByte(s[ha:e], ':'); c >= 0 {
		hb = ha + c
	}
	for hb > ha && s[hb-1] == '.' {
		hb--
	}
	pa, pb := e, e
	for pb < len(s) && !fimURL(s[pb]) && s[pb] != '?' && s[pb] != '#' {
		pb++
	}
	for pb > pa && (s[pb-1] == '.' || s[pb-1] == ':') {
		pb--
	}
	host := s[ha:hb]
	usuario := func(regra string) {
		if ua >= 0 && ub > ua && nomeSimples(s[ua:ub]) && !publicoDev(s[ua:ub]) {
			add(ObjAchado{ua, ub, "usuario", regra, true})
		}
	}
	switch esq {
	case "s3", "s3a", "s3n", "gs", "gcs", "az", "oss":
		if nomeSimples(host) && !publicoDev(host) && !strings.HasPrefix(host, "$") {
			add(ObjAchado{ha, hb, "bucket", "armazenamento", true})
		}
		addPastas(s, pa, pb, "armazenamento", add)
	case "abfs", "abfss", "wasb", "wasbs":
		if ua >= 0 && nomeSimples(s[ua:h+strings.LastIndexByte(s[h:e], '@')]) {
			c := s[ua : h+strings.LastIndexByte(s[h:e], '@')]
			if !publicoDev(c) {
				add(ObjAchado{ua, ua + len(c), "bucket", "armazenamento", true})
			}
		}
		if d := strings.IndexByte(host, '.'); d > 0 && !publicoDev(host[:d]) {
			add(ObjAchado{ha, ha + d, "conta_nuvem", "armazenamento", true})
		}
		addPastas(s, pa, pb, "armazenamento", add)
	case "hdfs", "webhdfs", "viewfs":
		if nomeSimples(host) && !dominioPublico(host) {
			addHost(s, ha, hb, "armazenamento", true, add)
		}
		addPastas(s, pa, pb, "armazenamento", add)
	case "amqp", "amqps", "kafka", "nats", "tls+nats", "mqtt", "mqtts", "pulsar", "pulsar+ssl", "stomp":
		usuario("fila")
		for x := ha; x < e; { // lista de hosts
			y := strings.IndexByte(s[x:e], ',')
			if y < 0 {
				y = e
			} else {
				y += x
			}
			z := y
			if c := strings.IndexByte(s[x:y], ':'); c >= 0 {
				z = x + c
			}
			if hh := s[x:z]; nomeSimples(hh) && !dominioPublico(hh) {
				addHost(s, x, z, "fila", true, add)
			}
			x = y + 1
		}
		if pa < pb && s[pa] == '/' {
			q := pa + 1
			r := q
			for r < pb && s[r] != '/' {
				r++
			}
			if v := s[q:r]; nomeSimples(v) && !publicoDev(v) {
				add(ObjAchado{q, r, "fila", "fila", true})
			}
		}
	case "ssh", "git", "git+ssh", "ssh+git", "svn+ssh":
		if hostInterno(host) {
			addHost(s, ha, hb, "git", true, add)
		}
		usuario("git")
		addRepo(s, pa, pb, add)
	default:
		interno := hostInterno(host) && nomeSimples(host)
		if interno {
			addHost(s, ha, hb, "url-interna", true, add)
			usuario("url-interna")
		}
		if (esq == "http" || esq == "https") && contextoGit(s, a, pa, pb) {
			addRepo(s, pa, pb, add)
		}
	}
}

// nomeSimples: [A-Za-z0-9_.-]+ com uma letra ou dígito no começo.
func nomeSimples(v string) bool {
	if v == "" || !ehAlnum(v[0]) {
		return false
	}
	for i := 0; i < len(v); i++ {
		if !(ehAlnum(v[i]) || v[i] == '_' || v[i] == '.' || v[i] == '-') {
			return false
		}
	}
	return true
}

// addPastas: pedaços de um caminho com cara de identificador -> pasta (fraco). O último pedaço
// com extensão é nome de arquivo e fica; partição "chave=valor", curingas e variáveis também.
func addPastas(s string, a, b int, regra string, add func(ObjAchado)) {
	for n := 0; a < b && n < 12; n++ {
		for a < b && s[a] == '/' {
			a++
		}
		c := a
		for c < b && s[c] != '/' {
			c++
		}
		v := s[a:c]
		if c == b { // último pedaço: arquivo?
			if d := strings.LastIndexByte(v, '.'); d > 0 && c-a-d <= 8 {
				return
			}
		}
		if nomeSimples(v) && caraDeIdentificador(v) && !publicoDev(v) && !pastaPublica(v) {
			add(ObjAchado{a, c, "pasta", regra, false})
		}
		a = c
	}
}

// contextoGit: a URL é um remoto do git (termina em .git, vem depois de git clone/remote/
// push/pull/fetch/submodule na mesma linha, ou é o "url =" de uma seção [remote "..."]).
func contextoGit(s string, a, pa, pb int) bool {
	if strings.HasSuffix(s[pa:pb], ".git") || strings.HasSuffix(s[pa:pb], ".git/") {
		return true
	}
	li := inicioLinhaJ(s, a)
	pre := s[max(li, a-300):a]
	if strings.Contains(pre, "git ") {
		for _, c := range []string{"git clone ", "git remote ", "git push ", "git pull ", "git fetch ", "git submodule "} {
			if strings.Contains(pre, c) {
				return true
			}
		}
	}
	t := strings.TrimSpace(pre)
	if strings.HasPrefix(t, "url") && strings.HasSuffix(strings.TrimSpace(t[3:]), "=") && li > 0 && s[li-1] == '\n' && (s[li] == '\t' || s[li] == ' ') {
		// a seção [remote "..."] fica poucas linhas acima do "url ="
		cfg := s[max(0, a-2000):a]
		return strings.Contains(cfg, "[remote \"") || strings.Contains(cfg, "[submodule \"")
	}
	return false
}

// pedaços de caminho que são da plataforma de hospedagem, não da organização
var pedacosHospedagem = conj("scm", "_git", "v3", "git", "-", "tree", "blob")

// addRepo: org(/subgrupo)/repo(.git) no caminho s[a:b] -> organização e repositório.
func addRepo(s string, a, b int, add func(ObjAchado)) {
	var ps [][2]int
	for x := a; x < b; {
		for x < b && s[x] == '/' {
			x++
		}
		y := x
		for y < b && s[y] != '/' {
			y++
		}
		if y > x {
			ps = append(ps, [2]int{x, y})
		}
		x = y
		if len(ps) > 8 {
			return
		}
	}
	if len(ps) < 2 {
		return
	}
	ult := ps[len(ps)-1]
	if strings.HasSuffix(s[ult[0]:ult[1]], ".git") {
		ult[1] -= 4
	}
	if v := s[ult[0]:ult[1]]; nomeSimples(v) && !publicoDev(v) {
		add(ObjAchado{ult[0], ult[1], "repositorio", "git", true})
	}
	for _, p := range ps[:len(ps)-1] {
		v := s[p[0]:p[1]]
		if pedacosHospedagem[v] || strings.HasPrefix(v, "~") || !nomeSimples(v) || publicoDev(v) {
			continue
		}
		add(ObjAchado{p[0], p[1], "organizacao", "git", true})
	}
}

// git@host:org/repo(.git) (forma scp do ssh)
func acharGitSCP(s string, add func(ObjAchado)) {
	for i := strings.Index(s, "git@"); i >= 0; {
		if i == 0 || !(ehAlnum(s[i-1]) || s[i-1] == '.' || s[i-1] == '-' || s[i-1] == '_' || s[i-1] == '/') {
			ha := i + 4
			hb := ha
			for hb < len(s) && (ehAlnum(s[hb]) || s[hb] == '.' || s[hb] == '-') {
				hb++
			}
			if hb < len(s) && s[hb] == ':' && hb > ha && hb+1 < len(s) && s[hb+1] != '/' && !ehDig(s[hb+1]) {
				pa := hb + 1
				pb := pa
				for pb < len(s) && (ehAlnum(s[pb]) || strings.IndexByte("_.-/~", s[pb]) >= 0) {
					pb++
				}
				for pb > pa && s[pb-1] == '.' && !strings.HasSuffix(s[pa:pb], ".git") {
					pb--
				}
				if hostInterno(s[ha:hb]) {
					addHost(s, ha, hb, "git", true, add)
				}
				addRepo(s, pa, pb, add)
			}
		}
		j := strings.Index(s[i+4:], "git@")
		if j < 0 {
			break
		}
		i += 4 + j
	}
}

// acharHostsInternos: nomes com sufixo interno fora de URL ("db01.corp:5432",
// "redis.vendas.svc.cluster.local"). Para não pegar atributo de código ("threading.local")
// nem pacote Java ("org.foo.internal"), pede dígito ou hífen, ou dois rótulos antes do sufixo
// sem cara de pacote, e não pode vir seguido de "(".
func acharHostsInternos(s string, add func(ObjAchado)) {
	for _, suf := range sufixosInternos {
		if suf == ".local" && !strings.Contains(s, ".local") {
			continue
		}
		for i := strings.Index(s, suf); i >= 0; {
			fim := i + len(suf)
			if fim >= len(s) || !(ehAlnum(s[fim]) || s[fim] == '-' || s[fim] == '_' || s[fim] == '.' && fim+1 < len(s) && ehAlnum(s[fim+1])) {
				hostInternoEm(s, i, fim, add)
			}
			j := strings.Index(s[i+1:], suf)
			if j < 0 {
				break
			}
			i += 1 + j
		}
	}
}

func hostInternoEm(s string, i, fim int, add func(ObjAchado)) {
	a := i
	for a > 0 && (ehAlnum(s[a-1]) || s[a-1] == '-' || s[a-1] == '.' || s[a-1] == '_') {
		a--
	}
	for a < i && (s[a] == '.' || s[a] == '-') {
		a++
	}
	if a >= i || a > 0 && (s[a-1] == '@' || s[a-1] == '/' && !(a >= 3 && s[a-3:a] == "://")) {
		return // e-mail, caminho (a URL fica com urlEm)
	}
	if a >= 3 && s[a-3:a] == "://" {
		return
	}
	if fim < len(s) && s[fim] == '(' {
		return
	}
	h := s[a:fim]
	if !sufixoInterno(h) { // nome DNS de serviço do Kubernetes: fica com o leitor de Kubernetes
		return
	}
	if strings.IndexByte(h, '_') >= 0 && !tracoOuDigito(h) {
		return
	}
	rotulos := strings.Count(s[a:i], ".") + 1
	primeiro := strings.ToLower(h[:strings.IndexByte(h, '.')])
	if raizesPacote[primeiro] {
		return
	}
	if !tracoOuDigito(s[a:i]) && rotulos < 2 {
		return
	}
	if publicoDev(s[a:i]) {
		return
	}
	add(ObjAchado{a, fim, "servidor", "host-interno", true})
}

// ---- caminhos de usuário --------------------------------------------------------------

func pastaPublica(v string) bool {
	l := strings.ToLower(v)
	if pastasPublicas[l] || reVersaoOuHash.MatchString(l) || dominioPublico(l) || strings.HasPrefix(l, "go-build") {
		return true
	}
	// ferramenta + versão: python3.10, go1.22.0, jdk-17, node-v20
	k := 0
	for k < len(l) && l[k] >= 'a' && l[k] <= 'z' {
		k++
	}
	if k > 0 && k < len(l) {
		r := strings.TrimLeft(l[k:], "-v")
		ok := r != ""
		for i := 0; i < len(r); i++ {
			if !(r[i] >= '0' && r[i] <= '9' || r[i] == '.') {
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func acharCaminhos(s string, add func(ObjAchado)) {
	for _, pref := range []string{"/home/", "/Users/", "\\Users\\", "\\users\\"} {
		for i := strings.Index(s, pref); i >= 0; {
			if pref[0] == '/' || i > 0 && (s[i-1] == ':' || s[i-1] == '\\') {
				caminhoEm(s, i+len(pref), add)
			}
			j := strings.Index(s[i+1:], pref)
			if j < 0 {
				break
			}
			i += 1 + j
		}
	}
}

func sepCaminho(c byte) bool { return c == '/' || c == '\\' }

func caminhoEm(s string, u int, add func(ObjAchado)) {
	for u < len(s) && s[u] == '\\' { // "C:\\Users\\" (escapado em JSON)
		u++
	}
	e := u
	for e < len(s) && e-u <= 32 && (ehAlnum(s[e]) || s[e] == '.' || s[e] == '_' || s[e] == '-') {
		e++
	}
	if e == u || e-u > 32 || e < len(s) && !sepCaminho(s[e]) && !fimURL(s[e]) && s[e] != ':' {
		return
	}
	nome := s[u:e]
	if !letraD(nome[0]) && !ehDig(nome[0]) || publicoDev(nome) {
		return
	}
	add(ObjAchado{u, e, "usuario", "caminho", true})
	for n, a := 0, e; n < 8 && a < len(s) && sepCaminho(s[a]); n++ {
		for a < len(s) && sepCaminho(s[a]) {
			a++
		}
		b := a
		for b < len(s) && (ehAlnum(s[b]) || s[b] == '.' || s[b] == '_' || s[b] == '-') {
			b++
		}
		if b == a {
			return
		}
		v := s[a:b]
		dir := b < len(s) && sepCaminho(s[b])
		if !dir && strings.IndexByte(v, '.') >= 0 {
			return // arquivo
		}
		if v[0] != '.' && caraDeIdentificador(v) && !pastaPublica(v) && !publicoDev(v) {
			add(ObjAchado{a, b, "pasta", "caminho", false})
		}
		if !dir {
			return
		}
		a = b
	}
}

// ---- usuário de rede ------------------------------------------------------------------

func acharUsuariosRede(s string, add func(ObjAchado)) {
	if strings.IndexByte(s, '\\') >= 0 {
		acharDominioUsuario(s, add)
	}
	for _, cmd := range []string{"ssh ", "scp ", "sftp ", "rsync "} {
		for i := strings.Index(s, cmd); i >= 0; {
			if i == 0 || strings.IndexByte(" \t\n\"'`$(|;&", s[i-1]) >= 0 {
				sshEm(s, i+len(cmd), add)
			}
			j := strings.Index(s[i+1:], cmd)
			if j < 0 {
				break
			}
			i += 1 + j
		}
	}
}

// DOMINIO\usuario: domínio NetBIOS (maiúsculas, 2 a 15 caracteres). O domínio fica com os
// outros detectores; aqui só o usuário.
func acharDominioUsuario(s string, add func(ObjAchado)) {
	// texto de JSON escapado (várias quebras de linha "\\n"): "COMMAND\\nroot" é fim de linha,
	// não domínio e usuário
	escapado := strings.Count(s, `\n`) >= 2
	for i := strings.IndexByte(s, '\\'); i >= 0; {
		if i > 0 && s[i-1] != '\\' {
			u := i + 1
			if u < len(s) && s[u] == '\\' { // escapado: "CORP\\joao"
				u++
			}
			if escapado && u+1 < len(s) && (s[u] == 'n' || s[u] == 't' || s[u] == 'r') && s[u-1] == '\\' && u == i+1 {
				u = len(s) // "\\n": quebra de linha escapada
			}
			a := i
			for a > 0 && i-a <= 15 && (s[a-1] >= 'A' && s[a-1] <= 'Z' || ehDig(s[a-1]) || s[a-1] == '-') {
				a--
			}
			d := s[a:i]
			antesOK := a == 0 || strings.IndexByte(" \t\n\"'(=:,>[", s[a-1]) >= 0
			if len(d) >= 2 && len(d) <= 15 && antesOK && strings.IndexFunc(d, func(r rune) bool { return r >= 'A' && r <= 'Z' }) >= 0 &&
				!dominiosPublicos[d] && u < len(s) && letraD(s[u]) {
				b := u
				for b < len(s) && b-u <= 64 && (ehAlnum(s[b]) || s[b] == '.' || s[b] == '_' || s[b] == '-') {
					b++
				}
				for b > u && s[b-1] == '.' {
					b--
				}
				v := s[u:b]
				escape := strings.IndexByte("ntrbfvaxuU0", v[0]) >= 0 && a > 0 && s[a-1] != ' ' && s[a-1] != '\t' && s[a-1] != '\n'
				// "\nPALAVRA", "\tCAMPO": letra de escape seguida de maiúscula é escape, não usuário
				escape = escape || strings.IndexByte("ntr", v[0]) >= 0 && len(v) > 1 && v[1] >= 'A' && v[1] <= 'Z'
				if len(v) >= 2 && !escape && (b == len(s) || s[b] != '\\' && s[b] != '/') && !publicoDev(v) {
					add(ObjAchado{u, b, "usuario", "usuario-rede", true})
				}
			}
		}
		j := strings.IndexByte(s[i+1:], '\\')
		if j < 0 {
			break
		}
		i += 1 + j
	}
}

// ssh|scp|sftp|rsync [-opções] usuario@host[:caminho]
func sshEm(s string, p int, add func(ObjAchado)) {
	fim := strings.IndexByte(s[p:], '\n')
	if fim < 0 {
		fim = len(s)
	} else {
		fim += p
	}
	fim = min(fim, p+400)
	for x := p; x < fim; {
		for x < fim && (s[x] == ' ' || s[x] == '\t') {
			x++
		}
		y := x
		for y < fim && s[y] != ' ' && s[y] != '\t' {
			y++
		}
		tok := s[x:y]
		if at := strings.IndexByte(tok, '@'); at > 0 && tok[0] != '-' && !strings.Contains(tok, "://") {
			ua, ub := x, x+at
			ha := ub + 1
			hb := ha
			for hb < y && (ehAlnum(s[hb]) || s[hb] == '.' || s[hb] == '-' || s[hb] == '_') {
				hb++
			}
			if v := s[ua:ub]; nomeSimples(v) && !publicoDev(v) {
				add(ObjAchado{ua, ub, "usuario", "ssh", true})
			}
			if h := strings.TrimRight(s[ha:hb], "."); h != "" && !ehIPv4Simples(h) && !dominioPublico(h) && nomeSimples(h) {
				addHost(s, ha, ha+len(h), "ssh", true, add)
			}
		}
		x = y
	}
}
