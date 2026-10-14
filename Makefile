GO ?= go
BIN := bin/llm-dlp
PACKAGE := github.com/yurivenancio30/llm-dlp
# the version is the one in internal/version/version.go (see docs/versioning.md)
VERSION := $(shell sed -n 's/^const Version = "\(.*\)"$$/\1/p' internal/version/version.go)
# the commit goes inside the binary ("llm-dlp version"); "-mod": there were uncommitted changes
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)$(shell git diff --quiet HEAD 2>/dev/null || echo -mod)
LDFLAGS := -s -w -X $(PACKAGE)/internal/version.Commit=$(COMMIT)

.PHONY: build test install version dist release

# CGO_ENABLED=0: static binary, runs on any Linux (glibc or musl/Alpine)
build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/llm-dlp

test:
	$(GO) vet ./...
	$(GO) test -count=1 ./...

# installs the binary in ~/.local/bin and, if llm-dlp is running, puts the new one in place
# without taking it offline (does not touch Claude Code; for that: llm-dlp install)
install: build
	install -D -m 0755 $(BIN) $(HOME)/.local/bin/llm-dlp
	@echo "installed in ~/.local/bin/llm-dlp"
	@$(HOME)/.local/bin/llm-dlp reload

version:
	@echo $(VERSION) "($(COMMIT))"

# release rehearsal on your machine, without publishing: the .tar.gz archive of each platform and
# checksums.txt in dist/, built by GoReleaser (.goreleaser.yaml) as GitHub does
dist:
	@command -v goreleaser >/dev/null || { echo "make dist needs goreleaser: https://goreleaser.com/install/"; exit 1; }
	goreleaser release --snapshot --clean

# checks, tests and creates the tag of the version in version.go (sends nothing: see docs/versioning.md)
release:
	./scripts/release.sh
