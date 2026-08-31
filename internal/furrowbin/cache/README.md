# The staged furrow, and why this folder is nearly empty

`make build` fetches the pinned furrow release for the machine it is building
for, verifies it against the sha256 in `../pin.json`, gzips it, and writes it
here as `furrow-<goos>-<goarch>.gz`. That file is gitignored and never
committed: six megabytes of binary in git is six megabytes in every clone
forever, and the pin beside it is the auditable thing.

This README is here so the folder exists in a fresh clone. `//go:embed cache`
needs at least one file it will actually take, so a plain `go build ./...`
before any fetch compiles against a folder holding only this page — an aforge
that carries no furrow, which `Embedded` reports as false and the seam in
`internal/furrow` treats as "look on PATH instead". `make build` never ships
that: the fetch runs first and fails loudly rather than quietly producing one.
