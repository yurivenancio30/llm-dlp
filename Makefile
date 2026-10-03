GO ?= go
BIN := bin/llm-dlp

.PHONY: build test instalar vetores

# CGO_ENABLED=0: binário estático, roda em qualquer Linux (glibc ou musl/Alpine)
build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/llm-dlp

test:
	$(GO) vet ./...
	$(GO) test -count=1 ./...

# instala o binário em ~/.local/bin (não mexe no Claude Code; para isso: llm-dlp instalar-claude)
instalar: build
	install -D -m 0755 $(BIN) $(HOME)/.local/bin/llm-dlp
	@echo "instalado em ~/.local/bin/llm-dlp"
