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

// update and reload: the swap does not leave the port empty nor cut a response, and goes back by
// itself when the new version does not start. The tests build llm-dlp twice (different commits) and
// run a real supervisor and proxy, with the home folder in a temporary directory and a fake API in
// place of Anthropic's.

var binaries struct {
	once sync.Once
	dir  string
	err  error
}

// TestMain puts the home folder in a temporary directory: install, uninstall and update touch
// ~/.claude and ~/.local/bin, and a test cannot touch the real ones. The Go caches stay in the
// usual ones (the go build of the swap tests does not recompile nor download everything again).
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

// compiled: llm-dlp with commit "aaaaaaa" and with "bbbbbbb".
func compiled(t *testing.T) (a, b string) {
	t.Helper()
	if testing.Short() {
		t.Skip("compila o llm-dlp duas vezes")
	}
	if runtime.GOOS != "linux" {
		t.Skip("the swap without a pause is Linux's")
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
				"-ldflags", "-X github.com/yurivenancio30/llm-dlp/internal/version.Commit="+c, ".").CombinedOutput()
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

// fakeAPI answers in streaming, slowly: 300 ms; 8 s if the message asks for a long answer (longer
// than graceDeadline: the answer has to survive the end of the old proxy's grace).
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

// log: the end of the test llm-dlp's log (only the fake API went through it).
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

// shutdown stops the supervisor (and the proxy) the test left running.
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
	// a new connection: a reused one could be with the old proxy, which still serves the
	// connections already open during the grace
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

// ask sends a message through the proxy and checks that the answer came whole.
func (e *environment) ask(cli *http.Client, text string) error {
	body := `{"model":"x","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"` + text + `"}]}`
	r, err := cli.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/messages", e.port), "application/json", strings.NewReader(body))
	if err != nil {
		return err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("response cut: %w", err)
	}
	if r.StatusCode != 200 || !bytes.Contains(b, []byte("message_stop")) {
		return fmt.Errorf("status %d, resposta incompleta: %.120s", r.StatusCode, b)
	}
	return nil
}

// load: n clients sending messages nonstop, reusing connections like Claude Code.
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
		t.Errorf("%s: %d of %d messages failed (e.g. %v)", what, n, n+c.ok.Load(), c.examples[0])
	}
	if c.ok.Load() < 8 {
		t.Errorf("%s: only %d messages went through; the load did not cover the swap", what, c.ok.Load())
	}
}

// pkgName: a .tar.gz with the file bin as "llm-dlp", like the release one.
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

// A version swap with messages arriving all the time and a long answer in the middle: no message
// fails, the long answer finishes whole (through the old proxy), and after that only the new one
// answers. The same for reload.
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
	// like install.sh: the new binary, from outside, updates the installed one
	out, err := e.runIt(b, "update", "--file", pkgName(t, b))
	if err != nil {
		t.Fatalf("update: %v\n%s\nlog:\n%s", err, out, e.log())
	}
	newOne, _ := e.health()
	if newOne.Commit != "bbbbbbb" || newOne.PID == old.PID {
		t.Fatalf("after update it answers %+v; expected commit bbbbbbb in a new process\n%s", newOne, out)
	}
	time.Sleep(300 * time.Millisecond)
	c.stop(t, "durante a troca")
	if err := <-longOne; err != nil {
		t.Errorf("the long response, started before the swap, was cut: %v", err)
	}
	if _, commit, _ := versionOf(e.destination); commit != "bbbbbbb" {
		t.Errorf("the installed one kept commit %q", commit)
	}
	if copies, _ := filepath.Glob(e.destination + ".bak-*"); len(copies) != 1 {
		t.Errorf("copies of the previous binary: %v; expected one", copies)
	}
	if !wentOut(old.PID, graceDeadline+5*time.Second) {
		t.Errorf("the old proxy (pid %d) did not exit", old.PID)
	}
	// the supervisor also moved to the new binary (re-executed, without releasing the port)
	if exe, _ := os.Readlink(fmt.Sprintf("/proc/%d/exe", e.supervisor())); exe != e.destination {
		t.Errorf("the supervisor runs %q; expected the installed binary %s\nlog:\n%s", exe, e.destination, e.log())
	}

	// reload: the same binary in a new process, also without any failure
	c = e.load(4)
	time.Sleep(200 * time.Millisecond)
	if out, err := e.runIt(e.destination, "reload"); err != nil {
		t.Fatalf("recarregar: %v\n%s", err, out)
	}
	time.Sleep(300 * time.Millisecond)
	c.stop(t, "durante o recarregar")
	if r, _ := e.health(); r.Commit != "bbbbbbb" || r.PID == newOne.PID {
		t.Errorf("after reload it answers %+v; expected bbbbbbb in a new process", r)
	}

	// the lock and supervisor.pid moved to the new supervisor: stop works
	if out, err := e.runIt(e.destination, "stop"); err != nil {
		t.Fatalf("stop after the swap: %v\n%s", err, out)
	}
	if s, ok := e.health(); ok {
		t.Errorf("after stop it still answers: %+v", s)
	}
}

// The proxy goes down in the middle of the load: the port belongs to the supervisor and stays open,
// so no message gets "connection refused"; they wait for the next proxy. (Those in the middle, in
// the proxy that went down, are cut: that cannot be avoided.)
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
		t.Errorf("%d messages got connection refused with the proxy down", refused)
	}
	if c.ok.Load() == before {
		t.Errorf("no message went through after the proxy came back")
	}
	if s, _ := e.health(); s.PID == old.PID || s.Commit != "aaaaaaa" {
		t.Errorf("after the crash it answers %+v", s)
	}
}

// The new version does not start: update gives up, puts the previous binary back, and the previous
// proxy never went offline.
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
	// (with the names from before 0.9.0, which are still accepted)
	out, err := e.runIt(e.destination, "atualizar", "--arquivo", pkgName(t, broken))
	updateOutput := out
	time.Sleep(time.Second)
	c.stop(t, "durante e depois da tentativa")
	if err == nil || !strings.Contains(out, "went back to") {
		t.Fatalf("update should fail and go back; err=%v\n%s", err, out)
	}
	if s, _ := e.health(); s.PID != old.PID || s.Commit != "aaaaaaa" {
		t.Errorf("answers %+v; expected the previous proxy (%+v), which should never have exited", s, old)
	}
	if _, commit, _ := versionOf(e.destination); commit != "aaaaaaa" {
		t.Errorf("the installed one kept commit %q; expected the previous one back", commit)
	}
	if out, err := e.runIt(e.destination, "stop"); err != nil {
		t.Errorf("stop: %v\n%s\nupdate output:\n%s\nlog:\n%s", err, out, updateOutput, e.log())
	}
}

// The download checks the archive against the release's checksums.txt: a different archive is not
// used.
func TestDownloadReleaseChecksChecksum(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("there is a ready binary only for Linux")
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
		t.Fatalf("newest version: %q, %v", v, err)
	}
	if _, err := downloadRelease(srv.URL, v, t.TempDir()); err != nil {
		t.Fatalf("right package refused: %v", err)
	}
	served = []byte("pacote trocado")
	if _, err := downloadRelease(srv.URL, v, t.TempDir()); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("package with a wrong checksum accepted: %v", err)
	}
	if _, err := publishedVersion(srv.URL, "1.2"); err == nil {
		t.Errorf("invalid requested version accepted")
	}
}

// Without supervisor.pid (when going from 0.7.0 to 0.8.0, the old supervisor deleted it on exit):
// reload and stop find the supervisor by the lock. reload swaps without a pause, with the
// supervisor moving to the installed binary, and the file comes back.
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
		t.Errorf("after reload it answers %+v; expected bbbbbbb in a new process", r)
	}
	if !strings.Contains(e.log(), fmt.Sprintf("now on the installed binary (pid %d)", sup)) {
		t.Errorf("supervisor %d neither swapped (SIGHUP) nor re-executed itself\nlog:\n%s", sup, e.log())
	}
	if p := e.supervisor(); p != sup {
		t.Errorf("supervisor.pid = %d; expected %d back", p, sup)
	}

	os.Remove(pidFile)
	if out, err := e.runIt(e.destination, "stop"); err != nil {
		t.Fatalf("stop without supervisor.pid: %v\n%s", err, out)
	}
	if !wentOut(sup, 5*time.Second) {
		t.Errorf("the supervisor (pid %d) did not exit with stop", sup)
	}
}
