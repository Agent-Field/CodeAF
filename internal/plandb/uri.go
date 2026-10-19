package plandb

import (
	"path/filepath"
	"runtime"

	"github.com/Agent-Field/codeaf/internal/sqlitefile"
)

// StoreURI keeps every plan reader at the writer's existing address. SQLite
// interprets escapes and fragments in this raw spelling, so escaping a reader's
// filename would send it to a different file for paths containing % or #.
// THE WRITER'S URI BYTES ARE THE COMPATIBILITY CONTRACT, including its query.
//
// WINDOWS IS THE ONE PLACE THE CONTRACT DOES NOT BIND. There the raw spelling
// could never open a store whose path held a percent escape or a #: SQLite
// decoded or cut the name to a folder that did not exist, and the open failed
// with "unable to open database file" (measured natively on 2026-10-19). No
// store was ever written at such an address, so Windows uses the escaped URI
// every other store uses, and for every ordinary path that names the same file
// the raw spelling did.
func StoreURI(path, query string) string { return storeURI(runtime.GOOS, path, query) }

func storeURI(goos, path, query string) string {
	if goos == "windows" {
		return sqlitefile.URI(path, query)
	}
	uri := "file:" + filepath.ToSlash(path)
	if query != "" {
		uri += "?" + query
	}
	return uri
}
