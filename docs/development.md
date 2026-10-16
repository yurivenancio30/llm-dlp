# For developers

[← back to the README](../README.md) · [Português](pt-BR/development.md)

```bash
make build   # static binary at bin/llm-dlp
make test    # go vet and all the tests
make version  # version and commit of the code

# load tests (minutes): memory with 1.2 GB of text, pathological inputs, 16 conversations
# in parallel, abandoned responses, API down, 32 MB request
LLM_DLP_STRESS=1 go test ./... -run Stress -v -timeout 60m
```

Versions and releasing: [versioning.md](versioning.md). How to contribute:
[CONTRIBUTING.md](../.github/CONTRIBUTING.md).

## What stays in Portuguese

The code is in English: identifiers, comments, type and detector names, commands, messages
and tests. What stays in Portuguese is there on purpose:

| What | Why |
|---|---|
| The words the rules look for in the text (`senha`, `nome_cliente`, `matricula`, `rua`...) and the lists in `internal/mask/data/` | People write in Portuguese, and these words are what gets detected. Translating them would stop the masking |
| The Portuguese sample texts of the tests | They check exactly that detection |
| The HMAC domain of each type (`hmacDomain`, `internal/mask/old_names.go`) | Each pseudonym is derived from the name its type had up to 0.9. Renaming the domain would change every pseudonym of the type. `pseudonym_golden_test.go` fails if any pseudonym changes |
| The names one version uses to talk to another during an update (`servir`, `supervisionar`, `LLM_DLP_PORTA_FD`, `/__llm-dlp/saude`...; the list is in `swap.go`) | An update from 0.9 runs the 0.9 code, which only knows these names |
| Old names that are still read (`config.OldTypeNames`, the `config.json` fields before 0.9.0, `chave`, `pessoas.json`, `vistos.json`, the log lines up to 0.9) | What an older version wrote keeps working |

Some words you will meet in the code:

| In the code | Meaning |
|---|---|
| reader | recognizes names by the structure of one format |
| brake | a condition that stops a rule from masking (or from learning) |
| decider, decision | rules that decide names looking at the whole text |
| call | the tool call (command and arguments) that produced an output |
| known, seen | known values (in RAM), seen values (hashes on disk) |
| sent | texts already sent, frozen so they go out identical |
| tracing, priority | a name is decided where it enters; what the model wrote before any data is public |
| field, object, term | field name, internal resource (table, server...), registered term |

## Repository map

```
cmd/llm-dlp/            the command-line program
internal/proxy/         the server that sits between Claude Code and the API
internal/mask/          detection and replacement with pseudonyms (depends on no API)
internal/mask/data/     lists embedded in the binary (almost all generated)
internal/config/        reading of config.json (and of the names before 0.10.0), writing of some of its fields, and the data file names
internal/ocr/           reading of text in images and PDFs (tesseract, poppler)
internal/version/       the version number and the commit
install.sh              installer: downloads the release binary and runs the setup
.goreleaser.yaml        how the release archives are built
scripts/                release.sh and release-notes.sh (used by make release)
.github/workflows/      tests on every push (ci.yml) and release publishing (release.yml)
docs/                   this documentation, in English
docs/pt-BR/             the same documentation, in Portuguese (README.md included)
CHANGELOG.md            what changed in each version
.github/CONTRIBUTING.md how to contribute (commits, tests, documentation)
.github/SECURITY.md     how to report a vulnerability
```

### cmd/llm-dlp and internal/proxy

```
cmd/llm-dlp/
  main.go       entry point, list of commands and their names before 0.9.0
  service.go    serve, supervise, ensure, check, status, stop
  swap.go       the supervisor: holds the port and lends it to the proxy; swap on SIGHUP
  update.go     update (download, checksum, swap, going back) and reload
  install.go    install, uninstall, the lock and the hooks
  inspect.go    last (what the API received) and simulate (replays an old session against a fake API)
  audit.go      audit: the summary of the log over a period
  tools.go      import-people, test, test-media, measure, columns
  emergency.go  emergency mode (requires sudo)

internal/proxy/
  proxy.go     receives the request, forwards it, returns the response
  request.go   outbound: masks the request body (with the hint of the command of each result)
  response.go  inbound: unmasks the response (streaming or whole)
  priority.go  what the model wrote before any data is public
  media.go     images and PDFs (OCR and black bars)
  local.go     who can talk to the local port (DNS rebinding, other users)
  gemini.go, openai.go, responses.go
               the Gemini, Chat Completions and Responses formats, seen in the Anthropic shape
```

### internal/mask

It is a single package (in Go a folder is a package, and the pieces use each other's internal
functions). It is organized by the **file name prefix**: each family does one thing.

| Prefix | What it does |
|---|---|
| (no prefix) | The main path: entry, detection, replacement and the way back |
| `detectors_` | Personal data and secrets recognized by **format** |
| `fields_` | Personal data recognized by the **field name** next to it |
| `reader_` | Internal resource name recognized by the **structure** of the text (one file per format) |
| `objects` | The common base of the readers: types, pseudonym, guards |
| `call_` | What the tool call says about its output |
| `decision`, `*_decider` | Rules that decide names looking at the whole text |
| `memory_` | Everything that is remembered, in RAM or on disk (see the table below) |
| `vocab_` | Public vocabularies and the reading of the lists in `data/` |

```
main path
  masker.go         the Masker type and its construction
  mask.go           Mask: the entry point, with the result cache; Batch freezes what went out
  detect.go         Detect: gathers the detectors; large text goes in pieces
  normalization.go  strips the transport (line number, grep, diff, ANSI, box, escaped JSON)
  pseudonyms.go     replaces the findings with pseudonyms
  unmask.go         swaps back, including in a response that arrives in chunks
  key.go            the secret key and the identifiers derived from it
  people.go         registry of people (names, e-mails, codes), as hashes only
  old_names.go      the type names up to 0.9 and the HMAC domain of each type (see above)

personal data and secrets
  detectors_format.go      e-mail, IP, hostname, CPF, CNPJ, phone, card, tokens
  detectors_passwords.go   common passwords
  detectors_auth.go        PIN, CVV, one-time and recovery codes, BIP-39 recovery phrase (data/bip39_*.txt)
  detectors_extra.go       HTTP header, IBAN, court case, documents with a word nearby
  detectors_network.go     internal IPv4 beyond the dotted form: shared range, hyphens, reverse DNS
  detectors_fiscal.go      NF-e access key, PIX copy-and-paste code (BR Code)
  detectors_br.go          bank slip and bill, tracking code, certificate number, ICD-10 code
  detectors_sensitive.go   LGPD sensitive data in the text (official value lists), fingerprint template
  detectors_validators.go  check digits (CPF, CNPJ, voter ID, IBAN...)
  fields_vocabulary.go     which field names hold which data
  fields.go                "field: value", JSON, SQL INSERT, XML
  fields_tables.go         CSV, markdown, terminal, spreadsheet

internal resource names (server, database, table, column, service, bucket...)
  objects.go                   types, pseudonym, guarded learning
  objects_lists.go             homogeneous lists and qualified name with a known part (after the readers)
  readers.go                   registry of all the readers (fixed and from the configuration)
  reader_sql.go                SQL and DDL, errors that quote objects
  reader_sql_brakes.go         SQL quoted in prose and in a code comment is not a statement
  reader_table.go              tables in any layout (CSV, fixed width, box, tuples, HTML, vertical)
  reader_cli_output.go         table of kubectl get, docker ps, helm list
  reader_schema.go             name + data type (dtypes, printSchema, Arrow, protobuf)
  reader_connection.go         connection strings, database URIs, DSN, tnsnames, URNs, dbt, Airflow
  reader_connection_brakes.go  model name in the database URI and in tnsnames
  reader_yaml.go               YAML/JSON engine by path and the dispatcher of the families
  reader_kubernetes.go         Kubernetes, Helm, images, service DNS, env as a list
  reader_iac.go                Terraform/HCL, Bicep, ARM, CloudFormation
  reader_ansible.go            Ansible inventory
  reader_ci.go                 CI pipelines and Jenkinsfile
  reader_key_value.go          generic key-value (YAML, JSON, INI, .env, .properties, XML, --option)
  reader_addresses.go          internal hosts, storage and queues, git, network user, home folder
  reader_paths.go              paths outside the home folder, other drives, UNC
  reader_cloud.go              AWS ARN, Azure IDs, GCP resources, cloud URLs
  reader_cloud_hosts.go        managed service hosts (RDS, Redshift, S3, Azure)
  reader_packages.go           go.mod, Maven/Gradle groupId, npm scope
  reader_code.go               code in any language: the name next to it and the called function give the type
  reader_traceback.go          traceback: the path of the line says whether the code is the client's or third-party
  reader_cli.go                command line
  reader_ip.go                 internal IPv6 (always), public IP (optional)
  reader_terms.go              company terms embedded in identifiers

what the conversation says
  call.go             what the proxy knows about a tool call (request, arguments, echo)
  call_command.go     the command says what the output is (SELECT, cut, listing)
  call_decider.go     provenance and identity in command output
  decision.go         names decided per text, deciders, record of who wrote what
  lexical_decider.go  lexical rule for code and configuration

memory
  memory_known.go                values already masked, recognized later anywhere (RAM)
  memory_seen.go                 the same values on disk, as hashes only
  memory_sent.go                 what each text already sent carried (to go out identical in resends)
  memory_conversation.go         names decided in the texts of a request hold for the others
  memory_conversation_brakes.go  where the conversation memory does not replace (program, comment, prose)
  memory_tracing.go              a name is decided where it enters and holds everywhere
  memory_software.go             public software with proof, and shadowing

vocabulary
  vocab_dev.go        type words, internal suffixes, public names, value with no owner
  vocab_types.go      data types of the languages (SQL, pandas, Arrow, Spark...)
  vocab_words.go      common words and dictionary (reads data/*_words.txt)
  vocab_generated.go  reads the other lists in data/
```

### internal/mask/data

| File | What it is | Where it comes from |
|---|---|---|
| `common_words.txt` | The 20 thousand most frequent words of pt and en | FrequencyWords (CC BY-SA 4.0) |
| `dictionary_words.txt` | The 50 thousand most frequent, minus the common ones | Generated: `TestGenerateDictionary` |
| `public_ref.txt` | Words frequent as a resource name in public material | Generated: `TestGeneratePublicRef` |
| `language_types.txt` | Data types that public Go code uses as a field type | Generated: `TestGeneratePublicRef` |
| `official_images.txt` | Docker Official Images | Generated: `TestGeneratePublicRef` (from `docker-library/docs`) |
| `public_software.txt`, `vendors.txt` | Name and owner of the GitHub repositories with 3000 stars or more | Generated: `TestGeneratePublicSoftware` |
| `vocab_sql.txt` | SQL reserved words and system objects | Written by hand, with the sources |

The generators are in `vocab_generate_test.go` and only run with the environment variables
described there (see [Derived public reference](structures.md#derived-public-reference)). An
outdated list never lets anything through: it only fails to recognize a public name, which
stays masked.

### Tests

The tests live next to the code, ending in `_test.go` (it is the Go convention, and it is
what allows testing internal functions). The test of a file has the same name (`fields.go`
and `fields_test.go`). The ones that span several files:

| File | What it checks |
|---|---|
| `public_cases_test.go` | Real formats taken from public repositories, with invented names: what must go and what must stay. Every rule that leaves a name in the clear has a case here |
| `readers_*_test.go` | The readers by area (data, DevOps, development, code) |
| `variations_test.go`, `general_rules_test.go` | Each case in every spelling variation; rules that hold for all the readers |
| `structure_shapes_test.go` | A reader only reads a name where the structure is the one it reads (labels, calls, the end of a table, CREATE TABLE in a sentence, a folder of a path); each case with the real name next to it still masked |
| `matrix_test.go`, `matrix_transport_test.go` | The same content in every layout and transport |
| `negatives_test.go`, `brakes_reference_test.go` | What must not be masked |
| `fuzz_test.go`, `readers_fuzz_test.go` | Fuzzing of the normalization and of the round trip |
| `benchmark_test.go`, `stress_test.go` | Time and load |
| `measurement_test.go` | Measurements on a public corpus and on real sessions (counts only; see below) |
| `pseudonym_golden_test.go` | The identifier of each pseudonym and the pseudonym of each resource name stay the same from one version to the next (`testdata/pseudonyms.golden`; rewrite it with `LLM_DLP_GOLDEN=1` only when the change is intended, since it changes what every user already sent) |
| `helpers_test.go` | Test helper functions |

## What is stored

Everything llm-dlp remembers, where it lives and how far it grows. No real value goes to
disk: there you only find hashes, positions and pseudonyms.

| What | What it is for | Where | How long it lasts | Cap |
|---|---|---|---|---|
| Result cache (`memo`, `mask.go`) | Not examining again the text that was already examined (the whole conversation is resent with every message) | RAM | Until it fills up | 64 MB of text, in two generations |
| Sent (`memory_sent.go`) | The text that already went out goes out the same in resends (the API cache depends on it) | RAM and `sent.log` | Until the configuration or the version changes | 64 MB in RAM; 400 thousand texts on disk |
| Known (`memory_known.go`) | A value masked with a hint next to it is recognized later without the hint | RAM (real value) | While llm-dlp is running | 50 thousand values (the older half is dropped) |
| Seen (`memory_seen.go`) | The known values, so they hold after a restart | `seen.json` (hash only) | 90 days without use, for a resource name | 500 thousand values and 500 thousand names |
| People (`people.go`) | Registered names, e-mails and codes | `people.json` (hash only) | Until you import again | Whatever you import |
| Conversation memory (`memory_conversation.go`) | A name decided in one text holds in the other texts of the same request | RAM | One request (recomputed for each one) | The texts of the request |
| Who wrote it (`writtenSet`, `decision.go`) | Knowing what the model wrote, so it is not treated as data | RAM | Until it fills up | 20 thousand texts, in two generations |
| Weak evidence (`weakSet`, `objects.go`) | A name seen by a weak rule only holds with a second one | RAM | While llm-dlp is running | 200 thousand names |
| Proven software and shadowing (`memory_software.go`) | Not spreading the default name of a public software through the conversation | RAM | While llm-dlp is running | The software list (measured: 3 names after 60 thousand texts) |
| Label class (`classes`, `fields_vocabulary.go`) | Not classifying the same field name again | RAM | Until it fills up | 20 thousand labels |
| Precedence (`internal/proxy/priority.go`) | What the model wrote before any data is public | RAM | One request | The conversation |
| Pseudonym → real table (`unmask.go`) | Undoing the replacement in the response | RAM | One request | The conversation |

What this costs in practice is in
[How much of the machine it uses](how-it-works.md#how-much-of-the-machine-it-uses).

## How a text is masked

```
proxy.ServeHTTP                      receives the request
 ├ maskBody (request.go)             1st pass: gathers the texts and masks them ahead (Warm),
 │                                   so every value learned in the request already holds when
 │                                   the request is assembled
 │                                   2nd pass: calls Batch.Mask on each text, in order
 │  └ Batch.Mask (mask.go)           did it already go out at this point of the conversation
 │                                   (memory or sent.log)? it goes out the same. The position
 │                                   is the hash of the block (everything before it and the block
 │                                   itself) plus the order of the text in it
 │                                   is it in memory and still valid? return it. otherwise:
 │     ├ Detect (detect.go)          runs the detectors and gathers the findings
 │     │  ├ detectors_*.go           format, passwords, extras
 │     │  ├ fields*.go               field name
 │     │  └ memory_known.go          values already seen
 │     └ apply (pseudonyms.go)       replaces each finding with the pseudonym
 ├ sends to the API and freezes the texts (Batch.Freeze)
 └ unmaskSSE (response.go)           swaps the pseudonyms back, chunk by chunk
```

## Plugging in another LLM API

llm-dlp understands the format of the Anthropic API (`/v1/messages`), that of Gemini
(`generateContent`, in `gemini.go`, also inside the Code Assist envelope of the Gemini CLI with
a Google account, `codeAssistBridge`) and those of OpenAI (Chat Completions, in `openai.go`, and
Responses, in `responses.go`, the one Codex uses). The others are the model for the next: the
request did not get a masker of its own, it is **seen in the Anthropic shape** (`geminiBridge`,
`openAIBridge`, `responsesBridge`) and goes through the same path, so the conversation memory,
the command hints, the freezing and the images work the same; only the way back
(`geminiReturn`, `openAIReturn`, `responsesReturn`) is written for the format. The code of a
format only runs for requests on its own path, and `isolation_test.go` checks that a Claude
Code request goes out byte for byte the same with or without it. The destination is chosen
by the path (`Proxy.destination`, with the `providers` of the configuration).

What a format with no reader gets today, and what to do to give it one (`responses.go` is
the most recent example to follow):

| Part | Today, with no reader | What to do |
|---|---|---|
| Address | Any API can be served under `/<name>`, with a provider in `providers` (`format` and `upstream`) | For a provider that comes built in, add it to `config.DefaultProviders`. If the format is recognized by the path (like `/chat/completions` and `/responses`), add it to `Proxy.destination` |
| Outbound (request) | Generic mode: **every text in the JSON is masked**. It is safe, but masks even fields that did not need it | Write `xBridge(v any) (map[string]any, func(), bool)` in a file of its own: a view of the request in the Anthropic shape (`system`, `messages`, `tools`) whose texts are written back to the original JSON by the returned function. Register it in the map of `maskBody` (`request.go`). Fields that may never change go to `bridgeUntouchableKeys` |
| Images and PDFs | In the generic mode they do not go through OCR | In the bridge, show them as `image` and `document` blocks, as `geminiBridge` does |
| Inbound without streaming | The `text`, `content` and `input` fields of the JSON are unmasked | Write `unmaskXJSON` and choose it by `format` in `proxy.go` |
| Inbound with streaming | **It is not unmasked**: you would see the pseudonyms. Nothing leaks | Write `unmaskSSEX`, with a `xReturn` that passes each text through its own `mask.Stream` (it takes care of a pseudonym split between two chunks), and choose it by `format` in `proxy.go` |
| `llm-dlp last` | Does not show the conversation | Add the `format` to the list that keeps the last request (`proxy.go`) and read its messages in `lastMessages` (`cmd/llm-dlp/inspect.go`) |
| Isolation | — | Add the format to `isolation_test.go`: malformed requests and responses, and a Claude Code request that stays the same |

The `mask` package depends on no API format: it receives text and returns text. All the work
for a new API stays in `internal/proxy`.

What to test when plugging one in: nothing real reaches the API, the response comes back
exactly real, and the old messages arrive identical from one request to the next (the cache
depends on it). The proxy's `stress_test.go` makes these three checks and serves as a
model.

## Adding a detector

1. Write the function in one of the `detectors_*.go` files (or in a new file). It receives
   the text and calls `add(start, end, "type")` for each span found.
2. Call it in `detectBase` (`detectors_format.go`), behind a cheap test that avoids
   running on text that does not have the "ingredient" (a keyword, a character).
3. The pseudonym comes out as `TYPE-identifier`. For another format, add a case in
   `Pseudonym` (`pseudonyms.go`).
4. If the value must be remembered after being masked once, add the type to `contextual`
   (`memory_known.go`).
5. Tests: one case that catches and one that does not in `detectors_test.go`, and the case
   in `cutCases` (`detect_test.go`), which checks the data at every cut position of a
   large text.
6. Run `go test ./...` and the benchmarks (`go test ./internal/mask -bench Cold`) to see
   whether it got heavier.
7. The readers only read the text (they do not consult what was learned): on a large text
   they run in parallel and the findings are applied afterwards, in registration order. A
   reader that depends on what was learned (like the lists) runs separately, after the
   others.
8. Never search for the beginning or the end of the line without a limit at every occurrence
   (`strings.LastIndexByte(s[:i], '\n')`): on a long single line (minified JSON) that becomes
   quadratic. Use `lineStartJ`/`lineEndJ` (`memory_known.go`, 1000-character
   window). `TestLongLineLinear` and the `-bench Line` benchmarks check it.

## How it was tested

| Test | What it does | How to run |
|---|---|---|
| Automatic suite | Each detector, round trip, streaming, and each kind of data at every cut position of a large text | `make test` |
| Reader variations | Each case of each reader in every spelling variation (letter case, spaces, quoting, comments, punctuation, 2–4 parts, `$ # -` and accents, several on a line, inside JSON), plus prose with SQL between backticks | `go test ./internal/mask -run ReaderVariations` |
| Load and stress | Memory with 1.2 GB of text, 120 thousand learned values, conversations in parallel, API down, 32 MB request | `LLM_DLP_STRESS=1 go test ./... -run Stress` |
| Real sessions | Replays an old Claude Code session through llm-dlp, against a local fake API (nothing leaves the machine), and checks refusals, round trip (of the text and of the tool calls the model writes: what goes to a generated file or to the terminal), cache and time | `llm-dlp simulate SESSION.jsonl` |
| Transport matrix | Each content (catalog, connection, Snowflake config, manifest, buckets, ORM) in 15 layouts (CSV, `;`, TAB, `\|`, box, fixed width, borders, tuples, HTML, vertical, JSON, JSONL, `k=v`, loose column, `uniq -c`) and 11 transports (raw, Read, `cat -n`, `grep -n`, diff, markdown, escaped JSON, ANSI, CRLF, quotation, log): the same names masked in every cell (target 95%) and an exact round trip; the negatives have nothing masked in any cell | `go test ./internal/mask -run Matrix -v` |
| Fuzzing | The normalization map, the round trip through the readers and the SQL scan against the regexes it replaced | `go test ./internal/mask -run '^$' -fuzz FuzzRoundTrip -fuzztime 60s` (and `FuzzNormalize`, `FuzzSQLScan`) |
| False positives | Per rule: findings in a public corpus (third-party code and documentation) and, in real sessions, how many learned names exist in the public corpus (limit 2%). Counts only; test key and configuration | `LLM_DLP_CORPUS=dir1:dir2 go test ./internal/mask -run MeasurePublicCorpus -v` and, with `LLM_DLP_SESSIONS=~/.claude/projects`, `-run MeasureRealSessions` |
| Real use | The actual Claude Code, in everyday scenarios (CSV, spreadsheet, secrets, log, pasted text, screenshot, SQL, web search, subagent), with fictitious data and a spy recording what reaches the API | Manual (see below) |

In the real-use test, each scenario runs twice, directly and through llm-dlp, to compare.
Result of the last round: none of the 179 sensitive values reached the API, no response
showed a pseudonym, files were written with the real values, input tokens −1.5% and +8 ms per
message (median).

To repeat this test, run Claude Code **in an isolated environment** (for example, with
`unshare` and `tmpfs` over your home folder), in which only the test folder exists.
`--allowedTools` is not enough: it only adds permissions, and your global settings may let
the agent read other files.

During use, `llm-dlp last` shows the last messages exactly as the API received them.
