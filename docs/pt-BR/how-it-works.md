# Como funciona

[← voltar ao README](README.md) · [English](../how-it-works.md)

## O caminho de uma mensagem

1. O Claude Code é configurado para falar com `http://127.0.0.1:8787` (o llm-dlp) em vez de
   falar direto com a API. Funciona com assinatura ou com chave de API.
2. **Ida:** o llm-dlp recebe a requisição, procura dado sensível em cada texto e troca por
   pseudônimos. Guarda, só em memória e só para aquela requisição, a tabela
   "pseudônimo → valor real".
3. Envia a requisição mascarada para a API.
4. **Volta:** a resposta chega em pedaços (streaming). O llm-dlp troca os pseudônimos de
   volta usando a tabela, inclusive quando um pseudônimo chega cortado entre dois pedaços.
5. O Claude Code recebe tudo real: mostra para você, roda comandos e grava arquivos com os
   valores verdadeiros.

Como o Claude Code reenvia a conversa inteira a cada mensagem, o llm-dlp memoriza o
resultado de cada texto. Só o que é novo é examinado.

## Pseudônimos

| Valor real | O que a API vê |
|---|---|
| `joao.silva@empresa.com.br` | `p.mxvotcyt@didl3.invalid` |
| `Joao Carlos Silva` / `JOAO CARLOS SILVA` | `Person mxvotcyt` / `PERSON MXVOTCYT` (mesmo identificador do e-mail, se for a mesma pessoa; a caixa segue a do nome, e cada grafia volta como estava) |
| `B123456` (código do João) | `USER-mxvotcyt` (`user-mxvotcyt` para `b123456`: como no nome, a caixa segue a do valor) |
| `10.42.7.15` | `250.153.31.15` (mesma sub-rede vira a mesma sub-rede falsa) |
| `mysql-dh.empresa.intra` | `mysql-x7k2.invalid` |
| `529.982.247-25` | `CPF-irvikcpk` |
| token ou senha | `SECRET-…` |

- São derivados de uma **chave secreta de 256 bits** (HMAC-SHA256). Sem a chave, não dá para
  saber quem é quem, nem testando listas de candidatos.
- O mesmo valor vira sempre o mesmo pseudônimo, entre mensagens e entre sessões.
- **Perder a chave não perde dado**, porque os seus arquivos são reais. Uma nova é gerada e
  os pseudônimos mudam dali em diante. Mesmo assim, guarde uma cópia de
  `~/.config/llm-dlp/key`.

## O que é lembrado

Alguns dados só são reconhecidos com uma pista ao lado (a palavra "RG", o nome da coluna).
Quando um desses valores é mascarado, o llm-dlp passa a lembrar dele e o reconhece depois em
qualquer lugar: na saída de um comando, num arquivo lido mais tarde, sem a pista por perto.

| Onde | O que guarda |
|---|---|
| Memória | O valor real, enquanto o llm-dlp estiver no ar |
| Disco (`seen.json`, `people.json`) | **Só o hash**, feito com a chave. Serve para reconhecer depois de reiniciar. Para nomes de recursos internos (tabela, servidor...), também o tipo e o dia em que foi aprendido e visto pela última vez (para a validade de 90 dias) |
| Disco (`sent.log`) | Para cada texto já enviado: um hash do ponto da conversa em que ele está e, de cada trecho trocado, onde ele fica no texto, o tipo e o pseudônimo. Serve para o texto sair igual depois de reiniciar (ver abaixo) |

Nenhum valor real e nenhum texto mascarado é gravado em disco pelo llm-dlp.

**O valor aprendido vale para texto novo.** O Claude Code reenvia a conversa inteira a cada
mensagem, e o cache da API só vale se o começo for idêntico ao da vez anterior. Por isso, quando
a conversa é reenviada, um texto que já saiu sai igual, mesmo que depois o llm-dlp aprenda um
valor que aparece nele: mudá-lo não protegeria nada (ele já foi enviado assim) e faria a
conversa inteira ser regravada no cache. Isso vale também depois de reiniciar, graças ao
`sent.log`.

O congelamento é do texto **naquele ponto da conversa** (o mesmo começo de requisição até
ele), não do texto em si: o mesmo texto numa mensagem nova, ou noutra conversa (o mesmo
arquivo lido de novo, por exemplo), é mascarado com tudo o que se sabe agora. Se a configuração ou a versão do llm-dlp mudar, ou se você importar pessoas,
o registro recomeça e o histórico é mascarado de novo com as regras atuais (o cache é
regravado uma vez).

**O que vem da internet não ensina.** O resultado de `WebFetch` e `WebSearch` (e o que o
Claude escreve nessas ferramentas) é mascarado onde aparece, mas nada dele é lembrado: uma
página de documentação com uma senha de exemplo numa URL não faz essa palavra virar segredo
em todo lugar.

## Falha fechada

| Situação | O que acontece |
|---|---|
| O llm-dlp está fora do ar | O Claude Code não consegue enviar a mensagem |
| A requisição não é JSON, ou tem algo que ele não sabe tratar | Recusada com erro; nada é enviado |
| A requisição não vem de um programa desta máquina (uma página no navegador) | Recusada; nada é enviado (ver [a porta local](security.md#a-porta-local)) |
| Outro programa responde na porta do llm-dlp | A mensagem é barrada: ele não consegue provar que é o llm-dlp |
| Imagem ou PDF que não deu para verificar (falta o OCR, arquivo ilegível) | O arquivo não sai: é retirado da requisição, e o modelo recebe no lugar um aviso com o motivo. A conversa continua |
| Alguém apaga a configuração (com a trava instalada) | O Claude Code se recusa a funcionar |

## Imagens e PDFs

O texto é lido por OCR e os trechos sensíveis são cobertos de preto antes de sair. Como o
OCR erra (lê `@` como `Q`, por exemplo), em imagem o llm-dlp cobre a mais: tudo com cara de
e-mail, domínio, IP ou número longo. PDF com texto vira texto mascarado; PDF escaneado vira
imagens cobertas.

Sem o OCR instalado, ou com um arquivo que ele não consegue ler (inclusive o que declara um
tamanho absurdo, montado para estourar a memória), a imagem ou o PDF não sai.
No lugar vai um aviso, e o Claude conta na resposta o que houve e o comando para instalar o
OCR; o detalhe do erro fica no log. Depois de instalar, é só mandar o arquivo de novo. O
`llm-dlp status` avisa quando falta o OCR.

## O texto do próprio Claude Code

O system e as definições das ferramentas são escritos pelo Claude Code, mas podem levar dado
seu (a pasta de trabalho, o `CLAUDE.md`, a descrição de uma ferramenta de MCP). São mascarados
como o resto, com duas diferenças:

- A palavra comum que a conversa aprendeu como nome (uma tabela `billing`) fica como palavra
  nesses textos, como na prosa das mensagens. Onde ela é o nome (no dado, num comando), é
  mascarada.
- Os dois blocos com que o Claude Code se identifica à API passam intactos: o cabeçalho
  `x-anthropic-billing-header` (só com a forma exata: pares `nome=valor` curtos, sem texto
  livre) e a frase de identidade (só quando é, inteira, uma das frases públicas dele). Com
  uma palavra trocada neles, a API deixa de reconhecer o Claude Code e responde 429 a toda
  mensagem daquela conversa.

## Ferramentas que vão para a internet

Quando o Claude usa uma ferramenta, o pseudônimo é trocado pelo valor real antes de ela
rodar. É assim que comandos e arquivos funcionam com os dados verdadeiros:

```
o Claude escreve:  grep "p.mxvotcyt@didl3.invalid" arquivo.txt
roda de verdade:   grep "joao.silva@empresa.com.br" arquivo.txt      (na sua máquina)
```

`WebFetch` (abrir uma página) e `WebSearch` (pesquisar) são exceção: o que o Claude escreve
nelas vai para um site de fora. Se a troca fosse feita, o dado real sairia junto, e uma
página maliciosa poderia pedir isso de propósito. Por isso, **só nessas duas ferramentas a
troca não é desfeita**: o site recebe o pseudônimo, que não serve para nada.

Os **conectores do claude.ai** (ferramentas `mcp__claude_ai_*`: Gmail, Drive, Agenda...) também
recebem só o pseudônimo, sempre, mesmo que o `config.json` tenha outra lista: eles rodam nos
servidores da Anthropic, fora da sua máquina.

O efeito colateral: pesquisar na web, ou num conector, algo que foi mascarado não acha nada.
Para mudar a lista das ferramentas da web, edite `tools_never_unmasked` no
`config.json`.

## Desempenho

Medido em sessões reais e em testes de carga.

| Item | Valor |
|---|---|
| Mensagem numa conversa em andamento | 5–30 ms (em conversas muito longas, de centenas de milhares de tokens, chega a ~0,1 s) |
| Texto novo | ~0,4 ms por trecho (95% abaixo de 8 ms) |
| Primeira mensagem depois de reiniciar, 1 MB de conversa | 0,1–0,45 s com 32 núcleos; 0,15–0,75 s com 4 |
| Um texto só de 1 MB | 0,1–0,3 s com 32 núcleos; 0,2–0,7 s com 4; 0,6–2,5 s com 1 |
| Imagem | 0,7–2 s na primeira vez; ~1 ms quando reenviada |
| Tokens | ~1,4 token a mais por valor mascarado; nada é acrescentado ao contexto. Num teste real com um arquivo em que quase todo campo é sensível (245 valores), a requisição ficou 0,6% maior |
| Cache da API | Igual ao uso sem o llm-dlp |
| Memória | 40–100 MB no uso normal; no pior caso medido, estabilizou em 150–210 MB (ver [Quanto usa da máquina](#quanto-usa-da-máquina)) |
| CPU parado | Zero |

- **Quantidade não pesa:** o tempo não cresce com o número de valores aprendidos nem de
  pseudônimos na conversa (medido até 120 mil valores e 20 mil e-mails distintos).
- **Tudo que é memorizado tem teto** e descarta o mais antigo.
- **Aprender um valor não regrava o cache:** o que já saiu continua igual (ver
  [O que é lembrado](#o-que-é-lembrado)).
- **Texto grande** é examinado em pedaços, em paralelo, usando os núcleos que a máquina
  tiver. O corte é sempre num espaço em branco, para nunca dividir uma senha ou token. Por
  isso um bloco gigante sem nenhum espaço (2 MB de base64 ou de JSON minificado) não é
  dividido e leva de 1 a 5 s.

## Quanto usa da máquina

Em resumo: parado não usa CPU; em uso, fica em torno de 100 MB de memória e alguns segundos de
CPU por hora; em disco, de 30 a 60 MB. Não precisa de GPU nem de rede própria.

| Recurso | No uso normal | No pior caso medido | O que faz crescer |
|---|---|---|---|
| CPU | ~0,1% de um núcleo, em média. Numa sessão real de 1 h 49 min, com conversa longa, o llm-dlp gastou 7 s de CPU ao todo | Um núcleo ocupado por 3,5 ms a cada KB de texto **novo** (2 núcleos, prioridade baixa): ler 21 MB de arquivos nunca vistos custou 77 s | Texto novo. Texto reenviado sai do cache de resultados e custa quase nada |
| Memória | 25 MB ao subir; 40–100 MB em uso (95 MB na sessão real acima) | Estabiliza em 150–210 MB com 60 mil textos todos diferentes, porque tudo que é lembrado tem teto ([a tabela](development.md#o-que-fica-guardado)) | Quantidade de texto e de valores diferentes na sessão |
| Memória do supervisor | ~23 MB (o processo que religa o llm-dlp se ele cair) | Igual | Nada |
| Disco: programa | 14 MB (`~/.local/bin/llm-dlp`) | Igual | Nada |
| Disco: dados | ~20 MB depois de uma semana de uso (`~/.config/llm-dlp`) | `sent.log` guarda até 400 mil textos; `llm-dlp.log`, até 20 MB (troca aos 10 MB e guarda um anterior); `seen.json`, até ~40 MB (1 milhão de hashes) | Quantidade de texto enviado |
| Tempo por mensagem | 5–30 ms | Arquivo novo: mediana de 6,5 ms; 1 em 10 passa de 65 ms; 1 em 100 passa de 300 ms | Tamanho do texto novo |
| Subida | 0,1 s | Igual | Nada |
| Rede | Nenhuma própria: só repassa o que o Claude Code enviaria | Igual | Nada |
| OCR (imagem e PDF) | Só quando há imagem: 0,7–2 s de CPU por imagem nova, em processo separado (tesseract) | Igual | Número de imagens novas |

Como ler:

- **Uma máquina de trabalho comum não sente.** O maior custo é o de memória, ~100 MB, parecido
  com o de uma aba de navegador.
- **O custo vem do texto novo, não do tamanho da conversa.** A conversa inteira é reenviada a
  cada mensagem, mas o que já foi examinado não é examinado de novo.
- **A versão 0.2.0 custa o mesmo que a 0.1.0**, apesar das listas e dos leitores novos: +4 MB
  de memória ao subir (as listas embutidas), +1 MB no binário e o mesmo tempo por texto
  (medido no mesmo material: 77 s contra 75 s).
- Os números de pior caso foram medidos de propósito com pouco recurso (2 núcleos, prioridade
  baixa). Com mais núcleos, texto grande é dividido e fica proporcionalmente mais rápido.

## O log

Cada requisição gera uma linha em `~/.config/llm-dlp/llm-dlp.log`, com contagens, tempos e
pseudônimos (nunca valores):

```
POST /v1/messages -> 200 | 245 replacements | 0 collisions | mask 9ms | total 2.97s | types: table 120, server 31, cpf 2
```

| Campo | Significado |
|---|---|
| `-> 200` | Resposta da API. `REFUSED` quer dizer que o llm-dlp barrou a requisição (falha fechada) |
| `replacements` | Quantos valores foram trocados por pseudônimos nessa requisição (a conversa inteira é reenviada a cada mensagem, então esse número cresce com ela) |
| `collisions` | Pseudônimos que valeriam para dois valores reais diferentes. Os pseudônimos são curtos, então isso é raro, mas possível, e mais comum em IPs (a sub-rede falsa tem poucas combinações). A ida continua mascarada; só a volta daquele pseudônimo não é feita, para não trocar pelo valor errado. No pior caso, você vê um pseudônimo numa resposta |
| `mask` | Tempo gasto pelo llm-dlp |
| `total` | Tempo total, dominado pela resposta do modelo |
| `types` | Quantos valores diferentes de cada tipo foram mascarados nessa requisição (`cpf 2`: dois CPFs diferentes). Só contagens |
| `collided: …` | Só quando acontece: os pseudônimos que colidiram (`240.1.2.x` é uma rede /24 falsa inteira). A forma de cada um diz o tipo do dado. Duas grafias da mesma pessoa importada (nome completo e primeiro + último nome) dividem o pseudônimo e não são colisão |
| `NOT UNDONE n: …` | Só quando acontece: pseudônimos gerados pelo llm-dlp que chegaram ao cliente na resposta sem voltar a ser o valor real (o modelo mudou ou cortou um, ou citou um de outra conversa; também um endereço da faixa falsa de IP). O que o cliente gravou em arquivo nessa resposta ficou com o pseudônimo. Os pseudônimos vão listados, para dar para procurá-los |
| `API: type: message` | Só quando a API responde com erro: o tipo e o começo da mensagem dela, como ela mandou (ela só recebeu pseudônimos). É o que distingue, por exemplo, um 429 de limite da conta de um 429 de requisição recusada |

Uma linha `WARNING` diz por que uma imagem ou um PDF foi retirado de uma requisição.

O log troca de arquivo aos 10 MB a qualquer momento, também com o llm-dlp no ar há semanas, e
guarda um anterior (`llm-dlp.log.1`): no máximo ~20 MB no disco.

Até a 0.9 as linhas eram em português (`substituições`, `RECUSADO`, `AVISO`...). O `llm-dlp audit`
lê os dois formatos, então um período que pega a atualização sai somado.

`llm-dlp audit` resume o log de um período (`24h` por padrão; também `90m`, `7d`): requisições
e erros da API, o que foi mascarado por tipo, os pseudônimos que voltaram sem virar o valor
real, envios sem máscara e recusas. Só lê o log, que não tem valor real. No espírito do registro
de eventos de sistemas de IA (ISO/IEC 42001, controle A.6.2.8): dá para conferir depois o que
aconteceu sem guardar o conteúdo.

```
$ llm-dlp audit 24h
llm-dlp: log of the last 24h (since 13/10 15:04)
  requests: 152 (status HTTP: 200 152)
  replacements: 31877; collisions (pseudonym left masked): 0
  masked by type (distinct values of each request, added up): table 9120, server 2210, cpf 14
  pseudonyms that came back without turning into the real value: none
  sent unmasked: none
```

