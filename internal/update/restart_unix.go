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

// Relaunch hands this terminal to the build a launch just installed, before
// anything has been drawn. Here it is [Restart]: the process is replaced in
// place, so the shell that started codeaf waits on the new build as though it
// had started it. It returns only when the replacement could not start.
func Relaunch(plan Plan) error {
	return Restart(plan)
}
