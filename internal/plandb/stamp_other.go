//go:build !unix

package plandb

import "os"

// statStamp is the size and modification time of path, or the zero stamp for a
// file that is not there, which is a stamp too.
func statStamp(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{size: info.Size(), mtime: info.ModTime().UnixNano()}
}
