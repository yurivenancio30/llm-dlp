package mask

import (
	"sort"
	"strings"
)

// Memória da conversa (ver docs/estruturas.md): os nomes decididos em TODOS os textos de uma
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
// RegistrarResposta) volta como a API o mandou: voltam a ser pseudônimo exatamente os trechos
// que o proxy traduziu, e a memória não se aplica ao resto (o modelo nunca viu o nome real; se
// escreveu a palavra, é palavra comum). As palavras traduzidas entram na memória (regra
// "traduzida", com o tipo do pseudônimo).

// nomeMem: um nome da memória, indexado pela primeira palavra dele.
type nomeMem struct {
	nome     string
	off      int // onde começa a primeira palavra dentro do nome
	ent      string
	semCaixa bool // tipo de SQL (ou servidor): casa em qualquer caixa
}

type memoria struct {
	exata    map[string][]nomeMem // primeira palavra -> nomes (do maior para o menor)
	semCaixa map[string][]nomeMem // primeira palavra em minúsculas -> nomes
	n        int
	// tamanhos das primeiras palavras (bit n = tamanho n; bit 63 = 63 ou mais): a palavra de
	// outro tamanho nem é consultada
	tamExata, tamSemCaixa uint64
}

func bitTam(n int) uint64 { return 1 << min(n, 63) }

// memMin: nome mais curto que a memória aplica fora do texto onde foi decidido (uma coluna
// "a" de um SELECT não pode mascarar toda letra "a" da conversa).
const memMin = 3

// ehPalavra: byte que faz parte de uma palavra (os limites de palavra da memória). Byte de
// UTF-8 conta como letra: "ação" é uma palavra só.
func ehPalavra(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// novaMemoria: as decisões, na ordem (a primeira decisão de uma chave define o tipo).
func (m *Masker) novaMemoria(decs []Decisao) *memoria {
	mm := &memoria{exata: map[string][]nomeMem{}, semCaixa: map[string][]nomeMem{}}
	visto := map[string]bool{}
	for _, d := range decs {
		if d.Generica || len(d.Nome) < memMin {
			continue
		}
		k := d.Chave()
		if visto[k] {
			continue
		}
		visto[k] = true
		if !m.objMascara(d.Ent) || ehPseudoObj(d.Nome) {
			continue
		}
		i := 0
		for i < len(d.Nome) && !ehPalavra(d.Nome[i]) {
			i++
		}
		j := i
		letra := false
		for j < len(d.Nome) && ehPalavra(d.Nome[j]) {
			if c := d.Nome[j]; c < '0' || c > '9' {
				letra = true
			}
			j++
		}
		if i == j || !letra && j-i == len(d.Nome) {
			continue // sem palavra, ou só número
		}
		nm := nomeMem{nome: d.Nome, off: i, ent: d.Ent, semCaixa: entSQL[d.Ent]}
		if nm.semCaixa {
			t := strings.ToLower(d.Nome[i:j])
			mm.semCaixa[t] = append(mm.semCaixa[t], nm)
			mm.tamSemCaixa |= bitTam(j - i)
		} else {
			mm.exata[d.Nome[i:j]] = append(mm.exata[d.Nome[i:j]], nm)
			mm.tamExata |= bitTam(j - i)
		}
		mm.n++
	}
	if mm.n == 0 {
		return nil
	}
	for _, idx := range []map[string][]nomeMem{mm.exata, mm.semCaixa} {
		for _, ns := range idx {
			sort.SliceStable(ns, func(a, b int) bool { return len(ns[a].nome) > len(ns[b].nome) })
		}
	}
	return mm
}

// casar: o nome nm ocupa s a partir de ini (palavra inteira)?
func (nm nomeMem) casar(s string, ini int) bool {
	fim := ini + len(nm.nome)
	if ini < 0 || fim > len(s) {
		return false
	}
	if nm.semCaixa {
		if !strings.EqualFold(s[ini:fim], nm.nome) {
			return false
		}
	} else if s[ini:fim] != nm.nome {
		return false
	}
	if ehPalavra(nm.nome[0]) && ini > 0 && ehPalavra(s[ini-1]) {
		return false
	}
	return !(ehPalavra(nm.nome[len(nm.nome)-1]) && fim < len(s) && ehPalavra(s[fim]))
}

// varrer: os trechos de s em que a memória casa, fora dos trechos já trocados (ts, em ordem).
// Uma passada: cada palavra de s é uma consulta no índice.
func (mm *memoria) varrer(s string, ts []trecho) []trecho {
	var out []trecho
	var buf []byte
	k := 0
	for i := 0; i < len(s); {
		if !ehPalavra(s[i]) {
			i++
			continue
		}
		j := i + 1
		for j < len(s) && ehPalavra(s[j]) {
			j++
		}
		var melhor *nomeMem
		ini := 0
		tentar := func(ns []nomeMem) {
			for x := range ns {
				nm := &ns[x]
				if melhor != nil && len(nm.nome) <= len(melhor.nome) {
					return
				}
				if a := i - nm.off; nm.casar(s, a) {
					melhor, ini = nm, a
					return
				}
			}
		}
		if mm.tamExata&bitTam(j-i) != 0 {
			tentar(mm.exata[s[i:j]])
		}
		if mm.tamSemCaixa&bitTam(j-i) != 0 {
			buf = buf[:0]
			for x := i; x < j; x++ {
				c := s[x]
				if c >= 'A' && c <= 'Z' {
					c += 32
				}
				buf = append(buf, c)
			}
			tentar(mm.semCaixa[string(buf)])
		}
		if melhor == nil {
			i = j
			continue
		}
		fim := ini + len(melhor.nome)
		for k < len(ts) && ts[k].Fim <= ini {
			k++
		}
		livre := (k >= len(ts) || ts[k].Ini >= fim) && (len(out) == 0 || out[len(out)-1].Fim <= ini)
		if livre {
			out = append(out, trecho{Ini: ini, Fim: fim, Tipo: prefTipoObj + melhor.ent})
			i = fim
			continue
		}
		i = j
	}
	return out
}

// comMemoria: o resultado de s com a memória da conversa aplicada (os trechos novos entram
// nas entradas e nos trechos, para a volta e para o congelamento).
func (l *Lote) comMemoria(s string, r resultado) resultado {
	if l.mem == nil {
		return r
	}
	extra := l.mem.varrer(s, r.trechos)
	if len(extra) == 0 {
		return r
	}
	for i := range extra {
		extra[i].Pseudo = l.m.Pseudonimo(extra[i].Tipo, s[extra[i].Ini:extra[i].Fim])
	}
	ts := make([]trecho, 0, len(r.trechos)+len(extra))
	a, b := 0, 0
	for a < len(r.trechos) || b < len(extra) {
		if b >= len(extra) || a < len(r.trechos) && r.trechos[a].Ini < extra[b].Ini {
			ts = append(ts, r.trechos[a])
			a++
		} else {
			ts = append(ts, extra[b])
			b++
		}
	}
	texto, entradas, ok := remontar(s, ts)
	if !ok {
		return r
	}
	return resultado{texto: texto, entradas: entradas, trechos: ts, gen: r.gen, semAprender: r.semAprender, decididos: r.decididos}
}

// Memoria monta a memória da conversa com os nomes decididos nos textos da requisição (depois
// de Aquecer, quando todo texto novo já está no memo) e nos trechos traduzidos dos textos do
// assistente (extras). Conteúdo da internet não entra (é mascarado, mas não ensina).
func (l *Lote) Memoria(itens []ItemLote, extras []Decisao) {
	m := l.m
	decs := make([][]Decisao, len(itens))
	var faltam []itemAquecer
	var iFaltam []int
	for i, it := range itens {
		if it.DaWeb || len(it.S) < 4 {
			continue
		}
		if r, ok := m.congelado(it.Pos, it.S); ok && r.decididos != nil {
			decs[i] = r.decididos
			continue
		}
		if r, ok := m.noMemo(it.S, it.Dica); ok {
			// o resultado vai direto para a montagem (MascararDica), sem calcular o hash de novo
			l.pre[it.Pos] = r
			if r.decididos != nil {
				decs[i] = r.decididos
				continue
			}
		}
		// texto que saiu antes de um reinício (o enviados.log só tem os trechos) ou que saiu do
		// memo: as decisões dele são recalculadas uma vez
		faltam = append(faltam, itemAquecer{it.S, true, it.Dica})
		iFaltam = append(iFaltam, i)
	}
	if len(faltam) > 0 {
		m.aquecer(faltam)
		for _, i := range iFaltam {
			it := itens[i]
			decs[i], _ = m.decididosMemo(it.S, it.Dica)
			m.mu.Lock()
			if r, ok := m.cong[it.Pos]; ok && r.decididos == nil {
				r.decididos = decs[i]
				if r.decididos == nil {
					r.decididos = []Decisao{}
				}
				m.cong[it.Pos] = r
			}
			m.mu.Unlock()
		}
	}
	var todas []Decisao
	vistas := map[*Decisao]bool{} // o mesmo texto repetido traz as mesmas decisões (do memo)
	for _, d := range decs {
		if len(d) == 0 || vistas[&d[0]] {
			continue
		}
		vistas[&d[0]] = true
		todas = append(todas, d...)
	}
	todas = append(todas, extras...)
	if len(todas) > 0 {
		l.mem = m.novaMemoria(todas)
	}
}

// decididosMemo: as decisões de s no memo (nil, false: s não está no memo).
func (m *Masker) decididosMemo(s, dica string) ([]Decisao, bool) {
	r, ok := m.noMemo(s, dica)
	if !ok || r.decididos == nil {
		return nil, false
	}
	return r.decididos, true
}

// noMemo: o resultado de s no memo, como mascararKE o devolveria (sem detectar de novo).
func (m *Masker) noMemo(s, dica string) (resultado, bool) {
	k := chaveMemo(s, dica)
	m.mu.Lock()
	r, ok := m.memo[k]
	if !ok {
		if r, ok = m.velho[k]; ok {
			r = m.guardar(k, r)
		}
	}
	m.mu.Unlock()
	if !ok {
		return r, false
	}
	// valor aprendido depois que o resultado foi calculado e que aparece em s: refaz (como em
	// mascararKE; conferido, o resultado fica marcado com a geração atual)
	if g := m.conh.geracao(); r.gen != g {
		if m.conh.contemDesde(s, r.gen) {
			return r, false
		}
		r.gen = g
		m.mu.Lock()
		r = m.guardar(k, r)
		m.mu.Unlock()
	}
	return r, true
}

// Escrito: des é um texto do assistente desmascarado aqui (há registro)? Devolve as decisões
// dos trechos traduzidos (regra "traduzida", com o tipo do pseudônimo), para a memória.
func (l *Lote) Escrito(des string) ([]Decisao, bool) {
	if len(des) < 4 {
		return nil, false
	}
	e, ok := l.m.escritoDe(des)
	if !ok {
		return nil, false
	}
	return decisoesTraduzidas(des, e.ts), true
}

func decisoesTraduzidas(des string, ts []trecho) []Decisao {
	out := []Decisao{}
	for _, t := range ts {
		if !ehObjeto(t.Tipo) {
			continue
		}
		v := des[t.Ini:t.Fim]
		out = append(out, Decisao{Nome: v, Ent: strings.TrimPrefix(t.Tipo, prefTipoObj), Regra: "traduzida", Generica: ehGenerica(v)})
	}
	return out
}

// MascararEscrito: como MascararDica, para um texto do assistente. Com registro (ver
// RegistrarResposta), sai o que a API mandou: só os trechos traduzidos voltam a ser
// pseudônimo. Sem registro (reinício sem enviados.log, outro processo), é um texto como
// os outros.
func (l *Lote) MascararEscrito(des string, daWeb bool, pos Posicao, dica string) (string, []Entrada) {
	if len(des) < 4 {
		return des, nil
	}
	if r, ok := l.m.congelado(pos, des); ok {
		return r.texto, r.entradas
	}
	if e, ok := l.m.escritoDe(des); ok && !daWeb {
		if texto, entradas, valido := remontar(des, e.ts); valido {
			r := resultado{texto: texto, entradas: entradas, trechos: e.ts, decididos: decisoesTraduzidas(des, e.ts)}
			if _, ja := l.saidas[pos]; !ja {
				l.saidas[pos] = r
			}
			return r.texto, r.entradas
		}
	}
	return l.MascararDica(des, daWeb, pos, dica)
}
