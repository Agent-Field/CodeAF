//go:build !windows

package fsreplace

// Retry leaves Unix operations byte-for-byte unchanged (I0/K3).
func Retry(operation func() error) error { return operation() }
