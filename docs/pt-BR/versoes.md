# Versões e lançamentos

[← voltar ao README](../../README.pt-BR.md) · [English](../versioning.md)

## Como saber a versão

```bash
llm-dlp versao        # llm-dlp 0.2.1 (4b6c73c): o binário instalado
llm-dlp status        # o commit que está no ar, e avisa se o instalado é mais novo
```

O número é a versão; entre parênteses, o commit de que o binário foi compilado (`-mod` no fim
quer dizer que havia mudança não commitada). O que mudou em cada versão está no
[CHANGELOG.md](CHANGELOG.md).

## O que cada número significa

A versão tem três números: `MAIOR.MENOR.CORREÇÃO`.

| Sobe | Quando | Exemplo | O que você faz ao atualizar |
|---|---|---|---|
| CORREÇÃO (`0.2.0` → `0.2.1`) | Conserto que não muda de propósito o que é mascarado: erro, desempenho, mensagem, documentação, empacotamento | Um comando que falhava, um texto que demorava | Nada |
| MENOR (`0.2.1` → `0.3.0`) | Muda o que é mascarado (regra ou leitor novo, lista regenerada) ou acrescenta comando ou campo de configuração | Um formato novo passa a ser lido | Nada. Vale ler o CHANGELOG |
| MAIOR (`0.x` → `1.0`) | Quebra compatibilidade: formato do `config.json`, dos arquivos em disco (`vistos.json`, `enviados.log`, `pessoas.json`) ou dos pseudônimos; comando removido | O `config.json` antigo não é mais aceito | Seguir o que o CHANGELOG mandar |

Enquanto a versão começar com `0.`, o projeto ainda está mudando rápido: uma versão MENOR pode
mudar bastante o que é mascarado.

Trocar de versão no meio de uma conversa faz o histórico dela ser mascarado de novo com as
regras novas, e o cache da API é regravado uma vez. Não se perde nada.

## O que vem numa release

Cada release no [GitHub](https://github.com/yurivenancio30/llm-dlp/releases) tem:

| Arquivo | O que é |
|---|---|
| `llm-dlp_X.Y.Z_linux_amd64.tar.gz`, `llm-dlp_X.Y.Z_linux_arm64.tar.gz` | O binário estático de cada arquitetura, com a LICENSE, o README e o CHANGELOG |
| `checksums.txt` | A soma SHA-256 de cada pacote (o [install.sh](../../install.sh) confere antes de instalar) |

Os pacotes são montados pelo [GoReleaser](https://goreleaser.com), a partir do
[.goreleaser.yaml](../../.goreleaser.yaml). A release 0.2.0 e as anteriores publicavam o
binário solto e um arquivo `SHA256SUMS`.

## Como lançar uma versão

O número fica num lugar só, `internal/versao/versao.go`. A tag do git e o CHANGELOG têm de
dizer o mesmo, e um teste (`TestVersaoBateComOChangelog`) e o `make release` conferem.

1. Na branch `main`, com tudo commitado, escolha o número pela tabela acima.
2. Edite `internal/versao/versao.go` e acrescente a seção da versão no topo do
   `CHANGELOG.md` e do `docs/pt-BR/CHANGELOG.md` (`## [X.Y.Z] - AAAA-MM-DD`). Commite
   (`chore(release): X.Y.Z`).
3. Rode `make release`. Ele confere a branch, a versão e o CHANGELOG, roda os testes e cria a
   tag `vX.Y.Z` assinada. Não envia nada.
4. Envie: `git push origin main vX.Y.Z`.
5. O GitHub (`.github/workflows/release.yml`) roda os testes de novo, e o GoReleaser monta os
   pacotes e publica a release com as notas tiradas do CHANGELOG.

| Comando | O que faz |
|---|---|
| `make versao` | Mostra a versão e o commit do código |
| `make test` | `go vet` e todos os testes |
| `make build` | Compila `bin/llm-dlp` para a sua máquina |
| `make dist` | Ensaia a release na sua máquina, sem publicar: pacotes e `checksums.txt` em `dist/` (precisa do [GoReleaser](https://goreleaser.com/install/)) |
| `make release` | Confere, testa e cria a tag da versão |

A cada envio para a `main` e a cada pull request, `.github/workflows/ci.yml` confere a
formatação, roda os testes e valida o `install.sh` e a configuração do GoReleaser.
