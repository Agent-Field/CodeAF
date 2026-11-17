//go:build linux

package cell

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
)

// platformProcessStatus reads /proc, which also gives the start time that tells
// a recycled pid from the owner, and shows a zombie for what it is. Where /proc
// is not mounted at all (a minimal container) it falls back to signal 0.
func platformProcessStatus(pid int) (startToken string, alive bool) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err == nil {
		if idx := bytes.LastIndexByte(raw, ')'); idx >= 0 && idx+2 < len(raw) {
			fields := strings.Fields(string(raw[idx+2:]))
			if len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X" || fields[0] == "x") {
				return "", false
			}
			if len(fields) > 19 {
				return fields[19], true
			}
		}
		return "", true
	}
	if errors.Is(err, os.ErrNotExist) {
		if _, selfErr := os.Stat("/proc/self/stat"); selfErr == nil {
			return "", false
		}
	}
	return "", signalAlive(pid)
}
