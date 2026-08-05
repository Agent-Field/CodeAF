BINARY := bin/aforge

.PHONY: build test vet clean

build:
	go build -o $(BINARY) ./cmd/aforge

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -rf bin
