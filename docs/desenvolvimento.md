# Para desenvolvedores

[← voltar ao README](../README.md)

```bash
make build   # binário estático em bin/llm-dlp
make test

# testes de carga (minutos): memória com 1,2 GB de texto, entradas patológicas, 16 conversas
# em paralelo, respostas abandonadas, API fora do ar, requisição de 32 MB
LLM_DLP_ESTRESSE=1 go test ./... -run Estresse -v -timeout 60m
```

## Mapa do código

```
cmd/llm-dlp/          o programa de linha de comando
  main.go               ponto de entrada e lista de comandos
  servico.go            servir, supervisionar, garantir, verificar, status, parar
  ferramentas.go        importar-pessoas, testar, testar-midia, medir, colunas
  instalar.go           instalar, desinstalar e a trava
  emergencia.go         modo emergência (exige sudo)

internal/config/      leitura do config.json

internal/proxy/       o servidor que fica entre o Claude Code e a API
  proxy.go              recebe a requisição, encaminha, devolve a resposta
  requisicao.go         ida: mascara o corpo da requisição
  resposta.go           volta: desmascara a resposta (streaming ou inteira)
  midia.go              imagens e PDFs (OCR e tarja preta)

internal/mask/        detecção e troca por pseudônimos
  masker.go             o tipo Masker e a sua construção
  mascarar.go           Mascarar: porta de entrada, com memória de resultados; Lote congela o que saiu
  detectar.go           Detectar: junta os detectores; texto grande vai em pedaços
  detectores_formato.go   e-mail, IP, hostname, CPF, CNPJ, telefone, cartão, tokens
  detectores_senhas.go    senhas comuns
  detectores_extras.go    cabeçalho HTTP, IBAN, processo, documentos com palavra por perto
  campos_vocabulario.go   nome do campo: que nomes guardam que dado
  campos.go               nome do campo: "campo: valor", JSON, SQL INSERT, XML
  campos_tabelas.go       nome do campo: CSV, markdown, terminal, planilha
  validadores.go        dígitos verificadores (CPF, CNPJ, título, IBAN...)
  conhecidos.go         valores já mascarados são reconhecidos depois (em memória)
  vistos.go             os mesmos valores em disco, só como hash
  enviados.go           o que cada texto já enviado levou, por ponto da conversa (para sair igual nos reenvios)
  pessoas.go            registro de pessoas (nomes, e-mails, códigos), só como hash
  chave.go              a chave secreta e os identificadores derivados dela
  pseudonimos.go        troca dos achados por pseudônimos
  desmascarar.go        troca de volta, inclusive em resposta que chega em pedaços

internal/ocr/         leitura de texto em imagem e PDF (tesseract, poppler)
internal/versao/      versão e commit (o Makefile grava o commit no binário)
```

Os testes ficam ao lado do código que testam, com o mesmo nome e `_test.go` no fim
(`campos.go` e `campos_test.go`): é a convenção do Go, e é o que permite testar funções
internas. À parte: `ajuda_test.go` (funções de apoio), `benchmark_test.go` (medições de
tempo) e `estresse_test.go` (testes de carga).

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
 │     │  └ conhecidos.go            valores já vistos
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
   `contextuais` (`conhecidos.go`).
5. Testes: um caso que pega e um que não pega em `detectores_test.go`, e o caso em
   `casosDeCorte` (`detectar_test.go`), que confere o dado em todas as posições de corte de
   um texto grande.
6. Rode `go test ./...` e os benchmarks (`go test ./internal/mask -bench Frio`) para ver se
   não pesou.

## Como foi testado

| Teste | O que faz | Como rodar |
|---|---|---|
| Suíte automática | Cada detector, ida e volta, streaming, e cada tipo de dado em todas as posições de corte de um texto grande | `make test` |
| Carga e estresse | Memória com 1,2 GB de texto, 120 mil valores aprendidos, conversas em paralelo, API fora do ar, requisição de 32 MB | `LLM_DLP_ESTRESSE=1 go test ./... -run Estresse` |
| Sessões reais | Reproduz uma sessão antiga do Claude Code pelo llm-dlp, contra uma API falsa local (nada sai da máquina), e confere recusas, ida e volta, cache e tempo | `llm-dlp simular SESSAO.jsonl` |
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
