package index

import (
	_ "embed"
)

// seedJSON is the index this build carries: the first measured pool, shipped
// inside the binary so a machine that has never fetched one still has numbers
// to pick a seat against on its first run.
//
// IT IS A FALLBACK AND NOT A SOURCE. Whoever reads an index reads the cache
// first and this beside it ([Fallback]): a fresh signed document wins, and
// this answers only when there is none, or when the one on disk is older or
// unreadable. seed.json is the same bytes as a file under version control, so
// what the binary carries is diffed and reviewed like any other figure here.
//
//go:embed seed.json
var seedJSON []byte

// Seed answers with the embedded document's bytes, as a COPY: the index is
// read many times and by many goroutines, and a caller that changed the slice
// would change what every later [Seed] call hands back.
func Seed() []byte {
	return append([]byte(nil), seedJSON...)
}

// SeedIndex parses the embedded document — the index a machine with no cache
// starts from. The error is returned rather than panicking, though the seed is
// a file this build was shipped with and a test in this package parses it, so
// an error here is a bug and not a mode.
func SeedIndex() (*Index, error) {
	return Parse(seedJSON)
}
