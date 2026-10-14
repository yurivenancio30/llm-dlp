package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Load tests of the whole proxy (only with LLM_DLP_STRESS=1).

// Load tests of the whole proxy: heavy, they only run with LLM_DLP_STRESS=1.
//
//	LLM_DLP_STRESS=1 go test ./internal/proxy -run Stress -v -timeout 60m
func stressOnly(t *testing.T) {
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

func buildStress(t *testing.T, upstream http.HandlerFunc) (px *httptest.Server) {
	t.Helper()
	cfg := config.Default()
	cfg.InternalDomains = []string{"empresa-ficticia"}
	key := []byte("0123456789abcdef0123456789abcdef")
	dir := t.TempDir()
	ps, _ := mask.LoadPeople(dir + "/p.json")
	ps.Import(mask.NewPseudo(key), "João Carlos Silva", email, "")
	vs, _ := mask.LoadSeen(dir + "/v.json")
	m, err := mask.NewMasker(cfg, key, ps, vs)
	if err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(upstream)
	cfg.Upstream = up.URL
	px = httptest.NewServer(New(cfg, m, vs, log.New(io.Discard, "", 0)))
	t.Cleanup(func() { px.Close(); up.Close() })
	return px
}

// data of one turn: ~4 KB with several data types, distinct per client and turn
func turnData(cli, turn int) string {
	var b strings.Builder
	for j := 0; j < 24; j++ {
		k := cli*1_000_000 + turn*100 + j
		fmt.Fprintf(&b, "reg %d: owner=u%d@empresa-ficticia.com.br host=10.42.%d.%d srv=db%d.empresa-ficticia.intra tel (11) 9%04d-%04d senha: Kq%08d!m RG: %02d.%03d.%03d-%d\n",
			k, k, k%4, k%250, k%9, k%10000, (k*7)%10000, k, 10+k%80, k%1000, (k/3)%1000, k%10)
	}
	return b.String()
}

var realMarks = []string{"@empresa-ficticia", "10.42.", ".empresa-ficticia.intra", "(11) 9", "senha: Kq"}

// "model" that returns, in chopped streaming, the last text it received (already masked)
func modelEcho(leaked *atomic.Int64, broke *atomic.Int64, previousOnes *sync.Map) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		for _, m := range realMarks {
			if bytes.Contains(body, []byte(m)) {
				leaked.Add(1)
			}
		}
		var req struct {
			Metadata struct {
				UserID string `json:"user_id"`
			} `json:"metadata"`
			Messages []json.RawMessage `json:"messages"`
		}
		json.Unmarshal(body, &req)
		// cache stability: the old messages have to arrive byte for byte equal
		if prev, ok := previousOnes.Load(req.Metadata.UserID); ok {
			for i, a := range prev.([]json.RawMessage) {
				if i >= len(req.Messages) || !bytes.Equal(a, req.Messages[i]) {
					broke.Add(1)
					break
				}
			}
		}
		previousOnes.Store(req.Metadata.UserID, req.Messages)
		var lastPos struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		json.Unmarshal(req.Messages[len(req.Messages)-1], &lastPos)
		w.Header().Set("content-type", "text/event-stream")
		sse(w, lastPos.Content[0].Text, "", "", 7)
	}
}

func readText(body io.Reader) string {
	var text strings.Builder
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		l := sc.Text()
		if !strings.HasPrefix(l, "data: ") {
			continue
		}
		var ev struct {
			Delta struct {
				Type, Text string
			}
		}
		json.Unmarshal([]byte(l[6:]), &ev)
		if ev.Delta.Type == "text_delta" {
			text.WriteString(ev.Delta.Text)
		}
	}
	return text.String()
}

// 1) Load: 16 conversations in parallel, growing up to ~1 MB each. Checks, on every request:
// nothing real reached the "API", the response came back exactly real, and the old messages arrived
// identical to those of the previous request (the API cache depends on it).
func TestStressProxyLoad(t *testing.T) {
	stressOnly(t)
	var leaked, broke atomic.Int64
	var previousOnes sync.Map
	px := buildStress(t, modelEcho(&leaked, &broke, &previousOnes))
	const clients, turns = 16, 120
	g0, h0 := runtime.NumGoroutine(), heapMB()
	var mu sync.Mutex
	var timings []time.Duration
	var errs atomic.Int64
	var totalBytes atomic.Int64
	t0 := time.Now()
	var wg sync.WaitGroup
	for c := 0; c < clients; c++ {
		wg.Add(1)
		go func(c int) {
			defer wg.Done()
			var msgs []any
			for tn := 0; tn < turns; tn++ {
				real := turnData(c, tn)
				msgs = append(msgs, map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": real}}})
				body, _ := json.Marshal(map[string]any{"model": "x", "stream": true, "metadata": map[string]any{"user_id": fmt.Sprint("cli", c)},
					"system": "sistema fixo", "messages": msgs})
				totalBytes.Add(int64(len(body)))
				ti := time.Now()
				resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(body))
				if err != nil {
					errs.Add(1)
					continue
				}
				got := readText(resp.Body)
				resp.Body.Close()
				d := time.Since(ti)
				if resp.StatusCode != 200 || got != real {
					errs.Add(1)
				}
				mu.Lock()
				timings = append(timings, d)
				mu.Unlock()
				msgs = append(msgs, map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": got}}})
			}
		}(c)
	}
	wg.Wait()
	sort.Slice(timings, func(i, j int) bool { return timings[i] < timings[j] })
	q := func(p float64) time.Duration { return timings[int(float64(len(timings)-1)*p)].Round(time.Millisecond) }
	time.Sleep(300 * time.Millisecond)
	t.Logf("%d requisições, %.0f MB enviados, em %s | por requisição (ida, eco e volta): mediana %s, p95 %s, máx %s",
		len(timings), float64(totalBytes.Load())/(1<<20), time.Since(t0).Round(time.Second), q(.5), q(.95), q(1))
	t.Logf("erros %d | vazamentos %d | quebras de cache %d | goroutines %d -> %d | heap %.0f -> %.0f MB",
		errs.Load(), leaked.Load(), broke.Load(), g0, runtime.NumGoroutine(), h0, heapMB())
	if errs.Load() > 0 || leaked.Load() > 0 || broke.Load() > 0 {
		t.Fatal("falhou sob carga")
	}
}

// 2) A client that gives up in the middle of the response, 400 times: no goroutine or connection may be left.
func TestStressProxyAborts(t *testing.T) {
	stressOnly(t)
	px := buildStress(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("content-type", "text/event-stream")
		fl := w.(http.Flusher)
		for i := 0; i < 400; i++ {
			if _, err := fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"palavra %d \"}}\n\n", i); err != nil {
				return
			}
			fl.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(2 * time.Millisecond):
			}
		}
	})
	body, _ := json.Marshal(map[string]any{"model": "x", "stream": true, "messages": []any{
		map[string]any{"role": "user", "content": turnData(1, 1)}}})
	g0 := runtime.NumGoroutine()
	var wg sync.WaitGroup
	for i := 0; i < 400; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(5+i%40)*time.Millisecond)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "POST", px.URL+"/v1/messages", bytes.NewReader(body))
			req.Header.Set("content-type", "application/json")
			if resp, err := http.DefaultClient.Do(req); err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}(i)
		if i%50 == 49 {
			wg.Wait()
		}
	}
	wg.Wait()
	http.DefaultClient.CloseIdleConnections()
	var g int
	for i := 0; i < 50; i++ {
		if g = runtime.NumGoroutine(); g <= g0+8 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Logf("400 respostas abandonadas no meio | goroutines %d -> %d", g0, g)
	if g > g0+8 {
		buf := make([]byte, 1<<16)
		t.Fatalf("goroutines sobrando: %d -> %d\n%s", g0, g, buf[:runtime.Stack(buf, true)])
	}
}

// 3) API with problems: error, a cut in the middle of streaming, no response. The proxy
// passes the error on or returns 502; it never hangs nor brings the process down.
func TestStressProxyBadUpstream(t *testing.T) {
	stressOnly(t)
	var mode atomic.Int64
	px := buildStress(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		switch mode.Load() {
		case 0:
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(529)
			fmt.Fprint(w, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
		case 1:
			w.WriteHeader(500)
			fmt.Fprint(w, "erro interno em texto puro")
		case 2: // cuts the connection in the middle of streaming
			w.Header().Set("content-type", "text/event-stream")
			fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"meio p.\"}}\n\n")
			w.(http.Flusher).Flush()
			if hj, ok := w.(http.Hijacker); ok {
				c, _, _ := hj.Hijack()
				c.Close()
			}
		case 3: // garbage in place of SSE
			w.Header().Set("content-type", "text/event-stream")
			fmt.Fprint(w, "data: {isto não é json\n\ndata: \n\nsem prefixo\n\n")
		case 4: // never answers
			<-r.Context().Done()
		}
	})
	body, _ := json.Marshal(map[string]any{"model": "x", "stream": true, "messages": []any{
		map[string]any{"role": "user", "content": turnData(2, 2)}}})
	g0 := runtime.NumGoroutine()
	names := []string{"529 em JSON", "500 em texto", "corte no meio do streaming", "lixo no streaming", "sem resposta (cliente desiste em 300 ms)"}
	for mo, name := range names {
		mode.Store(int64(mo))
		var wg sync.WaitGroup
		status := map[int]int{}
		var mu sync.Mutex
		ti := time.Now()
		for i := 0; i < 40; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
				if mo != 4 {
					ctx, cancel = context.WithTimeout(context.Background(), 20*time.Second)
				}
				defer cancel()
				req, _ := http.NewRequestWithContext(ctx, "POST", px.URL+"/v1/messages", bytes.NewReader(body))
				req.Header.Set("content-type", "application/json")
				st := -1
				if resp, err := http.DefaultClient.Do(req); err == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					st = resp.StatusCode
				}
				mu.Lock()
				status[st]++
				mu.Unlock()
			}()
		}
		wg.Wait()
		t.Logf("%-42s 40 requisições em %-6s status (-1 = cliente desistiu): %v", name, time.Since(ti).Round(time.Millisecond), status)
	}
	// after all of that, the proxy still answers
	resp, err := http.Get(px.URL + "/__llm-dlp/saude")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("proxy não responde mais: %v", err)
	}
	resp.Body.Close()
	http.DefaultClient.CloseIdleConnections()
	var g int
	for i := 0; i < 50; i++ {
		if g = runtime.NumGoroutine(); g <= g0+8 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Logf("goroutines %d -> %d", g0, g)
	if g > g0+8 {
		t.Fatalf("goroutines sobrando: %d -> %d", g0, g)
	}
}

// 4) Huge request (40 MB, 5 thousand distinct e-mails): it has to pass, and the second time
// (conversation already memoized) has to be fast.
func TestStressProxyHugeRequest(t *testing.T) {
	stressOnly(t)
	var leaked, broke atomic.Int64
	var previousOnes sync.Map
	px := buildStress(t, modelEcho(&leaked, &broke, &previousOnes))
	var msgs []any
	for i := 0; i < 9000; i++ {
		msgs = append(msgs, map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": turnData(7, i%220)}}})
	}
	msgs = append(msgs, map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "fim " + turnData(7, 1)}}})
	body, _ := json.Marshal(map[string]any{"model": "x", "stream": true, "metadata": map[string]any{"user_id": "g"}, "messages": msgs})
	for _, turnNo := range []string{"primeira vez (a frio)", "segunda vez (memorizado)"} {
		ti := time.Now()
		resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		got := readText(resp.Body)
		resp.Body.Close()
		t.Logf("%-26s %d MB, %d mensagens: %s | status %d | resposta correta=%v | heap %.0f MB", turnNo, len(body)>>20, len(msgs),
			time.Since(ti).Round(time.Millisecond), resp.StatusCode, got == "fim "+turnData(7, 1), heapMB())
		if resp.StatusCode != 200 || got != "fim "+turnData(7, 1) {
			t.Fatal("requisição gigante falhou")
		}
	}
	if leaked.Load() > 0 || broke.Load() > 0 {
		t.Fatalf("vazamentos %d, quebras de cache %d", leaked.Load(), broke.Load())
	}
}

// 5) Restart in the middle of a large conversation: the first request arrives with ~1 MB of
// text the proxy never saw. Measures the time of that request (cold) and of the next one.
func TestStressProxyAfterRestart(t *testing.T) {
	stressOnly(t)
	for _, tc := range []struct {
		name      string
		generates func(i int) string
	}{
		{"dados densos (24 registros sensíveis por mensagem)", func(i int) string { return turnData(9, i) }},
		{"código e log comuns", func(i int) string {
			var b strings.Builder
			for j := 0; j < 40; j++ {
				fmt.Fprintf(&b, "    if err := enc.Encode(v); err != nil { return nil, err } // linha %d-%d status=200 latency=%dms\n", i, j, j%97)
			}
			return b.String()
		}},
	} {
		var leaked, broke atomic.Int64
		var previousOnes sync.Map
		px := buildStress(t, modelEcho(&leaked, &broke, &previousOnes))
		var msgs []any
		for i := 0; len(msgs) < 260; i++ {
			msgs = append(msgs, map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": tc.generates(i)}}})
		}
		body, _ := json.Marshal(map[string]any{"model": "x", "stream": true, "metadata": map[string]any{"user_id": tc.name}, "messages": msgs})
		var ds []time.Duration
		for turnNo := 0; turnNo < 3; turnNo++ {
			ti := time.Now()
			resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			readText(resp.Body)
			resp.Body.Close()
			ds = append(ds, time.Since(ti).Round(time.Millisecond))
		}
		t.Logf("%-52s %4d KB em %d mensagens | a frio %s | depois %s e %s | vazamentos %d", tc.name, len(body)>>10, len(msgs), ds[0], ds[1], ds[2], leaked.Load())
		if leaked.Load() > 0 || broke.Load() > 0 {
			t.Fatal("vazou ou mudou entre requisições")
		}
	}
}
