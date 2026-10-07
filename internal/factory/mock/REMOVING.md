# Removing the factory mock

The mock floor is exactly four paths. Deleting them removes it whole:

- `internal/factory/mock/` (this package, its tests and this note)
- `cmd/codeaf/factorymock.go` and `cmd/codeaf/factorymock_stub.go`
  (the stub may instead stay as a one-line absent door returning
  `factory.Seam{}, false`)
- `cmd/factory-mock/` (the standalone hand-driven mock it was ported from)
- `internal/tui3/factory_mock_test.go` (the floor's verbs driven end to end
  over the mock; the surface itself never imports it)

Whoever calls `factoryMockSeam()` (`cmd/codeaf/factory.go`'s `factorySeam()`)
drops that call in the same change if the stub goes too.

## Turning it on

The shipped binary never carries the mock: `make build` is untagged and
compiles the stub. A dev build with the mock, on Spark:

```sh
go build -tags factorymock -o bin/codeaf-mock ./cmd/codeaf
CODEAF_FACTORY_MOCK=1 bin/codeaf-mock
```
