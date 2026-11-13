//go:build windows

package main

import "fmt"

func execEngineHolder(binary string, args, environment []string) error {
	return fmt.Errorf("automatic executable handoff is unavailable on this platform")
}
