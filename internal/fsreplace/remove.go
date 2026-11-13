package fsreplace

import "os"

// Remove preserves os.Remove's errors, retrying only transient Windows locks.
func Remove(path string) error { return Retry(func() error { return os.Remove(path) }) }

// RemoveAll retries transient Windows locks while retaining os.RemoveAll's
// missing-path and symlink semantics.
func RemoveAll(path string) error { return Retry(func() error { return os.RemoveAll(path) }) }
