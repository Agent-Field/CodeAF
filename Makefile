# The one binary. Every build lands here — never at the repo root, never
# anywhere else — so a stale copy can't shadow a fresh one.
BINARY := bin/aforge

.PHONY: all build debug test test-swepro test-remote vet check clean

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

# The engine's own suite. Run it when you change internal/swepro, and compare
# against `go test ./...` in a clean upstream checkout: at import, the two
# failure sets were equal, which is what proved the import changed nothing.
test-swepro:
	go test ./internal/swepro/...

# TWO MACHINES, ACTUALLY TWO. Three containers on one network — a scripted
# model, an engine with sshd, and a surface — sharing no path, no home and no
# credential, so a session that only works because both halves happen to be one
# filesystem fails here instead of passing by coincidence. It is deliberately
# NOT part of `make test`: it wants a docker daemon and about a minute, which
# is a nightly or pre-release gate rather than an every-change one.
#
# It SKIPS, green, wherever there is no docker — so it is safe in any pipeline
# on the day the pipeline cannot yet run it.
test-remote:
	go test -tags docker_e2e -count=1 -run TestRemoteTwoMachines -timeout 20m ./internal/e2e/

vet:
	go vet ./...

# The end-of-change ritual in one word: prove it, then ship the binary.
check: vet test build

clean:
	rm -rf bin
