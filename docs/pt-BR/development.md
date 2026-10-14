# Para desenvolvedores

[← voltar ao README](README.md) · [English](../development.md)

```bash
make build   # binário estático em bin/llm-dlp
make test    # go vet e todos os testes
make version  # versão e commit do código

# testes de carga (minutos): memória com 1,2 GB de texto, entradas patológicas, 16 conversas
# em paralelo, respostas abandonadas, API fora do ar, requisição de 32 MB
LLM_DLP_STRESS=1 go test ./... -run Stress -v -timeout 60m
```

Versões e lançamento: [versioning.md](versioning.md). Como contribuir:
[CONTRIBUTING.md](CONTRIBUTING.md).

## Mapa do repositório

```
cmd/llm-dlp/            o programa de linha de comando
internal/proxy/         o servidor que fica entre o Claude Code e a API
internal/mask/          detecção e troca por pseudônimos (não depende de nenhuma API)
internal/mask/data/    listas embutidas no binário (quase todas geradas)
internal/config/        leitura do config.json (e dos nomes de campo de antes da 0.9.0)
internal/ocr/           leitura de texto em imagem e PDF (tesseract, poppler)
internal/version/       o número da versão e o commit
install.sh              instalador: baixa o binário da release e roda a configuração
.goreleaser.yaml        como os pacotes da release são montados
scripts/                release.sh e release-notes.sh (usados pelo make release)
.github/workflows/      testes a cada envio (ci.yml) e publicação da release (release.yml)
docs/                   a documentação em inglês
docs/pt-BR/             esta documentação, em português (com o README)
CHANGELOG.md            o que mudou em cada versão (em português: docs/pt-BR/CHANGELOG.md)
.github/CONTRIBUTING.md como contribuir (commits, testes, documentação)
.github/SECURITY.md     como relatar uma vulnerabilidade
```

### cmd/llm-dlp e internal/proxy

```
cmd/llm-dlp/
  main.go               ponto de entrada, lista de comandos e os nomes deles antes da 0.9.0
  service.go            serve, supervise, ensure, check, status, stop
  swap.go              o supervisor: segura a porta e a empresta ao proxy; troca com SIGHUP
  update.go          update (download, checksum, troca, volta) e reload
  install.go           install, uninstall, a trava e os ganchos
  inspect.go           simulate: reproduz uma sessão antiga contra uma API falsa
  tools.go        import-people, test, test-media, measure, columns
  emergency.go         modo emergência (exige sudo)

internal/proxy/
  proxy.go              recebe a requisição, encaminha, devolve a resposta
  request.go         ida: mascara o corpo da requisição (com a dica do comando de cada resultado)
  response.go           volta: desmascara a resposta (streaming ou inteira)
  priority.go      o que o modelo escreveu antes de qualquer dado é público
  media.go              imagens e PDFs (OCR e tarja preta)
```

### internal/mask

O pacote é um só (em Go, uma pasta é um pacote, e as peças usam funções internas umas das
outras). A organização é pelo **prefixo do nome do arquivo**: cada família faz uma coisa.

| Prefixo | O que faz |
|---|---|
| (sem prefixo) | O caminho principal: entrada, detecção, troca e volta |
| `detectores_` | Dado pessoal e segredo reconhecidos pelo **formato** |
| `campos_` | Dado pessoal reconhecido pelo **nome do campo** ao lado |
| `leitor_` | Nome de recurso interno reconhecido pela **estrutura** do texto (um arquivo por formato) |
| `objetos` | A base comum dos leitores: tipos, pseudônimo, freios |
| `chamada_` | O que a chamada de ferramenta diz sobre a saída dela |
| `decisao`, `decisor_` | Regras que decidem nomes olhando o texto inteiro |
| `memoria_` | Tudo o que é lembrado, em RAM ou em disco (ver a tabela abaixo) |
| `vocab_` | Vocabulários públicos e a leitura das listas de `dados/` |

```
caminho principal
  masker.go                 o tipo Masker e a sua construção
  mask.go               Mascarar: porta de entrada, com o cache de resultados; Lote congela o que saiu
  detect.go               Detectar: junta os detectores; texto grande vai em pedaços
  normalization.go           tira o transporte (número de linha, grep, diff, ANSI, caixa, JSON escapado)
  pseudonyms.go            troca dos achados por pseudônimos
  unmask.go            troca de volta, inclusive em resposta que chega em pedaços
  key.go                  a chave secreta e os identificadores derivados dela
  people.go                registro de pessoas (nomes, e-mails, códigos), só como hash

dado pessoal e segredo
  detectors_format.go     e-mail, IP, hostname, CPF, CNPJ, telefone, cartão, tokens
  detectors_passwords.go      senhas comuns
  detectors_extra.go      cabeçalho HTTP, IBAN, processo, documentos com palavra por perto
  detectors_network.go        IPv4 interno além da forma com pontos: faixa compartilhada, hífens, DNS reverso
  detectors_fiscal.go     chave de acesso da NF-e, Pix copia e cola (BR Code)
  detectors_br.go          boleto e conta, código de rastreio, matrícula de certidão, código da CID-10
  detectors_sensitive.go   dado sensível da LGPD no texto (listas oficiais de valores), template de digital
  detectors_validators.go dígitos verificadores (CPF, CNPJ, título, IBAN...)
  fields_vocabulary.go     que nomes de campo guardam que dado
  fields.go                 "campo: valor", JSON, SQL INSERT, XML
  fields_tables.go         CSV, markdown, terminal, planilha

nomes de recursos internos (servidor, banco, tabela, coluna, serviço, bucket...)
  objects.go                tipos, pseudônimo, aprendizado com freios
  objects_lists.go         listas homogêneas e nome qualificado com parte conhecida (depois dos leitores)
  readers.go               registro de todos os leitores (fixos e da configuração)
  reader_sql.go             SQL e DDL, erros que citam objetos
  reader_sql_brakes.go      SQL citado em prosa e em comentário de código não é instrução
  reader_table.go          tabelas em qualquer desenho (CSV, largura fixa, caixa, tuplas, HTML, vertical)
  reader_cli_output.go       tabela de kubectl get, docker ps, helm list
  reader_schema.go         nome + tipo de dado (dtypes, printSchema, Arrow, protobuf)
  reader_connection.go         strings de conexão, URIs de banco, DSN, tnsnames, URNs, dbt, Airflow
  reader_connection_brakes.go  nome de modelo na URI de banco e no tnsnames
  reader_yaml.go            motor de YAML/JSON por caminho e o despachante das famílias
  reader_kubernetes.go      Kubernetes, Helm, imagens, DNS de serviço, env em lista
  reader_iac.go             Terraform/HCL, Bicep, ARM, CloudFormation
  reader_ansible.go         inventário do Ansible
  reader_ci.go              pipelines de CI e Jenkinsfile
  reader_key_value.go     chave-valor genérico (YAML, JSON, INI, .env, .properties, XML, --opção)
  reader_addresses.go       hosts internos, armazenamento e filas, git, usuário de rede, pasta pessoal
  reader_paths.go        caminhos fora da pasta pessoal, outras unidades, UNC
  reader_cloud.go           ARN da AWS, IDs do Azure, recursos do GCP, URLs de nuvem
  reader_cloud_hosts.go     hosts de serviço gerenciado (RDS, Redshift, S3, Azure)
  reader_packages.go         go.mod, groupId do Maven/Gradle, escopo do npm
  reader_code.go          código em qualquer linguagem: o nome ao lado e a função chamada dizem o tipo
  reader_traceback.go       traceback: o caminho da linha diz se o código é do cliente ou de terceiro
  reader_cli.go             linha de comando
  reader_ip.go              IPv6 interno (sempre), IP público (opcional)
  reader_terms.go          termos da empresa embutidos em identificadores

o que a conversa diz
  call.go                o que o proxy sabe de uma chamada de ferramenta (pedido, argumentos, eco)
  call_command.go        o comando diz o que a saída é (SELECT, cut, listagem)
  call_decider.go        proveniência e identidade nas saídas de comando
  decision.go                nomes decididos por texto, decisores, registro de quem escreveu
  lexical_decider.go         regra léxica de código e configuração

memória
  memory_known.go     valores já mascarados, reconhecidos depois em qualquer lugar (RAM)
  memory_seen.go         os mesmos valores em disco, só como hash
  memory_sent.go       o que cada texto já enviado levou (para sair igual nos reenvios)
  memory_conversation.go       nomes decididos nos textos de uma requisição valem para os outros
  memory_conversation_brakes.go  onde a memória da conversa não troca (programa, comentário, prosa)
  memory_tracing.go   o nome é decidido onde entra e vale em todo lugar
  memory_software.go       software público com prova e sombreamento

vocabulário
  vocab_dev.go              palavras de tipo, sufixos internos, nomes públicos, valor sem dono
  vocab_types.go            tipos de dado das linguagens (SQL, pandas, Arrow, Spark...)
  vocab_words.go         palavras comuns e dicionário (lê dados/palavras_*.txt)
  vocab_generated.go           lê as outras listas de dados/
```

### internal/mask/data

| Arquivo | O que é | De onde vem |
|---|---|---|
| `common_words.txt` | 20 mil palavras mais frequentes de pt e en | FrequencyWords (CC BY-SA 4.0) |
| `dictionary_words.txt` | As 50 mil mais frequentes, menos as comuns | Gerado: `TestGerarDicionario` |
| `public_ref.txt` | Palavras frequentes como nome de recurso no material público | Gerado: `TestGerarRefPublica` |
| `language_types.txt` | Tipos de dado que o código Go público usa como tipo de campo | Gerado: `TestGerarRefPublica` |
| `official_images.txt` | Imagens Oficiais do Docker | Gerado: `TestGerarRefPublica` (de `docker-library/docs`) |
| `public_software.txt`, `vendors.txt` | Nome e dono dos repositórios do GitHub com 3000 estrelas ou mais | Gerado: `TestGerarSoftwarePublico` |
| `vocab_sql.txt` | Palavras reservadas e objetos de sistema do SQL | Escrito à mão, com as fontes |

Os geradores estão em `vocab_generate_test.go` e só rodam com as variáveis de ambiente descritas
lá (ver [Referência pública derivada](structures.md#referência-pública-derivada)). Lista
desatualizada nunca libera nada: só deixa de reconhecer um nome público, que continua
mascarado.

### Testes

Os testes ficam ao lado do código, com `_test.go` no fim (é a convenção do Go, e é o que
permite testar funções internas). O teste de um arquivo tem o mesmo nome (`fields.go` e
`fields_test.go`). Os que atravessam vários arquivos:

| Arquivo | O que confere |
|---|---|
| `public_cases_test.go` | Formatos reais tirados de repositórios públicos, com nomes inventados: o que tem de sair e o que tem de ficar. Toda regra que deixa um nome em claro tem um caso aqui |
| `leitores_*_test.go` | Os leitores por área (dados, DevOps, desenvolvimento, código) |
| `variations_test.go`, `general_rules_test.go` | Cada caso em todas as variações de escrita; regras que valem para todos os leitores |
| `matrix_test.go`, `matrix_transport_test.go` | O mesmo conteúdo em todos os desenhos e transportes |
| `negatives_test.go`, `brakes_reference_test.go` | O que não pode ser mascarado |
| `fuzz_test.go`, `readers_fuzz_test.go` | Fuzz da normalização e da ida e volta |
| `benchmark_test.go`, `stress_test.go` | Tempo e carga |
| `measurement_test.go` | Medições em corpus público e em sessões reais (só contagens; ver abaixo) |
| `helpers_test.go` | Funções de apoio dos testes |

## O que fica guardado

Tudo o que o llm-dlp lembra, onde fica e até quanto cresce. Nenhum valor real vai para o
disco: lá só há hash, posição e pseudônimo.

| O que | Para que serve | Onde | Quanto dura | Teto |
|---|---|---|---|---|
| Cache de resultados (`memo`, `mask.go`) | Não examinar de novo o texto que já foi examinado (a conversa inteira é reenviada a cada mensagem) | RAM | Até encher | 64 MB de texto, em duas gerações |
| Enviados (`memory_sent.go`) | O texto que já saiu sai igual nos reenvios (o cache da API depende disso) | RAM e `enviados.log` | Até mudar a configuração ou a versão | 64 MB em RAM; 400 mil textos em disco |
| Conhecidos (`memory_known.go`) | Valor mascarado com uma pista ao lado é reconhecido depois sem a pista | RAM (valor real) | Enquanto o llm-dlp está no ar | 50 mil valores (sai a metade mais antiga) |
| Vistos (`memory_seen.go`) | Os conhecidos, para valerem depois de reiniciar | `vistos.json` (só hash) | 90 dias sem uso, para nome de recurso | 500 mil valores e 500 mil nomes |
| Pessoas (`people.go`) | Nomes, e-mails e códigos cadastrados | `pessoas.json` (só hash) | Até você importar de novo | O que você importar |
| Memória da conversa (`memory_conversation.go`) | Nome decidido num texto vale nos outros textos da mesma requisição | RAM | Uma requisição (é recalculada a cada uma) | Os textos da requisição |
| Quem escreveu (`escritos`, `decision.go`) | Saber o que o modelo escreveu, para não tratar como dado | RAM | Até encher | 20 mil textos, em duas gerações |
| Evidência fraca (`fracos`, `objects.go`) | Nome visto por uma regra fraca só vale com a segunda | RAM | Enquanto o llm-dlp está no ar | 200 mil nomes |
| Software provado e sombreamento (`memory_software.go`) | Não espalhar pela conversa o nome padrão de um software público | RAM | Enquanto o llm-dlp está no ar | A lista de software (medido: 3 nomes depois de 60 mil textos) |
| Classe do rótulo (`classes`, `fields_vocabulary.go`) | Não classificar de novo o mesmo nome de campo | RAM | Até encher | 20 mil rótulos |
| Anterioridade (`internal/proxy/priority.go`) | O que o modelo escreveu antes de qualquer dado é público | RAM | Uma requisição | A conversa |
| Tabela pseudônimo → real (`unmask.go`) | Desfazer a troca na resposta | RAM | Uma requisição | A conversa |

Quanto isso custa na prática está em
[Quanto usa da máquina](how-it-works.md#quanto-usa-da-máquina).

## Como um texto é mascarado

```
proxy.ServeHTTP                      recebe a requisição
 ├ mascararCorpo (request.go)     1ª passada: junta os textos e os mascara antes (Aquecer),
 │                                   para todo valor aprendido na requisição já valer na montagem
 │                                   2ª passada: chama Lote.Mascarar em cada texto, em ordem
 │  └ Lote.Mascarar (mask.go)    já saiu neste ponto da conversa (memória ou
 │                                   enviados.log)? sai igual. A posição é o hash do bloco
 │                                   (tudo antes dele e ele mesmo) mais a ordem do texto nele
 │                                   já está na memória e ainda vale? devolve. senão:
 │     ├ Detectar (detect.go)      roda os detectores e junta os achados
 │     │  ├ detectores_*.go          formato, senhas, extras
 │     │  ├ campos*.go               nome do campo
 │     │  └ memory_known.go    valores já vistos
 │     └ aplicar (pseudonyms.go)    troca cada achado pelo pseudônimo
 ├ envia para a API e congela os textos (Lote.Congelar)
 └ desmascararSSE (response.go)      troca os pseudônimos de volta, pedaço a pedaço
```

## Acoplar outra API de LLM

O llm-dlp entende o formato da API da Anthropic (`/v1/messages`), o do Gemini
(`generateContent`, em `gemini.go`, também dentro do envelope do Code Assist do Gemini CLI com
conta Google, `ponteCodeAssist`) e os da OpenAI (Chat Completions, em `openai.go`, e Responses,
em `responses.go`, o do Codex). Os outros são o modelo para o próximo: a requisição não ganhou
um mascarador próprio, ela é **vista na forma da Anthropic** (`ponteGemini`, `ponteOpenAI`,
`ponteResponses`) e passa pelo mesmo caminho, de modo que a memória da conversa, as dicas dos
comandos, o congelamento e as imagens valem igual; só a volta (`voltaGemini`, `voltaOpenAI`,
`voltaResponses`) é escrita para o formato. O código de um formato só roda para as requisições
do caminho dele, e o `isolation_test.go` confere que uma requisição do Claude Code sai byte a
byte igual com ou sem ele. O destino é
escolhido pelo caminho (`Proxy.destino`, com os `providers` da configuração).

O que um formato sem leitor tem hoje, e o que fazer para dar um leitor a ele (o
`responses.go` é o exemplo mais recente a seguir):

| Parte | Hoje, sem leitor | O que fazer |
|---|---|---|
| Endereço | Qualquer API pode ser atendida em `/<nome>`, com um provedor em `providers` (`format` e `upstream`) | Para um provedor que já vem embutido, acrescentá-lo em `config.ProvedoresPadrao`. Se o formato é reconhecido pelo caminho (como `/chat/completions` e `/responses`), acrescentá-lo em `Proxy.destino` |
| Ida (requisição) | Modo genérico: **todo texto do JSON é mascarado**. É seguro, mas mascara até campos que não precisava | Escrever `ponteX(v any) (map[string]any, func(), bool)` num arquivo próprio: uma visão da requisição na forma da Anthropic (`system`, `messages`, `tools`) cujos textos a função devolvida grava de volta no JSON original. Registrá-la no mapa do `mascararCorpo` (`request.go`). Campos que nunca podem mudar vão para `chavesIntocaveisPonte` |
| Imagens e PDFs | No modo genérico, não passam pelo OCR | Na ponte, mostrá-los como blocos `image` e `document`, como faz o `ponteGemini` |
| Volta sem streaming | Os campos `text`, `content` e `input` do JSON são desmascarados | Escrever `desmascararXJSON` e escolhê-lo pelo `format` em `proxy.go` |
| Volta com streaming | **Não é desmascarada**: você veria os pseudônimos. Nada vaza | Escrever `desmascararSSEX`, com um `voltaX` que passa cada texto pelo seu próprio `mask.Fluxo` (ele cuida do pseudônimo cortado entre dois pedaços), e escolhê-lo pelo `format` em `proxy.go` |
| `llm-dlp last` | Não mostra a conversa | Acrescentar o `format` à lista que guarda a última requisição (`proxy.go`) e ler as mensagens dele em `mensagensDaUltima` (`cmd/llm-dlp/inspect.go`) |
| Isolamento | — | Acrescentar o formato ao `isolation_test.go`: requisições e respostas malformadas, e uma requisição do Claude Code que continua igual |

O pacote `mask` não depende do formato de nenhuma API: recebe texto e devolve texto. Todo o
trabalho de uma API nova fica em `internal/proxy`.

O que testar ao acoplar: nada real chega à API, a resposta volta exatamente real, e as
mensagens antigas chegam idênticas de uma requisição para a outra (o cache depende disso).
O `stress_test.go` do proxy faz essas três conferências e serve de modelo.

## Acrescentar um detector

1. Escreva a função num dos `detectores_*.go` (ou num arquivo novo). Ela recebe o texto e
   chama `add(início, fim, "tipo")` para cada trecho achado.
2. Chame-a em `detectarBase` (`detectors_format.go`), atrás de um teste barato que evite
   rodar em texto que não tem o "ingrediente" (uma palavra-chave, um caractere).
3. O pseudônimo sai como `TIPO-identificador`. Para outro formato, acrescente um caso em
   `Pseudonimo` (`pseudonyms.go`).
4. Se o valor deve ser lembrado depois de mascarado uma vez, acrescente o tipo em
   `contextuais` (`memory_known.go`).
5. Testes: um caso que pega e um que não pega em `detectors_test.go`, e o caso em
   `casosDeCorte` (`detect_test.go`), que confere o dado em todas as posições de corte de
   um texto grande.
6. Rode `go test ./...` e os benchmarks (`go test ./internal/mask -bench Frio`) para ver se
   não pesou.
7. Os leitores só leem o texto (não consultam o que foi aprendido): num texto grande eles rodam
   em paralelo e os achados são aplicados depois, na ordem do registro. Um leitor que dependa
   do aprendido (como as listas) roda à parte, depois dos outros.
8. Nunca procure o começo ou o fim da linha sem limite a cada ocorrência
   (`strings.LastIndexByte(s[:i], '\n')`): numa linha única longa (JSON minificado) isso vira
   quadrático. Use `inicioLinhaJ`/`fimLinhaJ` (`memory_known.go`, janela de 1000 caracteres).
   `TestLinhaLongaLinear` e os benchmarks `-bench Linha` conferem.

## Como foi testado

| Teste | O que faz | Como rodar |
|---|---|---|
| Suíte automática | Cada detector, ida e volta, streaming, e cada tipo de dado em todas as posições de corte de um texto grande | `make test` |
| Variações dos leitores | Cada caso de cada leitor em todas as variações de escrita (caixa, espaços, citações, comentários, pontuação, 2–4 partes, `$ # -` e acento, vários na linha, dentro de JSON), mais prosa com SQL entre crases | `go test ./internal/mask -run Variacoes` |
| Carga e estresse | Memória com 1,2 GB de texto, 120 mil valores aprendidos, conversas em paralelo, API fora do ar, requisição de 32 MB | `LLM_DLP_STRESS=1 go test ./... -run Stress` |
| Sessões reais | Reproduz uma sessão antiga do Claude Code pelo llm-dlp, contra uma API falsa local (nada sai da máquina), e confere recusas, ida e volta (do texto e das chamadas de ferramenta que o modelo escreve: o que vai para um arquivo gerado ou para o terminal), cache e tempo | `llm-dlp simulate SESSAO.jsonl` |
| Matriz de transporte | Cada conteúdo (catálogo, conexão, config do Snowflake, manifesto, buckets, ORM) em 15 desenhos (CSV, `;`, TAB, `\|`, caixa, largura fixa, bordas, tuplas, HTML, vertical, JSON, JSONL, `k=v`, coluna solta, `uniq -c`) e 11 transportes (cru, Read, `cat -n`, `grep -n`, diff, markdown, JSON escapado, ANSI, CRLF, citação, log): os mesmos nomes mascarados em todas as células (meta 95%) e ida e volta exata; os negativos não têm nada mascarado em célula nenhuma | `go test ./internal/mask -run Matriz -v` |
| Fuzz | Mapa da normalização, ida e volta pelos leitores e a varredura do SQL contra as regex que ela substituiu | `go test ./internal/mask -run '^$' -fuzz FuzzIdaVolta -fuzztime 60s` (e `FuzzNormalizar`, `FuzzVarreduraSQL`) |
| Falso positivo | Por regra: achados num corpus público (código e documentação de terceiros) e, nas sessões reais, quantos nomes aprendidos existem no corpus público (limite 2%). Só contagens; chave e configuração de teste | `LLM_DLP_CORPUS=dir1:dir2 go test ./internal/mask -run MedirCorpus -v` e, com `LLM_DLP_SESSIONS=~/.claude/projects`, `-run MedirSessoes` |
| Uso real | Claude Code de verdade, em cenários do dia a dia (CSV, planilha, segredos, log, texto colado, print, SQL, pesquisa na web, subagente), com dados fictícios e um espião gravando o que chega à API | Manual (ver abaixo) |

No teste de uso real, cada cenário roda duas vezes, direto e pelo llm-dlp, para comparar.
Resultado da última rodada: nenhum dos 179 valores sensíveis chegou à API, nenhuma resposta
mostrou pseudônimo, arquivos gravados com os valores reais, tokens de entrada −1,5% e +8 ms
por mensagem (mediana).

Para repetir esse teste, rode o Claude Code **num ambiente isolado** (por exemplo, com
`unshare` e `tmpfs` por cima da sua pasta pessoal), em que só a pasta de teste exista.
`--allowedTools` não basta: ele só acrescenta permissões, e as suas configurações globais
podem deixar o agente ler outros arquivos.

Durante o uso, `llm-dlp last` mostra as últimas mensagens exatamente como a API recebeu.
