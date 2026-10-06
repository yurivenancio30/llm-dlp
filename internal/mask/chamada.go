package mask

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// O que o proxy sabe de uma chamada de ferramenta (ver docs/estruturas.md, seções
// Proveniência, Identidade e Eco). Nenhuma regra é por ferramenta: tudo vem da gramática do
// pedido (verbo de enumeração + substantivo), da proveniência das palavras (o programa que o
// modelo escreveu x os dados que a saída trouxe) e da ordem da conversa (eco).
//
// O proxy percorre a conversa em ordem (proxy/requisicao.go, dicasDosComandos):
//   - Argumentos(cmd), antes de tudo, para cada tool_use: as palavras em posição de argumento;
//   - Usuario(texto): a última mensagem do usuário (B6: "lista os grupos..." dá o tipo);
//   - Uso(cmd, traduzidas): o tool_use; devolve a Chamada (extensão da dica da entrada);
//   - Resultado(ch, saida): o tool_result; devolve a extensão da dica da saída;
//   - FimTurno(): fim de uma mensagem do assistente (o pedido do usuário vale só para as
//     chamadas da primeira resposta depois dele).
//
// A extensão vai depois de sepExt na dica (decisao.go) e entra na chave do memo: é compacta e
// determinística (hashes curtos das palavras, em ordem). Formato:
//
//	s;t=<tipo>;p=<h>,<h>...;x=<tipo>:<h>,...      (saída de comando)
//	u;e=<tipo>:<h>,...;x=<tipo>:<h>,...           (entrada do tool_use)
//
// t: o tipo pedido (verbo de enumeração do comando, ou da mensagem do usuário); p: as palavras
// do programa; x: as palavras que o proxy traduziu de um pseudônimo nesse tool_use (B1: são
// nome, nunca programa); e: as palavras do tool_use que ecoam uma saída anterior (B4). h é o
// FNV-1a de 32 bits da palavra em minúsculas (ASCII), em hexadecimal.

// entGenerica: o tipo de um nome cujo substantivo não casa com nenhuma palavra de tipo de
// EntObjeto (list_findings, branches). Não é chave de EntObjeto de propósito: pseudoObjeto usa
// então o prefixo genérico "OBJ" (o mesmo de sempre); objMascara vale (ligado por padrão,
// desligável em objetos.mascarar.objeto) e objPropaga é falso (não está em propagaPadrao): o
// nome é mascarado onde é decidido e pela memória da conversa, mas nunca aprendido.
const entGenerica = "objeto"

// conversaCmd: o estado de Comandos para as regras desta seção.
type conversaCmd struct {
	args     map[string]bool   // palavras em posição de argumento em algum tool_use
	vistas   map[string]string // argumento -> tipo pedido da saída em que apareceu ("" = sem tipo)
	pedido   string            // o tipo pedido pela última mensagem do usuário ("" = nada)
	temPed   bool
	usouPed  bool // já houve chamada neste turno
	nSaidas  int
	nVarrido int // bytes de saída varridos (teto)
	// scripts: o texto dos tool_use pelo nome de arquivo que citam. Um script escrito numa
	// chamada e executado noutra ("python3 scripts/x.py") é programa da segunda também.
	scripts map[string]string
}

// Chamada: um tool_use, com o que se sabe dele.
type Chamada struct {
	tipo   string
	prog   map[uint32]bool
	trad   []PalavraTraduzida
	ecos   []PalavraTraduzida
	extUso string
}

// PalavraTraduzida: uma palavra que o proxy pôs no texto ao traduzir um pseudônimo (Nome) e o
// tipo do pseudônimo.
type PalavraTraduzida struct{ Nome, Ent string }

// ExtUso: a extensão da dica para as strings da entrada do tool_use ("" = nada).
func (ch *Chamada) ExtUso() string {
	if ch == nil {
		return ""
	}
	return ch.extUso
}

// ComExtensao: a dica do leitor de tabela (codificar) com a extensão.
func ComExtensao(tabela, ext string) string {
	if ext == "" {
		return tabela
	}
	return tabela + sepExt + ext
}

const (
	maxArgs       = 4096
	maxVarrerSaid = 64 << 20 // bytes de saída varridos para o eco, por requisição
)

// Argumentos: registra as palavras em posição de argumento de um tool_use (antes do percurso
// em ordem, para a varredura das saídas procurar só por elas).
func (c *Comandos) Argumentos(cmd string) {
	if c.cv.args == nil {
		c.cv.args = map[string]bool{}
	}
	argumentos(limitar(cmd), func(p, _ string) {
		if len(c.cv.args) < maxArgs {
			c.cv.args[p] = true
		}
	})
}

// Usuario: uma mensagem do usuário. O tipo que ela pede vale para as chamadas da resposta
// seguinte.
func (c *Comandos) Usuario(texto string) {
	if strings.TrimSpace(texto) == "" {
		return
	}
	c.cv.pedido, c.cv.temPed = tipoNaProsa(texto)
	c.cv.usouPed = false
}

// FimTurno: acabou uma mensagem do assistente. Se ela teve chamadas, o pedido do usuário já foi
// usado.
func (c *Comandos) FimTurno() {
	if c.cv.usouPed {
		c.cv.pedido, c.cv.temPed, c.cv.usouPed = "", false, false
	}
}

// Uso: um tool_use. trad: as palavras que o proxy traduziu de um pseudônimo na entrada dele.
func (c *Comandos) Uso(cmd string, trad []PalavraTraduzida) *Chamada {
	cmd = limitar(cmd)
	ch := &Chamada{trad: trad}
	if e, ok := tipoDoComando(cmd); ok {
		ch.tipo = e
	} else if c.cv.temPed {
		ch.tipo = c.cv.pedido
	}
	c.cv.usouPed = true
	tradu := map[uint32]bool{}
	for _, t := range trad {
		tradu[hpal(t.Nome)] = true
	}
	ch.prog = map[uint32]bool{}
	prog := c.programaCompleto(cmd)
	palavrasPrograma(prog, func(p string) {
		if h := hpal(p); !tradu[h] {
			ch.prog[h] = true
		}
	})
	// eco: argumento que apareceu numa saída anterior
	visto := map[string]bool{}
	argumentos(cmd, func(p, ent string) {
		tipoSaida, ok := c.cv.vistas[p]
		if !ok || visto[p] || tradu[hpal(p)] || !palavraDecidivel(p) {
			return
		}
		visto[p] = true
		if ent == "" {
			ent = tipoSaida
		}
		if ent == "" {
			ent = entGenerica
		}
		ch.ecos = append(ch.ecos, PalavraTraduzida{p, ent})
	})
	if len(ch.ecos) > 0 || len(trad) > 0 {
		var b strings.Builder
		b.WriteString("u")
		escreverPares(&b, "e", ch.ecos)
		escreverPares(&b, "x", trad)
		ch.extUso = b.String()
	}
	return ch
}

// Resultado: a saída de uma chamada. Devolve a extensão da dica da saída. Também marca os
// argumentos de tool_use que aparecem nela (para o eco das chamadas seguintes).
func (c *Comandos) Resultado(ch *Chamada, saida string) string {
	tipo := ""
	if ch != nil {
		tipo = ch.tipo
	}
	if len(c.cv.args) > 0 && c.cv.nVarrido < maxVarrerSaid {
		c.cv.nVarrido += len(saida)
		if c.cv.vistas == nil {
			c.cv.vistas = map[string]string{}
		}
		varrerPalavras(saida, func(a, b int) {
			p := saida[a:b]
			if c.cv.args[p] {
				if _, ok := c.cv.vistas[p]; !ok {
					c.cv.vistas[strings.Clone(p)] = tipo
				}
			}
		})
	}
	if ch == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("s")
	if ch.tipo != "" {
		b.WriteString(";t=" + ch.tipo)
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
	escreverPares(&b, "x", ch.trad)
	return b.String()
}

func escreverPares(b *strings.Builder, k string, ps []PalavraTraduzida) {
	if len(ps) == 0 {
		return
	}
	hs := make([]string, 0, len(ps))
	for _, p := range ps {
		hs = append(hs, p.Ent+":"+strconv.FormatUint(uint64(hpal(p.Nome)), 16))
	}
	sort.Strings(hs)
	b.WriteString(";" + k + "=" + strings.Join(hs, ","))
}

func limitar(cmd string) string {
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

// byteDePalavra: letra ou dígito ASCII, "_", "-" e "." (o resto separa; letras fora do ASCII
// são tratadas em varrerPalavras).
func byteDePalavra(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.'
}

// varrerPalavras: as palavras de s, em uma passada: sequências de letras (de qualquer língua),
// dígitos, "_", "-" e ".", sem "." e "-" nas pontas.
func varrerPalavras(s string, f func(a, b int)) {
	i := 0
	for i < len(s) {
		a := i
		for i < len(s) {
			c := s[i]
			if c < utf8.RuneSelf {
				if !byteDePalavra(c) {
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

// partesIdent: as partes de um identificador: snake_case, kebab-case, a.b e camelCase
// ("Get-ADGroup" -> Get, AD, Group; "listBuckets" -> list, Buckets).
func partesIdent(v string, f func(p string)) {
	ini := 0
	emite := func(a, b int) {
		if b > a {
			f(v[a:b])
		}
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c == '_' || c == '-' || c == '.' {
			emite(ini, i)
			ini = i + 1
			continue
		}
		if i > ini && c >= 'A' && c <= 'Z' {
			p := v[i-1]
			prox := i+1 < len(v) && v[i+1] >= 'a' && v[i+1] <= 'z'
			if p >= 'a' && p <= 'z' || p >= '0' && p <= '9' || p >= 'A' && p <= 'Z' && prox {
				emite(ini, i)
				ini = i
			}
		}
	}
	emite(ini, len(v))
}

// maxScript: teto do texto guardado por nome de arquivo (e do programa montado).
const maxScript = 256 << 10

// arquivosCitados: os nomes de arquivo (com extensão) que cmd cita, sem o caminho.
func arquivosCitados(cmd string, f func(nome string)) {
	for _, t := range strings.FieldsFunc(cmd, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '"' || r == '\'' || r == '`' || r == '=' || r == ';' || r == '(' || r == ')' || r == '<' || r == '>' || r == '|'
	}) {
		nome := t[strings.LastIndexByte(t, '/')+1:]
		k := strings.LastIndexByte(nome, '.')
		if k <= 0 || k == len(nome)-1 || len(nome) > 128 || !soLetras(nome[k+1:]) {
			continue
		}
		f(nome)
	}
}

// programaCompleto: o comando mais o texto dos tool_use anteriores que citam os mesmos
// arquivos (o script que ele executa); e guarda este comando para os seguintes.
func (c *Comandos) programaCompleto(cmd string) string {
	prog := cmd
	vistos := map[string]bool{}
	arquivosCitados(cmd, func(nome string) {
		if t := c.cv.scripts[nome]; t != "" && !vistos[nome] && len(prog) < maxScript {
			vistos[nome] = true
			prog += "\n" + t
		}
	})
	if len(cmd) > 64 { // só um texto com conteúdo (não um comando curto que só cita o arquivo)
		if c.cv.scripts == nil {
			c.cv.scripts = map[string]string{}
		}
		arquivosCitados(cmd, func(nome string) {
			if t := c.cv.scripts[nome]; len(t)+len(cmd) <= maxScript && !strings.Contains(t, cmd) {
				c.cv.scripts[nome] = t + "\n" + cmd
			}
		})
	}
	return prog
}

// palavrasPrograma: B1, as palavras do programa (o comando ou o script): cada palavra inteira e
// as partes dela.
func palavrasPrograma(cmd string, f func(p string)) {
	varrerPalavras(cmd, func(a, b int) {
		w := cmd[a:b]
		f(w)
		partesIdent(w, f)
	})
}

// ---- gramática do pedido (B5, B6) --------------------------------------------------------

// verbos de enumeração e consulta (gramática fechada; em qualquer convenção)
var verbosEnum = conj("list", "get", "describe", "show", "ls", "search", "find", "query", "select", "scan",
	"enum", "enumerate", "dump", "export", "fetch", "inspect", "info")

// os mesmos verbos em português (imperativo, infinitivo) e as palavras de ligação entre o verbo
// e o substantivo numa frase ("lista todos os grupos", "show me all the tables")
var (
	verbosProsa = conj("list", "get", "describe", "show", "search", "find", "query", "select", "scan",
		"enumerate", "dump", "export", "fetch", "inspect",
		"lista", "liste", "listar", "listem", "mostra", "mostre", "mostrar", "exibe", "exiba", "exibir",
		"busca", "busque", "buscar", "procura", "procure", "procurar", "encontra", "encontre", "encontrar",
		"consulta", "consulte", "consultar", "pega", "pegue", "pegar", "obtenha", "obter",
		"descreve", "descreva", "descrever", "exporta", "exporte", "exportar", "inspeciona", "inspecione",
		"inspecionar", "enumera", "enumere", "enumerar", "seleciona", "selecione", "selecionar",
		"varre", "varra", "varrer", "traz", "traga", "trazer")
	ligacaoProsa = conj("o", "a", "os", "as", "um", "uma", "uns", "umas", "me", "todos", "todas", "todo", "toda",
		"meus", "minhas", "seus", "suas", "nossos", "nossas", "esses", "essas", "estes", "estas", "de", "do", "da",
		"dos", "das", "pra", "para", "mim", "the", "all", "my", "our", "your", "of", "these", "those", "some",
		"available", "existing", "current")
)

// entDoSubstantivo: o tipo de EntObjeto que o substantivo indica (singular ou plural; as
// palavras de tipo que já existem).
func entDoSubstantivo(w string) (string, bool) {
	l := strings.ToLower(w)
	try := func(x string) (string, bool) {
		if x == "" {
			return "", false
		}
		if e, ok := recursoListagem[x]; ok {
			return e, true
		}
		if e, ok := entPlural(x); ok {
			return e, true
		}
		if _, ok := EntObjeto[x]; ok {
			return x, true
		}
		if e, ok := entPedaco[x]; ok {
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
// EntObjeto: esse tipo. Não casa, mas é plural de letras: o tipo genérico (list_findings).
// Senão: nada (getLogger não é enumeração).
func substantivo(w string) (string, bool) {
	w = strings.Trim(w, `"'`)
	if w == "" {
		return "", false
	}
	if e, ok := entDoSubstantivo(w); ok {
		return e, true
	}
	if len(w) > 3 && (w[len(w)-1] == 's' || w[len(w)-1] == 'S') && soLetras(w) {
		return entGenerica, true
	}
	return "", false
}

func soLetras(w string) bool {
	for _, r := range w {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return w != ""
}

// tipoDoComando: B5, o tipo que o verbo de enumeração do comando pede. Convenções:
// get_x / listX / Get-X / describe-x (identificador), "<cli> get x" e "<cli> x list"
// (subcomando; o verbo nunca é o próprio programa: "ls" sozinho é do shell), GET /x (REST) e
// SHOW x / SELECT ... FROM <catálogo>.x (SQL).
func tipoDoComando(cmd string) (string, bool) {
	if cmd == "" {
		return "", false
	}
	// SQL: SHOW <tipos>; SELECT ... FROM <catálogo>.<tipos>
	low := strings.ToLower(cmd)
	if strings.Contains(low, "show") || strings.Contains(low, "select") {
		if m := reShow.FindStringSubmatch(cmd); m != nil {
			if e, ok := entPlural(m[1]); ok {
				return e, true
			}
		}
		if ms := reSelectFrom.FindAllStringSubmatch(cmd, -1); len(ms) > 0 {
			from := ms[len(ms)-1][2]
			ps := strings.Split(from, ".")
			if len(ps) > 1 {
				if e, ok := entPlural(semCitacao(ps[len(ps)-1])); ok {
					return e, true
				}
			}
		}
	}
	melhor, achou := "", false
	pegar := func(e string) {
		if !achou || melhor == entGenerica && e != entGenerica {
			melhor, achou = e, true
		}
	}
	for _, seg := range strings.FieldsFunc(cmd, func(r rune) bool { return r == '|' || r == ';' || r == '&' || r == '\n' }) {
		ts := strings.Fields(seg)
		for i, t := range ts {
			tl := strings.ToLower(strings.Trim(t, `"'`))
			// REST: GET /x, GET https://h/x
			if t == "GET" || t == `"GET"` || t == "'GET'" {
				for j := i + 1; j < len(ts) && j <= i+3; j++ {
					if e, ok := tipoDaURL(ts[j]); ok {
						pegar(e)
						break
					}
				}
			}
			// subcomando: <cli> get x / <cli> x list
			if i >= 1 && verbosEnum[tl] && tl != "select" {
				ok := false
				for j := i + 1; j < len(ts); j++ {
					if strings.HasPrefix(ts[j], "-") {
						continue
					}
					w := ts[j]
					if k := strings.IndexAny(w, "/,."); k > 0 {
						w = w[:k]
					}
					if e, o := substantivo(w); o {
						pegar(e)
						ok = true
					}
					break
				}
				if !ok && i >= 2 {
					if e, o := substantivo(ts[i-1]); o {
						pegar(e)
					}
				}
			}
			// identificador: list_findings, listBuckets, Get-ADGroup, describe-instances, get("/x")
			identificadores(t, func(id string, depois string) {
				var ps []string
				partesIdent(id, func(p string) { ps = append(ps, p) })
				if len(ps) == 0 || !verbosEnum[strings.ToLower(ps[0])] {
					return
				}
				if len(ps) == 1 {
					if strings.HasPrefix(depois, "(") { // get("https://h/api/queues")
						if e, ok := tipoDaURL(strings.TrimLeft(depois[1:], `fbru"'`)); ok {
							pegar(e)
						}
					}
					return
				}
				for k := len(ps) - 1; k >= 1; k-- {
					if e, ok := entDoSubstantivo(ps[k]); ok {
						pegar(e)
						return
					}
				}
				if e, ok := substantivo(ps[len(ps)-1]); ok {
					pegar(e)
				}
			})
		}
	}
	return melhor, achou
}

// identificadores: os identificadores de um token de shell (letras, dígitos, "_", "-"), cada um
// com o que vem logo depois dele no token.
func identificadores(t string, f func(id, depois string)) {
	for i := 0; i < len(t); {
		if !(ehAlnum(t[i]) || t[i] == '_') {
			i++
			continue
		}
		a := i
		for i < len(t) && (ehAlnum(t[i]) || t[i] == '_' || t[i] == '-') {
			i++
		}
		f(strings.Trim(t[a:i], "-"), t[i:])
	}
}

// tipoDaURL: o tipo do último segmento de um caminho REST ("/api/queues", "https://h/v1/buckets?x").
func tipoDaURL(u string) (string, bool) {
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
	seg := u[strings.LastIndexByte(u, '/')+1:]
	return substantivo(seg)
}

// tipoNaProsa: B6, o tipo pedido numa frase ("lista os grupos com acesso ao banco", "show me
// the queues"): um verbo de enumeração, até três palavras de ligação e o substantivo.
func tipoNaProsa(s string) (string, bool) {
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
		if !verbosProsa[strings.ToLower(w)] {
			continue
		}
		for j := k + 1; j < len(ws) && j <= k+4; j++ {
			l := strings.ToLower(ws[j])
			if ligacaoProsa[l] {
				continue
			}
			if e, ok := substantivo(ws[j]); ok {
				return e, true
			}
			break
		}
	}
	return "", false
}

// ---- argumentos (B4) -------------------------------------------------------------------

// palavras depois das quais vem um nome em SQL e em comandos (FROM x, TO ROLE x, USE x)
var antesDeNome = conj("from", "join", "into", "update", "table", "use", "role", "database", "schema", "warehouse")

// argumentos: as palavras de cmd em posição de argumento, pela gramática: o valor depois de uma
// opção (-n x, --namespace x, --namespace=x), de uma atribuição (x=v), de uma palavra de tipo
// (ns x, queue x) ou de FROM/INTO/TO ROLE; os segmentos de um caminho REST; o literal de uma
// palavra só ('x'). ent: o tipo que o contexto diz ("" = nenhum).
func argumentos(cmd string, f func(p, ent string)) {
	emitir := func(v, ent string) {
		v = strings.Trim(v, `"'`+"`(),;[]{}")
		if v == "" {
			return
		}
		if strings.ContainsAny(v, "/:") {
			for _, seg := range strings.FieldsFunc(v, func(r rune) bool { return r == '/' || r == ':' }) {
				if seg != "" && reIdentSimples.MatchString(seg) {
					f(seg, "")
				}
			}
			return
		}
		varrerPalavras(v, func(a, b int) { f(v[a:b], ent) })
	}
	for _, seg := range strings.FieldsFunc(cmd, func(r rune) bool { return r == '|' || r == ';' || r == '&' || r == '\n' }) {
		ts := strings.Fields(seg)
		for i, t := range ts {
			if strings.HasPrefix(t, "-") {
				nome := strings.TrimLeft(t, "-")
				if k, v, ok := strings.Cut(nome, "="); ok {
					e, _ := entChave(k)
					emitir(v, e)
					continue
				}
				if i+1 < len(ts) && !strings.HasPrefix(ts[i+1], "-") {
					e, _ := entChave(nome)
					emitir(ts[i+1], e)
				}
				continue
			}
			if k, v, ok := strings.Cut(t, "="); ok && k != "" && v != "" && !strings.ContainsAny(k, `"'(`) {
				e, _ := entChave(strings.Trim(k, `"'`))
				emitir(v, e)
				continue
			}
			if strings.Contains(t, "/") && (strings.HasPrefix(strings.Trim(t, `"'`), "/") || strings.Contains(t, "://")) {
				emitir(t, "")
				continue
			}
			if i >= 1 {
				ant := strings.ToLower(strings.Trim(ts[i-1], `"'`))
				if e, ok := entDoSubstantivo(ant); ok && !verbosEnum[strings.ToLower(t)] {
					emitir(t, e)
					continue
				}
				if antesDeNome[ant] {
					emitir(t, "")
					continue
				}
			}
			if len(t) >= 3 && (t[0] == '\'' || t[0] == '"') && t[len(t)-1] == t[0] && !strings.ContainsAny(t[1:len(t)-1], " \t") {
				emitir(t, "")
			}
		}
	}
}

// ---- palavras traduzidas (B1, exceção) ---------------------------------------------------

// entDoPrefixo: o tipo de um pseudônimo de objeto pelo prefixo ("ns_x" -> namespace).
var entDoPrefixo = func() map[string]string {
	m := map[string]string{"obj": entGenerica}
	for e, p := range EntObjeto {
		m[strings.ToLower(p)] = e
	}
	return m
}()

// PalavrasTraduzidas: as palavras de s que o proxy traduziu de um pseudônimo de objeto (s é
// uma string da entrada de um tool_use, como o Claude Code a reenvia). Sem registro: nada.
func (m *Masker) PalavrasTraduzidas(s string) []PalavraTraduzida {
	tr, ok := m.Traduzidas(s)
	if !ok {
		return nil
	}
	var out []PalavraTraduzida
	for _, t := range tr {
		if t.Ini < 0 || t.Fim > len(s) || t.Fim <= t.Ini {
			continue
		}
		k := strings.IndexByte(t.Pseudo, '_')
		if k <= 0 {
			continue
		}
		e, ok := entDoPrefixo[strings.ToLower(t.Pseudo[:k])]
		if !ok {
			continue
		}
		out = append(out, PalavraTraduzida{s[t.Ini:t.Fim], e})
	}
	return out
}
