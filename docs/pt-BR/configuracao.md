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
| `provedores` | **Beta.** Outras APIs atendidas na mesma porta, pelo caminho `/<nome>` (ver [Outros clientes e APIs](#outros-clientes-e-apis-beta)). Cada uma tem `upstream` e `formato` (`gemini`, `openai` ou `anthropic`; outro valor cai no modo genérico) | `gemini`, `openai` e `xai`, cada um com o endereço da sua API |

Nomes aceitos em `detectores_desligados`: `segredo`, `email`, `ip`, `host`, `cpf`, `cnpj`,
`pis`, `cnh`, `rg`, `telefone`, `cep`, `cartao`, `conta`, `pix`, `endereco`, `nascimento`,
`nome`, `campo` (detecção pelo nome do campo), `usuario`, `doc`, `iban`, `processo`, `mac`.

## Outros clientes e APIs (beta)

> [!WARNING]
> **Beta.** Tudo nesta seção (os formatos do Gemini e da OpenAI, e os clientes Gemini CLI,
> opencode e Copilot) está em beta. Só o Claude Code é estável. Os clientes em beta **não têm
> trava**: se o cliente deixar de apontar para o llm-dlp, ele fala direto com a API, sem
> máscara, e nada avisa. Leia [o que é beta](#o-que-é-beta) antes de usá-los com dado real.

O llm-dlp foi feito em torno do Claude Code, e o `llm-dlp instalar` configura **só o Claude
Code**. Outros clientes funcionam com o mesmo mascaramento, mas você os aponta à mão e eles
não têm trava.

| Cliente | Status | Instalado e travado pelo `llm-dlp instalar` |
|---|---|---|
| Claude Code | **Estável** | Sim |
| Gemini CLI | Beta | Não |
| opencode | Beta | Não |
| GitHub Copilot (CLI e VS Code), com a sua chave | Beta | Não |
| Qualquer outro cliente da API da Anthropic, do Gemini ou da OpenAI (Chat Completions) | Beta | Não |

### O que é beta

- **Vem ligado e no mesmo binário.** É testado (testes automáticos e clientes de verdade, ver
  a tabela [Cliente por cliente](#cliente-por-cliente)), mas foi muito menos usado que o
  caminho do Claude Code.
- **Pode mudar numa versão MENOR**, sem o aviso que a
  [tabela de versões](versoes.md#o-que-cada-número-significa) dá aos recursos estáveis: os
  endereços, o campo `provedores` e o que é lido em cada formato.
- **É isolado do Claude Code.** O código de cada formato só roda para as requisições do caminho
  dele (`/gemini`, `/openai`, `/xai`...). Uma requisição do Claude Code sai byte a byte igual
  com ou sem esses provedores, e uma requisição ou resposta malformada de outro formato não
  derruba o llm-dlp. As duas coisas são conferidas por testes automáticos. A memória da
  conversa é compartilhada: um nome mascarado numa conversa do Gemini também é mascarado no
  Claude Code (mais máscara, nunca menos).
- **Avise dos problemas** nas [issues](https://github.com/yurivenancio30/llm-dlp/issues),
  dizendo o cliente e a versão dele.

### Claude Code e os clientes em beta

| | Claude Code (estável) | Gemini CLI, opencode, Copilot e outros clientes (beta) |
|---|---|---|
| O que sai é mascarado e o que volta é desmascarado (texto, chamadas e resultados de ferramenta, arquivos que o agente escreve, imagens e PDFs) | Sim | Sim, o mesmo motor |
| `llm-dlp ultima` e o log | Sim | Sim |
| Apontado para o llm-dlp pelo `llm-dlp instalar` | Sim (passo 4) | **Não.** Você põe o endereço no cliente, como nas tabelas abaixo |
| O llm-dlp sobe sozinho | Sim, ao abrir uma sessão | **Não.** Rode `llm-dlp garantir` antes de usar o cliente. Se o llm-dlp não estiver no ar, o cliente recebe "conexão recusada" e nada sai |
| Conferência a cada mensagem (o llm-dlp está no ar, é da versão certa e prova que é ele) | Sim | **Não** |
| Trava: o cliente se recusa a funcionar fora do llm-dlp | Sim (passo 5) | **Não.** Se o endereço for tirado da configuração do cliente, ele fala direto com a API, sem máscara, e nada avisa |

Em resumo: com outro cliente, a proteção vale enquanto o endereço dele apontar para o llm-dlp.
Confira com `llm-dlp ultima` depois da primeira mensagem: ele tem de mostrar a sua conversa,
com os pseudônimos.

### Qual endereço dar ao cliente

Cada API é atendida num caminho da mesma porta. A chave da API continua sendo a sua e passa
direto (o llm-dlp não a guarda nem a grava no log).

| API | Endereço | O que o llm-dlp lê |
|---|---|---|
| Anthropic | `http://127.0.0.1:8787` | Messages (`/v1/messages`) |
| Gemini | `http://127.0.0.1:8787/gemini` | `generateContent`, `streamGenerateContent`, `countTokens` |
| Gemini, endereço compatível com a OpenAI | `http://127.0.0.1:8787/gemini/v1beta/openai` | Chat Completions |
| OpenAI | `http://127.0.0.1:8787/openai/v1` | Chat Completions |
| xAI (Grok) | `http://127.0.0.1:8787/xai/v1` | Chat Completions |
| Outra (OpenRouter, um gateway da empresa) | acrescente em `provedores`, com o `upstream` dela | `formato`: `openai`, `gemini` ou `anthropic` |

Um caminho terminado em `/chat/completions` é sempre lido como Chat Completions, seja qual
for o provedor.

### Cliente por cliente

| Cliente | Como apontar | Como foi conferido |
|---|---|---|
| Gemini CLI | `export GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:8787/gemini` | Rodando (0.63.0), API de verdade |
| opencode (provedor `google`) | No `opencode.json`: `"provider": {"google": {"options": {"baseURL": "http://127.0.0.1:8787/gemini/v1beta"}}}` | Rodando (1.19.0), API de verdade |
| GitHub Copilot CLI, com a sua chave | `export COPILOT_PROVIDER_BASE_URL=http://127.0.0.1:8787/openai/v1` (ou `/xai/v1`, ou outro endereço da tabela acima), mais `COPILOT_PROVIDER_API_KEY` e `COPILOT_MODEL` | Rodando (1.0.95), API de verdade, com a ressalva abaixo |
| GitHub Copilot no VS Code, com a sua chave | "Chat: Manage Language Models" > "Custom Endpoint". No `url` do modelo: `http://127.0.0.1:8787/openai/v1/chat/completions` (tipo de API Chat Completions) ou `http://127.0.0.1:8787/v1/messages` (tipo de API Messages) | **Não conferido.** Segue a documentação do VS Code; o formato é o mesmo do Copilot CLI |
| SDKs do Google (`google-genai`) | A opção de endereço base (`http_options.base_url` / `httpOptions.baseUrl`) com `http://127.0.0.1:8787/gemini` | **Não conferido** com uma SDK rodando |
| `curl` | `curl http://127.0.0.1:8787/gemini/v1beta/models/MODELO:generateContent -H "x-goog-api-key: $CHAVE" ...` | API de verdade, com e sem streaming e com chamada de ferramenta |

"Rodando, API de verdade" quer dizer: o próprio cliente, apontado como na tabela, fez uma
tarefa em que o agente leu um arquivo com um e-mail e um IP e escreveu outro arquivo. A API só
recebeu os pseudônimos e o arquivo saiu com os valores reais.

Sobre o Copilot:

- **Só o Copilot com a sua chave passa pelo llm-dlp** (o provedor próprio do Copilot CLI, ou o
  "Custom Endpoint" do VS Code). Os modelos que o GitHub hospeda e as sugestões enquanto você
  digita falam com os servidores do GitHub, não podem ser apontados para outro lugar e **não
  ficam protegidos**.
- O Copilot CLI foi conferido com um modelo do Gemini, pelo endereço do Gemini compatível com
  a OpenAI. Os dois não funcionam juntos sozinhos: o Gemini recusa o `"refusal": null` que o
  Copilot CLI devolve da segunda mensagem em diante, com ou sem o llm-dlp. No teste, um
  repassador depois do llm-dlp tirou esse campo. Com as APIs da OpenAI e da xAI, o formato
  está coberto pelos testes automáticos (contra uma API falsa), não por uma execução contra a
  API de verdade.
- Só o Chat Completions tem leitor. A API Responses (`/v1/responses`) cai no modo genérico
  abaixo.

### O que fica de fora

- **As instruções do próprio cliente também são mascaradas.** Um nome com cara de dado dentro
  das instruções que o cliente manda (a tabela de um exemplo de SQL, a pasta de um caminho de
  exemplo) vira pseudônimo. Funciona, porque a resposta é traduzida de volta, mas o modelo lê
  `t_x7k2...` onde o cliente escreveu o nome. O texto de identificação do Claude Code é o único
  que fica intacto.
- Áudio, vídeo e outros arquivos anexados que não são imagem nem PDF são retirados, com uma
  nota no lugar: o llm-dlp não tem como verificá-los.
- O resumo do raciocínio do modelo (`thought`, `reasoning_content`) volta com os pseudônimos,
  sem desmascarar.
- **Modo genérico.** Uma requisição que nenhum leitor entende (outro `formato`, a API
  Responses) tem todo texto mascarado, e a resposta em streaming volta com os pseudônimos. Nada
  vaza, mas o cliente recebe pseudônimos: não use um agente assim.

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
