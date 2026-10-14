package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
	"github.com/yurivenancio30/llm-dlp/internal/ocr"
	"github.com/yurivenancio30/llm-dlp/internal/proxy"
	"github.com/yurivenancio30/llm-dlp/internal/version"
)

// Service commands: serve, supervise, ensure, check, status, stop.

// serve: the proxy. Under the supervisor, it serves on the port the supervisor opened and lent (see
// swap.go).
func serve(args []string) error {
	if until, ok := emergencyActive(); ok {
		return serveEmergency(until)
	}
	cfg, key, people, seenSet, m, err := loadAll()
	if err != nil {
		return err
	}
	lg := openLog()
	ln, borrowed, err := servicePort(address(cfg))
	if err != nil {
		return err
	}
	lg.Printf("no ar em %s (pid %d, versão %s)", address(cfg), os.Getpid(), version.Complete())
	// what already went out to the API goes out the same on resends, also after this restart
	// (cache). If the configuration or the version changed, the record starts over.
	imp, _ := json.Marshal(cfg)
	if err := m.UseSent(config.Path("enviados.log"), version.Complete()+"\x00"+string(imp)); err != nil {
		lg.Printf("AVISO enviados.log: %v (o histórico pode ser regravado no cache uma vez)", err)
	}
	go watchEmergency(false) // if the administrator turns on emergency, it exits and the supervisor switches mode
	px := proxy.New(cfg, m, seenSet, lg).WithKey(key).WithSwap(borrowed)
	srv := &http.Server{Handler: px, ReadHeaderTimeout: 30 * time.Second}
	return serveUntilSignal(srv, ln, lg, func() {
		seenSet.SaveNow()
		people.SaveNow()
	})
}

type healthInfo struct {
	OK      bool   `json:"ok"`
	Service string `json:"servico"`
	Mode    string `json:"modo"`
	Until   string `json:"ate"`
	Proof   string `json:"prova"`
	Version string `json:"versao"`
	Commit  string `json:"commit"`
	PID     int    `json:"pid"`
	// Swap: the proxy is under a supervisor that holds the port and swaps it with SIGHUP (swap.go);
	// earlier versions do not say
	Swap bool `json:"troca"`
	// WithoutProof: something answers on the port saying it is llm-dlp, but did not prove it
	WithoutProof bool `json:"-"`
}

// health asks whoever is on the port whether it is llm-dlp, and requires the proof: the answer to a
// fresh challenge, which only whoever reads the key computes (proxy.Proof). Saying "I am llm-dlp"
// is not enough: another program that takes the port while llm-dlp is offline would receive the
// conversation without masking and the API credential. In emergency mode there is no proof (the
// pass-through does not load the key, which may be exactly what broke): the file only the
// administrator writes is what counts.
func health(cfg config.Config) (healthInfo, bool) {
	var s healthInfo
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return s, false
	}
	challenge := hex.EncodeToString(b)
	// a new connection for each question: during a version swap, reusing the connection would
	// always ask the same proxy (the old one), and the new one would never be seen
	c := http.Client{Timeout: 700 * time.Millisecond, Transport: &http.Transport{DisableKeepAlives: true}}
	r, err := c.Get("http://" + address(cfg) + "/__llm-dlp/saude?desafio=" + challenge)
	if err != nil {
		return s, false
	}
	defer r.Body.Close()
	if r.StatusCode != 200 || json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&s) != nil || !s.OK || s.Service != "llm-dlp" {
		return healthInfo{}, false
	}
	if s.Mode == "emergencia" {
		if _, active := emergencyActive(); active {
			return s, true
		}
		s.WithoutProof = true
		return s, false
	}
	key, err := mask.Key(config.Path("chave"))
	if err != nil || !proxy.ProofMatches(key, "saude\x00"+challenge, s.Proof) {
		s.WithoutProof = true
		return s, false
	}
	return s, true
}

// errNoProof: what answers on the port is not this llm-dlp (an earlier version still running, or
// another program).
var errNoProof = errors.New("o programa que responde na porta do llm-dlp não provou ser o seu llm-dlp. " +
	"Se você acabou de atualizar: feche o Claude Code, rode \"llm-dlp stop\" e abra de novo. " +
	"Se não: outro programa está usando a porta")

func healthy(cfg config.Config) bool {
	_, ok := health(cfg)
	return ok
}

// ensure is called by the SessionStart hook: it prints nothing when it works (hook output becomes
// context and would spend tokens).
func ensure() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	info, ok := health(cfg)
	if ok {
		return nil
	}
	if info.WithoutProof {
		return errNoProof // starting another one does not help: the port is taken
	}
	sup := exec.Command(serviceBinary(), "supervisionar")
	sup.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	devnull, _ := os.Open(os.DevNull)
	sup.Stdin, sup.Stdout, sup.Stderr = devnull, devnull, devnull
	if err := sup.Start(); err != nil {
		return err
	}
	sup.Process.Release()
	// TOTAL deadline (not a number of attempts): the hook must finish well before its timeout,
	// otherwise Claude Code ignores the hook and moves on
	limitN := time.Now().Add(ensureDeadline)
	for time.Now().Before(limitN) {
		if healthy(cfg) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("o proxy não subiu em %s; veja %s", ensureDeadline, config.Path("llm-dlp.log"))
}

// ensureDeadline + the health check stay well below timeoutHook.
const (
	ensureDeadline = 6 * time.Second
	timeoutHook    = 20 // seconds, written in settings.json by install
)

// check is the UserPromptSubmit hook: it blocks the message (exit 2) if Claude Code is not pointing
// to llm-dlp or if llm-dlp is not healthy. Silent if everything is fine (spends no tokens). It is
// the second layer; the first is allowedProviders in managed-settings.json.
func verifyIt() error {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default() // broken config: goes on with the default (emergency mode may be on)
	}
	expected := "http://" + address(cfg)
	if current := strings.TrimRight(os.Getenv("ANTHROPIC_BASE_URL"), "/"); current != expected {
		return blockRequest(fmt.Sprintf("o Claude Code não está apontando para o llm-dlp (ANTHROPIC_BASE_URL=%q, esperado %q)", current, expected))
	}
	info, ok := health(cfg)
	if !ok && !info.WithoutProof {
		ensure()
		info, ok = health(cfg)
	}
	if !ok {
		if info.WithoutProof {
			return blockRequest(errNoProof.Error())
		}
		return blockRequest("o llm-dlp está fora do ar e não consegui subi-lo; veja " + config.Path("llm-dlp.log") +
			". Para usar o Claude sem máscara enquanto conserta: rode, no seu terminal, sudo llm-dlp emergency")
	}
	// warnings only for you (systemMessage does not go to the API: zero tokens)
	warning := ""
	if info.Mode == "emergencia" {
		until := info.Until
		if t, err := time.Parse(time.RFC3339, info.Until); err == nil {
			until = t.Local().Format("15:04")
		}
		warning = "⚠️ llm-dlp em MODO EMERGÊNCIA: mensagens SEM máscara até " + until + " (sudo llm-dlp emergency off)"
	} else if weakSet := reducedProtection(cfg); len(weakSet) > 0 {
		warning = "⚠️ llm-dlp com a proteção reduzida pela configuração: " + strings.Join(weakSet, "; ") +
			". Se não foi você, confira " + config.Path("config.json")
	}
	if warning != "" {
		json.NewEncoder(os.Stdout).Encode(map[string]string{"systemMessage": warning})
	}
	return nil
}

// reducedProtection: what, in the configuration on disk, removes protection or sends the messages
// elsewhere. The configuration belongs to the user, and an agent induced by a malicious page can
// change it with a command (what only the administrator changes is emergency). While these fields
// are not locked by the administrator, what protects is the warning on every message; whoever
// changed it on purpose only sees the reminder.
func reducedProtection(cfg config.Config) []string {
	var out []string
	if u, err := url.Parse(cfg.Upstream); err != nil || u.Scheme != "https" || u.Hostname() != "api.anthropic.com" {
		destination := cfg.Upstream
		if err == nil && u.Host != "" {
			destination = u.Scheme + "://" + u.Host
		}
		out = append(out, "as mensagens e a credencial da API vão para "+proxy.Clean(destination)+" (upstream), não para a API da Anthropic")
	}
	pattern := config.DefaultProviders()
	for _, name := range []string{"gemini"} {
		if pv, ok := cfg.Providers[name]; ok && pv.Upstream != pattern[name].Upstream {
			out = append(out, "as mensagens e a credencial do provedor "+name+" vão para "+proxy.Clean(pv.Upstream)+", não para a API dele")
		}
	}
	if !cfg.FailClosed {
		out = append(out, "uma falha ao mascarar manda a mensagem sem máscara (fail_closed = false)")
	}
	if cfg.OCR.Mode == "allow" {
		out = append(out, "imagens e PDFs saem sem verificação (ocr.mode = allow)")
	}
	if !cfg.Objects.Enabled {
		out = append(out, "nomes de recursos internos não são mascarados (objects.enabled = false)")
	}
	if len(cfg.DisabledDetectors) > 0 {
		out = append(out, "detectores desligados: "+proxy.Clean(strings.Join(cfg.DisabledDetectors, ", ")))
	}
	if d := os.Getenv("LLM_DLP_HOME"); d != "" {
		out = append(out, "a configuração é lida de "+proxy.Clean(d)+" (LLM_DLP_HOME)")
	}
	return out
}

func blockRequest(reason string) error {
	fmt.Fprintln(os.Stderr, "🛑 llm-dlp: mensagem NÃO enviada — "+reason)
	os.Exit(2)
	return nil
}

func status() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	c := http.Client{Timeout: time.Second}
	r, err := c.Get("http://" + address(cfg) + "/__llm-dlp/saude")
	if err != nil {
		fmt.Println("fora do ar")
		return nil
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	fmt.Println("no ar:", string(b))
	var info struct {
		Commit string `json:"commit"`
		PID    int    `json:"pid"`
	}
	json.Unmarshal(b, &info)
	if warning := newBinary(info.Commit, info.PID); warning != "" {
		fmt.Println("⚠️  " + warning + ": rode llm-dlp update para trocar o que está no ar (sem fechar o Claude Code)")
	}
	if _, ok := health(cfg); !ok {
		fmt.Println("⚠️  " + errNoProof.Error())
	}
	for _, f := range reducedProtection(cfg) {
		fmt.Println("⚠️  proteção reduzida pela configuração: " + f)
	}
	if missingOne := missingOCR(cfg); missingOne != "" && cfg.OCR.Mode == "mask" {
		fmt.Printf("⚠️  sem OCR (falta %s): imagens e PDFs não são enviados. Para instalar: %s\n", missingOne, ocr.InstallHint())
	}
	return nil
}

// newBinary says whether the running process is not the installed binary: another commit, or its
// executable file was replaced after it started.
func newBinary(commit string, pid int) string {
	if commit != version.Commit {
		if commit == "" {
			commit = "sem commit (versão antiga)"
		}
		return fmt.Sprintf("binário novo instalado (no ar: %s; instalado: %s)", commit, version.Commit)
	}
	if pid > 0 {
		if target, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil && strings.HasSuffix(target, " (deleted)") {
			return "binário novo instalado (o arquivo do processo no ar foi substituído)"
		}
	}
	return ""
}

func stop() error {
	// the process number may have been reused by the system: supervisorUp checks it really is the
	// llm-dlp supervisor before sending the signal (and finds it by the lock, if the pid file
	// disappeared)
	pid := supervisorUp()
	if pid == 0 {
		os.Remove(config.Path("supervisor.pid"))
		return errors.New("supervisor não está rodando")
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	// waits for it to exit (and release the lock), so an "ensure" right after does not collide
	if !waitExit(pid, 5*time.Second) {
		return errors.New("o supervisor não encerrou em 5s")
	}
	return nil
}

// commandLine of the process: /proc on Linux; ps on macOS (which has no /proc).
func commandLine(pid int) string {
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
		return string(b)
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return ""
	}
	return string(out)
}
