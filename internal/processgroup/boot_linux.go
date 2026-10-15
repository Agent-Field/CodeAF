//go:build linux

package processgroup

import (
	"os"
	"strings"
)

// readBootID is the kernel's own name for this boot, which changes every time
// the machine starts.
func readBootID() string {
	raw, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
