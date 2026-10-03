# Configuração e comandos

[← voltar ao README](../README.md)

## Configuração

Arquivo `~/.config/llm-dlp/config.json`. Depois de mudar, rode `llm-dlp parar`.

| Campo | Para quê | Padrão |
|---|---|---|
| `dominios_internos` | Trechos que identificam os seus hostnames internos (`["empresa"]` pega `mysql.empresa.intra`) | vazio |
| `emails_liberados` | E-mails que passam sem máscara | vazio |
| `dominios_email_liberados` | Domínios de e-mail que passam sem máscara | `example.com`, `anthropic.com`… |
| `papeis_host` | Primeira parte do hostname que pode ficar visível (`mysql` em `mysql-x7k2.invalid`) | `mysql`, `postgres`, `redis`… |
| `campos_extras` | Palavras a mais para os nomes de campo | vazio |
| `padroes_extras` | Regex próprias | vazio |
| `termos` | Palavras exatas a mascarar | vazio |
| `detectores_desligados` | Detectores a desligar (lista abaixo) | vazio |
| `detectores_opcionais` | Detectores a ligar. Hoje só `quase`: sexo, idade, estado civil, profissão, nacionalidade, renda, latitude/longitude | vazio |
| `ferramentas_sem_desmascarar` | Ferramentas que recebem só pseudônimos | `WebFetch`, `WebSearch` |
| `falhar_fechado` | Recusa o que não sabe mascarar | `true` |
| `ocr` | `modo` (`mascarar`, `bloquear` ou `permitir`), `idioma`, `max_paginas`, caminhos do tesseract e do poppler | `mascarar`, `por`, 30 |
| `porta` | Porta local do proxy | 8787 |
| `upstream` | Endereço da API | `https://api.anthropic.com` |

Nomes aceitos em `detectores_desligados`: `segredo`, `email`, `ip`, `host`, `cpf`, `cnpj`,
`pis`, `cnh`, `rg`, `telefone`, `cep`, `cartao`, `conta`, `pix`, `endereco`, `nascimento`,
`nome`, `campo` (detecção pelo nome do campo), `usuario`, `doc`, `iban`, `processo`, `mac`.

## Comandos

| Comando | O que faz |
|---|---|
| `llm-dlp instalar` | Instala e configura tudo, explicando cada passo |
| `sudo llm-dlp instalar-trava` | O Claude Code só funciona passando pelo llm-dlp |
| `llm-dlp desinstalar` / `sudo llm-dlp desinstalar-trava` | Desfaz |
| `llm-dlp status` | Mostra se está no ar e em que modo |
| `llm-dlp parar` | Para o proxy (ele volta na próxima mensagem) |
| `sudo llm-dlp emergencia [30m\|sair]` | Libera o Claude sem máscara por tempo limitado |
| `llm-dlp importar-pessoas ARQ.csv --grupo COD:NOME:EMAIL` | Ensina pessoas |
| `llm-dlp colunas ARQ` | Mostra como os campos de um arquivo são entendidos |
| `llm-dlp testar < arquivo` | Mostra a versão mascarada de um texto |
| `llm-dlp testar-midia ARQ DIR` | Processa uma imagem ou PDF e grava o resultado em DIR |
| `llm-dlp medir ARQ.jsonl` | Tempo e cobertura sobre uma sessão antiga do Claude Code (só contagens) |
| `llm-dlp versao` | Versão |

`garantir`, `verificar`, `servir` e `supervisionar` são internos: quem chama são os ganchos
do Claude Code e o supervisor.
