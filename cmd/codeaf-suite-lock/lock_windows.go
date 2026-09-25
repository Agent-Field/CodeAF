//go:build windows

package main

import "os"

func startHolder(_, _ string, _ *os.File, _ int) (*os.Process, error) {
	return nil, errNoHolder
}

func holdLock(_ int, _ string) int { return 2 }

func procStartToken(_ int) string { return "" }

// commandLine is empty on Windows, where no old checkout ever took the
// directory lock: one-suite.sh is a bash script and names it only there.
func commandLine(_ int) string { return "" }

// pidVisibleHere is a no-op on Windows: the lock is not handed to a holder or a
// child, so the recorded holder is the only holder.
func pidVisibleHere(_ int) bool { return true }
