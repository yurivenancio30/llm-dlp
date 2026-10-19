package mask

import (
	"regexp"
	"strings"
)

// Paths outside the home folder. The home folder (/home/u, /Users/u, C:\Users\u) is left to the
// user paths reader. Here:
//
//	/srv/x, /opt/x, /data/x, /mnt/x, /media/x, /var/www/x   each folder that is not public
//	/var/lib/x, /var/log/x, /var/opt/x, /etc/x               only folders that look like an identifier
//	                                                         (installed programs live here:
//	                                                         /var/lib/postgresql, /etc/nginx)
//	D:\x ... Z:\x (other drives)                             each folder that is not public
//	\\server\share\x (UNC)                                   server; share and folders
//
// The last piece with an extension is a file name and stays. Public system directories (bin, lib,
// share, log, conf...) stay.

var pathRoots = []struct {
	pfx    string
	strict bool // only folders that look like an identifier
}{
	{"/srv/", false}, {"/opt/", false}, {"/data/", false}, {"/mnt/", false}, {"/media/", false}, {"/var/www/", false},
	{"/var/lib/", true}, {"/var/log/", true}, {"/var/opt/", true}, {"/etc/", true},
}

// common public folders inside those roots
var systemFolders = setOf("bin", "sbin", "lib", "lib64", "share", "include", "local", "log", "logs", "conf", "config",
	"etc", "tmp", "temp", "cache", "run", "data", "html", "www", "public", "static", "assets", "backup", "backups",
	"current", "releases", "shared", "src", "build", "dist", "out", "docs", "doc", "man", "lost+found", "default",
	"conf.d", "sites-available", "sites-enabled", "available", "enabled", "ssl", "certs", "private", "keys",
	"cron.d", "systemd", "system", "init.d", "apt", "dpkg", "docker", "containerd", "kubelet", "snap", "flatpak",
	"homebrew", "anaconda3", "miniconda3", "conda", "venv", "env", "python", "java", "node", "go", "dotnet",
	"windows", "program files", "program files (x86)", "programdata", "users", "temp")

// pathPieceEnd: where the path piece starting at s[a] ends: letters (accented too), digits, . _ - +
// and, in the middle, $ and # ("ped$hist", "c#"). "$" at the start is a variable.
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

// fileName: the last piece of a path is a file when what comes after its last dot has the shape of
// an extension: 1 to 5 lowercase letters or digits (x.tf, main.py, dump.sql.gz, v1.2.3), up to 4
// uppercase (IMG_01.JPG), or a known one. "HX.AWS.PRD.Carga" and "app.Settings" are names with dots
// (a folder of "cd ~/HX.AWS.PRD.Carga").
func fileName(v string) bool {
	v = strings.TrimRight(v, ".") // full stop of the sentence ("lendo /srv/x/saida.csv.")
	if strings.Trim(v, "0123456789.") == "" {
		return true // a number ("35."), not a name
	}
	d := strings.LastIndexByte(v, '.')
	if d <= 0 || d == len(v)-1 {
		return false
	}
	ext := v[d+1:]
	// "x.pt-BR", and the context line of grep -C ("relatorio.md-texto da linha")
	if h := strings.IndexByte(ext, '-'); h > 0 {
		ext = ext[:h]
	}
	if fileExtensions[strings.ToLower(ext)] {
		return true
	}
	lower, upper := len(ext) <= 5, len(ext) <= 4
	for i := 0; i < len(ext); i++ {
		c := ext[i]
		lower = lower && (c >= 'a' && c <= 'z' || isDig(c))
		upper = upper && (c >= 'A' && c <= 'Z' || isDig(c))
	}
	return lower || upper
}

// folderName: a path piece that may be a name (see pathPieceEnd).
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

// pathPieces marks the folders of s[a:] separated by sep, up to the end of the path.
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
			return // third-party code: site-packages/pandas/..., node_modules/express/...
		}
		prev = v
		dir := b < len(s) && s[b] == sep
		if !dir && fileName(v) {
			return // arquivo
		}
		if !dir {
			if b = a + len(strings.TrimRight(v, ".")); b == a {
				return
			}
			v = s[a:b] // the full stop of the sentence stays
		}
		if folderOutside(v, strict) {
			add(ObjFinding{a, b, "folder", "path", false})
		}
		if !dir {
			return
		}
		a = b
	}
}

// pathStart: the root starts a path (it is not a piece of a URL or of another path).
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
				// ":/srv/" only counts after a host (scp "host:/srv/x") or after a config "=" and
				// ":"
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
	// other drives: D:\ ... Z:\ (also escaped in JSON: D:\\)
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
	// UNC: \\server\share\... (or \\\\server\\... escaped)
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
		add(ObjFinding{a, b, "server", "path", true})
		slashPathPieces(s, b, add)
		i = b
	}
}

// escaped string escape ("\\uD801", "\\U000003B4", "\\x0a", "\\012"): neither a server nor a folder
var reTextEscape = regexp.MustCompile(`^(?:u[0-9A-Fa-f]{4}|U[0-9A-Fa-f]{8}|x[0-9A-Fa-f]{2}|[0-7]{3})`)

// slashPathPieces: like pathPieces, with "\" (or escaped "\\") as the separator.
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
			return // "\\u00e9", "\\x0a": string escape, not a folder
		}
		dir := b < len(s) && s[b] == '\\'
		if !dir && fileName(v) {
			return
		}
		if !dir {
			v = strings.TrimRight(v, ".") // the full stop of the sentence stays
		}
		if folderOutside(v, false) {
			add(ObjFinding{a, a + len(v), "folder", "path", false})
		}
		if !dir {
			return
		}
		a = b
	}
}

// public first level: system directories (FHS, macOS), the roots already handled above and common
// web routes (/api/v1/..., /static/...), which have the same shape as a path
var publicRoots = setOf("bin", "boot", "dev", "etc", "home", "lib", "lib32", "lib64", "libx32", "media", "mnt", "opt",
	"proc", "root", "run", "sbin", "snap", "srv", "sys", "tmp", "usr", "var", "data", "users", "library",
	"applications", "system", "volumes", "private", "cores", "nix", "gnu", "network", "afs", "net", "cdrom",
	"selinux", "workspace", "workspaces", "app", "code", "src", "go", "builds", "build", "buildd",
	"api", "v1", "v2", "v3", "v4", "static", "assets", "public", "docs", "doc", "auth", "oauth", "login", "logout",
	"health", "healthz", "readyz", "livez", "metrics", "graphql", "swagger", "openapi", "ws", "css", "js", "img",
	"images", "fonts", "icons", "favicon.ico", ".well-known", "admin", "user", "users", "search", "en", "pt", "pt-br",
	"blob", "tree", "raw", "wiki", "issues", "pull", "releases", "download", "downloads", "proxy", "callback",
	// cloud resource ID (left to the cloud reader)
	"subscriptions", "providers", "resourcegroups", "projects", "organizations", "folders", "locations", "apis")

// findRootPaths: absolute path with 2 or more levels whose first level is not public
// (/dados/Relatorios/..., /backup_x/...). Each folder that is not public is masked (weak).
func findRootPaths(s string, add func(ObjFinding)) {
	for i := strings.IndexByte(s, '/'); i >= 0 && i+1 < len(s); {
		if pathStart(s, i) && letterD(s[i+1]) {
			a := i + 1
			b := a
			for b < len(s) && (isAlnum(s[b]) || s[b] == '.' || s[b] == '_' || s[b] == '-') {
				b++
			}
			// first level with at least 4 characters: a short root (/gc, /cpu, /x) is a metric name
			// or route; the real 3-letter roots (/srv, /opt, /mnt) have their own rule
			if b-a >= 4 && b+1 < len(s) && s[b] == '/' && (isAlnum(s[b+1]) || s[b+1] == '_') && !publicRoots[strings.ToLower(s[a:b])] {
				// end of the path; "/gc/heap/allocs:bytes" (metric name with a unit) is not a file
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
