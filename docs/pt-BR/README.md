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

**2. O modelo responde com os pseudônimos, e o llm-dlp troca de volta antes de o agente
agir.** O script gravado no seu disco, o comando que roda e a resposta que você lê têm os
valores reais:

```bash
pg_dump -h hkdzp7s4a.invalid -U usr_xyxhy7ug -d db_nr3b5hpi -f backup.sql   # o que o modelo escreveu
pg_dump -h pg-prd.acme.intra -U app_crm -d crm_clientes -f backup.sql       # o que chega à sua máquina
```

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
| Gemini CLI (terminal, também dentro do VS Code), com conta Google ou chave de API | Beta | À mão, sem trava ([como](#gemini-cli-codex-opencode-e-copilot-beta)) |
| Codex (CLI e extensão do VS Code), com assinatura do ChatGPT ou chave de API | Beta | À mão, sem trava |
| opencode (terminal e VS Code), com o provedor `google` | Beta | À mão, sem trava |
| GitHub Copilot (CLI e VS Code), com a sua chave | Beta | À mão, sem trava |
| Copilot com a assinatura do GitHub, a extensão Gemini Code Assist, Vertex AI | **Não atendido** | Não deixam trocar o endereço com que falam: saem **sem máscara** |

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

O `llm-dlp instalar` não mexe nesses clientes. Para cada um, você põe o endereço do llm-dlp
onde o cliente o lê, e uma função no `~/.bashrc` que sobe o llm-dlp antes do cliente (só o
Claude Code o sobe sozinho; sem ela, depois de reiniciar a máquina o cliente recebe "conexão
recusada": nada vaza, mas não funciona).

| Cliente e conta | Endereço do llm-dlp |
|---|---|
| Gemini CLI, conta Google | `export CODE_ASSIST_ENDPOINT=http://127.0.0.1:8787/codeassist` |
| Gemini CLI, chave de API | `export GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:8787/gemini`, com a chave em `GEMINI_API_KEY` e *Use Gemini API Key* escolhido quando ele abrir |
| Codex, assinatura do ChatGPT ou chave de API | O provedor abaixo, no `~/.codex/config.toml` |
| opencode, chave de API do Gemini | No `opencode.json`: `{"provider": {"google": {"options": {"baseURL": "http://127.0.0.1:8787/gemini/v1beta"}}}}`, com a chave em `GOOGLE_GENERATIVE_AI_API_KEY` |
| Copilot CLI, a sua chave | `export COPILOT_PROVIDER_BASE_URL=http://127.0.0.1:8787/openai/v1` (xAI: `.../xai/v1`), com `COPILOT_PROVIDER_API_KEY` e `COPILOT_MODEL` |
| Copilot no VS Code, a sua chave | "Custom Endpoint", como em [Cliente por cliente](configuracao.md#cliente-por-cliente) |

```bash
# ~/.bashrc: o export da tabela, e uma função por cliente (gemini, codex, opencode, copilot)
gemini() { llm-dlp garantir && command gemini "$@"; }
```

```toml
# ~/.codex/config.toml: o Codex CLI e a extensão do VS Code o leem; o model_provider vai no topo
model_provider = "llm-dlp"

[model_providers.llm-dlp]
name = "llm-dlp"
wire_api = "responses"
base_url = "http://127.0.0.1:8787/chatgpt"    # chave de API: "http://127.0.0.1:8787/openai/v1"
requires_openai_auth = true                   # chave de API: env_key = "OPENAI_API_KEY"
```

**No VS Code**, o terminal integrado funciona como qualquer outro. Uma extensão (Codex,
Copilot) não passa pela função: rode `llm-dlp garantir` uma vez depois de ligar a máquina, e
dali em diante o llm-dlp volta sozinho.

**Confira:** mande uma mensagem e rode `llm-dlp ultima`. Ele tem de mostrar essa mensagem, com
os pseudônimos; se disser *"Ainda não passou nenhuma mensagem"*, o cliente não está passando
pelo llm-dlp. Só passa o que foi apontado: outro provedor ou outra conta fala direto com a API.
O que foi conferido em cada cliente está em [Cliente por cliente](configuracao.md#cliente-por-cliente).

### Atualizar para uma versão nova

```bash
llm-dlp atualizar
```

Ele baixa a release mais nova, confere com o `checksums.txt` da release e põe a versão nova no
lugar da que está no ar **sem tirar o llm-dlp do ar**: não precisa fechar o cliente, e nenhuma
mensagem falha nem é cortada. Se a versão nova não subir, ele volta sozinho para a anterior. O
comando de instalação de cima faz o mesmo quando o llm-dlp já está instalado. A sua
configuração e a sua chave não mudam. Como funciona:
[Atualizar o llm-dlp](configuracao.md#atualizar-o-llm-dlp).

Da 0.7.0 ou anterior, que não têm o `llm-dlp atualizar`, rode o comando de instalação de cima
uma vez, entre uma mensagem e outra: essas versões não sabem passar a porta, e uma resposta que
estiver sendo escrita é cortada (o Claude Code repete). Daí em diante, atualizar não corta nada.

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
| A mensagem foi barrada: "não provou ser o seu llm-dlp" | Um processo de uma versão até a 0.3.0 ainda está no ar: feche o Claude Code e rode `llm-dlp parar`. Se não, outro programa está usando a porta |
| Mudei o `config.json` | `llm-dlp recarregar` (aplica sem tirar o llm-dlp do ar) |
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
