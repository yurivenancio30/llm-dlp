package mask

import (
	"crypto/sha256"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
)

// Mask: a porta de entrada. Memoriza resultados, porque o Claude Code reenvia a conversa
// inteira a cada mensagem.

// Mask e a memória de resultados (o Claude Code reenvia a conversa inteira a cada mensagem).

const memoMax = 64 << 20 // teto (aproximado) do texto memorizado, somando as duas gerações

// Mask troca o dado sensível de s por pseudônimos e devolve as entradas usadas.
// Resultados são memorizados: o Claude Code reenvia a conversa inteira a cada mensagem.
// O proxy usa um Batch, que além disso congela o que saiu, por posição (ver memory_sent.go).
func (m *Masker) Mask(s string) (string, []Entry) {
	r, _ := m.maskIt(s, true)
	return r.text, r.entries
}

func (m *Masker) maskIt(s string, learn bool) (result, [32]byte) {
	return m.maskD(s, learn, "")
}

// maskD: com a dica do comando que produziu o texto (ver call_command.go; "" = sem dica).
func (m *Masker) maskD(s string, learn bool, hint string) (result, [32]byte) {
	if len(s) < 4 {
		return result{text: s}, [32]byte{}
	}
	k := memoKey(s, hint)
	return m.maskKE(s, k, learn, readHint(hint), hint), k
}

// memoKey: a chave do texto na memória de resultados (com a dica, se houver: o mesmo texto
// pode sair de comandos diferentes).
func memoKey(s, hint string) [32]byte {
	if hint == "" {
		return sha256.Sum256([]byte(s))
	}
	h := sha256.New()
	h.Write([]byte("dica\x00" + hint + "\x00"))
	h.Write([]byte(s))
	var k [32]byte
	copy(k[:], h.Sum(nil))
	return k
}

// maskK: com o hash do texto já calculado (k).
func (m *Masker) maskK(s string, k [32]byte, learn bool, d *outputHint) result {
	return m.maskKE(s, k, learn, d, "")
}

// maskKE: com a dica crua (a extensão dela vai para os decisores).
func (m *Masker) maskKE(s string, k [32]byte, learn bool, d *outputHint, hint string) result {
	m.mu.Lock()
	r, ok := m.memo[k]
	if !ok {
		if r, ok = m.stale[k]; ok {
			r = m.store(k, r) // ainda em uso: volta para a geração atual do memo
		}
	}
	m.mu.Unlock()
	g := m.known.generation()
	if ok && !(learn && r.noLearn) {
		// Vale, a não ser que contenha um valor aprendido DEPOIS (por outro texto, por exemplo).
		if r.gen == g {
			return r
		}
		if !m.known.containsSince(s, r.gen) {
			r.gen = g // conferido até aqui: da próxima vez não confere de novo
			m.mu.Lock()
			r = m.store(k, r)
			m.mu.Unlock()
			return r
		}
	}
	_, ext := hintParts(hint)
	findings, decidedSet := m.detectDE(s, learn, d, ext)
	m.recordSoftware(s)

	// Até que geração este resultado vale? Se nada foi aprendido durante a detecção, até g.
	// Se algo foi aprendido (por este texto ou por outro, em paralelo), confere: todo valor
	// novo que aparece em s já está entre os achados? Então vale até a geração atual. Se
	// algum ficou de fora (aprendido por outro texto depois que este já tinha sido varrido),
	// fica marcado com g, e a próxima consulta refaz.
	if g1 := m.known.generation(); g1 != g {
		covered := make(map[[2]int]bool, len(findings))
		for _, a := range findings {
			covered[[2]int{a.Start, a.End}] = true
		}
		missingOne := false
		m.known.scan(s, g, func(ini, end int, _ string) bool {
			missingOne = !covered[[2]int{ini, end}]
			return !missingOne
		})
		if !missingOne {
			g = g1
		}
	}
	text, entries, ts := m.applyT(s, findings)
	if decidedSet == nil {
		decidedSet = []Decision{} // calculado, sem decisões (nil = não se sabe: ver Batch.Memory)
	}

	m.mu.Lock()
	r = m.store(k, result{text, entries, ts, g, !learn, decidedSet})
	m.mu.Unlock()
	return r
}

// posID: como um texto numa posição é guardado em disco (HMAC da chave de posição).
func (m *Masker) posID(kc Position) string { return m.p.raw("enviado", string(kc[:]), 16) }

// UseSent liga o registro em disco do que já saiu (só o proxy usa). impressao
// identifica a configuração e a versão: se mudarem, os registros antigos não valem.
func (m *Masker) UseSent(path, fingerprint string) error {
	e, err := LoadSent(path, m.p.raw("impressao", fingerprint, 16))
	if err != nil {
		return err
	}
	m.sentSet = e
	return nil
}

// Position identifica um texto num ponto exato da conversa: um hash de tudo o que vem antes
// dele na requisição e dele mesmo (quem calcula é o proxy). O congelamento vale só para a
// mesma Position: o reenvio exato daquele ponto da conversa sai igual; o mesmo texto noutra
// conversa, ou numa mensagem nova, é mascarado com o que se sabe agora.
type Position [32]byte

// congelado: o resultado com que o texto saiu nesta posição, se já saiu (da memória, ou do
// enviados.log depois de um reinício).
func (m *Masker) frozen(kc Position, s string) (result, bool) {
	m.mu.Lock()
	r, ok := m.frz[kc]
	if !ok {
		if r, ok = m.frzOld[kc]; ok {
			m.storeFrozen(kc, r)
		}
	}
	m.mu.Unlock()
	if ok || m.sentSet == nil {
		return r, ok
	}
	ts, foundIt := m.sentSet.Lookup(m.posID(kc))
	if !foundIt {
		return r, false
	}
	text, entries, valid := rebuild(s, ts)
	if !valid {
		return r, false
	}
	r = result{text: text, entries: entries, snippets: ts}
	m.mu.Lock()
	m.storeFrozen(kc, r)
	m.mu.Unlock()
	return r, true
}

// storeFrozen: como guardar, para os congelados (com m.mu travado).
func (m *Masker) storeFrozen(kc Position, r result) {
	if _, ok := m.frz[kc]; !ok {
		if m.frzBytes > memoMax/2 {
			m.frzOld, m.frz, m.frzBytes = m.frz, map[Position]result{}, 0
		}
		m.frzBytes += len(r.text) + 96*len(r.entries) + 64
	}
	m.frz[kc] = r
}

// Batch: os textos de uma requisição. Depois que a requisição sai, Freeze fixa o
// resultado de cada um na sua posição.
type Batch struct {
	m       *Masker
	outputs map[Position]result // o que saiu em cada posição
	mem     *memory             // a memória da conversa (ver memory_conversation.go); nil = vazia
	pre     map[Position]result // o resultado do memo de cada texto novo, já consultado em Memory
	// rastreamento (ver memory_tracing.go)
	deducedSet    map[string]bool            // valores marcados por dedução: não servem de semente
	sources       map[string]map[string]bool // valor (minúsculas) -> fontes em que teve prova direta
	currentSource string                     // a fonte do texto sendo montado (vazia: sem comando)
	currentProv   map[string]bool            // decididos com prova no próprio texto sendo montado
	writing       bool                       // montando texto do assistente (recebe todo o contágio)
	prose         bool                       // montando texto de mensagem, do usuário ou da resposta (vocab_words.go)
	publicSet     map[string]bool            // anterioridade: o modelo escreveu antes de qualquer dado
}

func (m *Masker) NewBatch() *Batch {
	return &Batch{m: m, outputs: map[Position]result{}, pre: map[Position]result{}}
}

// BatchItem: um texto da requisição, onde está e se veio da internet.
type BatchItem struct {
	S       string
	FromWeb bool
	Pos     Position
	Hint    string // o que o comando que produziu o texto diz dele (Commands.Hint)
}

// Mask: texto que já saiu nesta posição sai igual (reescrever não protegeria nada e
// regravaria a conversa no cache da API). fromWeb = conteúdo da internet (resultado de
// WebFetch/WebSearch): é mascarado, mas nada dele é lembrado.
func (l *Batch) Mask(s string, fromWeb bool, pos Position) (string, []Entry) {
	return l.MaskHint(s, fromWeb, pos, "")
}

// MaskHint: como Mask, com a dica do comando que produziu o texto.
func (l *Batch) MaskHint(s string, fromWeb bool, pos Position, hint string) (string, []Entry) {
	l.currentSource, hint = SplitSource(hint)
	if l.currentSource == SystemSource {
		return l.MaskContagion(s, fromWeb, pos)
	}
	if len(s) < 4 {
		return s, nil
	}
	if r, ok := l.m.frozen(pos, s); ok {
		return r.text, r.entries
	}
	r, ok := l.pre[pos]
	if !ok || r.noLearn && !fromWeb {
		r, _ = l.m.maskD(s, !fromWeb, hint)
	}
	r = l.withMemory(s, r, hint)
	if _, already := l.outputs[pos]; !already {
		l.outputs[pos] = r
	}
	return r.text, r.entries
}

// Warm mascara antes da montagem os textos da requisição que ainda não saíram, para que
// todo valor aprendido nela já valha quando os textos forem montados em ordem.
func (l *Batch) Warm(items []BatchItem) {
	items = noSources(items)
	pend := make([]warmItem, 0, len(items))
	for _, it := range items {
		if len(it.S) < 4 {
			continue
		}
		if _, ok := l.m.frozen(it.Pos, it.S); ok {
			continue
		}
		pend = append(pend, warmItem{it.S, !it.FromWeb, it.Hint})
	}
	l.m.warm(pend)
}

// Freeze: a requisição saiu. O resultado de cada texto fica fixo na sua posição (e vai
// para o disco).
func (l *Batch) Freeze() {
	m := l.m
	newSet := make([]Position, 0, len(l.outputs))
	m.mu.Lock()
	for kc, r := range l.outputs {
		if _, ok := m.frz[kc]; ok {
			continue
		}
		m.storeFrozen(kc, r)
		newSet = append(newSet, kc)
	}
	m.mu.Unlock()
	if m.sentSet != nil {
		for _, kc := range newSet {
			m.sentSet.Record(m.posID(kc), l.outputs[kc].snippets)
		}
	}
}

// Persist grava em disco o que foi aprendido e ainda não foi gravado (pessoas novas).
func (m *Masker) Persist() {
	if m.people != nil {
		m.people.SaveIfDirty()
	}
	if m.sentSet != nil {
		m.sentSet.Save()
	}
}

// Warm mascara em paralelo os textos que ainda não estão no memo. Serve para o momento em
// que chega muito texto novo de uma vez (primeira mensagem depois de um reinício, sessão
// retomada): em vez de um texto por vez, usa todos os núcleos. O resultado final é o mesmo,
// porque quem monta a requisição chama Mask em ordem logo depois (e Mask reconfere
// o texto ainda não enviado contra valores aprendidos nesse meio-tempo).
func (m *Masker) Warm(texts []string) {
	items := make([]warmItem, len(texts))
	for i, t := range texts {
		items[i] = warmItem{t, true, ""}
	}
	m.warm(items)
}

type warmItem struct {
	s     string
	learn bool
	hint  string
}

func (m *Masker) warm(items []warmItem) {
	var pend []warmItem
	total := 0
	seen := map[[32]byte]bool{}
	m.mu.Lock()
	for _, it := range items {
		if len(it.s) < 4 {
			continue
		}
		k := memoKey(it.s, it.hint)
		_, a := m.memo[k]
		_, b := m.stale[k]
		if !a && !b && !seen[k] {
			seen[k] = true
			pend = append(pend, it)
			total += len(it.s)
		}
	}
	m.mu.Unlock()
	if len(pend) < 2 || total < warmMin {
		// pouco texto: um por vez (mas antes da montagem, para todo aprendizado valer nela)
		for _, it := range pend {
			m.maskD(it.s, it.learn, it.hint)
		}
		return
	}
	// os maiores primeiro, para as tarefas ficarem equilibradas
	sort.Slice(pend, func(i, j int) bool { return len(pend[i].s) > len(pend[j].s) })
	n := runtime.GOMAXPROCS(0)
	if n > len(pend) {
		n = len(pend)
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < n; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= len(pend) {
					return
				}
				m.maskD(pend[i].s, pend[i].learn, pend[i].hint)
			}
		}()
	}
	wg.Wait()
}

// warmMin: abaixo disso de texto novo, o paralelismo não compensa
const warmMin = 48 << 10

// guardar põe o resultado no memo (com m.mu travado) e o devolve. O memo tem duas
// gerações: quando a atual enche, vira a "velha" e começa outra vazia; o que continua em uso
// é trazido de volta na consulta, e o resto some quando a velha é descartada. Assim a RAM
// tem teto e não há um momento em que tudo é esquecido de uma vez.
func (m *Masker) store(k [32]byte, r result) result {
	if _, ok := m.memo[k]; !ok {
		if m.bytes > memoMax/2 {
			m.stale, m.memo, m.bytes = m.memo, map[[32]byte]result{}, 0
		}
		m.bytes += len(r.text) + 96*len(r.entries) + 64
	}
	m.memo[k] = r
	return r
}
