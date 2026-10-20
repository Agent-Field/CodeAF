//go:build linux || darwin

package teams

import (
	"os"
	"strconv"
	"syscall"
)

// stampIdentity makes an atomic replacement visible even if its writer kept
// the old file's size and modification time.
func stampIdentity(info os.FileInfo) string {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return "." + strconv.FormatUint(uint64(st.Dev), 10) + "." + strconv.FormatUint(uint64(st.Ino), 10)
	}
	return ""
}
