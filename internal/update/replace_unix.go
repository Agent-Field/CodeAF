//go:build !windows

package update

import "os"

func replaceExecutable(temporary, target string) error {
	return os.Rename(temporary, target)
}

// CleanupOld removes nothing on systems where the running executable can be
// replaced directly.
func CleanupOld(string) {}
