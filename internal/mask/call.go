package mask

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// What the proxy knows about a tool call (see docs/structures.md, sections Provenance, Identity and
// Echo). No rule is per tool: everything comes from the grammar of the request (enumeration verb +
// noun), from the provenance of the words (the program the model wrote vs. the data the output
// brought) and from the order of the conversation (echo).
//
// The proxy walks the conversation in order (proxy/request.go, commandHints):
//   - ArgumentText(cmd), before everything, for each tool_use: the words in argument position;
//   - User(text): the last user message (B6: "list the groups..." gives the type);
//   - Usage(cmd, translated): the tool_use; returns the Call (extension of the input hint);
//   - Result(ch, output): the tool_result; returns the extension of the output hint;
//   - EndTurn(): end of an assistant message (the user request only applies to the calls of
//     the first response after it).
//
// The extension goes after sepExt in the hint (decision.go) and enters the memo key: it is compact
// and deterministic (short hashes of the words, in order). Format:
//
//	s;t=<type>;p=<h>,<h>...;x=<type>:<h>,...      (command output)
//	u;e=<type>:<h>,...;x=<type>:<h>,...           (tool_use input)
//
// t: the requested type (enumeration verb of the command, or of the user message); p: the program
// words; x: the words the proxy translated from a pseudonym in that tool_use (B1: they are names,
// never program); e: the tool_use words that echo a previous output (B4). h is the 32-bit FNV-1a of
// the lowercased word (ASCII), in hexadecimal.

// genericEnt: the type of a name whose noun matches no type word of ObjectPrefixes (list_findings,
// branches). It is not an ObjectPrefixes key on purpose: pseudoObject then uses the generic "OBJ"
// prefix (the usual one); objMask applies (on by default, can be turned off in objects.mask.object)
// and objPropagates is false (it is not in defaultPropagates): the name is masked where it is
// decided and by the conversation memory, but never learned.
const genericEnt = "object"

// conversationCmd: the state of Commands for the rules of this section.
type conversationCmd struct {
	args     map[string]bool   // words in argument position in some tool_use
	seenOnes map[string]string // argument -> requested type of the output it appeared in ("" = no type)
	request  string            // the type requested by the last user message ("" = none)
	hasPed   bool
	usedPed  bool // there was already a call in this turn
	nOutputs int
	nScanned int // output bytes scanned (ceiling)
	// scripts: the text of the tool_uses by the file name they cite. A script written in one call
	// and run in another ("python3 scripts/x.py") is a program of the second one too.
	scripts map[string]string
}

// Call: a tool_use, with what is known about it.
type Call struct {
	typ      string
	prog     map[uint32]bool
	trans    []TranslatedWord
	echoes   []TranslatedWord
	usageExt string
}

// TranslatedWord: a word the proxy put in the text when translating a pseudonym (Name) and the type
// of the pseudonym.
type TranslatedWord struct{ Name, Ent string }

// UsageExt: the hint extension for the strings of the tool_use input ("" = none).
func (ch *Call) UsageExt() string {
	if ch == nil {
		return ""
	}
	return ch.usageExt
}

// WithExtension: the table reader's hint (encode) with the extension.
func WithExtension(table, ext string) string {
	if ext == "" {
		return table
	}
	return table + sepExt + ext
}

const (
	maxArgs       = 4096
	maxScanOutput = 64 << 20 // output bytes scanned for the echo, per request
)

// ArgumentText: records the words in argument position of a tool_use (before the in-order walk, so
// the scan of the outputs looks only for them).
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

// User: a user message. The type it asks for applies to the calls of the next response.
func (c *Commands) User(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	c.cv.request, c.cv.hasPed = typeInProse(text)
	c.cv.usedPed = false
}

// EndTurn: an assistant message ended. If it had calls, the user request was already used.
func (c *Commands) EndTurn() {
	if c.cv.usedPed {
		c.cv.request, c.cv.hasPed, c.cv.usedPed = "", false, false
	}
}

// Usage: a tool_use. trans: the words the proxy translated from a pseudonym in its input.
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
	// echo: an argument that appeared in a previous output
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

// Result: the output of a call. Returns the extension of the output hint. It also marks the
// tool_use arguments that appear in it (for the echo of the following calls).
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

// hpal: 32-bit FNV-1a of the word, with ASCII letters lowercased.
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

// wordByte: ASCII letter or digit, "_", "-" and "." (the rest separates; non-ASCII letters are
// handled in scanWords).
func wordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.'
}

// scanWords: the words of s, in one pass: sequences of letters (of any language), digits, "_", "-"
// and ".", without "." and "-" at the ends.
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

// identParts: the parts of an identifier: snake_case, kebab-case, a.b and camelCase ("Get-ADGroup"
// -> Get, AD, Group; "listBuckets" -> list, Buckets).
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

// maxScript: ceiling of the text kept per file name (and of the assembled program).
const maxScript = 256 << 10

// citedFiles: the file names (with extension) that cmd cites, without the path.
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

// fullProgram: the command plus the text of the previous tool_uses that cite the same files (the
// script it runs); and keeps this command for the following ones.
func (c *Commands) fullProgram(cmd string) string {
	prog := cmd
	seenSet := map[string]bool{}
	citedFiles(cmd, func(name string) {
		if t := c.cv.scripts[name]; t != "" && !seenSet[name] && len(prog) < maxScript {
			seenSet[name] = true
			prog += "\n" + t
		}
	})
	if len(cmd) > 64 { // only a text with content (not a short command that only cites the file)
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

// programWords: B1, the program words (the command or the script): each whole word and its parts.
func programWords(cmd string, f func(p string)) {
	scanWords(cmd, func(a, b int) {
		w := cmd[a:b]
		f(w)
		identParts(w, f)
	})
}

// ---- grammar of the request (B5, B6) -----------------------------------------------------

// enumeration and query verbs (closed grammar; in any convention)
var enumVerbs = setOf("list", "get", "describe", "show", "ls", "search", "find", "query", "select", "scan",
	"enum", "enumerate", "dump", "export", "fetch", "inspect", "info")

// the same verbs in Portuguese (imperative, infinitive) and the linking words between the verb and
// the noun in a sentence ("lista todos os grupos", "show me all the tables")
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

// entOfNoun: the ObjectPrefixes type the noun indicates (singular or plural; the type words that
// already exist).
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

// noun: the type of a word that follows (or precedes) an enumeration verb. Matches ObjectPrefixes:
// that type. Does not match, but is a plural of letters: the generic type (list_findings).
// Otherwise: nothing (getLogger is not an enumeration).
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

// commandType: B5, the type the command's enumeration verb asks for. Conventions: get_x / listX /
// Get-X / describe-x (identifier), "<cli> get x" and "<cli> x list" (subcommand; the verb is never
// the program itself: "ls" alone belongs to the shell), GET /x (REST) and SHOW x / SELECT ... FROM
// <catalog>.x (SQL).
func commandType(cmd string) (string, bool) {
	if cmd == "" {
		return "", false
	}
	// SQL: SHOW <types>; SELECT ... FROM <catalog>.<types>
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

// identifiers: the identifiers of a shell token (letters, digits, "_", "-"), each with what comes
// right after it in the token.
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

// urlType: the type of the last segment of a REST path ("/api/queues", "https://h/v1/buckets?x").
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

// typeInProse: B6, the type requested in a sentence ("lista os grupos com acesso ao banco", "show
// me the queues"): an enumeration verb, up to three linking words and the noun.
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

// words after which a name comes in SQL and in commands (FROM x, TO ROLE x, USE x)
var beforeName = setOf("from", "join", "into", "update", "table", "use", "role", "database", "schema", "warehouse")

// arguments: the words of cmd in argument position, by the grammar: the value after an option (-n
// x, --namespace x, --namespace=x), an assignment (x=v), a type word (ns x, queue x) or
// FROM/INTO/TO ROLE; the segments of a REST path; the literal of a single word ('x'). ent: the type
// the context says ("" = none).
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

// ---- translated words (B1, exception) ----------------------------------------------------

// entOfPrefix: the type of an object pseudonym by its prefix ("ns_x" -> namespace).
var entOfPrefix = func() map[string]string {
	m := map[string]string{"obj": genericEnt}
	for e, p := range ObjectPrefixes {
		m[strings.ToLower(p)] = e
	}
	return m
}()

// TranslatedWords: the words of s the proxy translated from an object pseudonym (s is a string of a
// tool_use input, as Claude Code resends it). Without a registry: nothing.
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
