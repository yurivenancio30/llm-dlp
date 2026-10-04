GO ?= go
BIN := bin/llm-dlp
# o commit vai dentro do binário ("llm-dlp versao"); "-mod": havia mudança não commitada
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo desconhecido)$(shell git diff --quiet HEAD 2>/dev/null || echo -mod)

.PHONY: build test instalar vetores

# CGO_ENABLED=0: binário estático, roda em qualquer Linux (glibc ou musl/Alpine)
build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w -X github.com/yurivenancio30/llm-dlp/internal/versao.Commit=$(COMMIT)" -o $(BIN) ./cmd/llm-dlp

test:
	$(GO) vet ./...
	$(GO) test -count=1 ./...

# instala o binário em ~/.local/bin (não mexe no Claude Code; para isso: llm-dlp instalar-claude)
instalar: build
	install -D -m 0755 $(BIN) $(HOME)/.local/bin/llm-dlp
	@echo "instalado em ~/.local/bin/llm-dlp"
