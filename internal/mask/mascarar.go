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
// O proxy usa um Lote, que além disso congela o que saiu (ver enviados.go).
func (m *Masker) Mascarar(s string) (string, []Entrada) {
	r, _ := m.mascarar(s, true)
	return r.texto, r.entradas
}

func (m *Masker) mascarar(s string, aprende bool) (resultado, [32]byte) {
	if len(s) < 4 {
		return resultado{texto: s}, [32]byte{}
	}
	k := sha256.Sum256([]byte(s))
	m.mu.Lock()
	r, ok := m.memo[k]
	if !ok {
		if r, ok = m.velho[k]; ok {
			r = m.guardar(k, r) // ainda em uso: volta para a geração atual do memo
		}
	}
	m.mu.Unlock()
	g := m.conh.geracao()
	if ok && r.congelado {
		// Já saiu: sai igual, mesmo que agora se saiba mais. Reescrever não protegeria nada
		// (o texto já foi enviado assim) e regravaria a conversa inteira no cache da API.
		if aprende && r.semAprender {
			m.detectar(s, true) // o mesmo texto chegou de fonte local: aprende com ele
			r.semAprender = false
			m.mu.Lock()
			r = m.guardar(k, r)
			m.mu.Unlock()
		}
		return r, k
	}
	if ok && !(aprende && r.semAprender) {
		// Ainda não saiu: vale, a não ser que contenha um valor aprendido DEPOIS (por outro
		// texto da mesma requisição, por exemplo).
		if r.gen == g {
			return r, k
		}
		if !m.conh.contemDesde(s, r.gen) {
			r.gen = g // conferido até aqui: da próxima vez não confere de novo
			m.mu.Lock()
			r = m.guardar(k, r)
			m.mu.Unlock()
			return r, k
		}
	}
	if !ok && m.enviados != nil {
		// saiu antes de um reinício: remonta exatamente como saiu
		if ts, achou := m.enviados.Buscar(m.idTexto(s)); achou {
			if texto, entradas, valido := remontar(s, ts); valido {
				m.mu.Lock()
				r = m.guardar(k, resultado{texto, entradas, ts, g, true, false})
				m.mu.Unlock()
				return r, k
			}
		}
	}

	achados := m.detectar(s, aprende)

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

	m.mu.Lock()
	r = m.guardar(k, resultado{texto, entradas, ts, g, false, !aprende})
	m.mu.Unlock()
	return r, k
}

// idTexto: o HMAC do texto, que é como ele é guardado em disco.
func (m *Masker) idTexto(s string) string { return m.p.raw("enviado", s, 16) }

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

// Lote: os textos de uma requisição. Depois que a requisição sai, Congelar fixa o
// resultado de cada um.
type Lote struct {
	m      *Masker
	textos map[[32]byte]string
}

func (m *Masker) NovoLote() *Lote { return &Lote{m: m, textos: map[[32]byte]string{}} }

// Mascarar: daWeb = conteúdo da internet (resultado de WebFetch/WebSearch): é mascarado,
// mas nada dele é lembrado.
func (l *Lote) Mascarar(s string, daWeb bool) (string, []Entrada) {
	r, k := l.m.mascarar(s, !daWeb)
	if len(s) >= 4 {
		l.textos[k] = s
	}
	return r.texto, r.entradas
}

// Aquecer mascara antes da montagem todos os textos novos da requisição, para que todo
// valor aprendido nela já valha quando os textos forem montados em ordem.
func (l *Lote) Aquecer(locais, daWeb []string) {
	itens := make([]itemAquecer, 0, len(locais)+len(daWeb))
	for _, s := range daWeb {
		itens = append(itens, itemAquecer{s, false})
	}
	for _, s := range locais {
		itens = append(itens, itemAquecer{s, true})
	}
	l.m.aquecer(itens)
}

// Congelar: a requisição saiu. O resultado de cada texto fica fixo (e vai para o disco).
func (l *Lote) Congelar() {
	m := l.m
	type novo struct {
		s  string
		ts []trecho
	}
	var novos []novo
	m.mu.Lock()
	for k, s := range l.textos {
		r, ok := m.memo[k]
		if !ok {
			if r, ok = m.velho[k]; !ok {
				continue // saiu do memo (requisição enorme): será refeito
			}
		}
		if r.congelado {
			continue
		}
		r.congelado = true
		m.guardar(k, r)
		novos = append(novos, novo{s, r.trechos})
	}
	m.mu.Unlock()
	if m.enviados != nil {
		for _, n := range novos {
			m.enviados.Gravar(m.idTexto(n.s), n.ts)
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
		itens[i] = itemAquecer{t, true}
	}
	m.aquecer(itens)
}

type itemAquecer struct {
	s       string
	aprende bool
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
		k := sha256.Sum256([]byte(it.s))
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
			m.mascarar(it.s, it.aprende)
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
				m.mascarar(pend[i].s, pend[i].aprende)
			}
		}()
	}
	wg.Wait()
}

// aquecerMin: abaixo disso de texto novo, o paralelismo não compensa
const aquecerMin = 48 << 10

// guardar põe o resultado no memo (com m.mu travado) e devolve o que ficou: um resultado
// congelado nunca é trocado por outro (uma requisição em paralelo pode ter calculado outro).
// O memo tem duas gerações: quando a atual enche, vira a "velha" e começa outra vazia; o
// que continua em uso é trazido de volta na consulta, e o resto some quando a velha é
// descartada. Assim a RAM tem teto e não há um momento em que tudo é esquecido de uma vez.
func (m *Masker) guardar(k [32]byte, r resultado) resultado {
	ant, ok := m.memo[k]
	if !ok {
		ant, ok = m.velho[k]
		ok = ok && ant.congelado // da geração velha só importa se estiver congelado
	}
	if ok && ant.congelado && !r.congelado {
		r = ant
	}
	if _, noMemo := m.memo[k]; !noMemo {
		if m.bytes > memoMax/2 {
			m.velho, m.memo, m.bytes = m.memo, map[[32]byte]resultado{}, 0
		}
		m.bytes += len(r.texto) + 96*len(r.entradas) + 64
	}
	m.memo[k] = r
	return r
}
