package mask

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// O comando diz o que a saída é (ver docs/estruturas.md, seção Saídas soltas de script e de
// shell). O proxy vê a chamada de ferramenta (tool_use) e o resultado (tool_result) do mesmo id:
// o comando tipa as colunas do resultado. Nenhum leitor pode fazer isso, porque um leitor só vê
// o próprio texto; a dica entra como evidência no leitor de tabela que já existe
// (classificarTabelaD) e, para a saída sem cabeçalho (psql -At, cut, uniq -c), numa leitura por
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

// dicaSaida: o que o comando diz das colunas da saída.
type dicaSaida struct {
	cols  []string          // tipo de cada coluna, em ordem ("" = sem tipo); vazio = posição desconhecida
	nomes map[string]string // nome da coluna no cabeçalho (minúsculo) -> tipo
	name  string            // tipo da coluna NAME/NAMES sem tipo próprio
	forte bool              // posição inequívoca (SQL, cabeçalho de arquivo): ensina sozinho
}

func (d *dicaSaida) vazia() bool {
	if d == nil {
		return true
	}
	for _, c := range d.cols {
		if c != "" {
			return false
		}
	}
	return len(d.nomes) == 0 && d.name == ""
}

// codificar: a dica como texto (vai junto do texto na chave da memória de resultados).
func (d *dicaSaida) codificar() string {
	if d.vazia() {
		return ""
	}
	var b strings.Builder
	if d.forte {
		b.WriteString("F")
	} else {
		b.WriteString("f")
	}
	b.WriteString("|" + strings.Join(d.cols, ",") + "|")
	ks := make([]string, 0, len(d.nomes))
	for k := range d.nomes {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for i, k := range ks {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k + ":" + d.nomes[k])
	}
	b.WriteString("|" + d.name)
	return b.String()
}

// sepExt separa, na dica, a parte do leitor de tabela (codificar) da extensão que os decisores
// leem (TextoCtx.Ext).
const sepExt = "\x1e"

// partesDica: a dica do leitor de tabela e a extensão.
func partesDica(s string) (tabela, ext string) {
	t, e, _ := strings.Cut(s, sepExt)
	return t, e
}

// lerDica: o inverso de codificar (nil se vazia ou inválida). A extensão (depois de sepExt) é
// ignorada aqui.
func lerDica(s string) *dicaSaida {
	s, _ = partesDica(s)
	ps := strings.Split(s, "|")
	if len(ps) != 4 {
		return nil
	}
	d := &dicaSaida{forte: ps[0] == "F", name: ps[3], nomes: map[string]string{}}
	if ps[1] != "" {
		d.cols = strings.Split(ps[1], ",")
	}
	if ps[2] != "" {
		for _, kv := range strings.Split(ps[2], ",") {
			if k, v, ok := strings.Cut(kv, ":"); ok {
				d.nomes[k] = v
			}
		}
	}
	if d.vazia() {
		return nil
	}
	return d
}

// tipo: o tipo da coluna k (de n) cujo cabeçalho é h.
func (d *dicaSaida) tipo(h string, k, n int) string {
	l := strings.ToLower(h)
	if t, ok := d.nomes[l]; ok {
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

// Comandos: os comandos de uma conversa, em ordem (o proxy cria um por requisição). Guarda o
// cabeçalho dos arquivos lidos, para a extração de coluna por posição.
type Comandos struct {
	cabs map[string][]string // caminho (e nome do arquivo) -> células do cabeçalho
	cv   conversaCmd         // proveniência, eco e tipo pedido (chamada.go)
}

func NovosComandos() *Comandos { return &Comandos{cabs: map[string][]string{}} }

// Dica: o que o comando diz da saída (texto vazio: nada). saida é o resultado do comando: se
// ele mostra o começo de um arquivo com cabeçalho, o cabeçalho fica lembrado para os comandos
// seguintes.
func (c *Comandos) Dica(cmd, saida string) string {
	if len(cmd) > 8<<10 {
		cmd = cmd[:8<<10]
	}
	d := c.dica(cmd)
	c.lembrarCabecalho(cmd, saida)
	return d.codificar()
}

func (c *Comandos) dica(cmd string) *dicaSaida {
	if d := dicaSQL(cmd); d != nil {
		return d
	}
	if d := c.dicaColuna(cmd); d != nil {
		return d
	}
	return dicaListagem(cmd)
}

// ---- SQL no comando --------------------------------------------------------------------

var (
	reSelectFrom = regexp.MustCompile(`(?is)\bselect\s+(.+?)\s+from\s+((?:[\w$]+|"[^"\n]+"|\x60[^\x60\n]+\x60|\[[^\]\n]+\])(?:\.(?:[\w$]+|"[^"\n]+"|\x60[^\x60\n]+\x60|\[[^\]\n]+\]))*)`)
	reShow       = regexp.MustCompile(`(?i)\bshow\s+(?:terse\s+|full\s+)?([a-z]+)\b`)
	reDescribe   = regexp.MustCompile(`(?i)\b(?:describe|desc)\s+(?:table\s+|view\s+)?[\w$."\x60\[\]]+`)
	reItemAlias  = regexp.MustCompile(`(?is)^(.*?\S)\s+(?:as\s+)?([A-Za-z_][\w$]*|"[^"]+")$`)
	reColSimples = regexp.MustCompile(`^(?:[\w$]+|"[^"]+"|\x60[^\x60]+\x60|\[[^\]]+\])(?:\.(?:[\w$]+|"[^"]+"|\x60[^\x60]+\x60|\[[^\]]+\]))*$`)
)

// dicaSQL: a dica de uma instrução SQL no comando (a última SELECT, ou SHOW/DESCRIBE).
func dicaSQL(cmd string) *dicaSaida {
	if !temAlgum(strings.ToLower(cmd), []string{"select", "show", "desc"}) {
		return nil
	}
	ms := reSelectFrom.FindAllStringSubmatchIndex(cmd, -1)
	if len(ms) > 0 {
		m := ms[len(ms)-1]
		if d := dicaSelect(cmd[m[2]:m[3]], cmd[m[4]:m[5]]); d != nil {
			return d
		}
	}
	if alvoDeSistema(cmd) {
		return nil // descreve ou lista um catálogo de sistema: a estrutura listada é pública
	}
	if m := reShow.FindStringSubmatch(cmd); m != nil {
		if e, ok := entPlural(m[1]); ok {
			return &dicaSaida{nomes: map[string]string{"name": e}, name: e, forte: true}
		}
	}
	if reDescribe.MatchString(cmd) {
		return &dicaSaida{nomes: map[string]string{"name": "coluna"}, name: "coluna", forte: true}
	}
	return nil
}

// entPlural: a entidade de uma palavra de tipo no plural ou no singular ("tables", "schemas").
func entPlural(w string) (string, bool) {
	l := strings.ToLower(w)
	if !pluralTipo[l] && entPedaco[l] == "" {
		return "", false
	}
	e, forte := entChave(l)
	return e, forte && e != ""
}

// dicaSelect: lista do SELECT e o objeto do FROM.
func dicaSelect(lista, from string) *dicaSaida {
	itens := dividirLista(lista)
	if len(itens) == 0 || len(itens) > 60 {
		return nil
	}
	fps := strings.Split(from, ".")
	catalogo := semCitacao(fps[len(fps)-1])
	d := &dicaSaida{nomes: map[string]string{}, forte: true}
	posicional := true
	for k, it := range itens {
		it = strings.TrimSpace(it)
		if k == 0 { // DISTINCT, TOP n
			for _, p := range []*regexp.Regexp{reDistinct, reTop} {
				if m := p.FindStringIndex(it); m != nil {
					it = strings.TrimSpace(it[m[1]:])
				}
			}
		}
		if it == "*" || strings.HasSuffix(it, ".*") {
			posicional = false
			continue
		}
		expr, alias := it, ""
		if !reColSimples.MatchString(it) {
			if m := reItemAlias.FindStringSubmatch(it); m != nil && !publicoSQL(m[2]) {
				expr, alias = m[1], semCitacao(m[2])
			}
		}
		base := ""
		if reColSimples.MatchString(expr) {
			ps := strings.Split(expr, ".")
			base = semCitacao(ps[len(ps)-1])
		}
		e := ""
		if base != "" {
			if t, ok := entCabecalho(base); ok {
				e = t
			} else if l := strings.ToLower(base); l == "name" || l == "nome" {
				e, _ = entPlural(catalogo)
			}
		}
		if e == "" && alias != "" {
			e, _ = entCabecalho(alias)
		}
		d.cols = append(d.cols, e)
		if e == "" {
			continue
		}
		for _, n := range []string{alias, base} {
			if n != "" && reIdentSimples.MatchString(n) {
				d.nomes[strings.ToLower(n)] = e
			}
		}
	}
	if !posicional {
		d.cols = nil
	}
	if d.vazia() {
		return nil
	}
	return d
}

var (
	reDistinct = regexp.MustCompile(`(?i)^(?:distinct|all)\s+`)
	reTop      = regexp.MustCompile(`(?i)^top\s+\(?\d+\)?\s+`)
)

// dividirLista: os itens de uma lista separada por vírgulas, fora de parênteses e aspas.
func dividirLista(s string) []string {
	var out []string
	prof, a := 0, 0
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
			prof++
		case c == ')' || c == ']':
			prof--
		case c == ',' && prof == 0:
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
	reCutCampo = regexp.MustCompile(`(?:-f\s*|--fields=)([\d,\-]+)`)
	reAwk      = regexp.MustCompile(`\b[gnm]?awk\b(?:\s+-F\s*(?:'[^'\n]*'|"[^"\n]*"|\S+))?[^{|\n]*?\{\s*print\s+(\$\d+(?:\s*,\s*\$\d+){0,9})\s*;?\s*\}`)
	reCaminho  = regexp.MustCompile(`[\w~./\-]*[\w\-]\.[A-Za-z][\w]{0,5}\b`)
)

// dicaColuna: cut -fN / awk '{print $N}' sobre um arquivo de cabeçalho conhecido.
func (c *Comandos) dicaColuna(cmd string) *dicaSaida {
	if len(c.cabs) == 0 || !strings.Contains(cmd, "cut") && !strings.Contains(cmd, "awk") {
		return nil
	}
	cab := c.cabecalhoCitado(cmd)
	if cab == nil {
		return nil
	}
	var campos []int
	if m := reCut.FindString(cmd); m != "" {
		if f := reCutCampo.FindStringSubmatch(m); f != nil {
			for _, p := range strings.Split(f[1], ",") {
				a, b, faixa := strings.Cut(p, "-")
				i, err := strconv.Atoi(a)
				if err != nil || i < 1 {
					continue
				}
				j := i
				if faixa {
					if j, err = strconv.Atoi(b); err != nil {
						j = len(cab)
					}
				}
				for ; i <= j && i <= len(cab) && len(campos) < 20; i++ {
					campos = append(campos, i)
				}
			}
		}
	} else if m := reAwk.FindStringSubmatch(cmd); m != nil {
		for _, p := range strings.Split(m[1], ",") {
			if i, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(p), "$")); err == nil && i >= 1 {
				campos = append(campos, i)
			}
		}
	}
	if len(campos) == 0 {
		return nil
	}
	d := &dicaSaida{nomes: map[string]string{}, forte: true}
	for _, i := range campos {
		e := ""
		if i <= len(cab) {
			e, _ = entCabecalho(cab[i-1])
			if e != "" {
				d.nomes[strings.ToLower(cab[i-1])] = e
			}
		}
		d.cols = append(d.cols, e)
	}
	if d.vazia() {
		return nil
	}
	return d
}

// cabecalhoCitado: o cabeçalho de um arquivo conhecido citado no comando.
func (c *Comandos) cabecalhoCitado(cmd string) []string {
	for _, p := range reCaminho.FindAllString(cmd, 20) {
		if cab, ok := c.cabs[p]; ok {
			return cab
		}
		if cab, ok := c.cabs[nomeArquivo(p)]; ok {
			return cab
		}
	}
	return nil
}

func nomeArquivo(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// lembrarCabecalho: a saída começa com o cabeçalho de um arquivo citado no comando ("a,b,c"
// com cada célula um identificador)? Fica lembrado pelo caminho e pelo nome do arquivo.
func (c *Comandos) lembrarCabecalho(cmd, saida string) {
	ps := reCaminho.FindAllString(cmd, 4)
	if len(ps) == 0 || saida == "" {
		return
	}
	if len(saida) > 4096 {
		saida = saida[:4096]
	}
	if n, ok := normalizar(saida); ok {
		saida = n.t
	}
	linha := ""
	for _, l := range strings.Split(saida, "\n") {
		if l = strings.TrimSpace(strings.TrimPrefix(l, "\ufeff")); l != "" {
			linha = l
			break
		}
	}
	for _, sep := range []string{",", "\t", ";", "|"} {
		cs := strings.Split(linha, sep)
		if len(cs) < 2 {
			continue
		}
		ok := true
		for i, v := range cs {
			v = strings.Trim(strings.TrimSpace(v), `"`)
			cs[i] = v
			if !reCelulaIdent.MatchString(v) {
				ok = false
				break
			}
		}
		if !ok {
			return
		}
		for _, p := range ps {
			c.cabs[p] = cs
			c.cabs[nomeArquivo(p)] = cs
		}
		return
	}
}

// ---- listagem de recurso ---------------------------------------------------------------

// tipos de recurso de contêiner e de orquestrador (com as abreviações das próprias CLIs) que a
// palavra de tipo genérica não cobre; o resto vem de entPlural (tables, topics, buckets...)
var recursoListagem = map[string]string{
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
var verbosListagem = conj("get", "list", "ls", "ps")

// dicaListagem: <cli> get|list|ls <tipo>, <cli> <tipo> list|ls, <cli> list-<tipo>, <cli> ps.
func dicaListagem(cmd string) *dicaSaida {
	seg := cmd
	if i := strings.IndexAny(seg, "|;&\n"); i >= 0 {
		seg = seg[:i]
	}
	ts := strings.Fields(seg)
	if len(ts) < 2 || len(ts) > 30 {
		return nil
	}
	tipoDe := func(w string) string {
		w = strings.ToLower(strings.Trim(w, `"'`))
		if i := strings.IndexAny(w, "/,."); i > 0 {
			w = w[:i]
		}
		if e, ok := recursoListagem[w]; ok {
			return e
		}
		if e, ok := entPlural(w); ok && e != "coluna" {
			return e
		}
		return ""
	}
	for i := 1; i < len(ts); i++ {
		v := strings.ToLower(ts[i])
		if strings.HasPrefix(v, "list-") {
			ps := strings.Split(v[5:], "-")
			if e := tipoDe(ps[len(ps)-1]); e != "" {
				return dicaNome(e)
			}
			return nil
		}
		if !verbosListagem[v] {
			continue
		}
		for j := i + 1; j < len(ts); j++ {
			if strings.HasPrefix(ts[j], "-") {
				continue
			}
			if e := tipoDe(ts[j]); e != "" {
				return dicaNome(e)
			}
			break
		}
		if e := tipoDe(ts[i-1]); e != "" && i >= 2 {
			return dicaNome(e)
		}
		if v == "ps" || v == "list" && i == 1 {
			return dicaNome("servico") // docker ps, helm list: contêineres e releases
		}
		return nil
	}
	return nil
}

func dicaNome(e string) *dicaSaida {
	return &dicaSaida{nomes: map[string]string{"name": e, "names": e}, name: e}
}

// ---- a dica aplicada -------------------------------------------------------------------

// publicoDica: o vocabulário dos formatos em que a dica aparece.
func publicoDica(v string) bool { return publicoSQL(v) || publicoDevops(v) }

// acharComDica: as colunas tipadas pela dica, na tabela com cabeçalho (o leitor de tabela, em
// modo dica) e na saída sem cabeçalho (uma linha por registro, o mesmo número de células).
func acharComDica(s string, d *dicaSaida, add func(ObjAchado)) {
	acharTabelasD(s, d, add)
	if len(d.cols) > 0 {
		linhasDica(s, d, add)
	}
}

var (
	reContagem = regexp.MustCompile(`^\s*\d+\s+\S`)
	reRodape   = regexp.MustCompile(`^\(\d+ (?:rows?|linhas?|registros?)(?: affected| afetad[oa]s?)?\)$`)
)

// linhasDica: saída sem cabeçalho (psql -At, cut, awk, sort | uniq -c): toda linha tem o mesmo
// número de células que a dica tem de colunas. Uma linha fora da forma desfaz tudo.
func linhasDica(s string, d *dicaSaida, add func(ObjAchado)) {
	n := len(d.cols)
	type lin struct{ a, b int }
	var ls []lin
	contagem := true
	for _, l := range quebraLinhas(s) {
		a, b := l[0], l[1]
		for a < b && (s[a] == ' ' || s[a] == '\t') {
			a++
		}
		for b > a && (s[b-1] == ' ' || s[b-1] == '\t') {
			b--
		}
		if a == b || reRodape.MatchString(s[a:b]) {
			continue
		}
		ls = append(ls, lin{a, b})
		contagem = contagem && reContagem.MatchString(s[a:b])
		if len(ls) > 20000 {
			return
		}
	}
	if len(ls) == 0 {
		return
	}
	if contagem && n == 1 { // sort | uniq -c: "   12 valor"
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
				if contaFora(s[l.a:l.b], c) != n-1 {
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
	var cels [][]celula
	for _, l := range ls {
		var cs []celula
		switch {
		case sep != 0:
			cs = celulasObj(s, l.a, l.b, sep, nil)
		default:
			cs = tokensLinha(s, l.a, l.b)
		}
		if len(cs) != n {
			return
		}
		cels = append(cels, cs)
	}
	for _, cs := range cels {
		cab := true // a linha do cabeçalho (cut -f2 arquivo.csv imprime o cabeçalho)
		for k, c := range cs {
			if d.tipo(s[c.a:c.b], k, 0) == "" || d.nomes[strings.ToLower(s[c.a:c.b])] == "" {
				cab = false
			}
		}
		if cab {
			continue
		}
		for k, c := range cs {
			if e := d.cols[k]; e != "" {
				celulaDica(s, c, e, d.forte, add)
			}
		}
	}
}

// celulaDica: o valor de uma coluna tipada pela dica (os freios da célula de catálogo).
func celulaDica(s string, c celula, e string, forte bool, add func(ObjAchado)) {
	if c.a < 0 || c.b <= c.a {
		return
	}
	v := strings.Trim(s[c.a:c.b], `"'`)
	a := c.a + strings.Index(s[c.a:c.b], v)
	if v == "" || !reCelulaIdent.MatchString(v) || strings.EqualFold(v, "null") || strings.EqualFold(v, "none") ||
		ehTipoDado(v) || strings.Trim(v, ".-") == "" || !forte && !caraDeIdentificador(v) {
		return
	}
	ps := strings.Split(v, ".")
	ents := entQual(len(ps), e)
	for k, p := range ps {
		if p != "" && !publicoDica(p) && reIdentSimples.MatchString(p) {
			add(ObjAchado{a, a + len(p), ents[k], "comando", forte && ents[k] != "coluna" && len(p) >= 5})
		}
		a += len(p) + 1
	}
}

// catalogosSistema: namespaces que são de sistema em toda instalação (os nomes de dentro deles
// são documentação pública). public e dbo ficam de fora: guardam os objetos do cliente.
var catalogosSistema = conj("information_schema", "pg_catalog", "pg_toast", "performance_schema", "sys",
	"sysibm", "syscat", "sysstat", "snowflake", "account_usage", "organization_usage", "data_sharing_usage",
	"readers_account_usage", "master", "msdb", "mysql")

var reAlvoDescShow = regexp.MustCompile(`(?i)\b(?:describe|desc)\s+(?:table\s+|view\s+)?([\w$."]+)|\bshow\b[^;\n]*?\bin\s+(?:account\s+|database\s+|schema\s+)?([\w$."]+)`)

// alvoDeSistema: o comando descreve (DESC) ou lista (SHOW ... IN) um objeto que está num
// catálogo de sistema (SNOWFLAKE.ACCOUNT_USAGE.TAG_REFERENCES, information_schema.columns).
// Só quando TODAS as instruções do comando são isso: um DESC de sistema junto com um SELECT (ou
// um SHOW sem IN, um DESC de tabela do cliente) no mesmo comando traz dado do cliente na saída.
func alvoDeSistema(cmd string) bool {
	sis := false
	for _, inst := range strings.Split(cmd, ";") {
		ms := reAlvoDescShow.FindAllStringSubmatchIndex(inst, -1)
		resto := inst
		for k := len(ms) - 1; k >= 0; k-- {
			m := ms[k]
			alvo := ""
			for _, g := range [][2]int{{m[2], m[3]}, {m[4], m[5]}} {
				if g[0] >= 0 {
					alvo += inst[g[0]:g[1]]
				}
			}
			if !objetoDeSistema(alvo) {
				return false
			}
			resto = resto[:m[0]] + resto[m[1]:]
		}
		if reInstrucaoSQL.MatchString(resto) {
			return false // outra instrução no mesmo trecho (SELECT, SHOW sem IN...)
		}
		sis = sis || len(ms) > 0
	}
	return sis
}

// objetoDeSistema: alguma parte do nome qualificado é um catálogo de sistema.
func objetoDeSistema(alvo string) bool {
	for _, p := range strings.Split(alvo, ".") {
		if catalogosSistema[strings.ToLower(strings.Trim(p, `"`))] {
			return true
		}
	}
	return false
}

// reInstrucaoSQL: palavra que começa uma instrução que lê ou muda dado.
var reInstrucaoSQL = regexp.MustCompile(`(?i)\b(?:select|with|insert|update|delete|merge|copy|call|execute|show|desc|describe|list|get|put|values)\b`)
