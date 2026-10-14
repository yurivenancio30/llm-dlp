package mask

import (
	"path/filepath"
	"sync"
	"testing"
)

// Key created by several processes at the same time: all of them have to see the SAME key.
func TestConcurrentKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chave")
	var wg sync.WaitGroup
	keys := make([]string, 32)
	for i := range keys {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			k, err := Key(path)
			if err != nil {
				t.Error(err)
			}
			keys[i] = string(k)
		}(i)
	}
	wg.Wait()
	for _, k := range keys[1:] {
		if k != keys[0] {
			t.Fatal("processos concorrentes ficaram com chaves diferentes")
		}
	}
}
