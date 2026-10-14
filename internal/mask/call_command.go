package mask

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// O comando diz o que a saída é (ver docs/pt-BR/structures.md, seção Saídas soltas de script e de
// shell). O proxy vê a chamada de ferramenta (tool_use) e o resultado (tool_result) do mesmo id:
// o comando tipa as colunas do resultado. Nenhum leitor pode fazer isso, porque um leitor só vê
// o próprio texto; a dica entra como evidência no leitor de tabela que já existe
// (classifyTableD) e, para a saída sem cabeçalho (psql -At, cut, uniq -c), numa leitura por
// linha com o mesmo número de células em todas as linhas.
//
// Três formas, nenhuma por ferramenta:
//   - SQL no comando: a lista do SELECT dá o tipo de cada coluna, em ordem e pelo nome (alias
//     ou coluna); "name" vale pelo catálogo do FROM (sys.tables -> tabela); SHOW <tipos> dá o
//     tipo da coluna name.
//   - extração de coluna por posição (cut -fN, awk '{print $N}') sobre um arquivo cujo
//     cabeçalho já passou pela conversa: a coluna N tem o tipo daquele cabeçalho.
//   - listagem de recurso (<cli> get|list|ls <tipo>, <tipo> list, list-<tipo>, <cli> ps): o
//     tipo pedido diz o tipo da coluna NAME. É evidência fraca: mascara no lugar e só ensina
//     com uma segunda regra.

// outputHint: o que o comando diz das colunas da saída.
type outputHint struct {
	cols   []string          // tipo de cada coluna, em ordem ("" = sem tipo); vazio = posição desconhecida
	names  map[string]string // nome da coluna no cabeçalho (minúsculo) -> tipo
	name   string            // tipo da coluna NAME/NAMES sem tipo próprio
	strong bool              // posição inequívoca (SQL, cabeçalho de arquivo): ensina sozinho
}

func (d *outputHint) empty() bool {
	if d == nil {
		return true
	}
	for _, c := range d.cols {
		if c != "" {
			return false
		}
	}
	return len(d.names) == 0 && d.name == ""
}

// codificar: a dica como texto (vai junto do texto na chave da memória de resultados).
func (d *outputHint) encode() string {
	if d.empty() {
		return ""
	}
	var b strings.Builder
	if d.strong {
		b.WriteString("F")
	} else {
		b.WriteString("f")
	}
	b.WriteString("|" + strings.Join(d.cols, ",") + "|")
	ks := make([]string, 0, len(d.names))
	for k := range d.names {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for i, k := range ks {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k + ":" + d.names[k])
	}
	b.WriteString("|" + d.name)
	return b.String()
}

// sepExt separa, na dica, a parte do leitor de tabela (codificar) da extensão que os decisores
// leem (TextCtx.Ext).
const sepExt = "\x1e"

// hintParts: a dica do leitor de tabela e a extensão.
func hintParts(s string) (table, ext string) {
	t, e, _ := strings.Cut(s, sepExt)
	return t, e
}

// readHint: o inverso de codificar (nil se vazia ou inválida). A extensão (depois de sepExt) é
// ignorada aqui.
func readHint(s string) *outputHint {
	s, _ = hintParts(s)
	ps := strings.Split(s, "|")
	if len(ps) != 4 {
		return nil
	}
	d := &outputHint{strong: ps[0] == "F", name: ps[3], names: map[string]string{}}
	if ps[1] != "" {
		d.cols = strings.Split(ps[1], ",")
	}
	if ps[2] != "" {
		for _, kv := range strings.Split(ps[2], ",") {
			if k, v, ok := strings.Cut(kv, ":"); ok {
				d.names[k] = v
			}
		}
	}
	if d.empty() {
		return nil
	}
	return d
}

// tipo: o tipo da coluna k (de n) cujo cabeçalho é h.
func (d *outputHint) typ(h string, k, n int) string {
	l := strings.ToLower(h)
	if t, ok := d.names[l]; ok {
		return t
	}
	if n > 0 && len(d.cols) == n && d.cols[k] != "" {
		return d.cols[k]
	}
	if (l == "name" || l == "names" || l == "nome") && d.name != "" {
		return d.name
	}
	return ""
}

// Commands: os comandos de uma conversa, em ordem (o proxy cria um por requisição). Guarda o
// cabeçalho dos arquivos lidos, para a extração de coluna por posição.
type Commands struct {
	hdrs map[string][]string // caminho (e nome do arquivo) -> células do cabeçalho
	cv   conversationCmd     // proveniência, eco e tipo pedido (call.go)
}

func NewCommands() *Commands { return &Commands{hdrs: map[string][]string{}} }

// Hint: o que o comando diz da saída (texto vazio: nada). saida é o resultado do comando: se
// ele mostra o começo de um arquivo com cabeçalho, o cabeçalho fica lembrado para os comandos
// seguintes.
func (c *Commands) Hint(cmd, output string) string {
	if len(cmd) > 8<<10 {
		cmd = cmd[:8<<10]
	}
	d := c.hint(cmd)
	c.rememberHeader(cmd, output)
	return d.encode()
}

func (c *Commands) hint(cmd string) *outputHint {
	if d := sqlHint(cmd); d != nil {
		return d
	}
	if d := c.columnHint(cmd); d != nil {
		return d
	}
	return listingHint(cmd)
}

// ---- SQL no comando --------------------------------------------------------------------

var (
	reSelectFrom = regexp.MustCompile(`(?is)\bselect\s+(.+?)\s+from\s+((?:[\w$]+|"[^"\n]+"|\x60[^\x60\n]+\x60|\[[^\]\n]+\])(?:\.(?:[\w$]+|"[^"\n]+"|\x60[^\x60\n]+\x60|\[[^\]\n]+\]))*)`)
	reShow       = regexp.MustCompile(`(?i)\bshow\s+(?:terse\s+|full\s+)?([a-z]+)\b`)
	reDescribe   = regexp.MustCompile(`(?i)\b(?:describe|desc)\s+(?:table\s+|view\s+)?[\w$."\x60\[\]]+`)
	reItemAlias  = regexp.MustCompile(`(?is)^(.*?\S)\s+(?:as\s+)?([A-Za-z_][\w$]*|"[^"]+")$`)
	reSimpleCol  = regexp.MustCompile(`^(?:[\w$]+|"[^"]+"|\x60[^\x60]+\x60|\[[^\]]+\])(?:\.(?:[\w$]+|"[^"]+"|\x60[^\x60]+\x60|\[[^\]]+\]))*$`)
)

// sqlHint: a dica de uma instrução SQL no comando (a última SELECT, ou SHOW/DESCRIBE).
func sqlHint(cmd string) *outputHint {
	if !hasAny(strings.ToLower(cmd), []string{"select", "show", "desc"}) {
		return nil
	}
	ms := reSelectFrom.FindAllStringSubmatchIndex(cmd, -1)
	if len(ms) > 0 {
		m := ms[len(ms)-1]
		if d := selectHint(cmd[m[2]:m[3]], cmd[m[4]:m[5]]); d != nil {
			return d
		}
	}
	if systemTarget(cmd) {
		return nil // descreve ou lista um catálogo de sistema: a estrutura listada é pública
	}
	if m := reShow.FindStringSubmatch(cmd); m != nil {
		if e, ok := pluralEnt(m[1]); ok {
			return &outputHint{names: map[string]string{"name": e}, name: e, strong: true}
		}
	}
	if reDescribe.MatchString(cmd) {
		return &outputHint{names: map[string]string{"name": "coluna"}, name: "coluna", strong: true}
	}
	return nil
}

// pluralEnt: a entidade de uma palavra de tipo no plural ou no singular ("tables", "schemas").
func pluralEnt(w string) (string, bool) {
	l := strings.ToLower(w)
	if !pluralType[l] && entPiece[l] == "" {
		return "", false
	}
	e, strong := entKey(l)
	return e, strong && e != ""
}

// selectHint: lista do SELECT e o objeto do FROM.
func selectHint(list, from string) *outputHint {
	items := splitList(list)
	if len(items) == 0 || len(items) > 60 {
		return nil
	}
	fps := strings.Split(from, ".")
	catalog := unquoted(fps[len(fps)-1])
	d := &outputHint{names: map[string]string{}, strong: true}
	positional := true
	for k, it := range items {
		it = strings.TrimSpace(it)
		if k == 0 { // DISTINCT, TOP n
			for _, p := range []*regexp.Regexp{reDistinct, reTop} {
				if m := p.FindStringIndex(it); m != nil {
					it = strings.TrimSpace(it[m[1]:])
				}
			}
		}
		if it == "*" || strings.HasSuffix(it, ".*") {
			positional = false
			continue
		}
		expr, alias := it, ""
		if !reSimpleCol.MatchString(it) {
			if m := reItemAlias.FindStringSubmatch(it); m != nil && !publicSQL(m[2]) {
				expr, alias = m[1], unquoted(m[2])
			}
		}
		base := ""
		if reSimpleCol.MatchString(expr) {
			ps := strings.Split(expr, ".")
			base = unquoted(ps[len(ps)-1])
		}
		e := ""
		if base != "" {
			if t, ok := entHeader(base); ok {
				e = t
			} else if l := strings.ToLower(base); l == "name" || l == "nome" {
				e, _ = pluralEnt(catalog)
			}
		}
		if e == "" && alias != "" {
			e, _ = entHeader(alias)
		}
		d.cols = append(d.cols, e)
		if e == "" {
			continue
		}
		for _, n := range []string{alias, base} {
			if n != "" && reSimpleIdent.MatchString(n) {
				d.names[strings.ToLower(n)] = e
			}
		}
	}
	if !positional {
		d.cols = nil
	}
	if d.empty() {
		return nil
	}
	return d
}

var (
	reDistinct = regexp.MustCompile(`(?i)^(?:distinct|all)\s+`)
	reTop      = regexp.MustCompile(`(?i)^top\s+\(?\d+\)?\s+`)
)

// splitList: os itens de uma lista separada por vírgulas, fora de parênteses e aspas.
func splitList(s string) []string {
	var out []string
	depth, a := 0, 0
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '\'' || c == '"' || c == '`':
			q = c
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case c == ',' && depth == 0:
			out = append(out, s[a:i])
			a = i + 1
		}
	}
	return append(out, s[a:])
}

// ---- extração de coluna por posição ----------------------------------------------------

var (
	reCut      = regexp.MustCompile(`\bcut\b(?:'[^'\n]*'|"[^"\n]*"|[^|;&\n'"])*`)
	reCutDelim = regexp.MustCompile(`(?:-d\s*|--delimiter=)(?:'([^']{1,4})'|"([^"]{1,4})"|(\S))`)
	reCutField = regexp.MustCompile(`(?:-f\s*|--fields=)([\d,\-]+)`)
	reAwk      = regexp.MustCompile(`\b[gnm]?awk\b(?:\s+-F\s*(?:'[^'\n]*'|"[^"\n]*"|\S+))?[^{|\n]*?\{\s*print\s+(\$\d+(?:\s*,\s*\$\d+){0,9})\s*;?\s*\}`)
	rePath     = regexp.MustCompile(`[\w~./\-]*[\w\-]\.[A-Za-z][\w]{0,5}\b`)
)

// columnHint: cut -fN / awk '{print $N}' sobre um arquivo de cabeçalho conhecido.
func (c *Commands) columnHint(cmd string) *outputHint {
	if len(c.hdrs) == 0 || !strings.Contains(cmd, "cut") && !strings.Contains(cmd, "awk") {
		return nil
	}
	hdr := c.quotedHeader(cmd)
	if hdr == nil {
		return nil
	}
	var fields []int
	if m := reCut.FindString(cmd); m != "" {
		if f := reCutField.FindStringSubmatch(m); f != nil {
			for _, p := range strings.Split(f[1], ",") {
				a, b, rng := strings.Cut(p, "-")
				i, err := strconv.Atoi(a)
				if err != nil || i < 1 {
					continue
				}
				j := i
				if rng {
					if j, err = strconv.Atoi(b); err != nil {
						j = len(hdr)
					}
				}
				for ; i <= j && i <= len(hdr) && len(fields) < 20; i++ {
					fields = append(fields, i)
				}
			}
		}
	} else if m := reAwk.FindStringSubmatch(cmd); m != nil {
		for _, p := range strings.Split(m[1], ",") {
			if i, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(p), "$")); err == nil && i >= 1 {
				fields = append(fields, i)
			}
		}
	}
	if len(fields) == 0 {
		return nil
	}
	d := &outputHint{names: map[string]string{}, strong: true}
	for _, i := range fields {
		e := ""
		if i <= len(hdr) {
			e, _ = entHeader(hdr[i-1])
			if e != "" {
				d.names[strings.ToLower(hdr[i-1])] = e
			}
		}
		d.cols = append(d.cols, e)
	}
	if d.empty() {
		return nil
	}
	return d
}

// quotedHeader: o cabeçalho de um arquivo conhecido citado no comando.
func (c *Commands) quotedHeader(cmd string) []string {
	for _, p := range rePath.FindAllString(cmd, 20) {
		if hdr, ok := c.hdrs[p]; ok {
			return hdr
		}
		if hdr, ok := c.hdrs[fileNameOf(p)]; ok {
			return hdr
		}
	}
	return nil
}

func fileNameOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// rememberHeader: a saída começa com o cabeçalho de um arquivo citado no comando ("a,b,c"
// com cada célula um identificador)? Fica lembrado pelo caminho e pelo nome do arquivo.
func (c *Commands) rememberHeader(cmd, output string) {
	ps := rePath.FindAllString(cmd, 4)
	if len(ps) == 0 || output == "" {
		return
	}
	if len(output) > 4096 {
		output = output[:4096]
	}
	if n, ok := normalize(output); ok {
		output = n.t
	}
	line := ""
	for _, l := range strings.Split(output, "\n") {
		if l = strings.TrimSpace(strings.TrimPrefix(l, "\ufeff")); l != "" {
			line = l
			break
		}
	}
	for _, sep := range []string{",", "\t", ";", "|"} {
		cs := strings.Split(line, sep)
		if len(cs) < 2 {
			continue
		}
		ok := true
		for i, v := range cs {
			v = strings.Trim(strings.TrimSpace(v), `"`)
			cs[i] = v
			if !reIdentCell.MatchString(v) {
				ok = false
				break
			}
		}
		if !ok {
			return
		}
		for _, p := range ps {
			c.hdrs[p] = cs
			c.hdrs[fileNameOf(p)] = cs
		}
		return
	}
}

// ---- listagem de recurso ---------------------------------------------------------------

// tipos de recurso de contêiner e de orquestrador (com as abreviações das próprias CLIs) que a
// palavra de tipo genérica não cobre; o resto vem de pluralEnt (tables, topics, buckets...)
var listingResource = map[string]string{
	"pod": "servico", "pods": "servico", "po": "servico", "deployment": "servico", "deployments": "servico",
	"deploy": "servico", "statefulset": "servico", "statefulsets": "servico", "sts": "servico",
	"daemonset": "servico", "daemonsets": "servico", "ds": "servico", "replicaset": "servico",
	"replicasets": "servico", "rs": "servico", "job": "servico", "jobs": "servico", "cronjob": "servico",
	"cronjobs": "servico", "cj": "servico", "svc": "servico", "services": "servico", "service": "servico",
	"release": "servico", "releases": "servico", "container": "servico", "containers": "servico",
	"ingress": "servico", "ingresses": "servico", "ing": "servico", "configmap": "servico",
	"configmaps": "servico", "cm": "servico", "secret": "servico", "secrets": "servico",
	"ns": "namespace", "namespaces": "namespace", "node": "servidor", "nodes": "servidor", "no": "servidor",
	"instances": "servidor", "clusters": "servidor", "vm": "servidor", "vms": "servidor",
	"apps": "servico", "functions": "servico", "groups": "servico", "repos": "repositorio",
}

// verbos de listagem (como subcomando, nunca o próprio programa: "ls" e "ps" sozinhos são do shell)
var listingVerbs = setOf("get", "list", "ls", "ps")

// listingHint: <cli> get|list|ls <tipo>, <cli> <tipo> list|ls, <cli> list-<tipo>, <cli> ps.
func listingHint(cmd string) *outputHint {
	sgm := cmd
	if i := strings.IndexAny(sgm, "|;&\n"); i >= 0 {
		sgm = sgm[:i]
	}
	ts := strings.Fields(sgm)
	if len(ts) < 2 || len(ts) > 30 {
		return nil
	}
	typeOf := func(w string) string {
		w = strings.ToLower(strings.Trim(w, `"'`))
		if i := strings.IndexAny(w, "/,."); i > 0 {
			w = w[:i]
		}
		if e, ok := listingResource[w]; ok {
			return e
		}
		if e, ok := pluralEnt(w); ok && e != "coluna" {
			return e
		}
		return ""
	}
	// o tipo listado: o do vocabulário ou, para qualquer outro substantivo no plural ("dags",
	// "projects", "pipelines"), objeto nomeado (servico)
	hintOf := func(w string) *outputHint {
		if e := typeOf(w); e != "" {
			return nameHint(e)
		}
		if pluralNoun(w) {
			return identityHint("servico", w)
		}
		return nil
	}
	for i := 1; i < len(ts); i++ {
		v := strings.ToLower(ts[i])
		if strings.HasPrefix(v, "list-") {
			ps := strings.Split(v[5:], "-")
			return hintOf(ps[len(ps)-1])
		}
		if !listingVerbs[v] {
			continue
		}
		for j := i + 1; j < len(ts); j++ {
			if strings.HasPrefix(ts[j], "-") {
				continue
			}
			if d := hintOf(ts[j]); d != nil {
				return d
			}
			break
		}
		if i >= 2 {
			if d := hintOf(ts[i-1]); d != nil {
				return d
			}
		}
		if v == "ps" || v == "list" && i == 1 {
			return nameHint("servico") // docker ps, helm list: contêineres e releases
		}
		return nil
	}
	return nil
}

func nameHint(e string) *outputHint {
	return &outputHint{names: map[string]string{"name": e, "names": e}, name: e}
}

// identityHint: a listagem de um tipo fora do vocabulário tipa só a coluna de identidade
// composta, o tipo no singular com _id ou _name (dags -> dag_id, projects -> project_id,
// clusters -> cluster_name). NAME ou o tipo sozinho não: "UNIT" de systemctl list-units não diz
// que o valor é nome de alguém.
func identityHint(e, typ string) *outputHint {
	d := &outputHint{names: map[string]string{}}
	t := strings.ToLower(strings.Trim(typ, `"'`))
	switch {
	case strings.HasSuffix(t, "ies"):
		t = t[:len(t)-3] + "y"
	case strings.HasSuffix(t, "sses"), strings.HasSuffix(t, "xes"):
		t = t[:len(t)-2]
	case strings.HasSuffix(t, "s"):
		t = t[:len(t)-1]
	}
	if len(t) >= 2 {
		for _, suf := range []string{"_id", "_name", "id", "name", "-id", "-name"} {
			d.names[t+suf] = e
		}
	}
	return d
}

// pluralNoun: uma palavra em minúsculas terminada em "s" (não "ss"): o tipo listado.
func pluralNoun(w string) bool {
	w = strings.Trim(w, `"'`)
	if len(w) < 4 || w[len(w)-1] != 's' || w[len(w)-2] == 's' {
		return false
	}
	for i := 0; i < len(w); i++ {
		if c := w[i]; !(c >= 'a' && c <= 'z' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// ---- a dica aplicada -------------------------------------------------------------------

// publicHint: o vocabulário dos formatos em que a dica aparece.
func publicHint(v string) bool { return publicSQL(v) || publicDevops(v) }

// findWithHint: as colunas tipadas pela dica, na tabela com cabeçalho (o leitor de tabela, em
// modo dica) e na saída sem cabeçalho (uma linha por registro, o mesmo número de células).
func findWithHint(s string, d *outputHint, add func(ObjFinding)) {
	findTablesD(s, d, add)
	if len(d.cols) > 0 {
		hintLines(s, d, add)
	}
}

var (
	reCount  = regexp.MustCompile(`^\s*\d+\s+\S`)
	reFooter = regexp.MustCompile(`^\(\d+ (?:rows?|linhas?|registros?)(?: affected| afetad[oa]s?)?\)$`)
)

// hintLines: saída sem cabeçalho (psql -At, cut, awk, sort | uniq -c): toda linha tem o mesmo
// número de células que a dica tem de colunas. Uma linha fora da forma desfaz tudo.
func hintLines(s string, d *outputHint, add func(ObjFinding)) {
	n := len(d.cols)
	type lineV struct{ a, b int }
	var ls []lineV
	counting := true
	for _, l := range wrapLines(s) {
		a, b := l[0], l[1]
		for a < b && (s[a] == ' ' || s[a] == '\t') {
			a++
		}
		for b > a && (s[b-1] == ' ' || s[b-1] == '\t') {
			b--
		}
		if a == b || reFooter.MatchString(s[a:b]) {
			continue
		}
		ls = append(ls, lineV{a, b})
		counting = counting && reCount.MatchString(s[a:b])
		if len(ls) > 20000 {
			return
		}
	}
	if len(ls) == 0 {
		return
	}
	if counting && n == 1 { // sort | uniq -c: "   12 valor"
		for i, l := range ls {
			k := l.a
			for k < l.b && s[k] >= '0' && s[k] <= '9' {
				k++
			}
			for k < l.b && (s[k] == ' ' || s[k] == '\t') {
				k++
			}
			ls[i].a = k
		}
	}
	var sep byte
	if n > 1 {
		for _, c := range []byte{'|', '\t', ',', ';'} {
			ok := true
			for _, l := range ls {
				if countOutside(s[l.a:l.b], c) != n-1 {
					ok = false
					break
				}
			}
			if ok {
				sep = c
				break
			}
		}
	}
	var cells [][]cell
	for _, l := range ls {
		var cs []cell
		switch {
		case sep != 0:
			cs = objCells(s, l.a, l.b, sep, nil)
		default:
			cs = lineTokens(s, l.a, l.b)
		}
		if len(cs) != n {
			return
		}
		cells = append(cells, cs)
	}
	for _, cs := range cells {
		hdr := true // a linha do cabeçalho (cut -f2 arquivo.csv imprime o cabeçalho)
		for k, c := range cs {
			if d.typ(s[c.a:c.b], k, 0) == "" || d.names[strings.ToLower(s[c.a:c.b])] == "" {
				hdr = false
			}
		}
		if hdr {
			continue
		}
		for k, c := range cs {
			if e := d.cols[k]; e != "" {
				hintCell(s, c, e, d.strong, add)
			}
		}
	}
}

// hintCell: o valor de uma coluna tipada pela dica (os freios da célula de catálogo).
func hintCell(s string, c cell, e string, strong bool, add func(ObjFinding)) {
	if c.a < 0 || c.b <= c.a {
		return
	}
	v := strings.Trim(s[c.a:c.b], `"'`)
	a := c.a + strings.Index(s[c.a:c.b], v)
	if v == "" || !reIdentCell.MatchString(v) || strings.EqualFold(v, "null") || strings.EqualFold(v, "none") ||
		isDataType(v) || strings.Trim(v, ".-") == "" || !strong && !identifierShape(v) {
		return
	}
	ps := strings.Split(v, ".")
	ents := entQual(len(ps), e)
	for k, p := range ps {
		if p != "" && !publicHint(p) && reSimpleIdent.MatchString(p) {
			add(ObjFinding{a, a + len(p), ents[k], "comando", strong && ents[k] != "coluna" && len(p) >= 5})
		}
		a += len(p) + 1
	}
}

// systemCatalogs: namespaces que são de sistema em toda instalação (os nomes de dentro deles
// são documentação pública). public e dbo ficam de fora: guardam os objetos do cliente.
var systemCatalogs = setOf("information_schema", "pg_catalog", "pg_toast", "performance_schema", "sys",
	"sysibm", "syscat", "sysstat", "snowflake", "account_usage", "organization_usage", "data_sharing_usage",
	"readers_account_usage", "master", "msdb", "mysql")

var reDescShowTarget = regexp.MustCompile(`(?i)\b(?:describe|desc)\s+(?:table\s+|view\s+)?([\w$."]+)|\bshow\b[^;\n]*?\bin\s+(?:account\s+|database\s+|schema\s+)?([\w$."]+)`)

// systemTarget: o comando descreve (DESC) ou lista (SHOW ... IN) um objeto que está num
// catálogo de sistema (SNOWFLAKE.ACCOUNT_USAGE.TAG_REFERENCES, information_schema.columns).
// Só quando TODAS as instruções do comando são isso: um DESC de sistema junto com um SELECT (ou
// um SHOW sem IN, um DESC de tabela do cliente) no mesmo comando traz dado do cliente na saída.
func systemTarget(cmd string) bool {
	sysName := false
	for _, inst := range strings.Split(cmd, ";") {
		ms := reDescShowTarget.FindAllStringSubmatchIndex(inst, -1)
		rest := inst
		for k := len(ms) - 1; k >= 0; k-- {
			m := ms[k]
			target := ""
			for _, g := range [][2]int{{m[2], m[3]}, {m[4], m[5]}} {
				if g[0] >= 0 {
					target += inst[g[0]:g[1]]
				}
			}
			if !systemObject(target) {
				return false
			}
			rest = rest[:m[0]] + rest[m[1]:]
		}
		if reSQLStatement.MatchString(rest) {
			return false // outra instrução no mesmo trecho (SELECT, SHOW sem IN...)
		}
		sysName = sysName || len(ms) > 0
	}
	return sysName
}

// systemObject: alguma parte do nome qualificado é um catálogo de sistema.
func systemObject(target string) bool {
	for _, p := range strings.Split(target, ".") {
		if systemCatalogs[strings.ToLower(strings.Trim(p, `"`))] {
			return true
		}
	}
	return false
}

// reSQLStatement: palavra que começa uma instrução que lê ou muda dado.
var reSQLStatement = regexp.MustCompile(`(?i)\b(?:select|with|insert|update|delete|merge|copy|call|execute|show|desc|describe|list|get|put|values)\b`)
