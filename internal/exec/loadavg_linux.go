package exec

import (
	"os"
	"strconv"
	"strings"
)

// /proc/loadavg leads with the one-minute average in plain decimal. Reading it
// needs no dependency and no syscall wrapper.
func loadAverageOne() (float64, bool) {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, false
	}
	one, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || one < 0 {
		return 0, false
	}
	return one, true
}
