package mask

import (
	"sort"
	"strings"
)

// Memória da conversa (ver docs/pt-BR/structures.md): os nomes decididos em TODOS os textos de uma
// requisição valem para os outros textos ainda não enviados dela. Um nome que um leitor ou um
// decisor reconheceu num texto (o inventário de namespaces, o cabeçalho de um CSV) é mascarado
// também onde aparece solto (na prosa, numa saída sem estrutura), palavra inteira.
//
// A memória é recalculada a cada requisição a partir dos próprios textos dela (as decisões de
// cada um estão no memo): nada fica guardado por conversa, e numa conversa nova ela começa
// vazia. É um passo depois do memo: a chave do memo não muda, e o texto já enviado (congelado)
// sai igual.
//
// Quem escreveu: um texto do assistente que o proxy desmascarou (registrado em
// RecordResponse) volta como a API o mandou: voltam a ser pseudônimo exatamente os trechos
// que o proxy traduziu, e a memória não se aplica ao resto (o modelo nunca viu o nome real; se
// escreveu a palavra, é palavra comum). As palavras traduzidas entram na memória (regra
// "traduzida", com o tipo do pseudônimo).

// memName: um nome da memória, indexado pela primeira palavra dele.
type memName struct {
	name     string
	off      int // onde começa a primeira palavra dentro do nome
	ent      string
	caseless bool // tipo de SQL (ou servidor): casa em qualquer caixa
}

type memory struct {
	exact    map[string][]memName // primeira palavra -> nomes (do maior para o menor)
	caseless map[string][]memName // primeira palavra em minúsculas -> nomes
	n        int
	has      map[string]bool // chaves já na memória (adicionar)
	// tamanhos das primeiras palavras (bit n = tamanho n; bit 63 = 63 ou mais): a palavra de
	// outro tamanho nem é consultada
	exactSize, caselessSize uint64
}

func bitSize(n int) uint64 { return 1 << min(n, 63) }

// memMin: nome mais curto que a memória aplica fora do texto onde foi decidido (uma coluna
// "a" de um SELECT não pode mascarar toda letra "a" da conversa).
const memMin = 3

// isWord: byte que faz parte de uma palavra (os limites de palavra da memória). Byte de
// UTF-8 conta como letra: "ação" é uma palavra só.
func isWord(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// newMemory: as decisões, na ordem (a primeira decisão de uma chave define o tipo).
func (m *Masker) newMemory(decs []Decision) *memory {
	mm := &memory{exact: map[string][]memName{}, caseless: map[string][]memName{}}
	seen := map[string]bool{}
	for _, d := range decs {
		if d.Generic || len(d.Name) < memMin || publicGeneral(d.Name) || m.provenSoftware(d.Name) {
			continue
		}
		k := d.Key()
		if seen[k] {
			continue
		}
		seen[k] = true
		if mm.has == nil {
			mm.has = map[string]bool{}
		}
		mm.has[k] = true
		if !m.objMask(d.Ent) || isPseudoObj(d.Name) {
			continue
		}
		i := 0
		for i < len(d.Name) && !isWord(d.Name[i]) {
			i++
		}
		j := i
		letter := false
		for j < len(d.Name) && isWord(d.Name[j]) {
			if c := d.Name[j]; c < '0' || c > '9' {
				letter = true
			}
			j++
		}
		if i == j || !letter && j-i == len(d.Name) {
			continue // sem palavra, ou só número
		}
		nm := memName{name: d.Name, off: i, ent: d.Ent, caseless: entSQL[d.Ent]}
		if nm.caseless {
			t := strings.ToLower(d.Name[i:j])
			mm.caseless[t] = append(mm.caseless[t], nm)
			mm.caselessSize |= bitSize(j - i)
		} else {
			mm.exact[d.Name[i:j]] = append(mm.exact[d.Name[i:j]], nm)
			mm.exactSize |= bitSize(j - i)
		}
		mm.n++
	}
	if mm.n == 0 {
		return nil
	}
	for _, idx := range []map[string][]memName{mm.exact, mm.caseless} {
		for _, ns := range idx {
			sort.SliceStable(ns, func(a, b int) bool { return len(ns[a].name) > len(ns[b].name) })
		}
	}
	return mm
}

// casar: o nome nm ocupa s a partir de ini (palavra inteira)?
func (nm memName) matchIt(s string, ini int) bool {
	end := ini + len(nm.name)
	if ini < 0 || end > len(s) {
		return false
	}
	if nm.caseless {
		if !strings.EqualFold(s[ini:end], nm.name) {
			return false
		}
	} else if s[ini:end] != nm.name {
		return false
	}
	if isWord(nm.name[0]) && ini > 0 && isWord(s[ini-1]) {
		return false
	}
	return !(isWord(nm.name[len(nm.name)-1]) && end < len(s) && isWord(s[end]))
}

// varrer: os trechos de s em que a memória casa, fora dos trechos já trocados (ts, em ordem).
// Uma passada: cada palavra de s é uma consulta no índice.
func (mm *memory) scan(s string, ts []snippet) []snippet { return mm.scanF(s, ts, memFilter{}) }

// scanF: como varrer, com os freios do texto (memory_conversation_brakes.go).
func (mm *memory) scanF(s string, ts []snippet, f memFilter) []snippet {
	var out []snippet
	var buf []byte
	k := 0
	for i := 0; i < len(s); {
		if !isWord(s[i]) {
			i++
			continue
		}
		j := i + 1
		for j < len(s) && isWord(s[j]) {
			j++
		}
		var best *memName
		ini := 0
		attempt := func(ns []memName) {
			for x := range ns {
				nm := &ns[x]
				if best != nil && len(nm.name) <= len(best.name) {
					return
				}
				if a := i - nm.off; nm.matchIt(s, a) {
					best, ini = nm, a
					return
				}
			}
		}
		if mm.exactSize&bitSize(j-i) != 0 {
			attempt(mm.exact[s[i:j]])
		}
		if mm.caselessSize&bitSize(j-i) != 0 {
			buf = buf[:0]
			for x := i; x < j; x++ {
				c := s[x]
				if c >= 'A' && c <= 'Z' {
					c += 32
				}
				buf = append(buf, c)
			}
			attempt(mm.caseless[string(buf)])
		}
		end := 0
		if best != nil {
			end = ini + len(best.name)
		} else if nm := mm.piece(s[i:j]); nm != nil {
			// o nome é um pedaço de uma palavra maior ("LH_BRONZE_VENDA2024"): a palavra inteira
			// é mascarada, com o tipo dele
			best, ini, end = nm, i, j
		} else {
			i = j
			continue
		}
		for k < len(ts) && ts[k].End <= ini {
			k++
		}
		free := (k >= len(ts) || ts[k].Start >= end) && (len(out) == 0 || out[len(out)-1].End <= ini)
		if free && !f.skips(s, ini, end, best) {
			out = append(out, snippet{Start: ini, End: end, Type: objTypePrefix + best.ent})
			i = end
			continue
		}
		i = j
	}
	return out
}

// pedaco: o nome da memória que é uma sequência de partes de p (palavra com "_"), com cara de
// identificador, se há; o mais longo. "VENDA2024" casa em "LH_BRONZE_VENDA2024" e em
// "T01_VENDA2024"; "CLIENTES_PF" em "DIM_CLIENTES_PF_HIST". Word comum ("status" em
// "user_status") fica de fora: só nome com "_", dígito ou mistura de caixa.
func (mm *memory) piece(p string) *memName {
	if strings.IndexByte(p, '_') < 0 || len(p) > 128 {
		return nil
	}
	var cuts []int // início de cada parte e, no fim, len(p)+1
	cuts = append(cuts, 0)
	for x := 0; x < len(p); x++ {
		if p[x] == '_' {
			cuts = append(cuts, x+1)
		}
	}
	cuts = append(cuts, len(p)+1)
	var best *memName
	for a := 0; a < len(cuts)-1; a++ {
		for b := a + 1; b < len(cuts); b++ {
			ini, end := cuts[a], cuts[b]-1
			if ini == 0 && end == len(p) || end-ini < memMin || best != nil && end-ini <= len(best.name) {
				continue
			}
			c := p[ini:end]
			if c[0] == '_' || c[len(c)-1] == '_' {
				continue
			}
			find := func(ns []memName, equal func(string) bool) {
				for x := range ns {
					if nm := &ns[x]; nm.off == 0 && equal(nm.name) && identifierShape(nm.name) {
						best = nm
						return
					}
				}
			}
			if mm.exactSize&bitSize(len(c)) != 0 {
				find(mm.exact[c], func(n string) bool { return n == c })
			}
			if mm.caselessSize&bitSize(len(c)) != 0 {
				find(mm.caseless[strings.ToLower(c)], func(n string) bool { return strings.EqualFold(n, c) })
			}
		}
	}
	return best
}

// withMemory: o resultado de s com a memória da conversa aplicada (os trechos novos entram
// nas entradas e nos trechos, para a volta e para o congelamento).
func (l *Batch) withMemory(s string, r result, hint string) result {
	if l.mem == nil {
		l.mem = &memory{exact: map[string][]memName{}, caseless: map[string][]memName{}}
	}
	f := filterOf(hint)
	f.output = !l.prose && !l.writing
	extra := l.mem.scanF(s, r.snippets, f)
	if !l.writing && fileSource(l.currentSource) {
		// coluna provada numa fonte não contamina OUTRO arquivo lido (o vocabulário do arquivo
		// é dele: "status" no app.js não é a coluna do schema.sql); a conversa e as saídas sem
		// arquivo (kubectl logs, git branch) recebem o contágio. Name de recurso (tabela,
		// schema, banco...) provado nesta requisição vale em qualquer arquivo (procedência).
		k := extra[:0]
		for _, x := range extra {
			if v := s[x.Start:x.End]; identifierShape(v) || l.provenAtSource(v) || !columnOrIndex(strings.TrimPrefix(x.Type, objTypePrefix)) {
				k = append(k, x)
			}
		}
		extra = k
	}
	extra = l.noCommonsInProse(s, extra)
	if !l.writing && documentSource(l.currentSource) {
		extra = l.noCommonsInDocument(s, extra)
	}
	ts := l.joinSnippets(r.snippets, extra)
	l.currentProv = map[string]bool{}
	for _, d := range r.decidedSet {
		if d.Rule != "âncora" {
			l.currentProv[strings.ToLower(d.Name)] = true
		}
	}
	ts = l.noPublics(s, ts)
	ts2 := l.anchorPositions(s, ts, f)
	if newOnes := l.learnAnchor(s, ts, ts2); len(newOnes) > 0 {
		// o valor deduzido vale nas outras ocorrências deste texto (regra 2)
		ts2 = l.joinSnippets(ts2, l.noCommonsInProse(s, l.mem.scanF(s, ts2, f)))
		r.decidedSet = append(append([]Decision{}, r.decidedSet...), newOnes...)
		extra = append(extra, snippet{})
	}
	if len(extra) == 0 && len(ts2) == len(r.snippets) && !typeChanged(ts2, r.snippets) {
		return r
	}
	for i := range ts2 {
		if ts2[i].Pseudo == "" {
			ts2[i].Pseudo = l.m.Pseudonym(ts2[i].Type, s[ts2[i].Start:ts2[i].End])
		}
	}
	ts2 = noOverlapLong(l.noPublics(s, ts2)) // conflito se resolve; nunca se descarta tudo
	text, entries, ok := rebuild(s, ts2)
	if !ok {
		return r
	}
	return result{text: text, entries: entries, snippets: ts2, gen: r.gen, noLearn: r.noLearn, decidedSet: r.decidedSet}
}

// noCommonsInProse: na prosa (texto da mensagem), os trechos que a memória da conversa trouxe e
// que são palavra comum saem: lá ela é a palavra (vocab_words.go). Os que um leitor decidiu
// no próprio texto não passam por aqui.
func (l *Batch) noCommonsInProse(s string, xs []snippet) []snippet {
	if !l.prose {
		return xs
	}
	out := xs[:0:0]
	for _, x := range xs {
		if !l.m.freeCommon(s[x.Start:x.End]) {
			out = append(out, x)
		}
	}
	return out
}

// noCommonsInDocument: num arquivo de documentação (README, prompt de agente, notas), o texto
// fora dos blocos de código é prosa, como a mensagem do usuário: a palavra comum que a memória
// trouxe fica como palavra. Dentro de um bloco de código (``` ou ~~~), a procedência vale como
// em qualquer código.
func (l *Batch) noCommonsInDocument(s string, xs []snippet) []snippet {
	if len(xs) == 0 {
		return xs
	}
	fences := codeBlocks(s)
	out := xs[:0:0]
	for _, x := range xs {
		if !insideOf(fences, x.Start) && l.m.freeCommon(s[x.Start:x.End]) {
			continue
		}
		out = append(out, x)
	}
	return out
}

// codeBlocks: os trechos de s entre cercas de código do Markdown (``` ou ~~~ no começo da
// linha, depois do número de linha do Read, se houver). Cerca sem fechamento vai até o fim.
func codeBlocks(s string) [][2]int {
	var out [][2]int
	opened := -1
	for ini := 0; ini < len(s); {
		end := strings.IndexByte(s[ini:], '\n')
		if end < 0 {
			end = len(s)
		} else {
			end += ini
		}
		c := lineContent(s, ini, end)
		if strings.HasPrefix(s[c:end], "```") || strings.HasPrefix(s[c:end], "~~~") {
			if opened < 0 {
				opened = ini
			} else {
				out = append(out, [2]int{opened, end})
				opened = -1
			}
		}
		ini = end + 1
	}
	if opened >= 0 {
		out = append(out, [2]int{opened, len(s)})
	}
	return out
}

// lineContent: onde começa o conteúdo da linha s[ini:fim], depois do recuo e do número de
// linha que o Read do Claude Code põe ("    12→" ou "12\t").
func lineContent(s string, ini, end int) int {
	i := ini
	for i < end && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	j := i
	for j < end && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j > i {
		if strings.HasPrefix(s[j:end], "→") {
			return j + len("→")
		}
		if j < end && s[j] == '\t' {
			return j + 1
		}
	}
	for i < end && i-ini < 8 && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

func insideOf(fs [][2]int, p int) bool {
	for _, f := range fs {
		if p >= f[0] && p < f[1] {
			return true
		}
	}
	return false
}

// typeChanged: algum trecho mudou de tipo (âncora por posição corrigiu o genérico).
func typeChanged(a, b []snippet) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i].Type != b[i].Type || a[i].Pseudo != b[i].Pseudo {
			return true
		}
	}
	return false
}

// joinSnippets: os trechos do memo e os da memória, em ordem (sem pseudônimo nos novos).
func (l *Batch) joinSnippets(base, extra []snippet) []snippet {
	r := struct{ snippets []snippet }{base}
	ts := make([]snippet, 0, len(r.snippets)+len(extra))
	a, b := 0, 0
	for a < len(r.snippets) || b < len(extra) {
		if b >= len(extra) || a < len(r.snippets) && r.snippets[a].Start < extra[b].Start {
			ts = append(ts, r.snippets[a])
			a++
		} else {
			ts = append(ts, extra[b])
			b++
		}
	}
	return ts
}

// Memory monta a memória da conversa com os nomes decididos nos textos da requisição (depois
// de Warm, quando todo texto novo já está no memo) e nos trechos traduzidos dos textos do
// assistente (extras). Conteúdo da internet não entra (é mascarado, mas não ensina).
func (l *Batch) Memory(items []BatchItem, extras []Decision) {
	itSources := make([]string, len(items))
	for i := range items {
		itSources[i], _ = SplitSource(items[i].Hint)
	}
	items = noSources(items)
	defer func() { l.recordSources(items, itSources) }()
	l.buildMemory(l.itemDecisions(items), extras)
}

// itemDecisions: as decisões de cada texto (do congelado, do memo ou recalculadas uma vez).
func (l *Batch) itemDecisions(items []BatchItem) [][]Decision {
	m := l.m
	decs := make([][]Decision, len(items))
	var missingCount []warmItem
	var iMissing []int
	for i, it := range items {
		if it.FromWeb || len(it.S) < 4 {
			continue
		}
		if r, ok := m.frozen(it.Pos, it.S); ok && r.decidedSet != nil {
			decs[i] = r.decidedSet
			continue
		}
		if r, ok := m.inMemo(it.S, it.Hint); ok {
			// o resultado vai direto para a montagem (MaskHint), sem calcular o hash de novo
			l.pre[it.Pos] = r
			if r.decidedSet != nil {
				decs[i] = r.decidedSet
				continue
			}
		}
		// texto que saiu antes de um reinício (o enviados.log só tem os trechos) ou que saiu do
		// memo: as decisões dele são recalculadas uma vez
		missingCount = append(missingCount, warmItem{it.S, true, it.Hint})
		iMissing = append(iMissing, i)
	}
	if len(missingCount) > 0 {
		m.warm(missingCount)
		for _, i := range iMissing {
			it := items[i]
			decs[i], _ = m.decidedMemo(it.S, it.Hint)
			if decs[i] == nil {
				// saiu do memo enquanto os outros eram calculados (conversa maior que o memo):
				// calcula de novo, sem depender do memo
				r, _ := m.maskD(it.S, true, it.Hint)
				decs[i] = r.decidedSet
			}
			m.mu.Lock()
			if r, ok := m.frz[it.Pos]; ok && r.decidedSet == nil {
				r.decidedSet = decs[i]
				if r.decidedSet == nil {
					r.decidedSet = []Decision{}
				}
				m.frz[it.Pos] = r
			}
			m.mu.Unlock()
		}
	}
	return decs
}

// DecidedByText: para a anterioridade, os nomes que cada texto decidiu por si (leitores e
// decisores daquele texto; nunca o que veio da memória da conversa ou do conhecimento
// acumulado), por texto. Text sem decisões calculadas (da internet) fica de fora.
func (l *Batch) DecidedByText(items []BatchItem) map[string][]string {
	items = noSources(items)
	decs := l.itemDecisions(items)
	out := make(map[string][]string, len(items))
	for i, it := range items {
		if decs[i] == nil {
			continue
		}
		ns := out[it.S]
		if ns == nil {
			ns = []string{}
		}
		for _, d := range decs[i] {
			ns = append(ns, d.Name)
		}
		out[it.S] = ns
	}
	return out
}

// joinDecisions: as decisões de todos os textos, cada texto repetido uma vez só.
func joinDecisions(decs [][]Decision) []Decision {
	var allOf []Decision
	seenOnes := map[*Decision]bool{} // o mesmo texto repetido traz as mesmas decisões (do memo)
	for _, d := range decs {
		if len(d) == 0 || seenOnes[&d[0]] {
			continue
		}
		seenOnes[&d[0]] = true
		allOf = append(allOf, d...)
	}
	return allOf
}

func (l *Batch) buildMemory(decs [][]Decision, extras []Decision) {
	m := l.m
	allOf := append(joinDecisions(decs), extras...)
	if len(allOf) > 0 {
		if len(l.publicSet) > 0 {
			k := allOf[:0:0]
			for _, d := range allOf {
				if !l.fromModel(d.Name) {
					k = append(k, d)
				}
			}
			allOf = k
		}
		l.mem = m.newMemory(allOf)
		for _, d := range allOf {
			if d.Rule == "âncora" {
				l.markDeduced(d.Name)
			}
		}
	}
}

// decidedMemo: as decisões de s no memo (nil, false: s não está no memo).
func (m *Masker) decidedMemo(s, hint string) ([]Decision, bool) {
	r, ok := m.inMemo(s, hint)
	if !ok || r.decidedSet == nil {
		return nil, false
	}
	return r.decidedSet, true
}

// inMemo: o resultado de s no memo, como maskKE o devolveria (sem detectar de novo).
func (m *Masker) inMemo(s, hint string) (result, bool) {
	k := memoKey(s, hint)
	m.mu.Lock()
	r, ok := m.memo[k]
	if !ok {
		if r, ok = m.stale[k]; ok {
			r = m.store(k, r)
		}
	}
	m.mu.Unlock()
	if !ok {
		return r, false
	}
	// valor aprendido depois que o resultado foi calculado e que aparece em s: refaz (como em
	// maskKE; conferido, o resultado fica marcado com a geração atual)
	if g := m.known.generation(); r.gen != g {
		if m.known.containsSince(s, r.gen) {
			return r, false
		}
		r.gen = g
		m.mu.Lock()
		r = m.store(k, r)
		m.mu.Unlock()
	}
	return r, true
}

// Written: des é um texto do assistente desmascarado aqui (há registro)? Devolve as decisões
// dos trechos traduzidos (regra "traduzida", com o tipo do pseudônimo), para a memória.
func (l *Batch) Written(des string) ([]Decision, bool) {
	if len(des) < 4 {
		return nil, false
	}
	e, ok := l.m.writtenOf(des)
	if !ok {
		return nil, false
	}
	return translatedDecisions(des, e.ts), true
}

func translatedDecisions(des string, ts []snippet) []Decision {
	out := []Decision{}
	for _, t := range ts {
		if !isObject(t.Type) {
			continue
		}
		v := des[t.Start:t.End]
		out = append(out, Decision{Name: v, Ent: strings.TrimPrefix(t.Type, objTypePrefix), Rule: "traduzida", Generic: isGeneric(v)})
	}
	return out
}

// MaskHintProse: como MaskHint, para o texto que o usuário escreveu na mensagem (não a
// saída de ferramenta): palavra comum que chega por contágio fica como palavra; a que um leitor
// decide ali mesmo (um DDL colado, kubectl -n) continua nome (vocab_words.go).
func (l *Batch) MaskHintProse(s string, fromWeb bool, pos Position, hint string) (string, []Entry) {
	l.prose = true
	defer func() { l.prose = false }()
	return l.MaskHint(s, fromWeb, pos, hint)
}

// MaskProse: como MaskWritten, para um bloco de texto da resposta (não a entrada de
// ferramenta): palavra comum decidida em outro lugar fica como palavra (vocab_words.go).
func (l *Batch) MaskProse(des string, fromWeb bool, pos Position, hint string) (string, []Entry) {
	l.prose = true
	defer func() { l.prose = false }()
	return l.MaskWritten(des, fromWeb, pos, hint)
}

// MaskWritten: como MaskHint, para um texto do assistente. Com registro (ver
// RecordResponse), sai o que a API mandou: só os trechos traduzidos voltam a ser
// pseudônimo. Sem registro (reinício sem enviados.log, outro processo), é um texto como
// os outros.
func (l *Batch) MaskWritten(des string, fromWeb bool, pos Position, hint string) (string, []Entry) {
	if len(des) < 4 {
		return des, nil
	}
	if r, ok := l.m.frozen(pos, des); ok {
		return r.text, r.entries
	}
	if e, ok := l.m.writtenOf(des); ok && !fromWeb {
		if text, entries, valid := rebuild(des, e.ts); valid {
			r := result{text: text, entries: entries, snippets: e.ts, decidedSet: translatedDecisions(des, e.ts)}
			if _, already := l.outputs[pos]; !already {
				l.outputs[pos] = r
			}
			return r.text, r.entries
		}
	}
	if !fromWeb {
		return l.MaskContagion(des, fromWeb, pos) // regra 1: o assistente não é fonte
	}
	return l.MaskHint(des, fromWeb, pos, hint)
}
