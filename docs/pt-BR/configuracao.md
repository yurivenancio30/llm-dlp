# Configuração e comandos

[← voltar ao README](README.md) · [English](../configuration.md)

## Configuração

Arquivo `~/.config/llm-dlp/config.json`. Depois de mudar, rode `llm-dlp parar`.

Enquanto a configuração tirar proteção (`falhar_fechado` desligado, `ocr.modo` em
`permitir`, `objetos` desligado, algum detector desligado) ou o `upstream` não for a API da
Anthropic, o Claude Code mostra um aviso a cada mensagem (ver
[Segurança](seguranca.md)). O aviso não vai para a API.

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
| `falhar_fechado` | Recusa o que não sabe mascarar. Desligado, uma falha ao mascarar manda a requisição **sem máscara** (só fica um aviso no log). Imagem e PDF que não deu para verificar nunca saem, com qualquer valor deste campo. Não recomendado desligar | `true` |
| `ocr` | `modo` (`mascarar`, `bloquear` ou `permitir`), `idioma`, `max_paginas`, caminhos do tesseract e do poppler. Em `bloquear`, e em `mascarar` quando não dá para verificar o arquivo (falta o OCR, arquivo ilegível), ele é retirado da requisição e o modelo recebe um aviso no lugar | `mascarar`, `por`, 30 |
| `objetos` | Nomes de recursos internos (ver [O que é detectado](deteccao.md#5-nomes-de-recursos-internos-pela-estrutura)). `ligado`; `mascarar` e `propagar`: mapas tipo → `true`/`false` (tipos: `servidor`, `database`, `schema`, `tabela`, `coluna`, `procedure`, `indice`, `usuario`, `namespace`, `servico`, `bucket`, `fila`). Ex.: `{"mascarar": {"coluna": false}}` deixa as colunas legíveis | ligado; mascara todos; propaga todos menos `coluna` e `indice` |
| `objetos.ip_publico` | Mascara também IPv4 público e IPv6 como nome de servidor (`HOST_...`). Ficam `::1`, link-local, faixas de documentação e os DNS públicos (`8.8.8.8`, `1.1.1.1`, `9.9.9.9`...). O IP privado é sempre mascarado pelo detector `ip` | desligado |
| `porta` | Porta local do proxy | 8787 |
| `upstream` | Endereço da API | `https://api.anthropic.com` |
| `provedores` | Outras APIs atendidas na mesma porta, pelo caminho `/<nome>` (ver [Outras APIs](#outras-apis-gemini)). Cada uma tem `upstream` e `formato` (`gemini` ou `anthropic`; outro valor cai no modo genérico) | `gemini` → `https://generativelanguage.googleapis.com` |

Nomes aceitos em `detectores_desligados`: `segredo`, `email`, `ip`, `host`, `cpf`, `cnpj`,
`pis`, `cnh`, `rg`, `telefone`, `cep`, `cartao`, `conta`, `pix`, `endereco`, `nascimento`,
`nome`, `campo` (detecção pelo nome do campo), `usuario`, `doc`, `iban`, `processo`, `mac`.

## Outras APIs (Gemini)

O llm-dlp atende a API do Gemini na mesma porta, no caminho `/gemini`: aponte o endereço da
API do cliente para `http://127.0.0.1:8787/gemini`. A chave da API continua sendo a sua e
passa direto (o llm-dlp não a guarda nem a grava no log).

| Cliente | Como apontar |
|---|---|
| Gemini CLI | `export GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:8787/gemini` |
| opencode (provedor `google`) | No `opencode.json`: `"provider": {"google": {"options": {"baseURL": "http://127.0.0.1:8787/gemini/v1beta"}}}` |
| SDKs do Google (`google-genai`) | A opção de endereço base (`http_options.base_url` / `httpOptions.baseUrl`) com `http://127.0.0.1:8787/gemini` |
| `curl` | `curl http://127.0.0.1:8787/gemini/v1beta/models/MODELO:generateContent -H "x-goog-api-key: $CHAVE" ...` |

Vale o mesmo mascaramento do Claude Code: texto, chamadas e resultados de ferramenta,
instruções do sistema, imagens e PDFs, com a resposta desmascarada também em streaming. Foi
testado com a API de verdade por `curl` (com e sem streaming e com chamada de ferramenta); a
linha de cada cliente acima segue a documentação dele e ainda não foi conferida com o
cliente rodando.

O que fica de fora:

- **A trava e os ganchos são do Claude Code.** Nada impede o outro cliente de falar direto com
  a API se o endereço dele for desfeito, e não há o aviso a cada mensagem.
- Áudio, vídeo e outros arquivos anexados (`inlineData` que não é imagem nem PDF) são
  retirados: o llm-dlp não tem como verificá-los.
- O resumo do raciocínio do modelo (`thought`) volta com os pseudônimos, sem desmascarar.
- APIs no formato da OpenAI (Grok, OpenRouter, Copilot) ainda não têm leitor próprio: num
  provedor com outro `formato`, todo texto da requisição é mascarado e a resposta em streaming
  volta com os pseudônimos.

## Comandos

| Comando | O que faz |
|---|---|
| `llm-dlp instalar` | Instala e configura tudo, explicando cada passo |
| `sudo llm-dlp instalar-trava` | O Claude Code só funciona passando pelo llm-dlp |
| `llm-dlp desinstalar` / `sudo llm-dlp desinstalar-trava` | Desfaz |
| `llm-dlp status` | Mostra se está no ar, em que modo e qual commit. Avisa se há um binário novo instalado que ainda não está no ar, se falta o OCR e se a configuração reduz a proteção |
| `llm-dlp parar` | Para o proxy (ele volta na próxima mensagem) |
| `sudo llm-dlp emergencia [30m\|sair]` | Libera o Claude sem máscara por tempo limitado |
| `llm-dlp importar-pessoas ARQ.csv --grupo COD:NOME:EMAIL` | Ensina pessoas |
| `llm-dlp colunas ARQ` | Mostra como os campos de um arquivo são entendidos |
| `llm-dlp testar < arquivo` | Mostra a versão mascarada de um texto |
| `llm-dlp testar-midia ARQ DIR` | Processa uma imagem ou PDF e grava o resultado em DIR |
| `llm-dlp medir ARQ.jsonl` | Tempo e cobertura sobre uma sessão antiga do Claude Code (só contagens) |
| `llm-dlp versao` | Versão e commit do binário (também `--version`) |
| `llm-dlp ajuda` | A lista de comandos (também `--help`) |

`garantir`, `verificar`, `servir` e `supervisionar` são internos: quem chama são os ganchos
do Claude Code e o supervisor.

## Atualizar o llm-dlp

Instalar o binário novo não troca o que está no ar: o processo antigo continua rodando até
ser parado.

1. Instale o binário novo (guarde o anterior, se quiser poder voltar):

   ```bash
   cp ~/.local/bin/llm-dlp ~/.local/bin/llm-dlp.bak-$(date +%Y%m%d-%H%M%S)
   ```

   Com o binário da release (o caminho comum), rode o instalador de novo; ele troca o
   programa e pula o que já está pronto:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/yurivenancio30/llm-dlp/main/install.sh | sh
   ~/.local/bin/llm-dlp versao        # mostra a versão e o commit novos
   ```

   Ou compilando do código:

   ```bash
   make instalar
   ~/.local/bin/llm-dlp versao
   ```

2. **Feche o Claude Code** (todas as janelas e sessões) e, num terminal, rode
   `llm-dlp parar`. Parar com uma conversa aberta deixa essa conversa sem resposta.
   Confira com `pgrep -a llm-dlp` que não sobrou nenhum processo.
3. Abra o Claude Code: o gancho da sessão sobe o binário novo. Confira com `llm-dlp status`
   (o commit tem que ser o novo, e sem aviso).

Depois de atualizar, a primeira mensagem de cada conversa regrava o cache uma vez (o
registro do que já saiu recomeça a cada versão).
