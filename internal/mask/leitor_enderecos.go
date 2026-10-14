package mask

import "strings"

// Endereços (ver docs/pt-BR/estruturas.md): host interno, armazenamento e filas em URLs, remoto do
// git, pasta pessoal (/home/u, C:\Users\u) e usuário de rede (DOMÍNIO\usuário). No começo, as
// regras de domínio que os outros leitores de desenvolvimento também usam (chave-valor,
// pacotes, caminhos, nuvem).

func internalSuffix(h string) bool {
	// nome DNS de serviço do Kubernetes (servico.namespace.svc[.cluster.local]): fica com o
	// leitor de Kubernetes, que mascara o serviço e o namespace e deixa o sufixo público
	if l := strings.ToLower(h); strings.HasSuffix(l, ".svc.cluster.local") || strings.HasSuffix(l, ".svc") {
		return false
	}
	for _, suf := range internalSuffixes {
		if len(h) > len(suf) && strings.HasSuffix(h, suf) {
			return true
		}
	}
	return false
}

// publicDomain: nome com TLD público ou reservado (não é mascarado: domínio do cliente vai em
// dominios_internos e é pego pelo detector de host).
func publicDomain(h string) bool {
	h = strings.TrimRight(strings.ToLower(h), ".")
	d := strings.LastIndexByte(h, '.')
	if d < 0 || internalSuffix(h) {
		return false
	}
	t := h[d+1:]
	if reservedTLDs[t] || publicTLDs[t] {
		return true
	}
	return len(t) == 2 && letterD(t[0]) && letterD(t[1])
}

// internalHost: rótulo único (sem ponto) ou nome com sufixo interno.
func internalHost(h string) bool {
	h = strings.TrimRight(strings.ToLower(h), ".")
	if len(h) < 2 || publicDev(h) || isSimpleIPv4(h) {
		return false
	}
	if strings.IndexByte(h, '.') < 0 {
		for i := 0; i < len(h); i++ {
			if !(isAlnum(h[i]) || h[i] == '-' || h[i] == '_') {
				return false
			}
		}
		return letterD(h[0])
	}
	return internalSuffix(h)
}

func publicDev(v string) bool {
	l := strings.ToLower(v)
	return vocabDev[l] || publicConnection(l) || codeReceivers[l]
}

// ---- URLs: host interno, armazenamento, filas e remotos do git -------------------------

func urlEnd(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '"', '\'', '`', '<', '>', ')', ']', '}', ',', ';', '|', '\\', '^':
		return true
	}
	return false
}

func findAddresses(s string, add func(ObjFinding)) {
	if strings.Contains(s, "://") {
		for i := strings.Index(s, "://"); i >= 0; {
			// a URL termina, no máximo, onde começa o esquema da próxima: URLs coladas sem
			// separador ("https://a/xhttps://b/y") não fazem cada uma varrer o resto da linha
			// (era quadrático). E nunca passa de maxURL bytes.
			lim, next := min(len(s), i+3+maxURL), -1
			if j := strings.Index(s[i+3:], "://"); j >= 0 {
				next = i + 3 + j
				lim = min(lim, max(i+3, schemeStart(s, next)))
			}
			urlIn(s[:lim], i, add)
			if next < 0 {
				break
			}
			i = next
		}
	}
	findInternalHosts(s, add)
}

// maxURL: até onde se lê uma URL depois do "://".
const maxURL = 2 * lineWindow

// schemeStart: onde começa o esquema que termina no "://" em s[i].
func schemeStart(s string, i int) int {
	a := i
	for a > 0 && i-a < 16 && (isAlnum(s[a-1]) || s[a-1] == '+' || s[a-1] == '-' || s[a-1] == '.') {
		a--
	}
	return a
}

// urlIn lê a URL cujo "://" está em s[i]. s termina onde a URL pode terminar (ver
// findAddresses); o que vem antes de i é lido inteiro.
func urlIn(s string, i int, add func(ObjFinding)) {
	a := schemeStart(s, i)
	// "${X:-http://...}": o "-" de ":-" não faz parte do esquema
	for a < i && s[a] == '-' {
		a++
	}
	if a == i || !letterD(s[a]) || a > 0 && (isAlnum(s[a-1]) || s[a-1] == ':' && !defaultPlaceholder(s, a-1) || s[a-1] == '-' && !defaultPlaceholder(s, a-1)) {
		return
	}
	left := strings.ToLower(s[a:i])
	if databaseSchemes[left] || strings.HasPrefix(left, "jdbc") {
		return
	}
	h := i + 3
	e := h
	for e < len(s) && !urlEnd(s[e]) && s[e] != '/' && s[e] != '?' && s[e] != '#' {
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
	for pb < len(s) && !urlEnd(s[pb]) && s[pb] != '?' && s[pb] != '#' {
		pb++
	}
	for pb > pa && (s[pb-1] == '.' || s[pb-1] == ':') {
		pb--
	}
	host := s[ha:hb]
	user := func(rule string) {
		if ua >= 0 && ub > ua && simpleName(s[ua:ub]) && !publicDev(s[ua:ub]) {
			add(ObjFinding{ua, ub, "usuario", rule, true})
		}
	}
	switch left {
	case "s3", "s3a", "s3n", "gs", "gcs", "az", "oss":
		if simpleName(host) && !publicDev(host) && !strings.HasPrefix(host, "$") {
			add(ObjFinding{ha, hb, "bucket", "armazenamento", true})
		}
		addFolders(s, pa, pb, "armazenamento", add)
	case "abfs", "abfss", "wasb", "wasbs":
		if ua >= 0 && simpleName(s[ua:h+strings.LastIndexByte(s[h:e], '@')]) {
			c := s[ua : h+strings.LastIndexByte(s[h:e], '@')]
			if !publicDev(c) {
				add(ObjFinding{ua, ua + len(c), "bucket", "armazenamento", true})
			}
		}
		if d := strings.IndexByte(host, '.'); d > 0 && !publicDev(host[:d]) {
			add(ObjFinding{ha, ha + d, "conta_nuvem", "armazenamento", true})
		}
		addFolders(s, pa, pb, "armazenamento", add)
	case "hdfs", "webhdfs", "viewfs":
		if simpleName(host) && !publicDomain(host) {
			addHost(s, ha, hb, "armazenamento", true, add)
		}
		addFolders(s, pa, pb, "armazenamento", add)
	case "amqp", "amqps", "kafka", "nats", "tls+nats", "mqtt", "mqtts", "pulsar", "pulsar+ssl", "stomp":
		user("fila")
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
			if hh := s[x:z]; simpleName(hh) && !publicDomain(hh) {
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
			if v := s[q:r]; simpleName(v) && !publicDev(v) {
				add(ObjFinding{q, r, "fila", "fila", true})
			}
		}
	case "ssh", "git", "git+ssh", "ssh+git", "svn+ssh":
		if internalHost(host) {
			addHost(s, ha, hb, "git", true, add)
		}
		user("git")
		addRepo(s, pa, pb, add)
	default:
		internal := internalHost(host) && simpleName(host)
		if internal {
			addHost(s, ha, hb, "url-interna", true, add)
			user("url-interna")
		}
		if (left == "http" || left == "https") && gitContext(s, a, pa, pb) {
			addRepo(s, pa, pb, add)
		}
	}
}

// simpleName: [A-Za-z0-9_.-]+ com uma letra ou dígito no começo.
func simpleName(v string) bool {
	if v == "" || !isAlnum(v[0]) {
		return false
	}
	for i := 0; i < len(v); i++ {
		if !(isAlnum(v[i]) || v[i] == '_' || v[i] == '.' || v[i] == '-') {
			return false
		}
	}
	return true
}

// addFolders: pedaços de um caminho com cara de identificador -> pasta (fraco). O último pedaço
// com extensão é nome de arquivo e fica; partição "chave=valor", curingas e variáveis também.
func addFolders(s string, a, b int, rule string, add func(ObjFinding)) {
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
		if simpleName(v) && identifierShape(v) && !publicDev(v) && !publicFolder(v) {
			add(ObjFinding{a, c, "pasta", rule, false})
		}
		a = c
	}
}

// gitContext: a URL é um remoto do git (termina em .git, vem depois de git clone/remote/
// push/pull/fetch/submodule na mesma linha, ou é o "url =" de uma seção [remote "..."]).
func gitContext(s string, a, pa, pb int) bool {
	if strings.HasSuffix(s[pa:pb], ".git") || strings.HasSuffix(s[pa:pb], ".git/") {
		return true
	}
	li := lineStartJ(s, a)
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
var hostingPieces = setOf("scm", "_git", "v3", "git", "-", "tree", "blob")

// addRepo: org(/subgrupo)/repo(.git) no caminho s[a:b] -> organização e repositório.
func addRepo(s string, a, b int, add func(ObjFinding)) {
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
	lastPos := ps[len(ps)-1]
	if strings.HasSuffix(s[lastPos[0]:lastPos[1]], ".git") {
		lastPos[1] -= 4
	}
	if v := s[lastPos[0]:lastPos[1]]; simpleName(v) && !publicDev(v) {
		add(ObjFinding{lastPos[0], lastPos[1], "repositorio", "git", true})
	}
	for _, p := range ps[:len(ps)-1] {
		v := s[p[0]:p[1]]
		if hostingPieces[v] || strings.HasPrefix(v, "~") || !simpleName(v) || publicDev(v) {
			continue
		}
		add(ObjFinding{p[0], p[1], "organizacao", "git", true})
	}
}

// git@host:org/repo(.git) (forma scp do ssh)
func findGitSCP(s string, add func(ObjFinding)) {
	for i := strings.Index(s, "git@"); i >= 0; {
		if i == 0 || !(isAlnum(s[i-1]) || s[i-1] == '.' || s[i-1] == '-' || s[i-1] == '_' || s[i-1] == '/') {
			ha := i + 4
			hb := ha
			for hb < len(s) && (isAlnum(s[hb]) || s[hb] == '.' || s[hb] == '-') {
				hb++
			}
			if hb < len(s) && s[hb] == ':' && hb > ha && hb+1 < len(s) && s[hb+1] != '/' && !isDig(s[hb+1]) {
				pa := hb + 1
				pb := pa
				for pb < len(s) && (isAlnum(s[pb]) || strings.IndexByte("_.-/~", s[pb]) >= 0) {
					pb++
				}
				for pb > pa && s[pb-1] == '.' && !strings.HasSuffix(s[pa:pb], ".git") {
					pb--
				}
				if internalHost(s[ha:hb]) {
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

// findInternalHosts: nomes com sufixo interno fora de URL ("db01.corp:5432",
// "redis.vendas.svc.cluster.local"). Para não pegar atributo de código ("threading.local")
// nem pacote Java ("org.foo.internal"), pede dígito ou hífen, ou dois rótulos antes do sufixo
// sem cara de pacote, e não pode vir seguido de "(".
func findInternalHosts(s string, add func(ObjFinding)) {
	for _, suf := range internalSuffixes {
		if suf == ".local" && !strings.Contains(s, ".local") {
			continue
		}
		for i := strings.Index(s, suf); i >= 0; {
			end := i + len(suf)
			if end >= len(s) || !(isAlnum(s[end]) || s[end] == '-' || s[end] == '_' || s[end] == '.' && end+1 < len(s) && isAlnum(s[end+1])) {
				internalHostIn(s, i, end, add)
			}
			j := strings.Index(s[i+1:], suf)
			if j < 0 {
				break
			}
			i += 1 + j
		}
	}
}

func internalHostIn(s string, i, end int, add func(ObjFinding)) {
	a := i
	for a > 0 && (isAlnum(s[a-1]) || s[a-1] == '-' || s[a-1] == '.' || s[a-1] == '_') {
		a--
	}
	for a < i && (s[a] == '.' || s[a] == '-') {
		a++
	}
	if a >= i || a > 0 && (s[a-1] == '@' || s[a-1] == '/' && !(a >= 3 && s[a-3:a] == "://")) {
		return // e-mail, caminho (a URL fica com urlIn)
	}
	if a >= 3 && s[a-3:a] == "://" {
		return
	}
	if end < len(s) && s[end] == '(' {
		return
	}
	h := s[a:end]
	if !internalSuffix(h) { // nome DNS de serviço do Kubernetes: fica com o leitor de Kubernetes
		return
	}
	if strings.IndexByte(h, '_') >= 0 && !dashOrDigit(h) {
		return
	}
	labels := strings.Count(s[a:i], ".") + 1
	first := strings.ToLower(h[:strings.IndexByte(h, '.')])
	if packageRoots[first] {
		return
	}
	if !dashOrDigit(s[a:i]) && labels < 2 {
		return
	}
	if publicDev(s[a:i]) {
		return
	}
	add(ObjFinding{a, end, "servidor", "host-interno", true})
}

// ---- caminhos de usuário --------------------------------------------------------------

func publicFolder(v string) bool {
	l := strings.ToLower(v)
	if publicFolders[l] || reVersionOrHash.MatchString(l) || publicDomain(l) || strings.HasPrefix(l, "go-build") {
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

func findPaths(s string, add func(ObjFinding)) {
	for _, pfx := range []string{"/home/", "/Users/", "\\Users\\", "\\users\\"} {
		for i := strings.Index(s, pfx); i >= 0; {
			if pfx[0] == '/' || i > 0 && (s[i-1] == ':' || s[i-1] == '\\') {
				pathIn(s, i+len(pfx), add)
			}
			j := strings.Index(s[i+1:], pfx)
			if j < 0 {
				break
			}
			i += 1 + j
		}
	}
}

func pathSep(c byte) bool { return c == '/' || c == '\\' }

func pathIn(s string, u int, add func(ObjFinding)) {
	for u < len(s) && s[u] == '\\' { // "C:\\Users\\" (escapado em JSON)
		u++
	}
	e := u
	for e < len(s) && e-u <= 32 && (isAlnum(s[e]) || s[e] == '.' || s[e] == '_' || s[e] == '-') {
		e++
	}
	if e == u || e-u > 32 || e < len(s) && !pathSep(s[e]) && !urlEnd(s[e]) && s[e] != ':' {
		return
	}
	name := s[u:e]
	if !letterD(name[0]) && !isDig(name[0]) {
		return
	}
	// usuário público (/home/ubuntu, /home/ec2-user) fica, mas as pastas do projeto abaixo dele
	// continuam sendo lidas
	if !publicDev(name) && len(name) >= 2 {
		add(ObjFinding{u, e, "usuario", "caminho", true})
	}
	prev := ""
	for n, a := 0, e; n < 8 && a < len(s) && pathSep(s[a]); n++ {
		for a < len(s) && pathSep(s[a]) {
			a++
		}
		b := a
		for b < len(s) && (isAlnum(s[b]) || s[b] == '.' || s[b] == '_' || s[b] == '-') {
			b++
		}
		if b == a {
			return
		}
		v := s[a:b]
		if publicDependency(prev, v, s[b:]) {
			return // código de terceiro: .venv/lib/python3.11/site-packages/pandas/...
		}
		prev = v
		dir := b < len(s) && pathSep(s[b])
		if !dir && strings.IndexByte(v, '.') >= 0 {
			return // arquivo
		}
		if v[0] != '.' && identifierShape(v) && !publicFolder(v) && !publicDev(v) {
			add(ObjFinding{a, b, "pasta", "caminho", false})
		}
		if !dir {
			return
		}
		a = b
	}
}

// ---- usuário de rede ------------------------------------------------------------------

func findNetworkUsers(s string, add func(ObjFinding)) {
	if strings.IndexByte(s, '\\') >= 0 {
		findDomainUser(s, add)
	}
	for _, cmd := range []string{"ssh ", "scp ", "sftp ", "rsync "} {
		for i := strings.Index(s, cmd); i >= 0; {
			if i == 0 || strings.IndexByte(" \t\n\"'`$(|;&", s[i-1]) >= 0 {
				sshIn(s, i+len(cmd), add)
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
func findDomainUser(s string, add func(ObjFinding)) {
	// texto de JSON escapado (várias quebras de linha "\\n"): "COMMAND\\nroot" é fim de linha,
	// não domínio e usuário
	escaped := strings.Count(s, `\n`) >= 2
	for i := strings.IndexByte(s, '\\'); i >= 0; {
		if i > 0 && s[i-1] != '\\' {
			u := i + 1
			if u < len(s) && s[u] == '\\' { // escapado: "CORP\\joao"
				u++
			}
			if escaped && u+1 < len(s) && (s[u] == 'n' || s[u] == 't' || s[u] == 'r') && s[u-1] == '\\' && u == i+1 {
				u = len(s) // "\\n": quebra de linha escapada
			}
			a := i
			for a > 0 && i-a <= 15 && (s[a-1] >= 'A' && s[a-1] <= 'Z' || isDig(s[a-1]) || s[a-1] == '-') {
				a--
			}
			d := s[a:i]
			beforeOK := a == 0 || strings.IndexByte(" \t\n\"'(=:,>[", s[a-1]) >= 0
			if len(d) >= 2 && len(d) <= 15 && beforeOK && strings.IndexFunc(d, func(r rune) bool { return r >= 'A' && r <= 'Z' }) >= 0 &&
				!publicDomains[d] && u < len(s) && letterD(s[u]) {
				b := u
				for b < len(s) && b-u <= 64 && (isAlnum(s[b]) || s[b] == '.' || s[b] == '_' || s[b] == '-') {
					b++
				}
				for b > u && s[b-1] == '.' {
					b--
				}
				v := s[u:b]
				escape := strings.IndexByte("ntrbfvaxuU0", v[0]) >= 0 && a > 0 && s[a-1] != ' ' && s[a-1] != '\t' && s[a-1] != '\n'
				// "\nPALAVRA", "\tCAMPO": letra de escape seguida de maiúscula é escape, não usuário
				escape = escape || strings.IndexByte("ntr", v[0]) >= 0 && len(v) > 1 && v[1] >= 'A' && v[1] <= 'Z'
				if len(v) >= 2 && !escape && (b == len(s) || s[b] != '\\' && s[b] != '/') && !publicDev(v) {
					add(ObjFinding{u, b, "usuario", "usuario-rede", true})
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
func sshIn(s string, p int, add func(ObjFinding)) {
	end := strings.IndexByte(s[p:], '\n')
	if end < 0 {
		end = len(s)
	} else {
		end += p
	}
	end = min(end, p+400)
	for x := p; x < end; {
		for x < end && (s[x] == ' ' || s[x] == '\t') {
			x++
		}
		y := x
		for y < end && s[y] != ' ' && s[y] != '\t' {
			y++
		}
		tok := s[x:y]
		if at := strings.IndexByte(tok, '@'); at > 0 && tok[0] != '-' && !strings.Contains(tok, "://") {
			ua, ub := x, x+at
			ha := ub + 1
			hb := ha
			for hb < y && (isAlnum(s[hb]) || s[hb] == '.' || s[hb] == '-' || s[hb] == '_') {
				hb++
			}
			if v := s[ua:ub]; simpleName(v) && !publicDev(v) {
				add(ObjFinding{ua, ub, "usuario", "ssh", true})
			}
			if h := strings.TrimRight(s[ha:hb], "."); h != "" && !isSimpleIPv4(h) && !publicDomain(h) && simpleName(h) {
				addHost(s, ha, ha+len(h), "ssh", true, add)
			}
		}
		x = y
	}
}
