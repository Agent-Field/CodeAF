//go:build darwin

package processgroup

import (
	"strconv"

	"golang.org/x/sys/unix"
)

// readBootID is the second the machine booted, which names one boot as well as
// any identifier would: two boots cannot start in the same second.
func readBootID() string {
	tv, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return ""
	}
	return strconv.FormatInt(int64(tv.Sec), 10)
}
