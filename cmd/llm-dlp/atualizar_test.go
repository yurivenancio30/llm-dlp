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

var binaries struct {
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
	if binaries.dir != "" {
		os.RemoveAll(binaries.dir)
	}
	os.Exit(c)
}

// compilados: o llm-dlp com o commit "aaaaaaa" e com o "bbbbbbb".
func compiled(t *testing.T) (a, b string) {
	t.Helper()
	if testing.Short() {
		t.Skip("compila o llm-dlp duas vezes")
	}
	if runtime.GOOS != "linux" {
		t.Skip("a troca sem pausa é do Linux")
	}
	binaries.once.Do(func() {
		goBin, err := exec.LookPath("go")
		if err != nil {
			goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
		}
		if binaries.dir, binaries.err = os.MkdirTemp("", "llm-dlp-bin-"); binaries.err != nil {
			return
		}
		for _, c := range []string{"aaaaaaa", "bbbbbbb"} {
			out, err := exec.Command(goBin, "build", "-o", filepath.Join(binaries.dir, c, "llm-dlp"),
				"-ldflags", "-X github.com/yurivenancio30/llm-dlp/internal/versao.Commit="+c, ".").CombinedOutput()
			if err != nil {
				binaries.err = fmt.Errorf("go build: %v\n%s", err, out)
				return
			}
		}
	})
	if binaries.err != nil {
		t.Fatal(binaries.err)
	}
	return filepath.Join(binaries.dir, "aaaaaaa", "llm-dlp"), filepath.Join(binaries.dir, "bbbbbbb", "llm-dlp")
}

type environment struct {
	t                 *testing.T
	home, destination string
	port              int
	env               []string
}

// fakeAPI responde em streaming, devagar: 300 ms; 8 s se a mensagem pede uma resposta longa
// (mais que graceDeadline: a resposta tem de sobreviver ao fim da graça do proxy antigo).
func fakeAPI(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	dur := 300 * time.Millisecond
	if bytes.Contains(b, []byte("LONGA")) {
		dur = 8 * time.Second
	}
	w.Header().Set("content-type", "text/event-stream")
	ev := func(typ, datum string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, datum)
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

func newEnvironment(t *testing.T) *environment {
	e := &environment{t: t, home: t.TempDir()}
	e.destination = filepath.Join(e.home, ".local", "bin", "llm-dlp")
	api := httptest.NewServer(http.HandlerFunc(fakeAPI))
	t.Cleanup(api.Close)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	e.port = ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cfg := config.Default()
	cfg.Port, cfg.Upstream = e.port, api.URL
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
	t.Cleanup(e.shutdown)
	return e
}

func (e *environment) install(bin string) {
	if err := copyFile(bin, e.destination, 0o755); err != nil {
		e.t.Fatal(err)
	}
}

// log: o fim do log do llm-dlp do teste (só a API falsa passou por ele).
func (e *environment) log() string {
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

func (e *environment) runIt(bin string, args ...string) (string, error) {
	c := exec.Command(bin, args...)
	c.Env = e.env
	out, err := c.CombinedOutput()
	return string(out), err
}

func (e *environment) supervisor() int {
	b, _ := os.ReadFile(filepath.Join(e.home, ".config", "llm-dlp", "supervisor.pid"))
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid
}

// encerrar para o supervisor (e o proxy) que o teste deixou no ar.
func (e *environment) shutdown() {
	e.runIt(e.destination, "stop")
	if pid := e.supervisor(); pid > 0 {
		syscall.Kill(pid, syscall.SIGKILL)
	}
	if s, ok := e.health(); ok {
		syscall.Kill(s.PID, syscall.SIGKILL)
	}
}

type testHealth struct {
	Commit string `json:"commit"`
	PID    int    `json:"pid"`
}

func (e *environment) health() (testHealth, bool) {
	var s testHealth
	// conexão nova: uma reaproveitada podia ser com o proxy antigo, que ainda atende as
	// conexões já abertas durante a graça
	c := http.Client{Timeout: time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	r, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/__llm-dlp/saude", e.port))
	if err != nil {
		return s, false
	}
	defer r.Body.Close()
	return s, json.NewDecoder(r.Body).Decode(&s) == nil && s.PID > 0
}

func (e *environment) waitCommit(commit string) testHealth {
	e.t.Helper()
	for limitN := time.Now().Add(20 * time.Second); time.Now().Before(limitN); time.Sleep(50 * time.Millisecond) {
		if s, ok := e.health(); ok && s.Commit == commit {
			return s
		}
	}
	e.t.Fatalf("o proxy com o commit %s não respondeu em 20s", commit)
	return testHealth{}
}

// pedir manda uma mensagem pelo proxy e confere que a resposta veio inteira.
func (e *environment) ask(cli *http.Client, text string) error {
	body := `{"model":"x","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"` + text + `"}]}`
	r, err := cli.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/messages", e.port), "application/json", strings.NewReader(body))
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
type load struct {
	stopped  chan struct{}
	wg       sync.WaitGroup
	ok, errs atomic.Int64
	mu       sync.Mutex
	examples []string
}

func (e *environment) load(n int) *load {
	c := &load{stopped: make(chan struct{})}
	cli := &http.Client{Timeout: 30 * time.Second}
	for i := 0; i < n; i++ {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			for {
				select {
				case <-c.stopped:
					return
				default:
				}
				if err := e.ask(cli, "oi"); err != nil {
					c.errs.Add(1)
					c.mu.Lock()
					c.examples = append(c.examples, err.Error())
					c.mu.Unlock()
				} else {
					c.ok.Add(1)
				}
			}
		}()
	}
	return c
}

func (c *load) stop(t *testing.T, what string) {
	t.Helper()
	close(c.stopped)
	c.wg.Wait()
	if n := c.errs.Load(); n > 0 {
		t.Errorf("%s: %d de %d mensagens falharam (ex.: %v)", what, n, n+c.ok.Load(), c.examples[0])
	}
	if c.ok.Load() < 8 {
		t.Errorf("%s: só %d mensagens passaram; a carga não cobriu a troca", what, c.ok.Load())
	}
}

// pacote: um .tar.gz com o arquivo bin como "llm-dlp", como o da release.
func pkgName(t *testing.T, bin string) string {
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

func wentOut(pid int, deadline time.Duration) bool {
	for limitN := time.Now().Add(deadline); time.Now().Before(limitN); time.Sleep(50 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
	}
	return false
}

// A troca de versão com mensagens chegando o tempo todo e uma resposta longa no meio: nenhuma
// mensagem falha, a resposta longa termina inteira (pelo proxy antigo), e depois só o novo
// responde. O mesmo para o recarregar.
func TestUpdateWithoutPause(t *testing.T) {
	a, b := compiled(t)
	e := newEnvironment(t)
	e.install(a)
	if out, err := e.runIt(e.destination, "ensure"); err != nil {
		t.Fatalf("garantir: %v\n%s", err, out)
	}
	old := e.waitCommit("aaaaaaa")

	c := e.load(4)
	longOne := make(chan error, 1)
	go func() { longOne <- e.ask(&http.Client{Timeout: time.Minute}, "LONGA") }()
	time.Sleep(500 * time.Millisecond)
	// como o install.sh: o binário novo, de fora, atualiza o instalado
	out, err := e.runIt(b, "update", "--file", pkgName(t, b))
	if err != nil {
		t.Fatalf("atualizar: %v\n%s\nlog:\n%s", err, out, e.log())
	}
	newOne, _ := e.health()
	if newOne.Commit != "bbbbbbb" || newOne.PID == old.PID {
		t.Fatalf("depois do atualizar responde %+v; esperado o commit bbbbbbb num processo novo\n%s", newOne, out)
	}
	time.Sleep(300 * time.Millisecond)
	c.stop(t, "durante a troca")
	if err := <-longOne; err != nil {
		t.Errorf("a resposta longa, começada antes da troca, foi cortada: %v", err)
	}
	if _, commit, _ := versionOf(e.destination); commit != "bbbbbbb" {
		t.Errorf("o instalado ficou com o commit %q", commit)
	}
	if copies, _ := filepath.Glob(e.destination + ".bak-*"); len(copies) != 1 {
		t.Errorf("cópias do binário anterior: %v; esperada uma", copies)
	}
	if !wentOut(old.PID, graceDeadline+5*time.Second) {
		t.Errorf("o proxy antigo (pid %d) não saiu", old.PID)
	}
	// o supervisor também passou para o binário novo (reexecutado, sem soltar a porta)
	if exe, _ := os.Readlink(fmt.Sprintf("/proc/%d/exe", e.supervisor())); exe != e.destination {
		t.Errorf("o supervisor roda %q; esperado o binário instalado %s\nlog:\n%s", exe, e.destination, e.log())
	}

	// recarregar: o mesmo binário num processo novo, também sem nenhuma falha
	c = e.load(4)
	time.Sleep(200 * time.Millisecond)
	if out, err := e.runIt(e.destination, "reload"); err != nil {
		t.Fatalf("recarregar: %v\n%s", err, out)
	}
	time.Sleep(300 * time.Millisecond)
	c.stop(t, "durante o recarregar")
	if r, _ := e.health(); r.Commit != "bbbbbbb" || r.PID == newOne.PID {
		t.Errorf("depois do recarregar responde %+v; esperado o bbbbbbb num processo novo", r)
	}

	// a trava e o supervisor.pid passaram para o supervisor novo: o parar funciona
	if out, err := e.runIt(e.destination, "stop"); err != nil {
		t.Fatalf("parar depois da troca: %v\n%s", err, out)
	}
	if s, ok := e.health(); ok {
		t.Errorf("depois do parar ainda responde: %+v", s)
	}
}

// O proxy cai no meio da carga: a porta é do supervisor e continua aberta, então nenhuma
// mensagem recebe "connection refused"; elas esperam o proxy seguinte. (As que estavam no
// meio, no proxy que caiu, são cortadas: isso não há como evitar.)
func TestProxyDownWithoutRefusing(t *testing.T) {
	a, _ := compiled(t)
	e := newEnvironment(t)
	e.install(a)
	if out, err := e.runIt(e.destination, "ensure"); err != nil {
		t.Fatalf("garantir: %v\n%s", err, out)
	}
	old := e.waitCommit("aaaaaaa")
	c := e.load(4)
	time.Sleep(500 * time.Millisecond)
	syscall.Kill(old.PID, syscall.SIGKILL)
	time.Sleep(3 * time.Second) // o supervisor espera 1 s e sobe outro
	before := c.ok.Load()
	time.Sleep(time.Second)
	close(c.stopped)
	c.wg.Wait()
	refused := 0
	for _, x := range c.examples {
		if strings.Contains(x, "connection refused") {
			refused++
		}
	}
	if refused > 0 {
		t.Errorf("%d mensagens receberam connection refused com o proxy fora", refused)
	}
	if c.ok.Load() == before {
		t.Errorf("nenhuma mensagem passou depois que o proxy voltou")
	}
	if s, _ := e.health(); s.PID == old.PID || s.Commit != "aaaaaaa" {
		t.Errorf("depois da queda responde %+v", s)
	}
}

// A versão nova não sobe: o atualizar desiste, põe o binário anterior de volta, e o proxy
// anterior nunca saiu do ar.
func TestUpdateRollsBackIfNotUp(t *testing.T) {
	a, _ := compiled(t)
	e := newEnvironment(t)
	e.install(a)
	if out, err := e.runIt(e.destination, "ensure"); err != nil {
		t.Fatalf("garantir: %v\n%s", err, out)
	}
	old := e.waitCommit("aaaaaaa")
	broken := filepath.Join(t.TempDir(), "llm-dlp")
	os.WriteFile(broken, []byte("#!/bin/sh\n[ \"$1\" = versao ] && echo 'llm-dlp 9.9.9 (quebrado)' && exit 0\nexit 1\n"), 0o755)

	c := e.load(2)
	// (com os nomes de antes da 0.9.0, que continuam aceitos)
	out, err := e.runIt(e.destination, "atualizar", "--arquivo", pkgName(t, broken))
	updateOutput := out
	time.Sleep(time.Second)
	c.stop(t, "durante e depois da tentativa")
	if err == nil || !strings.Contains(out, "voltei para a") {
		t.Fatalf("o atualizar devia falhar e voltar; err=%v\n%s", err, out)
	}
	if s, _ := e.health(); s.PID != old.PID || s.Commit != "aaaaaaa" {
		t.Errorf("responde %+v; esperado o proxy anterior (%+v), que nunca devia ter saído", s, old)
	}
	if _, commit, _ := versionOf(e.destination); commit != "aaaaaaa" {
		t.Errorf("o instalado ficou com o commit %q; esperado o anterior de volta", commit)
	}
	if out, err := e.runIt(e.destination, "stop"); err != nil {
		t.Errorf("parar: %v\n%s\nsaída do atualizar:\n%s\nlog:\n%s", err, out, updateOutput, e.log())
	}
}

// O download confere o pacote com o checksums.txt da release: um pacote diferente não é usado.
func TestDownloadReleaseChecksChecksum(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("só há binário pronto para Linux")
	}
	name := fmt.Sprintf("llm-dlp_1.2.3_linux_%s.tar.gz", runtime.GOARCH)
	content := []byte("pacote")
	sum := sha256.Sum256(content)
	sums := hex.EncodeToString(sum[:]) + "  " + name + "\n"
	served := content
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			http.Redirect(w, r, "/tag/v1.2.3", http.StatusFound)
		case "/download/v1.2.3/" + name:
			w.Write(served)
		case "/download/v1.2.3/checksums.txt":
			io.WriteString(w, sums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	v, err := publishedVersion(srv.URL, "")
	if err != nil || v != "1.2.3" {
		t.Fatalf("versão mais nova: %q, %v", v, err)
	}
	if _, err := downloadRelease(srv.URL, v, t.TempDir()); err != nil {
		t.Fatalf("pacote certo recusado: %v", err)
	}
	served = []byte("pacote trocado")
	if _, err := downloadRelease(srv.URL, v, t.TempDir()); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("pacote com checksum errado aceito: %v", err)
	}
	if _, err := publishedVersion(srv.URL, "1.2"); err == nil {
		t.Errorf("versão pedida inválida aceita")
	}
}

// Sem o supervisor.pid (na passagem da 0.7.0 para a 0.8.0, o supervisor antigo o apagava ao
// sair): reload e stop acham o supervisor pela trava. O reload faz a troca sem pausa, com o
// supervisor passando para o binário instalado, e o arquivo volta.
func TestSupervisorWithoutPidFile(t *testing.T) {
	a, b := compiled(t)
	e := newEnvironment(t)
	e.install(a)
	if out, err := e.runIt(e.destination, "ensure"); err != nil {
		t.Fatalf("ensure: %v\n%s", err, out)
	}
	old := e.waitCommit("aaaaaaa")
	sup := e.supervisor()
	pidFile := filepath.Join(e.home, ".config", "llm-dlp", "supervisor.pid")
	if sup <= 0 || os.Remove(pidFile) != nil {
		t.Fatalf("supervisor.pid: %d", sup)
	}

	e.install(b)
	c := e.load(4)
	time.Sleep(200 * time.Millisecond)
	if out, err := e.runIt(e.destination, "reload"); err != nil {
		t.Fatalf("reload: %v\n%s", err, out)
	}
	time.Sleep(300 * time.Millisecond)
	c.stop(t, "durante o reload")
	if r, _ := e.health(); r.Commit != "bbbbbbb" || r.PID == old.PID {
		t.Errorf("depois do reload responde %+v; esperado o bbbbbbb num processo novo", r)
	}
	if !strings.Contains(e.log(), fmt.Sprintf("agora no binário instalado (pid %d)", sup)) {
		t.Errorf("o supervisor %d não fez a troca (SIGHUP) nem se reexecutou\nlog:\n%s", sup, e.log())
	}
	if p := e.supervisor(); p != sup {
		t.Errorf("supervisor.pid = %d; esperado %d de volta", p, sup)
	}

	os.Remove(pidFile)
	if out, err := e.runIt(e.destination, "stop"); err != nil {
		t.Fatalf("stop sem o supervisor.pid: %v\n%s", err, out)
	}
	if !wentOut(sup, 5*time.Second) {
		t.Errorf("o supervisor (pid %d) não saiu com o stop", sup)
	}
}
