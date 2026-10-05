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

const ajuda = `llm-dlp — mascara dado sensível entre você e a LLM

Instalação
  llm-dlp instalar              instala e configura tudo, explicando cada passo
  sudo llm-dlp instalar-trava   (recomendado) o Claude Code só funciona passando pelo llm-dlp
  llm-dlp desinstalar           desfaz a configuração do Claude Code (sudo llm-dlp desinstalar-trava para a trava)

Uso diário (normalmente você não precisa de nada disto)
  llm-dlp status                mostra se está no ar e em que modo
  llm-dlp importar-pessoas ARQ.csv --grupo COD:NOME:EMAIL [--separador-nome /]
                                ensina nomes/e-mails de pessoas (guardados só como hash)
  sudo llm-dlp emergencia [30m|sair]
                                se o llm-dlp quebrar: libera o Claude SEM máscara por tempo limitado

Diagnóstico
  llm-dlp ultima [N]            mostra as últimas N mensagens como a API recebeu (já mascaradas)
  llm-dlp simular SESSAO.jsonl  reproduz uma sessão antiga do Claude Code pelo llm-dlp, sem falar com a API, e confere
  llm-dlp colunas ARQ           mostra como os campos de um arquivo são entendidos (o que será mascarado)
  llm-dlp testar < arquivo      mostra a versão mascarada de um texto
  llm-dlp testar-midia ARQ DIR  processa uma imagem/PDF como o proxy e grava o resultado em DIR
  llm-dlp medir ARQ.jsonl...    custo e cobertura sobre transcripts (só contagens)
  llm-dlp versao

Internos (chamados pelos hooks e pelo supervisor)
  garantir, verificar, servir, supervisionar, parar
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(ajuda)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "servir":
		err = servir()
	case "supervisionar":
		err = supervisionar()
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
	case "versao", "--version":
		fmt.Println("llm-dlp", versao.Completa())
	default:
		fmt.Print(ajuda)
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
