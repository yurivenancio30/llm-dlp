# Security policy

[Português](../docs/pt-BR/SECURITY.md)

## Supported versions

Fixes go into the latest release. Update before reporting, if you can
(`llm-dlp versao` shows what you have).

## Reporting a vulnerability

**Do not open a public issue.** Report it privately through GitHub:
[Report a vulnerability](https://github.com/yurivenancio30/llm-dlp/security/advisories/new)
(the "Security" tab of the repository, "Advisories").

In the report:

- the llm-dlp version (`llm-dlp versao`) and how you use it;
- a text that reproduces the problem, **with invented values**. Never send real data: no
  real names, documents, credentials or client content. A value with the same shape is
  enough;
- what you expected and what happened (what reached the API, what came back).

`llm-dlp testar < file` shows the masked version of a text and is usually enough to
reproduce a detection problem.

## What counts as a vulnerability

- A value that a documented rule should mask reaching the API in the clear.
- A response unmasked with the wrong value, or a real value sent to a tool that should only
  receive pseudonyms (`WebFetch`, `WebSearch`, claude.ai connectors).
- A way around fail-closed or the lock without `sudo`.
- A real value or masked text written to disk by llm-dlp.
- A way to reverse a pseudonym without the key.

## What is already known

The limits described in [docs/security.md](../docs/security.md#known-limits) (for example, a
name that only appears in running text, or a program run by the agent that takes the data
out) are documented behavior. Ideas to reduce them are welcome as ordinary issues, with
invented data.
