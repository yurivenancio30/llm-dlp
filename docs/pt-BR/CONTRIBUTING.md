# Como contribuir com o llm-dlp

[English](../../.github/CONTRIBUTING.md)

Obrigado por ajudar. Esta página diz como o projeto é organizado e o que uma mudança precisa
ter antes de entrar.

## Antes de tudo: nada de dado real

O llm-dlp existe para o dado sensível não sair da máquina. **Nunca ponha dado real** numa
issue, num teste, num commit ou num pull request: nada de nome real de pessoa, empresa,
servidor, tabela ou cliente, nem documento ou credencial de verdade. Reproduza o caso com
valores inventados que tenham a mesma forma (`vendashx`, `acme_pedidos`; `529.982.247-25` é o
CPF usado nos exemplos).

## Compilar e testar

Precisa do [Go](https://go.dev/dl/) (a versão que está no `go.mod`).

```bash
make build   # binário estático em bin/llm-dlp
make test    # go vet e todos os testes (alguns minutos)
gofmt -l cmd internal   # não pode mostrar nada
```

O mapa do código, o que cada família de arquivos faz e como o projeto foi testado estão em
[desenvolvimento.md](desenvolvimento.md).

## A regra para mudanças de mascaramento

Vazar é pior do que mascarar a mais: **na dúvida, mascara**.

- Uma regra que **mascara mais** precisa de um caso positivo (é mascarado) e de um negativo (o
  que tem de ficar, fica).
- Uma regra que **deixa um nome em claro** precisa de uma garantia de estrutura de que o nome
  é público (nunca "parece público"), tem de falhar fechada e precisa de um caso em
  `internal/mask/casos_publicos_test.go`. As regras que foram tentadas e recusadas, e por
  quê, estão em "Público só com prova", em [estruturas.md](estruturas.md).
- Compare o antes e o depois nos mesmos textos: o que a versão anterior mascarava e a sua
  deixa em claro é o que mais importa.
- Se a mudança custa tempo, rode os benchmarks (`go test ./internal/mask -bench Frio`).

## Idiomas

| O que | Idioma |
|---|---|
| Código: identificadores, comentários, nomes de comando, mensagens do programa | Português |
| Mensagens de commit, pull requests, issues | Inglês |
| Documentação | Inglês em `README.md` e `docs/`, português em `docs/pt-BR/`. Mude os dois no mesmo pull request |

## Commits

Os commits seguem o [Conventional Commits](https://www.conventionalcommits.org), em inglês:

```
type(scope): short description in lowercase, no final period

Why the change was needed and what it does, in sentences or a short list. For a masking
change, what was measured before and after.
```

- A linha de assunto tem no máximo 72 caracteres.
- Tipos: `feat` (comportamento novo), `fix`, `perf`, `refactor` (sem mudar comportamento),
  `docs`, `test`, `build` (Makefile, GoReleaser, empacotamento), `ci` (fluxos do GitHub),
  `chore`.
- Escopos em uso: `mask`, `proxy`, `install`, `cli`, `ocr`, `config`, `release`, `readme`. O
  escopo é opcional.
- Mudança que quebra compatibilidade (formato do `config.json`, dos arquivos em disco ou dos
  pseudônimos) leva `!` depois do tipo e um rodapé `BREAKING CHANGE:`.

Exemplos do histórico:

```
feat(mask): table reader for any layout
fix(proxy): text already sent always goes out identical
perf(mask): faster readers
docs: explain what each field of the log means
```

## Pull requests

- Um assunto por pull request.
- `make test` e `gofmt` passam (o CI roda os dois).
- Se a mudança aparece para quem usa o programa, acrescente uma linha no `CHANGELOG.md` e no
  `docs/pt-BR/CHANGELOG.md`.
- Diga o que você testou e, para mascaramento, o que mediu.

## Lançamentos

As versões seguem `MAIOR.MENOR.CORREÇÃO`. O que cada número significa e os passos de um
lançamento estão em [versoes.md](versoes.md).

## Segurança

Para relatar uma vulnerabilidade, não abra uma issue pública: veja [SECURITY.md](SECURITY.md).
