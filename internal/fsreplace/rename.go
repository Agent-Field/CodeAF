// Package fsreplace keeps replacement atomic while allowing Windows editors and
// antivirus scanners a bounded time to release a destination (K3).
package fsreplace

import "os"

// Rename preserves os.Rename's semantics, retrying only transient Windows locks.
// It never removes the destination to make a replacement succeed.
func Rename(old, new string) error {
	return Retry(func() error { return os.Rename(old, new) })
}
