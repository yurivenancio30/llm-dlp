package mask

import (
	"regexp"
	"strings"
)

// Leitor de traceback (ver docs/estruturas.md, seção Público só com prova). Num erro, cada linha
// do traceback diz de onde veio, como os caminhos de include de um compilador separam o cabeçalho
// do sistema do cabeçalho do projeto: a linha que aponta para uma dependência ou para a
// biblioteca padrão (site-packages, node_modules, go/pkg/mod, .m2, /usr/lib/python3.x, o runtime
// do Go) é código do fornecedor e fica; a que aponta para outro lugar é código do cliente: a
// função, os identificadores da linha de código e, no fim, o valor citado no erro (KeyError:
// 'COD_APOLICE') são nomes dele. As pastas do caminho ficam com os leitores de caminho.
//
// Formatos: Python (File "x", line N, in f + a linha de código), Java/Kotlin/Scala
// (at pacote.Classe.metodo(Arquivo.java:N)), JavaScript (at f (caminho:L:C)) e Go
// (pacote.Funcao(...) + a linha com o caminho).
//
// Estar numa pasta de dependências não basta (ver caminhoTerceiro): o pacote instalado tem de
// ser software público (software_publico.txt, Imagem Oficial) e não ser definido no projeto
// (sombreamento); o módulo no cache do Go, do Maven, do Gradle ou do Cargo tem de ser de dono
// ou grupo público. Um pacote interno do cliente instalado no ambiente virtual, ou um módulo
// privado baixado para o cache, continua sendo do cliente.

var (
	reFramePy   = regexp.MustCompile(`^\s*File "([^"\n]+)", line \d+, in ([\w<>.]+)`)
	reFrameJava = regexp.MustCompile(`^\s*at ((?:[\w$]+\.)+)([\w$<>]+)\(([\w$.-]*)(?::\d+)?\)`)
	reFrameJS   = regexp.MustCompile(`^\s*at (?:(?:async )?([\w$.<>\[\] ]+?) \()?((?:[A-Za-z]:)?[^():\s]+):\d+:\d+\)?\s*$`)
	reFrameGo   = regexp.MustCompile(`^((?:[\w.-]+/)*[\w.-]+)\.(\(?\*?[\w]+\)?\.)?([\w]+)\(`)
	reCaminhoGo = regexp.MustCompile(`^\t(/[^:\s]+\.go):\d+`)
	reErroFinal = regexp.MustCompile(`^([\w.]+(?:Error|Exception|Exit|Interrupt|Warning)|panic):\s*(.*)$`)
	reIdentCod  = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	reLibPython = regexp.MustCompile(`/lib(?:64)?/python\d+\.\d+/`)
	reCitado    = regexp.MustCompile(`'([^'\n]{2,80})'|"([^"\n]{2,80})"`)
)

// caminhoTerceiro: o arquivo p (de um frame ou de uma leitura) é de dependência pública ou da
// biblioteca padrão. Estar numa pasta de dependências não basta: o pacote tem de ser público
// (um pacote interno do cliente é instalado e baixado para as mesmas pastas).
func (m *Masker) caminhoTerceiro(p string) bool {
	p = strings.ReplaceAll(p, "\\", "/")
	if pkg, ok := pacoteInstalado(p); ok {
		_, local := m.softLocal.Load(normSoftware(pkg))
		return !local && pacoteDependencia(pkg)
	}
	for _, c := range []struct {
		pasta   string
		publico func(resto string) bool
	}{{"/pkg/mod/", moduloGoPublico}, {"/.m2/repository/", grupoMavenPublico}, {"/.cargo/registry/", registroCargoPublico},
		{"/.gradle/caches/", cacheGradlePublico}} {
		if i := strings.Index(p, c.pasta); i >= 0 {
			return c.publico(p[i+len(c.pasta):])
		}
	}
	// a JVM e o GOROOT nos lugares de instalação conhecidos
	for _, d := range []string{"/usr/lib/jvm/", "/usr/local/go/src/", "/usr/lib/go/", "/usr/lib/go-"} {
		if strings.Contains(p, d) {
			return true
		}
	}
	// internos do runtime, que vêm sem pasta: <frozen importlib._bootstrap>, node:internal/...
	// Só no começo, e sem o internal/ solto: a pasta internal/ de um projeto Go é do cliente.
	for _, d := range []string{"<frozen ", "node:"} {
		if strings.HasPrefix(p, d) {
			return true
		}
	}
	// biblioteca padrão do Python: .../lib/python3.11/<módulo> (fora de site-packages). Com a
	// versão menor: uma pasta lib/python3/ ou lib/python_etl/ de projeto não é a biblioteca.
	return reLibPython.MatchString(p)
}

// CaminhoTerceiro: o arquivo p é de dependência pública ou da biblioteca padrão, pelo critério
// estrito (para o proxy: a leitura dele não ensina a memória da conversa). Pacote instalado com
// nome de palavra de dicionário não conta: é o que um pacote interno do cliente costuma ter, e
// deixar de ensinar o que ele traz seria perder nome do cliente.
func (m *Masker) CaminhoTerceiro(p string) bool {
	if pkg, ok := pacoteInstalado(strings.ReplaceAll(p, "\\", "/")); ok && palavraDicionario(pkg) {
		return false
	}
	return m.caminhoTerceiro(p)
}

// pacoteInstalado: o pacote de p debaixo de site-packages, dist-packages ou node_modules (a
// última ocorrência: node_modules aninha). Escopo do npm vem junto: "@org/nome".
func pacoteInstalado(p string) (pkg string, ok bool) {
	for _, d := range []string{"/site-packages/", "/dist-packages/", "/node_modules/"} {
		if i := strings.LastIndex(p, d); i >= 0 {
			pkg, resto, _ := strings.Cut(p[i+len(d):], "/")
			if strings.HasPrefix(pkg, "@") {
				sub, _, _ := strings.Cut(resto, "/")
				pkg += "/" + sub
			}
			return strings.TrimSuffix(pkg, ".py"), true
		}
	}
	return "", false
}

// papeisPacote: nomes de papel que um pacote ou uma pasta de código costuma ter em qualquer
// projeto. Dizem o que a pasta faz, não de quem é: como nome de pacote instalado, é quase sempre
// pacote interno (há repositório popular chamado "core" e "common", mas não é ele que está lá).
var papeisPacote = conj("core", "common", "commons", "util", "utils", "shared", "lib", "libs", "config", "configs",
	"models", "helpers", "services", "domain", "infra", "backend", "frontend", "web", "site", "project", "package")

// pacoteDependencia: o pacote instalado é software público: nome de repositório popular ou
// Imagem Oficial (pandas, requests, express); no npm com escopo, o escopo público (@types,
// @aws-sdk). Nome de papel (core, common, app, server) não prova nada.
func pacoteDependencia(pkg string) bool {
	pkg = strings.ToLower(pkg)
	if strings.HasPrefix(pkg, "@") {
		esc, _, _ := strings.Cut(pkg[1:], "/")
		return escoposNpmPublicos[esc] || fornecedores[normSoftware(esc)]
	}
	if len(pkg) < 3 || papeisPacote[pkg] || vocabPapel[pkg] || vocabTipo[pkg] || pastasPublicas[pkg] {
		return false
	}
	return softwarePublico[normSoftware(pkg)] || imagensOficiais[pkg]
}

// dependenciaPublica: o pedaço v de um caminho, depois de ant, começa código de terceiro (o
// pacote público em site-packages ou node_modules, um módulo público no cache do Go, do Maven
// ou do Cargo): dali para a frente o caminho é do fornecedor e os leitores de caminho param.
// resto: o que vem depois de v. Critério estrito (os leitores de caminho mascaravam essas
// pastas antes): pacote com nome de palavra de dicionário não para.
func dependenciaPublica(ant, v, resto string) bool {
	switch strings.ToLower(ant) {
	case "site-packages", "dist-packages", "node_modules":
		return pacoteDependencia(v) && !palavraDicionario(v)
	}
	switch {
	case ant == "pkg" && v == "mod":
		return moduloGoPublico(resto)
	case ant == ".m2" && v == "repository":
		return grupoMavenPublico(resto)
	case ant == ".cargo" && v == "registry":
		return registroCargoPublico(resto)
	}
	return false
}

// pedacosDepois: os primeiros n pedaços do caminho r (com / ou \), cada um cortado onde o
// caminho acaba (espaço, aspas, dois-pontos).
func pedacosDepois(r string, n int) []string {
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

// hosts de módulo Go em que só há código aberto (nos de hospedagem, github.com e afins, o
// módulo pode ser privado: ver moduloGoPublico)
var hostsGoPublicos = conj("golang.org", "google.golang.org", "gopkg.in", "go.uber.org", "k8s.io", "sigs.k8s.io")

// moduloGoPublico: r, o caminho depois de pkg/mod/, é de um módulo público: num host só de
// código aberto, ou github.com/<dono>/<repositório> com as duas chaves (dono fornecedor e
// repositório popular). O cache escreve maiúscula como "!x" e a versão depois de "@".
func moduloGoPublico(r string) bool {
	ps := pedacosDepois(strings.TrimPrefix(strings.TrimLeft(strings.ReplaceAll(r, "\\", "/"), "/"), "cache/download/"), 3)
	limpo := func(x string) string {
		x, _, _ = strings.Cut(x, "@")
		return strings.ReplaceAll(strings.ToLower(x), "!", "")
	}
	if len(ps) == 0 {
		return false
	}
	h := limpo(ps[0])
	if hostsGoPublicos[h] {
		return true
	}
	if (h == "github.com" || h == "gitlab.com" || h == "bitbucket.org") && len(ps) == 3 {
		return fornecedores[normSoftware(limpo(ps[1]))] && softwarePublico[normSoftware(limpo(ps[2]))]
	}
	return false
}

// grupoMavenPublico: r, o caminho depois de .m2/repository/, começa por um groupId público
// (org/apache/commons/... -> org.apache.commons). Critério estrito (grupoJavaPublico).
func grupoMavenPublico(r string) bool {
	return grupoJavaPublico(strings.ToLower(strings.Join(pedacosDepois(r, 4), ".")), true)
}

// registroCargoPublico: r, o caminho depois de .cargo/registry/, é do crates.io
// (src/index.crates.io-<hash>/, ou o índice antigo no GitHub); registro privado tem outro nome.
func registroCargoPublico(r string) bool {
	ps := pedacosDepois(r, 2)
	return len(ps) == 2 && (strings.HasPrefix(ps[1], "index.crates.io-") || ps[1] == "github.com-1ecc6299db9ec823")
}

// cacheGradlePublico: r, o caminho depois de .gradle/caches/, é um artefato de groupId público
// (modules-2/files-2.1/<grupo>/<artefato>/...).
func cacheGradlePublico(r string) bool {
	_, depois, ok := strings.Cut(strings.ReplaceAll(r, "\\", "/"), "/files-2.1/")
	if !ok {
		return false
	}
	ps := pedacosDepois(depois, 1)
	return len(ps) == 1 && grupoJavaPublico(strings.ToLower(ps[0]), true)
}

// pacoteJavaPublico: o pacote de uma linha de traceback Java é de fornecedor: um grupo público
// do Maven (com os de TLD inteiro: io.netty, io.grpc e dev.* são as bibliotecas de quase toda
// pilha Java, e deixar a linha delas em claro é o que já acontecia sem este leitor) ou a
// plataforma (JDK, Kotlin, Scala).
func pacoteJavaPublico(pkg string) bool {
	pkg = strings.TrimSuffix(pkg, ".")
	if grupoJavaPublico(pkg, false) {
		return true
	}
	for _, g := range []string{"sun.", "jdk.", "kotlin", "kotlinx", "scala.", "com.sun."} {
		if strings.HasPrefix(pkg, g) && (len(pkg) == len(g) || pkg[len(g)] == '.' || g[len(g)-1] == '.') {
			return true
		}
	}
	return false
}

// primeiros pedaços de pacote Java que são domínio, não nome
var dominiosJava = conj("com", "br", "org", "net", "io", "gov", "edu", "co", "me", "dev", "app", "uk", "de", "fr", "es", "pt", "ar", "mx", "cl")

// nomeDoCliente: um identificador de código do cliente que vale mascarar (cara de identificador,
// fora do vocabulário público e dos nomes especiais __x__).
func nomeDoCliente(v string) bool {
	return caraDeIdentificador(v) && !strings.HasPrefix(v, "__") && !publicoDev(v) && !publicoSQL(v)
}

func (m *Masker) acharTraceback(s string, add func(ObjAchado)) {
	if !strings.Contains(s, "Traceback") && !strings.Contains(s, "\tat ") && !strings.Contains(s, "    at ") &&
		!strings.Contains(s, "goroutine ") {
		return
	}
	idents := func(a, b int, ent string) {
		for _, x := range reIdentCod.FindAllStringIndex(s[a:b], -1) {
			if v := s[a+x[0] : a+x[1]]; nomeDoCliente(v) {
				add(ObjAchado{a + x[0], a + x[1], ent, "traceback", false})
			}
		}
	}
	cliente := false  // o traceback tem frame do cliente (o erro final é dele)
	codPy := false    // a próxima linha recuada é o código de um frame do cliente
	var goFunc [4]int // pacote e (receptor e) função do frame Go, à espera da linha do caminho
	pos := 0
	linhas(s, func(l string) {
		a := pos
		pos += len(l) + 1
		switch {
		case codPy && strings.HasPrefix(l, "    ") && !strings.HasPrefix(strings.TrimSpace(l), "File \""):
			codPy = false
			idents(a, a+len(l), entGenerica)
			return
		case reFramePy.MatchString(l):
			codPy = false
			mt := reFramePy.FindStringSubmatchIndex(l)
			if !m.caminhoTerceiro(l[mt[2]:mt[3]]) {
				cliente, codPy = true, true
				// o arquivo do frame é o módulo do cliente (carga_sinistros.py); main.py e
				// utils.py não têm cara de identificador e ficam
				p := l[mt[2]:mt[3]]
				if i := strings.LastIndexAny(p, "/\\") + 1; strings.HasSuffix(p, ".py") {
					if st := p[i : len(p)-3]; nomeDoCliente(st) {
						add(ObjAchado{a + mt[2] + i, a + mt[2] + len(p) - 3, entGenerica, "traceback", false})
					}
				}
				if f := l[mt[4]:mt[5]]; nomeDoCliente(f) {
					add(ObjAchado{a + mt[4], a + mt[5], "procedure", "traceback", true})
				}
			}
			return
		case reFrameJava.MatchString(l):
			mt := reFrameJava.FindStringSubmatchIndex(l)
			if pkg := l[mt[2]:mt[3]]; !pacoteJavaPublico(pkg) {
				cliente = true
				// cada pedaço do pacote que não é domínio nem vocabulário público é do cliente,
				// inclusive em minúsculas (br.com.<empresa>.<sistema>)
				k := a + mt[2]
				for _, p := range strings.Split(strings.TrimSuffix(pkg, "."), ".") {
					if len(p) >= 2 && !dominiosJava[p] && !publicoDev(p) {
						add(ObjAchado{k, k + len(p), "pacote", "traceback", true})
					}
					k += len(p) + 1
				}
				idents(a+mt[4], a+mt[5], "pacote")
				if f := l[mt[6]:mt[7]]; strings.IndexByte(f, '.') > 0 {
					if st := f[:strings.IndexByte(f, '.')]; nomeDoCliente(st) {
						add(ObjAchado{a + mt[6], a + mt[6] + len(st), "pacote", "traceback", false})
					}
				}
			}
			return
		case reFrameJS.MatchString(l):
			mt := reFrameJS.FindStringSubmatchIndex(l)
			if !m.caminhoTerceiro(l[mt[4]:mt[5]]) {
				cliente = true
				if mt[2] >= 0 {
					idents(a+mt[2], a+mt[3], "procedure")
				}
			}
			return
		case reCaminhoGo.MatchString(l):
			if mt := reCaminhoGo.FindStringSubmatchIndex(l); goFunc[3] > 0 && !m.caminhoTerceiro(l[mt[2]:mt[3]]) {
				cliente = true
				// o pacote do frame do cliente: fora o host, cada pedaço que não é vocabulário
				// público é dele (github.com/<empresa>/<sistema>/internal/<módulo>), e a pasta
				// de mesmo nome na linha do caminho também
				doPacote := map[string]bool{}
				k := goFunc[0]
				for i, p := range strings.Split(s[goFunc[0]:goFunc[1]], "/") {
					if host := i == 0 && strings.IndexByte(p, '.') >= 0; !host && len(p) >= 2 && !publicoDev(p) && !pastaPublica(p) {
						add(ObjAchado{k, k + len(p), "pacote", "traceback", true})
						doPacote[p] = true
					}
					k += len(p) + 1
				}
				k = a + mt[2]
				for _, p := range strings.Split(l[mt[2]:mt[3]], "/") {
					if doPacote[p] {
						add(ObjAchado{k, k + len(p), "pacote", "traceback", true})
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
				ini = mt[4] // o tipo do receptor: (*CargaSinistros).calcular
			}
			goFunc = [4]int{a + mt[2], a + mt[3], a + ini, a + mt[7]}
			return
		}
		codPy = false
		// o erro final de um traceback com frame do cliente: o valor citado é nome dele
		if cliente {
			if mt := reErroFinal.FindStringSubmatchIndex(strings.TrimSpace(l)); mt != nil {
				off := a + strings.Index(l, strings.TrimSpace(l))
				msg := strings.TrimSpace(l)[mt[4]:mt[5]]
				base := off + mt[4]
				for _, c := range reCitado.FindAllStringSubmatchIndex(msg, -1) {
					i, j := c[2], c[3]
					if i < 0 {
						i, j = c[4], c[5]
					}
					if v := msg[i:j]; nomeDoCliente(v) && !strings.ContainsAny(v, " /") {
						add(ObjAchado{base + i, base + j, entGenerica, "traceback", false})
					}
				}
				cliente = false
			}
		}
	})
}
