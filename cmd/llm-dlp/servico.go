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

// Commands do serviço: servir, supervisionar, garantir, verificar, status, parar.

// servir: o proxy. Sob o supervisor, atende na porta que ele abriu e emprestou (ver troca.go).
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
	// o que já saiu à API sai igual nos reenvios, também depois deste reinício (cache).
	// Se a configuração ou a versão mudou, o registro recomeça.
	imp, _ := json.Marshal(cfg)
	if err := m.UseSent(config.Path("enviados.log"), version.Complete()+"\x00"+string(imp)); err != nil {
		lg.Printf("AVISO enviados.log: %v (o histórico pode ser regravado no cache uma vez)", err)
	}
	go watchEmergency(false) // se o administrador ligar a emergência, sai e o supervisor troca de modo
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
	// Swap: o proxy está num supervisor que segura a porta e o troca com SIGHUP (troca.go);
	// versões anteriores não dizem
	Swap bool `json:"troca"`
	// WithoutProof: algo responde na porta dizendo ser o llm-dlp, mas não provou
	WithoutProof bool `json:"-"`
}

// saude pergunta a quem está na porta se é o llm-dlp, e exige a prova: a resposta a um desafio
// novo, que só quem lê a chave calcula (proxy.Proof). Dizer "sou o llm-dlp" não basta: outro
// programa que pegue a porta enquanto o llm-dlp está fora do ar receberia a conversa sem máscara
// e a credencial da API. No modo emergência não há prova (o repasse não carrega a chave, que
// pode ser justamente o que quebrou): vale o arquivo que só o administrador grava.
func health(cfg config.Config) (healthInfo, bool) {
	var s healthInfo
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return s, false
	}
	challenge := hex.EncodeToString(b)
	// conexão nova a cada pergunta: na troca de versão, reaproveitar a conexão perguntaria
	// sempre ao mesmo proxy (o antigo), e o novo nunca seria visto
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

// errNoProof: o que responde na porta não é este llm-dlp (uma versão anterior ainda no ar, ou
// outro programa).
var errNoProof = errors.New("o programa que responde na porta do llm-dlp não provou ser o seu llm-dlp. " +
	"Se você acabou de atualizar: feche o Claude Code, rode \"llm-dlp stop\" e abra de novo. " +
	"Se não: outro programa está usando a porta")

func healthy(cfg config.Config) bool {
	_, ok := health(cfg)
	return ok
}

// garantir é chamado pelo hook SessionStart: não imprime nada quando dá certo
// (saída de hook vira contexto e gastaria tokens).
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
		return errNoProof // subir outro não adianta: a porta está ocupada
	}
	sup := exec.Command(serviceBinary(), "supervisionar")
	sup.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	devnull, _ := os.Open(os.DevNull)
	sup.Stdin, sup.Stdout, sup.Stderr = devnull, devnull, devnull
	if err := sup.Start(); err != nil {
		return err
	}
	sup.Process.Release()
	// prazo TOTAL (não número de tentativas): o hook precisa terminar bem antes do
	// timeout dele, senão o Claude Code ignora o hook e segue em frente
	limitN := time.Now().Add(ensureDeadline)
	for time.Now().Before(limitN) {
		if healthy(cfg) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("o proxy não subiu em %s; veja %s", ensureDeadline, config.Path("llm-dlp.log"))
}

// ensureDeadline + o teste de saúde ficam bem abaixo de timeoutHook.
const (
	ensureDeadline = 6 * time.Second
	timeoutHook    = 20 // segundos, gravado no settings.json pelo instalar-claude
)

// verificar é o hook UserPromptSubmit: barra a mensagem (exit 2) se o Claude Code não
// estiver apontando para o llm-dlp ou se o llm-dlp não estiver saudável. Silencioso se
// estiver tudo certo (não gasta tokens). É a segunda camada; a primeira é o
// allowedProviders no managed-settings.json.
func verifyIt() error {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default() // config quebrada: segue com o padrão (o modo emergência pode estar ligado)
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
	// avisos só para você (systemMessage não vai para a API: zero tokens)
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

// reducedProtection: o que, na configuração em disco, tira proteção ou manda as mensagens para
// outro lugar. A configuração é do usuário, e um agente induzido por uma página maliciosa
// consegue mudá-la com um comando (o que só o administrador muda é a emergência). Enquanto esses
// campos não forem travados pelo administrador, o que protege é o aviso a cada mensagem; quem
// mudou de propósito só vê o lembrete.
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

// newBinary diz se o processo no ar não é o binário instalado: outro commit, ou o
// arquivo do executável dele foi trocado depois que ele subiu.
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
	// o número do processo pode ter sido reaproveitado pelo sistema: supervisorUp confere
	// que é mesmo o supervisor do llm-dlp antes de mandar o sinal (e o acha pela trava, se o
	// arquivo de pid sumiu)
	pid := supervisorUp()
	if pid == 0 {
		os.Remove(config.Path("supervisor.pid"))
		return errors.New("supervisor não está rodando")
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	// espera ele sair (e soltar a trava), para um "garantir" logo em seguida não colidir
	if !waitExit(pid, 5*time.Second) {
		return errors.New("o supervisor não encerrou em 5s")
	}
	return nil
}

// commandLine do processo: /proc no Linux; ps no macOS (que não tem /proc).
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
