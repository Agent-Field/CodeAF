package sqliteuri

import (
	"net/url"
	"path/filepath"
	"strings"
)

// File builds a SQLite file URI from an absolute filesystem path.
func File(path string) url.URL {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return url.URL{Scheme: "file", Path: path}
}
