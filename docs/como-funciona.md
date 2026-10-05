# Como funciona

[← voltar ao README](../README.md)

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
| `JOAO CARLOS SILVA` | `Pessoa mxvotcyt` (mesmo identificador do e-mail, se for a mesma pessoa) |
| `B123456` (código do João) | `USUARIO-mxvotcyt` |
| `10.42.7.15` | `250.153.31.15` (mesma sub-rede vira a mesma sub-rede falsa) |
| `mysql-dh.empresa.intra` | `mysql-x7k2.invalid` |
| `529.982.247-25` | `CPF-irvikcpk` |
| token ou senha | `SEGREDO-…` |

- São derivados de uma **chave secreta de 256 bits** (HMAC-SHA256). Sem a chave, não dá para
  saber quem é quem, nem testando listas de candidatos.
- O mesmo valor vira sempre o mesmo pseudônimo, entre mensagens e entre sessões.
- **Perder a chave não perde dado**, porque os seus arquivos são reais. Uma nova é gerada e
  os pseudônimos mudam dali em diante. Mesmo assim, guarde uma cópia de
  `~/.config/llm-dlp/chave`.

## O que é lembrado

Alguns dados só são reconhecidos com uma pista ao lado (a palavra "RG", o nome da coluna).
Quando um desses valores é mascarado, o llm-dlp passa a lembrar dele e o reconhece depois em
qualquer lugar: na saída de um comando, num arquivo lido mais tarde, sem a pista por perto.

| Onde | O que guarda |
|---|---|
| Memória | O valor real, enquanto o llm-dlp estiver no ar |
| Disco (`vistos.json`, `pessoas.json`) | **Só o hash**, feito com a chave. Serve para reconhecer depois de reiniciar. Para nomes de recursos internos (tabela, servidor...), também o tipo e o dia em que foi aprendido e visto pela última vez (para a validade de 90 dias) |
| Disco (`enviados.log`) | Para cada texto já enviado: um hash do ponto da conversa em que ele está e, de cada trecho trocado, onde ele fica no texto, o tipo e o pseudônimo. Serve para o texto sair igual depois de reiniciar (ver abaixo) |

Nenhum valor real e nenhum texto mascarado é gravado em disco pelo llm-dlp.

**O valor aprendido vale para texto novo.** O Claude Code reenvia a conversa inteira a cada
mensagem, e o cache da API só vale se o começo for idêntico ao da vez anterior. Por isso, quando
a conversa é reenviada, um texto que já saiu sai igual, mesmo que depois o llm-dlp aprenda um
valor que aparece nele: mudá-lo não protegeria nada (ele já foi enviado assim) e faria a
conversa inteira ser regravada no cache. Isso vale também depois de reiniciar, graças ao
`enviados.log`.

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
| Imagem ou PDF que não deu para ler | Recusada |
| Alguém apaga a configuração (com a trava instalada) | O Claude Code se recusa a funcionar |

## Imagens e PDFs

O texto é lido por OCR e os trechos sensíveis são cobertos de preto antes de sair. Como o
OCR erra (lê `@` como `Q`, por exemplo), em imagem o llm-dlp cobre a mais: tudo com cara de
e-mail, domínio, IP ou número longo. PDF com texto vira texto mascarado; PDF escaneado vira
imagens cobertas.

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
Para mudar a lista das ferramentas da web, edite `ferramentas_sem_desmascarar` no
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
| Memória | 40–100 MB no uso normal; no pior caso medido, estabilizou em ~170 MB |
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

## O log

Cada requisição gera uma linha em `~/.config/llm-dlp/llm-dlp.log`, só com contagens e tempos:

```
POST /v1/messages -> 200 | 245 substituições | 0 colisões | mascarar 9ms | total 2.97s
```

| Campo | Significado |
|---|---|
| `-> 200` | Resposta da API. `RECUSADO` quer dizer que o llm-dlp barrou a requisição (falha fechada) |
| `substituições` | Quantos valores foram trocados por pseudônimos nessa requisição (a conversa inteira é reenviada a cada mensagem, então esse número cresce com ela) |
| `colisões` | Pseudônimos que valeriam para dois valores reais diferentes. Os pseudônimos são curtos, então isso é raro, mas possível, e mais comum em IPs (a sub-rede falsa tem poucas combinações). A ida continua mascarada; só a volta daquele pseudônimo não é feita, para não trocar pelo valor errado. No pior caso, você vê um pseudônimo numa resposta |
| `mascarar` | Tempo gasto pelo llm-dlp |
| `total` | Tempo total, dominado pela resposta do modelo |

