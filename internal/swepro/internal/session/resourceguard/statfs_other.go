//go:build !linux

// Conservative fallback for src/session/resource-guard.ts:40-45.
package resourceguard

import "errors"

func defaultStatFS(string) (StatFSResult, error) {
	return StatFSResult{}, errors.New("statfs unavailable")
}
