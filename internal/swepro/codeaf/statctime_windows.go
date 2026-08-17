//go:build windows

package codeaf

import "os"

// Windows' os.FileInfo does not expose the stable device/inode/ctime tuple
// used by the Unix fast path. Size and ModTime remain in every projection mark,
// so returning false keeps reconciliation correct and merely less specific.
func projectionFileIdentity(os.FileInfo) (uint64, uint64, int64, bool) {
	return 0, 0, 0, false
}
