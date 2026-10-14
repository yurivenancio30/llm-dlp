package mask

import (
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// Load tests (only with LLM_DLP_STRESS=1).

// Stress tests: heavy, they only run with LLM_DLP_STRESS=1.
//
//	LLM_DLP_STRESS=1 go test ./internal/mask -run Stress -v -timeout 60m
func stressOnly(t *testing.T) {
	if os.Getenv("LLM_DLP_STRESS") == "" {
		t.Skip("set LLM_DLP_STRESS=1 to run")
	}
}

func heapMB() float64 {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return float64(ms.HeapAlloc) / (1 << 20)
}

// varied text: dense log, code, paths/URLs and prose with data, always distinct
func variedText(i int) string {
	switch i % 4 {
	case 0:
		return denseText(i)
	case 1:
		return commonText(i)
	case 2:
		return pathsText(i)
	}
	var b strings.Builder
	for j := 0; j < 40; j++ {
		k := i*40 + j
		fmt.Fprintf(&b, "cliente %d: RG: %02d.%03d.%03d-%d, tel (11) 9%04d-%04d, senha: Abc%07d!x, CEP %05d-%03d, conta %04d-%d\n",
			k, 10+k%80, k%1000, (k*7)%1000, k%10, k%10000, (k*3)%10000, k, k%100000, k%1000, k%10000, k%10)
	}
	return b.String()
}

// 1) Memory: 1.2 GB of distinct text, in parallel. The heap has to stabilize.
func TestStressMemoryText(t *testing.T) {
	stressOnly(t)
	vs, _ := LoadSeen(t.TempDir() + "/v.json")
	base := newTest(t)
	m, _ := NewMasker(base.cfg, testKey, base.people, vs)
	const total, batch = 1200 << 20, 100 << 20
	var mu sync.Mutex
	next, done := 0, 0
	var samples []float64
	t0 := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				if done >= total {
					mu.Unlock()
					return
				}
				i := next
				next++
				mu.Unlock()
				s := variedText(i)
				m.Mask(s)
				mu.Lock()
				before := done / batch
				done += len(s)
				if done/batch != before {
					h := heapMB()
					samples = append(samples, h)
					t.Logf("%5d MB processed | heap %6.1f MB | values in RAM %d | hashes on disk %d | %s",
						done>>20, h, m.known.generation()-m.known.base, len(vs.ids), time.Since(t0).Round(time.Second))
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	n := len(samples)
	larger, end := 0.0, 0.0
	for i, a := range samples {
		if a > larger {
			larger = a
		}
		if i >= n*2/3 {
			end += a / float64(n-n*2/3)
		}
	}
	t.Logf("max heap %.1f MB, average of the last third %.1f MB, throughput %.1f MB/s (%d cores)", larger, end,
		float64(total>>20)/time.Since(t0).Seconds(), runtime.NumCPU())
	if larger > 450 {
		t.Fatalf("heap passed 450 MB: %.1f", larger)
	}
	if samples[n-1] > samples[n/2]*1.25+20 {
		t.Fatalf("heap still growing at the end: half %.1f MB, end %.1f MB", samples[n/2], samples[n-1])
	}
}

// 2) Many learned values: the time per new snippet cannot grow with the amount.
func TestStressLearnedValues(t *testing.T) {
	stressOnly(t)
	dir := t.TempDir()
	vs, _ := LoadSeen(dir + "/v.json")
	m, _ := NewMasker(newTest(t).cfg, testKey, nil, vs)
	measure := func() time.Duration {
		var ts []time.Duration
		for i := 0; i < 60; i++ {
			s := variedText(1_000_000 + rand.Intn(1_000_000)*4 + 1) // common code, always new
			t0 := time.Now()
			m.Mask(s)
			ts = append(ts, time.Since(t0))
		}
		sort.Slice(ts, func(i, j int) bool { return ts[i] < ts[j] })
		return ts[len(ts)/2]
	}
	base := measure()
	t.Logf("%7d values | new snippet (6 KB) %s | heap %.1f MB", 0, base, heapMB())
	learnedOnes, worst := 0, base
	for _, target := range []int{100, 1000, 10000, 50000, 120000} {
		for ; learnedOnes < target; learnedOnes++ {
			m.Mask(fmt.Sprintf("RG: %02d.%03d.%03d-%d e senha: Zx%08dq!%d", 10+learnedOnes%80, learnedOnes%1000, (learnedOnes/7)%1000, learnedOnes%10, learnedOnes, learnedOnes%13))
		}
		d := measure()
		if d > worst {
			worst = d
		}
		t.Logf("%7d values | new snippet (6 KB) %s | heap %.1f MB | in RAM %d | hashes %d | password lengths %d",
			target, d, heapMB(), len(m.known.reals), len(vs.ids), len(vs.size))
	}
	t0 := time.Now()
	vs.dirty = true
	vs.SaveIfDirty()
	saveNow := time.Since(t0)
	fi, _ := os.Stat(dir + "/v.json")
	t0 = time.Now()
	LoadSeen(dir + "/v.json")
	t.Logf("hashes file: %.1f MB | write %s | load %s", float64(fi.Size())/(1<<20), saveNow, time.Since(t0))
	if worst > base*4+2*time.Millisecond {
		t.Fatalf("time per snippet grew with the number of values: base %s, worst %s", base, worst)
	}
}

// 3) Pathological inputs: 2 MB of each. None may hang.
func TestStressPathological(t *testing.T) {
	stressOnly(t)
	rep := func(s string) string { return strings.Repeat(s, (2<<20)/len(s)+1)[:2<<20] }
	r := rand.New(rand.NewSource(3))
	b64 := make([]byte, 2<<20)
	for i := range b64 {
		b64[i] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"[r.Intn(64)]
	}
	cases := []struct{ name, text string }{
		{"uma linha só, sem espaço", rep("abcdefghij")},
		{"só dígitos", rep("0123456789")},
		{"dígitos com pontos (cara de IP/CPF)", rep("123.456.789-09 10.1.2.3 ")},
		{"só arrobas", rep("a@b@c@d.e@")},
		{"base64 aleatório contínuo", string(b64)},
		{"password= repetido", rep("password=Abc123!x&")},
		{"senha: repetida com valor diferente", func() string {
			var b strings.Builder
			for i := 0; b.Len() < 2<<20; i++ {
				fmt.Fprintf(&b, "senha: Qw%07d!z\n", i)
			}
			return b.String()
		}()},
		{"URLs longas com muitos separadores", rep("https://a.b/c?d=e&f=g:h/i#j|k+l=m&n=o:p/q?r=s ")},
		{"parênteses e telefones falsos", rep("(11) 91234-567 (11) 9 ")},
		{"JSON minificado", rep(`{"id":123456789,"email":"x@y.com.br","ip":"10.0.0.1","k":"v"},`)},
		{"unicode e acentos", rep("João Ávila da Conceição é ótimo, ação! ")},
		{"nomes com iniciais maiúsculas", rep("Maria Silva Joao Souza Ana Paula ")},
		{"bytes inválidos", rep("\xff\xfe\x00\x01 abc \xc3\x28 ")},
		{"espaços em branco", rep(" \t\n")},
	}
	for _, c := range cases {
		vs, _ := LoadSeen(t.TempDir() + "/v.json")
		base := newTest(t)
		m, _ := NewMasker(base.cfg, testKey, base.people, vs)
		m.Mask("DB_PASSWORD=Ficticia@2024 e RG: 12.345.678-9") // with values already learned
		t0 := time.Now()
		out, ents := m.Mask(c.text)
		d := time.Since(t0)
		back := NewTable(ents).Unmask(out, false)
		t.Logf("%-40s %8s  %6.2f MB/s  %7d replacements  round trip ok=%v", c.name, d.Round(time.Millisecond),
			2/d.Seconds(), len(ents), back == c.text)
		if d > 60*time.Second {
			t.Errorf("%s: took %s", c.name, d)
		}
	}
}

// 4) Concurrency with learning and cap at the same time: no race, no hang, stable result.
func TestStressConcurrency(t *testing.T) {
	stressOnly(t)
	vs, _ := LoadSeen(t.TempDir() + "/v.json")
	m, _ := NewMasker(newTest(t).cfg, testKey, nil, vs)
	fixed := "erro ao logar com Ficticia@2024 no host; RG 12.345.678-9 inválido"
	m.Mask("DB_PASSWORD=Ficticia@2024 e RG: 12.345.678-9")
	wants, _ := m.Mask(fixed)
	if strings.Contains(wants, "Ficticia@2024") || strings.Contains(wants, "12.345.678-9") {
		t.Fatalf("premise: %s", wants)
	}
	var wg sync.WaitGroup
	var errs sync.Map
	for w := 0; w < 32; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 4000; i++ {
				m.Mask(fmt.Sprintf("senha: Wk%02d%06dp! e RG: %02d.%03d.%03d-%d", w, i, 10+w, i%1000, (i*3)%1000, i%10))
				if got, _ := m.Mask(fixed); got != wants {
					errs.Store(got, true)
				}
				if i%500 == 0 {
					vs.SaveIfDirty()
				}
			}
		}(w)
	}
	wg.Wait()
	errs.Range(func(k, _ any) bool { t.Errorf("result changed under concurrency: %v", k); return false })
	t.Logf("128 thousand learnings in 32 goroutines | in RAM %d | hashes %d | heap %.1f MB", len(m.known.reals), len(vs.ids), heapMB())
}
