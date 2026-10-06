package mask

import (
	"crypto/sha256"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
)

// Mascarar: a porta de entrada. Memoriza resultados, porque o Claude Code reenvia a conversa
// inteira a cada mensagem.

// Mascarar e a memória de resultados (o Claude Code reenvia a conversa inteira a cada mensagem).

const memoMax = 64 << 20 // teto (aproximado) do texto memorizado, somando as duas gerações

// Mascarar troca o dado sensível de s por pseudônimos e devolve as entradas usadas.
// Resultados são memorizados: o Claude Code reenvia a conversa inteira a cada mensagem.
// O proxy usa um Lote, que além disso congela o que saiu, por posição (ver enviados.go).
func (m *Masker) Mascarar(s string) (string, []Entrada) {
	r, _ := m.mascarar(s, true)
	return r.texto, r.entradas
}

func (m *Masker) mascarar(s string, aprende bool) (resultado, [32]byte) {
	return m.mascararD(s, aprende, "")
}

// mascararD: com a dica do comando que produziu o texto (ver comando.go; "" = sem dica).
func (m *Masker) mascararD(s string, aprende bool, dica string) (resultado, [32]byte) {
	if len(s) < 4 {
		return resultado{texto: s}, [32]byte{}
	}
	k := chaveMemo(s, dica)
	return m.mascararKE(s, k, aprende, lerDica(dica), dica), k
}

// chaveMemo: a chave do texto na memória de resultados (com a dica, se houver: o mesmo texto
// pode sair de comandos diferentes).
func chaveMemo(s, dica string) [32]byte {
	if dica == "" {
		return sha256.Sum256([]byte(s))
	}
	h := sha256.New()
	h.Write([]byte("dica\x00" + dica + "\x00"))
	h.Write([]byte(s))
	var k [32]byte
	copy(k[:], h.Sum(nil))
	return k
}

// mascararK: com o hash do texto já calculado (k).
func (m *Masker) mascararK(s string, k [32]byte, aprende bool, d *dicaSaida) resultado {
	return m.mascararKE(s, k, aprende, d, "")
}

// mascararKE: com a dica crua (a extensão dela vai para os decisores).
func (m *Masker) mascararKE(s string, k [32]byte, aprende bool, d *dicaSaida, dica string) resultado {
	m.mu.Lock()
	r, ok := m.memo[k]
	if !ok {
		if r, ok = m.velho[k]; ok {
			r = m.guardar(k, r) // ainda em uso: volta para a geração atual do memo
		}
	}
	m.mu.Unlock()
	g := m.conh.geracao()
	if ok && !(aprende && r.semAprender) {
		// Vale, a não ser que contenha um valor aprendido DEPOIS (por outro texto, por exemplo).
		if r.gen == g {
			return r
		}
		if !m.conh.contemDesde(s, r.gen) {
			r.gen = g // conferido até aqui: da próxima vez não confere de novo
			m.mu.Lock()
			r = m.guardar(k, r)
			m.mu.Unlock()
			return r
		}
	}
	_, ext := partesDica(dica)
	achados, decididos := m.detectarDE(s, aprende, d, ext)

	// Até que geração este resultado vale? Se nada foi aprendido durante a detecção, até g.
	// Se algo foi aprendido (por este texto ou por outro, em paralelo), confere: todo valor
	// novo que aparece em s já está entre os achados? Então vale até a geração atual. Se
	// algum ficou de fora (aprendido por outro texto depois que este já tinha sido varrido),
	// fica marcado com g, e a próxima consulta refaz.
	if g1 := m.conh.geracao(); g1 != g {
		cobertos := make(map[[2]int]bool, len(achados))
		for _, a := range achados {
			cobertos[[2]int{a.Ini, a.Fim}] = true
		}
		falta := false
		m.conh.varrer(s, g, func(ini, fim int, _ string) bool {
			falta = !cobertos[[2]int{ini, fim}]
			return !falta
		})
		if !falta {
			g = g1
		}
	}
	texto, entradas, ts := m.aplicarT(s, achados)
	if decididos == nil {
		decididos = []Decisao{} // calculado, sem decisões (nil = não se sabe: ver Lote.Memoria)
	}

	m.mu.Lock()
	r = m.guardar(k, resultado{texto, entradas, ts, g, !aprende, decididos})
	m.mu.Unlock()
	return r
}

// idPos: como um texto numa posição é guardado em disco (HMAC da chave de posição).
func (m *Masker) idPos(kc Posicao) string { return m.p.raw("enviado", string(kc[:]), 16) }

// UsarEnviados liga o registro em disco do que já saiu (só o proxy usa). impressao
// identifica a configuração e a versão: se mudarem, os registros antigos não valem.
func (m *Masker) UsarEnviados(path, impressao string) error {
	e, err := CarregarEnviados(path, m.p.raw("impressao", impressao, 16))
	if err != nil {
		return err
	}
	m.enviados = e
	return nil
}

// Posicao identifica um texto num ponto exato da conversa: um hash de tudo o que vem antes
// dele na requisição e dele mesmo (quem calcula é o proxy). O congelamento vale só para a
// mesma Posicao: o reenvio exato daquele ponto da conversa sai igual; o mesmo texto noutra
// conversa, ou numa mensagem nova, é mascarado com o que se sabe agora.
type Posicao [32]byte

// congelado: o resultado com que o texto saiu nesta posição, se já saiu (da memória, ou do
// enviados.log depois de um reinício).
func (m *Masker) congelado(kc Posicao, s string) (resultado, bool) {
	m.mu.Lock()
	r, ok := m.cong[kc]
	if !ok {
		if r, ok = m.congVelho[kc]; ok {
			m.guardarCong(kc, r)
		}
	}
	m.mu.Unlock()
	if ok || m.enviados == nil {
		return r, ok
	}
	ts, achou := m.enviados.Buscar(m.idPos(kc))
	if !achou {
		return r, false
	}
	texto, entradas, valido := remontar(s, ts)
	if !valido {
		return r, false
	}
	r = resultado{texto: texto, entradas: entradas, trechos: ts}
	m.mu.Lock()
	m.guardarCong(kc, r)
	m.mu.Unlock()
	return r, true
}

// guardarCong: como guardar, para os congelados (com m.mu travado).
func (m *Masker) guardarCong(kc Posicao, r resultado) {
	if _, ok := m.cong[kc]; !ok {
		if m.congBytes > memoMax/2 {
			m.congVelho, m.cong, m.congBytes = m.cong, map[Posicao]resultado{}, 0
		}
		m.congBytes += len(r.texto) + 96*len(r.entradas) + 64
	}
	m.cong[kc] = r
}

// Lote: os textos de uma requisição. Depois que a requisição sai, Congelar fixa o
// resultado de cada um na sua posição.
type Lote struct {
	m      *Masker
	saidas map[Posicao]resultado // o que saiu em cada posição
	mem    *memoria              // a memória da conversa (ver memoria.go); nil = vazia
	pre    map[Posicao]resultado // o resultado do memo de cada texto novo, já consultado em Memoria
	// rastreamento (ver rastreamento.go)
	deduzidos  map[string]bool            // valores marcados por dedução: não servem de semente
	fontes     map[string]map[string]bool // valor (minúsculas) -> fontes em que teve prova direta
	fonteAtual string                     // a fonte do texto sendo montado (vazia: sem comando)
	provAtual  map[string]bool            // decididos com prova no próprio texto sendo montado
	traduzidas map[string]bool            // palavras que o proxy traduziu (valem em qualquer fonte)
	escrevendo bool                       // montando texto do assistente (recebe todo o contágio)
}

func (m *Masker) NovoLote() *Lote {
	return &Lote{m: m, saidas: map[Posicao]resultado{}, pre: map[Posicao]resultado{}}
}

// ItemLote: um texto da requisição, onde está e se veio da internet.
type ItemLote struct {
	S     string
	DaWeb bool
	Pos   Posicao
	Dica  string // o que o comando que produziu o texto diz dele (Comandos.Dica)
}

// Mascarar: texto que já saiu nesta posição sai igual (reescrever não protegeria nada e
// regravaria a conversa no cache da API). daWeb = conteúdo da internet (resultado de
// WebFetch/WebSearch): é mascarado, mas nada dele é lembrado.
func (l *Lote) Mascarar(s string, daWeb bool, pos Posicao) (string, []Entrada) {
	return l.MascararDica(s, daWeb, pos, "")
}

// MascararDica: como Mascarar, com a dica do comando que produziu o texto.
func (l *Lote) MascararDica(s string, daWeb bool, pos Posicao, dica string) (string, []Entrada) {
	l.fonteAtual, dica = SepararFonte(dica)
	if len(s) < 4 {
		return s, nil
	}
	if r, ok := l.m.congelado(pos, s); ok {
		return r.texto, r.entradas
	}
	r, ok := l.pre[pos]
	if !ok || r.semAprender && !daWeb {
		r, _ = l.m.mascararD(s, !daWeb, dica)
	}
	r = l.comMemoria(s, r, dica)
	if _, ja := l.saidas[pos]; !ja {
		l.saidas[pos] = r
	}
	return r.texto, r.entradas
}

// Aquecer mascara antes da montagem os textos da requisição que ainda não saíram, para que
// todo valor aprendido nela já valha quando os textos forem montados em ordem.
func (l *Lote) Aquecer(itens []ItemLote) {
	itens = semFontes(itens)
	pend := make([]itemAquecer, 0, len(itens))
	for _, it := range itens {
		if len(it.S) < 4 {
			continue
		}
		if _, ok := l.m.congelado(it.Pos, it.S); ok {
			continue
		}
		pend = append(pend, itemAquecer{it.S, !it.DaWeb, it.Dica})
	}
	l.m.aquecer(pend)
}

// Congelar: a requisição saiu. O resultado de cada texto fica fixo na sua posição (e vai
// para o disco).
func (l *Lote) Congelar() {
	m := l.m
	novos := make([]Posicao, 0, len(l.saidas))
	m.mu.Lock()
	for kc, r := range l.saidas {
		if _, ok := m.cong[kc]; ok {
			continue
		}
		m.guardarCong(kc, r)
		novos = append(novos, kc)
	}
	m.mu.Unlock()
	if m.enviados != nil {
		for _, kc := range novos {
			m.enviados.Gravar(m.idPos(kc), l.saidas[kc].trechos)
		}
	}
}

// Persistir grava em disco o que foi aprendido e ainda não foi gravado (pessoas novas).
func (m *Masker) Persistir() {
	if m.pessoas != nil {
		m.pessoas.SalvarSeSujo()
	}
	if m.enviados != nil {
		m.enviados.Salvar()
	}
}

// Aquecer mascara em paralelo os textos que ainda não estão no memo. Serve para o momento em
// que chega muito texto novo de uma vez (primeira mensagem depois de um reinício, sessão
// retomada): em vez de um texto por vez, usa todos os núcleos. O resultado final é o mesmo,
// porque quem monta a requisição chama Mascarar em ordem logo depois (e Mascarar reconfere
// o texto ainda não enviado contra valores aprendidos nesse meio-tempo).
func (m *Masker) Aquecer(textos []string) {
	itens := make([]itemAquecer, len(textos))
	for i, t := range textos {
		itens[i] = itemAquecer{t, true, ""}
	}
	m.aquecer(itens)
}

type itemAquecer struct {
	s       string
	aprende bool
	dica    string
}

func (m *Masker) aquecer(itens []itemAquecer) {
	var pend []itemAquecer
	total := 0
	visto := map[[32]byte]bool{}
	m.mu.Lock()
	for _, it := range itens {
		if len(it.s) < 4 {
			continue
		}
		k := chaveMemo(it.s, it.dica)
		_, a := m.memo[k]
		_, b := m.velho[k]
		if !a && !b && !visto[k] {
			visto[k] = true
			pend = append(pend, it)
			total += len(it.s)
		}
	}
	m.mu.Unlock()
	if len(pend) < 2 || total < aquecerMin {
		// pouco texto: um por vez (mas antes da montagem, para todo aprendizado valer nela)
		for _, it := range pend {
			m.mascararD(it.s, it.aprende, it.dica)
		}
		return
	}
	// os maiores primeiro, para as tarefas ficarem equilibradas
	sort.Slice(pend, func(i, j int) bool { return len(pend[i].s) > len(pend[j].s) })
	n := runtime.GOMAXPROCS(0)
	if n > len(pend) {
		n = len(pend)
	}
	var prox atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < n; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(prox.Add(1)) - 1
				if i >= len(pend) {
					return
				}
				m.mascararD(pend[i].s, pend[i].aprende, pend[i].dica)
			}
		}()
	}
	wg.Wait()
}

// aquecerMin: abaixo disso de texto novo, o paralelismo não compensa
const aquecerMin = 48 << 10

// guardar põe o resultado no memo (com m.mu travado) e o devolve. O memo tem duas
// gerações: quando a atual enche, vira a "velha" e começa outra vazia; o que continua em uso
// é trazido de volta na consulta, e o resto some quando a velha é descartada. Assim a RAM
// tem teto e não há um momento em que tudo é esquecido de uma vez.
func (m *Masker) guardar(k [32]byte, r resultado) resultado {
	if _, ok := m.memo[k]; !ok {
		if m.bytes > memoMax/2 {
			m.velho, m.memo, m.bytes = m.memo, map[[32]byte]resultado{}, 0
		}
		m.bytes += len(r.texto) + 96*len(r.entradas) + 64
	}
	m.memo[k] = r
	return r
}
