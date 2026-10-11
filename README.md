# llm-dlp

[![ci](https://github.com/yurivenancio30/llm-dlp/actions/workflows/ci.yml/badge.svg)](https://github.com/yurivenancio30/llm-dlp/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/yurivenancio30/llm-dlp)](https://github.com/yurivenancio30/llm-dlp/releases/latest)
[![license](https://img.shields.io/github/license/yurivenancio30/llm-dlp)](LICENSE)

**English** · [Português](docs/pt-BR/README.md)

A local proxy that **masks sensitive data between you and the LLM**. The model's API only
sees pseudonyms; you, your terminal and the files the agent writes see the real values.

Made for **Claude Code** (stable), with a subscription (Pro, Max) or an API key. The Gemini
CLI, Codex, opencode and GitHub Copilot with your own key also work, **in beta** (see
[Supported clients](#supported-clients)).

```
          your machine                                          │ internet
 you ──► Claude Code (or another client) ──► llm-dlp ── masks ──►│──► model API
                                                                │    (sees only pseudonyms)
 you ◄── real values ◄──────────────────── llm-dlp ◄─ unmasks ──│◄── answer with pseudonyms
```

## An example, step by step

You ask the agent: *"write a script that backs up the customer database; the connection is
in `.env`"*.

**1. The agent reads `.env`.** This is the file on your disk:

```bash
DB_HOST=pg-prd.acme.intra
DB_NAME=crm_clientes
DB_USER=app_crm
DB_PASSWORD=Pr0dS3nha2024
SUPPORT_EMAIL=maria.lopes@acme.com.br
```

And this is what leaves your machine, the only thing the model's API ever sees:

```bash
DB_HOST=hkdzp7s4a.invalid
DB_NAME=db_nr3b5hpi
DB_USER=usr_xyxhy7ug
DB_PASSWORD=SEGREDO-vqxz5dvy
SUPPORT_EMAIL=p.ouhiojea@ds373.invalid
```

**2. The model answers with the pseudonyms**, because they are all it has seen:

```bash
pg_dump -h hkdzp7s4a.invalid -U usr_xyxhy7ug -d db_nr3b5hpi -f backup.sql
```

**3. llm-dlp swaps them back before the agent acts.** The script saved on your disk, and the
command that runs, have the real values:

```bash
pg_dump -h pg-prd.acme.intra -U app_crm -d crm_clientes -f backup.sql
```

The answer you read also comes back real: *"backup.sh dumps crm_clientes from
pg-prd.acme.intra"*.

| Real value | Pseudonym | The model still knows that it is |
|---|---|---|
| `pg-prd.acme.intra` | `hkdzp7s4a.invalid` | a server (`.invalid` is a domain that never exists: nothing can ever be sent to it) |
| `crm_clientes` | `db_nr3b5hpi` | a database |
| `app_crm` | `usr_xyxhy7ug` | a user |
| `Pr0dS3nha2024` | `SEGREDO-vqxz5dvy` | a password (*segredo* is "secret") |
| `maria.lopes@acme.com.br` | `p.ouhiojea@ds373.invalid` | an e-mail |
| `529.982.247-25` | `CPF-5qwopr5x` | a CPF (Brazilian taxpayer number) |

The same value always becomes the same pseudonym, so the model still understands that two
mentions are the same server. Pseudonyms come from a secret key that only exists on your
machine. `pg_dump`, `postgresql` and `5432` stay as they are: public names are not masked.

llm-dlp was built in Brazil: besides the universal formats (e-mail, tokens, cards, IPs), it
knows Brazilian documents (CPF, CNPJ, RG, CNH) and Portuguese field names. The program
itself (commands, questions, messages) speaks Portuguese; this documentation says what each
command does.

## What it does

- **Masks everything that goes to the API:** what you type, files that are read, command
  output, MCP data, subagents, attachments and even the session title.
- **Masks three kinds of data:**
  - personal data (CPF, CNPJ, e-mail, phone, a person's name, address, card);
  - secrets (password, token, API key);
  - names that identify the company or the client: server, database, schema, table, column,
    service, bucket, queue, user, folder, a function in a traceback. They are recognized by
    the structure of the text (SQL, YAML, JSON, `.env`, connection strings, Kubernetes,
    Terraform, code, command output), with no per-client list.
- **Leaves what is public readable:** `postgres`, `redis`, `my-bucket`, `org.apache.kafka`,
  the `pandas` line in a traceback. A name only stays in the clear with proof that it is
  public; when in doubt, it is masked.
- **Unmasks everything that comes back:** you read the real values; commands run and files
  are written with the real values.
- **Reads images and PDFs** (OCR) and covers in black whatever is sensitive.
- **Fail-closed:** if llm-dlp goes down or does not know how to handle something, the message
  **does not go out**. There is no silent leak.
- **Is light:** a message costs 5–30 ms, the number of tokens hardly changes and the API
  cache is not disturbed.
- **Asks nothing of you day to day** (with Claude Code): it starts with the session and comes
  back by itself if it goes down.

## Supported clients

| Client | Status | How it is connected |
|---|---|---|
| Claude Code (terminal, VS Code, JetBrains), with a subscription (Pro, Max) or an API key | **Stable** | By `llm-dlp instalar`, with a lock: Claude Code refuses to work outside llm-dlp |
| Gemini CLI, with a Google account or a Gemini API key | Beta | By hand, in `~/.bashrc` ([how](#gemini-cli-codex-opencode-and-copilot-beta)). No lock |
| Codex CLI, with a ChatGPT subscription or an OpenAI API key | Beta | By hand, in `~/.codex/config.toml`. No lock |
| opencode, with the `google` provider | Beta | By hand, in `~/.bashrc` and `opencode.json`. No lock |
| GitHub Copilot CLI and VS Code, with your own key | Beta | By hand. No lock |
| GitHub Copilot with the GitHub subscription | **Not supported** | Copilot does not let you change the address of the models GitHub hosts |

> [!WARNING]
> **Beta clients have no lock.** The protection lasts while the client's address points to
> llm-dlp: if it stops pointing (a removed line, another account type, another provider), the
> client talks straight to the API, unmasked, and nothing warns you. Beta may change in a
> MINOR version. Details: [Other clients and APIs (beta)](docs/configuration.md#other-clients-and-apis-beta).

## Installation

It works on any Linux, including WSL on Windows. You do not need to clone the repository or
install Go.

### 1. Install llm-dlp (the same for every client)

```bash
curl -fsSL https://raw.githubusercontent.com/yurivenancio30/llm-dlp/main/install.sh | sh
```

The [script](install.sh) downloads the binary of the latest release for your machine (amd64
or arm64), checks its SHA-256 against the published checksums and runs `llm-dlp instalar`.

<details>
<summary>Other ways: read the script first, download by hand, a specific version, build from source</summary>

**Read the script before running it:**

```bash
curl -fsSLO https://raw.githubusercontent.com/yurivenancio30/llm-dlp/main/install.sh
less install.sh
sh install.sh
```

**Download by hand** from the [Releases](https://github.com/yurivenancio30/llm-dlp/releases)
page (`llm-dlp_X.Y.Z_linux_amd64.tar.gz` or `..._arm64.tar.gz`, and `checksums.txt`):

```bash
sha256sum -c checksums.txt --ignore-missing     # must print "OK"
tar -xzf llm-dlp_*_linux_*.tar.gz llm-dlp
./llm-dlp instalar
```

**A specific version, or only the binary** (without running the setup):

```bash
curl -fsSL https://raw.githubusercontent.com/yurivenancio30/llm-dlp/main/install.sh | sh -s -- --version 0.2.1
curl -fsSL https://raw.githubusercontent.com/yurivenancio30/llm-dlp/main/install.sh | sh -s -- --no-setup
```

**Build from source** (needs [Go](https://go.dev/dl/)). This is the path on macOS, where it
builds but has not been fully tested yet, and for whoever is going to change the code:

```bash
git clone https://github.com/yurivenancio30/llm-dlp.git && cd llm-dlp && make build
./bin/llm-dlp instalar
```

</details>

`instalar` asks what it needs, in 5 steps (in Portuguese; answer `s` for yes, `n` for no):

| Step | What happens | You answer |
|---|---|---|
| 1. Program | Copies llm-dlp to `~/.local/bin` | Nothing. If it warns that `~/.local/bin` is not in your PATH, add `export PATH="$HOME/.local/bin:$PATH"` to `~/.bashrc` |
| 2. Configuration | Creates `~/.config/llm-dlp` with the configuration and the secret key | The internal domain, which appears in the server names (e.g. `acme`, for `pg-prd.acme.intra`), and the names that must never go out (company, client, projects). E-mails are always masked |
| 3. OCR | Installs tesseract and poppler, if missing | Yes, and the `sudo` password |
| 4. Claude Code | Connects Claude Code to llm-dlp (with a backup of `~/.claude/settings.json`) | **Yes if you use Claude Code; otherwise no** |
| 5. Lock | Makes Claude Code refuse to work outside llm-dlp | **Yes if you use Claude Code** (and the `sudo` password); skipped otherwise |

At the end it shows a summary. ✓ is done, ✗ is missing (run `llm-dlp instalar` again: it
skips what is already done), and – is what you chose not to set up.

### 2. Connect your client

#### Claude Code (stable)

Steps 4 and 5 did everything. Close and open Claude Code (in VS Code, reload the window) and
check with `llm-dlp status`.

#### Gemini CLI, Codex, opencode and Copilot (beta)

> [!WARNING]
> Beta, and with no lock: read the warning in [Supported clients](#supported-clients).

`llm-dlp instalar` does not touch these clients. For each one you add to `~/.bashrc`:

- **the address of llm-dlp** in place of the client's API address;
- **a function with the client's name** that starts llm-dlp (`llm-dlp garantir`) before
  opening the client. Only Claude Code starts llm-dlp by itself: without the function, after
  you restart the machine the client gets "connection refused". Nothing leaks, but it does not
  work.

**Gemini CLI.** Each kind of account talks to a different Google API, with its own line.
Vertex AI is not covered: with it, **the conversation goes out unmasked**.

With your Google account (*Login with Google*, the free plan and Google AI Pro):

```bash
# ~/.bashrc
export CODE_ASSIST_ENDPOINT=http://127.0.0.1:8787/codeassist
gemini() { llm-dlp garantir && command gemini "$@"; }
```

With a Gemini **API key** (from Google AI Studio):

```bash
# ~/.bashrc
export GEMINI_API_KEY="your-key"
export GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:8787/gemini
gemini() { llm-dlp garantir && command gemini "$@"; }
```

With the API key, also choose it in `~/.gemini/settings.json` (or pick *Use Gemini API Key*
the first time it opens):

```json
{ "security": { "auth": { "selectedType": "gemini-api-key" } } }
```

**Codex CLI**, with your ChatGPT subscription (sign in with `codex login`, as usual):

```toml
# ~/.codex/config.toml
model_provider = "llm-dlp"

[model_providers.llm-dlp]
name = "llm-dlp"
base_url = "http://127.0.0.1:8787/chatgpt"
wire_api = "responses"
requires_openai_auth = true
```

```bash
# ~/.bashrc
codex() { llm-dlp garantir && command codex "$@"; }
```

With an OpenAI API key instead, use `base_url = "http://127.0.0.1:8787/openai/v1"` and
`env_key = "OPENAI_API_KEY"` in place of `requires_openai_auth = true`.

**opencode**, with the `google` provider:

```bash
# ~/.bashrc
export GOOGLE_GENERATIVE_AI_API_KEY="your-key"
opencode() { llm-dlp garantir && command opencode "$@"; }
```

```json
// ~/.config/opencode/opencode.json
{ "provider": { "google": { "options": { "baseURL": "http://127.0.0.1:8787/gemini/v1beta" } } } }
```

Only the provider pointed there goes through llm-dlp. If you switch opencode to another
provider, point that one too.

**GitHub Copilot CLI**, only with your own key (OpenAI or xAI). The models GitHub hosts and
the suggestions while you type do not go through llm-dlp.

```bash
# ~/.bashrc
export COPILOT_PROVIDER_BASE_URL=http://127.0.0.1:8787/openai/v1    # xAI (Grok): .../xai/v1
export COPILOT_PROVIDER_API_KEY="your-key"
export COPILOT_MODEL="model-name"
copilot() { llm-dlp garantir && command copilot "$@"; }
```

Copilot in VS Code and other clients: [Client by client](docs/configuration.md#client-by-client).

**Check that it is protected.** Open a new terminal (or run `source ~/.bashrc`), send one
message from the client and run `llm-dlp ultima`: it must show that message, with the
pseudonyms. If it says *"Ainda não passou nenhuma mensagem"* ("no message has gone through
yet"), the client is not going through llm-dlp: check the address.

The lines of the Gemini CLI with an API key and of opencode were followed as they are on a
clean machine (a new home folder): the agent read a file with an e-mail and an IP and wrote
another one; the API only received the pseudonyms and the file came out with the real values.
The Gemini CLI with a Google account and Codex follow the clients' source code and the
automated tests, and **have not been checked with a real account yet**.

### Updating to a new version

1. Run the installation command above again. It replaces the program and skips what is
   already done; your configuration and your key do not change.
2. Close your client (with Claude Code, every window) and run `llm-dlp parar`. What is
   running is only replaced when the old process stops.
3. Open the client again (with a beta client, the function starts the new version) and check
   with `llm-dlp status`.

What changed in each version is in the [CHANGELOG.md](CHANGELOG.md). After updating, the
first message of each conversation rewrites the API cache once.

## Day to day

With Claude Code there is nothing to run: llm-dlp starts when a session opens and comes back
by itself if it goes down. With a beta client, the function in `~/.bashrc` starts it when you
open the client, and from then on it also comes back by itself.

| Situation | What happens / what to do |
|---|---|
| I want to see whether everything is fine | `llm-dlp status` |
| I want to see what the API received | `llm-dlp ultima` (the last messages, with the pseudonyms) |
| I want to know which version I have | `llm-dlp versao` (what changed in each one: [CHANGELOG.md](CHANGELOG.md)) |
| llm-dlp went down | It comes back by itself in ~1 s. While it is out, no message goes out |
| It does not come back, and I need Claude to fix it | `sudo llm-dlp emergencia 30m` lets Claude through **unmasked** for a limited time. Use a new session, with no client data. It goes back to normal when the time is up, or with `sudo llm-dlp emergencia sair`. Every message shows a warning |
| The image or PDF did not reach the model | OCR is missing: the model says so in its answer, and `llm-dlp status` shows the installation command. After installing, send the file again |
| The message was blocked: "não provou ser o seu llm-dlp" (did not prove it is your llm-dlp) | You have just updated and the old process is still running: close Claude Code and run `llm-dlp parar`. If you did not update, another program is using the port |
| I changed `config.json` | `llm-dlp parar` (it comes back on the next message, with the new configuration) |
| I want to see what was done on each message | `~/.config/llm-dlp/llm-dlp.log` (counts and times only, never values) |
| I want to undo everything | `llm-dlp desinstalar` (and `sudo llm-dlp desinstalar-trava`, if you installed the lock). Then close and open Claude Code and run `llm-dlp parar`. With a beta client, also remove its lines from `~/.bashrc` (and from `opencode.json` or `~/.codex/config.toml`) |

## More details

| Document | What is in it |
|---|---|
| [How it works](docs/how-it-works.md) | The path of a message, the pseudonyms, what is remembered, fail-closed, images, performance and how much of the machine it uses |
| [What is detected](docs/detection.md) | The ways of recognizing a piece of data and how to teach it your company's people and field names |
| [Configuration and commands](docs/configuration.md) | Every field of `config.json` and every command |
| [Security and limits](docs/security.md) | What it protects, what it does not protect and what goes through unmasked |
| [Policy](docs/policy.md) | The four levels of information, what llm-dlp does with each one, the references (LGPD, MITRE ATT&CK, CWE, NIST) and the declared limit on code and business rules |
| [For developers](docs/development.md) | Code map, what is stored in memory and on disk, how to plug in another LLM API, how to add a detector, how it was tested |
| [Structures](docs/structures.md) | The research behind the structure rules: where internal resource names live in each format, and the rules for when a name is public |
| [Versions and releases](docs/versioning.md) | What each version number means, what a release contains and how to make one |
| [Changes](CHANGELOG.md) | What changed in each version |
| [Contributing](.github/CONTRIBUTING.md) · [Security policy](.github/SECURITY.md) | How to contribute and how to report a vulnerability |

## License

MIT. See [LICENSE](LICENSE).

The data files `internal/mask/dados/palavras_comuns.txt` and `palavras_dicionario.txt` are
adaptations of the frequency lists of
[FrequencyWords](https://github.com/hermitdave/FrequencyWords), by Hermit Dave (OpenSubtitles
2018 data), distributed under [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/);
origin and criterion are in the header of each file.
