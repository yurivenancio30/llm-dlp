package mask

import (
	"regexp"
	"strings"
	"unicode"
)

// Prova de software (ver docs/pt-BR/estruturas.md, seção Público só com prova): o banco do DataHub se
// chama "datahub", o do Airflow "airflow". O nome padrão de um software público não diz nada do
// cliente, mas decidido como nome (a DSN .../datahub) a memória da conversa o espalhava por toda
// a conversa, inclusive onde é a ferramenta ("o datahub ingere metadados").
//
// A prova vem da própria conversa, como o compilador resolve um nome: a palavra está ligada a
// um pacote externo quando é instalada (pip/npm/brew/helm/go get... install), importada (import
// datahub, from datahub.x import, require('x'), import "github.com/org/x"), rodada como imagem
// pública (as duas chaves de caminhoPublico) ou executada como programa (RegistrarComando). Com a
// prova, a memória não espalha a palavra; onde um leitor a decide (a própria DSN), ela continua
// mascarada.
//
// Duas chaves, falha fechada: além da prova estrutural, a palavra tem de ser nome de software
// público (software_publico.txt ou Imagem Oficial do Docker). Palavra real do dicionário
// (palavras_comuns.txt e palavras_dicionario.txt: atlas, apollo, polaris, kraken, nexus) nunca
// recebe a prova: é o que um codinome de cliente costuma ser, e um pacote interno com esse nome
// é importado do mesmo jeito. Medido: dos nomes de repositório populares que são codinome
// típico, nenhum passa; datahub, airflow, superset, minio, trino, grafana passam.
//
// Sombreamento (como num compilador, a definição local esconde a global): se a conversa mostra
// o nome definido no projeto (name = "x" num manifesto, "name": "x" no package.json, module .../x
// no go.mod, uma pasta x/__init__.py fora de site-packages), ele é do dono e a prova não vale.
// O sombreamento só bloqueia: casar a mais nunca libera nada.
//
// Termo cadastrado sempre vence. Lista desatualizada só deixa de provar (a palavra continua
// mascarada), nunca libera.
//
// Custo: uma passada por linha; só a linha com a forma certa no começo (from, import, aspas,
// image, FROM, name, module) ou com a palavra de instalação passa por regex.

var (
	reInstalar = regexp.MustCompile(`\b(?:pip3?|pipx|uv(?:\s+pip)?|poetry|conda|mamba|npm|pnpm|yarn|bun|brew|apt(?:-get)?|apk|dnf|yum|gem|cargo|go|helm|choco|winget|snap)` +
		`(?:\s+-{1,2}[\w-]+(?:=\S+)?)*\s+(?:install|add|get|i)\b([^;&|#]*)`)
	reImportPy = regexp.MustCompile(`^(?:from[ \t]+([A-Za-z_]\w*)(?:\.\w+)*[ \t]+import\b|import[ \t]+([A-Za-z_]\w*(?:[ \t]*,[ \t]*[A-Za-z_]\w*)*))`)
	reImportJS = regexp.MustCompile(`(?:\brequire\(\s*|\bfrom\s+|^import\s+)['"](@?[\w.-]+)(?:/([\w.-]+))?[^'"]*['"]`)
	reImportGo = regexp.MustCompile(`"(?:[a-z0-9-]+\.)+[a-z]{2,}/([\w.-]+)/([\w.-]+)[^"]*"`)

	reDefManifesto = regexp.MustCompile(`^(?:name[ \t]*=[ \t]*|"name"[ \t]*:[ \t]*|setup\([^)]*\bname[ \t]*=[ \t]*)["']@?(?:[\w.-]+/)?([\w.-]+)["']|^module[ \t]+\S*?([\w.-]+)[ \t]*$`)
	reDefPacote    = regexp.MustCompile(`(?:^|[\s/"'(])([\w.-]+)/__init__\.py`)
)

// linhas: chama f com cada linha de s (sem a quebra), sem alocar.
func linhas(s string, f func(l string)) {
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			f(s)
			return
		}
		f(s[:i])
		s = s[i+1:]
	}
}

// provasSoftware: as palavras que s liga a um pacote externo (ainda sem a segunda chave).
func provasSoftware(s string) []string {
	var out []string
	add := func(vs ...string) {
		for _, v := range vs {
			if v = strings.ToLower(strings.Trim(v, `"'`)); len(v) >= 3 {
				out = append(out, v)
			}
		}
	}
	linhas(s, func(l string) {
		t := strings.TrimLeft(l, " \t")
		if t == "" {
			return
		}
		// pacote instalado: o nome sem versão, extras, escopo e registro; pedaços só com o nome
		// inteiro público (apache-airflow; acme-airflow não prova "airflow")
		if strings.Contains(t, "install") || strings.Contains(t, " add ") || strings.Contains(t, " get ") || strings.Contains(t, " i ") {
			for _, m := range reInstalar.FindAllStringSubmatch(t, -1) {
				for _, x := range strings.Fields(m[1]) {
					if strings.HasPrefix(x, "-") || strings.ContainsAny(x, "$<>{}") || extensoesArquivo[strings.ToLower(x[strings.LastIndexByte(x, '.')+1:])] {
						continue
					}
					x = strings.Trim(x, `"'`)
					if i := strings.IndexAny(x, "[=<>~!@:"); i > 0 {
						x = x[:i]
					}
					ps := strings.Split(strings.TrimPrefix(x, "@"), "/")
					nome := ps[len(ps)-1]
					add(nome)
					if len(ps) > 1 {
						add(ps[len(ps)-2]) // org/nome (helm repo/chart, npm @org/nome, go host/org/nome)
					}
					if nomePublico(nome) {
						add(pedacosValor(nome)...)
					}
				}
			}
		}
		if strings.HasPrefix(t, "from ") || strings.HasPrefix(t, "import ") {
			for _, m := range reImportPy.FindAllStringSubmatch(t, -1) {
				add(m[1])
				for _, x := range strings.Split(m[2], ",") {
					add(strings.TrimSpace(x))
				}
			}
		}
		// Go: import "host/org/nome", com ou sem apelido, dentro ou fora do bloco
		if q := strings.IndexByte(t, '"'); q >= 0 && strings.Count(t, "/") >= 2 && (q == 0 || strings.HasPrefix(t, "import ") || caraDeApelido(t[:q])) {
			for _, m := range reImportGo.FindAllStringSubmatch(t, -1) {
				add(m[1], m[2])
			}
		}
		if strings.Contains(t, "require(") || strings.Contains(t, "from '") || strings.Contains(t, `from "`) || strings.HasPrefix(t, "import ") {
			for _, m := range reImportJS.FindAllStringSubmatch(t, -1) {
				if !strings.HasPrefix(m[1], ".") {
					add(strings.TrimPrefix(m[1], "@"), m[2])
				}
			}
		}
		// imagem pública com as duas chaves (redis, bitnami/redis, acryldata/datahub-gms)
		if strings.Contains(t, "image") || strings.HasPrefix(t, "FROM") {
			for _, m := range reImagemQualquer.FindAllStringSubmatch(t, -1) {
				ref, _, _ := strings.Cut(m[1], "@")
				ps := strings.Split(ref, "/")
				if len(ps) > 1 && strings.ContainsAny(ps[0], ".:") {
					if !registrosPublicos[strings.ToLower(ps[0])] {
						continue
					}
					ps = ps[1:]
				}
				ps[len(ps)-1], _, _ = strings.Cut(ps[len(ps)-1], ":")
				nome := ps[len(ps)-1]
				if len(ps) == 1 && imagensOficiais[strings.ToLower(nome)] || len(ps) == 2 && orgPublica(ps[0]) && nomePublico(nome) {
					add(nome)
					add(pedacosValor(nome)...)
				}
			}
		}
	})
	return out
}

// caraDeApelido: o que vem antes da aspa num import de Go com apelido ("es ", "_ ", ". ").
func caraDeApelido(p string) bool {
	p = strings.TrimSpace(p)
	if p == "_" || p == "." {
		return true
	}
	for i := 0; i < len(p); i++ {
		if !(ehAlnum(p[i]) || p[i] == '_') {
			return false
		}
	}
	return p != ""
}

// definicoesLocais: os nomes que s define no próprio projeto (ver Sombreamento acima).
func definicoesLocais(s string) []string {
	var out []string
	linhas(s, func(l string) {
		t := strings.TrimLeft(l, " \t")
		if strings.HasPrefix(t, "name") || strings.HasPrefix(t, `"name"`) || strings.HasPrefix(t, "module ") || strings.HasPrefix(t, "setup(") {
			for _, m := range reDefManifesto.FindAllStringSubmatch(strings.TrimRight(t, "\r,"), -1) {
				out = append(out, strings.ToLower(m[1]+m[2]))
			}
		}
		if strings.Contains(t, "__init__.py") {
			for _, m := range reDefPacote.FindAllStringSubmatchIndex(t, -1) {
				ini := strings.LastIndexAny(t[:m[2]], " \t\"'(") + 1
				if c := t[ini:m[2]]; !strings.Contains(c, "site-packages") && !strings.Contains(c, "dist-packages") {
					out = append(out, strings.ToLower(t[m[2]:m[3]]))
				}
			}
		}
	})
	return out
}

// palavraDicionario: v é palavra real de um dos idiomas (as 50000 mais frequentes).
func palavraDicionario(v string) bool {
	return palavraComum(v) || palavrasDicionario[strings.ToLower(v)]
}

// registrarSoftware: as provas de s que passam na segunda chave entram no conjunto do Masker, e
// as definições locais de s, no conjunto que as anula (fato sobre o mundo e sobre o projeto:
// vale para qualquer conversa, só em RAM).
//
// Os dois conjuntos são limitados pela lista de software (só entra nome que poderia ser provado
// ou passar por dependência pública: uma definição local de outro nome não muda nada) e nunca
// guardam nome do cliente. O sombreamento é guardado sem - _ . (normSoftware): o manifesto
// escreve "vendas-core" e a pasta do pacote é vendas_core.
func (m *Masker) registrarSoftware(s string) {
	for _, v := range definicoesLocais(s) {
		if provavel(v) || pacoteDependencia(v) {
			m.softLocal.Store(normSoftware(v), true)
		}
	}
	for _, v := range provasSoftware(s) {
		m.provar(v)
	}
}

// provavel: v pode receber a prova (segunda chave e não é palavra real). Número puro não é nome
// de software, mesmo havendo repositório com esse nome (2048, 12306).
func provavel(v string) bool {
	return len(v) >= 3 && strings.IndexFunc(v, unicode.IsLetter) >= 0 && !palavraDicionario(v) &&
		(softwarePublico[normSoftware(v)] || imagensOficiais[v])
}

// provar: v entra como software provado se pode receber a prova.
func (m *Masker) provar(v string) {
	if provavel(v) {
		m.soft.Store(v, true)
	}
}

// softwareProvado: v (palavra inteira, qualquer caixa) é software público com prova estrutural,
// não é definido no projeto (sombreamento) e não contém termo cadastrado.
func (m *Masker) softwareProvado(v string) bool {
	l := strings.ToLower(v)
	if _, ok := m.soft.Load(l); !ok {
		return false
	}
	_, local := m.softLocal.Load(normSoftware(l))
	return !local && !m.temTermo(v)
}

var reTrechoComando = regexp.MustCompile(`\|\||&&|[|;\n&()]`)

// programas que embrulham outro (o programa de verdade vem depois): sudo airflow ..., python -m
// datahub ..., npx superset ...
var embrulhos = conj("sudo", "time", "nohup", "env", "exec", "nice", "timeout", "xargs", "watch", "npx", "uvx", "bunx", "pipx", "run", "poetry", "pdm", "uv", "command")

// RegistrarComando: o programa que um comando de shell executa (o primeiro nome de cada trecho
// do pipeline, "airflow dags list", "datahub check ...", "python -m dagster ...") é prova de
// software, com as mesmas duas chaves de registrarSoftware. O proxy chama com a entrada de cada
// chamada da ferramenta de shell (só ele sabe que o texto é um comando).
func (m *Masker) RegistrarComando(cmd string) {
	for _, seg := range reTrechoComando.Split(cmd, -1) {
		ts := strings.Fields(seg)
		for i := 0; i < len(ts); i++ {
			t := ts[i]
			switch {
			case strings.Contains(t, "=") && !strings.HasPrefix(t, "-"), strings.HasPrefix(t, "-"), embrulhos[t], len(t) > 0 && t[0] >= '0' && t[0] <= '9':
				continue // VAR=x, opção, embrulho, número (timeout 30)
			case (t == "python" || t == "python3") && i+2 < len(ts) && ts[i+1] == "-m":
				t = ts[i+2]
			}
			if j := strings.LastIndexByte(t, '/'); j >= 0 {
				t = t[j+1:]
			}
			m.provar(strings.ToLower(strings.Trim(t, "\"'")))
			break
		}
	}
}
