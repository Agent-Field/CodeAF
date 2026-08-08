# The one binary. Every build lands here — never at the repo root, never
# anywhere else — so a stale copy can't shadow a fresh one.
BINARY := bin/aforge

.PHONY: all build debug test vet check clean

all: build

# The symbol table and DWARF are a third of the shipped binary and nothing at
# runtime reads them. Stripping costs symbolized panic traces, which is exactly
# what `debug` keeps — build that when a stack trace is what you need.
build:
	go build -ldflags="-s -w" -o $(BINARY) ./cmd/aforge

debug:
	go build -o $(BINARY) ./cmd/aforge

test:
	go test ./...

vet:
	go vet ./...

# The end-of-change ritual in one word: prove it, then ship the binary.
check: vet test build

clean:
	rm -rf bin
