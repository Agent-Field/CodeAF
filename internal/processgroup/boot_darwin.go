//go:build darwin

package processgroup

import (
	"strconv"

	"golang.org/x/sys/unix"
)

// readBootID preserves the kernel's complete boot timestamp.
func readBootID() string {
	tv, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return ""
	}
	if tv.Sec <= 0 || tv.Usec < 0 || tv.Usec >= 1_000_000 {
		return ""
	}
	return strconv.FormatInt(int64(tv.Sec), 10) + ":" + strconv.FormatInt(int64(tv.Usec), 10)
}
