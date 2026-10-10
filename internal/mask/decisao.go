package mask

import (
	"crypto/sha256"
	"strings"
	"sync"
)

// Interface comum das regras que decidem nomes (ver docs/pt-BR/estruturas.md, seção Memória da
// conversa). Três peças:
//
//   - Decisao: um nome que uma regra decidiu num texto (nome, tipo, regra). Fica no resultado
//     do memo (resultado.decididos), em RAM; nunca vai para o vistos.json. A memória da
//     conversa (proxy, entre as duas passadas) junta as decisões de todos os textos do pedido.
//   - Decisor: uma regra que decide nomes olhando o texto inteiro e o que já foi achado nele
//     (proveniência, identidade, eco, léxica). Cada decisor chama TextoCtx.Decidir, que
//     mascara no lugar e registra a decisão. Os decisores se registram com registrarDecisor,
//     cada um no init() do próprio arquivo.
//   - Quem escreveu: o texto que a API mandou (só com pseudônimos), guardado pelo hash do
//     texto desmascarado que o Claude Code vai reenviar (RegistrarResposta / OriginalDe /
//     Traduzidas).

// Decisao: um nome decidido num texto.
type Decisao struct {
	Nome  string // como aparece no texto (sem citação)
	Ent   string // tipo (chave de EntObjeto)
	Regra string // quem decidiu
	// Generica: palavra da referência pública derivada (ehGenerica). É mascarada onde foi
	// decidida, mas não entra na memória da conversa.
	Generica bool
}

// Chave: a forma que identifica o nome (a mesma regra de caixa do vistos: SQL e servidor
// sem caixa, os outros tipos exatos).
func (d Decisao) Chave() string { return canonObj(d.Ent, d.Nome) }

// TextoCtx: o que um decisor vê de um texto.
type TextoCtx struct {
	S       string
	Achados []Achado // o que os detectores e leitores já acharam em S (posições de S)
	Aprende bool     // false: conteúdo da internet (decide no lugar, sem lembrar)
	// Ext: a parte da dica que não é a do leitor de tabela (chamada_comando.go): o que o proxy sabe
	// da chamada que produziu o texto. "" = nada. O formato é de quem a escreve.
	Ext string

	m   *Masker
	out []Achado
	dec []Decisao
}

// Decidir: s[ini:fim] é um nome do tipo ent. Mascara no lugar e registra a decisão.
func (c *TextoCtx) Decidir(ini, fim int, ent, regra string) {
	if ini < 0 || fim > len(c.S) || fim <= ini {
		return
	}
	if c.m != nil && !c.m.objMascara(ent) {
		return
	}
	if !valorInteiro(c.S, ini, fim) || publicoSistema(semCitacao(c.S[ini:fim])) {
		return // pedaço de uma expressão, ou vocabulário de sistema (ver memoria_rastreamento.go)
	}
	v := c.S[ini:fim]
	if ehPseudoObj(v) {
		return
	}
	c.out = append(c.out, Achado{ini, fim, prefTipoObj + ent, v})
	c.dec = append(c.dec, Decisao{Nome: semCitacao(v), Ent: ent, Regra: regra, Generica: ehGenerica(v)})
}

// Conhecido: o tipo com que v já é nome (aprendido em RAM ou no vistos.json), se é.
func (c *TextoCtx) Conhecido(v string) (string, bool) {
	if c.m == nil {
		return "", false
	}
	return c.m.entAprendido(v)
}

// Decisor: uma regra que decide nomes num texto.
type Decisor struct {
	Nome    string
	Decidir func(c *TextoCtx)
}

var (
	decisoresMu sync.RWMutex
	decisores   []Decisor
)

// registrarDecisor: chamado no init() do arquivo de cada regra.
func registrarDecisor(d Decisor) {
	decisoresMu.Lock()
	decisores = append(decisores, d)
	decisoresMu.Unlock()
}

// rodarDecisores: todos os decisores em s (depois dos leitores). Devolve os achados novos e
// as decisões.
func (m *Masker) rodarDecisores(s string, achados []Achado, aprende bool, ext string) ([]Achado, []Decisao) {
	decisoresMu.RLock()
	ds := decisores
	decisoresMu.RUnlock()
	if len(ds) == 0 || !m.cfg.Objetos.Ligado {
		return nil, nil
	}
	c := &TextoCtx{S: s, Achados: achados, Aprende: aprende, Ext: ext, m: m}
	for _, d := range ds {
		d.Decidir(c)
	}
	return c.out, c.dec
}

// decisoesDosAchados: os nomes de objeto achados pelos leitores, como decisões (regra
// "leitor"). Junto com as dos decisores, formam os nomes decididos do texto.
func decisoesDosAchados(as []Achado) []Decisao {
	var out []Decisao
	visto := map[string]bool{}
	for _, a := range as {
		if !ehObjeto(a.Tipo) {
			continue
		}
		d := Decisao{Nome: semCitacao(a.Real), Ent: strings.TrimPrefix(a.Tipo, prefTipoObj), Regra: "leitor", Generica: ehGenerica(a.Real)}
		if k := d.Chave(); !visto[k] {
			visto[k] = true
			out = append(out, d)
		}
	}
	return out
}

// Decididos: os nomes decididos no texto s (o resultado no memo). nil se s ainda não foi
// mascarado com essa dica.
func (m *Masker) Decididos(s, dica string) []Decisao {
	k := chaveMemo(s, dica)
	m.mu.Lock()
	r, ok := m.memo[k]
	if !ok {
		r, ok = m.velho[k]
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return r.decididos
}

// ---------------------------------------------------------------------------------------
// Referência pública derivada: palavras muito frequentes como nome de recurso no
// material público (default, public, api...). Preenchida por um arquivo gerado por medição.

var refPublica map[string]bool

// ehGenerica: v é genérico demais para entrar na memória da conversa.
func ehGenerica(v string) bool { return refPublica[strings.ToLower(semCitacao(v))] }

// ---------------------------------------------------------------------------------------
// Quem escreveu: o texto original (com pseudônimos) de cada texto do assistente, pelo hash do
// texto desmascarado. Em RAM, com teto, e no enviados.log (só o HMAC do texto desmascarado e,
// para cada trecho traduzido, onde está, o tipo e o pseudônimo: nenhum valor real).

// Traducao: um trecho do texto desmascarado que veio de um pseudônimo.
type Traducao struct {
	Ini, Fim int
	Pseudo   string
	Tipo     string // o tipo do pseudônimo ("obj.namespace", "email"...)
}

type escrito struct {
	original string
	ts       []trecho // os trechos traduzidos, em posições do texto desmascarado
}

type registroEscritos struct {
	mu    sync.Mutex
	atual map[[32]byte]escrito
	velho map[[32]byte]escrito
}

const maxEscritos = 20_000

func (r *registroEscritos) guardar(k [32]byte, e escrito) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.atual == nil {
		r.atual = map[[32]byte]escrito{}
	}
	if len(r.atual) >= maxEscritos {
		r.velho, r.atual = r.atual, map[[32]byte]escrito{}
	}
	r.atual[k] = e
}

func (r *registroEscritos) buscar(k [32]byte) (escrito, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.atual[k]; ok {
		return e, true
	}
	e, ok := r.velho[k]
	return e, ok
}

// idEscrito: como um texto do assistente é guardado em disco (HMAC do texto desmascarado).
func (m *Masker) idEscrito(des string) string { return m.p.raw("escrito", des, 16) }

// RegistrarResposta: o texto original que a API mandou e a tabela usada para desmascará-lo.
// Devolve o texto desmascarado (o mesmo que tab.Desmascarar). Todo texto é registrado, mesmo
// sem pseudônimo: o que o modelo escreveu sozinho é palavra comum (ele nunca viu o nome real).
func (m *Masker) RegistrarResposta(original string, tab *Tabela) string {
	des := tab.Desmascarar(original, false)
	if len(des) < 4 { // texto curto nunca é mascarado (ver Lote.MascararDica)
		return des
	}
	var ts []trecho
	if des != original {
		var b strings.Builder
		ult := 0
		tab.varrer(original, func(ini, fim int) {
			b.WriteString(original[ult:ini])
			p := original[ini:fim]
			a := b.Len()
			b.WriteString(tab.m[p])
			ts = append(ts, trecho{a, b.Len(), tab.tipo[p], p})
			ult = fim
		})
		b.WriteString(original[ult:])
		if b.String() != des { // a volta fez mais que trocar pseudônimos (sub-rede de IP): sem registro
			return des
		}
	}
	m.escritos.guardar(sha256.Sum256([]byte(des)), escrito{original, ts})
	if m.enviados != nil {
		m.enviados.Gravar(m.idEscrito(des), ts)
	}
	return des
}

// escritoDe: o registro de des (da RAM, ou do enviados.log depois de um reinício).
func (m *Masker) escritoDe(des string) (escrito, bool) {
	k := sha256.Sum256([]byte(des))
	if e, ok := m.escritos.buscar(k); ok {
		return e, true
	}
	if m.enviados == nil {
		return escrito{}, false
	}
	ts, ok := m.enviados.Buscar(m.idEscrito(des))
	if !ok {
		return escrito{}, false
	}
	orig, _, valido := remontar(des, ts)
	if !valido {
		return escrito{}, false
	}
	e := escrito{orig, ts}
	m.escritos.guardar(k, e)
	return e, true
}

// OriginalDe: o texto que a API mandou, se des é um texto do assistente desmascarado aqui.
func (m *Masker) OriginalDe(des string) (string, bool) {
	e, ok := m.escritoDe(des)
	return e.original, ok
}

// Traduzidas: os trechos de des que o proxy traduziu de um pseudônimo (ok=false: sem registro).
func (m *Masker) Traduzidas(des string) ([]Traducao, bool) {
	e, ok := m.escritoDe(des)
	if !ok {
		return nil, false
	}
	var tr []Traducao
	for _, t := range e.ts {
		tr = append(tr, Traducao{t.Ini, t.Fim, t.Pseudo, t.Tipo})
	}
	return tr, true
}
