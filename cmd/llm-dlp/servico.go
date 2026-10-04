package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/proxy"
	"github.com/yurivenancio30/llm-dlp/internal/versao"
)

// Comandos do serviço: servir, supervisionar, garantir, verificar, status, parar.

func servir() error {
	if ate, ok := emergenciaAtiva(); ok {
		return servirEmergencia(ate)
	}
	cfg, _, _, vistos, m, err := carregarTudo()
	if err != nil {
		return err
	}
	lg := abrirLog()
	ln, err := net.Listen("tcp", endereco(cfg))
	if err != nil {
		return err
	}
	lg.Printf("no ar em %s (pid %d, versão %s)", endereco(cfg), os.Getpid(), versao.Completa())
	// o que já saiu à API sai igual nos reenvios, também depois deste reinício (cache).
	// Se a configuração ou a versão mudou, o registro recomeça.
	imp, _ := json.Marshal(cfg)
	if err := m.UsarEnviados(config.Caminho("enviados.log"), versao.Completa()+"\x00"+string(imp)); err != nil {
		lg.Printf("AVISO enviados.log: %v (o histórico pode ser regravado no cache uma vez)", err)
	}
	go vigiarEmergencia(false) // se o administrador ligar a emergência, sai e o supervisor troca de modo
	srv := &http.Server{Handler: proxy.Novo(cfg, m, vistos, lg), ReadHeaderTimeout: 30 * time.Second}
	return srv.Serve(ln)
}

// supervisionar mantém o proxy no ar: se ele cair, sobe de novo (espera 1s, 2s, 4s... até 10s).
func supervisionar() error {
	lock, err := os.OpenFile(config.Caminho("supervisor.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil // já existe um supervisor
	}
	os.WriteFile(config.Caminho("supervisor.pid"), []byte(strconv.Itoa(os.Getpid())), 0o600)
	lg := abrirLog()
	eu, _ := os.Executable()
	sinais := make(chan os.Signal, 1)
	signal.Notify(sinais, syscall.SIGTERM, syscall.SIGINT)
	espera := time.Second
	for {
		filho := exec.Command(eu, "servir")
		filho.Stdout, filho.Stderr = lg.Writer(), lg.Writer()
		if err := filho.Start(); err != nil {
			lg.Printf("supervisor: não consegui iniciar: %v", err)
		} else {
			lg.Printf("supervisor: proxy iniciado (pid %d)", filho.Process.Pid)
			fim := make(chan error, 1)
			ini := time.Now()
			go func() { fim <- filho.Wait() }()
			select {
			case s := <-sinais:
				filho.Process.Signal(syscall.SIGTERM)
				<-fim
				lg.Printf("supervisor: encerrado (%v)", s)
				os.Remove(config.Caminho("supervisor.pid"))
				return nil
			case err := <-fim:
				if err == nil {
					// saída intencional (troca de modo normal <-> emergência): volta na hora,
					// sem a espera crescente de quando ele cai
					lg.Printf("supervisor: troca de modo; reiniciando já")
					espera = time.Second
					continue
				}
				lg.Printf("supervisor: proxy caiu (%v); reiniciando em %s", err, espera)
				if time.Since(ini) > time.Minute {
					espera = time.Second
				}
			}
		}
		select {
		case <-time.After(espera):
		case <-sinais:
			os.Remove(config.Caminho("supervisor.pid"))
			return nil
		}
		if espera < 10*time.Second {
			espera *= 2
		}
	}
}

type infoSaude struct {
	OK      bool   `json:"ok"`
	Servico string `json:"servico"`
	Modo    string `json:"modo"`
	Ate     string `json:"ate"`
}

func saude(cfg config.Config) (infoSaude, bool) {
	var s infoSaude
	c := http.Client{Timeout: 700 * time.Millisecond}
	r, err := c.Get("http://" + endereco(cfg) + "/__llm-dlp/saude")
	if err != nil {
		return s, false
	}
	defer r.Body.Close()
	// não basta responder 200: tem que ser o llm-dlp (outro programa na porta não conta)
	ok := r.StatusCode == 200 && json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&s) == nil &&
		s.OK && s.Servico == "llm-dlp"
	return s, ok
}

func saudavel(cfg config.Config) bool {
	_, ok := saude(cfg)
	return ok
}

// garantir é chamado pelo hook SessionStart: não imprime nada quando dá certo
// (saída de hook vira contexto e gastaria tokens).
func garantir() error {
	cfg, err := config.Carregar()
	if err != nil {
		return err
	}
	if saudavel(cfg) {
		return nil
	}
	eu, _ := os.Executable()
	sup := exec.Command(eu, "supervisionar")
	sup.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	devnull, _ := os.Open(os.DevNull)
	sup.Stdin, sup.Stdout, sup.Stderr = devnull, devnull, devnull
	if err := sup.Start(); err != nil {
		return err
	}
	sup.Process.Release()
	// prazo TOTAL (não número de tentativas): o hook precisa terminar bem antes do
	// timeout dele, senão o Claude Code ignora o hook e segue em frente
	limite := time.Now().Add(prazoGarantir)
	for time.Now().Before(limite) {
		if saudavel(cfg) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("o proxy não subiu em %s; veja %s", prazoGarantir, config.Caminho("llm-dlp.log"))
}

// prazoGarantir + o teste de saúde ficam bem abaixo de timeoutHook.
const (
	prazoGarantir = 6 * time.Second
	timeoutHook   = 20 // segundos, gravado no settings.json pelo instalar-claude
)

// verificar é o hook UserPromptSubmit: barra a mensagem (exit 2) se o Claude Code não
// estiver apontando para o llm-dlp ou se o llm-dlp não estiver saudável. Silencioso se
// estiver tudo certo (não gasta tokens). É a segunda camada; a primeira é o
// allowedProviders no managed-settings.json.
func verificar() error {
	cfg, err := config.Carregar()
	if err != nil {
		cfg = config.Padrao() // config quebrada: segue com o padrão (o modo emergência pode estar ligado)
	}
	esperado := "http://" + endereco(cfg)
	if atual := strings.TrimRight(os.Getenv("ANTHROPIC_BASE_URL"), "/"); atual != esperado {
		return barrar(fmt.Sprintf("o Claude Code não está apontando para o llm-dlp (ANTHROPIC_BASE_URL=%q, esperado %q)", atual, esperado))
	}
	info, ok := saude(cfg)
	if !ok {
		garantir()
		if info, ok = saude(cfg); !ok {
			return barrar("o llm-dlp está fora do ar e não consegui subi-lo; veja " + config.Caminho("llm-dlp.log") +
				". Para usar o Claude sem máscara enquanto conserta: rode, no seu terminal, sudo llm-dlp emergencia")
		}
	}
	if info.Modo == "emergencia" {
		// aviso só para você (systemMessage não vai para a API: zero tokens)
		ate := info.Ate
		if t, err := time.Parse(time.RFC3339, info.Ate); err == nil {
			ate = t.Local().Format("15:04")
		}
		json.NewEncoder(os.Stdout).Encode(map[string]string{"systemMessage": "⚠️ llm-dlp em MODO EMERGÊNCIA: mensagens SEM máscara até " + ate + " (sudo llm-dlp emergencia sair)"})
	}
	return nil
}

func barrar(motivo string) error {
	fmt.Fprintln(os.Stderr, "🛑 llm-dlp: mensagem NÃO enviada — "+motivo)
	os.Exit(2)
	return nil
}

func status() error {
	cfg, err := config.Carregar()
	if err != nil {
		return err
	}
	c := http.Client{Timeout: time.Second}
	r, err := c.Get("http://" + endereco(cfg) + "/__llm-dlp/saude")
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
	if aviso := binarioNovo(info.Commit, info.PID); aviso != "" {
		fmt.Println("⚠️  " + aviso + ": rode llm-dlp parar com o Claude Code fechado para aplicar (ele volta sozinho na próxima mensagem)")
	}
	return nil
}

// binarioNovo diz se o processo no ar não é o binário instalado: outro commit, ou o
// arquivo do executável dele foi trocado depois que ele subiu.
func binarioNovo(commit string, pid int) string {
	if commit != versao.Commit {
		if commit == "" {
			commit = "sem commit (versão antiga)"
		}
		return fmt.Sprintf("binário novo instalado (no ar: %s; instalado: %s)", commit, versao.Commit)
	}
	if pid > 0 {
		if alvo, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil && strings.HasSuffix(alvo, " (deleted)") {
			return "binário novo instalado (o arquivo do processo no ar foi substituído)"
		}
	}
	return ""
}

func parar() error {
	b, err := os.ReadFile(config.Caminho("supervisor.pid"))
	if err != nil {
		return errors.New("supervisor não está rodando")
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	// o número do processo pode ter sido reaproveitado pelo sistema: confere que é mesmo
	// o supervisor do llm-dlp antes de mandar o sinal
	cmdline := linhaDeComando(pid)
	if !strings.Contains(cmdline, "supervisionar") || !strings.Contains(cmdline, "llm-dlp") {
		os.Remove(config.Caminho("supervisor.pid"))
		return errors.New("supervisor não está rodando (arquivo de pid antigo removido)")
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	// espera ele sair (e soltar a trava), para um "garantir" logo em seguida não colidir
	for i := 0; i < 50; i++ {
		if syscall.Kill(pid, 0) != nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("o supervisor não encerrou em 5s")
}

// linhaDeComando do processo: /proc no Linux; ps no macOS (que não tem /proc).
func linhaDeComando(pid int) string {
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
		return string(b)
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return ""
	}
	return string(out)
}
