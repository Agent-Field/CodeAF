//go:build windows

package session

import (
	"fmt"
	"os"
)

// wgStamp names a file's present state by size and modification instant, and
// "-" for a file that is not there.
func wgStamp(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "-"
	}
	return fmt.Sprintf("%d/%d", info.Size(), info.ModTime().UnixNano())
}
