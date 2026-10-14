package mask

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// O que o proxy sabe de uma chamada de ferramenta (ver docs/pt-BR/estruturas.md, seções
// Proveniência, Identidade e Eco). Nenhuma regra é por ferramenta: tudo vem da gramática do
// pedido (verbo de enumeração + substantivo), da proveniência das palavras (o programa que o
// modelo escreveu x os dados que a saída trouxe) e da ordem da conversa (eco).
//
// O proxy percorre a conversa em ordem (proxy/requisicao.go, commandHints):
//   - ArgumentText(cmd), antes de tudo, para cada tool_use: as palavras em posição de argumento;
//   - User(texto): a última mensagem do usuário (B6: "lista os grupos..." dá o tipo);
//   - Usage(cmd, traduzidas): o tool_use; devolve a Call (extensão da dica da entrada);
//   - Result(ch, saida): o tool_result; devolve a extensão da dica da saída;
//   - EndTurn(): fim de uma mensagem do assistente (o pedido do usuário vale só para as
//     chamadas da primeira resposta depois dele).
//
// A extensão vai depois de sepExt na dica (decisao.go) e entra na chave do memo: é compacta e
// determinística (hashes curtos das palavras, em ordem). Format:
//
//	s;t=<tipo>;p=<h>,<h>...;x=<tipo>:<h>,...      (saída de comando)
//	u;e=<tipo>:<h>,...;x=<tipo>:<h>,...           (entrada do tool_use)
//
// t: o tipo pedido (verbo de enumeração do comando, ou da mensagem do usuário); p: as palavras
// do programa; x: as palavras que o proxy traduziu de um pseudônimo nesse tool_use (B1: são
// nome, nunca programa); e: as palavras do tool_use que ecoam uma saída anterior (B4). h é o
// FNV-1a de 32 bits da palavra em minúsculas (ASCII), em hexadecimal.

// genericEnt: o tipo de um nome cujo substantivo não casa com nenhuma palavra de tipo de
// ObjectPrefixes (list_findings, branches). Não é chave de ObjectPrefixes de propósito: pseudoObject usa
// então o prefixo genérico "OBJ" (o mesmo de sempre); objMask vale (ligado por padrão,
// desligável em objetos.mascarar.objeto) e objPropagates é falso (não está em defaultPropagates): o
// nome é mascarado onde é decidido e pela memória da conversa, mas nunca aprendido.
const genericEnt = "objeto"

// conversationCmd: o estado de Commands para as regras desta seção.
type conversationCmd struct {
	args     map[string]bool   // palavras em posição de argumento em algum tool_use
	seenOnes map[string]string // argumento -> tipo pedido da saída em que apareceu ("" = sem tipo)
	request  string            // o tipo pedido pela última mensagem do usuário ("" = nada)
	hasPed   bool
	usedPed  bool // já houve chamada neste turno
	nOutputs int
	nScanned int // bytes de saída varridos (teto)
	// scripts: o texto dos tool_use pelo nome de arquivo que citam. Um script escrito numa
	// chamada e executado noutra ("python3 scripts/x.py") é programa da segunda também.
	scripts map[string]string
}

// Call: um tool_use, com o que se sabe dele.
type Call struct {
	typ      string
	prog     map[uint32]bool
	trans    []TranslatedWord
	echoes   []TranslatedWord
	usageExt string
}

// TranslatedWord: uma palavra que o proxy pôs no texto ao traduzir um pseudônimo (Name) e o
// tipo do pseudônimo.
type TranslatedWord struct{ Name, Ent string }

// UsageExt: a extensão da dica para as strings da entrada do tool_use ("" = nada).
func (ch *Call) UsageExt() string {
	if ch == nil {
		return ""
	}
	return ch.usageExt
}

// WithExtension: a dica do leitor de tabela (codificar) com a extensão.
func WithExtension(table, ext string) string {
	if ext == "" {
		return table
	}
	return table + sepExt + ext
}

const (
	maxArgs       = 4096
	maxScanOutput = 64 << 20 // bytes de saída varridos para o eco, por requisição
)

// ArgumentText: registra as palavras em posição de argumento de um tool_use (antes do percurso
// em ordem, para a varredura das saídas procurar só por elas).
func (c *Commands) ArgumentText(cmd string) {
	if c.cv.args == nil {
		c.cv.args = map[string]bool{}
	}
	arguments(limitTo(cmd), func(p, _ string) {
		if len(c.cv.args) < maxArgs {
			c.cv.args[p] = true
		}
	})
}

// User: uma mensagem do usuário. O tipo que ela pede vale para as chamadas da resposta
// seguinte.
func (c *Commands) User(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	c.cv.request, c.cv.hasPed = typeInProse(text)
	c.cv.usedPed = false
}

// EndTurn: acabou uma mensagem do assistente. Se ela teve chamadas, o pedido do usuário já foi
// usado.
func (c *Commands) EndTurn() {
	if c.cv.usedPed {
		c.cv.request, c.cv.hasPed, c.cv.usedPed = "", false, false
	}
}

// Usage: um tool_use. trad: as palavras que o proxy traduziu de um pseudônimo na entrada dele.
func (c *Commands) Usage(cmd string, trans []TranslatedWord) *Call {
	cmd = limitTo(cmd)
	ch := &Call{trans: trans}
	if e, ok := commandType(cmd); ok {
		ch.typ = e
	} else if c.cv.hasPed {
		ch.typ = c.cv.request
	}
	c.cv.usedPed = true
	transl := map[uint32]bool{}
	for _, t := range trans {
		transl[hpal(t.Name)] = true
	}
	ch.prog = map[uint32]bool{}
	prog := c.fullProgram(cmd)
	programWords(prog, func(p string) {
		if h := hpal(p); !transl[h] {
			ch.prog[h] = true
		}
	})
	// eco: argumento que apareceu numa saída anterior
	seen := map[string]bool{}
	arguments(cmd, func(p, ent string) {
		outputType, ok := c.cv.seenOnes[p]
		if !ok || seen[p] || transl[hpal(p)] || !decidableWord(p) {
			return
		}
		seen[p] = true
		if ent == "" {
			ent = outputType
		}
		if ent == "" {
			ent = genericEnt
		}
		ch.echoes = append(ch.echoes, TranslatedWord{p, ent})
	})
	if len(ch.echoes) > 0 || len(trans) > 0 {
		var b strings.Builder
		b.WriteString("u")
		writePairs(&b, "e", ch.echoes)
		writePairs(&b, "x", trans)
		ch.usageExt = b.String()
	}
	return ch
}

// Result: a saída de uma chamada. Devolve a extensão da dica da saída. Também marca os
// argumentos de tool_use que aparecem nela (para o eco das chamadas seguintes).
func (c *Commands) Result(ch *Call, output string) string {
	typ := ""
	if ch != nil {
		typ = ch.typ
	}
	if len(c.cv.args) > 0 && c.cv.nScanned < maxScanOutput {
		c.cv.nScanned += len(output)
		if c.cv.seenOnes == nil {
			c.cv.seenOnes = map[string]string{}
		}
		scanWords(output, func(a, b int) {
			p := output[a:b]
			if c.cv.args[p] {
				if _, ok := c.cv.seenOnes[p]; !ok {
					c.cv.seenOnes[strings.Clone(p)] = typ
				}
			}
		})
	}
	if ch == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("s")
	if ch.typ != "" {
		b.WriteString(";t=" + ch.typ)
	}
	if len(ch.prog) > 0 {
		hs := make([]uint32, 0, len(ch.prog))
		for h := range ch.prog {
			hs = append(hs, h)
		}
		sort.Slice(hs, func(i, j int) bool { return hs[i] < hs[j] })
		b.WriteString(";p=")
		for i, h := range hs {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.FormatUint(uint64(h), 16))
		}
	}
	writePairs(&b, "x", ch.trans)
	return b.String()
}

func writePairs(b *strings.Builder, k string, ps []TranslatedWord) {
	if len(ps) == 0 {
		return
	}
	hs := make([]string, 0, len(ps))
	for _, p := range ps {
		hs = append(hs, p.Ent+":"+strconv.FormatUint(uint64(hpal(p.Name)), 16))
	}
	sort.Strings(hs)
	b.WriteString(";" + k + "=" + strings.Join(hs, ","))
}

func limitTo(cmd string) string {
	if len(cmd) > 64<<10 {
		return cmd[:64<<10]
	}
	return cmd
}

// hpal: FNV-1a de 32 bits da palavra, com as letras ASCII em minúsculas.
func hpal(p string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		h ^= uint32(c)
		h *= 16777619
	}
	return h
}

// ---- palavras --------------------------------------------------------------------------

// wordByte: letra ou dígito ASCII, "_", "-" e "." (o resto separa; letras fora do ASCII
// são tratadas em scanWords).
func wordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.'
}

// scanWords: as palavras de s, em uma passada: sequências de letras (de qualquer língua),
// dígitos, "_", "-" e ".", sem "." e "-" nas pontas.
func scanWords(s string, f func(a, b int)) {
	i := 0
	for i < len(s) {
		a := i
		for i < len(s) {
			c := s[i]
			if c < utf8.RuneSelf {
				if !wordByte(c) {
					break
				}
				i++
				continue
			}
			r, n := utf8.DecodeRuneInString(s[i:])
			if !unicode.IsLetter(r) {
				break
			}
			i += n
		}
		if i == a {
			if s[i] < utf8.RuneSelf {
				i++
			} else {
				_, n := utf8.DecodeRuneInString(s[i:])
				i += n
			}
			continue
		}
		b := i
		for a < b && (s[a] == '.' || s[a] == '-') {
			a++
		}
		for b > a && (s[b-1] == '.' || s[b-1] == '-') {
			b--
		}
		if b > a {
			f(a, b)
		}
	}
}

// identParts: as partes de um identificador: snake_case, kebab-case, a.b e camelCase
// ("Get-ADGroup" -> Get, AD, Group; "listBuckets" -> list, Buckets).
func identParts(v string, f func(p string)) {
	ini := 0
	emits := func(a, b int) {
		if b > a {
			f(v[a:b])
		}
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c == '_' || c == '-' || c == '.' {
			emits(ini, i)
			ini = i + 1
			continue
		}
		if i > ini && c >= 'A' && c <= 'Z' {
			p := v[i-1]
			next := i+1 < len(v) && v[i+1] >= 'a' && v[i+1] <= 'z'
			if p >= 'a' && p <= 'z' || p >= '0' && p <= '9' || p >= 'A' && p <= 'Z' && next {
				emits(ini, i)
				ini = i
			}
		}
	}
	emits(ini, len(v))
}

// maxScript: teto do texto guardado por nome de arquivo (e do programa montado).
const maxScript = 256 << 10

// citedFiles: os nomes de arquivo (com extensão) que cmd cita, sem o caminho.
func citedFiles(cmd string, f func(name string)) {
	for _, t := range strings.FieldsFunc(cmd, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '"' || r == '\'' || r == '`' || r == '=' || r == ';' || r == '(' || r == ')' || r == '<' || r == '>' || r == '|'
	}) {
		name := t[strings.LastIndexByte(t, '/')+1:]
		k := strings.LastIndexByte(name, '.')
		if k <= 0 || k == len(name)-1 || len(name) > 128 || !lettersOnly(name[k+1:]) {
			continue
		}
		f(name)
	}
}

// fullProgram: o comando mais o texto dos tool_use anteriores que citam os mesmos
// arquivos (o script que ele executa); e guarda este comando para os seguintes.
func (c *Commands) fullProgram(cmd string) string {
	prog := cmd
	seenSet := map[string]bool{}
	citedFiles(cmd, func(name string) {
		if t := c.cv.scripts[name]; t != "" && !seenSet[name] && len(prog) < maxScript {
			seenSet[name] = true
			prog += "\n" + t
		}
	})
	if len(cmd) > 64 { // só um texto com conteúdo (não um comando curto que só cita o arquivo)
		if c.cv.scripts == nil {
			c.cv.scripts = map[string]string{}
		}
		citedFiles(cmd, func(name string) {
			if t := c.cv.scripts[name]; len(t)+len(cmd) <= maxScript && !strings.Contains(t, cmd) {
				c.cv.scripts[name] = t + "\n" + cmd
			}
		})
	}
	return prog
}

// programWords: B1, as palavras do programa (o comando ou o script): cada palavra inteira e
// as partes dela.
func programWords(cmd string, f func(p string)) {
	scanWords(cmd, func(a, b int) {
		w := cmd[a:b]
		f(w)
		identParts(w, f)
	})
}

// ---- gramática do pedido (B5, B6) --------------------------------------------------------

// verbos de enumeração e consulta (gramática fechada; em qualquer convenção)
var enumVerbs = setOf("list", "get", "describe", "show", "ls", "search", "find", "query", "select", "scan",
	"enum", "enumerate", "dump", "export", "fetch", "inspect", "info")

// os mesmos verbos em português (imperativo, infinitivo) e as palavras de ligação entre o verbo
// e o substantivo numa frase ("lista todos os grupos", "show me all the tables")
var (
	proseVerbs = setOf("list", "get", "describe", "show", "search", "find", "query", "select", "scan",
		"enumerate", "dump", "export", "fetch", "inspect",
		"lista", "liste", "listar", "listem", "mostra", "mostre", "mostrar", "exibe", "exiba", "exibir",
		"busca", "busque", "buscar", "procura", "procure", "procurar", "encontra", "encontre", "encontrar",
		"consulta", "consulte", "consultar", "pega", "pegue", "pegar", "obtenha", "obter",
		"descreve", "descreva", "descrever", "exporta", "exporte", "exportar", "inspeciona", "inspecione",
		"inspecionar", "enumera", "enumere", "enumerar", "seleciona", "selecione", "selecionar",
		"varre", "varra", "varrer", "traz", "traga", "trazer")
	proseLink = setOf("o", "a", "os", "as", "um", "uma", "uns", "umas", "me", "todos", "todas", "todo", "toda",
		"meus", "minhas", "seus", "suas", "nossos", "nossas", "esses", "essas", "estes", "estas", "de", "do", "da",
		"dos", "das", "pra", "para", "mim", "the", "all", "my", "our", "your", "of", "these", "those", "some",
		"available", "existing", "current")
)

// entOfNoun: o tipo de ObjectPrefixes que o substantivo indica (singular ou plural; as
// palavras de tipo que já existem).
func entOfNoun(w string) (string, bool) {
	l := strings.ToLower(w)
	try := func(x string) (string, bool) {
		if x == "" {
			return "", false
		}
		if e, ok := listingResource[x]; ok {
			return e, true
		}
		if e, ok := pluralEnt(x); ok {
			return e, true
		}
		if _, ok := ObjectPrefixes[x]; ok {
			return x, true
		}
		if e, ok := entPiece[x]; ok {
			return e, true
		}
		return "", false
	}
	if e, ok := try(l); ok {
		return e, true
	}
	if strings.HasSuffix(l, "es") && len(l) > 4 {
		if e, ok := try(l[:len(l)-2]); ok {
			return e, true
		}
	}
	if strings.HasSuffix(l, "s") && len(l) > 3 {
		return try(l[:len(l)-1])
	}
	return "", false
}

// substantivo: o tipo de uma palavra que segue (ou precede) um verbo de enumeração. Casa com
// ObjectPrefixes: esse tipo. Não casa, mas é plural de letras: o tipo genérico (list_findings).
// Senão: nada (getLogger não é enumeração).
func noun(w string) (string, bool) {
	w = strings.Trim(w, `"'`)
	if w == "" {
		return "", false
	}
	if e, ok := entOfNoun(w); ok {
		return e, true
	}
	if len(w) > 3 && (w[len(w)-1] == 's' || w[len(w)-1] == 'S') && lettersOnly(w) {
		return genericEnt, true
	}
	return "", false
}

func lettersOnly(w string) bool {
	for _, r := range w {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return w != ""
}

// commandType: B5, o tipo que o verbo de enumeração do comando pede. Convenções:
// get_x / listX / Get-X / describe-x (identificador), "<cli> get x" e "<cli> x list"
// (subcomando; o verbo nunca é o próprio programa: "ls" sozinho é do shell), GET /x (REST) e
// SHOW x / SELECT ... FROM <catálogo>.x (SQL).
func commandType(cmd string) (string, bool) {
	if cmd == "" {
		return "", false
	}
	// SQL: SHOW <tipos>; SELECT ... FROM <catálogo>.<tipos>
	low := strings.ToLower(cmd)
	if strings.Contains(low, "show") || strings.Contains(low, "select") {
		if m := reShow.FindStringSubmatch(cmd); m != nil {
			if e, ok := pluralEnt(m[1]); ok {
				return e, true
			}
		}
		if ms := reSelectFrom.FindAllStringSubmatch(cmd, -1); len(ms) > 0 {
			from := ms[len(ms)-1][2]
			ps := strings.Split(from, ".")
			if len(ps) > 1 {
				if e, ok := pluralEnt(unquoted(ps[len(ps)-1])); ok {
					return e, true
				}
			}
		}
	}
	best, foundIt := "", false
	take := func(e string) {
		if !foundIt || best == genericEnt && e != genericEnt {
			best, foundIt = e, true
		}
	}
	for _, sgm := range strings.FieldsFunc(cmd, func(r rune) bool { return r == '|' || r == ';' || r == '&' || r == '\n' }) {
		ts := strings.Fields(sgm)
		for i, t := range ts {
			tl := strings.ToLower(strings.Trim(t, `"'`))
			// REST: GET /x, GET https://h/x
			if t == "GET" || t == `"GET"` || t == "'GET'" {
				for j := i + 1; j < len(ts) && j <= i+3; j++ {
					if e, ok := urlType(ts[j]); ok {
						take(e)
						break
					}
				}
			}
			// subcomando: <cli> get x / <cli> x list
			if i >= 1 && enumVerbs[tl] && tl != "select" {
				ok := false
				for j := i + 1; j < len(ts); j++ {
					if strings.HasPrefix(ts[j], "-") {
						continue
					}
					w := ts[j]
					if k := strings.IndexAny(w, "/,."); k > 0 {
						w = w[:k]
					}
					if e, o := noun(w); o {
						take(e)
						ok = true
					}
					break
				}
				if !ok && i >= 2 {
					if e, o := noun(ts[i-1]); o {
						take(e)
					}
				}
			}
			// identificador: list_findings, listBuckets, Get-ADGroup, describe-instances, get("/x")
			identifiers(t, func(id string, after string) {
				var ps []string
				identParts(id, func(p string) { ps = append(ps, p) })
				if len(ps) == 0 || !enumVerbs[strings.ToLower(ps[0])] {
					return
				}
				if len(ps) == 1 {
					if strings.HasPrefix(after, "(") { // get("https://h/api/queues")
						if e, ok := urlType(strings.TrimLeft(after[1:], `fbru"'`)); ok {
							take(e)
						}
					}
					return
				}
				for k := len(ps) - 1; k >= 1; k-- {
					if e, ok := entOfNoun(ps[k]); ok {
						take(e)
						return
					}
				}
				if e, ok := noun(ps[len(ps)-1]); ok {
					take(e)
				}
			})
		}
	}
	return best, foundIt
}

// identificadores: os identificadores de um token de shell (letras, dígitos, "_", "-"), cada um
// com o que vem logo depois dele no token.
func identifiers(t string, f func(id, after string)) {
	for i := 0; i < len(t); {
		if !(isAlnum(t[i]) || t[i] == '_') {
			i++
			continue
		}
		a := i
		for i < len(t) && (isAlnum(t[i]) || t[i] == '_' || t[i] == '-') {
			i++
		}
		f(strings.Trim(t[a:i], "-"), t[i:])
	}
}

// urlType: o tipo do último segmento de um caminho REST ("/api/queues", "https://h/v1/buckets?x").
func urlType(u string) (string, bool) {
	u = strings.Trim(u, `"'()`)
	if k := strings.Index(u, "://"); k >= 0 {
		u = u[k+3:]
		if j := strings.IndexByte(u, '/'); j >= 0 {
			u = u[j:]
		} else {
			return "", false
		}
	}
	if !strings.HasPrefix(u, "/") {
		return "", false
	}
	if j := strings.IndexAny(u, "?#"); j >= 0 {
		u = u[:j]
	}
	u = strings.TrimRight(u, "/")
	sgm := u[strings.LastIndexByte(u, '/')+1:]
	return noun(sgm)
}

// typeInProse: B6, o tipo pedido numa frase ("lista os grupos com acesso ao banco", "show me
// the queues"): um verbo de enumeração, até três palavras de ligação e o substantivo.
func typeInProse(s string) (string, bool) {
	if len(s) > 8<<10 {
		s = s[:8<<10]
	}
	var ws []string
	i := 0
	for i < len(s) && len(ws) < 4000 {
		r, n := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsLetter(r) {
			i += n
			continue
		}
		a := i
		for i < len(s) {
			r, n := utf8.DecodeRuneInString(s[i:])
			if !unicode.IsLetter(r) && r != '_' && r != '-' {
				break
			}
			i += n
		}
		ws = append(ws, s[a:i])
	}
	for k, w := range ws {
		if !proseVerbs[strings.ToLower(w)] {
			continue
		}
		for j := k + 1; j < len(ws) && j <= k+4; j++ {
			l := strings.ToLower(ws[j])
			if proseLink[l] {
				continue
			}
			if e, ok := noun(ws[j]); ok {
				return e, true
			}
			break
		}
	}
	return "", false
}

// ---- argumentos (B4) -------------------------------------------------------------------

// palavras depois das quais vem um nome em SQL e em comandos (FROM x, TO ROLE x, USE x)
var beforeName = setOf("from", "join", "into", "update", "table", "use", "role", "database", "schema", "warehouse")

// argumentos: as palavras de cmd em posição de argumento, pela gramática: o valor depois de uma
// opção (-n x, --namespace x, --namespace=x), de uma atribuição (x=v), de uma palavra de tipo
// (ns x, queue x) ou de FROM/INTO/TO ROLE; os segmentos de um caminho REST; o literal de uma
// palavra só ('x'). ent: o tipo que o contexto diz ("" = nenhum).
func arguments(cmd string, f func(p, ent string)) {
	emitIt := func(v, ent string) {
		v = strings.Trim(v, `"'`+"`(),;[]{}")
		if v == "" {
			return
		}
		if strings.ContainsAny(v, "/:") {
			for _, sgm := range strings.FieldsFunc(v, func(r rune) bool { return r == '/' || r == ':' }) {
				if sgm != "" && reSimpleIdent.MatchString(sgm) {
					f(sgm, "")
				}
			}
			return
		}
		scanWords(v, func(a, b int) { f(v[a:b], ent) })
	}
	for _, sgm := range strings.FieldsFunc(cmd, func(r rune) bool { return r == '|' || r == ';' || r == '&' || r == '\n' }) {
		ts := strings.Fields(sgm)
		for i, t := range ts {
			if strings.HasPrefix(t, "-") {
				name := strings.TrimLeft(t, "-")
				if k, v, ok := strings.Cut(name, "="); ok {
					e, _ := entKey(k)
					emitIt(v, e)
					continue
				}
				if i+1 < len(ts) && !strings.HasPrefix(ts[i+1], "-") {
					e, _ := entKey(name)
					emitIt(ts[i+1], e)
				}
				continue
			}
			if k, v, ok := strings.Cut(t, "="); ok && k != "" && v != "" && !strings.ContainsAny(k, `"'(`) {
				e, _ := entKey(strings.Trim(k, `"'`))
				emitIt(v, e)
				continue
			}
			if strings.Contains(t, "/") && (strings.HasPrefix(strings.Trim(t, `"'`), "/") || strings.Contains(t, "://")) {
				emitIt(t, "")
				continue
			}
			if i >= 1 {
				prev := strings.ToLower(strings.Trim(ts[i-1], `"'`))
				if e, ok := entOfNoun(prev); ok && !enumVerbs[strings.ToLower(t)] {
					emitIt(t, e)
					continue
				}
				if beforeName[prev] {
					emitIt(t, "")
					continue
				}
			}
			if len(t) >= 3 && (t[0] == '\'' || t[0] == '"') && t[len(t)-1] == t[0] && !strings.ContainsAny(t[1:len(t)-1], " \t") {
				emitIt(t, "")
			}
		}
	}
}

// ---- palavras traduzidas (B1, exceção) ---------------------------------------------------

// entOfPrefix: o tipo de um pseudônimo de objeto pelo prefixo ("ns_x" -> namespace).
var entOfPrefix = func() map[string]string {
	m := map[string]string{"obj": genericEnt}
	for e, p := range ObjectPrefixes {
		m[strings.ToLower(p)] = e
	}
	return m
}()

// TranslatedWords: as palavras de s que o proxy traduziu de um pseudônimo de objeto (s é
// uma string da entrada de um tool_use, como o Claude Code a reenvia). Sem registro: nada.
func (m *Masker) TranslatedWords(s string) []TranslatedWord {
	tr, ok := m.Translated(s)
	if !ok {
		return nil
	}
	var out []TranslatedWord
	for _, t := range tr {
		if t.Start < 0 || t.End > len(s) || t.End <= t.Start {
			continue
		}
		k := strings.IndexByte(t.Pseudo, '_')
		if k <= 0 {
			continue
		}
		e, ok := entOfPrefix[strings.ToLower(t.Pseudo[:k])]
		if !ok {
			continue
		}
		out = append(out, TranslatedWord{s[t.Start:t.End], e})
	}
	return out
}
