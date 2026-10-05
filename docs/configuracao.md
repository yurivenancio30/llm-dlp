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
| `ferramentas_sem_desmascarar` | Ferramentas da web: recebem só pseudônimos, e o que trazem não é lembrado. Os conectores do claude.ai (`mcp__claude_ai_*`) recebem só pseudônimos sempre, fora desta lista | `WebFetch`, `WebSearch` |
| `documentos_sem_contexto` | Mascara CPF e CNPJ só com dígitos mesmo sem a palavra "cpf"/"cnpj" por perto, se o dígito verificador conferir. Cerca de 1 em 100 números aleatórios desse tamanho também confere e é mascarado a mais | `true` |
| `falhar_fechado` | Recusa o que não sabe mascarar. Desligado, uma falha ao mascarar manda a requisição **sem máscara** (só fica um aviso no log). Imagem e PDF que não deu para verificar são recusados de qualquer jeito. Não recomendado desligar | `true` |
| `ocr` | `modo` (`mascarar`, `bloquear` ou `permitir`), `idioma`, `max_paginas`, caminhos do tesseract e do poppler | `mascarar`, `por`, 30 |
| `objetos` | Nomes de recursos internos (ver [O que é detectado](deteccao.md#5-nomes-de-recursos-internos-pela-estrutura)). `ligado`; `mascarar` e `propagar`: mapas tipo → `true`/`false` (tipos: `servidor`, `database`, `schema`, `tabela`, `coluna`, `procedure`, `indice`, `usuario`, `namespace`, `servico`, `bucket`, `fila`). Ex.: `{"mascarar": {"coluna": false}}` deixa as colunas legíveis | ligado; mascara todos; propaga todos menos `coluna` e `indice` |
| `objetos.ip_publico` | Mascara também IPv4 público e IPv6 como nome de servidor (`HOST_...`). Ficam `::1`, link-local, faixas de documentação e os DNS públicos (`8.8.8.8`, `1.1.1.1`, `9.9.9.9`...). O IP privado é sempre mascarado pelo detector `ip` | desligado |
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
| `llm-dlp status` | Mostra se está no ar, em que modo e qual commit. Avisa se há um binário novo instalado que ainda não está no ar |
| `llm-dlp parar` | Para o proxy (ele volta na próxima mensagem) |
| `sudo llm-dlp emergencia [30m\|sair]` | Libera o Claude sem máscara por tempo limitado |
| `llm-dlp importar-pessoas ARQ.csv --grupo COD:NOME:EMAIL` | Ensina pessoas |
| `llm-dlp colunas ARQ` | Mostra como os campos de um arquivo são entendidos |
| `llm-dlp testar < arquivo` | Mostra a versão mascarada de um texto |
| `llm-dlp testar-midia ARQ DIR` | Processa uma imagem ou PDF e grava o resultado em DIR |
| `llm-dlp medir ARQ.jsonl` | Tempo e cobertura sobre uma sessão antiga do Claude Code (só contagens) |
| `llm-dlp versao` | Versão e commit do binário |

`garantir`, `verificar`, `servir` e `supervisionar` são internos: quem chama são os ganchos
do Claude Code e o supervisor.

## Atualizar o llm-dlp

Instalar o binário novo não troca o que está no ar: o processo antigo continua rodando até
ser parado.

1. Compile e instale (o anterior fica guardado):

   ```bash
   make build
   cp ~/.local/bin/llm-dlp ~/.local/bin/llm-dlp.bak-$(date +%Y%m%d-%H%M%S)
   install -m 755 bin/llm-dlp ~/.local/bin/llm-dlp.new && mv ~/.local/bin/llm-dlp.new ~/.local/bin/llm-dlp
   ~/.local/bin/llm-dlp versao        # mostra o commit novo
   ```

2. **Feche o Claude Code** (todas as janelas e sessões) e, num terminal, rode
   `llm-dlp parar`. Parar com uma conversa aberta deixa essa conversa sem resposta.
   Confira com `pgrep -a llm-dlp` que não sobrou nenhum processo.
3. Abra o Claude Code: o gancho da sessão sobe o binário novo. Confira com `llm-dlp status`
   (o commit tem que ser o novo, e sem aviso).

Depois de atualizar, a primeira mensagem de cada conversa regrava o cache uma vez (o
registro do que já saiu recomeça a cada versão).
