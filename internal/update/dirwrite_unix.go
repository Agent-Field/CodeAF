//go:build !windows

package update

import (
	"os"

	"golang.org/x/sys/unix"
)

// dirWritable reports whether this account may create files in dir, which is
// the same permission an in-place replacement needs.
func dirWritable(dir string) bool {
	// A DIRECTORIES THAT DOES NOT EXIST IS NOT A PERMISSION VERDICT: the install
	// itself will fail with its own error, and calling it a refusal would name
	// the wrong reason.
	if _, err := os.Stat(dir); err != nil {
		return true
	}
	return unix.Access(dir, unix.W_OK) == nil
}
