# Para desenvolvedores

[← voltar ao README](README.md) · [English](../development.md)

```bash
make build   # binário estático em bin/llm-dlp
make test    # go vet e todos os testes
make versao  # versão e commit do código

# testes de carga (minutos): memória com 1,2 GB de texto, entradas patológicas, 16 conversas
# em paralelo, respostas abandonadas, API fora do ar, requisição de 32 MB
LLM_DLP_ESTRESSE=1 go test ./... -run Estresse -v -timeout 60m
```

Versões e lançamento: [versoes.md](versoes.md). Como contribuir:
[CONTRIBUTING.md](CONTRIBUTING.md).

## Mapa do repositório

```
cmd/llm-dlp/            o programa de linha de comando
internal/proxy/         o servidor que fica entre o Claude Code e a API
internal/mask/          detecção e troca por pseudônimos (não depende de nenhuma API)
internal/mask/dados/    listas embutidas no binário (quase todas geradas)
internal/config/        leitura do config.json
internal/ocr/           leitura de texto em imagem e PDF (tesseract, poppler)
internal/versao/        o número da versão e o commit
install.sh              instalador: baixa o binário da release e roda a configuração
.goreleaser.yaml        como os pacotes da release são montados
scripts/                release.sh e notas-da-versao.sh (usados pelo make release)
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
  main.go               ponto de entrada e lista de comandos
  servico.go            servir, supervisionar, garantir, verificar, status, parar
  instalar.go           instalar, desinstalar e a trava
  conferir.go           simular: reproduz uma sessão antiga contra uma API falsa
  ferramentas.go        importar-pessoas, testar, testar-midia, medir, colunas
  emergencia.go         modo emergência (exige sudo)

internal/proxy/
  proxy.go              recebe a requisição, encaminha, devolve a resposta
  requisicao.go         ida: mascara o corpo da requisição (com a dica do comando de cada resultado)
  resposta.go           volta: desmascara a resposta (streaming ou inteira)
  anterioridade.go      o que o modelo escreveu antes de qualquer dado é público
  midia.go              imagens e PDFs (OCR e tarja preta)
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
  mascarar.go               Mascarar: porta de entrada, com o cache de resultados; Lote congela o que saiu
  detectar.go               Detectar: junta os detectores; texto grande vai em pedaços
  normalizacao.go           tira o transporte (número de linha, grep, diff, ANSI, caixa, JSON escapado)
  pseudonimos.go            troca dos achados por pseudônimos
  desmascarar.go            troca de volta, inclusive em resposta que chega em pedaços
  chave.go                  a chave secreta e os identificadores derivados dela
  pessoas.go                registro de pessoas (nomes, e-mails, códigos), só como hash

dado pessoal e segredo
  detectores_formato.go     e-mail, IP, hostname, CPF, CNPJ, telefone, cartão, tokens
  detectores_senhas.go      senhas comuns
  detectores_extras.go      cabeçalho HTTP, IBAN, processo, documentos com palavra por perto
  detectores_validadores.go dígitos verificadores (CPF, CNPJ, título, IBAN...)
  campos_vocabulario.go     que nomes de campo guardam que dado
  campos.go                 "campo: valor", JSON, SQL INSERT, XML
  campos_tabelas.go         CSV, markdown, terminal, planilha

nomes de recursos internos (servidor, banco, tabela, coluna, serviço, bucket...)
  objetos.go                tipos, pseudônimo, aprendizado com freios
  objetos_listas.go         listas homogêneas e nome qualificado com parte conhecida (depois dos leitores)
  leitores.go               registro de todos os leitores (fixos e da configuração)
  leitor_sql.go             SQL e DDL, erros que citam objetos
  leitor_sql_freios.go      SQL citado em prosa e em comentário de código não é instrução
  leitor_tabela.go          tabelas em qualquer desenho (CSV, largura fixa, caixa, tuplas, HTML, vertical)
  leitor_saida_cli.go       tabela de kubectl get, docker ps, helm list
  leitor_esquema.go         nome + tipo de dado (dtypes, printSchema, Arrow, protobuf)
  leitor_conexao.go         strings de conexão, URIs de banco, DSN, tnsnames, URNs, dbt, Airflow
  leitor_conexao_freios.go  nome de modelo na URI de banco e no tnsnames
  leitor_yaml.go            motor de YAML/JSON por caminho e o despachante das famílias
  leitor_kubernetes.go      Kubernetes, Helm, imagens, DNS de serviço, env em lista
  leitor_iac.go             Terraform/HCL, Bicep, ARM, CloudFormation
  leitor_ansible.go         inventário do Ansible
  leitor_ci.go              pipelines de CI e Jenkinsfile
  leitor_chave_valor.go     chave-valor genérico (YAML, JSON, INI, .env, .properties, XML, --opção)
  leitor_enderecos.go       hosts internos, armazenamento e filas, git, usuário de rede, pasta pessoal
  leitor_caminhos.go        caminhos fora da pasta pessoal, outras unidades, UNC
  leitor_nuvem.go           ARN da AWS, IDs do Azure, recursos do GCP, URLs de nuvem
  leitor_nuvem_hosts.go     hosts de serviço gerenciado (RDS, Redshift, S3, Azure)
  leitor_pacotes.go         go.mod, groupId do Maven/Gradle, escopo do npm
  leitor_codigo.go          código em qualquer linguagem: o nome ao lado e a função chamada dizem o tipo
  leitor_traceback.go       traceback: o caminho da linha diz se o código é do cliente ou de terceiro
  leitor_cli.go             linha de comando
  leitor_ip.go              IP público e IPv6 (opcional)
  leitor_termos.go          termos da empresa embutidos em identificadores

o que a conversa diz
  chamada.go                o que o proxy sabe de uma chamada de ferramenta (pedido, argumentos, eco)
  chamada_comando.go        o comando diz o que a saída é (SELECT, cut, listagem)
  chamada_decisor.go        proveniência e identidade nas saídas de comando
  decisao.go                nomes decididos por texto, decisores, registro de quem escreveu
  decisor_lexico.go         regra léxica de código e configuração

memória
  memoria_conhecidos.go     valores já mascarados, reconhecidos depois em qualquer lugar (RAM)
  memoria_vistos.go         os mesmos valores em disco, só como hash
  memoria_enviados.go       o que cada texto já enviado levou (para sair igual nos reenvios)
  memoria_conversa.go       nomes decididos nos textos de uma requisição valem para os outros
  memoria_conversa_freios.go  onde a memória da conversa não troca (programa, comentário, prosa)
  memoria_rastreamento.go   o nome é decidido onde entra e vale em todo lugar
  memoria_software.go       software público com prova e sombreamento

vocabulário
  vocab_dev.go              palavras de tipo, sufixos internos, nomes públicos, valor sem dono
  vocab_tipos.go            tipos de dado das linguagens (SQL, pandas, Arrow, Spark...)
  vocab_palavras.go         palavras comuns e dicionário (lê dados/palavras_*.txt)
  vocab_gerado.go           lê as outras listas de dados/
```

### internal/mask/dados

| Arquivo | O que é | De onde vem |
|---|---|---|
| `palavras_comuns.txt` | 20 mil palavras mais frequentes de pt e en | FrequencyWords (CC BY-SA 4.0) |
| `palavras_dicionario.txt` | As 50 mil mais frequentes, menos as comuns | Gerado: `TestGerarDicionario` |
| `ref_publica.txt` | Palavras frequentes como nome de recurso no material público | Gerado: `TestGerarRefPublica` |
| `tipos_linguagem.txt` | Tipos de dado que o código Go público usa como tipo de campo | Gerado: `TestGerarRefPublica` |
| `imagens_oficiais.txt` | Imagens Oficiais do Docker | Gerado: `TestGerarRefPublica` (de `docker-library/docs`) |
| `software_publico.txt`, `fornecedores.txt` | Nome e dono dos repositórios do GitHub com 3000 estrelas ou mais | Gerado: `TestGerarSoftwarePublico` |
| `vocab_sql.txt` | Palavras reservadas e objetos de sistema do SQL | Escrito à mão, com as fontes |

Os geradores estão em `vocab_gerar_test.go` e só rodam com as variáveis de ambiente descritas
lá (ver [Referência pública derivada](estruturas.md#referência-pública-derivada)). Lista
desatualizada nunca libera nada: só deixa de reconhecer um nome público, que continua
mascarado.

### Testes

Os testes ficam ao lado do código, com `_test.go` no fim (é a convenção do Go, e é o que
permite testar funções internas). O teste de um arquivo tem o mesmo nome (`campos.go` e
`campos_test.go`). Os que atravessam vários arquivos:

| Arquivo | O que confere |
|---|---|
| `casos_publicos_test.go` | Formatos reais tirados de repositórios públicos, com nomes inventados: o que tem de sair e o que tem de ficar. Toda regra que deixa um nome em claro tem um caso aqui |
| `leitores_*_test.go` | Os leitores por área (dados, DevOps, desenvolvimento, código) |
| `variacoes_test.go`, `regras_gerais_test.go` | Cada caso em todas as variações de escrita; regras que valem para todos os leitores |
| `matriz_test.go`, `matriz_transporte_test.go` | O mesmo conteúdo em todos os desenhos e transportes |
| `negativos_test.go`, `freios_referencia_test.go` | O que não pode ser mascarado |
| `fuzz_test.go`, `leitores_fuzz_test.go` | Fuzz da normalização e da ida e volta |
| `benchmark_test.go`, `estresse_test.go` | Tempo e carga |
| `medicao_test.go` | Medições em corpus público e em sessões reais (só contagens; ver abaixo) |
| `ajuda_test.go` | Funções de apoio dos testes |

## O que fica guardado

Tudo o que o llm-dlp lembra, onde fica e até quanto cresce. Nenhum valor real vai para o
disco: lá só há hash, posição e pseudônimo.

| O que | Para que serve | Onde | Quanto dura | Teto |
|---|---|---|---|---|
| Cache de resultados (`memo`, `mascarar.go`) | Não examinar de novo o texto que já foi examinado (a conversa inteira é reenviada a cada mensagem) | RAM | Até encher | 64 MB de texto, em duas gerações |
| Enviados (`memoria_enviados.go`) | O texto que já saiu sai igual nos reenvios (o cache da API depende disso) | RAM e `enviados.log` | Até mudar a configuração ou a versão | 64 MB em RAM; 400 mil textos em disco |
| Conhecidos (`memoria_conhecidos.go`) | Valor mascarado com uma pista ao lado é reconhecido depois sem a pista | RAM (valor real) | Enquanto o llm-dlp está no ar | 50 mil valores (sai a metade mais antiga) |
| Vistos (`memoria_vistos.go`) | Os conhecidos, para valerem depois de reiniciar | `vistos.json` (só hash) | 90 dias sem uso, para nome de recurso | 500 mil valores e 500 mil nomes |
| Pessoas (`pessoas.go`) | Nomes, e-mails e códigos cadastrados | `pessoas.json` (só hash) | Até você importar de novo | O que você importar |
| Memória da conversa (`memoria_conversa.go`) | Nome decidido num texto vale nos outros textos da mesma requisição | RAM | Uma requisição (é recalculada a cada uma) | Os textos da requisição |
| Quem escreveu (`escritos`, `decisao.go`) | Saber o que o modelo escreveu, para não tratar como dado | RAM | Até encher | 20 mil textos, em duas gerações |
| Evidência fraca (`fracos`, `objetos.go`) | Nome visto por uma regra fraca só vale com a segunda | RAM | Enquanto o llm-dlp está no ar | 200 mil nomes |
| Software provado e sombreamento (`memoria_software.go`) | Não espalhar pela conversa o nome padrão de um software público | RAM | Enquanto o llm-dlp está no ar | A lista de software (medido: 3 nomes depois de 60 mil textos) |
| Classe do rótulo (`classes`, `campos_vocabulario.go`) | Não classificar de novo o mesmo nome de campo | RAM | Até encher | 20 mil rótulos |
| Anterioridade (`internal/proxy/anterioridade.go`) | O que o modelo escreveu antes de qualquer dado é público | RAM | Uma requisição | A conversa |
| Tabela pseudônimo → real (`desmascarar.go`) | Desfazer a troca na resposta | RAM | Uma requisição | A conversa |

Quanto isso custa na prática está em
[Quanto usa da máquina](como-funciona.md#quanto-usa-da-máquina).

## Como um texto é mascarado

```
proxy.ServeHTTP                      recebe a requisição
 ├ mascararCorpo (requisicao.go)     1ª passada: junta os textos e os mascara antes (Aquecer),
 │                                   para todo valor aprendido na requisição já valer na montagem
 │                                   2ª passada: chama Lote.Mascarar em cada texto, em ordem
 │  └ Lote.Mascarar (mascarar.go)    já saiu neste ponto da conversa (memória ou
 │                                   enviados.log)? sai igual. A posição é o hash do bloco
 │                                   (tudo antes dele e ele mesmo) mais a ordem do texto nele
 │                                   já está na memória e ainda vale? devolve. senão:
 │     ├ Detectar (detectar.go)      roda os detectores e junta os achados
 │     │  ├ detectores_*.go          formato, senhas, extras
 │     │  ├ campos*.go               nome do campo
 │     │  └ memoria_conhecidos.go    valores já vistos
 │     └ aplicar (pseudonimos.go)    troca cada achado pelo pseudônimo
 ├ envia para a API e congela os textos (Lote.Congelar)
 └ desmascararSSE (resposta.go)      troca os pseudônimos de volta, pedaço a pedaço
```

## Acoplar outra API de LLM

Hoje o llm-dlp entende o formato da API da Anthropic (`/v1/messages`). O que já funciona e o
que falta para outro formato (OpenAI, Gemini):

| Parte | Hoje, com outra API | O que fazer |
|---|---|---|
| Cliente | Qualquer cliente que aceite trocar o endereço da API pode apontar para o llm-dlp | Configurar a variável do cliente (como o `ANTHROPIC_BASE_URL` do Claude Code) e o `upstream` no `config.json` |
| Ida (requisição) | Um caminho que não seja `/v1/messages` cai no modo genérico: **todo texto do JSON é mascarado**. É seguro, mas mascara até campos que não precisava | Em `requisicao.go`, escrever uma função como `requisicaoAnthropic` para o formato novo: quais campos levam texto do usuário, quais nunca podem mudar, onde vêm as imagens. Escolher a função pelo caminho em `mascararCorpo` |
| Imagens | Só o formato da Anthropic é tratado. No modo genérico, imagem em outro formato não passa pelo OCR | Na função nova, mandar os blocos de imagem para `Midia.Processar` |
| Volta sem streaming | Os campos `text`, `content` e `input` do JSON são desmascarados | Acrescentar os campos do formato novo em `desmascararJSONResposta` (`resposta.go`) |
| Volta com streaming | **Não é desmascarada**: você veria os pseudônimos. Nada vaza | Em `resposta.go`, tratar os eventos do formato novo como `desmascararSSE` faz: achar o campo que traz o texto e passá-lo por `mask.Fluxo`, que cuida do pseudônimo cortado entre dois pedaços |
| Mais de uma API ao mesmo tempo | Não: há um `upstream` só | Escolher o destino pelo caminho da requisição em `proxy.go` |

O pacote `mask` não depende do formato de nenhuma API: recebe texto e devolve texto. Todo o
trabalho de uma API nova fica em `internal/proxy`.

O que testar ao acoplar: nada real chega à API, a resposta volta exatamente real, e as
mensagens antigas chegam idênticas de uma requisição para a outra (o cache depende disso).
O `estresse_test.go` do proxy faz essas três conferências e serve de modelo.

## Acrescentar um detector

1. Escreva a função num dos `detectores_*.go` (ou num arquivo novo). Ela recebe o texto e
   chama `add(início, fim, "tipo")` para cada trecho achado.
2. Chame-a em `detectarBase` (`detectores_formato.go`), atrás de um teste barato que evite
   rodar em texto que não tem o "ingrediente" (uma palavra-chave, um caractere).
3. O pseudônimo sai como `TIPO-identificador`. Para outro formato, acrescente um caso em
   `Pseudonimo` (`pseudonimos.go`).
4. Se o valor deve ser lembrado depois de mascarado uma vez, acrescente o tipo em
   `contextuais` (`memoria_conhecidos.go`).
5. Testes: um caso que pega e um que não pega em `detectores_test.go`, e o caso em
   `casosDeCorte` (`detectar_test.go`), que confere o dado em todas as posições de corte de
   um texto grande.
6. Rode `go test ./...` e os benchmarks (`go test ./internal/mask -bench Frio`) para ver se
   não pesou.
7. Os leitores só leem o texto (não consultam o que foi aprendido): num texto grande eles rodam
   em paralelo e os achados são aplicados depois, na ordem do registro. Um leitor que dependa
   do aprendido (como as listas) roda à parte, depois dos outros.
8. Nunca procure o começo ou o fim da linha sem limite a cada ocorrência
   (`strings.LastIndexByte(s[:i], '\n')`): numa linha única longa (JSON minificado) isso vira
   quadrático. Use `inicioLinhaJ`/`fimLinhaJ` (`memoria_conhecidos.go`, janela de 1000 caracteres).
   `TestLinhaLongaLinear` e os benchmarks `-bench Linha` conferem.

## Como foi testado

| Teste | O que faz | Como rodar |
|---|---|---|
| Suíte automática | Cada detector, ida e volta, streaming, e cada tipo de dado em todas as posições de corte de um texto grande | `make test` |
| Variações dos leitores | Cada caso de cada leitor em todas as variações de escrita (caixa, espaços, citações, comentários, pontuação, 2–4 partes, `$ # -` e acento, vários na linha, dentro de JSON), mais prosa com SQL entre crases | `go test ./internal/mask -run Variacoes` |
| Carga e estresse | Memória com 1,2 GB de texto, 120 mil valores aprendidos, conversas em paralelo, API fora do ar, requisição de 32 MB | `LLM_DLP_ESTRESSE=1 go test ./... -run Estresse` |
| Sessões reais | Reproduz uma sessão antiga do Claude Code pelo llm-dlp, contra uma API falsa local (nada sai da máquina), e confere recusas, ida e volta, cache e tempo | `llm-dlp simular SESSAO.jsonl` |
| Matriz de transporte | Cada conteúdo (catálogo, conexão, config do Snowflake, manifesto, buckets, ORM) em 15 desenhos (CSV, `;`, TAB, `\|`, caixa, largura fixa, bordas, tuplas, HTML, vertical, JSON, JSONL, `k=v`, coluna solta, `uniq -c`) e 11 transportes (cru, Read, `cat -n`, `grep -n`, diff, markdown, JSON escapado, ANSI, CRLF, citação, log): os mesmos nomes mascarados em todas as células (meta 95%) e ida e volta exata; os negativos não têm nada mascarado em célula nenhuma | `go test ./internal/mask -run Matriz -v` |
| Fuzz | Mapa da normalização, ida e volta pelos leitores e a varredura do SQL contra as regex que ela substituiu | `go test ./internal/mask -run '^$' -fuzz FuzzIdaVolta -fuzztime 60s` (e `FuzzNormalizar`, `FuzzVarreduraSQL`) |
| Falso positivo | Por regra: achados num corpus público (código e documentação de terceiros) e, nas sessões reais, quantos nomes aprendidos existem no corpus público (limite 2%). Só contagens; chave e configuração de teste | `LLM_DLP_CORPUS=dir1:dir2 go test ./internal/mask -run MedirCorpus -v` e, com `LLM_DLP_SESSOES=~/.claude/projects`, `-run MedirSessoes` |
| Uso real | Claude Code de verdade, em cenários do dia a dia (CSV, planilha, segredos, log, texto colado, print, SQL, pesquisa na web, subagente), com dados fictícios e um espião gravando o que chega à API | Manual (ver abaixo) |

No teste de uso real, cada cenário roda duas vezes, direto e pelo llm-dlp, para comparar.
Resultado da última rodada: nenhum dos 179 valores sensíveis chegou à API, nenhuma resposta
mostrou pseudônimo, arquivos gravados com os valores reais, tokens de entrada −1,5% e +8 ms
por mensagem (mediana).

Para repetir esse teste, rode o Claude Code **num ambiente isolado** (por exemplo, com
`unshare` e `tmpfs` por cima da sua pasta pessoal), em que só a pasta de teste exista.
`--allowedTools` não basta: ele só acrescenta permissões, e as suas configurações globais
podem deixar o agente ler outros arquivos.

Durante o uso, `llm-dlp ultima` mostra as últimas mensagens exatamente como a API recebeu.
