package mask

import "strings"

// Addresses (see docs/structures.md): internal host, storage and queues in URLs, git remote, home
// folder (/home/u, C:\Users\u) and network user (DOMAIN\user). At the start, the domain rules the
// other development readers also use (key-value, packages, paths, cloud).

func internalSuffix(h string) bool {
	// Kubernetes service DNS name (service.namespace.svc[.cluster.local]): stays with the
	// Kubernetes reader, which masks the service and the namespace and leaves the public suffix
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

// publicDomain: a name with a public or reserved TLD (not masked: the client domain goes in
// internal_domains and is caught by the host detector).
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

// internalHost: a single label (without a dot) or a name with an internal suffix.
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

// ---- URLs: internal host, storage, queues and git remotes ----------------------------------

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
			// the URL ends, at most, where the scheme of the next one starts: URLs glued without a
			// separator ("https://a/xhttps://b/y") do not make each one scan the rest of the line
			// (it was quadratic). And it never goes past maxURL bytes.
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

// maxURL: how far a URL is read after the "://".
const maxURL = 2 * lineWindow

// schemeStart: where the scheme that ends at the "://" at s[i] starts.
func schemeStart(s string, i int) int {
	a := i
	for a > 0 && i-a < 16 && (isAlnum(s[a-1]) || s[a-1] == '+' || s[a-1] == '-' || s[a-1] == '.') {
		a--
	}
	return a
}

// urlIn reads the URL whose "://" is at s[i]. s ends where the URL may end (see findAddresses);
// what comes before i is read whole.
func urlIn(s string, i int, add func(ObjFinding)) {
	a := schemeStart(s, i)
	// "${X:-http://...}": the "-" of ":-" is not part of the scheme
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
			add(ObjFinding{ua, ub, "user", rule, true})
		}
	}
	switch left {
	case "s3", "s3a", "s3n", "gs", "gcs", "az", "oss":
		if simpleName(host) && !publicDev(host) && !strings.HasPrefix(host, "$") {
			add(ObjFinding{ha, hb, "bucket", "storage", true})
		}
		addFolders(s, pa, pb, "storage", add)
	case "abfs", "abfss", "wasb", "wasbs":
		if ua >= 0 && simpleName(s[ua:h+strings.LastIndexByte(s[h:e], '@')]) {
			c := s[ua : h+strings.LastIndexByte(s[h:e], '@')]
			if !publicDev(c) {
				add(ObjFinding{ua, ua + len(c), "bucket", "storage", true})
			}
		}
		if d := strings.IndexByte(host, '.'); d > 0 && !publicDev(host[:d]) {
			add(ObjFinding{ha, ha + d, "cloud_account", "storage", true})
		}
		addFolders(s, pa, pb, "storage", add)
	case "hdfs", "webhdfs", "viewfs":
		if simpleName(host) && !publicDomain(host) {
			addHost(s, ha, hb, "storage", true, add)
		}
		addFolders(s, pa, pb, "storage", add)
	case "amqp", "amqps", "kafka", "nats", "tls+nats", "mqtt", "mqtts", "pulsar", "pulsar+ssl", "stomp":
		user("queue")
		for x := ha; x < e; { // host list
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
				addHost(s, x, z, "queue", true, add)
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
				add(ObjFinding{q, r, "queue", "queue", true})
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
			addHost(s, ha, hb, "internal-url", true, add)
			user("internal-url")
		}
		if (left == "http" || left == "https") && gitContext(s, a, pa, pb) {
			addRepo(s, pa, pb, add)
		}
	}
}

// simpleName: [A-Za-z0-9_.-]+ with a letter or digit at the start.
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

// addFolders: identifier-shaped pieces of a path -> folder (weak). The last piece with an extension
// is a file name and stays; "key=value" partitions, wildcards and variables too.
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
		if c == b { // last piece: a file?
			if d := strings.LastIndexByte(v, '.'); d > 0 && c-a-d <= 8 {
				return
			}
		}
		if simpleName(v) && identifierShape(v) && !publicDev(v) && !publicFolder(v) {
			add(ObjFinding{a, c, "folder", rule, false})
		}
		a = c
	}
}

// gitContext: the URL is a git remote (ends in .git, comes after git
// clone/remote/push/pull/fetch/submodule on the same line, or is the "url =" of a [remote "..."]
// section).
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
		// the [remote "..."] section is a few lines above the "url ="
		cfg := s[max(0, a-2000):a]
		return strings.Contains(cfg, "[remote \"") || strings.Contains(cfg, "[submodule \"")
	}
	return false
}

// path pieces that belong to the hosting platform, not to the organization
var hostingPieces = setOf("scm", "_git", "v3", "git", "-", "tree", "blob")

// addRepo: org(/subgroup)/repo(.git) in the path s[a:b] -> organization and repository.
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
		add(ObjFinding{lastPos[0], lastPos[1], "repository", "git", true})
	}
	for _, p := range ps[:len(ps)-1] {
		v := s[p[0]:p[1]]
		if hostingPieces[v] || strings.HasPrefix(v, "~") || !simpleName(v) || publicDev(v) {
			continue
		}
		add(ObjFinding{p[0], p[1], "organization", "git", true})
	}
}

// git@host:org/repo(.git) (ssh's scp form)
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

// findInternalHosts: names with an internal suffix outside a URL ("db01.corp:5432",
// "redis.vendas.svc.cluster.local"). To not catch a code attribute ("threading.local") or a Java
// package ("org.foo.internal"), it asks for a digit or a hyphen, or two labels before the suffix
// without a package look, and it cannot be followed by "(".
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
		return // e-mail, path (the URL stays with urlIn)
	}
	if a >= 3 && s[a-3:a] == "://" {
		return
	}
	if end < len(s) && s[end] == '(' {
		return
	}
	h := s[a:end]
	if !internalSuffix(h) { // Kubernetes service DNS name: stays with the Kubernetes reader
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
	add(ObjFinding{a, end, "server", "internal-host", true})
}

// ---- user paths ----------------------------------------------------------------------------

func publicFolder(v string) bool {
	l := strings.ToLower(v)
	if publicFolders[l] || reVersionOrHash.MatchString(l) || publicDomain(l) || strings.HasPrefix(l, "go-build") {
		return true
	}
	// tool + version: python3.10, go1.22.0, jdk-17, node-v20
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
	for u < len(s) && s[u] == '\\' { // "C:\\Users\\" (escaped in JSON)
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
	// a public user (/home/ubuntu, /home/ec2-user) stays, but the project folders below it are
	// still read
	if !publicDev(name) && len(name) >= 2 {
		add(ObjFinding{u, e, "user", "path", true})
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
			return // third-party code: .venv/lib/python3.11/site-packages/pandas/...
		}
		prev = v
		dir := b < len(s) && pathSep(s[b])
		if !dir && strings.IndexByte(v, '.') >= 0 {
			return // arquivo
		}
		if v[0] != '.' && identifierShape(v) && !publicFolder(v) && !publicDev(v) {
			add(ObjFinding{a, b, "folder", "path", false})
		}
		if !dir {
			return
		}
		a = b
	}
}

// ---- network user ---------------------------------------------------------------------------

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

// DOMAIN\user: NetBIOS domain (uppercase, 2 to 15 characters). The domain stays with the other
// detectors; here only the user.
func findDomainUser(s string, add func(ObjFinding)) {
	// escaped JSON text (several "\\n" line breaks): "COMMAND\\nroot" is an end of line, not domain
	// and user
	escaped := strings.Count(s, `\n`) >= 2
	for i := strings.IndexByte(s, '\\'); i >= 0; {
		if i > 0 && s[i-1] != '\\' {
			u := i + 1
			if u < len(s) && s[u] == '\\' { // escapado: "CORP\\joao"
				u++
			}
			if escaped && u+1 < len(s) && (s[u] == 'n' || s[u] == 't' || s[u] == 'r') && s[u-1] == '\\' && u == i+1 {
				u = len(s) // "\\n": escaped line break
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
				// "\nWORD", "\tFIELD": an escape letter followed by a capital is an escape, not a
				// user
				escape = escape || strings.IndexByte("ntr", v[0]) >= 0 && len(v) > 1 && v[1] >= 'A' && v[1] <= 'Z'
				if len(v) >= 2 && !escape && (b == len(s) || s[b] != '\\' && s[b] != '/') && !publicDev(v) {
					if insideWinPath(s, a) {
						// "...\Power BI\Banco de Dados": the folder after a folder whose name has a
						// space (where the path readers stop), not a user
						if folderOutside(v, false) {
							add(ObjFinding{u, b, "folder", "path", false})
						}
					} else {
						add(ObjFinding{u, b, "user", "network-user", true})
					}
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

// insideWinPath: the word that starts at s[a] (the candidate domain) ends a folder name of a
// Windows path ("...\Power BI\Banco de Dados", "C:\Users\x\OneDrive - Empresa SA\Arquivos"), and
// what comes after the "\" is the next folder, not a user: from it
// back to the previous "\" there are only characters a folder name accepts (Windows reserves
// < > : " / | ? * in names: learn.microsoft.com/windows/win32/fileio/naming-a-file), and that "\" does
// not close another DOMAIN\user ("CORP\ana CORP\joao"). Up to 260 characters back (MAX_PATH, the
// same page).
func insideWinPath(s string, a int) bool {
	k := a - 1
	for k >= 0 && s[k] != '\\' {
		if a-k > 260 {
			return false
		}
		switch s[k] {
		case '<', '>', ':', '"', '/', '|', '?', '*', '\n', '\r', '\t', '\'', '`':
			return false
		}
		k--
	}
	if k < 0 {
		return false
	}
	for k > 0 && s[k-1] == '\\' { // escaped: "\\"
		k--
	}
	d := k
	for d > 0 && (s[d-1] >= 'A' && s[d-1] <= 'Z' || isDig(s[d-1]) || s[d-1] == '-') {
		d--
	}
	otherUser := k-d >= 2 && k-d <= 15 && (d == 0 || strings.IndexByte(" \t\n\"'(=:,>[", s[d-1]) >= 0)
	return !otherUser
}

// ssh|scp|sftp|rsync [-options] user@host[:path]
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
				add(ObjFinding{ua, ub, "user", "ssh", true})
			}
			if h := strings.TrimRight(s[ha:hb], "."); h != "" && !isSimpleIPv4(h) && !publicDomain(h) && simpleName(h) {
				addHost(s, ha, ha+len(h), "ssh", true, add)
			}
		}
		x = y
	}
}
