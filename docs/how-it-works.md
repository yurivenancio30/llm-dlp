# How it works

[← back to the README](../README.md) · [Português](pt-BR/how-it-works.md)

## The path of a message

1. Claude Code is configured to talk to `http://127.0.0.1:8787` (llm-dlp) instead of talking
   directly to the API. It works with a subscription or with an API key.
2. **Outbound:** llm-dlp receives the request, looks for sensitive data in every text and
   replaces it with pseudonyms. It keeps the "pseudonym → real value" table in memory only,
   and only for that request.
3. It sends the masked request to the API.
4. **Inbound:** the response arrives in chunks (streaming). llm-dlp swaps the pseudonyms back
   using the table, including when a pseudonym arrives split between two chunks.
5. Claude Code receives everything real: it shows it to you, runs commands and writes files
   with the true values.

Since Claude Code resends the whole conversation with every message, llm-dlp memoizes the
result for each text. Only what is new is examined.

## Pseudonyms

| Real value | What the API sees |
|---|---|
| `joao.silva@empresa.com.br` | `p.mxvotcyt@didl3.invalid` |
| `Joao Carlos Silva` / `JOAO CARLOS SILVA` | `Person mxvotcyt` / `PERSON MXVOTCYT` (same identifier as the e-mail, if it is the same person; the case follows the name, and each spelling comes back as it was) |
| `B123456` (João's user code) | `USER-mxvotcyt` (`user-mxvotcyt` for `b123456`: as with a name, the case follows the value) |
| `10.42.7.15` | `250.153.31.15` (the same subnet becomes the same fake subnet) |
| `mysql-dh.empresa.intra` | `mysql-x7k2.invalid` |
| `529.982.247-25` | `CPF-irvikcpk` |
| token or password | `SECRET-…` |

- They are derived from a **256-bit secret key** (HMAC-SHA256). Without the key there is no
  way to tell who is who, not even by trying lists of candidates.
- The same value always becomes the same pseudonym, across messages and across sessions.
- **Losing the key does not lose data**, because your files are real. A new one is generated
  and the pseudonyms change from then on. Even so, keep a copy of
  `~/.config/llm-dlp/key`.

## What is remembered

Some data is only recognized with a hint next to it (the word "RG", the column name). When
one of those values is masked, llm-dlp starts remembering it and recognizes it later
anywhere: in a command's output, in a file read afterwards, with no hint nearby.

| Where | What it stores |
|---|---|
| Memory | The real value, while llm-dlp is running |
| Disk (`seen.json`, `people.json`) | **Only the hash**, made with the key. It is used to recognize the value after a restart. For internal resource names (table, server...), also the type and the day it was learned and last seen (for the 90-day validity) |
| Disk (`sent.log`) | For each text already sent: a hash of the point of the conversation where it sits and, for each replaced span, where it is in the text, the type and the pseudonym. It makes the text go out identical after a restart (see below) |

No real value and no masked text is written to disk by llm-dlp.

**A learned value applies to new text.** Claude Code resends the whole conversation with
every message, and the API cache is only valid if the beginning is identical to the previous
time. So when the conversation is resent, a text that already went out goes out the same,
even if llm-dlp later learns a value that appears in it: changing it would protect nothing
(it was already sent that way) and would make the whole conversation be rewritten in the
cache. This also holds after a restart, thanks to `sent.log`.

What is frozen is the text **at that point of the conversation** (the same beginning of the
request up to it), not the text itself: the same text in a new message, or in another
conversation (the same file read again, for example), is masked with everything known now.
If the configuration or the llm-dlp version changes, or if you import people, the record
starts over and the history is masked again with the current rules (the cache is rewritten
once).

**What comes from the internet teaches nothing.** The result of `WebFetch` and `WebSearch`
(and what Claude writes into those tools) is masked where it appears, but nothing from it is
remembered: a documentation page with an example password in a URL does not turn that word
into a secret everywhere.

## Fail-closed

| Situation | What happens |
|---|---|
| llm-dlp is down | Claude Code cannot send the message |
| The request is not JSON, or has something it does not know how to handle | Refused with an error; nothing is sent |
| The request does not come from a program on this machine (a page in the browser) | Refused; nothing is sent (see [the local port](security.md#the-local-port)) |
| Another program answers on llm-dlp's port | The message is blocked: it cannot prove it is llm-dlp |
| An image or PDF that could not be verified (OCR missing, unreadable file) | The file does not go out: it is removed from the request, and the model gets a note in its place with the reason. The conversation goes on |
| Someone deletes the configuration (with the lock installed) | Claude Code refuses to work |

## Images and PDFs

The text is read by OCR and the sensitive spans are covered in black before going out. Since
OCR makes mistakes (it reads `@` as `Q`, for example), in images llm-dlp covers more than
needed: everything that looks like an e-mail, domain, IP or long number. A PDF with text
becomes masked text; a scanned PDF becomes covered images.

Without OCR installed, or with a file it cannot read (including one that declares an absurd
size, built to exhaust the memory), the image or PDF does not go out. A
note goes in its place, and Claude says in its answer what happened and the command that
installs OCR; the detail of the error stays in the log. After installing, just send the file
again. `llm-dlp status` warns when OCR is missing.

## Claude Code's own text

The system prompt and the tool definitions are written by Claude Code, but they can carry
your data (the working folder, `CLAUDE.md`, the description of an MCP tool). They are masked
like everything else, with two differences:

- A common word that the conversation learned as a name (a `billing` table) stays a word in
  these texts, as in the prose of the messages. Where it is the name (in the data, in a
  command), it is masked.
- The two blocks Claude Code uses to identify itself to the API go through untouched: the
  `x-anthropic-billing-header` header (only with its exact shape: short `name=value` pairs,
  no free text) and the identity sentence (only when it is, whole, one of its public
  sentences). With one word replaced in them, the API stops recognizing Claude Code and
  answers 429 to every message of that conversation.

## Tools that go out to the internet

When Claude uses a tool, the pseudonym is swapped for the real value before the tool runs.
That is how commands and files work with the true data:

```
Claude writes:   grep "p.mxvotcyt@didl3.invalid" arquivo.txt
what runs:       grep "joao.silva@empresa.com.br" arquivo.txt      (on your machine)
```

`WebFetch` (open a page) and `WebSearch` (search) are the exception: what Claude writes into
them goes to an outside site. If the swap were made, the real data would go out with it, and
a malicious page could ask for that on purpose. So **only in these two tools the swap is not
undone**: the site receives the pseudonym, which is useless.

The **claude.ai connectors** (`mcp__claude_ai_*` tools: Gmail, Drive, Calendar...) also
receive only the pseudonym, always, even if `config.json` has another list: they run on
Anthropic's servers, outside your machine.

The side effect: searching the web, or a connector, for something that was masked finds
nothing. To change the list of web tools, edit `tools_never_unmasked` in
`config.json`.

## Performance

Measured in real sessions and in load tests.

| Item | Value |
|---|---|
| A message in an ongoing conversation | 5–30 ms (in very long conversations, hundreds of thousands of tokens, up to ~0.1 s) |
| New text | ~0.4 ms per span (95% under 8 ms) |
| First message after a restart, 1 MB of conversation | 0.1–0.45 s with 32 cores; 0.15–0.75 s with 4 |
| A single 1 MB text | 0.1–0.3 s with 32 cores; 0.2–0.7 s with 4; 0.6–2.5 s with 1 |
| Image | 0.7–2 s the first time; ~1 ms when resent |
| Tokens | ~1.4 extra tokens per masked value; nothing is added to the context. In a real test with a file where almost every field is sensitive (245 values), the request got 0.6% larger |
| API cache | Same as using it without llm-dlp |
| Memory | 40–100 MB in normal use; in the worst measured case it levelled off at 150–210 MB (see [How much of the machine it uses](#how-much-of-the-machine-it-uses)) |
| CPU when idle | Zero |

- **Quantity does not weigh:** the time does not grow with the number of learned values or
  of pseudonyms in the conversation (measured up to 120 thousand values and 20 thousand
  distinct e-mails).
- **Everything memoized has a cap** and drops the oldest.
- **Learning a value does not rewrite the cache:** what already went out stays the same (see
  [What is remembered](#what-is-remembered)).
- **Large text** is examined in pieces, in parallel, using the cores the machine has. The
  cut is always at whitespace, so a password or token is never split. That is why a giant
  block with no whitespace at all (2 MB of base64 or minified JSON) is not divided and takes
  1 to 5 s.

## How much of the machine it uses

In short: idle, it uses no CPU; in use, it stays around 100 MB of memory and a few seconds of
CPU per hour; on disk, 30 to 60 MB. It needs no GPU and no network of its own.

| Resource | In normal use | In the worst measured case | What makes it grow |
|---|---|---|---|
| CPU | ~0.1% of one core, on average. In a real session of 1 h 49 min, with a long conversation, llm-dlp spent 7 s of CPU in total | One core busy for 3.5 ms per KB of **new** text (2 cores, low priority): reading 21 MB of never-seen files cost 77 s | New text. Resent text comes from the result cache and costs almost nothing |
| Memory | 25 MB at startup; 40–100 MB in use (95 MB in the real session above) | Levels off at 150–210 MB with 60 thousand texts that are all different, because everything remembered has a cap ([the table](development.md#what-is-stored)) | Amount of text and of different values in the session |
| Supervisor memory | ~23 MB (the process that restarts llm-dlp if it goes down) | Same | Nothing |
| Disk: program | 14 MB (`~/.local/bin/llm-dlp`) | Same | Nothing |
| Disk: data | ~20 MB after a week of use (`~/.config/llm-dlp`) | `sent.log` keeps up to 400 thousand texts; `llm-dlp.log`, up to 20 MB (rotates at 10 MB and keeps one previous file); `seen.json`, up to ~40 MB (1 million hashes) | Amount of text sent |
| Time per message | 5–30 ms | New file: median of 6.5 ms; 1 in 10 goes over 65 ms; 1 in 100 goes over 300 ms | Size of the new text |
| Startup | 0.1 s | Same | Nothing |
| Network | None of its own: it only forwards what Claude Code would send | Same | Nothing |
| OCR (image and PDF) | Only when there is an image: 0.7–2 s of CPU per new image, in a separate process (tesseract) | Same | Number of new images |

How to read it:

- **An ordinary work machine does not feel it.** The largest cost is memory, ~100 MB, similar
  to a browser tab.
- **The cost comes from new text, not from the size of the conversation.** The whole
  conversation is resent with every message, but what was already examined is not examined
  again.
- **Version 0.2.0 costs the same as 0.1.0**, despite the new lists and readers: +4 MB of
  memory at startup (the embedded lists), +1 MB in the binary and the same time per text
  (measured on the same material: 77 s against 75 s).
- The worst-case numbers were measured on purpose with few resources (2 cores, low
  priority). With more cores, large text is divided and gets proportionally faster.

## The log

Each request produces one line in `~/.config/llm-dlp/llm-dlp.log`, with counts, times and
pseudonyms (never values):

```
POST /v1/messages -> 200 | 245 replacements | 0 collisions | mask 9ms | total 2.97s | types: table 120, server 31, cpf 2
```

| Field | Meaning |
|---|---|
| `-> 200` | The API's response. `REFUSED` means llm-dlp blocked the request (fail-closed) |
| `replacements` | How many values were replaced with pseudonyms in that request (the whole conversation is resent with every message, so this number grows with it) |
| `collisions` | Pseudonyms that would stand for two different real values. Pseudonyms are short, so this is rare but possible, and more common with IPs (the fake subnet has few combinations). The outbound text stays masked; only the swap back of that pseudonym is not made, so it is not replaced with the wrong value. At worst, you see a pseudonym in a response |
| `mask` | Time spent by llm-dlp |
| `total` | Total time, dominated by the model's response |
| `types` | How many different values of each type were masked in that request (`cpf 2`: two different CPFs). Counts only |
| `collided: …` | Only when it happens: the pseudonyms that collided (`240.1.2.x` is a whole fake /24 network). The form of each one tells the type of the data. Two spellings of the same imported person (full name and first + last name) share the pseudonym and are not a collision |
| `NOT UNDONE n: …` | Only when it happens: pseudonyms generated by llm-dlp that reached the client in the response without becoming the real value again (the model changed or cut one, or quoted one from another conversation; also an address of the fake IP range). Whatever the client wrote to a file in that response kept the pseudonym. The pseudonyms themselves are listed, so you can search for them |
| `API: type: message` | Only when the API answers with an error: its type and the beginning of its message, as it came (the API only received pseudonyms). It is what tells, for example, a 429 for the account limit from a 429 for a rejected request |

A `WARNING` line says why an image or a PDF was removed from a request.

The log rotates at 10 MB at any time, also with llm-dlp running for weeks, and keeps one
previous file (`llm-dlp.log.1`): at most ~20 MB on disk.

Up to 0.9 the lines were in Portuguese (`substituições`, `RECUSADO`, `AVISO`...). `llm-dlp audit`
reads both formats, so a period that spans the update is added up.

`llm-dlp audit` sums up the log of a period (`24h` by default; also `90m`, `7d`): requests and
API errors, what was masked by type, the pseudonyms that came back without becoming the real
value, requests sent without masking and refused ones. It reads only the log, which has no
real value. In the spirit of event logging for AI systems (ISO/IEC 42001, control A.6.2.8):
what happened can be checked later without keeping the content.

```
$ llm-dlp audit 24h
llm-dlp: log of the last 24h (since 13/10 15:04)
  requests: 152 (status HTTP: 200 152)
  replacements: 31877; collisions (pseudonym left masked): 0
  masked by type (distinct values of each request, added up): table 9120, server 2210, cpf 14
  pseudonyms that came back without turning into the real value: none
  sent unmasked: none
```
