GO ?= go
BIN := bin/llm-dlp
PACOTE := github.com/yurivenancio30/llm-dlp
# a versão é a de internal/versao/versao.go (ver docs/versoes.md)
VERSAO := $(shell sed -n 's/^const Versao = "\(.*\)"$$/\1/p' internal/versao/versao.go)
# o commit vai dentro do binário ("llm-dlp versao"); "-mod": havia mudança não commitada
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo desconhecido)$(shell git diff --quiet HEAD 2>/dev/null || echo -mod)
LDFLAGS := -s -w -X $(PACOTE)/internal/versao.Commit=$(COMMIT)
# plataformas dos binários de uma versão (sistema/arquitetura)
PLATAFORMAS := linux/amd64 linux/arm64

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

# os binários de uma versão, um por plataforma, e as somas SHA-256 (em dist/)
dist:
	rm -rf dist && mkdir -p dist
	@for p in $(PLATAFORMAS); do \
		so=$${p%/*}; arq=$${p#*/}; saida=dist/llm-dlp_$(VERSAO)_$${so}_$${arq}; \
		echo "compilando $$saida"; \
		CGO_ENABLED=0 GOOS=$$so GOARCH=$$arq $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $$saida ./cmd/llm-dlp || exit 1; \
	done
	cd dist && sha256sum llm-dlp_* > SHA256SUMS

# confere, testa e cria a tag da versão que está em versao.go (não envia nada: ver docs/versoes.md)
release:
	./scripts/release.sh
