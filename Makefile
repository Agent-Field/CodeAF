# The one binary. Every build lands here — never at the repo root, never
# anywhere else — so a stale copy can't shadow a fresh one.
BINARY := bin/aforge

.PHONY: all build debug test test-swepro vet check clean

# The vendored swe-pro engine (internal/swepro) is held bug-for-bug at its
# upstream commit, and that includes its test suite: on macOS a dozen of its
# tests fail upstream too, on /var-vs-/private/var, a case-insensitive
# filesystem, and JS float-rounding parity. Those are upstream's verdicts to
# change, not ours, so they are not aforge's end-of-change ritual. This is the
# same line the repo already draws around its other vendored tree,
# internal/tui/charmbubbles, which sits outside ./... by being its own module.
# The exclusion is only this narrow: `go build ./...` and `go vet ./...` still
# cover internal/swepro, both are green, and the tests that cover the
# `// aforge-embed:` patches themselves are named TestAforgeEmbed* so `test`
# can run exactly them out of the vendored package.
AFORGE_PKGS = $(shell go list ./... | grep -v '/internal/swepro/')

all: build

# The symbol table and DWARF are a third of the shipped binary and nothing at
# runtime reads them. Stripping costs symbolized panic traces, which is exactly
# what `debug` keeps — build that when a stack trace is what you need.
build:
	go build -ldflags="-s -w" -o $(BINARY) ./cmd/aforge

debug:
	go build -o $(BINARY) ./cmd/aforge

test:
	go test $(AFORGE_PKGS)
	go test -run AforgeEmbed ./internal/swepro/codeaf

# What a re-vendor runs, and the only place the engine's own suite belongs.
# Compare its output against the same `go test ./...` in a clean swe-pro-go
# clone: equal failure sets mean the embedding changed nothing.
test-swepro:
	go test ./internal/swepro/...

vet:
	go vet ./...

# The end-of-change ritual in one word: prove it, then ship the binary.
check: vet test build

clean:
	rm -rf bin
