# Configuration and commands

[← back to the README](../README.md) · [Português](pt-BR/configuracao.md)

The field names of `config.json` and the command names are in Portuguese; the tables below
say what each one is for.

## Configuration

File `~/.config/llm-dlp/config.json`. After changing it, run `llm-dlp reload`: it applies
the change without taking llm-dlp offline.

Up to 0.8.0 the fields had Portuguese names (`porta`, `dominios_internos`, `termos`...). They
are still read, and `llm-dlp update` rewrites the file with the names below (keeping the previous
one as `config.json.bak-DATE`). Going back to an older version with `llm-dlp update --version`
converts the file back, since an older version would ignore the new names.

While the configuration removes protection (`fail_closed` off, `ocr.mode` set to
`allow`, `objects` off, any detector turned off) or `upstream` is not the Anthropic API,
Claude Code shows a warning on every message (see [Security](security.md)). The warning
does not go to the API.

| Field | What it is for | Default |
|---|---|---|
| `internal_domains` | Pieces that identify your internal hostnames (`["empresa"]` catches `mysql.empresa.intra`) | empty |
| `allowed_emails` | E-mails that go through unmasked | empty |
| `allowed_email_domains` | E-mail domains that go through unmasked | `example.com`, `anthropic.com`… |
| `host_roles` | First part of the hostname that may stay visible (`mysql` in `mysql-x7k2.invalid`) | `mysql`, `postgres`, `redis`… |
| `extra_fields` | Extra words for the field names | empty |
| `extra_patterns` | Your own regexes | empty |
| `terms` | Exact words to mask | empty |
| `disabled_detectors` | Detectors to turn off (list below) | empty |
| `optional_detectors` | Detectors to turn on. Today only `quase`: sex, age, marital status, occupation, nationality, income, latitude/longitude | empty |
| `tools_never_unmasked` | Web tools: they receive pseudonyms only, and what they bring is not remembered. The claude.ai connectors (`mcp__claude_ai_*`) always receive pseudonyms only, regardless of this list | `WebFetch`, `WebSearch` |
| `documents_without_context` | Masks digit-only CPF and CNPJ even without the word "cpf"/"cnpj" nearby, if the check digit matches. About 1 in 100 random numbers of that length also matches and is masked needlessly | `true` |
| `fail_closed` | Refuses what it does not know how to mask. When off, a failure while masking sends the request **unmasked** (only a warning is logged). An image or PDF that could not be verified never goes out, whatever the value of this field. Turning it off is not recommended | `true` |
| `ocr` | `mode` (`mask`, `block` or `allow`), `language`, `max_pages` (maximum pages), paths of tesseract and poppler. With `block`, and with `mask` when the file cannot be verified (OCR missing, unreadable file), it is removed from the request and the model gets a note in its place | `mask`, `por`, 30 |
| `objects` | Internal resource names (see [What is detected](detection.md#5-internal-resource-names-by-structure)). `enabled`; `mask` and `propagate`: maps type → `true`/`false` (types: `servidor`, `database`, `schema`, `tabela`, `coluna`, `procedure`, `indice`, `usuario`, `namespace`, `servico`, `bucket`, `fila`). E.g. `{"mask": {"coluna": false}}` leaves the columns readable | on; masks all; propagates all except `coluna` and `indice` |
| `objects.public_ip` | Also masks public IPv4 and IPv6 as a server name (`HOST_...`). `::1`, link-local, documentation ranges and the public DNS servers (`8.8.8.8`, `1.1.1.1`, `9.9.9.9`...) stay. A private IP is always masked by the `ip` detector | off |
| `port` | Local port of the proxy | 8787 |
| `upstream` | Address of the API | `https://api.anthropic.com` |
| `providers` | **Beta.** Other APIs served on the same port, under the path `/<name>` (see [Other clients and APIs](#other-clients-and-apis-beta)). Each one has `upstream` and `format` (`gemini`, `openai` or `anthropic`; any other value falls into the generic mode) | `gemini`, `openai` and `xai`, each with the address of its API |

Names accepted in `disabled_detectors`: `segredo`, `email`, `ip`, `host`, `cpf`, `cnpj`,
`pis`, `cnh`, `rg`, `telefone`, `cep`, `cartao`, `conta`, `pix`, `endereco`, `nascimento`,
`nome`, `campo` (detection by the field name), `usuario`, `doc`, `iban`, `processo`, `mac`.

## Other clients and APIs (beta)

> [!WARNING]
> **Beta.** Everything in this section (the Gemini, Code Assist and OpenAI formats, and the
> Gemini CLI, Codex, opencode and Copilot clients) is beta. Only Claude Code is stable. Beta clients have **no
> lock**: if the client stops pointing to llm-dlp, it talks straight to the API, unmasked, and
> nothing warns you. Read [what beta means](#what-beta-means) before using them with real data.

llm-dlp was built around Claude Code, and `llm-dlp install` sets up **only Claude Code**.
Other clients work with the same masking, but you point them by hand and they have no lock.

| Client | Status | Installed and locked by `llm-dlp install` |
|---|---|---|
| Claude Code, with a subscription (Pro, Max) or an API key | **Stable** | Yes |
| Gemini CLI (terminal, also inside VS Code), with a Google account or a Gemini API key | Beta | No |
| Codex (CLI and VS Code extension), with a ChatGPT subscription or an OpenAI API key | Beta | No |
| opencode (terminal and VS Code) | Beta | No |
| GitHub Copilot (CLI and VS Code), with your own key | Beta | No |
| Any other client of the Anthropic, Gemini or OpenAI (Chat Completions, Responses) API | Beta | No |
| GitHub Copilot with the GitHub subscription (the models GitHub hosts), the Gemini Code Assist extension (VS Code, JetBrains), Vertex AI | **Not supported** | They do not let you change the address they talk to |

### What beta means

- **It is on by default and comes in the same binary.** It is tested (automated tests and
  real clients, see the table [Client by client](#client-by-client)), but it has been used
  much less than the Claude Code path.
- **It may change in a MINOR version**, without the notice that the
  [versions table](versioning.md#what-each-number-means) gives to stable features: the
  addresses, the `providers` field and what is read in each format.
- **It is isolated from Claude Code.** The code of each format only runs for requests on its
  own path (`/gemini`, `/openai`, `/xai`...). A request from Claude Code goes out byte for
  byte the same with or without these providers, and a malformed request or response from
  another format does not bring llm-dlp down. Both are checked by automated tests. The
  conversation memory is shared: a name masked in a Gemini conversation is also masked in
  Claude Code (more masking, never less).
- **Report problems** in the [issues](https://github.com/yurivenancio30/llm-dlp/issues), saying
  the client and its version.

### Claude Code and the beta clients

| | Claude Code (stable) | Gemini CLI, Codex, opencode, Copilot and other clients (beta) |
|---|---|---|
| What goes out is masked and what comes back is unmasked (text, tool calls and results, files the agent writes, images and PDFs) | Yes | Yes, the same engine |
| `llm-dlp last` and the log | Yes | Yes |
| Pointed to llm-dlp by `llm-dlp install` | Yes (step 4) | **No.** You set the address where the client reads it (`~/.bashrc`, `~/.codex/config.toml`, `opencode.json`), as in the tables below |
| llm-dlp is started for you | Yes, when a session opens | **No.** A function in `~/.bashrc` runs `llm-dlp ensure` before opening the client ([README](../README.md#gemini-cli-codex-opencode-and-copilot-beta)). Without it, if llm-dlp is not running, the client gets "connection refused" and nothing goes out |
| Account types that go through llm-dlp | Subscription (Pro, Max) and API key: Claude Code always talks to the Anthropic API | The ones pointed in [Client by client](#client-by-client): the Gemini CLI with a Google account or an API key, Codex with a ChatGPT subscription or an API key. **Not** Vertex AI, the Gemini Code Assist extension or Copilot with the models GitHub hosts: they talk to other servers and go out **unmasked** |
| Check on every message (llm-dlp is running, is the right version and proves it is itself) | Yes | **No** |
| Lock: the client refuses to work outside llm-dlp | Yes (step 5) | **No.** If the address is removed from the client's configuration, the client talks straight to the API, unmasked, and nothing warns you |

The step by step, with the lines for each file, is in the
[README](../README.md#gemini-cli-codex-opencode-and-copilot-beta).

In short: with another client, the protection lasts while its address points to llm-dlp. Check
it with `llm-dlp last` after the first message: it must show your conversation, with the
pseudonyms.

### Which address to give the client

Each API is served under a path of the same port. The API key is still yours and goes
straight through (llm-dlp does not store it or write it to the log).

| API | Address | What llm-dlp reads |
|---|---|---|
| Anthropic | `http://127.0.0.1:8787` | Messages (`/v1/messages`) |
| Gemini | `http://127.0.0.1:8787/gemini` | `generateContent`, `streamGenerateContent`, `countTokens` |
| Gemini with a Google account (Code Assist, the Gemini CLI's *Login with Google*) | `http://127.0.0.1:8787/codeassist` | The same, inside the Code Assist envelope. The account methods (login, quota, experiments) carry no conversation and go through as they came |
| ChatGPT subscription (Codex) | `http://127.0.0.1:8787/chatgpt` | Responses |
| Gemini, OpenAI-compatible address | `http://127.0.0.1:8787/gemini/v1beta/openai` | Chat Completions |
| OpenAI | `http://127.0.0.1:8787/openai/v1` | Chat Completions and Responses |
| xAI (Grok) | `http://127.0.0.1:8787/xai/v1` | Chat Completions |
| Another one (OpenRouter, a company gateway) | add it to `providers`, with its `upstream` | `format`: `openai`, `responses`, `gemini`, `codeassist` or `anthropic` |

A path that ends in `/chat/completions` is always read as Chat Completions, and one that ends
in `/responses`, as Responses, whatever the provider.

### Client by client

| Client | How to point it | Checked how |
|---|---|---|
| Gemini CLI, with an API key | `export GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:8787/gemini` and `GEMINI_API_KEY`, with the key chosen in `~/.gemini/settings.json` (`"security": {"auth": {"selectedType": "gemini-api-key"}}`). Without that choice it stops with "Invalid auth method selected" | Running (0.63.0), real API, following the README on a clean home folder |
| Gemini CLI, with a Google account (*Login with Google*) | `export CODE_ASSIST_ENDPOINT=http://127.0.0.1:8787/codeassist` | **Not checked with a real account yet.** The format follows the Gemini CLI source code and is covered by the automated tests |
| Codex CLI, with a ChatGPT subscription | In `~/.codex/config.toml`: `model_provider = "llm-dlp"` and `[model_providers.llm-dlp]` with `base_url = "http://127.0.0.1:8787/chatgpt"`, `wire_api = "responses"`, `requires_openai_auth = true` | **Not checked with a real account yet.** The format follows the Codex source code and is covered by the automated tests |
| Codex CLI, with an OpenAI API key | The same provider, with `base_url = "http://127.0.0.1:8787/openai/v1"` and `env_key = "OPENAI_API_KEY"` (instead of `requires_openai_auth`) | **Not checked with a real key yet** (automated tests only) |
| Codex extension for VS Code | Reads the same `~/.codex/config.toml` as the CLI. It does not go through the `~/.bashrc` function: run `llm-dlp ensure` once after turning on the machine | **Not checked.** OpenAI's documentation says the CLI and the extension share the configuration |
| Gemini CLI inside VS Code (integrated terminal, *Gemini CLI Companion*) | The same lines as the Gemini CLI: it is the same program, in the editor's terminal | Like the Gemini CLI |
| opencode (`google` provider) | In `opencode.json`: `"provider": {"google": {"options": {"baseURL": "http://127.0.0.1:8787/gemini/v1beta"}}}`, and the key in `GOOGLE_GENERATIVE_AI_API_KEY` | Running (1.19.0), real API, following the README on a clean home folder |
| GitHub Copilot CLI, with your own key | `export COPILOT_PROVIDER_BASE_URL=http://127.0.0.1:8787/openai/v1` (or `/xai/v1`, or another address of the table above), plus `COPILOT_PROVIDER_API_KEY` and `COPILOT_MODEL` | Running (1.0.95), real API, with the caveat below |
| GitHub Copilot in VS Code, with your own key | "Chat: Manage Language Models" > "Custom Endpoint". In the model's `url`: `http://127.0.0.1:8787/openai/v1/chat/completions` (API type Chat Completions) or `http://127.0.0.1:8787/v1/messages` (API type Messages). Like the Codex extension, start llm-dlp with `llm-dlp ensure` | **Not checked.** It follows the VS Code documentation; the format is the same as the Copilot CLI's |
| Google SDKs (`google-genai`) | The base address option (`http_options.base_url` / `httpOptions.baseUrl`) with `http://127.0.0.1:8787/gemini` | **Not checked** with an SDK running |
| `curl` | `curl http://127.0.0.1:8787/gemini/v1beta/models/MODEL:generateContent -H "x-goog-api-key: $KEY" ...` | Real API, with and without streaming, and with a tool call |

"Running, real API" means: the client itself, pointed as in the table, did a task in which
the agent read a file with an e-mail and an IP and wrote another file. The API only received
the pseudonyms and the file came out with the real values.

About Copilot:

- **Only Copilot with your own key goes through llm-dlp** (the Copilot CLI's own provider,
  or the "Custom Endpoint" of VS Code). The models that GitHub hosts and the suggestions while
  you type talk to GitHub's servers, cannot be pointed elsewhere and are **not protected**.
  Covering them would need llm-dlp to intercept the HTTPS connection (a local certificate
  authority trusted by the machine), which is what corporate gateways do for Copilot. That is
  not done: it is a decision of its own, with its own risks.
- The Copilot CLI was checked with a Gemini model, through the OpenAI-compatible address of
  Gemini. These two do not work together on their own: Gemini refuses the `"refusal": null`
  that the Copilot CLI sends back from the second message on, with or without llm-dlp. In the
  test, a relay after llm-dlp removed that field. With the OpenAI and the xAI APIs, the format
  is covered by the automated tests (against a fake API), not by a run against the real API.
- Chat Completions and Responses have a reader. Other OpenAI APIs (Assistants, Realtime) fall
  into the generic mode below.

### What is left out

- **The client's own instructions are masked too.** A name that looks like data inside the
  instructions the client sends (a table of an SQL example, a folder of an example path)
  becomes a pseudonym. It works, because the answer is translated back, but the model reads
  `t_x7k2...` where the client wrote the name. Claude Code's own identification text is the
  only one left untouched.
- Audio, video and other attached files that are neither an image nor a PDF are removed, with
  a note in their place: llm-dlp has no way to verify them.
- The summary of the model's reasoning (`thought`, `reasoning_content`, the `reasoning` items
  of Responses) comes back with the pseudonyms, not unmasked.
- **Generic mode.** A request that none of the readers understands (another `format`, another
  OpenAI API) has every text masked, and its streaming response comes back with the
  pseudonyms. Nothing leaks, but the client receives pseudonyms: do not use an agent this way.

## Commands

| Command | What it does |
|---|---|
| `llm-dlp install` | Installs and configures everything, explaining each step |
| `sudo llm-dlp install-lock` | Claude Code only works when going through llm-dlp (the lock) |
| `llm-dlp uninstall` / `sudo llm-dlp uninstall-lock` | Undoes it |
| `llm-dlp update [--version X.Y.Z] [--file FILE.tar.gz]` | Downloads the latest version (or the one asked for), checks it and puts it in place of the one running, without taking llm-dlp offline (see [Updating llm-dlp](#updating-llm-dlp)) |
| `llm-dlp reload` | Puts the installed binary in the air again, with the configuration as it is now, without taking llm-dlp offline |
| `llm-dlp status` | Shows whether it is running, in which mode and at which commit. Warns if a new binary is installed but not yet running, if OCR is missing and if the configuration reduces the protection |
| `llm-dlp stop` | Stops the proxy (it comes back on the next message). To update or apply the configuration, use `update` or `reload`, which do not leave the port empty |
| `sudo llm-dlp emergency [30m\|off]` | Lets Claude through unmasked for a limited time |
| `llm-dlp import-people FILE.csv --group CODE:NAME:EMAIL` | Teaches people |
| `llm-dlp columns FILE` | Shows how the fields of a file are understood |
| `llm-dlp test < file` | Shows the masked version of a text |
| `llm-dlp test-media FILE DIR` | Processes an image or PDF and writes the result to DIR |
| `llm-dlp measure FILE.jsonl` | Time and coverage over an old Claude Code session (counts only) |
| `llm-dlp version` | Version and commit of the binary (also `--version`) |
| `llm-dlp help` | The list of commands (also `--help`) |

Up to 0.8.0 the commands had Portuguese names (`atualizar`, `parar`, `ultima`...); they still
work.

`ensure`, `check`, `serve` and `supervise` are internal: they are called by the
Claude Code hooks and by the supervisor. `ensure` (starts llm-dlp if it is not running) is
also the one in the `~/.bashrc` function of the beta clients.

## Updating llm-dlp

```bash
llm-dlp update                   # the latest release
llm-dlp update --version 0.7.1    # a given version (also to go back to an earlier one)
llm-dlp update --file llm-dlp_0.7.1_linux_amd64.tar.gz   # a package already downloaded
```

What it does, in order:

1. Downloads the package for this machine and checks its SHA-256 against the `checksums.txt`
   published with the release (with `--file`, it uses the package you gave).
2. Puts the new binary in `~/.local/bin/llm-dlp` and keeps the previous one next to it
   (`llm-dlp.bak-DATE`; only the latest copy is kept).
3. Asks the supervisor for the swap. The supervisor is what holds the port (the proxy only
   borrows it, as with systemd socket activation): it starts the new version on the same port
   while the old one is still answering, and waits for the new one to say it is ready.
4. Tells the old one to leave: it stops accepting connections, still answers what arrives on
   the connections already open (telling the client to open the next one on the new
   version), finishes the answers in progress and exits. The supervisor then re-executes
   itself with the new binary, without letting go of the port.
5. Checks that the new version answers, with its commit and the proof of the key.

The port is never empty. That matters because Claude Code sends requests in the middle of a
turn (after a tool) without going through the hook that starts llm-dlp, and an empty port at
that moment is "Connection refused". If the new version does not get ready within 20 s, the
supervisor stops it, the command puts the previous binary back and says so; the old version
never left.

The same holds when the proxy goes down: the port stays open with the supervisor, and the
requests wait in the queue until the next proxy (about a second), instead of being refused.

The installation command (`curl ... | sh`) does the same when llm-dlp is already installed in
`~/.local/bin`. Building from source, `make instalar` installs the binary and runs
`llm-dlp reload`, which does steps 3 to 5 with the installed binary (it is also what
applies a change in `config.json`).

**From 0.8.0**: run `llm-dlp atualizar` (the name that version knows) and then `llm-dlp reload`
once (on 0.9.0 or 0.9.1: `llm-dlp update`, then `llm-dlp reload`). The update is done by the
code of the version installed, and the one of 0.8.0 does not switch `config.json` and the hooks
to the English names; `reload` (or `update`) on the new version does it. And when llm-dlp came
from 0.7.0, the old supervisor erased `supervisor.pid` as it exited: without that file, updates
up to 0.9.1 did not find the supervisor, cut the answer in progress and left the supervisor on
0.8.0. From 0.9.2 on, llm-dlp finds the supervisor by its lock, and the `reload` puts it on the
installed binary. The installation command does all of it at once.

**From 0.7.0 or older** (no update command): run the installation command once. In those
versions the proxy holds the port itself: the new supervisor takes the port the moment the old
one exits (a few milliseconds), but an answer that was being written is cut (Claude Code
repeats it). Do it between messages.

After updating, the first message of each conversation rewrites the cache once (the record
of what already went out starts over with each version).
