package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
	"github.com/yurivenancio30/llm-dlp/internal/version"
)

// llm-dlp: ponto de entrada e lista de comandos.

const help = `llm-dlp %s — mascara dado sensível entre você e a LLM (Claude Code)

Uso: llm-dlp COMANDO [argumentos]

Instalar e atualizar
  llm-dlp install               instala e configura tudo, explicando cada passo (pode rodar de novo)
  sudo llm-dlp install-lock     (recomendado) o Claude Code só funciona passando pelo llm-dlp
  llm-dlp uninstall             desfaz a configuração do Claude Code
  sudo llm-dlp uninstall-lock   tira a trava
  llm-dlp update [--version X.Y.Z] [--file ARQ.tar.gz]
                                baixa a versão nova (ou a pedida), confere e troca a que está no ar
                                sem tirá-lo do ar: não precisa fechar o Claude Code

Dia a dia (normalmente você não precisa de nada disto)
  llm-dlp status                mostra se está no ar, em qual versão, e o que falta (OCR, binário novo)
  llm-dlp reload                aplica uma mudança na configuração, sem tirar o llm-dlp do ar
  llm-dlp stop                  para o llm-dlp, com o Claude Code fechado; ele volta sozinho na próxima
                                mensagem
  llm-dlp import-people ARQ.csv --group COD:NOME:EMAIL [--name-separator /]
                                ensina nomes e e-mails de pessoas (guardados só como hash)
  sudo llm-dlp emergency [30m|off]
                                se o llm-dlp quebrar: libera o Claude SEM máscara por tempo limitado

Conferir o que é mascarado
  llm-dlp last [N]              mostra as últimas N mensagens como a API recebeu (já mascaradas)
  llm-dlp audit [24h|7d]        resume o log do período: o que foi mascarado por tipo, pseudônimos que
                                voltaram sem virar o valor real, envios sem máscara, recusas
  llm-dlp test < ARQ            mostra a versão mascarada de um texto
  llm-dlp test-media ARQ DIR    processa uma imagem ou PDF como o proxy e grava o resultado em DIR
  llm-dlp columns ARQ           mostra como os campos de um arquivo são entendidos
  llm-dlp simulate SESSAO.jsonl repete uma sessão antiga do Claude Code, sem falar com a API, e confere
  llm-dlp measure ARQ.jsonl...  tempo e cobertura sobre sessões antigas (só contagens)

Outros
  llm-dlp version               mostra a versão e o commit (também: --version)
  llm-dlp help                  mostra esta ajuda (também: --help, -h)

Configuração: %s (depois de mudar: llm-dlp reload)
Log:          %s
Documentação: https://github.com/yurivenancio30/llm-dlp

Internos (quem chama são os ganchos do Claude Code e o supervisor):
  ensure, check, serve, supervise
`

func helpText() string {
	return fmt.Sprintf(help, version.Version, config.Path("config.json"), config.Path("llm-dlp.log"))
}

// oldNames: os nomes em português de antes da 0.9.0. Continuam aceitos (os hooks já
// instalados no Claude Code chamam "garantir" e "verificar", e o supervisor sobe "servir" e
// "supervisionar", que toda versão entende: ver startProxy), mas não aparecem na ajuda nem
// nas docs.
var oldNames = map[string]string{
	"ajuda": "help", "servir": "serve", "supervisionar": "supervise", "garantir": "ensure",
	"verificar": "check", "atualizar": "update", "recarregar": "reload", "emergencia": "emergency",
	"parar": "stop", "importar-pessoas": "import-people", "testar": "test", "testar-midia": "test-media",
	"medir": "measure", "colunas": "columns", "ultima": "last", "simular": "simulate",
	"instalar": "install", "desinstalar": "uninstall", "instalar-trava": "install-lock",
	"desinstalar-trava": "uninstall-lock", "versao": "version",
}

func main() {
	if len(os.Args) < 2 {
		fmt.Print(helpText())
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	if newOne, ok := oldNames[cmd]; ok {
		cmd = newOne
	}
	var err error
	switch cmd {
	case "help", "--help", "-h":
		fmt.Print(helpText())
	case "serve":
		err = serve(args)
	case "supervise":
		err = supervise(args)
	case "update":
		err = update(args)
	case "reload":
		err = reload()
	case "ensure":
		err = ensure()
	case "status":
		err = status()
	case "check":
		err = verifyIt()
	case "emergency":
		err = emergency(args)
	case "stop":
		err = stop()
	case "import-people":
		err = importPeople(args)
	case "test":
		err = test()
	case "test-media":
		err = testMedia(args)
	case "measure":
		err = measure(args)
	case "columns":
		err = columns(args)
	case "last":
		err = lastConv(args)
	case "simulate":
		err = simulate(args)
	case "audit":
		err = audit(args)
	case "install":
		err = install(args)
	case "uninstall":
		err = uninstall(args)
	case "install-lock":
		err = installLock(false)
	case "uninstall-lock":
		err = installLock(true)
	case "version", "--version", "-v":
		fmt.Println("llm-dlp", version.Complete())
	default:
		fmt.Fprintf(os.Stderr, "llm-dlp: comando desconhecido: %q\n\n", cmd)
		fmt.Fprint(os.Stderr, helpText())
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "llm-dlp:", err)
		os.Exit(1)
	}
}

func loadAll() (config.Config, []byte, *mask.People, *mask.Seen, *mask.Masker, error) {
	cfg, err := config.Load()
	if err != nil {
		return cfg, nil, nil, nil, nil, fmt.Errorf("config: %w", err)
	}
	key, err := mask.Key(config.Path("chave"))
	if err != nil {
		return cfg, nil, nil, nil, nil, err
	}
	people, err := mask.LoadPeople(config.Path("pessoas.json"))
	if err != nil {
		return cfg, nil, nil, nil, nil, err
	}
	seenSet, err := mask.LoadSeen(config.Path("vistos.json"))
	if err != nil {
		return cfg, nil, nil, nil, nil, err
	}
	m, err := mask.NewMasker(cfg, key, people, seenSet)
	return cfg, key, people, seenSet, m, err
}

func openLog() *log.Logger {
	l := &rotatingLog{p: config.Path("llm-dlp.log")}
	l.mu.Lock()
	l.reopen()
	l.mu.Unlock()
	if l.f == nil {
		return log.New(os.Stderr, "", log.LstdFlags)
	}
	return log.New(l, "", log.LstdFlags)
}

// maxLog: o tamanho em que o llm-dlp.log gira. Fica um só antigo (llm-dlp.log.1): no disco,
// no máximo uns 20 MB.
var maxLog int64 = 10 << 20

// rotatingLog: o llm-dlp.log, que gira ao passar de maxLog a qualquer momento (antes só girava
// quando o llm-dlp subia, e um proxy no ar por semanas fazia o arquivo crescer sem limite). Mais
// de um processo escreve nele (supervisor e proxy): quem percebe que o outro girou reabre.
type rotatingLog struct {
	mu sync.Mutex
	p  string
	f  *os.File
}

func (l *rotatingLog) reopen() {
	if l.f != nil {
		l.f.Close()
	}
	l.f, _ = os.OpenFile(l.p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

func (l *rotatingLog) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	st, err := os.Stat(l.p)
	if err == nil && st.Size()+int64(len(b)) > maxLog {
		os.Rename(l.p, l.p+".1")
		l.reopen()
	} else if fst, e := statOf(l.f); err != nil || e != nil || !os.SameFile(st, fst) {
		l.reopen() // outro processo girou (ou apagaram o arquivo)
	}
	if l.f == nil {
		return os.Stderr.Write(b)
	}
	return l.f.Write(b)
}

func statOf(f *os.File) (os.FileInfo, error) {
	if f == nil {
		return nil, os.ErrClosed
	}
	return f.Stat()
}

// logFile: o arquivo em que o log escreve agora (o supervisor o passa como saída de erro
// do proxy, para um panic ficar registrado), ou nil.
func logFile(lg *log.Logger) *os.File {
	switch w := lg.Writer().(type) {
	case *os.File:
		return w
	case *rotatingLog:
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.f
	}
	return nil
}

func address(cfg config.Config) string { return "127.0.0.1:" + strconv.Itoa(cfg.Port) }
