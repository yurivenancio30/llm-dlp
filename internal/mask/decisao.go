package mask

import (
	"crypto/sha256"
	"strings"
	"sync"
)

// Interface comum das regras que decidem nomes (ver docs/pt-BR/estruturas.md, seção Memória da
// conversa). Três peças:
//
//   - Decision: um nome que uma regra decidiu num texto (nome, tipo, regra). Fica no resultado
//     do memo (resultado.decididos), em RAM; nunca vai para o vistos.json. A memória da
//     conversa (proxy, entre as duas passadas) junta as decisões de todos os textos do pedido.
//   - Decider: uma regra que decide nomes olhando o texto inteiro e o que já foi achado nele
//     (proveniência, identidade, eco, léxica). Cada decisor chama TextCtx.Decide, que
//     mascara no lugar e registra a decisão. Os decisores se registram com registerDecider,
//     cada um no init() do próprio arquivo.
//   - Quem escreveu: o texto que a API mandou (só com pseudônimos), guardado pelo hash do
//     texto desmascarado que o Claude Code vai reenviar (RecordResponse / OriginalOf /
//     Translated).

// Decision: um nome decidido num texto.
type Decision struct {
	Name string // como aparece no texto (sem citação)
	Ent  string // tipo (chave de ObjectPrefixes)
	Rule string // quem decidiu
	// Generic: palavra da referência pública derivada (isGeneric). É mascarada onde foi
	// decidida, mas não entra na memória da conversa.
	Generic bool
}

// Key: a forma que identifica o nome (a mesma regra de caixa do vistos: SQL e servidor
// sem caixa, os outros tipos exatos).
func (d Decision) Key() string { return canonObj(d.Ent, d.Name) }

// TextCtx: o que um decisor vê de um texto.
type TextCtx struct {
	S        string
	Findings []Finding // o que os detectores e leitores já acharam em S (posições de S)
	Learn    bool      // false: conteúdo da internet (decide no lugar, sem lembrar)
	// Ext: a parte da dica que não é a do leitor de tabela (chamada_comando.go): o que o proxy sabe
	// da chamada que produziu o texto. "" = nada. O formato é de quem a escreve.
	Ext string

	m   *Masker
	out []Finding
	dec []Decision
}

// Decide: s[ini:fim] é um nome do tipo ent. Mascara no lugar e registra a decisão.
func (c *TextCtx) Decide(ini, end int, ent, rule string) {
	if ini < 0 || end > len(c.S) || end <= ini {
		return
	}
	if c.m != nil && !c.m.objMask(ent) {
		return
	}
	if !wholeValue(c.S, ini, end) || publicSystem(unquoted(c.S[ini:end])) {
		return // pedaço de uma expressão, ou vocabulário de sistema (ver memoria_rastreamento.go)
	}
	v := c.S[ini:end]
	if isPseudoObj(v) {
		return
	}
	c.out = append(c.out, Finding{ini, end, objTypePrefix + ent, v})
	c.dec = append(c.dec, Decision{Name: unquoted(v), Ent: ent, Rule: rule, Generic: isGeneric(v)})
}

// Known: o tipo com que v já é nome (aprendido em RAM ou no vistos.json), se é.
func (c *TextCtx) Known(v string) (string, bool) {
	if c.m == nil {
		return "", false
	}
	return c.m.learnedEnt(v)
}

// Decider: uma regra que decide nomes num texto.
type Decider struct {
	Name   string
	Decide func(c *TextCtx)
}

var (
	decidersMu sync.RWMutex
	deciders   []Decider
)

// registerDecider: chamado no init() do arquivo de cada regra.
func registerDecider(d Decider) {
	decidersMu.Lock()
	deciders = append(deciders, d)
	decidersMu.Unlock()
}

// runDeciders: todos os decisores em s (depois dos leitores). Devolve os achados novos e
// as decisões.
func (m *Masker) runDeciders(s string, findings []Finding, learn bool, ext string) ([]Finding, []Decision) {
	decidersMu.RLock()
	ds := deciders
	decidersMu.RUnlock()
	if len(ds) == 0 || !m.cfg.Objects.Enabled {
		return nil, nil
	}
	c := &TextCtx{S: s, Findings: findings, Learn: learn, Ext: ext, m: m}
	for _, d := range ds {
		d.Decide(c)
	}
	return c.out, c.dec
}

// findingDecisions: os nomes de objeto achados pelos leitores, como decisões (regra
// "leitor"). Junto com as dos decisores, formam os nomes decididos do texto.
func findingDecisions(as []Finding) []Decision {
	var out []Decision
	seen := map[string]bool{}
	for _, a := range as {
		if !isObject(a.Type) {
			continue
		}
		d := Decision{Name: unquoted(a.Real), Ent: strings.TrimPrefix(a.Type, objTypePrefix), Rule: "leitor", Generic: isGeneric(a.Real)}
		if k := d.Key(); !seen[k] {
			seen[k] = true
			out = append(out, d)
		}
	}
	return out
}

// Decided: os nomes decididos no texto s (o resultado no memo). nil se s ainda não foi
// mascarado com essa dica.
func (m *Masker) Decided(s, hint string) []Decision {
	k := memoKey(s, hint)
	m.mu.Lock()
	r, ok := m.memo[k]
	if !ok {
		r, ok = m.stale[k]
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return r.decidedSet
}

// ---------------------------------------------------------------------------------------
// Referência pública derivada: palavras muito frequentes como nome de recurso no
// material público (default, public, api...). Preenchida por um arquivo gerado por medição.

var publicRef map[string]bool

// isGeneric: v é genérico demais para entrar na memória da conversa.
func isGeneric(v string) bool { return publicRef[strings.ToLower(unquoted(v))] }

// ---------------------------------------------------------------------------------------
// Quem escreveu: o texto original (com pseudônimos) de cada texto do assistente, pelo hash do
// texto desmascarado. Em RAM, com teto, e no enviados.log (só o HMAC do texto desmascarado e,
// para cada trecho traduzido, onde está, o tipo e o pseudônimo: nenhum valor real).

// Translation: um trecho do texto desmascarado que veio de um pseudônimo.
type Translation struct {
	Start, End int
	Pseudo     string
	Type       string // o tipo do pseudônimo ("obj.namespace", "email"...)
}

type written struct {
	original string
	ts       []snippet // os trechos traduzidos, em posições do texto desmascarado
}

type writtenRegistry struct {
	mu      sync.Mutex
	current map[[32]byte]written
	stale   map[[32]byte]written
}

const maxWritten = 20_000

func (r *writtenRegistry) store(k [32]byte, e written) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current == nil {
		r.current = map[[32]byte]written{}
	}
	if len(r.current) >= maxWritten {
		r.stale, r.current = r.current, map[[32]byte]written{}
	}
	r.current[k] = e
}

func (r *writtenRegistry) lookup(k [32]byte) (written, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.current[k]; ok {
		return e, true
	}
	e, ok := r.stale[k]
	return e, ok
}

// writtenID: como um texto do assistente é guardado em disco (HMAC do texto desmascarado).
func (m *Masker) writtenID(des string) string { return m.p.raw("escrito", des, 16) }

// RecordResponse: o texto original que a API mandou e a tabela usada para desmascará-lo.
// Devolve o texto desmascarado (o mesmo que tab.Unmask). Todo texto é registrado, mesmo
// sem pseudônimo: o que o modelo escreveu sozinho é palavra comum (ele nunca viu o nome real).
func (m *Masker) RecordResponse(original string, tab *Table) string {
	des := tab.Unmask(original, false)
	if len(des) < 4 { // texto curto nunca é mascarado (ver Batch.MaskHint)
		return des
	}
	var ts []snippet
	if des != original {
		var b strings.Builder
		lastPos := 0
		tab.scan(original, func(ini, end int) {
			b.WriteString(original[lastPos:ini])
			p := original[ini:end]
			a := b.Len()
			b.WriteString(tab.m[p])
			ts = append(ts, snippet{a, b.Len(), tab.typ[p], p})
			lastPos = end
		})
		b.WriteString(original[lastPos:])
		if b.String() != des { // a volta fez mais que trocar pseudônimos (sub-rede de IP): sem registro
			return des
		}
	}
	m.writtenSet.store(sha256.Sum256([]byte(des)), written{original, ts})
	if m.sentSet != nil {
		m.sentSet.Record(m.writtenID(des), ts)
	}
	return des
}

// writtenOf: o registro de des (da RAM, ou do enviados.log depois de um reinício).
func (m *Masker) writtenOf(des string) (written, bool) {
	k := sha256.Sum256([]byte(des))
	if e, ok := m.writtenSet.lookup(k); ok {
		return e, true
	}
	if m.sentSet == nil {
		return written{}, false
	}
	ts, ok := m.sentSet.Lookup(m.writtenID(des))
	if !ok {
		return written{}, false
	}
	orig, _, valid := rebuild(des, ts)
	if !valid {
		return written{}, false
	}
	e := written{orig, ts}
	m.writtenSet.store(k, e)
	return e, true
}

// OriginalOf: o texto que a API mandou, se des é um texto do assistente desmascarado aqui.
func (m *Masker) OriginalOf(des string) (string, bool) {
	e, ok := m.writtenOf(des)
	return e.original, ok
}

// Translated: os trechos de des que o proxy traduziu de um pseudônimo (ok=false: sem registro).
func (m *Masker) Translated(des string) ([]Translation, bool) {
	e, ok := m.writtenOf(des)
	if !ok {
		return nil, false
	}
	var tr []Translation
	for _, t := range e.ts {
		tr = append(tr, Translation{t.Start, t.End, t.Pseudo, t.Type})
	}
	return tr, true
}
