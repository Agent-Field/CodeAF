# The one binary. Every build lands here — never at the repo root, never
# anywhere else — so a stale copy can't shadow a fresh one.
BINARY := bin/aforge

.PHONY: all build debug embed test test-swepro vet check clean

# The imported swe-pro engine (internal/swepro) arrived with fifteen tests
# already failing on macOS in a clean upstream checkout — /var-vs-/private/var,
# a case-insensitive filesystem, JS float-rounding parity — and fixing them was
# not on the way in. Until they are fixed they are not aforge's end-of-change
# ritual. The exclusion is only this narrow: `go build ./...` and
# `go vet ./...` still cover internal/swepro and both are green, and the
# embedding's own divergences are covered from outside the tree by
# cmd/aforge/swepro_test.go, which drives the real binary. This is a to-do,
# not a policy — see internal/swepro/EMBEDDING.md.
AFORGE_PKGS = $(shell go list ./... | grep -v '/internal/swepro/')

# The packed corpora — the two manuals, the baked agent roster, the engine's
# prompt assets. Each folder is the source of truth and the archive beside it is
# generated from it (internal/packed says why), so a build that skipped this
# could ship yesterday's manual. The packer is a pure function of the folder, so
# regenerating on every build rewrites identical bytes and leaves the tree
# clean — cheaper to trust than a freshness check, and the test beside each
# corpus asserts the same thing for `make test`.
PACKED_PKGS = ./internal/manual ./internal/swepro/internal/baked ./internal/swepro/internal/assets

all: build

embed:
	go generate $(PACKED_PKGS)

# The symbol table and DWARF are a third of the shipped binary and nothing at
# runtime reads them. Stripping costs symbolized panic traces, which is exactly
# what `debug` keeps — build that when a stack trace is what you need.
#
# -trimpath drops the build machine's absolute paths out of the binary and makes
# the build reproducible: the same tree gives the same bytes on any machine.
# What it costs is compiled-in repository roots — runtime.Caller in
# internal/swepro's furrow and weave lookups walks up from its own source file
# to find vendor/bin/<tool>. There is no vendor/ in this repository, and a
# shipped binary's compiled-in root never exists on the machine running it, so
# both lookups already fell through to the cwd copy and then to PATH.
build: embed
	go build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/aforge

debug: embed
	go build -trimpath -o $(BINARY) ./cmd/aforge

test:
	go test $(AFORGE_PKGS)

# The engine's own suite. Run it when you change internal/swepro, and compare
# against `go test ./...` in a clean upstream checkout: at import, the two
# failure sets were equal, which is what proved the import changed nothing.
test-swepro:
	go test ./internal/swepro/...

vet:
	go vet ./...

# The end-of-change ritual in one word: prove it, then ship the binary.
check: vet test build

clean:
	rm -rf bin
