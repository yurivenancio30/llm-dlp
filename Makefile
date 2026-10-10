GO ?= go
BIN := bin/llm-dlp
PACOTE := github.com/yurivenancio30/llm-dlp
# a versão é a de internal/versao/versao.go (ver docs/pt-BR/versoes.md)
VERSAO := $(shell sed -n 's/^const Versao = "\(.*\)"$$/\1/p' internal/versao/versao.go)
# o commit vai dentro do binário ("llm-dlp versao"); "-mod": havia mudança não commitada
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo desconhecido)$(shell git diff --quiet HEAD 2>/dev/null || echo -mod)
LDFLAGS := -s -w -X $(PACOTE)/internal/versao.Commit=$(COMMIT)

.PHONY: build test instalar versao dist release

# CGO_ENABLED=0: binário estático, roda em qualquer Linux (glibc ou musl/Alpine)
build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/llm-dlp

test:
	$(GO) vet ./...
	$(GO) test -count=1 ./...

# instala o binário em ~/.local/bin (não mexe no Claude Code; para isso: llm-dlp instalar-claude)
instalar: build
	install -D -m 0755 $(BIN) $(HOME)/.local/bin/llm-dlp
	@echo "instalado em ~/.local/bin/llm-dlp"

versao:
	@echo $(VERSAO) "($(COMMIT))"

# ensaio da release na sua máquina, sem publicar: os pacotes .tar.gz de cada plataforma e o
# checksums.txt em dist/, montados pelo GoReleaser (.goreleaser.yaml) como o GitHub faz
dist:
	@command -v goreleaser >/dev/null || { echo "make dist precisa do goreleaser: https://goreleaser.com/install/"; exit 1; }
	goreleaser release --snapshot --clean

# confere, testa e cria a tag da versão que está em versao.go (não envia nada: ver docs/pt-BR/versoes.md)
release:
	./scripts/release.sh
