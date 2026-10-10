# Contributing to llm-dlp

[Português](../docs/pt-BR/CONTRIBUTING.md)

Thank you for helping. This page says how the project is organized and what a change needs
before it is merged.

## Before anything: no real data

llm-dlp exists so that sensitive data does not leave the machine. **Never put real data** in
an issue, a test, a commit or a pull request: no real names of people, companies, servers,
tables or clients, and no real documents or credentials. Reproduce the case with invented
values that have the same shape (`vendashx`, `acme_pedidos`, `529.982.247-25` is the CPF
used in the examples).

## Building and testing

You need [Go](https://go.dev/dl/) (the version in `go.mod`).

```bash
make build   # static binary at bin/llm-dlp
make test    # go vet and all the tests (a few minutes)
gofmt -l cmd internal   # must print nothing
```

The code map, what each file family does and how the project was tested are in
[docs/development.md](../docs/development.md).

## The rule for masking changes

A leak is worse than masking too much: **when in doubt, mask**.

- A rule that **masks more** needs a positive case (it is masked) and a negative one (what
  must stay stays).
- A rule that **leaves a name in the clear** needs a structural guarantee that the name is
  public (never "it looks public"), must fail closed, and needs a case in
  `internal/mask/casos_publicos_test.go`. The rules that were tried and rejected, and why,
  are listed under "Public only with proof" in [docs/structures.md](../docs/structures.md).
- Compare before and after on the same texts: what the previous version masked and yours
  leaves in the clear is what matters most.
- If the change costs time, run the benchmarks (`go test ./internal/mask -bench Frio`).

## Languages

| What | Language |
|---|---|
| Code: identifiers, comments, command names, program messages | Portuguese (there is a small glossary in [docs/development.md](../docs/development.md#the-code-is-in-portuguese)) |
| Commit messages, pull requests, issues | English |
| Documentation | English in `README.md` and `docs/`, Portuguese in `docs/pt-BR/`. Change both in the same pull request |

## Commits

Commits follow [Conventional Commits](https://www.conventionalcommits.org), in English:

```
type(scope): short description in lowercase, no final period

Why the change was needed and what it does, in sentences or a short list. For a masking
change, what was measured before and after.
```

- The subject line has at most 72 characters.
- Types: `feat` (new behavior), `fix`, `perf`, `refactor` (no behavior change), `docs`,
  `test`, `build` (Makefile, GoReleaser, packaging), `ci` (workflows), `chore`.
- Scopes in use: `mask`, `proxy`, `install`, `cli`, `ocr`, `config`, `release`, `readme`. The
  scope is optional.
- A change that breaks compatibility (the format of `config.json`, of the files on disk or of
  the pseudonyms) has `!` after the type and a `BREAKING CHANGE:` footer.

Examples from the history:

```
feat(mask): table reader for any layout
fix(proxy): text already sent always goes out identical
perf(mask): faster readers
docs: explain what each field of the log means
```

## Pull requests

- One subject per pull request.
- `make test` and `gofmt` pass (the CI runs both).
- If the change is visible to whoever uses the program, add a line to `CHANGELOG.md` and to
  `docs/pt-BR/CHANGELOG.md`.
- Say what you tested and, for masking, what you measured.

## Releases

Versions follow `MAJOR.MINOR.PATCH`. What each number means and the steps of a release are
in [docs/versioning.md](../docs/versioning.md).

## Security

To report a vulnerability, do not open a public issue: see [SECURITY.md](SECURITY.md).
