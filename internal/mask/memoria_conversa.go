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
	tem      map[string]bool // chaves já na memória (adicionar)
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
		if d.Generica || len(d.Nome) < memMin || publicoGeral(d.Nome) || m.softwareProvado(d.Nome) {
			continue
		}
		k := d.Chave()
		if visto[k] {
			continue
		}
		visto[k] = true
		if mm.tem == nil {
			mm.tem = map[string]bool{}
		}
		mm.tem[k] = true
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
func (mm *memoria) varrer(s string, ts []trecho) []trecho { return mm.varrerF(s, ts, filtroMem{}) }

// varrerF: como varrer, com os freios do texto (memoria_conversa_freios.go).
func (mm *memoria) varrerF(s string, ts []trecho, f filtroMem) []trecho {
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
		if livre && !f.pula(s, ini, fim, melhor) {
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
func (l *Lote) comMemoria(s string, r resultado, dica string) resultado {
	if l.mem == nil {
		l.mem = &memoria{exata: map[string][]nomeMem{}, semCaixa: map[string][]nomeMem{}}
	}
	f := filtroDe(dica)
	extra := l.mem.varrerF(s, r.trechos, f)
	if !l.escrevendo && fonteArquivo(l.fonteAtual) {
		// palavra comum provada numa fonte não contamina OUTRO arquivo lido (o vocabulário do
		// arquivo é dele: "status" no app.js não é a coluna do schema.sql); a conversa e as
		// saídas sem arquivo (kubectl logs, git branch) recebem o contágio
		k := extra[:0]
		for _, x := range extra {
			if v := s[x.Ini:x.Fim]; caraDeIdentificador(v) || l.provadoNaFonte(v) {
				k = append(k, x)
			}
		}
		extra = k
	}
	extra = l.semComunsNaProsa(s, extra)
	ts := l.juntarTrechos(r.trechos, extra)
	l.provAtual = map[string]bool{}
	for _, d := range r.decididos {
		if d.Regra != "âncora" {
			l.provAtual[strings.ToLower(d.Nome)] = true
		}
	}
	ts = l.semPublicos(s, ts)
	ts2 := l.ancorarPosicoes(s, ts, f)
	if novas := l.aprenderAncora(s, ts, ts2); len(novas) > 0 {
		// o valor deduzido vale nas outras ocorrências deste texto (regra 2)
		ts2 = l.juntarTrechos(ts2, l.semComunsNaProsa(s, l.mem.varrerF(s, ts2, f)))
		r.decididos = append(append([]Decisao{}, r.decididos...), novas...)
		extra = append(extra, trecho{})
	}
	if len(extra) == 0 && len(ts2) == len(r.trechos) && !mudouTipo(ts2, r.trechos) {
		return r
	}
	for i := range ts2 {
		if ts2[i].Pseudo == "" {
			ts2[i].Pseudo = l.m.Pseudonimo(ts2[i].Tipo, s[ts2[i].Ini:ts2[i].Fim])
		}
	}
	ts2 = semSobreporLongo(l.semPublicos(s, ts2)) // conflito se resolve; nunca se descarta tudo
	texto, entradas, ok := remontar(s, ts2)
	if !ok {
		return r
	}
	return resultado{texto: texto, entradas: entradas, trechos: ts2, gen: r.gen, semAprender: r.semAprender, decididos: r.decididos}
}

// semComunsNaProsa: na prosa (texto da mensagem), os trechos que a memória da conversa trouxe e
// que são palavra comum saem: lá ela é a palavra (vocab_palavras.go). Os que um leitor decidiu
// no próprio texto não passam por aqui.
func (l *Lote) semComunsNaProsa(s string, xs []trecho) []trecho {
	if !l.prosa {
		return xs
	}
	out := xs[:0:0]
	for _, x := range xs {
		if !l.m.comumLivre(s[x.Ini:x.Fim]) {
			out = append(out, x)
		}
	}
	return out
}

// mudouTipo: algum trecho mudou de tipo (âncora por posição corrigiu o genérico).
func mudouTipo(a, b []trecho) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i].Tipo != b[i].Tipo || a[i].Pseudo != b[i].Pseudo {
			return true
		}
	}
	return false
}

// juntarTrechos: os trechos do memo e os da memória, em ordem (sem pseudônimo nos novos).
func (l *Lote) juntarTrechos(base, extra []trecho) []trecho {
	r := struct{ trechos []trecho }{base}
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
	return ts
}

// Memoria monta a memória da conversa com os nomes decididos nos textos da requisição (depois
// de Aquecer, quando todo texto novo já está no memo) e nos trechos traduzidos dos textos do
// assistente (extras). Conteúdo da internet não entra (é mascarado, mas não ensina).
func (l *Lote) Memoria(itens []ItemLote, extras []Decisao) {
	fontesIt := make([]string, len(itens))
	for i := range itens {
		fontesIt[i], _ = SepararFonte(itens[i].Dica)
	}
	itens = semFontes(itens)
	defer func() { l.registrarFontes(itens, fontesIt) }()
	l.montarMemoria(l.decisoesDosItens(itens), extras)
}

// decisoesDosItens: as decisões de cada texto (do congelado, do memo ou recalculadas uma vez).
func (l *Lote) decisoesDosItens(itens []ItemLote) [][]Decisao {
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
			if decs[i] == nil {
				// saiu do memo enquanto os outros eram calculados (conversa maior que o memo):
				// calcula de novo, sem depender do memo
				r, _ := m.mascararD(it.S, true, it.Dica)
				decs[i] = r.decididos
			}
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
	return decs
}

// DecididosPorTexto: para a anterioridade, os nomes que cada texto decidiu por si (leitores e
// decisores daquele texto; nunca o que veio da memória da conversa ou do conhecimento
// acumulado), por texto. Texto sem decisões calculadas (da internet) fica de fora.
func (l *Lote) DecididosPorTexto(itens []ItemLote) map[string][]string {
	itens = semFontes(itens)
	decs := l.decisoesDosItens(itens)
	out := make(map[string][]string, len(itens))
	for i, it := range itens {
		if decs[i] == nil {
			continue
		}
		ns := out[it.S]
		if ns == nil {
			ns = []string{}
		}
		for _, d := range decs[i] {
			ns = append(ns, d.Nome)
		}
		out[it.S] = ns
	}
	return out
}

// juntarDecisoes: as decisões de todos os textos, cada texto repetido uma vez só.
func juntarDecisoes(decs [][]Decisao) []Decisao {
	var todas []Decisao
	vistas := map[*Decisao]bool{} // o mesmo texto repetido traz as mesmas decisões (do memo)
	for _, d := range decs {
		if len(d) == 0 || vistas[&d[0]] {
			continue
		}
		vistas[&d[0]] = true
		todas = append(todas, d...)
	}
	return todas
}

func (l *Lote) montarMemoria(decs [][]Decisao, extras []Decisao) {
	m := l.m
	todas := append(juntarDecisoes(decs), extras...)
	if len(todas) > 0 {
		if len(l.publicos) > 0 {
			k := todas[:0:0]
			for _, d := range todas {
				if !l.doModelo(d.Nome) {
					k = append(k, d)
				}
			}
			todas = k
		}
		l.mem = m.novaMemoria(todas)
		for _, d := range todas {
			if d.Regra == "âncora" {
				l.marcarDeduzido(d.Nome)
			}
		}
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

// MascararDicaProsa: como MascararDica, para o texto que o usuário escreveu na mensagem (não a
// saída de ferramenta): palavra comum que chega por contágio fica como palavra; a que um leitor
// decide ali mesmo (um DDL colado, kubectl -n) continua nome (vocab_palavras.go).
func (l *Lote) MascararDicaProsa(s string, daWeb bool, pos Posicao, dica string) (string, []Entrada) {
	l.prosa = true
	defer func() { l.prosa = false }()
	return l.MascararDica(s, daWeb, pos, dica)
}

// MascararProsa: como MascararEscrito, para um bloco de texto da resposta (não a entrada de
// ferramenta): palavra comum decidida em outro lugar fica como palavra (vocab_palavras.go).
func (l *Lote) MascararProsa(des string, daWeb bool, pos Posicao, dica string) (string, []Entrada) {
	l.prosa = true
	defer func() { l.prosa = false }()
	return l.MascararEscrito(des, daWeb, pos, dica)
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
	if !daWeb {
		return l.MascararContagio(des, daWeb, pos) // regra 1: o assistente não é fonte
	}
	return l.MascararDica(des, daWeb, pos, dica)
}
