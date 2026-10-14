package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
	"github.com/yurivenancio30/llm-dlp/internal/versao"
)

// llm-dlp: ponto de entrada e lista de comandos.

const ajuda = `llm-dlp %s — mascara dado sensível entre você e a LLM (Claude Code)

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

func textoAjuda() string {
	return fmt.Sprintf(ajuda, versao.Versao, config.Caminho("config.json"), config.Caminho("llm-dlp.log"))
}

// nomesAntigos: os nomes em português de antes da 0.9.0. Continuam aceitos (os hooks já
// instalados no Claude Code chamam "garantir" e "verificar", e o supervisor sobe "servir" e
// "supervisionar", que toda versão entende: ver iniciarProxy), mas não aparecem na ajuda nem
// nas docs.
var nomesAntigos = map[string]string{
	"ajuda": "help", "servir": "serve", "supervisionar": "supervise", "garantir": "ensure",
	"verificar": "check", "atualizar": "update", "recarregar": "reload", "emergencia": "emergency",
	"parar": "stop", "importar-pessoas": "import-people", "testar": "test", "testar-midia": "test-media",
	"medir": "measure", "colunas": "columns", "ultima": "last", "simular": "simulate",
	"instalar": "install", "desinstalar": "uninstall", "instalar-trava": "install-lock",
	"desinstalar-trava": "uninstall-lock", "versao": "version",
}

func main() {
	if len(os.Args) < 2 {
		fmt.Print(textoAjuda())
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	if novo, ok := nomesAntigos[cmd]; ok {
		cmd = novo
	}
	var err error
	switch cmd {
	case "help", "--help", "-h":
		fmt.Print(textoAjuda())
	case "serve":
		err = servir(args)
	case "supervise":
		err = supervisionar(args)
	case "update":
		err = atualizar(args)
	case "reload":
		err = recarregar()
	case "ensure":
		err = garantir()
	case "status":
		err = status()
	case "check":
		err = verificar()
	case "emergency":
		err = emergencia(args)
	case "stop":
		err = parar()
	case "import-people":
		err = importarPessoas(args)
	case "test":
		err = testar()
	case "test-media":
		err = testarMidia(args)
	case "measure":
		err = medir(args)
	case "columns":
		err = colunas(args)
	case "last":
		err = ultima(args)
	case "simulate":
		err = simular(args)
	case "audit":
		err = auditar(args)
	case "install":
		err = instalar(args)
	case "uninstall":
		err = desinstalar(args)
	case "install-lock":
		err = instalarTrava(false)
	case "uninstall-lock":
		err = instalarTrava(true)
	case "version", "--version", "-v":
		fmt.Println("llm-dlp", versao.Completa())
	default:
		fmt.Fprintf(os.Stderr, "llm-dlp: comando desconhecido: %q\n\n", cmd)
		fmt.Fprint(os.Stderr, textoAjuda())
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "llm-dlp:", err)
		os.Exit(1)
	}
}

func carregarTudo() (config.Config, []byte, *mask.Pessoas, *mask.Vistos, *mask.Masker, error) {
	cfg, err := config.Carregar()
	if err != nil {
		return cfg, nil, nil, nil, nil, fmt.Errorf("config: %w", err)
	}
	chave, err := mask.Chave(config.Caminho("chave"))
	if err != nil {
		return cfg, nil, nil, nil, nil, err
	}
	pessoas, err := mask.CarregarPessoas(config.Caminho("pessoas.json"))
	if err != nil {
		return cfg, nil, nil, nil, nil, err
	}
	vistos, err := mask.CarregarVistos(config.Caminho("vistos.json"))
	if err != nil {
		return cfg, nil, nil, nil, nil, err
	}
	m, err := mask.NovoMasker(cfg, chave, pessoas, vistos)
	return cfg, chave, pessoas, vistos, m, err
}

func abrirLog() *log.Logger {
	l := &logGirando{p: config.Caminho("llm-dlp.log")}
	l.mu.Lock()
	l.reabrir()
	l.mu.Unlock()
	if l.f == nil {
		return log.New(os.Stderr, "", log.LstdFlags)
	}
	return log.New(l, "", log.LstdFlags)
}

// maxLog: o tamanho em que o llm-dlp.log gira. Fica um só antigo (llm-dlp.log.1): no disco,
// no máximo uns 20 MB.
var maxLog int64 = 10 << 20

// logGirando: o llm-dlp.log, que gira ao passar de maxLog a qualquer momento (antes só girava
// quando o llm-dlp subia, e um proxy no ar por semanas fazia o arquivo crescer sem limite). Mais
// de um processo escreve nele (supervisor e proxy): quem percebe que o outro girou reabre.
type logGirando struct {
	mu sync.Mutex
	p  string
	f  *os.File
}

func (l *logGirando) reabrir() {
	if l.f != nil {
		l.f.Close()
	}
	l.f, _ = os.OpenFile(l.p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

func (l *logGirando) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	st, err := os.Stat(l.p)
	if err == nil && st.Size()+int64(len(b)) > maxLog {
		os.Rename(l.p, l.p+".1")
		l.reabrir()
	} else if fst, e := statDe(l.f); err != nil || e != nil || !os.SameFile(st, fst) {
		l.reabrir() // outro processo girou (ou apagaram o arquivo)
	}
	if l.f == nil {
		return os.Stderr.Write(b)
	}
	return l.f.Write(b)
}

func statDe(f *os.File) (os.FileInfo, error) {
	if f == nil {
		return nil, os.ErrClosed
	}
	return f.Stat()
}

// arquivoDoLog: o arquivo em que o log escreve agora (o supervisor o passa como saída de erro
// do proxy, para um panic ficar registrado), ou nil.
func arquivoDoLog(lg *log.Logger) *os.File {
	switch w := lg.Writer().(type) {
	case *os.File:
		return w
	case *logGirando:
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.f
	}
	return nil
}

func endereco(cfg config.Config) string { return "127.0.0.1:" + strconv.Itoa(cfg.Porta) }
