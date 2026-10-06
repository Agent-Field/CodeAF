//go:build windows

package session

import "os"

// contextualOpenRead is the platform fallback. Windows has no portable
// non-blocking open for this case; the regular-file check in
// [contextualReadFile] still refuses a non-file before any bytes are read.
func contextualOpenRead(path string) (*os.File, error) {
	return os.Open(path)
}
