// Package ci holds the laws about the repository's own gate — the tests that
// read .github and the Makefile rather than the product, and refuse the shapes
// that let dev go red unseen on 2026-09-02 (#372). There is no code here to
// ship; the package exists so that those laws have a home `go test ./...` and
// scripts/laws.sh both reach.
package ci
