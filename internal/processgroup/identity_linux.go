//go:build linux

package processgroup

import (
	"os"
	"strconv"
	"strings"
)

// readProcessStart reads a process's start time out of /proc/<pid>/stat, field
// 22: the number of clock ticks between boot and the process's creation. It is
// unique to one process within one boot, and it costs one small file read —
// which is why it, and not a forked ps, is what a teardown checks.
func readProcessStart(pid int) (uint64, bool) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	// comm (field 2) is parenthesised and may itself contain spaces and
	// parentheses, so the fields are counted from after its LAST ')': the state
	// (field 3) is then index 0 and the start time (field 22) is index 19.
	text := string(raw)
	close := strings.LastIndexByte(text, ')')
	if close < 0 {
		return 0, false
	}
	fields := strings.Fields(text[close+1:])
	if len(fields) <= 19 {
		return 0, false
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, false
	}
	return start, true
}
