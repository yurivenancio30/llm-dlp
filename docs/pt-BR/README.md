# llm-dlp

[![ci](https://github.com/yurivenancio30/llm-dlp/actions/workflows/ci.yml/badge.svg)](https://github.com/yurivenancio30/llm-dlp/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/yurivenancio30/llm-dlp)](https://github.com/yurivenancio30/llm-dlp/releases/latest)
[![license](https://img.shields.io/github/license/yurivenancio30/llm-dlp)](../../LICENSE)

[English](../../README.md) · **Português**

Proxy local que **mascara dado sensível entre você e a LLM**. A API do modelo só vê
pseudônimos; você, o seu terminal e os arquivos que o agente escreve veem os valores reais.

Feito para o **Claude Code** (estável), com assinatura (Pro, Max) ou chave de API. O Gemini
CLI, o Codex, o opencode e o GitHub Copilot com a sua chave também funcionam, **em beta** (ver
[Clientes atendidos](#clientes-atendidos)).

```
          sua máquina                                             │ internet
 você ──► Claude Code (ou outro cliente) ──► llm-dlp ── mascara ──►│──► API do modelo
                                                                  │    (só vê pseudônimos)
 você ◄── valores reais ◄───────────────── llm-dlp ◄─ desmascara ─│◄── responde com pseudônimos
```

## Um exemplo, passo a passo

Você pede ao agente: *"faça um script de backup do banco de clientes; a conexão está no
`.env`"*.

**1. O agente lê o `.env`.** Este é o arquivo no seu disco:

```bash
DB_HOST=pg-prd.acme.intra
DB_NAME=crm_clientes
DB_USER=app_crm
DB_PASSWORD=Pr0dS3nha2024
SUPPORT_EMAIL=maria.lopes@acme.com.br
```

E isto é o que sai da sua máquina, a única coisa que a API do modelo vê:

```bash
DB_HOST=hkdzp7s4a.invalid
DB_NAME=db_nr3b5hpi
DB_USER=usr_xyxhy7ug
DB_PASSWORD=SEGREDO-vqxz5dvy
SUPPORT_EMAIL=p.ouhiojea@ds373.invalid
```

**2. O modelo responde com os pseudônimos**, porque é só o que ele viu:

```bash
pg_dump -h hkdzp7s4a.invalid -U usr_xyxhy7ug -d db_nr3b5hpi -f backup.sql
```

**3. O llm-dlp troca de volta antes de o agente agir.** O script gravado no seu disco, e o
comando que roda, têm os valores reais:

```bash
pg_dump -h pg-prd.acme.intra -U app_crm -d crm_clientes -f backup.sql
```

A resposta que você lê também volta real: *"o backup.sh faz o dump de crm_clientes em
pg-prd.acme.intra"*.

| Valor real | Pseudônimo | O modelo continua sabendo que é |
|---|---|---|
| `pg-prd.acme.intra` | `hkdzp7s4a.invalid` | um servidor (`.invalid` é um domínio que nunca existe: nada pode ser enviado para ele) |
| `crm_clientes` | `db_nr3b5hpi` | um banco |
| `app_crm` | `usr_xyxhy7ug` | um usuário |
| `Pr0dS3nha2024` | `SEGREDO-vqxz5dvy` | uma senha |
| `maria.lopes@acme.com.br` | `p.ouhiojea@ds373.invalid` | um e-mail |
| `529.982.247-25` | `CPF-5qwopr5x` | um CPF |

O mesmo valor vira sempre o mesmo pseudônimo, então o modelo continua entendendo que duas
menções são o mesmo servidor. Os pseudônimos vêm de uma chave secreta que só existe na sua
máquina. `pg_dump`, `postgresql` e `5432` ficam como estão: nome público não é mascarado.

## O que ele faz

- **Mascara tudo que vai para a API:** o que você digita, arquivos lidos, saída de comandos,
  dados de MCP, subagentes, anexos e até o título da sessão.
- **Mascara três tipos de dado:**
  - dado pessoal (CPF, CNPJ, e-mail, telefone, nome de pessoa, endereço, cartão);
  - segredo (senha, token, chave de API);
  - nome que identifica a empresa ou o cliente: servidor, banco, schema, tabela, coluna,
    serviço, bucket, fila, usuário, pasta, função num traceback. São reconhecidos
    pela estrutura do texto (SQL, YAML, JSON, `.env`, strings de conexão, Kubernetes,
    Terraform, código, saída de comando), sem lista por cliente.
- **Deixa legível o que é público:** `postgres`, `redis`, `my-bucket`, `org.apache.kafka`, a
  linha do `pandas` num traceback. Um nome só fica em claro com prova de que é público; na
  dúvida, é mascarado.
- **Desmascara tudo que volta:** você lê os valores reais; comandos rodam e arquivos são
  gravados com os valores reais.
- **Lê imagens e PDFs** (OCR) e cobre de preto o que for sensível.
- **Falha fechada:** se o llm-dlp cair ou não souber tratar algo, a mensagem **não sai**. Não
  existe vazamento silencioso.
- **Não pesa:** uma mensagem custa 5–30 ms, quase não muda o número de tokens e não atrapalha
  o cache da API.
- **Não exige nada no dia a dia** (com o Claude Code): sobe sozinho com a sessão e volta
  sozinho se cair.

## Clientes atendidos

| Cliente | Status | Como é ligado |
|---|---|---|
| Claude Code (terminal, VS Code, JetBrains), com assinatura (Pro, Max) ou chave de API | **Estável** | Pelo `llm-dlp instalar`, com trava: o Claude Code se recusa a funcionar fora do llm-dlp |
| Gemini CLI, com conta Google ou chave de API do Gemini | Beta | À mão, no `~/.bashrc` ([como](#gemini-cli-codex-opencode-e-copilot-beta)). Sem trava |
| Codex CLI, com assinatura do ChatGPT ou chave de API da OpenAI | Beta | À mão, no `~/.codex/config.toml` e no `~/.bashrc` ([como](#gemini-cli-codex-opencode-e-copilot-beta)). Sem trava |
| opencode, com o provedor `google` | Beta | À mão, no `opencode.json` e no `~/.bashrc` ([como](#gemini-cli-codex-opencode-e-copilot-beta)). Sem trava |
| GitHub Copilot CLI e VS Code, com a sua chave | Beta | À mão, no `~/.bashrc` ([como](#gemini-cli-codex-opencode-e-copilot-beta)). Sem trava |
| GitHub Copilot com a assinatura do GitHub | **Não atendido** | O Copilot não deixa trocar o endereço dos modelos que o GitHub hospeda |

> [!WARNING]
> **Os clientes em beta não têm trava.** A proteção vale enquanto o endereço do cliente
> apontar para o llm-dlp: se deixar de apontar (uma linha apagada, outro tipo de conta, outro
> provedor), o cliente fala direto com a API, sem máscara, e nada avisa. Beta pode mudar numa
> versão MENOR. Detalhes: [Outros clientes e APIs (beta)](configuracao.md#outros-clientes-e-apis-beta).

## Instalação

Funciona em qualquer Linux, inclusive WSL no Windows. Não precisa clonar o repositório nem
instalar o Go.

### 1. Instale o llm-dlp (igual para todos os clientes)

```bash
curl -fsSL https://raw.githubusercontent.com/yurivenancio30/llm-dlp/main/install.sh | sh
```

O [script](../../install.sh) baixa o binário da versão mais recente para a sua máquina (amd64 ou
arm64), confere a soma SHA-256 com a publicada na release e roda o `llm-dlp instalar`.

<details>
<summary>Outras formas: ler o script antes, baixar à mão, uma versão específica, compilar do código</summary>

**Ler o script antes de rodar:**

```bash
curl -fsSLO https://raw.githubusercontent.com/yurivenancio30/llm-dlp/main/install.sh
less install.sh
sh install.sh
```

**Baixar à mão** pela página de [Releases](https://github.com/yurivenancio30/llm-dlp/releases)
(`llm-dlp_X.Y.Z_linux_amd64.tar.gz` ou `..._arm64.tar.gz`, e o `checksums.txt`):

```bash
sha256sum -c checksums.txt --ignore-missing     # tem de mostrar "OK"
tar -xzf llm-dlp_*_linux_*.tar.gz llm-dlp
./llm-dlp instalar
```

**Uma versão específica, ou só o binário** (sem rodar a configuração):

```bash
curl -fsSL https://raw.githubusercontent.com/yurivenancio30/llm-dlp/main/install.sh | sh -s -- --version 0.2.1
curl -fsSL https://raw.githubusercontent.com/yurivenancio30/llm-dlp/main/install.sh | sh -s -- --no-setup
```

**Compilar do código** (precisa do [Go](https://go.dev/dl/)). É o caminho no macOS, onde
compila mas ainda não foi testado por completo, e para quem vai mexer no código:

```bash
git clone https://github.com/yurivenancio30/llm-dlp.git && cd llm-dlp && make build
./bin/llm-dlp instalar
```

</details>

O `instalar` pergunta o que precisa, em 5 passos (responda `s` para sim, `n` para não):

| Passo | O que acontece | Você responde |
|---|---|---|
| 1. Programa | Copia o llm-dlp para `~/.local/bin` | Nada. Se ele avisar que `~/.local/bin` não está no seu PATH, acrescente `export PATH="$HOME/.local/bin:$PATH"` ao `~/.bashrc` |
| 2. Configuração | Cria `~/.config/llm-dlp` com a configuração e a chave secreta | O domínio interno, que aparece no nome dos servidores (ex.: `acme`, para `pg-prd.acme.intra`), e os nomes que nunca podem sair (empresa, cliente, projetos). E-mails são mascarados sempre |
| 3. OCR | Instala o tesseract e o poppler, se faltarem | Sim e a senha do `sudo` |
| 4. Claude Code | Liga o Claude Code ao llm-dlp (com backup do `~/.claude/settings.json`) | **Sim, se você usa o Claude Code; senão, não** |
| 5. Trava | Faz o Claude Code se recusar a funcionar fora do llm-dlp | **Sim, se você usa o Claude Code** (e a senha do `sudo`); pulado nos outros casos |

No fim, ele mostra um resumo. ✓ é feito, ✗ é o que falta (rode `llm-dlp instalar` de novo:
ele pula o que já está pronto) e – é o que você escolheu não configurar.

### 2. Ligue o seu cliente

#### Claude Code (estável)

Os passos 4 e 5 fizeram tudo. Feche e abra o Claude Code (no VS Code, recarregue a janela) e
confira com `llm-dlp status`.

#### Gemini CLI, Codex, opencode e Copilot (beta)

> [!WARNING]
> Beta, e sem trava: leia o aviso em [Clientes atendidos](#clientes-atendidos).

O `llm-dlp instalar` não mexe nesses clientes. Para cada um, você configura duas coisas:

- **o endereço do llm-dlp** no lugar do endereço da API do cliente, onde o cliente o lê
  (tabela abaixo);
- **uma função com o nome do cliente** no `~/.bashrc`, que sobe o llm-dlp (`llm-dlp garantir`)
  antes de abrir o cliente. Só o Claude Code sobe o llm-dlp sozinho: sem a função, depois de
  reiniciar a máquina o cliente recebe "conexão recusada". Nada vaza, mas não funciona.

| Cliente | Conta | Onde vai o endereço |
|---|---|---|
| Gemini CLI | Conta Google (*Login with Google*) | `CODE_ASSIST_ENDPOINT`, no `~/.bashrc` |
| Gemini CLI | Chave de API do Gemini | `GOOGLE_GEMINI_BASE_URL`, no `~/.bashrc` |
| Codex CLI | Assinatura do ChatGPT | Um provedor no `~/.codex/config.toml` |
| Codex CLI | Chave de API da OpenAI | Um provedor no `~/.codex/config.toml` |
| opencode | Chave de API do Gemini | `baseURL` do provedor `google`, no `opencode.json` |
| GitHub Copilot CLI | A sua chave da OpenAI ou da xAI | `COPILOT_PROVIDER_BASE_URL`, no `~/.bashrc` |

**Gemini CLI.** Cada tipo de conta fala com uma API diferente do Google, com uma linha
própria. O Vertex AI não é coberto: com ele, **a conversa sai sem máscara**.

Com a sua conta Google (*Login with Google*, o plano gratuito e o Google AI Pro):

```bash
# ~/.bashrc
export CODE_ASSIST_ENDPOINT=http://127.0.0.1:8787/codeassist
gemini() { llm-dlp garantir && command gemini "$@"; }
```

Com **chave de API** do Gemini (do Google AI Studio):

```bash
# ~/.bashrc
export GEMINI_API_KEY="sua-chave"
export GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:8787/gemini
gemini() { llm-dlp garantir && command gemini "$@"; }
```

Com a chave de API, escolha-a também no `~/.gemini/settings.json` (ou escolha *Use Gemini API
Key* na primeira vez que ele abrir):

```json
{ "security": { "auth": { "selectedType": "gemini-api-key" } } }
```

**Codex CLI.** O Codex lê o endereço de um provedor no `~/.codex/config.toml` (crie o arquivo
se ele não existir; o `model_provider` vai no topo, antes de qualquer `[seção]`).

Com a sua assinatura do ChatGPT (entre com `codex login`, como sempre):

```toml
# ~/.codex/config.toml
model_provider = "llm-dlp"

[model_providers.llm-dlp]
name = "llm-dlp"
base_url = "http://127.0.0.1:8787/chatgpt"
wire_api = "responses"
requires_openai_auth = true
```

Com **chave de API** da OpenAI:

```toml
# ~/.codex/config.toml
model_provider = "llm-dlp"

[model_providers.llm-dlp]
name = "llm-dlp"
base_url = "http://127.0.0.1:8787/openai/v1"
wire_api = "responses"
env_key = "OPENAI_API_KEY"
```

E, nos dois casos:

```bash
# ~/.bashrc
export OPENAI_API_KEY="sua-chave"     # só com a chave de API
codex() { llm-dlp garantir && command codex "$@"; }
```

Só o provedor `llm-dlp` passa pelo llm-dlp: se você trocar o Codex para outro provedor
(`--profile`, `-c model_provider=...`), ele fala direto com a API.

**opencode**, com o provedor `google`:

```bash
# ~/.bashrc
export GOOGLE_GENERATIVE_AI_API_KEY="sua-chave"
opencode() { llm-dlp garantir && command opencode "$@"; }
```

```json
// ~/.config/opencode/opencode.json
{ "provider": { "google": { "options": { "baseURL": "http://127.0.0.1:8787/gemini/v1beta" } } } }
```

Só o provedor apontado ali passa pelo llm-dlp. Se você trocar o opencode para outro provedor,
aponte esse também.

**GitHub Copilot CLI**, só com a sua chave (OpenAI ou xAI). Os modelos que o GitHub hospeda e
as sugestões enquanto você digita não passam pelo llm-dlp.

```bash
# ~/.bashrc
export COPILOT_PROVIDER_BASE_URL=http://127.0.0.1:8787/openai/v1    # xAI (Grok): .../xai/v1
export COPILOT_PROVIDER_API_KEY="sua-chave"
export COPILOT_MODEL="nome-do-modelo"
copilot() { llm-dlp garantir && command copilot "$@"; }
```

Copilot no VS Code e outros clientes: [Cliente por cliente](configuracao.md#cliente-por-cliente).

**Confira se está protegido.** Abra um terminal novo (ou rode `source ~/.bashrc`), mande uma
mensagem pelo cliente e rode `llm-dlp ultima`: ele tem de mostrar essa mensagem, com os
pseudônimos. Se disser *"Ainda não passou nenhuma mensagem"*, o cliente não está passando pelo
llm-dlp: confira o endereço.

As linhas do Gemini CLI com chave de API e do opencode foram seguidas como estão numa máquina
limpa (uma pasta pessoal nova): o agente leu um arquivo com um e-mail e um IP e escreveu outro;
a API só recebeu os pseudônimos e o arquivo saiu com os valores reais. O Gemini CLI com conta
Google e o Codex seguem o código-fonte dos clientes e os testes automáticos, e **ainda não
foram conferidos com uma conta de verdade**.

### Atualizar para uma versão nova

1. Rode de novo o comando de instalação de cima. Ele troca o programa e pula o que já está
   pronto; a sua configuração e a sua chave não mudam.
2. Feche o seu cliente (no Claude Code, todas as janelas) e rode `llm-dlp parar`. O que está
   no ar só é trocado quando o processo antigo para.
3. Abra o cliente de novo (num cliente em beta, a função sobe a versão nova) e confira com
   `llm-dlp status`.

O que mudou em cada versão está no [CHANGELOG.md](CHANGELOG.md). Depois de
atualizar, a primeira mensagem de cada conversa regrava o cache da API uma vez.

## No dia a dia

Com o Claude Code, não há nada para rodar: o llm-dlp sobe ao abrir uma sessão e volta sozinho
se cair. Num cliente em beta, a função do `~/.bashrc` o sobe quando você abre o cliente, e daí
em diante ele também volta sozinho.

| Situação | O que acontece / o que fazer |
|---|---|
| Quero ver se está tudo certo | `llm-dlp status` |
| Quero ver o que a API recebeu | `llm-dlp ultima` (as últimas mensagens, com os pseudônimos) |
| Quero saber qual versão tenho | `llm-dlp versao` (o que mudou em cada uma: [CHANGELOG.md](CHANGELOG.md)) |
| O llm-dlp caiu | Volta sozinho em ~1 s. Enquanto estiver fora, nenhuma mensagem sai |
| Ele não volta, e preciso do Claude para consertar | `sudo llm-dlp emergencia 30m` libera o Claude **sem máscara** por tempo limitado. Use uma sessão nova, sem dados de cliente. Volta ao normal no fim do prazo, ou com `sudo llm-dlp emergencia sair`. Cada mensagem mostra um aviso |
| A imagem ou o PDF não chegou ao modelo | Falta o OCR: o modelo avisa na resposta, e o `llm-dlp status` mostra o comando de instalação. Depois de instalar, mande o arquivo de novo |
| A mensagem foi barrada: "não provou ser o seu llm-dlp" | Você acabou de atualizar e o processo antigo ainda está no ar: feche o Claude Code e rode `llm-dlp parar`. Se não atualizou, outro programa está usando a porta |
| Mudei o `config.json` | `llm-dlp parar` (ele volta na próxima mensagem, com a configuração nova) |
| Quero ver o que foi feito em cada mensagem | `~/.config/llm-dlp/llm-dlp.log` (só contagens e tempos, nunca valores) |
| Quero desfazer tudo | `llm-dlp desinstalar` (e `sudo llm-dlp desinstalar-trava`, se instalou a trava). Depois, feche e abra o Claude Code e rode `llm-dlp parar`. Num cliente em beta, tire também as linhas dele do `~/.bashrc` (e do `opencode.json` ou do `~/.codex/config.toml`) |

## Mais detalhes

| Documento | O que tem |
|---|---|
| [Como funciona](como-funciona.md) | O caminho de uma mensagem, os pseudônimos, o que é lembrado, falha fechada, imagens, desempenho e quanto usa da máquina |
| [O que é detectado](deteccao.md) | Os jeitos de reconhecer um dado e como ensinar pessoas e nomes de campo da sua empresa |
| [Configuração e comandos](configuracao.md) | Todos os campos do `config.json` e todos os comandos |
| [Segurança e limites](seguranca.md) | O que protege, o que não protege e o que passa sem máscara |
| [Política](politica.md) | Os quatro níveis de informação, o que o llm-dlp faz com cada um, as referências (LGPD, MITRE ATT&CK, CWE, NIST) e o limite declarado sobre código e regras de negócio |
| [Para desenvolvedores](desenvolvimento.md) | Mapa do código, o que fica guardado em memória e em disco, como acoplar outra API de LLM, como acrescentar um detector, como foi testado |
| [Estruturas](estruturas.md) | A pesquisa por trás das regras de estrutura: onde ficam os nomes de recursos internos em cada formato, e as regras de quando um nome é público |
| [Versões e lançamentos](versoes.md) | O que cada número da versão significa, o que vem numa release e como lançar uma |
| [Mudanças](CHANGELOG.md) | O que mudou em cada versão |
| [Como contribuir](CONTRIBUTING.md) · [Política de segurança](SECURITY.md) | Como contribuir e como relatar uma vulnerabilidade |

## Licença

MIT. Ver [LICENSE](../../LICENSE).

Os arquivos de dados `internal/mask/dados/palavras_comuns.txt` e `palavras_dicionario.txt` são
adaptações das listas de frequência do [FrequencyWords](https://github.com/hermitdave/FrequencyWords),
de Hermit Dave (dados do OpenSubtitles 2018), distribuídas sob
[CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/); origem e critério no cabeçalho
de cada arquivo.
