//go:build !unix

package skills

import "os"

// openNonblocking uses the platform's ordinary open after the path type check;
// the opened descriptor is checked again by OpenRegular before any read.
func openNonblocking(path string) (*os.File, error) {
	return os.Open(path)
}
