// Package fsreplace keeps replacement atomic while allowing Windows editors and
// antivirus scanners a bounded time to release a destination (K3).
package fsreplace

// Rename atomically replaces files, retrying only transient Windows locks.
// Readers opened with Open or OpenFile retain their old generation.
// It never removes the destination to make a replacement succeed.
func Rename(old, new string) error {
	return Retry(func() error { return rename(old, new) })
}
