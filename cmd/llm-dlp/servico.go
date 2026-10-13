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
	"github.com/yurivenancio30/llm-dlp/internal/versao"
)

// Comandos do serviço: servir, supervisionar, garantir, verificar, status, parar.

// servir: o proxy. Sob o supervisor, atende na porta que ele abriu e emprestou (ver troca.go).
func servir(args []string) error {
	if ate, ok := emergenciaAtiva(); ok {
		return servirEmergencia(ate)
	}
	cfg, chave, pessoas, vistos, m, err := carregarTudo()
	if err != nil {
		return err
	}
	lg := abrirLog()
	ln, emprestada, err := portaDoServico(endereco(cfg))
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
	px := proxy.Novo(cfg, m, vistos, lg).ComChave(chave).ComTroca(emprestada)
	srv := &http.Server{Handler: px, ReadHeaderTimeout: 30 * time.Second}
	return servirAteSinal(srv, ln, lg, func() {
		vistos.SalvarAgora()
		pessoas.SalvarAgora()
	})
}

type infoSaude struct {
	OK      bool   `json:"ok"`
	Servico string `json:"servico"`
	Modo    string `json:"modo"`
	Ate     string `json:"ate"`
	Prova   string `json:"prova"`
	Versao  string `json:"versao"`
	Commit  string `json:"commit"`
	PID     int    `json:"pid"`
	// Troca: o proxy está num supervisor que segura a porta e o troca com SIGHUP (troca.go);
	// versões anteriores não dizem
	Troca bool `json:"troca"`
	// SemProva: algo responde na porta dizendo ser o llm-dlp, mas não provou
	SemProva bool `json:"-"`
}

// saude pergunta a quem está na porta se é o llm-dlp, e exige a prova: a resposta a um desafio
// novo, que só quem lê a chave calcula (proxy.Prova). Dizer "sou o llm-dlp" não basta: outro
// programa que pegue a porta enquanto o llm-dlp está fora do ar receberia a conversa sem máscara
// e a credencial da API. No modo emergência não há prova (o repasse não carrega a chave, que
// pode ser justamente o que quebrou): vale o arquivo que só o administrador grava.
func saude(cfg config.Config) (infoSaude, bool) {
	var s infoSaude
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return s, false
	}
	desafio := hex.EncodeToString(b)
	// conexão nova a cada pergunta: na troca de versão, reaproveitar a conexão perguntaria
	// sempre ao mesmo proxy (o antigo), e o novo nunca seria visto
	c := http.Client{Timeout: 700 * time.Millisecond, Transport: &http.Transport{DisableKeepAlives: true}}
	r, err := c.Get("http://" + endereco(cfg) + "/__llm-dlp/saude?desafio=" + desafio)
	if err != nil {
		return s, false
	}
	defer r.Body.Close()
	if r.StatusCode != 200 || json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&s) != nil || !s.OK || s.Servico != "llm-dlp" {
		return infoSaude{}, false
	}
	if s.Modo == "emergencia" {
		if _, ativa := emergenciaAtiva(); ativa {
			return s, true
		}
		s.SemProva = true
		return s, false
	}
	chave, err := mask.Chave(config.Caminho("chave"))
	if err != nil || !proxy.ProvaConfere(chave, "saude\x00"+desafio, s.Prova) {
		s.SemProva = true
		return s, false
	}
	return s, true
}

// errSemProva: o que responde na porta não é este llm-dlp (uma versão anterior ainda no ar, ou
// outro programa).
var errSemProva = errors.New("o programa que responde na porta do llm-dlp não provou ser o seu llm-dlp. " +
	"Se você acabou de atualizar: feche o Claude Code, rode \"llm-dlp parar\" e abra de novo. " +
	"Se não: outro programa está usando a porta")

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
	info, ok := saude(cfg)
	if ok {
		return nil
	}
	if info.SemProva {
		return errSemProva // subir outro não adianta: a porta está ocupada
	}
	sup := exec.Command(binarioServico(), "supervisionar")
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
	if !ok && !info.SemProva {
		garantir()
		info, ok = saude(cfg)
	}
	if !ok {
		if info.SemProva {
			return barrar(errSemProva.Error())
		}
		return barrar("o llm-dlp está fora do ar e não consegui subi-lo; veja " + config.Caminho("llm-dlp.log") +
			". Para usar o Claude sem máscara enquanto conserta: rode, no seu terminal, sudo llm-dlp emergencia")
	}
	// avisos só para você (systemMessage não vai para a API: zero tokens)
	aviso := ""
	if info.Modo == "emergencia" {
		ate := info.Ate
		if t, err := time.Parse(time.RFC3339, info.Ate); err == nil {
			ate = t.Local().Format("15:04")
		}
		aviso = "⚠️ llm-dlp em MODO EMERGÊNCIA: mensagens SEM máscara até " + ate + " (sudo llm-dlp emergencia sair)"
	} else if fracos := protecaoReduzida(cfg); len(fracos) > 0 {
		aviso = "⚠️ llm-dlp com a proteção reduzida pela configuração: " + strings.Join(fracos, "; ") +
			". Se não foi você, confira " + config.Caminho("config.json")
	}
	if aviso != "" {
		json.NewEncoder(os.Stdout).Encode(map[string]string{"systemMessage": aviso})
	}
	return nil
}

// protecaoReduzida: o que, na configuração em disco, tira proteção ou manda as mensagens para
// outro lugar. A configuração é do usuário, e um agente induzido por uma página maliciosa
// consegue mudá-la com um comando (o que só o administrador muda é a emergência). Enquanto esses
// campos não forem travados pelo administrador, o que protege é o aviso a cada mensagem; quem
// mudou de propósito só vê o lembrete.
func protecaoReduzida(cfg config.Config) []string {
	var out []string
	if u, err := url.Parse(cfg.Upstream); err != nil || u.Scheme != "https" || u.Hostname() != "api.anthropic.com" {
		destino := cfg.Upstream
		if err == nil && u.Host != "" {
			destino = u.Scheme + "://" + u.Host
		}
		out = append(out, "as mensagens e a credencial da API vão para "+proxy.Limpo(destino)+" (upstream), não para a API da Anthropic")
	}
	padrao := config.ProvedoresPadrao()
	for _, nome := range []string{"gemini"} {
		if pv, ok := cfg.Provedores[nome]; ok && pv.Upstream != padrao[nome].Upstream {
			out = append(out, "as mensagens e a credencial do provedor "+nome+" vão para "+proxy.Limpo(pv.Upstream)+", não para a API dele")
		}
	}
	if !cfg.FalharFechado {
		out = append(out, "uma falha ao mascarar manda a mensagem sem máscara (falhar_fechado = false)")
	}
	if cfg.OCR.Modo == "permitir" {
		out = append(out, "imagens e PDFs saem sem verificação (ocr.modo = permitir)")
	}
	if !cfg.Objetos.Ligado {
		out = append(out, "nomes de recursos internos não são mascarados (objetos.ligado = false)")
	}
	if len(cfg.DetectoresDesligados) > 0 {
		out = append(out, "detectores desligados: "+proxy.Limpo(strings.Join(cfg.DetectoresDesligados, ", ")))
	}
	if d := os.Getenv("LLM_DLP_HOME"); d != "" {
		out = append(out, "a configuração é lida de "+proxy.Limpo(d)+" (LLM_DLP_HOME)")
	}
	return out
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
		fmt.Println("⚠️  " + aviso + ": rode llm-dlp atualizar para trocar o que está no ar (sem fechar o Claude Code)")
	}
	if _, ok := saude(cfg); !ok {
		fmt.Println("⚠️  " + errSemProva.Error())
	}
	for _, f := range protecaoReduzida(cfg) {
		fmt.Println("⚠️  proteção reduzida pela configuração: " + f)
	}
	if falta := faltaOCR(cfg); falta != "" && cfg.OCR.Modo == "mascarar" {
		fmt.Printf("⚠️  sem OCR (falta %s): imagens e PDFs não são enviados. Para instalar: %s\n", falta, ocr.DicaInstalacao())
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
	if _, err := os.Stat(config.Caminho("supervisor.pid")); err != nil {
		return errors.New("supervisor não está rodando")
	}
	// o número do processo pode ter sido reaproveitado pelo sistema: supervisorNoAr confere
	// que é mesmo o supervisor do llm-dlp antes de mandar o sinal
	pid := supervisorNoAr()
	if pid == 0 {
		os.Remove(config.Caminho("supervisor.pid"))
		return errors.New("supervisor não está rodando (arquivo de pid antigo removido)")
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	// espera ele sair (e soltar a trava), para um "garantir" logo em seguida não colidir
	if !esperarSair(pid, 5*time.Second) {
		return errors.New("o supervisor não encerrou em 5s")
	}
	return nil
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
