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
func (m *Masker) Mascarar(s string) (string, []Entrada) {
	if len(s) < 4 {
		return s, nil
	}
	k := sha256.Sum256([]byte(s))
	m.mu.Lock()
	r, ok := m.memo[k]
	if !ok {
		if r, ok = m.velho[k]; ok {
			m.guardar(k, r) // ainda em uso: volta para a geração atual do memo
		}
	}
	m.mu.Unlock()
	g := m.conh.geracao()
	if ok {
		// O resultado memorizado continua valendo, a não ser que o texto contenha um valor
		// aprendido DEPOIS (ex.: o número apareceu solto antes de aparecer como "RG: ...").
		if r.gen == g {
			return r.texto, r.entradas
		}
		if !m.conh.contemDesde(s, r.gen) {
			r.gen = g // conferido até aqui: da próxima vez não confere de novo
			m.mu.Lock()
			m.guardar(k, r)
			m.mu.Unlock()
			return r.texto, r.entradas
		}
	}

	achados := m.Detectar(s)

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
	texto, entradas := m.aplicar(s, achados)

	m.mu.Lock()
	m.guardar(k, resultado{texto, entradas, g})
	m.mu.Unlock()
	return texto, entradas
}

// Persistir grava em disco o que foi aprendido e ainda não foi gravado (pessoas novas).
func (m *Masker) Persistir() {
	if m.pessoas != nil {
		m.pessoas.SalvarSeSujo()
	}
}

// Aquecer mascara em paralelo os textos que ainda não estão no memo. Serve para o momento em
// que chega muito texto novo de uma vez (primeira mensagem depois de um reinício, sessão
// retomada): em vez de um texto por vez, usa todos os núcleos. O resultado final é o mesmo,
// porque quem monta a requisição chama Mascarar em ordem logo depois (e Mascarar reconfere
// o texto memorizado contra valores aprendidos nesse meio-tempo).
func (m *Masker) Aquecer(textos []string) {
	chaves := make([][32]byte, len(textos))
	for i, t := range textos {
		chaves[i] = sha256.Sum256([]byte(t))
	}
	var pend []string
	total := 0
	visto := map[[32]byte]bool{}
	m.mu.Lock()
	for i, t := range textos {
		_, a := m.memo[chaves[i]]
		_, b := m.velho[chaves[i]]
		if !a && !b && !visto[chaves[i]] {
			visto[chaves[i]] = true
			pend = append(pend, t)
			total += len(t)
		}
	}
	m.mu.Unlock()
	if len(pend) < 2 || total < aquecerMin {
		return
	}
	// os maiores primeiro, para as tarefas ficarem equilibradas
	sort.Slice(pend, func(i, j int) bool { return len(pend[i]) > len(pend[j]) })
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
				m.Mascarar(pend[i])
			}
		}()
	}
	wg.Wait()
}

// aquecerMin: abaixo disso de texto novo, o paralelismo não compensa
const aquecerMin = 48 << 10

// guardar põe o resultado no memo (com m.mu travado). O memo tem duas gerações: quando a
// atual enche, vira a "velha" e começa outra vazia; o que continua em uso é trazido de
// volta na consulta, e o resto some quando a velha é descartada. Assim a RAM tem teto e
// não há um momento em que tudo é esquecido de uma vez.
func (m *Masker) guardar(k [32]byte, r resultado) {
	if _, ok := m.memo[k]; !ok {
		if m.bytes > memoMax/2 {
			m.velho, m.memo, m.bytes = m.memo, map[[32]byte]resultado{}, 0
		}
		m.bytes += len(r.texto) + 96*len(r.entradas) + 64
	}
	m.memo[k] = r
}
