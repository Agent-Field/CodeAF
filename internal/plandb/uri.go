package plandb

import "github.com/Agent-Field/codeaf/internal/sqlitefile"

// StoreURI keeps plan readers and writers at the same literal filesystem path.
// Filename characters are escaped separately from the caller's query on every
// platform, and relative names stay relative to the working directory.
func StoreURI(path, query string) string { return sqlitefile.URI(path, query) }
