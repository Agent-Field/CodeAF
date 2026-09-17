//go:build !windows

package update

import (
	"os"
	"syscall"
)

// Restart replaces this process after the terminal has been restored.
func Restart(plan Plan) error {
	argv := append([]string{plan.Path}, plan.Args...)
	return syscall.Exec(plan.Path, argv, os.Environ())
}
