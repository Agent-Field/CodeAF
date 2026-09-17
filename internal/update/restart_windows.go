//go:build windows

package update

import (
	"os"
	"os/exec"
)

// Restart starts the replacement process with this process's terminal streams.
func Restart(plan Plan) error {
	command := exec.Command(plan.Path, plan.Args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Start()
}
