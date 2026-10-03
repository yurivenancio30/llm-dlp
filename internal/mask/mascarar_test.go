package mask

import (
	"fmt"
	"strings"
	"testing"
)

// O memo tem teto e não esquece o que está em uso quando troca de geração.
func TestMemoDuasGeracoes(t *testing.T) {
	m := novoTeste(t)
	emUso := "texto em uso joao.silva@empresa-ficticia.com.br"
	m.Mascarar(emUso)
	k := func() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.memo) + len(m.velho) }
	grande := strings.Repeat("x", 1<<20)
	maior := 0
	for i := 0; i < 300; i++ { // 300 MB de texto distinto
		m.mu.Lock() // põe direto no memo (detectar 1 MB a cada volta deixaria o teste lento)
		m.guardar(sha(fmt.Sprint(i)), resultado{texto: fmt.Sprint(i, grande)})
		m.mu.Unlock()
		m.Mascarar(emUso)
		m.mu.Lock()
		_, a := m.memo[sha(emUso)]
		_, b := m.velho[sha(emUso)]
		tot := 0
		for _, r := range m.memo {
			tot += len(r.texto)
		}
		for _, r := range m.velho {
			tot += len(r.texto)
		}
		m.mu.Unlock()
		if !a && !b {
			t.Fatalf("texto em uso foi esquecido na volta %d", i)
		}
		if tot > maior {
			maior = tot
		}
	}
	if maior > memoMax+(4<<20) {
		t.Fatalf("memo passou do teto: %d MB (%d textos)", maior>>20, k())
	}
}
