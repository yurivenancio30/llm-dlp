# Changelog

[Português](docs/pt-BR/CHANGELOG.md)

What changed in each version of llm-dlp. Versions follow `MAJOR.MINOR.PATCH`; what each
number means here is in [docs/versioning.md](docs/versioning.md). The newest comes first.

In each version: **Masking** is what started (or stopped) being masked; **Program** is
commands, installation, configuration, performance and documentation; **Code** is what only
matters to whoever changes the code.

## [0.8.0] - 2026-10-13

Updating no longer takes llm-dlp offline, and neither does the proxy going down: the supervisor
now holds the port and only lends it to the proxy. `llm-dlp atualizar` downloads the new
version, checks it, starts it on the same port while the old one is still answering, and only
then lets the old one go. No need to close Claude Code or run `llm-dlp parar`, and if the new
version does not start, it goes back to the previous one by itself. The installation command
does the same when llm-dlp is already installed. To get there from 0.7.0 or older, run the
installation command once, between messages.

### Program

- The supervisor opens the port once and lends it to each proxy it starts, as systemd does with
  socket activation. When the proxy goes down, the port stays open and the requests wait in the
  queue until the next proxy (about a second). Before, the proxy held the port: while it was
  down, every request was refused. Killing the proxy under four clients sending nonstop gave
  19,655 "Connection refused" in about a second on 0.7.0, and none now. That error reached the
  screen because Claude Code sends requests in the middle of a turn (after a tool) without going
  through the hook that starts llm-dlp.
- `llm-dlp atualizar [--versao X.Y.Z] [--arquivo FILE.tar.gz]`: downloads the release (the
  latest or the one asked for), checks its SHA-256 against the release's `checksums.txt`,
  replaces `~/.local/bin/llm-dlp` (keeping only the latest previous copy) and asks the
  supervisor for the swap: it starts the new version on the same port, waits for it to say it
  is ready, then the old one stops accepting connections, still answers what arrives on the
  connections already open (with `Connection: close`), finishes the answers in progress and
  exits; the supervisor re-executes itself with the new binary without letting go of the port.
  Tested with four clients sending messages nonstop and an 8-second answer in the middle: none
  failed and none was cut. If the new version does not get ready within 20 s, it is stopped,
  the previous binary goes back and the old proxy never left.
- The installation command (`curl ... | sh`) runs `llm-dlp atualizar` when llm-dlp is already
  installed in `~/.local/bin`: no setup questions. `llm-dlp instalar`, run again with an older
  version running, swaps it the same way.
- `llm-dlp recarregar` applies a change in `config.json` without taking llm-dlp offline (before:
  `llm-dlp parar`). `make instalar` runs it after installing.
- The installer started the supervisor from the file it was running, which `install.sh` deletes
  at the end: the proxy kept running, but the first time it had to restart, the supervisor could
  not find the program and kept the lock, so nothing brought llm-dlp back until
  `llm-dlp parar`. The supervisor now always runs `~/.local/bin/llm-dlp`.
- `vistos.json` and `pessoas.json` no longer lose what another process wrote: each write joins
  what was written since it last read the file. That covers the two versions running together
  during an update, and `llm-dlp importar-pessoas` with the proxy running (before, the proxy's
  next write erased the imported people).
- `llm-dlp status` suggests `llm-dlp atualizar` when the installed binary is not the one running.

## [0.7.0] - 2026-10-12

Masking measured with canaries (fake data planted in 21 output formats, 8 kinds of code file
and a multi-turn conversation, through the whole proxy): a person's name in a JSON, XML,
Python dict or YAML record is masked, the keys of a JSON object stay keys, a name the request
proved stays masked in the code and the output that follow, and each spelling of a term or
user code comes back in its own case. Names in clear in code went from 107 to 75 of 253 in a
new conversation, and from 73 to 41 with the data earlier in the request. After updating,
close Claude Code and run `llm-dlp parar`.

### Masking

- A field called only `nome` or `name` is a person's name when the same record has another
  personal field (`cpf`, `telefone`...), as it already was in a table: the `{...}` around it
  (JSON, Python dict), the neighbouring lines with the same indent (XML, YAML, `.env`) or the
  line (log). A record without one keeps it (`<produto><nome>CDB</nome>...`). Before, the name
  went out in clear in JSON, minified JSON, XML and a Python dict.
- The keys of a JSON object (and of YAML, `.env`, `key=value`) are never taken as the names
  in an inventory. With a command that names a plural noun (`curl .../api/clientes/1`),
  `"email"` and `"telefone"` were masked as objects and the model saw the JSON without its keys.
- In data (a command output, a file read), a resource name the request proved (table, schema,
  database, server, service...) stays masked inside a comment and in another file read: the
  proof is where the value came from. Before, `# carrega as propostas do dia` in a `.py` read
  after `\dt` went out in clear. In prose nothing changes: the user's message, the model's
  reply and, outside code blocks, a documentation file (`.md`, `.mdx`, `.txt`, `.rst`,
  `.adoc`), where a common word stays a word. Columns and indexes keep the old behaviour (a
  `status` column does not mask `status` in another file).
- A comment marker inside quotes is text, `--` glued to a letter is a command option, and `#`
  followed by a digit is a number: `pg_dump --schema=comercial` and `"lote #3: propostas"`
  released names as if they were comments.
- The application of a DataHub URN (`urn:li:application:SGP`) is masked, even a short acronym.
- The pseudonym of a term, a user code and the other labelled types follows the case of the
  value, as a person's does: `SGP` becomes `EMPRESA-abcd2345`, `sgp` becomes `empresa-abcd2345`,
  `SgP` becomes `Empresa-abcd2345` (the same identifier). Before, every spelling came back as the
  first one seen, so a command or file the model wrote could come back with the case changed
  (`echo SgP sgp SGP` became `echo SgP SgP SgP`). Found in 21 of 6,841 writes replaying a
  real session.

### Code

- `llm-dlp simular` counts the new pseudonym forms when it looks for a pseudonym left in a write.

## [0.6.0] - 2026-10-11

Three masking fixes found replaying a real Microsoft Fabric session: a name already known is
masked inside a longer word too, a GUID of an id field is masked, and a person's name comes
back in the case it was written (before, a file could be saved with a name in another case).
The README fits the beta clients in one table and says which run in VS Code. After updating,
close Claude Code and run `llm-dlp parar`.

### Masking

- A name the conversation already knows is masked inside a longer word too: a table
  `VENDA2024`, masked in the SQL, went out in `LH_BRONZE_VENDA2024`, `T01_VENDA2024.sql` or
  `DW$VENDA2024_HIST`. Now the whole word is masked. Only a name that looks like an identifier
  (with `_`, a digit or mixed case) counts this way: a column `status` does not mask
  `user_status` in code.
- A GUID that is the value of an id field (`workspace_id: X`, `"lakehouseId": "X"`,
  `tenant_id=X`, `"id": "X"`) is masked as a document, and from then on in the other places it
  appears. It identifies a resource of the client (a Fabric workspace, an Azure tenant). The id
  of an OAuth application (`client_id`, `app_id`) stays: it is not a secret (RFC 6749, 2.2).
- The pseudonym of a person follows the case of the name: `MARIA DA SILVA` becomes
  `PESSOA ABCD2345`, `maria da silva` becomes `pessoa abcd2345`, `Maria da Silva` stays
  `Pessoa abcd2345` (the same person, the same identifier). Before, every spelling came back
  as the first one seen in the conversation: a report the model wrote with the name in capitals
  was saved with it capitalized differently, and an Edit whose `old_string` had the name in
  capitals could fail to match the file.
- These were found replaying a real Microsoft Fabric session: names that llm-dlp itself had
  recognized went out unmasked in new command output, and 7 of the 7,790 file writes and
  commands came back with a person's name in another case.

### Program

- The README fits the beta clients in one table, with the exact line for each client and
  account, and says which ones run in VS Code: the Gemini CLI in the editor's terminal, the
  Codex extension (the same `config.toml` as the CLI), opencode and Copilot. An extension does
  not go through the `~/.bashrc` function: run `llm-dlp garantir` once after turning on the
  machine. The Gemini Code Assist extension is not supported.

### Code

- `llm-dlp simular` replays a subagent file (`subagents/agent-*.jsonl`). Before, every message
  of it was skipped as a side conversation, and the file gave zero requests and zero
  problems. A file with nothing to replay now says so.
- `llm-dlp simular` no longer counts as a different round trip a text that was cut by the
  fake API (a text just under 4000 bytes that grows when masked).

## [0.5.0] - 2026-10-11

Subscriptions, **in beta**: the Gemini CLI with a Google account and Codex with a ChatGPT
subscription (or an OpenAI key) now go through llm-dlp; they have not been checked with a real
account yet. A connection string now has each part masked with its own type. For Claude Code,
only that masking fix changes. After updating, close Claude Code and run `llm-dlp parar`.

### Masking

- A connection string (`scheme://user:password@server/database`) gets each part masked with
  its own type. Before, a password with no symbol was read as an e-mail together with the
  server (`password@server` became an e-mail pseudonym), a password that ended in `!` or `.`
  left that character in the clear, and the server had one pseudonym inside the URL
  (`host_...`) and another one elsewhere in the text (`....invalid`). Now the server of the
  internal domain has the same pseudonym everywhere, and the password is masked up to the `@`.

### Program

- **Subscriptions, in beta.** Besides the API key, llm-dlp now also serves the Gemini CLI with
  a Google account (*Login with Google*: the Code Assist API, under `/codeassist`) and Codex
  with a ChatGPT subscription (under `/chatgpt`). New reader for the OpenAI Responses format,
  the one Codex uses, with the key too (`/openai/v1/responses`): text, tool calls, the
  `apply_patch` patch and the shell command are unmasked on the way back, in streaming too.
  Built-in providers `codeassist` and `chatgpt`. The account methods of Code Assist (login,
  quota) go through as they came. Not yet checked with a real account. Claude Code was
  already covered with a subscription (Pro, Max); GitHub Copilot with the GitHub subscription
  is not, since it does not let you change its address.
- The README explains how to connect each client. Installation is split into installing
  llm-dlp (the same for everyone) and connecting your client: Claude Code (steps 4 and 5) or
  the beta clients, with a table of where each one reads the address (`~/.bashrc`,
  `~/.codex/config.toml`, `opencode.json`), the lines for each account type and a function in
  `~/.bashrc` that starts llm-dlp before opening the client, and says which kinds of account go through llm-dlp and
  which go out unmasked (Vertex AI, Copilot with the GitHub subscription). The steps of the
  Gemini CLI with an API key and of opencode were followed as written on a clean home folder.
- The example at the top of the README now goes step by step: the file the agent reads, what
  the API receives, what the model writes and what is saved on your disk, with a table of
  how to read each pseudonym.
- `llm-dlp instalar` says that steps 4 and 5 are only for Claude Code. Whoever answers no
  no longer gets a ✗ telling them to run those steps again: the summary shows them as not set
  up (–) and points to the README.

## [0.4.0] - 2026-10-11

Other clients and APIs, **in beta**: the Gemini API and the OpenAI Chat Completions format,
for the Gemini CLI, opencode and GitHub Copilot with your own key. Claude Code stays the only
stable client, and nothing changes in what is masked for it. Beta clients are pointed by hand
and have no lock: read [Other clients and APIs (beta)](docs/configuration.md#other-clients-and-apis-beta)
before using them. After updating, close Claude Code and run `llm-dlp parar`.

### Program

- **Gemini API (beta).** llm-dlp now also serves the Gemini API (`generateContent`,
  `streamGenerateContent`, `countTokens`), under the `/gemini` path of the same port: point
  the Gemini CLI, opencode or a Google SDK to `http://127.0.0.1:8787/gemini`. The same masking
  as with Claude Code applies (text, tool calls and results, system instructions, images and
  PDFs, conversation memory, frozen history), and the response is unmasked in streaming too.
  New configuration field `provedores`; other APIs can be added there (without a reader of
  their own, every text is masked). Checked with the Gemini CLI and opencode running, and
  `llm-dlp ultima` shows the Gemini conversation too. See
  [docs/configuration.md](docs/configuration.md#other-clients-and-apis-beta).
- **OpenAI format, Chat Completions (beta).** A request whose path ends in `/chat/completions` is
  now read like the others (system, tools, text, tool calls and results, images), and the
  response is unmasked, in streaming too. With it, GitHub Copilot with your own key, and the
  OpenAI, the xAI and the OpenAI-compatible Gemini APIs go through llm-dlp: built-in providers
  `openai` and `xai`, next to `gemini`. Checked with the Copilot CLI running.
- The documentation marks the other clients as beta (Claude Code stays the only stable one)
  and says what is covered for each: only Claude Code is pointed,
  started, checked and locked by the installation; the other clients are pointed by hand and
  have no lock.
- When the API cannot be reached, the log and the error no longer show the address of the
  request, which may carry the API key (`?key=...`).

### Code

- `llm-dlp simular` also checks the way back of what the model writes: the fake API returns
  the last tool call with the pseudonyms, and the replay counts the calls that came back
  different from the original (what would go to a generated file or to the terminal).

## [0.3.1] - 2026-10-11

Security fixes in the program itself. Nothing changes in what is masked. After updating,
close Claude Code and run `llm-dlp parar`.

### Program

- **The local port is protected from web pages and from other users.** A page open in the
  browser could reach the port through DNS rebinding and read the last (masked)
  conversation, and so could any other user of the machine; a program that took the port
  while llm-dlp was down passed for it and would receive the unmasked conversation and the
  API credential. Now a request that does not use the local address, or that comes from a
  web page, is refused; the last conversation only goes out to whoever reads the key
  (`llm-dlp ultima`); and on every message llm-dlp has to prove it is itself by answering a
  challenge with the key. A request path with a line break no longer forges a log line.
  **After updating, close Claude Code and run `llm-dlp parar`:** the old process cannot
  give the proof, and the messages are blocked until it is replaced.
- **Warning when the configuration reduces the protection.** `config.json` belongs to the
  user, so a command run by the agent can change it: turn a detector off, or point
  `upstream` to another server, which would receive the messages and the API credential.
  While that is the case, Claude Code shows a warning on every message and so does
  `llm-dlp status`. `llm-dlp instalar` also forbids the agent to mention the llm-dlp folder
  in a command (run it again to get the rule).
- An image that declares an enormous size, or a PDF whose text expands without end, no
  longer exhausts the memory and brings llm-dlp down: it is refused by its header (more
  than 16000 points on a side or 64 million in total) and the output of the external
  programs has a ceiling.
- Built with Go 1.27.2, which fixes 9 known vulnerabilities of the standard library
  (HTTP and TLS) present in the binaries up to 0.3.0; `xz` dependency updated.
- **Release notes on GitHub:** the releases 0.2.1 and 0.3.0 were published with empty notes
  (a GoReleaser setting skipped the step that reads them). The notes are the section of the
  version in this file again, now with each paragraph on one line, links that work from the
  release page and the install command at the end. A release without notes fails the
  workflow.

## [0.3.0] - 2026-10-11

A conversation no longer gets stuck: neither on 429 after an update, nor on an image that
cannot be verified.

### Masking

- **Claude Code's own text is no longer damaged.** A common word that the conversation
  learned as a name (a `billing` table) was replaced inside the system prompt and the tool
  definitions whenever they were masked again (after an update, for example). With the word
  replaced inside `x-anthropic-billing-header`, the API stopped recognizing Claude Code and
  answered 429 to every message of that conversation, which stayed stuck. Now a common word
  stays a word in these texts (as in the prose of the messages), and Claude Code's header and
  identity sentence go through untouched, proven by their exact shape. A conversation that
  was stuck works again after the update.

### Program

- **An image or PDF that cannot be verified no longer blocks the conversation.** Without OCR
  installed (or with an unreadable file, or with `ocr.modo = bloquear`) the whole request was
  refused with a 502, on every message from then on. Now the file is removed from the
  request (it still never goes out) and the model gets a note in its place, with the reason
  and the command that installs OCR. `llm-dlp status` warns when OCR is missing.
- The log shows the type and the message of an API error (`| API: rate_limit_error: ...`).
- `llm-dlp --help` (and `ajuda`, `-h`) shows the help and exits with 0; an unknown command
  says which one it was. The help shows the version, how to update, `parar`, and where the
  configuration and the log are.
- Fewer files at the root of the repository: `CONTRIBUTING.md` and `SECURITY.md` moved to
  `.github/` (GitHub still shows them), and the Portuguese README to `docs/pt-BR/README.md`.

## [0.2.1] - 2026-10-10

Installation with one command and releases packaged the way large projects do it. Nothing
changes in what is masked.

### Program

- **One-line installation:** `install.sh` (the command is in the README) downloads the binary
  of the latest release for the machine (amd64 or arm64), checks its SHA-256 against the
  published checksums and runs `llm-dlp instalar`. Options: `--version X.Y.Z`, `--no-setup`,
  `--bin-dir DIR`.
- **Release archives:** each release now has one `.tar.gz` per architecture (the binary, the
  LICENSE, the README and the CHANGELOG) and a `checksums.txt`, built by GoReleaser. Releases
  up to 0.2.0 published the bare binary and a `SHA256SUMS` file.
- **Documentation in English and Portuguese:** `README.md` and `docs/` are in English;
  `README.pt-BR.md` and `docs/pt-BR/` are in Portuguese.
- `CONTRIBUTING.md` and `SECURITY.md` (how to contribute and how to report a vulnerability).

### Code

- The commit history was rewritten in English, following
  [Conventional Commits](https://www.conventionalcommits.org). The content of every commit is
  the same; the commit hashes from before this version changed.
- `make dist` rehearses the release with GoReleaser; the CI also validates `install.sh`
  (shellcheck) and the GoReleaser configuration.

## [0.2.0] - 2026-10-10

A public name only stays in the clear with proof. Every rule that lets a name through now
requires a structural guarantee, fail-closed: when in doubt, mask.

### Masking

- **Non-Latin scripts:** a database, schema, table or user name in any script (Cyrillic,
  Japanese, accents) is masked. So is a dotted user name (`maria.souza`).
- **Traceback** (Python, Java, JavaScript, Go): the line that points to the project has the
  function, the module, the code and the value quoted in the error masked. The line of a
  dependency or of the standard library (`pandas`, `net/http`) stays readable.
- **Container image:** it only stays in the clear with two keys, a vendor organization
  **and** a public software name. `acme/redis` and the image built in the project are masked.
- **Public software in the conversation:** the default name of a software (`airflow`,
  `datahub`, `superset`) is no longer spread through the conversation when the conversation
  shows that it is the software (installed, imported, run as a public image or executed). A
  dictionary word, a name defined in the project and a registered term never get this proof.
- **Origin by path:** the path readers stop at a public package
  (`site-packages/sqlalchemy/...` stays whole) and keep reading the project folders under a
  public user (`/home/ubuntu/<project>`). A dependency file that was read does not teach
  names to the conversation. Being in a dependency folder is not enough: the package, the
  module or the group must be public, because the client's internal package lives in the
  same folders.
- **docker-compose:** a service is recognized by any key of the specification, including
  `<<`.
- **Command-line listing:** the command in sight (`$ airflow dags list`) says the header
  belongs to the tool; for a type outside the vocabulary, only the identity column (`dag_id`)
  is typed. The table of `kubectl get` and `docker ps` is read by its header.
- **Managed cloud service hosts** (RDS, Redshift, S3, Azure, GCP): the client's label is
  masked and the provider's domain stays.

### Program

- Version and changes organized: `llm-dlp versao`, this file, ready-made binaries in each
  release and `make release` (see [docs/versioning.md](docs/versioning.md)).
- Estimate of CPU, memory and disk usage in
  [docs/how-it-works.md](docs/how-it-works.md#how-much-of-the-machine-it-uses).

### Code

- Generated lists in `internal/mask/dados/`; files of the `mask` package grouped by prefix
  (`detectores_`, `campos_`, `leitor_`, `chamada_`, `decisor_`, `memoria_`, `vocab_`). The map
  and the table of what is stored are in [docs/development.md](docs/development.md).
- New generated lists: `software_publico.txt` and `fornecedores.txt` (GitHub repositories
  with 3000 stars or more), `imagens_oficiais.txt` (Docker Official Images) and
  `palavras_dicionario.txt` (the 50 thousand most frequent words of Portuguese and English).

### Measured

Against 0.1.0, on hand-labelled names from 16 public repositories:

| | 0.1.0 | 0.2.0 |
|---|---|---|
| Owner's name that leaked (5 repositories, validation) | 6 of 22 | 1 of 22 |
| Owner's name that leaked (11 new repositories, validation) | 2 of 9 | 1 of 9 |
| Public name masked for nothing (5 repositories, validation) | 138 of 207 | 124 of 207 |
| Public name masked for nothing (11 new repositories, validation) | 40 of 122 | 46 of 122 |
| Words replaced in prose / code / configuration | 2.29% / 2.06% / 5.47% | 2.16% / 1.70% / 4.91% |
| Time for 21 MB of new text (2 cores) | 75 s | 77 s |
| Memory at startup | 21 MB | 25 MB |

The over-masking that went up in the new repositories is translation catalog labels (`"User":
"Gebruiker"`): a rule that let them through also let exported spreadsheet data through, and
was rejected.

## [0.1.0] - 2026-10-08

First version: everything that existed up to the commit `815a6ac`.

### Masking

- **Personal data and secrets by format:** e-mail, CPF, CNPJ, phone, card, documents with a
  check digit, tokens and keys (gitleaks), common passwords.
- **Personal data by the field name:** `field: value`, JSON, SQL, XML, CSV, spreadsheet,
  terminal table.
- **Registered people** (names, e-mails, codes), stored only as hashes.
- **Internal resource names by structure:** SQL and DDL, tables in any layout, schemas,
  connection strings, YAML/JSON of Kubernetes, Helm, compose, Terraform, Ansible and CI,
  key-value, addresses, paths, packages, cloud, code and command line.
- **Transport normalization:** the content is read as it arrives (line number, `grep`, diff,
  ANSI, escaped JSON).
- **Conversation memory and tracking:** a name decided where it enters holds for the whole
  conversation; what the model wrote before any data is public; a common word stays a word
  in prose.
- **Images and PDFs:** OCR and black bars.

### Program

- Local proxy for Claude Code, fail-closed, with an exact round trip and text already sent
  frozen (the API cache stays valid).
- `llm-dlp instalar` (program, configuration, OCR, Claude Code, lock), `status`, `parar`,
  `emergencia`, `desinstalar`.
