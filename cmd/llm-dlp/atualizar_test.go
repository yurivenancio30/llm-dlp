package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// atualizar e recarregar: a troca não deixa a porta vazia nem corta resposta, e volta sozinha
// quando a versão nova não sobe. Os testes compilam o llm-dlp duas vezes (commits diferentes) e
// rodam supervisor e proxy de verdade, com a pasta pessoal num diretório temporário e uma API
// falsa no lugar da Anthropic.

var binarios struct {
	once sync.Once
	dir  string
	err  error
}

// TestMain põe a pasta pessoal num diretório temporário: install, uninstall e update mexem no
// ~/.claude e no ~/.local/bin, e um teste não pode tocar nos de verdade. Os caches do Go ficam
// nos de sempre (o go build dos testes de troca não recompila nem baixa tudo de novo).
func TestMain(m *testing.M) {
	if os.Getenv("GOPATH") == "" {
		if h, err := os.UserHomeDir(); err == nil {
			os.Setenv("GOPATH", filepath.Join(h, "go"))
		}
	}
	if cache, err := os.UserCacheDir(); err == nil && os.Getenv("GOCACHE") == "" {
		os.Setenv("GOCACHE", filepath.Join(cache, "go-build"))
	}
	if cfg, err := os.UserConfigDir(); err == nil && os.Getenv("GOENV") == "" {
		os.Setenv("GOENV", filepath.Join(cfg, "go", "env"))
	}
	home, err := os.MkdirTemp("", "llm-dlp-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Unsetenv("XDG_CONFIG_HOME")
	os.Unsetenv("XDG_CACHE_HOME")
	c := m.Run()
	os.RemoveAll(home)
	if binarios.dir != "" {
		os.RemoveAll(binarios.dir)
	}
	os.Exit(c)
}

// compilados: o llm-dlp com o commit "aaaaaaa" e com o "bbbbbbb".
func compilados(t *testing.T) (a, b string) {
	t.Helper()
	if testing.Short() {
		t.Skip("compila o llm-dlp duas vezes")
	}
	if runtime.GOOS != "linux" {
		t.Skip("a troca sem pausa é do Linux")
	}
	binarios.once.Do(func() {
		goBin, err := exec.LookPath("go")
		if err != nil {
			goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
		}
		if binarios.dir, binarios.err = os.MkdirTemp("", "llm-dlp-bin-"); binarios.err != nil {
			return
		}
		for _, c := range []string{"aaaaaaa", "bbbbbbb"} {
			out, err := exec.Command(goBin, "build", "-o", filepath.Join(binarios.dir, c, "llm-dlp"),
				"-ldflags", "-X github.com/yurivenancio30/llm-dlp/internal/versao.Commit="+c, ".").CombinedOutput()
			if err != nil {
				binarios.err = fmt.Errorf("go build: %v\n%s", err, out)
				return
			}
		}
	})
	if binarios.err != nil {
		t.Fatal(binarios.err)
	}
	return filepath.Join(binarios.dir, "aaaaaaa", "llm-dlp"), filepath.Join(binarios.dir, "bbbbbbb", "llm-dlp")
}

type ambiente struct {
	t             *testing.T
	home, destino string
	porta         int
	env           []string
}

// apiFalsa responde em streaming, devagar: 300 ms; 8 s se a mensagem pede uma resposta longa
// (mais que prazoGraca: a resposta tem de sobreviver ao fim da graça do proxy antigo).
func apiFalsa(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	dur := 300 * time.Millisecond
	if bytes.Contains(b, []byte("LONGA")) {
		dur = 8 * time.Second
	}
	w.Header().Set("content-type", "text/event-stream")
	ev := func(tipo, dado string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", tipo, dado)
		w.(http.Flusher).Flush()
	}
	ev("message_start", `{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","content":[]}}`)
	ev("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	for i := 0; i < 6; i++ {
		time.Sleep(dur / 6)
		ev("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok "}}`)
	}
	ev("content_block_stop", `{"type":"content_block_stop","index":0}`)
	ev("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`)
	ev("message_stop", `{"type":"message_stop"}`)
}

func novoAmbiente(t *testing.T) *ambiente {
	e := &ambiente{t: t, home: t.TempDir()}
	e.destino = filepath.Join(e.home, ".local", "bin", "llm-dlp")
	api := httptest.NewServer(http.HandlerFunc(apiFalsa))
	t.Cleanup(api.Close)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	e.porta = ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cfg := config.Padrao()
	cfg.Porta, cfg.Upstream = e.porta, api.URL
	dir := filepath.Join(e.home, ".config", "llm-dlp")
	os.MkdirAll(dir, 0o700)
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "HOME=") && !strings.HasPrefix(v, "LLM_DLP_HOME=") {
			e.env = append(e.env, v)
		}
	}
	e.env = append(e.env, "HOME="+e.home)
	t.Cleanup(e.encerrar)
	return e
}

func (e *ambiente) instalar(bin string) {
	if err := copiarArquivo(bin, e.destino, 0o755); err != nil {
		e.t.Fatal(err)
	}
}

// log: o fim do log do llm-dlp do teste (só a API falsa passou por ele).
func (e *ambiente) log() string {
	b, _ := os.ReadFile(filepath.Join(e.home, ".config", "llm-dlp", "llm-dlp.log"))
	var ls []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if !strings.Contains(l, " POST /v1/messages -> 200 ") {
			ls = append(ls, l)
		}
	}
	if len(ls) > 30 {
		ls = ls[len(ls)-30:]
	}
	return strings.Join(ls, "\n")
}

func (e *ambiente) rodar(bin string, args ...string) (string, error) {
	c := exec.Command(bin, args...)
	c.Env = e.env
	out, err := c.CombinedOutput()
	return string(out), err
}

func (e *ambiente) supervisor() int {
	b, _ := os.ReadFile(filepath.Join(e.home, ".config", "llm-dlp", "supervisor.pid"))
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid
}

// encerrar para o supervisor (e o proxy) que o teste deixou no ar.
func (e *ambiente) encerrar() {
	e.rodar(e.destino, "stop")
	if pid := e.supervisor(); pid > 0 {
		syscall.Kill(pid, syscall.SIGKILL)
	}
	if s, ok := e.saude(); ok {
		syscall.Kill(s.PID, syscall.SIGKILL)
	}
}

type saudeTeste struct {
	Commit string `json:"commit"`
	PID    int    `json:"pid"`
}

func (e *ambiente) saude() (saudeTeste, bool) {
	var s saudeTeste
	// conexão nova: uma reaproveitada podia ser com o proxy antigo, que ainda atende as
	// conexões já abertas durante a graça
	c := http.Client{Timeout: time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	r, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/__llm-dlp/saude", e.porta))
	if err != nil {
		return s, false
	}
	defer r.Body.Close()
	return s, json.NewDecoder(r.Body).Decode(&s) == nil && s.PID > 0
}

func (e *ambiente) esperarCommit(commit string) saudeTeste {
	e.t.Helper()
	for limite := time.Now().Add(20 * time.Second); time.Now().Before(limite); time.Sleep(50 * time.Millisecond) {
		if s, ok := e.saude(); ok && s.Commit == commit {
			return s
		}
	}
	e.t.Fatalf("o proxy com o commit %s não respondeu em 20s", commit)
	return saudeTeste{}
}

// pedir manda uma mensagem pelo proxy e confere que a resposta veio inteira.
func (e *ambiente) pedir(cli *http.Client, texto string) error {
	corpo := `{"model":"x","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"` + texto + `"}]}`
	r, err := cli.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/messages", e.porta), "application/json", strings.NewReader(corpo))
	if err != nil {
		return err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("resposta cortada: %w", err)
	}
	if r.StatusCode != 200 || !bytes.Contains(b, []byte("message_stop")) {
		return fmt.Errorf("status %d, resposta incompleta: %.120s", r.StatusCode, b)
	}
	return nil
}

// carga: n clientes mandando mensagens sem parar, reaproveitando conexões como o Claude Code.
type carga struct {
	pare      chan struct{}
	wg        sync.WaitGroup
	ok, erros atomic.Int64
	mu        sync.Mutex
	exemplos  []string
}

func (e *ambiente) carga(n int) *carga {
	c := &carga{pare: make(chan struct{})}
	cli := &http.Client{Timeout: 30 * time.Second}
	for i := 0; i < n; i++ {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			for {
				select {
				case <-c.pare:
					return
				default:
				}
				if err := e.pedir(cli, "oi"); err != nil {
					c.erros.Add(1)
					c.mu.Lock()
					c.exemplos = append(c.exemplos, err.Error())
					c.mu.Unlock()
				} else {
					c.ok.Add(1)
				}
			}
		}()
	}
	return c
}

func (c *carga) parar(t *testing.T, oque string) {
	t.Helper()
	close(c.pare)
	c.wg.Wait()
	if n := c.erros.Load(); n > 0 {
		t.Errorf("%s: %d de %d mensagens falharam (ex.: %v)", oque, n, n+c.ok.Load(), c.exemplos[0])
	}
	if c.ok.Load() < 8 {
		t.Errorf("%s: só %d mensagens passaram; a carga não cobriu a troca", oque, c.ok.Load())
	}
}

// pacote: um .tar.gz com o arquivo bin como "llm-dlp", como o da release.
func pacote(t *testing.T, bin string) string {
	t.Helper()
	b, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "llm-dlp", Mode: 0o755, Size: int64(len(b)), Typeflag: tar.TypeReg})
	tw.Write(b)
	tw.Close()
	gz.Close()
	p := filepath.Join(t.TempDir(), "llm-dlp_linux.tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func saiu(pid int, prazo time.Duration) bool {
	for limite := time.Now().Add(prazo); time.Now().Before(limite); time.Sleep(50 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
	}
	return false
}

// A troca de versão com mensagens chegando o tempo todo e uma resposta longa no meio: nenhuma
// mensagem falha, a resposta longa termina inteira (pelo proxy antigo), e depois só o novo
// responde. O mesmo para o recarregar.
func TestAtualizarSemPausa(t *testing.T) {
	a, b := compilados(t)
	e := novoAmbiente(t)
	e.instalar(a)
	if out, err := e.rodar(e.destino, "ensure"); err != nil {
		t.Fatalf("garantir: %v\n%s", err, out)
	}
	antigo := e.esperarCommit("aaaaaaa")

	c := e.carga(4)
	longa := make(chan error, 1)
	go func() { longa <- e.pedir(&http.Client{Timeout: time.Minute}, "LONGA") }()
	time.Sleep(500 * time.Millisecond)
	// como o install.sh: o binário novo, de fora, atualiza o instalado
	out, err := e.rodar(b, "update", "--file", pacote(t, b))
	if err != nil {
		t.Fatalf("atualizar: %v\n%s\nlog:\n%s", err, out, e.log())
	}
	novo, _ := e.saude()
	if novo.Commit != "bbbbbbb" || novo.PID == antigo.PID {
		t.Fatalf("depois do atualizar responde %+v; esperado o commit bbbbbbb num processo novo\n%s", novo, out)
	}
	time.Sleep(300 * time.Millisecond)
	c.parar(t, "durante a troca")
	if err := <-longa; err != nil {
		t.Errorf("a resposta longa, começada antes da troca, foi cortada: %v", err)
	}
	if _, commit, _ := versaoDe(e.destino); commit != "bbbbbbb" {
		t.Errorf("o instalado ficou com o commit %q", commit)
	}
	if copias, _ := filepath.Glob(e.destino + ".bak-*"); len(copias) != 1 {
		t.Errorf("cópias do binário anterior: %v; esperada uma", copias)
	}
	if !saiu(antigo.PID, prazoGraca+5*time.Second) {
		t.Errorf("o proxy antigo (pid %d) não saiu", antigo.PID)
	}
	// o supervisor também passou para o binário novo (reexecutado, sem soltar a porta)
	if exe, _ := os.Readlink(fmt.Sprintf("/proc/%d/exe", e.supervisor())); exe != e.destino {
		t.Errorf("o supervisor roda %q; esperado o binário instalado %s\nlog:\n%s", exe, e.destino, e.log())
	}

	// recarregar: o mesmo binário num processo novo, também sem nenhuma falha
	c = e.carga(4)
	time.Sleep(200 * time.Millisecond)
	if out, err := e.rodar(e.destino, "reload"); err != nil {
		t.Fatalf("recarregar: %v\n%s", err, out)
	}
	time.Sleep(300 * time.Millisecond)
	c.parar(t, "durante o recarregar")
	if r, _ := e.saude(); r.Commit != "bbbbbbb" || r.PID == novo.PID {
		t.Errorf("depois do recarregar responde %+v; esperado o bbbbbbb num processo novo", r)
	}

	// a trava e o supervisor.pid passaram para o supervisor novo: o parar funciona
	if out, err := e.rodar(e.destino, "stop"); err != nil {
		t.Fatalf("parar depois da troca: %v\n%s", err, out)
	}
	if s, ok := e.saude(); ok {
		t.Errorf("depois do parar ainda responde: %+v", s)
	}
}

// O proxy cai no meio da carga: a porta é do supervisor e continua aberta, então nenhuma
// mensagem recebe "connection refused"; elas esperam o proxy seguinte. (As que estavam no
// meio, no proxy que caiu, são cortadas: isso não há como evitar.)
func TestProxyCaiSemRecusar(t *testing.T) {
	a, _ := compilados(t)
	e := novoAmbiente(t)
	e.instalar(a)
	if out, err := e.rodar(e.destino, "ensure"); err != nil {
		t.Fatalf("garantir: %v\n%s", err, out)
	}
	antigo := e.esperarCommit("aaaaaaa")
	c := e.carga(4)
	time.Sleep(500 * time.Millisecond)
	syscall.Kill(antigo.PID, syscall.SIGKILL)
	time.Sleep(3 * time.Second) // o supervisor espera 1 s e sobe outro
	antes := c.ok.Load()
	time.Sleep(time.Second)
	close(c.pare)
	c.wg.Wait()
	recusadas := 0
	for _, x := range c.exemplos {
		if strings.Contains(x, "connection refused") {
			recusadas++
		}
	}
	if recusadas > 0 {
		t.Errorf("%d mensagens receberam connection refused com o proxy fora", recusadas)
	}
	if c.ok.Load() == antes {
		t.Errorf("nenhuma mensagem passou depois que o proxy voltou")
	}
	if s, _ := e.saude(); s.PID == antigo.PID || s.Commit != "aaaaaaa" {
		t.Errorf("depois da queda responde %+v", s)
	}
}

// A versão nova não sobe: o atualizar desiste, põe o binário anterior de volta, e o proxy
// anterior nunca saiu do ar.
func TestAtualizarVoltaSeNaoSobe(t *testing.T) {
	a, _ := compilados(t)
	e := novoAmbiente(t)
	e.instalar(a)
	if out, err := e.rodar(e.destino, "ensure"); err != nil {
		t.Fatalf("garantir: %v\n%s", err, out)
	}
	antigo := e.esperarCommit("aaaaaaa")
	quebrado := filepath.Join(t.TempDir(), "llm-dlp")
	os.WriteFile(quebrado, []byte("#!/bin/sh\n[ \"$1\" = versao ] && echo 'llm-dlp 9.9.9 (quebrado)' && exit 0\nexit 1\n"), 0o755)

	c := e.carga(2)
	// (com os nomes de antes da 0.9.0, que continuam aceitos)
	out, err := e.rodar(e.destino, "atualizar", "--arquivo", pacote(t, quebrado))
	saidaAtualizar := out
	time.Sleep(time.Second)
	c.parar(t, "durante e depois da tentativa")
	if err == nil || !strings.Contains(out, "voltei para a") {
		t.Fatalf("o atualizar devia falhar e voltar; err=%v\n%s", err, out)
	}
	if s, _ := e.saude(); s.PID != antigo.PID || s.Commit != "aaaaaaa" {
		t.Errorf("responde %+v; esperado o proxy anterior (%+v), que nunca devia ter saído", s, antigo)
	}
	if _, commit, _ := versaoDe(e.destino); commit != "aaaaaaa" {
		t.Errorf("o instalado ficou com o commit %q; esperado o anterior de volta", commit)
	}
	if out, err := e.rodar(e.destino, "stop"); err != nil {
		t.Errorf("parar: %v\n%s\nsaída do atualizar:\n%s\nlog:\n%s", err, out, saidaAtualizar, e.log())
	}
}

// O download confere o pacote com o checksums.txt da release: um pacote diferente não é usado.
func TestBaixarReleaseConfereChecksum(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("só há binário pronto para Linux")
	}
	nome := fmt.Sprintf("llm-dlp_1.2.3_linux_%s.tar.gz", runtime.GOARCH)
	conteudo := []byte("pacote")
	soma := sha256.Sum256(conteudo)
	sums := hex.EncodeToString(soma[:]) + "  " + nome + "\n"
	servido := conteudo
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			http.Redirect(w, r, "/tag/v1.2.3", http.StatusFound)
		case "/download/v1.2.3/" + nome:
			w.Write(servido)
		case "/download/v1.2.3/checksums.txt":
			io.WriteString(w, sums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	v, err := versaoPublicada(srv.URL, "")
	if err != nil || v != "1.2.3" {
		t.Fatalf("versão mais nova: %q, %v", v, err)
	}
	if _, err := baixarRelease(srv.URL, v, t.TempDir()); err != nil {
		t.Fatalf("pacote certo recusado: %v", err)
	}
	servido = []byte("pacote trocado")
	if _, err := baixarRelease(srv.URL, v, t.TempDir()); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("pacote com checksum errado aceito: %v", err)
	}
	if _, err := versaoPublicada(srv.URL, "1.2"); err == nil {
		t.Errorf("versão pedida inválida aceita")
	}
}
