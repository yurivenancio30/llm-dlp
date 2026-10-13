# Configuração e comandos

[← voltar ao README](README.md) · [English](../configuration.md)

## Configuração

Arquivo `~/.config/llm-dlp/config.json`. Depois de mudar, rode `llm-dlp recarregar`: ele aplica
a mudança sem tirar o llm-dlp do ar.

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
> **Beta.** Tudo nesta seção (os formatos do Gemini, do Code Assist e da OpenAI, e os clientes
> Gemini CLI, Codex, opencode e Copilot) está em beta. Só o Claude Code é estável. Os clientes em beta **não têm
> trava**: se o cliente deixar de apontar para o llm-dlp, ele fala direto com a API, sem
> máscara, e nada avisa. Leia [o que é beta](#o-que-é-beta) antes de usá-los com dado real.

O llm-dlp foi feito em torno do Claude Code, e o `llm-dlp instalar` configura **só o Claude
Code**. Outros clientes funcionam com o mesmo mascaramento, mas você os aponta à mão e eles
não têm trava.

| Cliente | Status | Instalado e travado pelo `llm-dlp instalar` |
|---|---|---|
| Claude Code, com assinatura (Pro, Max) ou chave de API | **Estável** | Sim |
| Gemini CLI (terminal, também dentro do VS Code), com conta Google ou chave de API do Gemini | Beta | Não |
| Codex (CLI e extensão do VS Code), com assinatura do ChatGPT ou chave de API da OpenAI | Beta | Não |
| opencode (terminal e VS Code) | Beta | Não |
| GitHub Copilot (CLI e VS Code), com a sua chave | Beta | Não |
| Qualquer outro cliente da API da Anthropic, do Gemini ou da OpenAI (Chat Completions, Responses) | Beta | Não |
| GitHub Copilot com a assinatura do GitHub (os modelos que o GitHub hospeda), a extensão Gemini Code Assist (VS Code, JetBrains), Vertex AI | **Não atendido** | Não deixam trocar o endereço com que falam |

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

| | Claude Code (estável) | Gemini CLI, Codex, opencode, Copilot e outros clientes (beta) |
|---|---|---|
| O que sai é mascarado e o que volta é desmascarado (texto, chamadas e resultados de ferramenta, arquivos que o agente escreve, imagens e PDFs) | Sim | Sim, o mesmo motor |
| `llm-dlp ultima` e o log | Sim | Sim |
| Apontado para o llm-dlp pelo `llm-dlp instalar` | Sim (passo 4) | **Não.** Você põe o endereço onde o cliente o lê (`~/.bashrc`, `~/.codex/config.toml`, `opencode.json`), como nas tabelas abaixo |
| O llm-dlp sobe sozinho | Sim, ao abrir uma sessão | **Não.** Uma função no `~/.bashrc` roda `llm-dlp garantir` antes de abrir o cliente ([README](README.md#gemini-cli-codex-opencode-e-copilot-beta)). Sem ela, se o llm-dlp não estiver no ar, o cliente recebe "conexão recusada" e nada sai |
| Tipos de conta que passam pelo llm-dlp | Assinatura (Pro, Max) e chave de API: o Claude Code fala sempre com a API da Anthropic | Os apontados em [Cliente por cliente](#cliente-por-cliente): o Gemini CLI com conta Google ou chave de API, o Codex com assinatura do ChatGPT ou chave de API. **Não** o Vertex AI, a extensão Gemini Code Assist nem o Copilot com os modelos que o GitHub hospeda: eles falam com outros servidores e saem **sem máscara** |
| Conferência a cada mensagem (o llm-dlp está no ar, é da versão certa e prova que é ele) | Sim | **Não** |
| Trava: o cliente se recusa a funcionar fora do llm-dlp | Sim (passo 5) | **Não.** Se o endereço for tirado da configuração do cliente, ele fala direto com a API, sem máscara, e nada avisa |

O passo a passo, com as linhas de cada arquivo, está no
[README](README.md#gemini-cli-codex-opencode-e-copilot-beta).

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
| Gemini com conta Google (Code Assist, o *Login with Google* do Gemini CLI) | `http://127.0.0.1:8787/codeassist` | O mesmo, dentro do envelope do Code Assist. Os métodos da conta (login, cota, experimentos) não levam conversa e passam como vieram |
| Assinatura do ChatGPT (Codex) | `http://127.0.0.1:8787/chatgpt` | Responses |
| Gemini, endereço compatível com a OpenAI | `http://127.0.0.1:8787/gemini/v1beta/openai` | Chat Completions |
| OpenAI | `http://127.0.0.1:8787/openai/v1` | Chat Completions e Responses |
| xAI (Grok) | `http://127.0.0.1:8787/xai/v1` | Chat Completions |
| Outra (OpenRouter, um gateway da empresa) | acrescente em `provedores`, com o `upstream` dela | `formato`: `openai`, `responses`, `gemini`, `codeassist` ou `anthropic` |

Um caminho terminado em `/chat/completions` é sempre lido como Chat Completions, e um terminado
em `/responses`, como Responses, seja qual for o provedor.

### Cliente por cliente

| Cliente | Como apontar | Como foi conferido |
|---|---|---|
| Gemini CLI, com chave de API | `export GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:8787/gemini` e `GEMINI_API_KEY`, com a chave escolhida no `~/.gemini/settings.json` (`"security": {"auth": {"selectedType": "gemini-api-key"}}`). Sem essa escolha ele para com "Invalid auth method selected" | Rodando (0.63.0), API de verdade, seguindo o README numa pasta pessoal limpa |
| Gemini CLI, com conta Google (*Login with Google*) | `export CODE_ASSIST_ENDPOINT=http://127.0.0.1:8787/codeassist` | **Ainda não conferido com uma conta de verdade.** O formato segue o código-fonte do Gemini CLI e está coberto pelos testes automáticos |
| Codex CLI, com assinatura do ChatGPT | No `~/.codex/config.toml`: `model_provider = "llm-dlp"` e `[model_providers.llm-dlp]` com `base_url = "http://127.0.0.1:8787/chatgpt"`, `wire_api = "responses"`, `requires_openai_auth = true` | **Ainda não conferido com uma conta de verdade.** O formato segue o código-fonte do Codex e está coberto pelos testes automáticos |
| Codex CLI, com chave de API da OpenAI | O mesmo provedor, com `base_url = "http://127.0.0.1:8787/openai/v1"` e `env_key = "OPENAI_API_KEY"` (no lugar de `requires_openai_auth`) | **Ainda não conferido com uma chave de verdade** (só testes automáticos) |
| Extensão do Codex para o VS Code | Lê o mesmo `~/.codex/config.toml` do CLI. Não passa pela função do `~/.bashrc`: rode `llm-dlp garantir` uma vez depois de ligar a máquina | **Não conferido.** A documentação da OpenAI diz que o CLI e a extensão dividem a configuração |
| Gemini CLI dentro do VS Code (terminal integrado, *Gemini CLI Companion*) | As mesmas linhas do Gemini CLI: é o mesmo programa, no terminal do editor | Como o Gemini CLI |
| opencode (provedor `google`) | No `opencode.json`: `"provider": {"google": {"options": {"baseURL": "http://127.0.0.1:8787/gemini/v1beta"}}}`, e a chave em `GOOGLE_GENERATIVE_AI_API_KEY` | Rodando (1.19.0), API de verdade, seguindo o README numa pasta pessoal limpa |
| GitHub Copilot CLI, com a sua chave | `export COPILOT_PROVIDER_BASE_URL=http://127.0.0.1:8787/openai/v1` (ou `/xai/v1`, ou outro endereço da tabela acima), mais `COPILOT_PROVIDER_API_KEY` e `COPILOT_MODEL` | Rodando (1.0.95), API de verdade, com a ressalva abaixo |
| GitHub Copilot no VS Code, com a sua chave | "Chat: Manage Language Models" > "Custom Endpoint". No `url` do modelo: `http://127.0.0.1:8787/openai/v1/chat/completions` (tipo de API Chat Completions) ou `http://127.0.0.1:8787/v1/messages` (tipo de API Messages). Como na extensão do Codex, suba o llm-dlp com `llm-dlp garantir` | **Não conferido.** Segue a documentação do VS Code; o formato é o mesmo do Copilot CLI |
| SDKs do Google (`google-genai`) | A opção de endereço base (`http_options.base_url` / `httpOptions.baseUrl`) com `http://127.0.0.1:8787/gemini` | **Não conferido** com uma SDK rodando |
| `curl` | `curl http://127.0.0.1:8787/gemini/v1beta/models/MODELO:generateContent -H "x-goog-api-key: $CHAVE" ...` | API de verdade, com e sem streaming e com chamada de ferramenta |

"Rodando, API de verdade" quer dizer: o próprio cliente, apontado como na tabela, fez uma
tarefa em que o agente leu um arquivo com um e-mail e um IP e escreveu outro arquivo. A API só
recebeu os pseudônimos e o arquivo saiu com os valores reais.

Sobre o Copilot:

- **Só o Copilot com a sua chave passa pelo llm-dlp** (o provedor próprio do Copilot CLI, ou o
  "Custom Endpoint" do VS Code). Os modelos que o GitHub hospeda e as sugestões enquanto você
  digita falam com os servidores do GitHub, não podem ser apontados para outro lugar e **não
  ficam protegidos**. Cobri-los exigiria o llm-dlp interceptar a conexão HTTPS (uma autoridade
  certificadora local, confiada pela máquina), que é o que os gateways corporativos fazem para o
  Copilot. Isso não é feito: é uma decisão à parte, com riscos próprios.
- O Copilot CLI foi conferido com um modelo do Gemini, pelo endereço do Gemini compatível com
  a OpenAI. Os dois não funcionam juntos sozinhos: o Gemini recusa o `"refusal": null` que o
  Copilot CLI devolve da segunda mensagem em diante, com ou sem o llm-dlp. No teste, um
  repassador depois do llm-dlp tirou esse campo. Com as APIs da OpenAI e da xAI, o formato
  está coberto pelos testes automáticos (contra uma API falsa), não por uma execução contra a
  API de verdade.
- O Chat Completions e o Responses têm leitor. Outras APIs da OpenAI (Assistants, Realtime) caem
  no modo genérico abaixo.

### O que fica de fora

- **As instruções do próprio cliente também são mascaradas.** Um nome com cara de dado dentro
  das instruções que o cliente manda (a tabela de um exemplo de SQL, a pasta de um caminho de
  exemplo) vira pseudônimo. Funciona, porque a resposta é traduzida de volta, mas o modelo lê
  `t_x7k2...` onde o cliente escreveu o nome. O texto de identificação do Claude Code é o único
  que fica intacto.
- Áudio, vídeo e outros arquivos anexados que não são imagem nem PDF são retirados, com uma
  nota no lugar: o llm-dlp não tem como verificá-los.
- O resumo do raciocínio do modelo (`thought`, `reasoning_content`, os itens `reasoning` do
  Responses) volta com os pseudônimos, sem desmascarar.
- **Modo genérico.** Uma requisição que nenhum leitor entende (outro `formato`, outra API da
  OpenAI) tem todo texto mascarado, e a resposta em streaming volta com os pseudônimos. Nada
  vaza, mas o cliente recebe pseudônimos: não use um agente assim.

## Comandos

| Comando | O que faz |
|---|---|
| `llm-dlp instalar` | Instala e configura tudo, explicando cada passo |
| `sudo llm-dlp instalar-trava` | O Claude Code só funciona passando pelo llm-dlp |
| `llm-dlp desinstalar` / `sudo llm-dlp desinstalar-trava` | Desfaz |
| `llm-dlp atualizar [--versao X.Y.Z] [--arquivo ARQ.tar.gz]` | Baixa a versão mais nova (ou a pedida), confere e põe no lugar da que está no ar, sem tirar o llm-dlp do ar (ver [Atualizar o llm-dlp](#atualizar-o-llm-dlp)) |
| `llm-dlp recarregar` | Põe no ar de novo o binário instalado, com a configuração como está agora, sem tirar o llm-dlp do ar |
| `llm-dlp status` | Mostra se está no ar, em que modo e qual commit. Avisa se há um binário novo instalado que ainda não está no ar, se falta o OCR e se a configuração reduz a proteção |
| `llm-dlp parar` | Para o proxy (ele volta na próxima mensagem). Para atualizar ou aplicar a configuração, use `atualizar` ou `recarregar`, que não deixam a porta vazia |
| `sudo llm-dlp emergencia [30m\|sair]` | Libera o Claude sem máscara por tempo limitado |
| `llm-dlp importar-pessoas ARQ.csv --grupo COD:NOME:EMAIL` | Ensina pessoas |
| `llm-dlp colunas ARQ` | Mostra como os campos de um arquivo são entendidos |
| `llm-dlp testar < arquivo` | Mostra a versão mascarada de um texto |
| `llm-dlp testar-midia ARQ DIR` | Processa uma imagem ou PDF e grava o resultado em DIR |
| `llm-dlp medir ARQ.jsonl` | Tempo e cobertura sobre uma sessão antiga do Claude Code (só contagens) |
| `llm-dlp versao` | Versão e commit do binário (também `--version`) |
| `llm-dlp ajuda` | A lista de comandos (também `--help`) |

`garantir`, `verificar`, `servir` e `supervisionar` são internos: quem chama são os ganchos
do Claude Code e o supervisor. O `garantir` (sobe o llm-dlp se ele não estiver rodando) é
também o da função do `~/.bashrc` dos clientes em beta.

## Atualizar o llm-dlp

```bash
llm-dlp atualizar                   # a release mais nova
llm-dlp atualizar --versao 0.7.1    # uma versão dada (também para voltar a uma anterior)
llm-dlp atualizar --arquivo llm-dlp_0.7.1_linux_amd64.tar.gz   # um pacote já baixado
```

O que ele faz, nesta ordem:

1. Baixa o pacote desta máquina e confere o SHA-256 com o `checksums.txt` publicado com a
   release (com `--arquivo`, usa o pacote que você deu).
2. Põe o binário novo em `~/.local/bin/llm-dlp` e guarda o anterior ao lado
   (`llm-dlp.bak-DATA`; só fica a cópia mais recente).
3. Pede a troca ao supervisor. Quem segura a porta é o supervisor (o proxy só a recebe
   emprestada, como no socket activation do systemd): ele sobe a versão nova na mesma porta
   enquanto a antiga ainda atende, e espera a nova avisar que está pronta.
4. Manda a antiga sair: ela para de aceitar conexões, ainda atende o que chegar pelas conexões
   já abertas (avisando o cliente para abrir a próxima na versão nova), termina as respostas em
   andamento e sai. O supervisor então se reexecuta com o binário novo, sem soltar a porta.
5. Confere que a versão nova responde, com o commit dela e a prova da chave.

A porta nunca fica vazia. Isso importa porque o Claude Code manda requisições no meio de um
turno (depois de uma ferramenta) sem passar pelo gancho que sobe o llm-dlp, e uma porta vazia
nessa hora é "Connection refused". Se a versão nova não ficar pronta em 20 s, o supervisor a
para, o comando põe o binário anterior de volta e avisa; a versão antiga nunca saiu do ar.

O mesmo vale quando o proxy cai: a porta continua aberta com o supervisor, e as requisições
esperam na fila até o proxy seguinte (cerca de um segundo), em vez de serem recusadas.

O comando de instalação (`curl ... | sh`) faz o mesmo quando o llm-dlp já está instalado em
`~/.local/bin`. Compilando do código, `make instalar` instala o binário e roda
`llm-dlp recarregar`, que faz os passos 3 a 5 com o binário instalado (é também o que aplica
uma mudança no `config.json`).

**Da 0.7.0 ou anterior** (sem `llm-dlp atualizar`): rode o comando de instalação uma vez.
Nessas versões o próprio proxy segura a porta: o supervisor novo pega a porta no instante em
que o antigo sai (alguns milissegundos), mas uma resposta que estava sendo escrita é cortada
(o Claude Code repete). Faça entre uma mensagem e outra.

Depois de atualizar, a primeira mensagem de cada conversa regrava o cache uma vez (o
registro do que já saiu recomeça a cada versão).
