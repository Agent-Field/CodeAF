// Package sqlitefile gives every engine store the same native-path URI spelling
// without importing any store or the SQLite driver itself.
//
// On 2026-10-19 the native Windows store tests failed every open with "SQL logic
// error: out of memory (1)": url.URL had rendered C:\Users\… as file:C:%5CUsers….
// SQLite needs forward slashes and a leading slash before a drive. UNC names
// stay in the path as file:////server/share/… because this driver's SQLite build
// rejects non-local URI authorities. UNIX URI BYTES ARE UNTOUCHED.
package sqlitefile

import (
	"net/url"
	"runtime"
	"strings"
)

// URI escapes the filename separately from the query, so a filename's ?, # or
// percent sign cannot become a query, fragment or escape in the database name.
// The caller owns the query's encoding and order; neither is rewritten here.
func URI(path, query string) string { return uri(runtime.GOOS, path, query) }

func uri(goos, path, query string) string {
	if goos == "windows" {
		path = strings.ReplaceAll(path, `\`, "/")
		if len(path) >= 3 && path[1] == ':' && path[2] == '/' {
			path = "/" + path
		}
	}
	return (&url.URL{Scheme: "file", Path: path, RawQuery: query}).String()
}
