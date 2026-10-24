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

// FromPath resolves a filesystem path before encoding it as a SQLite URI.
// Unlike File, it also accepts relative paths without changing their meaning.
func FromPath(path string) (url.URL, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return url.URL{}, err
	}
	return File(absolute), nil
}
