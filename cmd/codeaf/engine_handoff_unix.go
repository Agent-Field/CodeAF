//go:build !windows

package main

import "syscall"

// Exec retains the bridge's arguments and all three standard streams.
func execEngineHolder(binary string, args, environment []string) error {
	return syscall.Exec(binary, args, environment)
}
