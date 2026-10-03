# llm-dlp

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

Funciona em qualquer Linux, inclusive WSL no Windows. O programa é um arquivo só, sem
dependências. No macOS compila, mas ainda não foi testado por completo.

**1. Obter o programa** (por enquanto, a partir do código; precisa do Go):

```bash
git clone git@github.com:yurivenancio30/llm-dlp.git && cd llm-dlp && make build
```

**2. Instalar.** Um comando, que explica e pede confirmação a cada passo:

```bash
./bin/llm-dlp instalar
```

| O que ele faz | Por quê |
|---|---|
| Copia o programa para `~/.local/bin` | Para ser chamado de qualquer pasta |
| Cria `~/.config/llm-dlp` com a configuração e a chave secreta | A chave gera os pseudônimos. Ele pergunta os seus domínios internos (ex.: `empresa`) |
| Confere o OCR (tesseract) | Se faltar, mostra o comando `sudo` certo para a sua distribuição. Sem OCR, imagens e PDFs ficam bloqueados |
| Liga o Claude Code ao llm-dlp | Mostra o que vai mudar no `~/.claude/settings.json` e só aplica se você confirmar (com backup) |

**3. Trava contra vazamento silencioso** (recomendada):

```bash
sudo llm-dlp instalar-trava
```

Sem a trava, se a configuração do passo 2 for apagada, o Claude Code volta a falar direto
com a API, sem máscara e sem avisar. Com ela, o Claude Code **se recusa a funcionar** fora
do llm-dlp. Precisa de `sudo` porque fica numa pasta do sistema, que o agente não consegue
alterar. Requer Claude Code 2.1.285 ou mais novo.

Depois, feche e abra o Claude Code.

## No dia a dia

Não há nada para rodar. O llm-dlp sobe ao abrir uma sessão e volta sozinho se cair.

| Situação | O que acontece / o que fazer |
|---|---|
| Quero ver se está tudo certo | `llm-dlp status` |
| O llm-dlp caiu | Volta sozinho em ~1 s. Enquanto estiver fora, nenhuma mensagem sai |
| Ele não volta, e preciso do Claude para consertar | `sudo llm-dlp emergencia 30m` libera o Claude **sem máscara** por tempo limitado. Use uma sessão nova, sem dados de cliente. Volta ao normal no fim do prazo, ou com `sudo llm-dlp emergencia sair`. Cada mensagem mostra um aviso |
| Imagem ou PDF bloqueado | Falta o OCR: a mensagem de erro traz o comando de instalação |
| Mudei o `config.json` | `llm-dlp parar` (ele volta na próxima mensagem, com a configuração nova) |
| Quero ver o que foi feito em cada mensagem | `~/.config/llm-dlp/llm-dlp.log` (só contagens e tempos, nunca valores) |
| Quero desfazer tudo | `llm-dlp desinstalar` (e `sudo llm-dlp desinstalar-trava`, se instalou a trava). Depois, feche e abra o Claude Code e rode `llm-dlp parar` |

## Mais detalhes

| Documento | O que tem |
|---|---|
| [Como funciona](docs/como-funciona.md) | O caminho de uma mensagem, os pseudônimos, o que é lembrado, falha fechada, imagens, desempenho |
| [O que é detectado](docs/deteccao.md) | Os quatro jeitos de reconhecer um dado e como ensinar pessoas e nomes de campo da sua empresa |
| [Configuração e comandos](docs/configuracao.md) | Todos os campos do `config.json` e todos os comandos |
| [Segurança e limites](docs/seguranca.md) | O que protege, o que não protege e o que passa sem máscara |
| [Para desenvolvedores](docs/desenvolvimento.md) | Mapa do código, como acoplar outra API de LLM, como acrescentar um detector, como foi testado |

## Licença

MIT. Ver [LICENSE](LICENSE).
