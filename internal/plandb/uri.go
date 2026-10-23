package plandb

import "github.com/Agent-Field/codeaf/internal/sqliteuri"

// StoreURI uses the writer's literal filesystem address for every reader (I0).
// The newer plan-store fix already escapes percent and fragment characters on
// Unix too; merging Windows support must not reintroduce the earlier raw URI.
// An unresolvable relative path supplies no address to a read-only caller.
func StoreURI(path, query string) string {
	uri, err := sqliteuri.FromPath(path)
	if err != nil {
		return ""
	}
	uri.RawQuery = query
	return uri.String()
}
