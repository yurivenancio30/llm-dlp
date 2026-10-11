# Configuration and commands

[← back to the README](../README.md) · [Português](pt-BR/configuracao.md)

The field names of `config.json` and the command names are in Portuguese; the tables below
say what each one is for.

## Configuration

File `~/.config/llm-dlp/config.json`. After changing it, run `llm-dlp parar`.

While the configuration removes protection (`falhar_fechado` off, `ocr.modo` set to
`permitir`, `objetos` off, any detector turned off) or `upstream` is not the Anthropic API,
Claude Code shows a warning on every message (see [Security](security.md)). The warning
does not go to the API.

| Field | What it is for | Default |
|---|---|---|
| `dominios_internos` | Pieces that identify your internal hostnames (`["empresa"]` catches `mysql.empresa.intra`) | empty |
| `emails_liberados` | E-mails that go through unmasked | empty |
| `dominios_email_liberados` | E-mail domains that go through unmasked | `example.com`, `anthropic.com`… |
| `papeis_host` | First part of the hostname that may stay visible (`mysql` in `mysql-x7k2.invalid`) | `mysql`, `postgres`, `redis`… |
| `campos_extras` | Extra words for the field names | empty |
| `padroes_extras` | Your own regexes | empty |
| `termos` | Exact words to mask | empty |
| `detectores_desligados` | Detectors to turn off (list below) | empty |
| `detectores_opcionais` | Detectors to turn on. Today only `quase`: sex, age, marital status, occupation, nationality, income, latitude/longitude | empty |
| `ferramentas_sem_desmascarar` | Web tools: they receive pseudonyms only, and what they bring is not remembered. The claude.ai connectors (`mcp__claude_ai_*`) always receive pseudonyms only, regardless of this list | `WebFetch`, `WebSearch` |
| `documentos_sem_contexto` | Masks digit-only CPF and CNPJ even without the word "cpf"/"cnpj" nearby, if the check digit matches. About 1 in 100 random numbers of that length also matches and is masked needlessly | `true` |
| `falhar_fechado` | Refuses what it does not know how to mask. When off, a failure while masking sends the request **unmasked** (only a warning is logged). An image or PDF that could not be verified never goes out, whatever the value of this field. Turning it off is not recommended | `true` |
| `ocr` | `modo` (`mascarar`, `bloquear` or `permitir`: mask, block or allow), `idioma` (language), `max_paginas` (maximum pages), paths of tesseract and poppler. With `bloquear`, and with `mascarar` when the file cannot be verified (OCR missing, unreadable file), it is removed from the request and the model gets a note in its place | `mascarar`, `por`, 30 |
| `objetos` | Internal resource names (see [What is detected](detection.md#5-internal-resource-names-by-structure)). `ligado` (on); `mascarar` (mask) and `propagar` (propagate): maps type → `true`/`false` (types: `servidor`, `database`, `schema`, `tabela`, `coluna`, `procedure`, `indice`, `usuario`, `namespace`, `servico`, `bucket`, `fila`). E.g. `{"mascarar": {"coluna": false}}` leaves the columns readable | on; masks all; propagates all except `coluna` and `indice` |
| `objetos.ip_publico` | Also masks public IPv4 and IPv6 as a server name (`HOST_...`). `::1`, link-local, documentation ranges and the public DNS servers (`8.8.8.8`, `1.1.1.1`, `9.9.9.9`...) stay. A private IP is always masked by the `ip` detector | off |
| `porta` | Local port of the proxy | 8787 |
| `upstream` | Address of the API | `https://api.anthropic.com` |
| `provedores` | **Beta.** Other APIs served on the same port, under the path `/<name>` (see [Other clients and APIs](#other-clients-and-apis-beta)). Each one has `upstream` and `formato` (`gemini`, `openai` or `anthropic`; any other value falls into the generic mode) | `gemini`, `openai` and `xai`, each with the address of its API |

Names accepted in `detectores_desligados`: `segredo`, `email`, `ip`, `host`, `cpf`, `cnpj`,
`pis`, `cnh`, `rg`, `telefone`, `cep`, `cartao`, `conta`, `pix`, `endereco`, `nascimento`,
`nome`, `campo` (detection by the field name), `usuario`, `doc`, `iban`, `processo`, `mac`.

## Other clients and APIs (beta)

> [!WARNING]
> **Beta.** Everything in this section (the Gemini and OpenAI formats, and the Gemini CLI,
> opencode and Copilot clients) is beta. Only Claude Code is stable. Beta clients have **no
> lock**: if the client stops pointing to llm-dlp, it talks straight to the API, unmasked, and
> nothing warns you. Read [what beta means](#what-beta-means) before using them with real data.

llm-dlp was built around Claude Code, and `llm-dlp instalar` sets up **only Claude Code**.
Other clients work with the same masking, but you point them by hand and they have no lock.

| Client | Status | Installed and locked by `llm-dlp instalar` |
|---|---|---|
| Claude Code, with a subscription (Pro, Max) or an API key | **Stable** | Yes |
| Gemini CLI, with a Google account or a Gemini API key | Beta | No |
| Codex CLI, with a ChatGPT subscription or an OpenAI API key | Beta | No |
| opencode | Beta | No |
| GitHub Copilot (CLI and VS Code), with your own key | Beta | No |
| Any other client of the Anthropic, Gemini or OpenAI (Chat Completions, Responses) API | Beta | No |
| GitHub Copilot with the GitHub subscription (the models GitHub hosts) | **Not supported** | Copilot does not let you change the address it talks to |

### What beta means

- **It is on by default and comes in the same binary.** It is tested (automated tests and
  real clients, see the table [Client by client](#client-by-client)), but it has been used
  much less than the Claude Code path.
- **It may change in a MINOR version**, without the notice that the
  [versions table](versioning.md#what-each-number-means) gives to stable features: the
  addresses, the `provedores` field and what is read in each format.
- **It is isolated from Claude Code.** The code of each format only runs for requests on its
  own path (`/gemini`, `/openai`, `/xai`...). A request from Claude Code goes out byte for
  byte the same with or without these providers, and a malformed request or response from
  another format does not bring llm-dlp down. Both are checked by automated tests. The
  conversation memory is shared: a name masked in a Gemini conversation is also masked in
  Claude Code (more masking, never less).
- **Report problems** in the [issues](https://github.com/yurivenancio30/llm-dlp/issues), saying
  the client and its version.

### Claude Code and the beta clients

| | Claude Code (stable) | Gemini CLI, opencode, Copilot and other clients (beta) |
|---|---|---|
| What goes out is masked and what comes back is unmasked (text, tool calls and results, files the agent writes, images and PDFs) | Yes | Yes, the same engine |
| `llm-dlp ultima` and the log | Yes | Yes |
| Pointed to llm-dlp by `llm-dlp instalar` | Yes (step 4) | **No.** You set the address in the client, as in the tables below |
| llm-dlp is started for you | Yes, when a session opens | **No.** A function in `~/.bashrc` runs `llm-dlp garantir` before opening the client ([README](../README.md#gemini-cli-codex-opencode-and-copilot-beta)). Without it, if llm-dlp is not running, the client gets "connection refused" and nothing goes out |
| Account types that go through llm-dlp | Subscription (Pro, Max) and API key: Claude Code always talks to the Anthropic API | The ones pointed in [Client by client](#client-by-client): the Gemini CLI with a Google account or an API key, Codex with a ChatGPT subscription or an API key. **Not** Vertex AI, and **not** Copilot with the models GitHub hosts: they talk to other servers and go out **unmasked** |
| Check on every message (llm-dlp is running, is the right version and proves it is itself) | Yes | **No** |
| Lock: the client refuses to work outside llm-dlp | Yes (step 5) | **No.** If the address is removed from the client's configuration, the client talks straight to the API, unmasked, and nothing warns you |

The step by step, with the lines for `~/.bashrc`, is in the
[README](../README.md#gemini-cli-codex-opencode-and-copilot-beta).

In short: with another client, the protection lasts while its address points to llm-dlp. Check
it with `llm-dlp ultima` after the first message: it must show your conversation, with the
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
| Another one (OpenRouter, a company gateway) | add it to `provedores`, with its `upstream` | `formato`: `openai`, `responses`, `gemini`, `codeassist` or `anthropic` |

A path that ends in `/chat/completions` is always read as Chat Completions, and one that ends
in `/responses`, as Responses, whatever the provider.

### Client by client

| Client | How to point it | Checked how |
|---|---|---|
| Gemini CLI, with an API key | `export GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:8787/gemini` and `GEMINI_API_KEY`, with the key chosen in `~/.gemini/settings.json` (`"security": {"auth": {"selectedType": "gemini-api-key"}}`). Without that choice it stops with "Invalid auth method selected" | Running (0.63.0), real API, following the README on a clean home folder |
| Gemini CLI, with a Google account (*Login with Google*) | `export CODE_ASSIST_ENDPOINT=http://127.0.0.1:8787/codeassist` | **Not checked with a real account yet.** The format follows the Gemini CLI source code and is covered by the automated tests |
| Codex CLI, with a ChatGPT subscription | In `~/.codex/config.toml`: `model_provider = "llm-dlp"` and `[model_providers.llm-dlp]` with `base_url = "http://127.0.0.1:8787/chatgpt"`, `wire_api = "responses"`, `requires_openai_auth = true` | **Not checked with a real account yet.** The format follows the Codex source code and is covered by the automated tests |
| Codex CLI, with an OpenAI API key | The same provider, with `base_url = "http://127.0.0.1:8787/openai/v1"` and `env_key = "OPENAI_API_KEY"` (instead of `requires_openai_auth`) | **Not checked with a real key yet** (automated tests only) |
| opencode (`google` provider) | In `opencode.json`: `"provider": {"google": {"options": {"baseURL": "http://127.0.0.1:8787/gemini/v1beta"}}}`, and the key in `GOOGLE_GENERATIVE_AI_API_KEY` | Running (1.19.0), real API, following the README on a clean home folder |
| GitHub Copilot CLI, with your own key | `export COPILOT_PROVIDER_BASE_URL=http://127.0.0.1:8787/openai/v1` (or `/xai/v1`, or another address of the table above), plus `COPILOT_PROVIDER_API_KEY` and `COPILOT_MODEL` | Running (1.0.95), real API, with the caveat below |
| GitHub Copilot in VS Code, with your own key | "Chat: Manage Language Models" > "Custom Endpoint". In the model's `url`: `http://127.0.0.1:8787/openai/v1/chat/completions` (API type Chat Completions) or `http://127.0.0.1:8787/v1/messages` (API type Messages) | **Not checked.** It follows the VS Code documentation; the format is the same as the Copilot CLI's |
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
- The summary of the model's reasoning (`thought`, `reasoning_content`) comes back with the
  pseudonyms, not unmasked.
- **Generic mode.** A request that none of the readers understands (another `formato`, the
  Responses API) has every text masked, and its streaming response comes back with the
  pseudonyms. Nothing leaks, but the client receives pseudonyms: do not use an agent this way.

## Commands

| Command | What it does |
|---|---|
| `llm-dlp instalar` | Installs and configures everything, explaining each step |
| `sudo llm-dlp instalar-trava` | Claude Code only works when going through llm-dlp (the lock) |
| `llm-dlp desinstalar` / `sudo llm-dlp desinstalar-trava` | Undoes it |
| `llm-dlp status` | Shows whether it is running, in which mode and at which commit. Warns if a new binary is installed but not yet running, if OCR is missing and if the configuration reduces the protection |
| `llm-dlp parar` | Stops the proxy (it comes back on the next message) |
| `sudo llm-dlp emergencia [30m\|sair]` | Lets Claude through unmasked for a limited time |
| `llm-dlp importar-pessoas FILE.csv --grupo CODE:NAME:EMAIL` | Teaches people |
| `llm-dlp colunas FILE` | Shows how the fields of a file are understood |
| `llm-dlp testar < file` | Shows the masked version of a text |
| `llm-dlp testar-midia FILE DIR` | Processes an image or PDF and writes the result to DIR |
| `llm-dlp medir FILE.jsonl` | Time and coverage over an old Claude Code session (counts only) |
| `llm-dlp versao` | Version and commit of the binary (also `--version`) |
| `llm-dlp ajuda` | The list of commands (also `--help`) |

`garantir`, `verificar`, `servir` and `supervisionar` are internal: they are called by the
Claude Code hooks and by the supervisor.

## Updating llm-dlp

Installing the new binary does not replace what is running: the old process keeps running
until it is stopped.

1. Install the new binary (keep the previous one, if you want to be able to go back):

   ```bash
   cp ~/.local/bin/llm-dlp ~/.local/bin/llm-dlp.bak-$(date +%Y%m%d-%H%M%S)
   ```

   With the release binary (the usual path), run the installer again; it replaces the
   program and skips what is already done:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/yurivenancio30/llm-dlp/main/install.sh | sh
   ~/.local/bin/llm-dlp versao        # shows the new version and commit
   ```

   Or building from source:

   ```bash
   make instalar
   ~/.local/bin/llm-dlp versao
   ```

2. **Close Claude Code** (every window and session) and, in a terminal, run `llm-dlp parar`.
   Stopping with a conversation open leaves that conversation without an answer. Check with
   `pgrep -a llm-dlp` that no process is left.
3. Open Claude Code: the session hook starts the new binary. Check with `llm-dlp status` (the
   commit must be the new one, with no warning).

After updating, the first message of each conversation rewrites the cache once (the record
of what already went out starts over with each version).
