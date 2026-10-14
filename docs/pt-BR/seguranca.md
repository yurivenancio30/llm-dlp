# Segurança e limites

[← voltar ao README](README.md) · [English](../security.md)

Para relatar uma vulnerabilidade, veja [SECURITY.md](SECURITY.md).

**O que protege:**

- A API da LLM não recebe os valores detectados, só pseudônimos.
- Os pseudônimos não podem ser revertidos sem a chave.
- Nada sensível é gravado em disco pelo llm-dlp (só hashes).
- Se algo falhar, a mensagem não sai.

**Por que a trava e a emergência exigem `sudo`:** assim, nada que rode com o seu usuário,
inclusive o agente induzido por uma página maliciosa, consegue ligar a emergência ou tirar
a trava. Se o seu `sudo` não pede senha, essa barreira não existe.

**A configuração não tem essa barreira:** o `config.json` é do seu usuário, e um comando
rodado pelo agente consegue mudá-lo (desligar um detector, liberar imagens, apontar o
`upstream` para outro servidor, que passaria a receber as mensagens e a credencial da API).
As regras que o `llm-dlp install` põe no Claude Code proíbem o agente de ler, editar e
citar a pasta num comando, mas isso não é uma garantia. Por isso, enquanto a configuração
tirar proteção ou mandar as mensagens para fora da API da Anthropic, o Claude Code mostra
um aviso **a cada mensagem**, e o `llm-dlp status` também. Se você não mudou nada e o aviso
apareceu, confira o `config.json`.

**O que não protege:**

- Dado que ele não detecta (ver [Limites conhecidos](seguranca.md#limites-conhecidos)).
- `curl` ou outro programa chamado pelo agente, que leve o dado real para fora. A proteção
  de saída vale para `WebFetch`, `WebSearch` e os conectores do claude.ai.
- Um programa malicioso rodando com o seu próprio usuário: ele lê a chave, pode se passar
  pelo proxy e mudar a configuração (isso último gera o aviso a cada mensagem).
- O que não passa por ele: outra instalação do Claude Code (a nativa do Windows, por
  exemplo), outros clientes.

**Arquivos em `~/.config/llm-dlp`:**

| Arquivo | Conteúdo |
|---|---|
| `chave` | A chave secreta. Não compartilhe; guarde uma cópia |
| `config.json` | A configuração |
| `pessoas.json` | Hashes de nomes, e-mails e códigos de pessoas |
| `vistos.json` | Hashes de valores já mascarados |
| `enviados.log` | De cada texto já enviado: hashes, posições, tipos e pseudônimos, nunca valores |
| `llm-dlp.log` | Uma linha por requisição: contagens, tempos e os pseudônimos que voltaram sem virar o valor real, nunca valores. No máximo ~20 MB |

## A porta local

O llm-dlp escuta só em `127.0.0.1`, mas o navegador e os outros usuários da máquina também
chegam nesse endereço. O que cada um consegue:

| Quem tenta | O que acontece |
|---|---|
| Uma página aberta no navegador, que aponta o próprio domínio para `127.0.0.1` (DNS rebinding) ou manda a requisição de outro site | Recusada (403). Só é atendido quem chama pelo endereço local (`127.0.0.1`, `localhost`) e não é uma página da web. Nada vai para a API |
| Outro usuário da máquina querendo ler a sua última conversa (mascarada) | Não lê: ela só sai com uma prova que só quem lê a sua `chave` calcula. É o que o comando `llm-dlp last` faz |
| Outro programa que ocupa a porta enquanto o llm-dlp está fora do ar, para receber a conversa sem máscara e a credencial da API | A cada mensagem, o Claude Code pergunta à porta se é o llm-dlp e exige a resposta a um desafio novo, que só quem lê a `chave` calcula. Sem a prova, a mensagem é barrada |

Quem roda com o **seu** usuário lê a chave e passa pelas três barreiras (ver "O que não
protege", acima).

Quem segura a porta é o supervisor, não o proxy: quando o proxy cai ou é trocado
(`llm-dlp update`, `llm-dlp reload`), a porta continua aberta, e nenhum outro programa
consegue pegá-la no meio. A janela da última linha acima fica só para quando o próprio
supervisor não está no ar (antes da primeira sessão, ou depois de um `llm-dlp stop`).

Ao atualizar de uma versão até a 0.3.0, o processo antigo ainda no ar não sabe dar essa
prova, e as mensagens são barradas até você fechar o Claude Code e rodar `llm-dlp stop`.

## Limites conhecidos

**O que passa sem máscara:**

- Nome de pessoa que não é conhecido e aparece sem nome de campo (texto corrido, ou um
  pedaço de CSV lido sem a linha de cabeçalho).
- Campo com nome fora do vocabulário, até você acrescentar em `extra_fields`.
- **Nome de recurso interno (banco, schema, tabela, sistema) que só aparece em frases**, sem
  nunca ter passado por uma estrutura que o llm-dlp reconheça (ver
  [O que é detectado](deteccao.md#5-nomes-de-recursos-internos-pela-estrutura)). Para mascarar
  um nome sempre, use `terms`.
- Dado pessoal sensível (saúde, religião, etnia) em texto corrido. Só é detectado num campo
  com esse nome.
- Valores financeiros (saldo, renda, limite). O que os liga a uma pessoa (nome, CPF, conta)
  é mascarado.
- Senha só de letras (`senha: abacaxi`) ou só de números com até 5 dígitos, a não ser
  dentro de URL.
- Texto muito pequeno ou estilizado numa imagem, que o OCR não lê.

**Outros limites:**

- Com centenas de sub-redes diferentes na mesma conversa, duas podem ganhar a mesma sub-rede
  falsa. Os IPs delas continuam mascarados na ida, mas não são trocados de volta (você vê o
  IP falso), para não apontar um comando para o host errado.
- Depois de reiniciar, um valor lembrado só pelo hash é reconhecido quando aparece "solto"
  ou separado por `= & : / ? @ | # +`. Colado direto em letras, só depois de reaparecer uma
  vez solto.
- Valor aprendido depois não é aplicado ao que já saiu: se um valor passou em claro numa
  mensagem e só depois foi reconhecido, essa mensagem continua saindo em claro nos reenvios
  da mesma conversa (ela já tinha sido enviada assim). O congelamento é por ponto da
  conversa: o mesmo texto numa mensagem nova, ou noutra conversa, já sai mascarado.
- A exceção é uma conversa que começa **exatamente igual** a uma anterior: as mesmas
  instruções do Claude Code, as mesmas ferramentas e as mesmas mensagens até aquele texto
  (uma sessão retomada, por exemplo). Para o llm-dlp, isso é um reenvio, e o texto sai como saiu da primeira vez.
  O conteúdo é o mesmo que já tinha sido enviado; nada novo vaza, mas o valor aprendido
  depois também não é aplicado ali.
- Senha só de letras é detectada dentro de uma URL (`mysql://app:SENHA@host`), mas não depois
  de um rótulo (`senha: SENHA`, `DB_PASSWORD=SENHA`): ali, o detector exige um dígito ou um
  símbolo, para não confundir com código e texto comum.
- Imagens e PDFs: o resultado fica memorizado só enquanto o llm-dlp está no ar. Depois de
  reiniciar, são examinados de novo com o que se sabe na hora.
- Só o Claude Code é travado e conferido a cada mensagem. Um cliente em beta (Gemini CLI, Codex,
  opencode, Copilot) fica protegido enquanto o endereço dele apontar para o llm-dlp, e nada
  avisa se deixar de apontar.
- Os formatos da Anthropic, do Gemini (também dentro do Code Assist) e da OpenAI (Chat
  Completions e Responses) têm leitor próprio (ver
  [Outros clientes e APIs (beta)](configuracao.md#outros-clientes-e-apis-beta) e [Acoplar outra API de LLM](desenvolvimento.md#acoplar-outra-api-de-llm)).

## Decisões de projeto

Comportamentos que são escolhas, não defeitos:

| Comportamento | Por quê |
|---|---|
| Só IPs de rede interna são mascarados | IP público (de um serviço na internet) não identifica a empresa nem uma pessoa, e mascará-lo atrapalharia o diagnóstico de rede |
| Domínios públicos não são mascarados | `github.com`, `pypi.org`, nomes de arquivo (`config.py`) têm formato de domínio; mascará-los piora as respostas sem proteger nada. Só os `internal_domains` são mascarados |
| E-mail precisa de domínio com ponto | `joao@localhost`, `root@servidor` e `ssh usuario@host` não são tratados como e-mail (seriam falsos positivos em comandos). E-mail escrito por extenso (`joao [at] empresa`) também não é reconhecido |
| `WebFetch`, `WebSearch` e conectores do claude.ai recebem pseudônimos | O que vai para fora da máquina não pode levar o dado real. Efeito: pesquisar na web ou num conector algo mascarado não acha nada |
| O que vem da web não é lembrado | Uma senha de exemplo numa página não é segredo seu. É mascarada onde aparece, mas não vira segredo em todo lugar |
| Pseudônimo com colisão não volta ao real | Dois valores com o mesmo pseudônimo: a ida continua mascarada, mas a volta não é feita, para não trocar pelo valor errado. A ferramenta recebe o pseudônimo e pode falhar |
| Valor aprendido depois de já ter passado | Se um valor passou em claro numa mensagem e só depois foi reconhecido, ele é mascarado dali em diante, só em texto novo. A mensagem antiga continua saindo igual nos reenvios: ela já foi enviada assim, e mudá-la regravaria a conversa inteira no cache sem proteger nada. O modelo pode ligar o pseudônimo ao valor que viu antes |

## Conectores e MCP

| Onde o MCP roda | O que ele recebe | Situação |
|---|---|---|
| Na sua máquina (MCP local: banco, arquivos) | O valor **real**: a chamada é desmascarada antes de rodar | Correto: é o mesmo que um comando seu |
| Na sua máquina, mas falando com um serviço de terceiros (uma API na internet, outro LLM) | O valor **real**, que sai da sua máquina sem passar pelo llm-dlp | **Fora da proteção.** Só use esses MCPs com dados que podem sair |
| Do lado da Anthropic (conectores do claude.ai, `mcp_tool_use` e `server_tool_use`) | O **pseudônimo**: a chamada é feita pela API, que só conhece os pseudônimos | Uma consulta com valor mascarado falha ou não acha nada. O que esses conectores trazem já está do lado da Anthropic e não passa pelo llm-dlp |

