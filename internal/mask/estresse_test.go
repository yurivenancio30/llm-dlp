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

// Testes de carga (só com LLM_DLP_ESTRESSE=1).

// Testes de estresse: pesados, rodam só com LLM_DLP_ESTRESSE=1.
//
//	LLM_DLP_ESTRESSE=1 go test ./internal/mask -run Estresse -v -timeout 60m
func soEstresse(t *testing.T) {
	if os.Getenv("LLM_DLP_ESTRESSE") == "" {
		t.Skip("defina LLM_DLP_ESTRESSE=1 para rodar")
	}
}

func heapMB() float64 {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return float64(ms.HeapAlloc) / (1 << 20)
}

// texto variado: log denso, código, caminhos/URLs e prosa com dados, sempre distinto
func textoVariado(i int) string {
	switch i % 4 {
	case 0:
		return textoDenso(i)
	case 1:
		return textoComum(i)
	case 2:
		return textoCaminhos(i)
	}
	var b strings.Builder
	for j := 0; j < 40; j++ {
		k := i*40 + j
		fmt.Fprintf(&b, "cliente %d: RG: %02d.%03d.%03d-%d, tel (11) 9%04d-%04d, senha: Abc%07d!x, CEP %05d-%03d, conta %04d-%d\n",
			k, 10+k%80, k%1000, (k*7)%1000, k%10, k%10000, (k*3)%10000, k, k%100000, k%1000, k%10000, k%10)
	}
	return b.String()
}

// 1) Memória: 1,2 GB de texto distinto, em paralelo. O heap tem que estabilizar.
func TestEstresseMemoriaTexto(t *testing.T) {
	soEstresse(t)
	vs, _ := CarregarVistos(t.TempDir() + "/v.json")
	base := novoTeste(t)
	m, _ := NovoMasker(base.cfg, chaveTeste, base.pessoas, vs)
	const total, lote = 1200 << 20, 100 << 20
	var mu sync.Mutex
	prox, feito := 0, 0
	var amostras []float64
	t0 := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				if feito >= total {
					mu.Unlock()
					return
				}
				i := prox
				prox++
				mu.Unlock()
				s := textoVariado(i)
				m.Mascarar(s)
				mu.Lock()
				antes := feito / lote
				feito += len(s)
				if feito/lote != antes {
					h := heapMB()
					amostras = append(amostras, h)
					t.Logf("%5d MB processados | heap %6.1f MB | valores em RAM %d | hashes em disco %d | %s",
						feito>>20, h, m.conh.geracao()-m.conh.base, len(vs.ids), time.Since(t0).Round(time.Second))
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	n := len(amostras)
	maior, fim := 0.0, 0.0
	for i, a := range amostras {
		if a > maior {
			maior = a
		}
		if i >= n*2/3 {
			fim += a / float64(n-n*2/3)
		}
	}
	t.Logf("heap máximo %.1f MB, média do último terço %.1f MB, vazão %.1f MB/s (%d núcleos)", maior, fim,
		float64(total>>20)/time.Since(t0).Seconds(), runtime.NumCPU())
	if maior > 450 {
		t.Fatalf("heap passou de 450 MB: %.1f", maior)
	}
	if amostras[n-1] > amostras[n/2]*1.25+20 {
		t.Fatalf("heap ainda crescendo no fim: metade %.1f MB, fim %.1f MB", amostras[n/2], amostras[n-1])
	}
}

// 2) Muitos valores aprendidos: o tempo por trecho novo não pode crescer com a quantidade.
func TestEstresseValoresAprendidos(t *testing.T) {
	soEstresse(t)
	dir := t.TempDir()
	vs, _ := CarregarVistos(dir + "/v.json")
	m, _ := NovoMasker(novoTeste(t).cfg, chaveTeste, nil, vs)
	medir := func() time.Duration {
		var ts []time.Duration
		for i := 0; i < 60; i++ {
			s := textoVariado(1_000_000 + rand.Intn(1_000_000)*4 + 1) // código comum, sempre novo
			t0 := time.Now()
			m.Mascarar(s)
			ts = append(ts, time.Since(t0))
		}
		sort.Slice(ts, func(i, j int) bool { return ts[i] < ts[j] })
		return ts[len(ts)/2]
	}
	base := medir()
	t.Logf("%7d valores | trecho novo (6 KB) %s | heap %.1f MB", 0, base, heapMB())
	aprendidos, pior := 0, base
	for _, alvo := range []int{100, 1000, 10000, 50000, 120000} {
		for ; aprendidos < alvo; aprendidos++ {
			m.Mascarar(fmt.Sprintf("RG: %02d.%03d.%03d-%d e senha: Zx%08dq!%d", 10+aprendidos%80, aprendidos%1000, (aprendidos/7)%1000, aprendidos%10, aprendidos, aprendidos%13))
		}
		d := medir()
		if d > pior {
			pior = d
		}
		t.Logf("%7d valores | trecho novo (6 KB) %s | heap %.1f MB | em RAM %d | hashes %d | tamanhos de senha %d",
			alvo, d, heapMB(), len(m.conh.reais), len(vs.ids), len(vs.tam))
	}
	t0 := time.Now()
	vs.sujo = true
	vs.SalvarSeSujo()
	salvar := time.Since(t0)
	fi, _ := os.Stat(dir + "/v.json")
	t0 = time.Now()
	CarregarVistos(dir + "/v.json")
	t.Logf("arquivo de hashes: %.1f MB | gravar %s | carregar %s", float64(fi.Size())/(1<<20), salvar, time.Since(t0))
	if pior > base*4+2*time.Millisecond {
		t.Fatalf("tempo por trecho cresceu com a quantidade de valores: base %s, pior %s", base, pior)
	}
}

// 3) Entradas patológicas: 2 MB de cada. Nenhuma pode travar.
func TestEstressePatologicos(t *testing.T) {
	soEstresse(t)
	rep := func(s string) string { return strings.Repeat(s, (2<<20)/len(s)+1)[:2<<20] }
	r := rand.New(rand.NewSource(3))
	b64 := make([]byte, 2<<20)
	for i := range b64 {
		b64[i] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"[r.Intn(64)]
	}
	casos := []struct{ nome, texto string }{
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
	for _, c := range casos {
		vs, _ := CarregarVistos(t.TempDir() + "/v.json")
		base := novoTeste(t)
		m, _ := NovoMasker(base.cfg, chaveTeste, base.pessoas, vs)
		m.Mascarar("DB_PASSWORD=Ficticia@2024 e RG: 12.345.678-9") // com valores já aprendidos
		t0 := time.Now()
		out, ents := m.Mascarar(c.texto)
		d := time.Since(t0)
		volta := NovaTabela(ents).Desmascarar(out, false)
		t.Logf("%-40s %8s  %6.2f MB/s  %7d substituições  ida e volta ok=%v", c.nome, d.Round(time.Millisecond),
			2/d.Seconds(), len(ents), volta == c.texto)
		if d > 60*time.Second {
			t.Errorf("%s: demorou %s", c.nome, d)
		}
	}
}

// 4) Concorrência com aprendizado e teto ao mesmo tempo: sem corrida, sem travar, resultado estável.
func TestEstresseConcorrencia(t *testing.T) {
	soEstresse(t)
	vs, _ := CarregarVistos(t.TempDir() + "/v.json")
	m, _ := NovoMasker(novoTeste(t).cfg, chaveTeste, nil, vs)
	fixo := "erro ao logar com Ficticia@2024 no host; RG 12.345.678-9 inválido"
	m.Mascarar("DB_PASSWORD=Ficticia@2024 e RG: 12.345.678-9")
	quer, _ := m.Mascarar(fixo)
	if strings.Contains(quer, "Ficticia@2024") || strings.Contains(quer, "12.345.678-9") {
		t.Fatalf("premissa: %s", quer)
	}
	var wg sync.WaitGroup
	var erros sync.Map
	for w := 0; w < 32; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 4000; i++ {
				m.Mascarar(fmt.Sprintf("senha: Wk%02d%06dp! e RG: %02d.%03d.%03d-%d", w, i, 10+w, i%1000, (i*3)%1000, i%10))
				if got, _ := m.Mascarar(fixo); got != quer {
					erros.Store(got, true)
				}
				if i%500 == 0 {
					vs.SalvarSeSujo()
				}
			}
		}(w)
	}
	wg.Wait()
	erros.Range(func(k, _ any) bool { t.Errorf("resultado mudou sob concorrência: %v", k); return false })
	t.Logf("128 mil aprendizados em 32 goroutines | em RAM %d | hashes %d | heap %.1f MB", len(m.conh.reais), len(vs.ids), heapMB())
}
