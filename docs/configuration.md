# Configuration and commands

[← back to the README](../README.md) · [Português](pt-BR/configuracao.md)

The field names of `config.json` and the command names are in Portuguese; the tables below
say what each one is for.

## Configuration

File `~/.config/llm-dlp/config.json`. After changing it, run `llm-dlp parar`.

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
| `falhar_fechado` | Refuses what it does not know how to mask. When off, a failure while masking sends the request **unmasked** (only a warning is logged). An image or PDF that could not be verified is refused anyway. Turning it off is not recommended | `true` |
| `ocr` | `modo` (`mascarar`, `bloquear` or `permitir`: mask, block or allow), `idioma` (language), `max_paginas` (maximum pages), paths of tesseract and poppler | `mascarar`, `por`, 30 |
| `objetos` | Internal resource names (see [What is detected](detection.md#5-internal-resource-names-by-structure)). `ligado` (on); `mascarar` (mask) and `propagar` (propagate): maps type → `true`/`false` (types: `servidor`, `database`, `schema`, `tabela`, `coluna`, `procedure`, `indice`, `usuario`, `namespace`, `servico`, `bucket`, `fila`). E.g. `{"mascarar": {"coluna": false}}` leaves the columns readable | on; masks all; propagates all except `coluna` and `indice` |
| `objetos.ip_publico` | Also masks public IPv4 and IPv6 as a server name (`HOST_...`). `::1`, link-local, documentation ranges and the public DNS servers (`8.8.8.8`, `1.1.1.1`, `9.9.9.9`...) stay. A private IP is always masked by the `ip` detector | off |
| `porta` | Local port of the proxy | 8787 |
| `upstream` | Address of the API | `https://api.anthropic.com` |

Names accepted in `detectores_desligados`: `segredo`, `email`, `ip`, `host`, `cpf`, `cnpj`,
`pis`, `cnh`, `rg`, `telefone`, `cep`, `cartao`, `conta`, `pix`, `endereco`, `nascimento`,
`nome`, `campo` (detection by the field name), `usuario`, `doc`, `iban`, `processo`, `mac`.

## Commands

| Command | What it does |
|---|---|
| `llm-dlp instalar` | Installs and configures everything, explaining each step |
| `sudo llm-dlp instalar-trava` | Claude Code only works when going through llm-dlp (the lock) |
| `llm-dlp desinstalar` / `sudo llm-dlp desinstalar-trava` | Undoes it |
| `llm-dlp status` | Shows whether it is running, in which mode and at which commit. Warns if a new binary is installed but not yet running |
| `llm-dlp parar` | Stops the proxy (it comes back on the next message) |
| `sudo llm-dlp emergencia [30m\|sair]` | Lets Claude through unmasked for a limited time |
| `llm-dlp importar-pessoas FILE.csv --grupo CODE:NAME:EMAIL` | Teaches people |
| `llm-dlp colunas FILE` | Shows how the fields of a file are understood |
| `llm-dlp testar < file` | Shows the masked version of a text |
| `llm-dlp testar-midia FILE DIR` | Processes an image or PDF and writes the result to DIR |
| `llm-dlp medir FILE.jsonl` | Time and coverage over an old Claude Code session (counts only) |
| `llm-dlp versao` | Version and commit of the binary |

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
