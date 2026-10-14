package mask

import (
	"regexp"
	"sort"
	"strings"
)

// Rastreamento (ver docs/pt-BR/structures.md): cada nome do cliente é decidido onde ENTRA na conversa
// e, a partir daí, é mascarado em todo lugar. Três regras:
//
//  1. Entry: só os dados (saída de comando, arquivo lido) e o texto do usuário trazem nome do
//     cliente. O texto que o assistente escreveu não é fonte: o modelo só conhece o cliente pelo
//     que o proxy deixou passar; palavra nova dele é conhecimento público (MaskContagion).
//  2. Contágio: o valor decidido vale em todas as ocorrências, em qualquer texto e formato.
//  3. Limpeza: vocabulário público nunca é marcado (publicSystem, publicGeneral).
//
// Cada marca leva a origem: decidida com prova direta (leitor, decisor) ou deduzida (âncora por
// posição). Dedução não gera dedução (sem cascata), e só vale com sementes provadas na mesma
// fonte (o arquivo ou recurso que o comando leu): quatro nomes de tabela que coincidem com
// pacotes de um constraints.txt não fazem a lista inteira virar tabela. Word comum provada numa
// fonte não contamina outro arquivo lido; na conversa e nas saídas sem arquivo ela é o nome.

// adicionar: põe d na memória (sem repetir). Devolve se entrou.
func (mm *memory) addEntry(m *Masker, d Decision) bool {
	if d.Generic || len(d.Name) < memMin || publicGeneral(d.Name) || m.provenSoftware(d.Name) || !m.objMask(d.Ent) || isPseudoObj(d.Name) {
		return false
	}
	if mm.has == nil {
		mm.has = map[string]bool{}
	}
	k := d.Key()
	if mm.has[k] {
		return false
	}
	i := 0
	for i < len(d.Name) && !isWord(d.Name[i]) {
		i++
	}
	j := i
	for j < len(d.Name) && isWord(d.Name[j]) {
		j++
	}
	if i == j {
		return false
	}
	mm.has[k] = true
	nm := memName{name: d.Name, off: i, ent: d.Ent, caseless: entSQL[d.Ent]}
	idx, w := mm.exact, d.Name[i:j]
	if nm.caseless {
		idx, w = mm.caseless, strings.ToLower(w)
		mm.caselessSize |= bitSize(j - i)
	} else {
		mm.exactSize |= bitSize(j - i)
	}
	ns := append(idx[w], nm)
	sort.SliceStable(ns, func(a, b int) bool { return len(ns[a].name) > len(ns[b].name) })
	idx[w] = ns
	mm.n++
	return true
}

// learnAnchor: os trechos novos de ts2 (fora de antes) são decisões de valor (regra 2):
// entram na memória e ficam marcados como deduzidos. Devolve as que entraram.
func (l *Batch) learnAnchor(s string, before, ts2 []snippet) []Decision {
	already := map[[2]int]bool{}
	for _, t := range before {
		already[[2]int{t.Start, t.End}] = true
	}
	var out []Decision
	for _, t := range ts2 {
		if already[[2]int{t.Start, t.End}] || !isObject(t.Type) || t.Type == objTypePrefix+genericEnt {
			continue
		}
		v := s[t.Start:t.End]
		if l.fromModel(v) {
			continue
		}
		d := Decision{Name: v, Ent: t.Type[len(objTypePrefix):], Rule: "âncora", Generic: isGeneric(v)}
		l.markDeduced(v)
		if l.mem.addEntry(l.m, d) {
			out = append(out, d)
		}
	}
	return out
}

func (l *Batch) markDeduced(v string) {
	if l.deducedSet == nil {
		l.deducedSet = map[string]bool{}
	}
	l.deducedSet[strings.ToLower(v)] = true
}

func (l *Batch) deduced(v string) bool { return l.deducedSet[strings.ToLower(v)] }

// MaskContagion (regra 1): texto que o assistente escreveu e que não bate com o registro do
// que o proxy traduziu. Só recebe o contágio: os detectores de formato (CPF, chave, e-mail:
// valem por si), os valores já marcados e a memória da conversa. Nenhum leitor de estrutura nem
// decisor roda aqui, e nada é aprendido.
func (l *Batch) MaskContagion(s string, fromWeb bool, pos Position) (string, []Entry) {
	if len(s) < 4 {
		return s, nil
	}
	if r, ok := l.m.frozen(pos, s); ok {
		return r.text, r.entries
	}
	out := l.m.detectBase(s)
	longNum, _ := numericProfile(s)
	l.m.findKnown(s, longNum, func(ini, end int, typ string) {
		out = append(out, Finding{ini, end, typ, s[ini:end]})
	})
	l.m.unifyObjects(out)
	text, ents, ts := l.m.applyT(s, out)
	l.writing, l.currentSource = true, ""
	r := l.withMemory(s, result{text: text, entries: ents, snippets: ts, decidedSet: []Decision{}}, "")
	l.writing = false
	if _, already := l.outputs[pos]; !already {
		l.outputs[pos] = r
	}
	return r.text, r.entries
}

// publicSystem (regra 3): vocabulário de sistema (SQL, desenvolvimento, devops, bancos); nunca
// é mascarado, nem no lugar.
func publicSystem(v string) bool {
	return publicSQL(v) || publicDev(v) || publicDevops(v) || databaseSchemes[strings.ToLower(v)]
}

// publicGeneral: o de sistema e a referência medida (publicRef). A referência, com prova, é
// mascarada no lugar, mas não se espalha.
func publicGeneral(v string) bool { return publicSystem(v) || publicRef[strings.ToLower(v)] }

// wholeValue (só o valor inteiro é candidato): s[a:b] é o valor inteiro do seu token, e não um
// pedaço de uma expressão. Um literal entre aspas é um valor; fora disso, o token vai até espaço
// ou separador de campo (, ; | / : =), sem as aspas e os parênteses que o embrulham.
// "pagamentos" numa coluna é; "CPF" em '.*CPF.*' ou em (CPF|CNPJ) não é.
func wholeValue(s string, a, b int) bool {
	if a > 0 && b < len(s) && s[a-1] == s[b] && strings.IndexByte("'\"`", s[b]) >= 0 {
		return true
	}
	sep := func(c byte) bool { return strings.IndexByte(" \t\r\n,;|/:=", c) >= 0 }
	i, j := a, b
	for i > 0 && !sep(s[i-1]) {
		i--
	}
	for j < len(s) && !sep(s[j]) {
		j++
	}
	for i < a && strings.IndexByte("'\"`([{", s[i]) >= 0 {
		i++
	}
	for j > b && strings.IndexByte("'\"`)]}.", s[j-1]) >= 0 {
		j--
	}
	return i == a && j == b
}

// countOutsideQuotes: quantos c há em l fora de aspas simples ou duplas (fechadas na linha).
func countOutsideQuotes(l string, c byte) int {
	n := 0
	var q byte
	for i := 0; i < len(l); i++ {
		switch {
		case q != 0:
			if l[i] == q {
				q = 0
			}
		case l[i] == '"' || l[i] == '\'':
			if strings.IndexByte(l[i+1:], l[i]) >= 0 {
				q = l[i]
			}
		case l[i] == c:
			n++
		}
	}
	return n
}

// noOverlapLong: os trechos em ordem, sem sobreposição; no conflito fica o que cobre mais
// (nunca se descarta o resultado inteiro por causa de um conflito).
func noOverlapLong(ts []snippet) []snippet {
	sortSnippets(ts)
	out := make([]snippet, 0, len(ts))
	for _, t := range ts {
		if n := len(out); n > 0 && t.Start < out[n-1].End {
			if t.End-t.Start > out[n-1].End-out[n-1].Start {
				out[n-1] = t
			}
			continue
		}
		out = append(out, t)
	}
	return out
}

// lastCovering: o último trecho (em ordem) que se sobrepõe a [a, b), ou -1.
func lastCovering(ts []snippet, a, b int) int {
	i := sort.Search(len(ts), func(k int) bool { return ts[k].Start >= b })
	if k := i - 1; k >= 0 && ts[k].End > a {
		return k
	}
	return -1
}

// ---- fonte -------------------------------------------------------------------------------

// sourceSep: a fonte vai na frente da dica do resultado (WithSource); o Batch a separa na entrada.
const sourceSep = "\x1d"

// WithSource: a dica com a fonte (os arquivos ou o recurso do comando) na frente.
func WithSource(source, hint string) string {
	if source == "" {
		return hint
	}
	return sourceSep + source + sourceSep + hint
}

// SplitSource: o inverso de WithSource.
func SplitSource(d string) (source, hint string) {
	if !strings.HasPrefix(d, sourceSep) {
		return "", d
	}
	f, rest, _ := strings.Cut(d[len(sourceSep):], sourceSep)
	return f, rest
}

func noSources(items []BatchItem) []BatchItem {
	out := make([]BatchItem, len(items))
	for i, it := range items {
		_, it.Hint = SplitSource(it.Hint)
		out[i] = it
	}
	return out
}

// recordSources: as decisões com prova direta de cada texto ficam ligadas à fonte dele.
func (l *Batch) recordSources(items []BatchItem, sources []string) {
	if l.sources == nil {
		l.sources = map[string]map[string]bool{}
	}
	for i, it := range items {
		if sources[i] == "" || it.FromWeb {
			continue
		}
		ds, _ := l.m.decidedMemo(it.S, it.Hint)
		for _, d := range ds {
			if d.Rule == "âncora" {
				continue
			}
			k := strings.ToLower(d.Name)
			if l.sources[k] == nil {
				l.sources[k] = map[string]bool{}
			}
			for _, f := range strings.Split(sources[i], ",") {
				l.sources[k][f] = true
			}
		}
	}
}

// provenAtSource: v foi decidido com prova no próprio texto ou numa fonte do texto atual.
func (l *Batch) provenAtSource(v string) bool {
	k := strings.ToLower(v)
	if l.currentProv[k] {
		return true
	}
	if l.currentSource == "" {
		return false
	}
	for _, f := range strings.Split(l.currentSource, ",") {
		if l.sources[k][f] {
			return true
		}
	}
	return false
}

var reFileSource = regexp.MustCompile(`[\w~./\-]*[\w\-]\.[A-Za-z][\w]{0,5}\b`)

// CommandSource: os arquivos que o comando cita (nome base, minúsculas); sem arquivo, o
// programa e o primeiro argumento que não é opção ("kubectl get").
func CommandSource(cmd string) string {
	vis := map[string]bool{}
	var fs []string
	for _, a := range reFileSource.FindAllString(cmd, 64) {
		b := strings.ToLower(a[strings.LastIndexAny(a, "/\\")+1:])
		if !vis[b] {
			vis[b] = true
			fs = append(fs, b)
		}
	}
	if len(fs) == 0 {
		var ws []string
		for _, w := range strings.Fields(cmd) {
			if !strings.HasPrefix(w, "-") {
				ws = append(ws, strings.ToLower(w))
			}
			if len(ws) == 2 {
				break
			}
		}
		if len(ws) > 0 {
			fs = append(fs, "cmd:"+strings.Join(ws, " "))
		}
	}
	sort.Strings(fs)
	return strings.Join(fs, ",")
}

// fileSource: a fonte cita arquivo (e não só um comando sem arquivo, nem nada).
func fileSource(f string) bool {
	for _, x := range strings.Split(f, ",") {
		if x != "" && !strings.HasPrefix(x, "cmd:") {
			return true
		}
	}
	return false
}

// documentExt: arquivos de documentação em texto (o conteúdo é prosa, menos os blocos de código).
var documentExt = map[string]bool{"md": true, "markdown": true, "mdx": true, "txt": true, "rst": true, "adoc": true, "asciidoc": true}

// documentSource: todo arquivo da fonte é documentação (com um arquivo de código junto, vale a
// regra do código, que protege mais).
func documentSource(f string) bool {
	n := 0
	for _, x := range strings.Split(f, ",") {
		if x == "" || strings.HasPrefix(x, "cmd:") {
			continue
		}
		k := strings.LastIndexByte(x, '.')
		if k < 0 || !documentExt[x[k+1:]] {
			return false
		}
		n++
	}
	return n > 0
}

// SystemSource: a fonte de uma saída que descreve ou lista um catálogo de sistema (DESC/SHOW em
// SNOWFLAKE.ACCOUNT_USAGE, information_schema...). A estrutura listada é documentação pública:
// a saída não é fonte de decisão (como o texto do assistente) e só recebe o contágio dos nomes
// do cliente já conhecidos.
const SystemSource = "sistema"

// SystemTarget: o comando descreve ou lista um objeto de catálogo de sistema.
func SystemTarget(cmd string) bool { return systemTarget(cmd) }

// UsePublic: as palavras (minúsculas) que o modelo escreveu antes de um dado trazê-las nesta
// conversa (anterioridade, no proxy). São conhecimento dele: nenhum caminho as marca aqui.
func (l *Batch) UsePublic(p map[string]bool) { l.publicSet = p }

// fromModel: v é conhecimento do modelo nesta conversa (e não contém termo cadastrado: o
// cadastro sempre vence).
func (l *Batch) fromModel(v string) bool {
	if len(l.publicSet) == 0 {
		return false
	}
	return l.publicSet[strings.ToLower(v)] && !l.m.hasTerm(v)
}

// noPublics: os trechos de objeto cujo valor é conhecimento do modelo saem (os de formato,
// como CPF e chave, ficam: valem por si).
func (l *Batch) noPublics(s string, ts []snippet) []snippet {
	out := ts[:0:0]
	for _, t := range ts {
		if v := s[t.Start:t.End]; isObject(t.Type) && l.fromModel(v) {
			continue
		}
		out = append(out, t)
	}
	return out
}
