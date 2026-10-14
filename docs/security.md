# Security and limits

[← back to the README](../README.md) · [Português](pt-BR/security.md)

To report a vulnerability, see [SECURITY.md](../.github/SECURITY.md).

**What it protects:**

- The LLM API does not receive the detected values, only pseudonyms.
- The pseudonyms cannot be reversed without the key: every one comes from an HMAC-SHA256 with
  the key (the ID and, in a mixed-case object name, the case of its letters). Of a fake IP, the
  /24 network comes from the key and the last number stays (see [How it works](how-it-works.md)).
- Nothing sensitive is written to disk by llm-dlp (only hashes, also with the key).
- If something fails, the message does not go out.

**Why the lock and the emergency mode require `sudo`:** this way, nothing running as your
user, including the agent induced by a malicious page, can turn the emergency mode on or
remove the lock. If your `sudo` does not ask for a password, this barrier does not exist.

**The configuration does not have that barrier:** `config.json` belongs to your user, and a
command run by the agent can change it (turn a detector off, let images through, point
`upstream` to another server, which would then receive the messages and the API credential).
The rules that `llm-dlp install` adds to Claude Code forbid the agent to read, edit and
mention the folder in a command, but that is not a guarantee. So, while the configuration
removes protection or sends the messages somewhere other than the Anthropic API, Claude
Code shows a warning **on every message**, and so does `llm-dlp status`. If you changed
nothing and the warning showed up, check `config.json`.

**What it does not protect:**

- Data it does not detect (see [Known limits](security.md#known-limits)).
- `curl` or another program called by the agent that takes the real data out. The outbound
  protection applies to `WebFetch`, `WebSearch` and the claude.ai connectors.
- A malicious program running as your own user: it reads the key, can impersonate the proxy
  and change the configuration (the latter raises the warning on every message).
- What does not go through it: another Claude Code installation (the native Windows one, for
  example), other clients.

**Files in `~/.config/llm-dlp`:**

| File | Content |
|---|---|
| `key` | The secret key. Do not share it; keep a copy |
| `config.json` | The configuration |
| `people.json` | Hashes of people's names, e-mails and codes |
| `seen.json` | Hashes of values already masked |
| `sent.log` | For each text already sent: hashes, positions, types and pseudonyms, never values |
| `llm-dlp.log` | One line per request: counts, times and the pseudonyms that came back without becoming the real value, never values. At most ~20 MB |

Up to 0.9 these files had Portuguese names (`chave`, `pessoas.json`, `vistos.json`,
`enviados.log`). The first time a version from 0.10 on needs one of them, it copies the old file
to the new name (the key is the same, so the pseudonyms keep their identifiers). The old files stay, in case you go back
to an older version; `enviados.log` is deleted, since the record starts over with each version
anyway.

## The local port

llm-dlp listens only on `127.0.0.1`, but the browser and the other users of the machine also
reach that address. What each one can do:

| Who tries | What happens |
|---|---|
| A page open in the browser that points its own domain to `127.0.0.1` (DNS rebinding) or sends the request from another site | Refused (403). Only a caller that uses the local address (`127.0.0.1`, `localhost`) and is not a web page is served. Nothing goes to the API |
| Another user of the machine who wants to read your last (masked) conversation | Cannot read it: it only goes out with a proof that only whoever reads your key (`key`) can compute. That is what the `llm-dlp last` command does |
| Another program that takes the port while llm-dlp is down, to receive the unmasked conversation and the API credential | On every message, Claude Code asks the port whether it is llm-dlp and requires the answer to a fresh challenge, which only whoever reads the key can compute. Without the proof, the message is blocked |

Whoever runs as **your** user reads the key and gets through the three barriers (see "What it
does not protect", above).

The port is held by the supervisor, not by the proxy: when the proxy goes down or is swapped
(`llm-dlp update`, `llm-dlp reload`), the port stays open and no other program can take
it in between. The window for the last row above is only when the supervisor itself is not
running (before the first session, or after `llm-dlp stop`).

When updating from a version up to 0.3.0, the old process still running cannot give that
proof, and the messages are blocked until you close Claude Code and run `llm-dlp stop`.

## Known limits

**What goes through unmasked:**

- A person's name that is not known and appears without a field name (running text, or a
  piece of CSV read without the header line).
- A field whose name is outside the vocabulary, until you add it to `extra_fields`.
- **An internal resource name (database, schema, table, system) that only appears in
  sentences**, without ever having gone through a structure llm-dlp recognizes (see
  [What is detected](detection.md#5-internal-resource-names-by-structure)). To always mask a
  name, use `terms`.
- Sensitive personal data (health, religion, ethnicity) in running text. It is only detected
  in a field with that name.
- Financial values (balance, income, limit). What ties them to a person (name, CPF, account)
  is masked.
- A letters-only password (`senha: abacaxi`) or a digits-only one with up to 5 digits, unless
  inside a URL.
- Very small or stylized text in an image, which OCR does not read.

**Other limits:**

- With hundreds of different subnets in the same conversation, two may get the same fake
  subnet. Their IPs stay masked outbound, but are not swapped back (you see the fake IP), so
  a command is not pointed at the wrong host.
- After a restart, a value remembered only by its hash is recognized when it appears "loose"
  or separated by `= & : / ? @ | # +`. Glued directly to letters, only after it reappears
  loose once.
- A value learned later is not applied to what already went out: if a value went through in
  the clear in a message and was only recognized afterwards, that message keeps going out in
  the clear in the resends of the same conversation (it had already been sent that way). The
  freeze is per point of the conversation: the same text in a new message, or in another
  conversation, already goes out masked.
- The exception is a conversation that starts **exactly like** a previous one: the same
  Claude Code instructions, the same tools and the same messages up to that text (a resumed
  session, for example). To llm-dlp that is a resend, and the text goes out as it did the
  first time. The content is the same that had already been sent; nothing new leaks, but the
  value learned later is not applied there either.
- A letters-only password is detected inside a URL (`mysql://app:SENHA@host`), but not after a
  label (`senha: SENHA`, `DB_PASSWORD=SENHA`): there the detector requires a digit or a
  symbol, so it is not confused with code and ordinary text.
- Images and PDFs: the result is memoized only while llm-dlp is running. After a restart
  they are examined again with what is known at that moment.
- Only Claude Code is locked and checked on every message. A beta client (Gemini CLI, Codex,
  opencode, Copilot) is protected while its address points to llm-dlp, and nothing warns you
  if it stops pointing there.
- The Anthropic, the Gemini (also inside Code Assist) and the OpenAI (Chat Completions and
  Responses) formats have their own reader (see
  [Other clients and APIs (beta)](configuration.md#other-clients-and-apis-beta) and [Plugging in another LLM API](development.md#plugging-in-another-llm-api)).

## Design decisions

Behaviors that are choices, not defects:

| Behavior | Why |
|---|---|
| Only internal network IPs are masked | A public IP (of a service on the internet) identifies neither the company nor a person, and masking it would get in the way of network troubleshooting |
| Public domains are not masked | `github.com`, `pypi.org`, file names (`config.py`) have the format of a domain; masking them makes the answers worse and protects nothing. Only the `internal_domains` are masked |
| An e-mail needs a domain with a dot | `joao@localhost`, `root@servidor` and `ssh user@host` are not treated as e-mail (they would be false positives in commands). An e-mail spelled out (`joao [at] empresa`) is not recognized either |
| `WebFetch`, `WebSearch` and claude.ai connectors receive pseudonyms | What leaves the machine cannot carry the real data. Effect: searching the web or a connector for something masked finds nothing |
| What comes from the web is not remembered | An example password on a page is not your secret. It is masked where it appears, but does not become a secret everywhere |
| A pseudonym with a collision does not go back to the real value | Two values with the same pseudonym: the outbound text stays masked, but the swap back is not made, so it is not replaced with the wrong value. The tool receives the pseudonym and may fail |
| A value learned after it already went through | If a value went through in the clear in a message and was only recognized afterwards, it is masked from then on, in new text only. The old message keeps going out the same in resends: it was already sent that way, and changing it would rewrite the whole conversation in the cache without protecting anything. The model may link the pseudonym to the value it saw before |

## Connectors and MCP

| Where the MCP runs | What it receives | Status |
|---|---|---|
| On your machine (local MCP: database, files) | The **real** value: the call is unmasked before it runs | Correct: it is the same as a command of yours |
| On your machine, but talking to a third-party service (an API on the internet, another LLM) | The **real** value, which leaves your machine without going through llm-dlp | **Outside the protection.** Only use these MCPs with data that may go out |
| On Anthropic's side (claude.ai connectors, `mcp_tool_use` and `server_tool_use`) | The **pseudonym**: the call is made by the API, which only knows the pseudonyms | A query with a masked value fails or finds nothing. What these connectors bring is already on Anthropic's side and does not go through llm-dlp |
