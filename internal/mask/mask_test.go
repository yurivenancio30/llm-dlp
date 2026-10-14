package mask

import (
	"fmt"
	"strings"
	"testing"
)

// The memo has a ceiling and does not forget what is in use when it switches generations.
func TestMemoTwoGenerations(t *testing.T) {
	m := newTest(t)
	inUse := "texto em uso joao.silva@empresa-ficticia.com.br"
	m.Mask(inUse)
	k := func() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.memo) + len(m.stale) }
	large := strings.Repeat("x", 1<<20)
	larger := 0
	for i := 0; i < 300; i++ { // 300 MB of distinct text
		m.mu.Lock() // puts it straight in the memo (detecting 1 MB each round would make the test slow)
		m.store(sha(fmt.Sprint(i)), result{text: fmt.Sprint(i, large)})
		m.mu.Unlock()
		m.Mask(inUse)
		m.mu.Lock()
		_, a := m.memo[sha(inUse)]
		_, b := m.stale[sha(inUse)]
		tot := 0
		for _, r := range m.memo {
			tot += len(r.text)
		}
		for _, r := range m.stale {
			tot += len(r.text)
		}
		m.mu.Unlock()
		if !a && !b {
			t.Fatalf("texto em uso foi esquecido na volta %d", i)
		}
		if tot > larger {
			larger = tot
		}
	}
	if larger > memoMax+(4<<20) {
		t.Fatalf("memo passou do teto: %d MB (%d textos)", larger>>20, k())
	}
}
