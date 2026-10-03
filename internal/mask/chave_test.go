package mask

import (
	"path/filepath"
	"sync"
	"testing"
)

// Chave criada por vários processos ao mesmo tempo: todos têm que ver a MESMA chave.
func TestChaveConcorrente(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chave")
	var wg sync.WaitGroup
	chaves := make([]string, 32)
	for i := range chaves {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			k, err := Chave(path)
			if err != nil {
				t.Error(err)
			}
			chaves[i] = string(k)
		}(i)
	}
	wg.Wait()
	for _, k := range chaves[1:] {
		if k != chaves[0] {
			t.Fatal("processos concorrentes ficaram com chaves diferentes")
		}
	}
}
