# Changelog

[Português](docs/pt-BR/CHANGELOG.md)

What changed in each version of llm-dlp. Versions follow `MAJOR.MINOR.PATCH`; what each
number means here is in [docs/versioning.md](docs/versioning.md). The newest comes first.

In each version: **Masking** is what started (or stopped) being masked; **Program** is
commands, installation, configuration, performance and documentation; **Code** is what only
matters to whoever changes the code.

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
