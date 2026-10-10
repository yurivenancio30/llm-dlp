# llm-dlp

[![ci](https://github.com/yurivenancio30/llm-dlp/actions/workflows/ci.yml/badge.svg)](https://github.com/yurivenancio30/llm-dlp/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/yurivenancio30/llm-dlp)](https://github.com/yurivenancio30/llm-dlp/releases/latest)
[![license](https://img.shields.io/github/license/yurivenancio30/llm-dlp)](LICENSE)

[English](README.md) · **Português**

Proxy local que **mascara dado sensível entre você e a LLM**. Hoje funciona com o Claude Code.

Você e o seu disco veem tudo real. A API do modelo só vê pseudônimos.

```
você ─► Claude Code ─► [ llm-dlp ] ─► API da Anthropic
 real       real        mascara ►      só vê pseudônimos
 real   ◄── real    ◄── desmascara ◄── responde com pseudônimos
```

Exemplo do que acontece com uma mensagem:

```
você escreve:   o cliente de cpf 529.982.247-25 (maria.lopes@empresa.com.br) não acessa mysql.empresa.intra
a API recebe:   o cliente de cpf CPF-irvikcpk (p.k3f9x2ab@d7q2m.invalid) não acessa mysql-x7k2.invalid
a API responde: verifique se p.k3f9x2ab@d7q2m.invalid tem permissão em mysql-x7k2.invalid
você lê:        verifique se maria.lopes@empresa.com.br tem permissão em mysql.empresa.intra
```

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
- **Mantém a referência:** o mesmo valor vira sempre o mesmo pseudônimo, então o modelo
  continua entendendo que duas menções são da mesma pessoa, do mesmo servidor, da mesma conta.
- **Lê imagens e PDFs** (OCR) e cobre de preto o que for sensível.
- **Falha fechada:** se o llm-dlp cair ou não souber tratar algo, a mensagem **não sai**. Não
  existe vazamento silencioso.
- **Não pesa:** uma mensagem custa 5–30 ms, quase não muda o número de tokens e não atrapalha
  o cache da API.
- **Não exige nada no dia a dia:** sobe sozinho com a sessão e volta sozinho se cair.

## Instalação

Funciona em qualquer Linux, inclusive WSL no Windows. Não precisa clonar o repositório nem
instalar o Go:

```bash
curl -fsSL https://raw.githubusercontent.com/yurivenancio30/llm-dlp/main/install.sh | sh
```

O [script](install.sh) baixa o binário da versão mais recente para a sua máquina (amd64 ou
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

O `instalar` faz tudo, em 5 passos, perguntando o que precisa:

| Passo | O que acontece | Você responde |
|---|---|---|
| 1. Programa | Copia o llm-dlp para `~/.local/bin` | Nada |
| 2. Configuração | Cria `~/.config/llm-dlp` com a configuração e a chave secreta | O domínio interno, que aparece no nome dos servidores (ex.: `empresa`, para `mysql.empresa.intra`), e os nomes que nunca podem sair (empresa, cliente, projetos). E-mails são mascarados sempre |
| 3. OCR | Instala o tesseract e o poppler, se faltarem | Sim e a senha do `sudo` |
| 4. Claude Code | Liga o Claude Code ao llm-dlp (com backup do `~/.claude/settings.json`) | Sim |
| 5. Trava | Faz o Claude Code se recusar a funcionar fora do llm-dlp | Sim e a senha do `sudo` |

No fim, ele mostra um resumo com ✓ e ✗. Se algo ficar com ✗, é só rodar
`llm-dlp instalar` de novo: ele pula o que já está pronto.

Depois, feche e abra o Claude Code (no VS Code, recarregue a janela).

### Atualizar para uma versão nova

1. Rode de novo o comando de instalação de cima. Ele troca o programa e pula o que já está
   pronto; a sua configuração e a sua chave não mudam.
2. Feche o Claude Code (todas as janelas) e rode `llm-dlp parar`. O que está no ar só é
   trocado quando o processo antigo para.
3. Abra o Claude Code e confira com `llm-dlp status`.

O que mudou em cada versão está no [CHANGELOG.md](docs/pt-BR/CHANGELOG.md). Depois de
atualizar, a primeira mensagem de cada conversa regrava o cache da API uma vez.

## No dia a dia

Não há nada para rodar. O llm-dlp sobe ao abrir uma sessão e volta sozinho se cair.

| Situação | O que acontece / o que fazer |
|---|---|
| Quero ver se está tudo certo | `llm-dlp status` |
| Quero saber qual versão tenho | `llm-dlp versao` (o que mudou em cada uma: [CHANGELOG.md](docs/pt-BR/CHANGELOG.md)) |
| O llm-dlp caiu | Volta sozinho em ~1 s. Enquanto estiver fora, nenhuma mensagem sai |
| Ele não volta, e preciso do Claude para consertar | `sudo llm-dlp emergencia 30m` libera o Claude **sem máscara** por tempo limitado. Use uma sessão nova, sem dados de cliente. Volta ao normal no fim do prazo, ou com `sudo llm-dlp emergencia sair`. Cada mensagem mostra um aviso |
| Imagem ou PDF bloqueado | Falta o OCR: a mensagem de erro traz o comando de instalação |
| Mudei o `config.json` | `llm-dlp parar` (ele volta na próxima mensagem, com a configuração nova) |
| Quero ver o que foi feito em cada mensagem | `~/.config/llm-dlp/llm-dlp.log` (só contagens e tempos, nunca valores) |
| Quero desfazer tudo | `llm-dlp desinstalar` (e `sudo llm-dlp desinstalar-trava`, se instalou a trava). Depois, feche e abra o Claude Code e rode `llm-dlp parar` |

## Mais detalhes

| Documento | O que tem |
|---|---|
| [Como funciona](docs/pt-BR/como-funciona.md) | O caminho de uma mensagem, os pseudônimos, o que é lembrado, falha fechada, imagens, desempenho e quanto usa da máquina |
| [O que é detectado](docs/pt-BR/deteccao.md) | Os jeitos de reconhecer um dado e como ensinar pessoas e nomes de campo da sua empresa |
| [Configuração e comandos](docs/pt-BR/configuracao.md) | Todos os campos do `config.json` e todos os comandos |
| [Segurança e limites](docs/pt-BR/seguranca.md) | O que protege, o que não protege e o que passa sem máscara |
| [Política](docs/pt-BR/politica.md) | Os quatro níveis de informação, o que o llm-dlp faz com cada um, as referências (LGPD, MITRE ATT&CK, CWE, NIST) e o limite declarado sobre código e regras de negócio |
| [Para desenvolvedores](docs/pt-BR/desenvolvimento.md) | Mapa do código, o que fica guardado em memória e em disco, como acoplar outra API de LLM, como acrescentar um detector, como foi testado |
| [Estruturas](docs/pt-BR/estruturas.md) | A pesquisa por trás das regras de estrutura: onde ficam os nomes de recursos internos em cada formato, e as regras de quando um nome é público |
| [Versões e lançamentos](docs/pt-BR/versoes.md) | O que cada número da versão significa, o que vem numa release e como lançar uma |
| [Mudanças](docs/pt-BR/CHANGELOG.md) | O que mudou em cada versão |
| [Como contribuir](docs/pt-BR/CONTRIBUTING.md) · [Política de segurança](docs/pt-BR/SECURITY.md) | Como contribuir e como relatar uma vulnerabilidade |

## Licença

MIT. Ver [LICENSE](LICENSE).

Os arquivos de dados `internal/mask/dados/palavras_comuns.txt` e `palavras_dicionario.txt` são
adaptações das listas de frequência do [FrequencyWords](https://github.com/hermitdave/FrequencyWords),
de Hermit Dave (dados do OpenSubtitles 2018), distribuídas sob
[CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/); origem e critério no cabeçalho
de cada arquivo.
