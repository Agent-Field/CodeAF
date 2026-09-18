//go:build !windows && !linux

package processgroup

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// readProcessStart asks ps for a process's start time on the Unix targets that
// have no /proc. The answer is a wall-clock second — coarser than Linux's tick
// count, but the same identity the service re-adoption path has always trusted.
func readProcessStart(pid int) (uint64, bool) {
	command := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid))
	// ps renders lstart in the caller's locale, which reorders month and day.
	// Asking for C makes the one format we depend on the one we get.
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "LC_TIME=C")
	output, err := command.Output()
	if err != nil {
		return 0, false
	}
	raw := strings.Join(strings.Fields(string(output)), " ")
	for _, layout := range []string{
		"Mon Jan 2 15:04:05 2006", "Mon Jan _2 15:04:05 2006",
		"Mon 2 Jan 15:04:05 2006", "Jan 2 15:04:05 2006", "2 Jan 15:04:05 2006",
	} {
		if parsed, parseErr := time.ParseInLocation(layout, raw, time.Local); parseErr == nil {
			return uint64(parsed.Unix()), true
		}
	}
	return 0, false
}
