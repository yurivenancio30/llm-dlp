# For developers

[← back to the README](../README.md) · [Português](pt-BR/desenvolvimento.md)

```bash
make build   # static binary at bin/llm-dlp
make test    # go vet and all the tests
make versao  # version and commit of the code

# load tests (minutes): memory with 1.2 GB of text, pathological inputs, 16 conversations
# in parallel, abandoned responses, API down, 32 MB request
LLM_DLP_ESTRESSE=1 go test ./... -run Estresse -v -timeout 60m
```

Versions and releasing: [versioning.md](versioning.md). How to contribute:
[CONTRIBUTING.md](../CONTRIBUTING.md).

## The code is in Portuguese

Identifiers, comments, command names and the program's messages are in Portuguese. The words
you will meet most often:

| In the code | Meaning |
|---|---|
| `mascarar`, `desmascarar` | to mask, to unmask |
| `leitor` | reader: recognizes names by the structure of one format |
| `freio` | guard: a condition that stops a rule from masking (or from learning) |
| `decisor`, `decisao` | decider, decision: rules that decide names looking at the whole text |
| `chamada` | the tool call (command and arguments) that produced an output |
| `conhecidos`, `vistos` | known values (in RAM), seen values (hashes on disk) |
| `enviados` | texts already sent, frozen so they go out identical |
| `rastreamento`, `anterioridade` | tracking (a name is decided where it enters), precedence (what the model wrote before any data is public) |
| `campo`, `objeto`, `termo` | field, internal resource (table, server...), registered term |
| `versao`, `instalar`, `parar` | version, install, stop |

## Repository map

```
cmd/llm-dlp/            the command-line program
internal/proxy/         the server that sits between Claude Code and the API
internal/mask/          detection and replacement with pseudonyms (depends on no API)
internal/mask/dados/    lists embedded in the binary (almost all generated)
internal/config/        reading of config.json
internal/ocr/           reading of text in images and PDFs (tesseract, poppler)
internal/versao/        the version number and the commit
install.sh              installer: downloads the release binary and runs the setup
.goreleaser.yaml        how the release archives are built
scripts/                release.sh and notas-da-versao.sh (used by make release)
.github/workflows/      tests on every push (ci.yml) and release publishing (release.yml)
docs/                   this documentation, in English
docs/pt-BR/             the same documentation, in Portuguese
CHANGELOG.md            what changed in each version
CONTRIBUTING.md         how to contribute (commits, tests, documentation)
SECURITY.md             how to report a vulnerability
```

### cmd/llm-dlp and internal/proxy

```
cmd/llm-dlp/
  main.go               entry point and list of commands
  servico.go            servir, supervisionar, garantir, verificar, status, parar
  instalar.go           instalar, desinstalar and the lock
  conferir.go           simular: replays an old session against a fake API
  ferramentas.go        importar-pessoas, testar, testar-midia, medir, colunas
  emergencia.go         emergency mode (requires sudo)

internal/proxy/
  proxy.go              receives the request, forwards it, returns the response
  requisicao.go         outbound: masks the request body (with the hint of the command of each result)
  resposta.go           inbound: unmasks the response (streaming or whole)
  anterioridade.go      what the model wrote before any data is public
  midia.go              images and PDFs (OCR and black bars)
```

### internal/mask

It is a single package (in Go a folder is a package, and the pieces use each other's internal
functions). It is organized by the **file name prefix**: each family does one thing.

| Prefix | What it does |
|---|---|
| (no prefix) | The main path: entry, detection, replacement and the way back |
| `detectores_` | Personal data and secrets recognized by **format** |
| `campos_` | Personal data recognized by the **field name** next to it |
| `leitor_` | Internal resource name recognized by the **structure** of the text (one file per format) |
| `objetos` | The common base of the readers: types, pseudonym, guards |
| `chamada_` | What the tool call says about its output |
| `decisao`, `decisor_` | Rules that decide names looking at the whole text |
| `memoria_` | Everything that is remembered, in RAM or on disk (see the table below) |
| `vocab_` | Public vocabularies and the reading of the lists in `dados/` |

```
main path
  masker.go                 the Masker type and its construction
  mascarar.go               Mascarar: the entry point, with the result cache; Lote freezes what went out
  detectar.go               Detectar: gathers the detectors; large text goes in pieces
  normalizacao.go           strips the transport (line number, grep, diff, ANSI, box, escaped JSON)
  pseudonimos.go            replaces the findings with pseudonyms
  desmascarar.go            swaps back, including in a response that arrives in chunks
  chave.go                  the secret key and the identifiers derived from it
  pessoas.go                registry of people (names, e-mails, codes), as hashes only

personal data and secrets
  detectores_formato.go     e-mail, IP, hostname, CPF, CNPJ, phone, card, tokens
  detectores_senhas.go      common passwords
  detectores_extras.go      HTTP header, IBAN, court case, documents with a word nearby
  detectores_validadores.go check digits (CPF, CNPJ, voter ID, IBAN...)
  campos_vocabulario.go     which field names hold which data
  campos.go                 "field: value", JSON, SQL INSERT, XML
  campos_tabelas.go         CSV, markdown, terminal, spreadsheet

internal resource names (server, database, table, column, service, bucket...)
  objetos.go                types, pseudonym, guarded learning
  objetos_listas.go         homogeneous lists and qualified name with a known part (after the readers)
  leitores.go               registry of all the readers (fixed and from the configuration)
  leitor_sql.go             SQL and DDL, errors that quote objects
  leitor_sql_freios.go      SQL quoted in prose and in a code comment is not a statement
  leitor_tabela.go          tables in any layout (CSV, fixed width, box, tuples, HTML, vertical)
  leitor_saida_cli.go       table of kubectl get, docker ps, helm list
  leitor_esquema.go         name + data type (dtypes, printSchema, Arrow, protobuf)
  leitor_conexao.go         connection strings, database URIs, DSN, tnsnames, URNs, dbt, Airflow
  leitor_conexao_freios.go  model name in the database URI and in tnsnames
  leitor_yaml.go            YAML/JSON engine by path and the dispatcher of the families
  leitor_kubernetes.go      Kubernetes, Helm, images, service DNS, env as a list
  leitor_iac.go             Terraform/HCL, Bicep, ARM, CloudFormation
  leitor_ansible.go         Ansible inventory
  leitor_ci.go              CI pipelines and Jenkinsfile
  leitor_chave_valor.go     generic key-value (YAML, JSON, INI, .env, .properties, XML, --option)
  leitor_enderecos.go       internal hosts, storage and queues, git, network user, home folder
  leitor_caminhos.go        paths outside the home folder, other drives, UNC
  leitor_nuvem.go           AWS ARN, Azure IDs, GCP resources, cloud URLs
  leitor_nuvem_hosts.go     managed service hosts (RDS, Redshift, S3, Azure)
  leitor_pacotes.go         go.mod, Maven/Gradle groupId, npm scope
  leitor_codigo.go          code in any language: the name next to it and the called function give the type
  leitor_traceback.go       traceback: the path of the line says whether the code is the client's or third-party
  leitor_cli.go             command line
  leitor_ip.go              public IP and IPv6 (optional)
  leitor_termos.go          company terms embedded in identifiers

what the conversation says
  chamada.go                what the proxy knows about a tool call (request, arguments, echo)
  chamada_comando.go        the command says what the output is (SELECT, cut, listing)
  chamada_decisor.go        provenance and identity in command output
  decisao.go                names decided per text, deciders, record of who wrote what
  decisor_lexico.go         lexical rule for code and configuration

memory
  memoria_conhecidos.go     values already masked, recognized later anywhere (RAM)
  memoria_vistos.go         the same values on disk, as hashes only
  memoria_enviados.go       what each text already sent carried (to go out identical in resends)
  memoria_conversa.go       names decided in the texts of a request hold for the others
  memoria_conversa_freios.go  where the conversation memory does not replace (program, comment, prose)
  memoria_rastreamento.go   a name is decided where it enters and holds everywhere
  memoria_software.go       public software with proof, and shadowing

vocabulary
  vocab_dev.go              type words, internal suffixes, public names, value with no owner
  vocab_tipos.go            data types of the languages (SQL, pandas, Arrow, Spark...)
  vocab_palavras.go         common words and dictionary (reads dados/palavras_*.txt)
  vocab_gerado.go           reads the other lists in dados/
```

### internal/mask/dados

| File | What it is | Where it comes from |
|---|---|---|
| `palavras_comuns.txt` | The 20 thousand most frequent words of pt and en | FrequencyWords (CC BY-SA 4.0) |
| `palavras_dicionario.txt` | The 50 thousand most frequent, minus the common ones | Generated: `TestGerarDicionario` |
| `ref_publica.txt` | Words frequent as a resource name in public material | Generated: `TestGerarRefPublica` |
| `tipos_linguagem.txt` | Data types that public Go code uses as a field type | Generated: `TestGerarRefPublica` |
| `imagens_oficiais.txt` | Docker Official Images | Generated: `TestGerarRefPublica` (from `docker-library/docs`) |
| `software_publico.txt`, `fornecedores.txt` | Name and owner of the GitHub repositories with 3000 stars or more | Generated: `TestGerarSoftwarePublico` |
| `vocab_sql.txt` | SQL reserved words and system objects | Written by hand, with the sources |

The generators are in `vocab_gerar_test.go` and only run with the environment variables
described there (see [Derived public reference](structures.md#derived-public-reference)). An
outdated list never lets anything through: it only fails to recognize a public name, which
stays masked.

### Tests

The tests live next to the code, ending in `_test.go` (it is the Go convention, and it is
what allows testing internal functions). The test of a file has the same name (`campos.go`
and `campos_test.go`). The ones that span several files:

| File | What it checks |
|---|---|
| `casos_publicos_test.go` | Real formats taken from public repositories, with invented names: what must go and what must stay. Every rule that leaves a name in the clear has a case here |
| `leitores_*_test.go` | The readers by area (data, DevOps, development, code) |
| `variacoes_test.go`, `regras_gerais_test.go` | Each case in every spelling variation; rules that hold for all the readers |
| `matriz_test.go`, `matriz_transporte_test.go` | The same content in every layout and transport |
| `negativos_test.go`, `freios_referencia_test.go` | What must not be masked |
| `fuzz_test.go`, `leitores_fuzz_test.go` | Fuzzing of the normalization and of the round trip |
| `benchmark_test.go`, `estresse_test.go` | Time and load |
| `medicao_test.go` | Measurements on a public corpus and on real sessions (counts only; see below) |
| `ajuda_test.go` | Test helper functions |

## What is stored

Everything llm-dlp remembers, where it lives and how far it grows. No real value goes to
disk: there you only find hashes, positions and pseudonyms.

| What | What it is for | Where | How long it lasts | Cap |
|---|---|---|---|---|
| Result cache (`memo`, `mascarar.go`) | Not examining again the text that was already examined (the whole conversation is resent with every message) | RAM | Until it fills up | 64 MB of text, in two generations |
| Sent (`memoria_enviados.go`) | The text that already went out goes out the same in resends (the API cache depends on it) | RAM and `enviados.log` | Until the configuration or the version changes | 64 MB in RAM; 400 thousand texts on disk |
| Known (`memoria_conhecidos.go`) | A value masked with a hint next to it is recognized later without the hint | RAM (real value) | While llm-dlp is running | 50 thousand values (the older half is dropped) |
| Seen (`memoria_vistos.go`) | The known values, so they hold after a restart | `vistos.json` (hash only) | 90 days without use, for a resource name | 500 thousand values and 500 thousand names |
| People (`pessoas.go`) | Registered names, e-mails and codes | `pessoas.json` (hash only) | Until you import again | Whatever you import |
| Conversation memory (`memoria_conversa.go`) | A name decided in one text holds in the other texts of the same request | RAM | One request (recomputed for each one) | The texts of the request |
| Who wrote it (`escritos`, `decisao.go`) | Knowing what the model wrote, so it is not treated as data | RAM | Until it fills up | 20 thousand texts, in two generations |
| Weak evidence (`fracos`, `objetos.go`) | A name seen by a weak rule only holds with a second one | RAM | While llm-dlp is running | 200 thousand names |
| Proven software and shadowing (`memoria_software.go`) | Not spreading the default name of a public software through the conversation | RAM | While llm-dlp is running | The software list (measured: 3 names after 60 thousand texts) |
| Label class (`classes`, `campos_vocabulario.go`) | Not classifying the same field name again | RAM | Until it fills up | 20 thousand labels |
| Precedence (`internal/proxy/anterioridade.go`) | What the model wrote before any data is public | RAM | One request | The conversation |
| Pseudonym → real table (`desmascarar.go`) | Undoing the replacement in the response | RAM | One request | The conversation |

What this costs in practice is in
[How much of the machine it uses](how-it-works.md#how-much-of-the-machine-it-uses).

## How a text is masked

```
proxy.ServeHTTP                      receives the request
 ├ mascararCorpo (requisicao.go)     1st pass: gathers the texts and masks them ahead (Aquecer),
 │                                   so every value learned in the request already holds when
 │                                   the request is assembled
 │                                   2nd pass: calls Lote.Mascarar on each text, in order
 │  └ Lote.Mascarar (mascarar.go)    did it already go out at this point of the conversation
 │                                   (memory or enviados.log)? it goes out the same. The position
 │                                   is the hash of the block (everything before it and the block
 │                                   itself) plus the order of the text in it
 │                                   is it in memory and still valid? return it. otherwise:
 │     ├ Detectar (detectar.go)      runs the detectors and gathers the findings
 │     │  ├ detectores_*.go          format, passwords, extras
 │     │  ├ campos*.go               field name
 │     │  └ memoria_conhecidos.go    values already seen
 │     └ aplicar (pseudonimos.go)    replaces each finding with the pseudonym
 ├ sends to the API and freezes the texts (Lote.Congelar)
 └ desmascararSSE (resposta.go)      swaps the pseudonyms back, chunk by chunk
```

## Plugging in another LLM API

Today llm-dlp understands the format of the Anthropic API (`/v1/messages`). What already
works and what is missing for another format (OpenAI, Gemini):

| Part | Today, with another API | What to do |
|---|---|---|
| Client | Any client that accepts changing the API address can point to llm-dlp | Set the client's variable (like Claude Code's `ANTHROPIC_BASE_URL`) and `upstream` in `config.json` |
| Outbound (request) | A path other than `/v1/messages` falls into the generic mode: **every text in the JSON is masked**. It is safe, but masks even fields that did not need it | In `requisicao.go`, write a function like `requisicaoAnthropic` for the new format: which fields carry user text, which may never change, where the images come. Pick the function by the path in `mascararCorpo` |
| Images | Only the Anthropic format is handled. In the generic mode, an image in another format does not go through OCR | In the new function, send the image blocks to `Midia.Processar` |
| Inbound without streaming | The `text`, `content` and `input` fields of the JSON are unmasked | Add the fields of the new format in `desmascararJSONResposta` (`resposta.go`) |
| Inbound with streaming | **It is not unmasked**: you would see the pseudonyms. Nothing leaks | In `resposta.go`, handle the events of the new format the way `desmascararSSE` does: find the field that carries the text and pass it through `mask.Fluxo`, which takes care of a pseudonym split between two chunks |
| More than one API at the same time | No: there is a single `upstream` | Choose the destination by the request path in `proxy.go` |

The `mask` package depends on no API format: it receives text and returns text. All the work
for a new API stays in `internal/proxy`.

What to test when plugging one in: nothing real reaches the API, the response comes back
exactly real, and the old messages arrive identical from one request to the next (the cache
depends on it). The proxy's `estresse_test.go` makes these three checks and serves as a
model.

## Adding a detector

1. Write the function in one of the `detectores_*.go` files (or in a new file). It receives
   the text and calls `add(start, end, "type")` for each span found.
2. Call it in `detectarBase` (`detectores_formato.go`), behind a cheap test that avoids
   running on text that does not have the "ingredient" (a keyword, a character).
3. The pseudonym comes out as `TYPE-identifier`. For another format, add a case in
   `Pseudonimo` (`pseudonimos.go`).
4. If the value must be remembered after being masked once, add the type to `contextuais`
   (`memoria_conhecidos.go`).
5. Tests: one case that catches and one that does not in `detectores_test.go`, and the case
   in `casosDeCorte` (`detectar_test.go`), which checks the data at every cut position of a
   large text.
6. Run `go test ./...` and the benchmarks (`go test ./internal/mask -bench Frio`) to see
   whether it got heavier.
7. The readers only read the text (they do not consult what was learned): on a large text
   they run in parallel and the findings are applied afterwards, in registration order. A
   reader that depends on what was learned (like the lists) runs separately, after the
   others.
8. Never search for the beginning or the end of the line without a limit at every occurrence
   (`strings.LastIndexByte(s[:i], '\n')`): on a long single line (minified JSON) that becomes
   quadratic. Use `inicioLinhaJ`/`fimLinhaJ` (`memoria_conhecidos.go`, 1000-character
   window). `TestLinhaLongaLinear` and the `-bench Linha` benchmarks check it.

## How it was tested

| Test | What it does | How to run |
|---|---|---|
| Automatic suite | Each detector, round trip, streaming, and each kind of data at every cut position of a large text | `make test` |
| Reader variations | Each case of each reader in every spelling variation (letter case, spaces, quoting, comments, punctuation, 2–4 parts, `$ # -` and accents, several on a line, inside JSON), plus prose with SQL between backticks | `go test ./internal/mask -run Variacoes` |
| Load and stress | Memory with 1.2 GB of text, 120 thousand learned values, conversations in parallel, API down, 32 MB request | `LLM_DLP_ESTRESSE=1 go test ./... -run Estresse` |
| Real sessions | Replays an old Claude Code session through llm-dlp, against a local fake API (nothing leaves the machine), and checks refusals, round trip, cache and time | `llm-dlp simular SESSAO.jsonl` |
| Transport matrix | Each content (catalog, connection, Snowflake config, manifest, buckets, ORM) in 15 layouts (CSV, `;`, TAB, `\|`, box, fixed width, borders, tuples, HTML, vertical, JSON, JSONL, `k=v`, loose column, `uniq -c`) and 11 transports (raw, Read, `cat -n`, `grep -n`, diff, markdown, escaped JSON, ANSI, CRLF, quotation, log): the same names masked in every cell (target 95%) and an exact round trip; the negatives have nothing masked in any cell | `go test ./internal/mask -run Matriz -v` |
| Fuzzing | The normalization map, the round trip through the readers and the SQL scan against the regexes it replaced | `go test ./internal/mask -run '^$' -fuzz FuzzIdaVolta -fuzztime 60s` (and `FuzzNormalizar`, `FuzzVarreduraSQL`) |
| False positives | Per rule: findings in a public corpus (third-party code and documentation) and, in real sessions, how many learned names exist in the public corpus (limit 2%). Counts only; test key and configuration | `LLM_DLP_CORPUS=dir1:dir2 go test ./internal/mask -run MedirCorpus -v` and, with `LLM_DLP_SESSOES=~/.claude/projects`, `-run MedirSessoes` |
| Real use | The actual Claude Code, in everyday scenarios (CSV, spreadsheet, secrets, log, pasted text, screenshot, SQL, web search, subagent), with fictitious data and a spy recording what reaches the API | Manual (see below) |

In the real-use test, each scenario runs twice, directly and through llm-dlp, to compare.
Result of the last round: none of the 179 sensitive values reached the API, no response
showed a pseudonym, files were written with the real values, input tokens −1.5% and +8 ms per
message (median).

To repeat this test, run Claude Code **in an isolated environment** (for example, with
`unshare` and `tmpfs` over your home folder), in which only the test folder exists.
`--allowedTools` is not enough: it only adds permissions, and your global settings may let
the agent read other files.

During use, `llm-dlp ultima` shows the last messages exactly as the API received them.
