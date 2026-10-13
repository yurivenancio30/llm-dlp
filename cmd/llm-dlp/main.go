package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
	"github.com/yurivenancio30/llm-dlp/internal/versao"
)

// llm-dlp: ponto de entrada e lista de comandos.

const ajuda = `llm-dlp %s — mascara dado sensível entre você e a LLM (Claude Code)

Uso: llm-dlp COMANDO [argumentos]

Instalar e atualizar
  llm-dlp instalar              instala e configura tudo, explicando cada passo (pode rodar de novo)
  sudo llm-dlp instalar-trava   (recomendado) o Claude Code só funciona passando pelo llm-dlp
  llm-dlp desinstalar           desfaz a configuração do Claude Code
  sudo llm-dlp desinstalar-trava
                                tira a trava
  llm-dlp atualizar [--versao X.Y.Z] [--arquivo ARQ.tar.gz]
                                baixa a versão nova (ou a pedida), confere e troca a que está no ar
                                sem tirá-lo do ar: não precisa fechar o Claude Code

Dia a dia (normalmente você não precisa de nada disto)
  llm-dlp status                mostra se está no ar, em qual versão, e o que falta (OCR, binário novo)
  llm-dlp recarregar            aplica uma mudança na configuração, sem tirar o llm-dlp do ar
  llm-dlp parar                 para o llm-dlp, com o Claude Code fechado; ele volta sozinho na próxima
                                mensagem
  llm-dlp importar-pessoas ARQ.csv --grupo COD:NOME:EMAIL [--separador-nome /]
                                ensina nomes e e-mails de pessoas (guardados só como hash)
  sudo llm-dlp emergencia [30m|sair]
                                se o llm-dlp quebrar: libera o Claude SEM máscara por tempo limitado

Conferir o que é mascarado
  llm-dlp ultima [N]            mostra as últimas N mensagens como a API recebeu (já mascaradas)
  llm-dlp testar < ARQ          mostra a versão mascarada de um texto
  llm-dlp testar-midia ARQ DIR  processa uma imagem ou PDF como o proxy e grava o resultado em DIR
  llm-dlp colunas ARQ           mostra como os campos de um arquivo são entendidos
  llm-dlp simular SESSAO.jsonl  repete uma sessão antiga do Claude Code, sem falar com a API, e confere
  llm-dlp medir ARQ.jsonl...    tempo e cobertura sobre sessões antigas (só contagens)

Outros
  llm-dlp versao                mostra a versão e o commit (também: --version)
  llm-dlp ajuda                 mostra esta ajuda (também: --help, -h)

Configuração: %s (depois de mudar: llm-dlp recarregar)
Log:          %s
Documentação: https://github.com/yurivenancio30/llm-dlp

Internos (quem chama são os ganchos do Claude Code e o supervisor):
  garantir, verificar, servir, supervisionar
`

func textoAjuda() string {
	return fmt.Sprintf(ajuda, versao.Versao, config.Caminho("config.json"), config.Caminho("llm-dlp.log"))
}

func main() {
	if len(os.Args) < 2 {
		fmt.Print(textoAjuda())
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "ajuda", "help", "--help", "-h":
		fmt.Print(textoAjuda())
	case "servir":
		err = servir(args)
	case "supervisionar":
		err = supervisionar(args)
	case "atualizar":
		err = atualizar(args)
	case "recarregar":
		err = recarregar()
	case "garantir":
		err = garantir()
	case "status":
		err = status()
	case "verificar":
		err = verificar()
	case "emergencia":
		err = emergencia(args)
	case "parar":
		err = parar()
	case "importar-pessoas":
		err = importarPessoas(args)
	case "testar":
		err = testar()
	case "testar-midia":
		err = testarMidia(args)
	case "medir":
		err = medir(args)
	case "colunas":
		err = colunas(args)
	case "ultima":
		err = ultima(args)
	case "simular":
		err = simular(args)
	case "instalar":
		err = instalar(args)
	case "desinstalar":
		err = desinstalar(args)
	case "instalar-trava":
		err = instalarTrava(false)
	case "desinstalar-trava":
		err = instalarTrava(true)
	case "versao", "--version", "-v":
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
	p := config.Caminho("llm-dlp.log")
	if st, err := os.Stat(p); err == nil && st.Size() > 10<<20 {
		os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return log.New(os.Stderr, "", log.LstdFlags)
	}
	return log.New(f, "", log.LstdFlags)
}

func endereco(cfg config.Config) string { return "127.0.0.1:" + strconv.Itoa(cfg.Porta) }
