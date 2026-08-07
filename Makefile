BINARY := bin/aforge

.PHONY: build debug test vet clean

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

clean:
	rm -rf bin
