# llm-dlp

[![ci](https://github.com/yurivenancio30/llm-dlp/actions/workflows/ci.yml/badge.svg)](https://github.com/yurivenancio30/llm-dlp/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/yurivenancio30/llm-dlp)](https://github.com/yurivenancio30/llm-dlp/releases/latest)
[![license](https://img.shields.io/github/license/yurivenancio30/llm-dlp)](LICENSE)

**English** · [Português](docs/pt-BR/README.md)

A local proxy that **masks sensitive data between you and the LLM**. Today it works with
Claude Code.

You and your disk see everything real. The model's API only sees pseudonyms.

```
you ─► Claude Code ─► [ llm-dlp ] ─► Anthropic API
real      real         masks ►        only sees pseudonyms
real  ◄── real     ◄── unmasks ◄──    answers with pseudonyms
```

An example of what happens to a message:

```
you write:        the customer with cpf 529.982.247-25 (maria.lopes@empresa.com.br) cannot reach mysql.empresa.intra
the API receives: the customer with cpf CPF-irvikcpk (p.k3f9x2ab@d7q2m.invalid) cannot reach mysql-x7k2.invalid
the API answers:  check whether p.k3f9x2ab@d7q2m.invalid has permission on mysql-x7k2.invalid
you read:         check whether maria.lopes@empresa.com.br has permission on mysql.empresa.intra
```

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
- **Keeps the reference:** the same value always becomes the same pseudonym, so the model
  still understands that two mentions are the same person, the same server, the same account.
- **Reads images and PDFs** (OCR) and covers in black whatever is sensitive.
- **Fail-closed:** if llm-dlp goes down or does not know how to handle something, the message
  **does not go out**. There is no silent leak.
- **Is light:** a message costs 5–30 ms, the number of tokens hardly changes and the API
  cache is not disturbed.
- **Asks nothing of you day to day:** it starts with the session and comes back by itself if
  it goes down.

## Installation

It works on any Linux, including WSL on Windows. You do not need to clone the repository or
install Go:

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

`instalar` does everything, in 5 steps, asking what it needs (in Portuguese; answer `s` for
yes):

| Step | What happens | You answer |
|---|---|---|
| 1. Program | Copies llm-dlp to `~/.local/bin` | Nothing |
| 2. Configuration | Creates `~/.config/llm-dlp` with the configuration and the secret key | The internal domain, which appears in the server names (e.g. `empresa`, for `mysql.empresa.intra`), and the names that must never go out (company, client, projects). E-mails are always masked |
| 3. OCR | Installs tesseract and poppler, if missing | Yes, and the `sudo` password |
| 4. Claude Code | Connects Claude Code to llm-dlp (with a backup of `~/.claude/settings.json`) | Yes |
| 5. Lock | Makes Claude Code refuse to work outside llm-dlp | Yes, and the `sudo` password |

At the end it shows a summary with ✓ and ✗. If something is left with ✗, just run
`llm-dlp instalar` again: it skips what is already done.

Then close and open Claude Code (in VS Code, reload the window).

### Updating to a new version

1. Run the installation command above again. It replaces the program and skips what is
   already done; your configuration and your key do not change.
2. Close Claude Code (every window) and run `llm-dlp parar`. What is running is only replaced
   when the old process stops.
3. Open Claude Code and check with `llm-dlp status`.

What changed in each version is in the [CHANGELOG.md](CHANGELOG.md). After updating, the
first message of each conversation rewrites the API cache once.

## Day to day

There is nothing to run. llm-dlp starts when a session opens and comes back by itself if it
goes down.

| Situation | What happens / what to do |
|---|---|
| I want to see whether everything is fine | `llm-dlp status` |
| I want to know which version I have | `llm-dlp versao` (what changed in each one: [CHANGELOG.md](CHANGELOG.md)) |
| llm-dlp went down | It comes back by itself in ~1 s. While it is out, no message goes out |
| It does not come back, and I need Claude to fix it | `sudo llm-dlp emergencia 30m` lets Claude through **unmasked** for a limited time. Use a new session, with no client data. It goes back to normal when the time is up, or with `sudo llm-dlp emergencia sair`. Every message shows a warning |
| Image or PDF blocked | OCR is missing: the error message brings the installation command |
| I changed `config.json` | `llm-dlp parar` (it comes back on the next message, with the new configuration) |
| I want to see what was done on each message | `~/.config/llm-dlp/llm-dlp.log` (counts and times only, never values) |
| I want to undo everything | `llm-dlp desinstalar` (and `sudo llm-dlp desinstalar-trava`, if you installed the lock). Then close and open Claude Code and run `llm-dlp parar` |

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
