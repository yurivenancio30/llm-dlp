package mask

import (
	"regexp"
	"strings"
)

// Caminhos fora da pasta pessoal. A pasta pessoal (/home/u, /Users/u,
// C:\Users\u) fica com o leitor de caminhos de usuário. Aqui:
//
//	/srv/x, /opt/x, /data/x, /mnt/x, /media/x, /var/www/x   cada pasta que não é pública
//	/var/lib/x, /var/log/x, /var/opt/x, /etc/x               só pastas com cara de identificador
//	                                                         (aqui vivem os programas instalados:
//	                                                         /var/lib/postgresql, /etc/nginx)
//	D:\x ... Z:\x (outras unidades)                          cada pasta que não é pública
//	\\servidor\compartilhamento\x (UNC)                      servidor; compartilhamento e pastas
//
// O último pedaço com extensão é nome de arquivo e fica. Diretórios públicos do sistema
// (bin, lib, share, log, conf...) ficam.

var pathRoots = []struct {
	pfx    string
	strict bool // só pastas com cara de identificador
}{
	{"/srv/", false}, {"/opt/", false}, {"/data/", false}, {"/mnt/", false}, {"/media/", false}, {"/var/www/", false},
	{"/var/lib/", true}, {"/var/log/", true}, {"/var/opt/", true}, {"/etc/", true},
}

// pastas públicas comuns dentro dessas raízes
var systemFolders = setOf("bin", "sbin", "lib", "lib64", "share", "include", "local", "log", "logs", "conf", "config",
	"etc", "tmp", "temp", "cache", "run", "data", "html", "www", "public", "static", "assets", "backup", "backups",
	"current", "releases", "shared", "src", "build", "dist", "out", "docs", "doc", "man", "lost+found", "default",
	"conf.d", "sites-available", "sites-enabled", "available", "enabled", "ssl", "certs", "private", "keys",
	"cron.d", "systemd", "system", "init.d", "apt", "dpkg", "docker", "containerd", "kubelet", "snap", "flatpak",
	"homebrew", "anaconda3", "miniconda3", "conda", "venv", "env", "python", "java", "node", "go", "dotnet",
	"windows", "program files", "program files (x86)", "programdata", "users", "temp")

// pathPieceEnd: onde termina o pedaço de caminho que começa em s[a]: letras (também
// acentuadas), dígitos, . _ - + e, no meio, $ e # ("ped$hist", "c#"). "$" no começo é variável.
func pathPieceEnd(s string, a int) int {
	b := a
	for b < len(s) {
		c := s[b]
		switch {
		case isAlnum(c) || c == '.' || c == '_' || c == '-' || c == '+':
			b++
		case (c == '$' || c == '#') && b > a && b+1 < len(s) && s[b+1] != '{' && s[b+1] != '(':
			b++
		default:
			if n := utf8Letter(s, b); n > 0 {
				b += n
				continue
			}
			return b
		}
	}
	return b
}

// folderName: um pedaço de caminho que pode ser nome (ver pathPieceEnd).
func folderName(v string) bool {
	return v != "" && (isAlnum(v[0]) || utf8Letter(v, 0) > 0) && pathPieceEnd(v, 0) == len(v)
}

func folderOutside(v string, strict bool) bool {
	if len(v) < 2 || v[0] == '.' || !folderName(v) {
		return false
	}
	l := strings.ToLower(v)
	if systemFolders[l] || publicFolder(v) || publicDev(v) {
		return false
	}
	return !strict || identifierShape(v)
}

// pathPieces marca as pastas de s[a:] separadas por sep, até o fim do caminho.
func pathPieces(s string, a int, sep byte, strict bool, add func(ObjFinding)) {
	prev := ""
	for n := 0; n < 10 && a < len(s); n++ {
		for a < len(s) && s[a] == sep {
			a++
		}
		b := pathPieceEnd(s, a)
		if b == a {
			return
		}
		v := s[a:b]
		if publicDependency(prev, v, s[b:]) {
			return // código de terceiro: site-packages/pandas/..., node_modules/express/...
		}
		prev = v
		dir := b < len(s) && s[b] == sep
		if !dir && strings.IndexByte(v, '.') > 0 {
			return // arquivo
		}
		if folderOutside(v, strict) {
			add(ObjFinding{a, b, "pasta", "caminho", false})
		}
		if !dir {
			return
		}
		a = b
	}
}

// pathStart: a raiz começa um caminho (não é pedaço de URL nem de outro caminho).
func pathStart(s string, i int) bool {
	if i == 0 {
		return true
	}
	c := s[i-1]
	return c == ' ' || c == '\t' || c == '\n' || c == '"' || c == '\'' || c == '`' || c == '=' || c == '(' ||
		c == ',' || c == ':' || c == '[' || c == '{' || c == '>'
}

func findPathsOutside(s string, add func(ObjFinding)) {
	if strings.IndexByte(s, '/') >= 0 {
		for _, r := range pathRoots {
			for i := strings.Index(s, r.pfx); i >= 0; {
				// ":/srv/" só vale depois de um host (scp "host:/srv/x") ou de "=" e ":" de config
				if pathStart(s, i) && !(i >= 2 && s[i-1] == ':' && s[i-2] == '/') {
					pathPieces(s, i+len(r.pfx), '/', r.strict, add)
				}
				j := strings.Index(s[i+1:], r.pfx)
				if j < 0 {
					break
				}
				i += 1 + j
			}
		}
	}
	if strings.IndexByte(s, '/') >= 0 {
		findRootPaths(s, add)
	}
	if strings.IndexByte(s, '\\') < 0 {
		return
	}
	// outras unidades: D:\ ... Z:\ (também escapado em JSON: D:\\)
	for i := 1; i+2 < len(s); i++ {
		if s[i] != ':' || s[i+1] != '\\' {
			continue
		}
		u := s[i-1] | 0x20
		if u < 'd' || u > 'z' || i >= 2 && (isAlnum(s[i-2]) || s[i-2] == '_' || s[i-2] == '%') {
			continue
		}
		a := i + 1
		for a < len(s) && s[a] == '\\' {
			a++
		}
		slashPathPieces(s, a, add)
	}
	// UNC: \\servidor\compartilhamento\... (ou \\\\servidor\\... escapado)
	for i := 0; i+3 < len(s); i++ {
		if s[i] != '\\' || s[i+1] != '\\' || i > 0 && (s[i-1] == '\\' || isAlnum(s[i-1])) {
			continue
		}
		a := i + 2
		for a < len(s) && s[a] == '\\' {
			a++
		}
		b := a
		for b < len(s) && (isAlnum(s[b]) || s[b] == '.' || s[b] == '-' || s[b] == '_') {
			b++
		}
		if b == a || b >= len(s) || s[b] != '\\' || a-i > 4 {
			continue
		}
		h := s[a:b]
		if !letterD(h[0]) && !isDig(h[0]) || publicDev(h) || publicDomain(strings.ToLower(h)) || len(h) < 2 || reTextEscape.MatchString(h) {
			continue
		}
		add(ObjFinding{a, b, "servidor", "caminho", true})
		slashPathPieces(s, b, add)
		i = b
	}
}

// escape de string escapado ("\\uD801", "\\U000003B4", "\\x0a", "\\012"): não é servidor nem pasta
var reTextEscape = regexp.MustCompile(`^(?:u[0-9A-Fa-f]{4}|U[0-9A-Fa-f]{8}|x[0-9A-Fa-f]{2}|[0-7]{3})`)

// slashPathPieces: como pathPieces, com "\" (ou "\\" escapado) como separador.
func slashPathPieces(s string, a int, add func(ObjFinding)) {
	for n := 0; n < 10 && a < len(s); n++ {
		for a < len(s) && s[a] == '\\' {
			a++
		}
		b := a
		for b < len(s) && (isAlnum(s[b]) || s[b] == '.' || s[b] == '_' || s[b] == '-' || s[b] == '$') {
			b++
		}
		if b == a {
			return
		}
		v := strings.TrimSuffix(s[a:b], "$") // compartilhamento administrativo: C$
		if reTextEscape.MatchString(v) {
			return // "\\u00e9", "\\x0a": escape de string, não pasta
		}
		dir := b < len(s) && s[b] == '\\'
		if !dir && strings.IndexByte(v, '.') > 0 {
			return
		}
		if folderOutside(v, false) {
			add(ObjFinding{a, a + len(v), "pasta", "caminho", false})
		}
		if !dir {
			return
		}
		a = b
	}
}

// primeiro nível público: diretórios do sistema (FHS, macOS), as raízes já tratadas acima e
// as rotas web comuns (/api/v1/..., /static/...), que têm a mesma forma de um caminho
var publicRoots = setOf("bin", "boot", "dev", "etc", "home", "lib", "lib32", "lib64", "libx32", "media", "mnt", "opt",
	"proc", "root", "run", "sbin", "snap", "srv", "sys", "tmp", "usr", "var", "data", "users", "library",
	"applications", "system", "volumes", "private", "cores", "nix", "gnu", "network", "afs", "net", "cdrom",
	"selinux", "workspace", "workspaces", "app", "code", "src", "go", "builds", "build", "buildd",
	"api", "v1", "v2", "v3", "v4", "static", "assets", "public", "docs", "doc", "auth", "oauth", "login", "logout",
	"health", "healthz", "readyz", "livez", "metrics", "graphql", "swagger", "openapi", "ws", "css", "js", "img",
	"images", "fonts", "icons", "favicon.ico", ".well-known", "admin", "user", "users", "search", "en", "pt", "pt-br",
	"blob", "tree", "raw", "wiki", "issues", "pull", "releases", "download", "downloads", "proxy", "callback",
	// ID de recurso de nuvem (fica com o leitor de nuvem)
	"subscriptions", "providers", "resourcegroups", "projects", "organizations", "folders", "locations", "apis")

// findRootPaths: caminho absoluto com 2 ou mais níveis cujo primeiro nível não é público
// (/dados/Relatorios/..., /backup_x/...). Cada pasta que não é pública é mascarada (fraco).
func findRootPaths(s string, add func(ObjFinding)) {
	for i := strings.IndexByte(s, '/'); i >= 0 && i+1 < len(s); {
		if pathStart(s, i) && letterD(s[i+1]) {
			a := i + 1
			b := a
			for b < len(s) && (isAlnum(s[b]) || s[b] == '.' || s[b] == '_' || s[b] == '-') {
				b++
			}
			// primeiro nível com pelo menos 4 caracteres: raiz curta (/gc, /cpu, /x) é nome de
			// métrica ou rota; as raízes reais de 3 letras (/srv, /opt, /mnt) têm regra própria
			if b-a >= 4 && b+1 < len(s) && s[b] == '/' && (isAlnum(s[b+1]) || s[b+1] == '_') && !publicRoots[strings.ToLower(s[a:b])] {
				// fim do caminho; "/gc/heap/allocs:bytes" (nome de métrica com unidade) não é arquivo
				e := b
				for e < len(s) && (isAlnum(s[e]) || strings.IndexByte("._-/+", s[e]) >= 0) {
					e++
				}
				if !(e+1 < len(s) && s[e] == ':' && letterD(s[e+1])) {
					pathPieces(s, a, '/', false, add)
				}
			}
		}
		j := strings.IndexByte(s[i+1:], '/')
		if j < 0 {
			break
		}
		i += 1 + j
	}
}
