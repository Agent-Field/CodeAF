//go:build windows

package update

import (
	"errors"
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

// Relaunch hands this console to the build a launch just installed, before
// anything has been drawn. WINDOWS HAS NO EXEC, so the new build runs as a
// child on this console and THIS PROCESS WAITS FOR IT AND EXITS WITH ITS
// STATUS: a parent that returned at once would give the prompt back to the
// shell while the chat was still drawing over it. Like an exec it does not
// return once the replacement has started; it returns only when the
// replacement could not start.
func Relaunch(plan Plan) error {
	command := exec.Command(plan.Path, plan.Args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return err
	}
	err := command.Wait()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.ExitCode())
	}
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
	return nil
}
