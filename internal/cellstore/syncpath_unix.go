//go:build !windows

package cellstore

import "os"

// syncPath commits file data and directory entries before a boundary returns.
func syncPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
