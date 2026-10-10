# Versions and releases

[← back to the README](../README.md) · [Português](pt-BR/versoes.md)

## How to find out the version

```bash
llm-dlp versao        # llm-dlp 0.2.1 (4b6c73c): the installed binary
llm-dlp status        # the commit that is running, and a warning if the installed one is newer
```

The number is the version; in parentheses, the commit the binary was built from (`-mod` at
the end means there were uncommitted changes). What changed in each version is in the
[CHANGELOG.md](../CHANGELOG.md).

## What each number means

The version has three numbers: `MAJOR.MINOR.PATCH`.

| Goes up | When | Example | What you do when updating |
|---|---|---|---|
| PATCH (`0.2.0` → `0.2.1`) | A fix that does not change on purpose what is masked: bug, performance, message, documentation, packaging | A command that failed, a text that took too long | Nothing |
| MINOR (`0.2.1` → `0.3.0`) | Changes what is masked (new rule or reader, regenerated list) or adds a command or a configuration field | A new format starts being read | Nothing. The CHANGELOG is worth reading |
| MAJOR (`0.x` → `1.0`) | Breaks compatibility: the format of `config.json`, of the files on disk (`vistos.json`, `enviados.log`, `pessoas.json`) or of the pseudonyms; a removed command | The old `config.json` is no longer accepted | Follow what the CHANGELOG says |

While the version starts with `0.`, the project is still changing fast: a MINOR version may
change a lot of what is masked.

Switching versions in the middle of a conversation makes its history be masked again with the
new rules, and the API cache is rewritten once. Nothing is lost.

## What a release contains

Each release on [GitHub](https://github.com/yurivenancio30/llm-dlp/releases) has:

| File | What it is |
|---|---|
| `llm-dlp_X.Y.Z_linux_amd64.tar.gz`, `llm-dlp_X.Y.Z_linux_arm64.tar.gz` | The static binary for each architecture, with the LICENSE, the README and the CHANGELOG |
| `checksums.txt` | The SHA-256 of each archive ([install.sh](../install.sh) checks it before installing) |

The archives are built by [GoReleaser](https://goreleaser.com) from
[.goreleaser.yaml](../.goreleaser.yaml). Release 0.2.0 and earlier published bare binaries
and a `SHA256SUMS` file.

## How to release a version

The number lives in a single place, `internal/versao/versao.go`. The git tag and the
CHANGELOG must say the same, and a test (`TestVersaoBateComOChangelog`) and `make release`
check it.

1. On the `main` branch, with everything committed, choose the number from the table above.
2. Edit `internal/versao/versao.go` and add the version's section at the top of
   `CHANGELOG.md` and of `docs/pt-BR/CHANGELOG.md` (`## [X.Y.Z] - YYYY-MM-DD`). Commit
   (`chore(release): X.Y.Z`).
3. Run `make release`. It checks the branch, the version and the CHANGELOG, runs the tests
   and creates the signed `vX.Y.Z` tag. It sends nothing.
4. Send it: `git push origin main vX.Y.Z`.
5. GitHub (`.github/workflows/release.yml`) runs the tests again, and GoReleaser builds the
   archives and publishes the release with the notes taken from the CHANGELOG.

| Command | What it does |
|---|---|
| `make versao` | Shows the version and the commit of the code |
| `make test` | `go vet` and all the tests |
| `make build` | Builds `bin/llm-dlp` for your machine |
| `make dist` | Rehearses the release on your machine, without publishing: archives and `checksums.txt` in `dist/` (needs [GoReleaser](https://goreleaser.com/install/)) |
| `make release` | Checks, tests and creates the version's tag |

On every push to `main` and on every pull request, `.github/workflows/ci.yml` checks the
formatting, runs the tests and validates `install.sh` and the GoReleaser configuration.
