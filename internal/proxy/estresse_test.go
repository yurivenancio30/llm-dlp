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

// Testes de carga do proxy inteiro (só com LLM_DLP_ESTRESSE=1).

// Testes de carga do proxy inteiro: pesados, rodam só com LLM_DLP_ESTRESSE=1.
//
//	LLM_DLP_ESTRESSE=1 go test ./internal/proxy -run Estresse -v -timeout 60m
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

func montarEstresse(t *testing.T, upstream http.HandlerFunc) (px *httptest.Server) {
	t.Helper()
	cfg := config.Padrao()
	cfg.DominiosInternos = []string{"empresa-ficticia"}
	chave := []byte("0123456789abcdef0123456789abcdef")
	dir := t.TempDir()
	ps, _ := mask.CarregarPessoas(dir + "/p.json")
	ps.Importar(mask.NovoPseudo(chave), "João Carlos Silva", email, "")
	vs, _ := mask.CarregarVistos(dir + "/v.json")
	m, err := mask.NovoMasker(cfg, chave, ps, vs)
	if err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(upstream)
	cfg.Upstream = up.URL
	px = httptest.NewServer(Novo(cfg, m, vs, log.New(io.Discard, "", 0)))
	t.Cleanup(func() { px.Close(); up.Close() })
	return px
}

// dados de um turno: ~4 KB com vários tipos de dado, distintos por cliente e turno
func dadosTurno(cli, turno int) string {
	var b strings.Builder
	for j := 0; j < 24; j++ {
		k := cli*1_000_000 + turno*100 + j
		fmt.Fprintf(&b, "reg %d: owner=u%d@empresa-ficticia.com.br host=10.42.%d.%d srv=db%d.empresa-ficticia.intra tel (11) 9%04d-%04d senha: Kq%08d!m RG: %02d.%03d.%03d-%d\n",
			k, k, k%4, k%250, k%9, k%10000, (k*7)%10000, k, 10+k%80, k%1000, (k/3)%1000, k%10)
	}
	return b.String()
}

var marcasReais = []string{"@empresa-ficticia", "10.42.", ".empresa-ficticia.intra", "(11) 9", "senha: Kq"}

// "modelo" que devolve, em streaming picotado, o último texto que recebeu (já mascarado)
func modeloEco(vazou *atomic.Int64, quebrou *atomic.Int64, anteriores *sync.Map) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		corpo, _ := io.ReadAll(r.Body)
		for _, m := range marcasReais {
			if bytes.Contains(corpo, []byte(m)) {
				vazou.Add(1)
			}
		}
		var req struct {
			Metadata struct {
				UserID string `json:"user_id"`
			} `json:"metadata"`
			Messages []json.RawMessage `json:"messages"`
		}
		json.Unmarshal(corpo, &req)
		// estabilidade do cache: as mensagens antigas têm que chegar byte a byte iguais
		if ant, ok := anteriores.Load(req.Metadata.UserID); ok {
			for i, a := range ant.([]json.RawMessage) {
				if i >= len(req.Messages) || !bytes.Equal(a, req.Messages[i]) {
					quebrou.Add(1)
					break
				}
			}
		}
		anteriores.Store(req.Metadata.UserID, req.Messages)
		var ult struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		json.Unmarshal(req.Messages[len(req.Messages)-1], &ult)
		w.Header().Set("content-type", "text/event-stream")
		sse(w, ult.Content[0].Text, "", "", 7)
	}
}

func lerTexto(body io.Reader) string {
	var texto strings.Builder
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
			texto.WriteString(ev.Delta.Text)
		}
	}
	return texto.String()
}

// 1) Carga: 16 conversas em paralelo, crescendo até ~1 MB cada. Confere, em toda requisição:
// nada real chegou à "API", a resposta voltou exatamente real, e as mensagens antigas
// chegaram idênticas às da requisição anterior (o cache da API depende disso).
func TestEstresseProxyCarga(t *testing.T) {
	soEstresse(t)
	var vazou, quebrou atomic.Int64
	var anteriores sync.Map
	px := montarEstresse(t, modeloEco(&vazou, &quebrou, &anteriores))
	const clientes, turnos = 16, 120
	g0, h0 := runtime.NumGoroutine(), heapMB()
	var mu sync.Mutex
	var tempos []time.Duration
	var erros atomic.Int64
	var bytesTotal atomic.Int64
	t0 := time.Now()
	var wg sync.WaitGroup
	for c := 0; c < clientes; c++ {
		wg.Add(1)
		go func(c int) {
			defer wg.Done()
			var msgs []any
			for tn := 0; tn < turnos; tn++ {
				real := dadosTurno(c, tn)
				msgs = append(msgs, map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": real}}})
				corpo, _ := json.Marshal(map[string]any{"model": "x", "stream": true, "metadata": map[string]any{"user_id": fmt.Sprint("cli", c)},
					"system": "sistema fixo", "messages": msgs})
				bytesTotal.Add(int64(len(corpo)))
				ti := time.Now()
				resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(corpo))
				if err != nil {
					erros.Add(1)
					continue
				}
				got := lerTexto(resp.Body)
				resp.Body.Close()
				d := time.Since(ti)
				if resp.StatusCode != 200 || got != real {
					erros.Add(1)
				}
				mu.Lock()
				tempos = append(tempos, d)
				mu.Unlock()
				msgs = append(msgs, map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": got}}})
			}
		}(c)
	}
	wg.Wait()
	sort.Slice(tempos, func(i, j int) bool { return tempos[i] < tempos[j] })
	q := func(p float64) time.Duration { return tempos[int(float64(len(tempos)-1)*p)].Round(time.Millisecond) }
	time.Sleep(300 * time.Millisecond)
	t.Logf("%d requisições, %.0f MB enviados, em %s | por requisição (ida, eco e volta): mediana %s, p95 %s, máx %s",
		len(tempos), float64(bytesTotal.Load())/(1<<20), time.Since(t0).Round(time.Second), q(.5), q(.95), q(1))
	t.Logf("erros %d | vazamentos %d | quebras de cache %d | goroutines %d -> %d | heap %.0f -> %.0f MB",
		erros.Load(), vazou.Load(), quebrou.Load(), g0, runtime.NumGoroutine(), h0, heapMB())
	if erros.Load() > 0 || vazou.Load() > 0 || quebrou.Load() > 0 {
		t.Fatal("falhou sob carga")
	}
}

// 2) Cliente que desiste no meio da resposta, 400 vezes: não pode sobrar goroutine nem conexão.
func TestEstresseProxyAbortos(t *testing.T) {
	soEstresse(t)
	px := montarEstresse(t, func(w http.ResponseWriter, r *http.Request) {
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
	corpo, _ := json.Marshal(map[string]any{"model": "x", "stream": true, "messages": []any{
		map[string]any{"role": "user", "content": dadosTurno(1, 1)}}})
	g0 := runtime.NumGoroutine()
	var wg sync.WaitGroup
	for i := 0; i < 400; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(5+i%40)*time.Millisecond)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "POST", px.URL+"/v1/messages", bytes.NewReader(corpo))
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

// 3) API com problema: erro, corte no meio do streaming, sem resposta. O proxy repassa o
// erro ou devolve 502; nunca trava nem derruba o processo.
func TestEstresseProxyUpstreamRuim(t *testing.T) {
	soEstresse(t)
	var modo atomic.Int64
	px := montarEstresse(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		switch modo.Load() {
		case 0:
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(529)
			fmt.Fprint(w, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
		case 1:
			w.WriteHeader(500)
			fmt.Fprint(w, "erro interno em texto puro")
		case 2: // corta a conexão no meio do streaming
			w.Header().Set("content-type", "text/event-stream")
			fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"meio p.\"}}\n\n")
			w.(http.Flusher).Flush()
			if hj, ok := w.(http.Hijacker); ok {
				c, _, _ := hj.Hijack()
				c.Close()
			}
		case 3: // lixo no lugar de SSE
			w.Header().Set("content-type", "text/event-stream")
			fmt.Fprint(w, "data: {isto não é json\n\ndata: \n\nsem prefixo\n\n")
		case 4: // nunca responde
			<-r.Context().Done()
		}
	})
	corpo, _ := json.Marshal(map[string]any{"model": "x", "stream": true, "messages": []any{
		map[string]any{"role": "user", "content": dadosTurno(2, 2)}}})
	g0 := runtime.NumGoroutine()
	nomes := []string{"529 em JSON", "500 em texto", "corte no meio do streaming", "lixo no streaming", "sem resposta (cliente desiste em 300 ms)"}
	for mo, nome := range nomes {
		modo.Store(int64(mo))
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
				req, _ := http.NewRequestWithContext(ctx, "POST", px.URL+"/v1/messages", bytes.NewReader(corpo))
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
		t.Logf("%-42s 40 requisições em %-6s status (-1 = cliente desistiu): %v", nome, time.Since(ti).Round(time.Millisecond), status)
	}
	// depois de tudo isso, o proxy continua respondendo
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

// 4) Requisição gigante (40 MB, 5 mil e-mails distintos): tem que passar, e a segunda vez
// (conversa já memorizada) tem que ser rápida.
func TestEstresseProxyRequisicaoGigante(t *testing.T) {
	soEstresse(t)
	var vazou, quebrou atomic.Int64
	var anteriores sync.Map
	px := montarEstresse(t, modeloEco(&vazou, &quebrou, &anteriores))
	var msgs []any
	for i := 0; i < 9000; i++ {
		msgs = append(msgs, map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": dadosTurno(7, i%220)}}})
	}
	msgs = append(msgs, map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "fim " + dadosTurno(7, 1)}}})
	corpo, _ := json.Marshal(map[string]any{"model": "x", "stream": true, "metadata": map[string]any{"user_id": "g"}, "messages": msgs})
	for _, vez := range []string{"primeira vez (a frio)", "segunda vez (memorizado)"} {
		ti := time.Now()
		resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(corpo))
		if err != nil {
			t.Fatal(err)
		}
		got := lerTexto(resp.Body)
		resp.Body.Close()
		t.Logf("%-26s %d MB, %d mensagens: %s | status %d | resposta correta=%v | heap %.0f MB", vez, len(corpo)>>20, len(msgs),
			time.Since(ti).Round(time.Millisecond), resp.StatusCode, got == "fim "+dadosTurno(7, 1), heapMB())
		if resp.StatusCode != 200 || got != "fim "+dadosTurno(7, 1) {
			t.Fatal("requisição gigante falhou")
		}
	}
	if vazou.Load() > 0 || quebrou.Load() > 0 {
		t.Fatalf("vazamentos %d, quebras de cache %d", vazou.Load(), quebrou.Load())
	}
}

// 5) Reinício no meio de uma conversa grande: a primeira requisição chega com ~1 MB de texto
// que o proxy nunca viu. Mede o tempo dessa requisição (a frio) e da seguinte.
func TestEstresseProxyAposReinicio(t *testing.T) {
	soEstresse(t)
	for _, caso := range []struct {
		nome string
		gera func(i int) string
	}{
		{"dados densos (24 registros sensíveis por mensagem)", func(i int) string { return dadosTurno(9, i) }},
		{"código e log comuns", func(i int) string {
			var b strings.Builder
			for j := 0; j < 40; j++ {
				fmt.Fprintf(&b, "    if err := enc.Encode(v); err != nil { return nil, err } // linha %d-%d status=200 latency=%dms\n", i, j, j%97)
			}
			return b.String()
		}},
	} {
		var vazou, quebrou atomic.Int64
		var anteriores sync.Map
		px := montarEstresse(t, modeloEco(&vazou, &quebrou, &anteriores))
		var msgs []any
		for i := 0; len(msgs) < 260; i++ {
			msgs = append(msgs, map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": caso.gera(i)}}})
		}
		corpo, _ := json.Marshal(map[string]any{"model": "x", "stream": true, "metadata": map[string]any{"user_id": caso.nome}, "messages": msgs})
		var ds []time.Duration
		for vez := 0; vez < 3; vez++ {
			ti := time.Now()
			resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(corpo))
			if err != nil {
				t.Fatal(err)
			}
			lerTexto(resp.Body)
			resp.Body.Close()
			ds = append(ds, time.Since(ti).Round(time.Millisecond))
		}
		t.Logf("%-52s %4d KB em %d mensagens | a frio %s | depois %s e %s | vazamentos %d", caso.nome, len(corpo)>>10, len(msgs), ds[0], ds[1], ds[2], vazou.Load())
		if vazou.Load() > 0 || quebrou.Load() > 0 {
			t.Fatal("vazou ou mudou entre requisições")
		}
	}
}
