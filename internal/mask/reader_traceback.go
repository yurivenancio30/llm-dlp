package mask

import (
	"regexp"
	"strings"
)

// Traceback reader (see docs/structures.md, section Public only with proof). In an error, each
// traceback line says where it came from, the way a compiler's include paths separate system
// headers from project headers: the line pointing to a dependency or to the standard library
// (site-packages, node_modules, go/pkg/mod, .m2, /usr/lib/python3.x, the Go runtime) is vendor code
// and stays; one pointing elsewhere is customer code: the function, the identifiers of the code
// line and, at the end, the value cited in the error (KeyError: 'COD_APOLICE') are its names. The
// path folders are left to the path readers.
//
// Formats: Python (File "x", line N, in f + the code line), Java/Kotlin/Scala (at
// package.Class.method(File.java:N)), JavaScript (at f (path:L:C)) and Go (package.Function(...) +
// the line with the path).
//
// Being in a dependency folder is not enough (see thirdPartyPath): the installed package has to be
// public software (public_software.txt, Official Image) and not be defined in the project
// (shadowing); the module in the Go, Maven, Gradle or Cargo cache has to be from a public owner or
// group. A customer's internal package installed in the virtual environment, or a private module
// downloaded to the cache, still belongs to the customer.

var (
	reFramePy    = regexp.MustCompile(`^\s*File "([^"\n]+)", line \d+, in ([\w<>.]+)`)
	reFrameJava  = regexp.MustCompile(`^\s*at ((?:[\w$]+\.)+)([\w$<>]+)\(([\w$.-]*)(?::\d+)?\)`)
	reFrameJS    = regexp.MustCompile(`^\s*at (?:(?:async )?([\w$.<>\[\] ]+?) \()?((?:[A-Za-z]:)?[^():\s]+):\d+:\d+\)?\s*$`)
	reFrameGo    = regexp.MustCompile(`^((?:[\w.-]+/)*[\w.-]+)\.(\(?\*?[\w]+\)?\.)?([\w]+)\(`)
	reGoPath     = regexp.MustCompile(`^\t(/[^:\s]+\.go):\d+`)
	reFinalError = regexp.MustCompile(`^([\w.]+(?:Error|Exception|Exit|Interrupt|Warning)|panic):\s*(.*)$`)
	reCodeIdent  = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	reLibPython  = regexp.MustCompile(`/lib(?:64)?/python\d+\.\d+/`)
	reQuoted     = regexp.MustCompile(`'([^'\n]{2,80})'|"([^"\n]{2,80})"`)
)

// thirdPartyPath: the file p (from a frame or a read) is from a public dependency or from the
// standard library. Being in a dependency folder is not enough: the package has to be public (a
// customer's internal package is installed and downloaded to the same folders).
func (m *Masker) thirdPartyPath(p string) bool {
	p = strings.ReplaceAll(p, "\\", "/")
	if pkg, ok := installedPackage(p); ok {
		_, local := m.localSoft.Load(normSoftware(pkg))
		return !local && dependencyPackage(pkg)
	}
	for _, c := range []struct {
		folder string
		public func(rest string) bool
	}{{"/pkg/mod/", publicGoModule}, {"/.m2/repository/", publicMavenGroup}, {"/.cargo/registry/", publicCargoRegistry},
		{"/.gradle/caches/", publicGradleCache}} {
		if i := strings.Index(p, c.folder); i >= 0 {
			return c.public(p[i+len(c.folder):])
		}
	}
	// the JVM and GOROOT in the known installation places
	for _, d := range []string{"/usr/lib/jvm/", "/usr/local/go/src/", "/usr/lib/go/", "/usr/lib/go-"} {
		if strings.Contains(p, d) {
			return true
		}
	}
	// runtime internals, which come without a folder: <frozen importlib._bootstrap>,
	// node:internal/... Only at the start, and without a loose internal/: the internal/ folder of a
	// Go project belongs to the customer.
	for _, d := range []string{"<frozen ", "node:"} {
		if strings.HasPrefix(p, d) {
			return true
		}
	}
	// Python standard library: .../lib/python3.11/<module> (outside site-packages). With the minor
	// version: a project's lib/python3/ or lib/python_etl/ folder is not the library.
	return reLibPython.MatchString(p)
}

// ThirdPartyPath: the file p is from a public dependency or from the standard library, by the
// strict criterion (for the proxy: reading it does not teach the conversation memory). An installed
// package named with a dictionary word does not count: that is what a customer's internal package
// usually has, and not teaching what it brings would mean missing a customer name.
func (m *Masker) ThirdPartyPath(p string) bool {
	if pkg, ok := installedPackage(strings.ReplaceAll(p, "\\", "/")); ok && dictionaryWord(pkg) {
		return false
	}
	return m.thirdPartyPath(p)
}

// installedPackage: the package of p under site-packages, dist-packages or node_modules (the last
// occurrence: node_modules nests). An npm scope comes along: "@org/name".
func installedPackage(p string) (pkg string, ok bool) {
	for _, d := range []string{"/site-packages/", "/dist-packages/", "/node_modules/"} {
		if i := strings.LastIndex(p, d); i >= 0 {
			pkg, rest, _ := strings.Cut(p[i+len(d):], "/")
			if strings.HasPrefix(pkg, "@") {
				sub, _, _ := strings.Cut(rest, "/")
				pkg += "/" + sub
			}
			return strings.TrimSuffix(pkg, ".py"), true
		}
	}
	return "", false
}

// packageRoles: role names that a package or code folder usually has in any project. They say what
// the folder does, not whose it is: as an installed package name, it is almost always an internal
// package (there are popular repositories called "core" and "common", but that is not what is
// there).
var packageRoles = setOf("core", "common", "commons", "util", "utils", "shared", "lib", "libs", "config", "configs",
	"models", "helpers", "services", "domain", "infra", "backend", "frontend", "web", "site", "project", "package")

// dependencyPackage: the installed package is public software: a popular repository name or an
// Official Image (pandas, requests, express); in scoped npm, the public scope (@types, @aws-sdk). A
// role name (core, common, app, server) proves nothing.
func dependencyPackage(pkg string) bool {
	pkg = strings.ToLower(pkg)
	if strings.HasPrefix(pkg, "@") {
		esc, _, _ := strings.Cut(pkg[1:], "/")
		return publicNpmScopes[esc] || vendors[normSoftware(esc)]
	}
	if len(pkg) < 3 || packageRoles[pkg] || roleVocab[pkg] || typeVocab[pkg] || publicFolders[pkg] {
		return false
	}
	return publicSoftware[normSoftware(pkg)] || officialImages[pkg]
}

// publicDependency: the piece v of a path, after prev, starts third-party code (the public package
// in site-packages or node_modules, a public module in the Go, Maven or Cargo cache): from there on
// the path belongs to the vendor and the path readers stop. rest: what comes after v. Strict
// criterion (the path readers used to mask these folders): a package named with a dictionary word
// does not stop.
func publicDependency(prev, v, rest string) bool {
	switch strings.ToLower(prev) {
	case "site-packages", "dist-packages", "node_modules":
		return dependencyPackage(v) && !dictionaryWord(v)
	}
	switch {
	case prev == "pkg" && v == "mod":
		return publicGoModule(rest)
	case prev == ".m2" && v == "repository":
		return publicMavenGroup(rest)
	case prev == ".cargo" && v == "registry":
		return publicCargoRegistry(rest)
	}
	return false
}

// piecesAfter: the first n pieces of the path r (with / or \), each cut where the path ends (space,
// quotes, colon).
func piecesAfter(r string, n int) []string {
	if len(r) > 400 {
		r = r[:400]
	}
	ps := strings.SplitN(strings.TrimLeft(strings.ReplaceAll(r, "\\", "/"), "/"), "/", n+1)
	if len(ps) > n {
		ps = ps[:n]
	}
	for i, p := range ps {
		if k := strings.IndexAny(p, " \t\r\n\"':;,)"); k >= 0 {
			ps[i] = p[:k]
			return ps[:i+1]
		}
	}
	return ps
}

// Go module hosts with only open source (on hosting ones, github.com and the like, the module may
// be private: see publicGoModule)
var publicGoHosts = setOf("golang.org", "google.golang.org", "gopkg.in", "go.uber.org", "k8s.io", "sigs.k8s.io")

// publicGoModule: r, the path after pkg/mod/, is from a public module: on an open-source-only host,
// or github.com/<owner>/<repository> with both keys (vendor owner and popular repository). The
// cache writes an uppercase letter as "!x" and the version after "@".
func publicGoModule(r string) bool {
	ps := piecesAfter(strings.TrimPrefix(strings.TrimLeft(strings.ReplaceAll(r, "\\", "/"), "/"), "cache/download/"), 3)
	cleaned := func(x string) string {
		x, _, _ = strings.Cut(x, "@")
		return strings.ReplaceAll(strings.ToLower(x), "!", "")
	}
	if len(ps) == 0 {
		return false
	}
	h := cleaned(ps[0])
	if publicGoHosts[h] {
		return true
	}
	if (h == "github.com" || h == "gitlab.com" || h == "bitbucket.org") && len(ps) == 3 {
		return vendors[normSoftware(cleaned(ps[1]))] && publicSoftware[normSoftware(cleaned(ps[2]))]
	}
	return false
}

// publicMavenGroup: r, the path after .m2/repository/, starts with a public groupId
// (org/apache/commons/... -> org.apache.commons). Strict criterion (publicJavaGroup).
func publicMavenGroup(r string) bool {
	return publicJavaGroup(strings.ToLower(strings.Join(piecesAfter(r, 4), ".")), true)
}

// publicCargoRegistry: r, the path after .cargo/registry/, is from crates.io
// (src/index.crates.io-<hash>/, or the old index on GitHub); a private registry has another name.
func publicCargoRegistry(r string) bool {
	ps := piecesAfter(r, 2)
	return len(ps) == 2 && (strings.HasPrefix(ps[1], "index.crates.io-") || ps[1] == "github.com-1ecc6299db9ec823")
}

// publicGradleCache: r, the path after .gradle/caches/, is an artifact of a public groupId
// (modules-2/files-2.1/<group>/<artifact>/...).
func publicGradleCache(r string) bool {
	_, after, ok := strings.Cut(strings.ReplaceAll(r, "\\", "/"), "/files-2.1/")
	if !ok {
		return false
	}
	ps := piecesAfter(after, 1)
	return len(ps) == 1 && publicJavaGroup(strings.ToLower(ps[0]), true)
}

// publicJavaPackage: the package of a Java traceback line belongs to a vendor: a public Maven group
// (including whole-TLD ones: io.netty, io.grpc and dev.* are the libraries of almost every Java
// stack, and leaving their line in plain text is what already happened without this reader) or the
// platform (JDK, Kotlin, Scala).
func publicJavaPackage(pkg string) bool {
	pkg = strings.TrimSuffix(pkg, ".")
	if publicJavaGroup(pkg, false) {
		return true
	}
	for _, g := range []string{"sun.", "jdk.", "kotlin", "kotlinx", "scala.", "com.sun."} {
		if strings.HasPrefix(pkg, g) && (len(pkg) == len(g) || pkg[len(g)] == '.' || g[len(g)-1] == '.') {
			return true
		}
	}
	return false
}

// first pieces of a Java package that are a domain, not a name
var javaDomains = setOf("com", "br", "org", "net", "io", "gov", "edu", "co", "me", "dev", "app", "uk", "de", "fr", "es", "pt", "ar", "mx", "cl")

// clientName: a customer code identifier worth masking (looks like an identifier, outside the
// public vocabulary and the special __x__ names).
func clientName(v string) bool {
	return identifierShape(v) && !strings.HasPrefix(v, "__") && !publicDev(v) && !publicSQL(v)
}

func (m *Masker) findTraceback(s string, add func(ObjFinding)) {
	if !strings.Contains(s, "Traceback") && !strings.Contains(s, "\tat ") && !strings.Contains(s, "    at ") &&
		!strings.Contains(s, "goroutine ") {
		return
	}
	idents := func(a, b int, ent string) {
		for _, x := range reCodeIdent.FindAllStringIndex(s[a:b], -1) {
			if v := s[a+x[0] : a+x[1]]; clientName(v) {
				add(ObjFinding{a + x[0], a + x[1], ent, "traceback", false})
			}
		}
	}
	client := false   // the traceback has a customer frame (the final error is theirs)
	pyCode := false   // the next indented line is the code of a customer frame
	var goFunc [4]int // package and (receiver and) function of the Go frame, waiting for the path line
	pos := 0
	lines(s, func(l string) {
		a := pos
		pos += len(l) + 1
		switch {
		case pyCode && strings.HasPrefix(l, "    ") && !strings.HasPrefix(strings.TrimSpace(l), "File \""):
			pyCode = false
			idents(a, a+len(l), genericEnt)
			return
		case reFramePy.MatchString(l):
			pyCode = false
			mt := reFramePy.FindStringSubmatchIndex(l)
			if !m.thirdPartyPath(l[mt[2]:mt[3]]) {
				client, pyCode = true, true
				// the frame's file is the customer's module (carga_sinistros.py); main.py and
				// utils.py do not look like an identifier and stay
				p := l[mt[2]:mt[3]]
				if i := strings.LastIndexAny(p, "/\\") + 1; strings.HasSuffix(p, ".py") {
					if st := p[i : len(p)-3]; clientName(st) {
						add(ObjFinding{a + mt[2] + i, a + mt[2] + len(p) - 3, genericEnt, "traceback", false})
					}
				}
				if f := l[mt[4]:mt[5]]; clientName(f) {
					add(ObjFinding{a + mt[4], a + mt[5], "procedure", "traceback", true})
				}
			}
			return
		case reFrameJava.MatchString(l):
			mt := reFrameJava.FindStringSubmatchIndex(l)
			if pkg := l[mt[2]:mt[3]]; !publicJavaPackage(pkg) {
				client = true
				// each piece of the package that is neither a domain nor public vocabulary belongs
				// to the customer, even in lowercase (br.com.<empresa>.<sistema>)
				k := a + mt[2]
				for _, p := range strings.Split(strings.TrimSuffix(pkg, "."), ".") {
					if len(p) >= 2 && !javaDomains[p] && !publicDev(p) {
						add(ObjFinding{k, k + len(p), "pacote", "traceback", true})
					}
					k += len(p) + 1
				}
				idents(a+mt[4], a+mt[5], "pacote")
				if f := l[mt[6]:mt[7]]; strings.IndexByte(f, '.') > 0 {
					if st := f[:strings.IndexByte(f, '.')]; clientName(st) {
						add(ObjFinding{a + mt[6], a + mt[6] + len(st), "pacote", "traceback", false})
					}
				}
			}
			return
		case reFrameJS.MatchString(l):
			mt := reFrameJS.FindStringSubmatchIndex(l)
			if !m.thirdPartyPath(l[mt[4]:mt[5]]) {
				client = true
				if mt[2] >= 0 {
					idents(a+mt[2], a+mt[3], "procedure")
				}
			}
			return
		case reGoPath.MatchString(l):
			if mt := reGoPath.FindStringSubmatchIndex(l); goFunc[3] > 0 && !m.thirdPartyPath(l[mt[2]:mt[3]]) {
				client = true
				// the package of the customer frame: apart from the host, each piece that is not
				// public vocabulary is theirs (github.com/<empresa>/<sistema>/internal/<modulo>),
				// and so is the folder with the same name on the path line
				fromPackage := map[string]bool{}
				k := goFunc[0]
				for i, p := range strings.Split(s[goFunc[0]:goFunc[1]], "/") {
					if host := i == 0 && strings.IndexByte(p, '.') >= 0; !host && len(p) >= 2 && !publicDev(p) && !publicFolder(p) {
						add(ObjFinding{k, k + len(p), "pacote", "traceback", true})
						fromPackage[p] = true
					}
					k += len(p) + 1
				}
				k = a + mt[2]
				for _, p := range strings.Split(l[mt[2]:mt[3]], "/") {
					if fromPackage[p] {
						add(ObjFinding{k, k + len(p), "pacote", "traceback", true})
					}
					k += len(p) + 1
				}
				idents(goFunc[2], goFunc[3], "procedure")
			}
			goFunc = [4]int{}
			return
		case reFrameGo.MatchString(l) && strings.HasSuffix(strings.TrimSpace(l), ")"):
			mt := reFrameGo.FindStringSubmatchIndex(l)
			ini := mt[6]
			if mt[4] >= 0 {
				ini = mt[4] // the receiver type: (*CargaSinistros).calcular
			}
			goFunc = [4]int{a + mt[2], a + mt[3], a + ini, a + mt[7]}
			return
		}
		pyCode = false
		// the final error of a traceback with a customer frame: the cited value is their name
		if client {
			if mt := reFinalError.FindStringSubmatchIndex(strings.TrimSpace(l)); mt != nil {
				off := a + strings.Index(l, strings.TrimSpace(l))
				msg := strings.TrimSpace(l)[mt[4]:mt[5]]
				base := off + mt[4]
				for _, c := range reQuoted.FindAllStringSubmatchIndex(msg, -1) {
					i, j := c[2], c[3]
					if i < 0 {
						i, j = c[4], c[5]
					}
					if v := msg[i:j]; clientName(v) && !strings.ContainsAny(v, " /") {
						add(ObjFinding{base + i, base + j, genericEnt, "traceback", false})
					}
				}
				client = false
			}
		}
	})
}
