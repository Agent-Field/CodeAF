//go:build !linux && !darwin

package taskdoor

import (
	"errors"
	"os/exec"
)

func separateProcessGroup(*exec.Cmd) {}
func signalChildGroup(int, bool) error {
	return errors.New(UnsupportedComputerLine)
}
func childIdentity(int, string, string) (string, string, error) {
	return "", "", errors.New(UnsupportedComputerLine)
}

func findAttemptChild(string, string) (int, string, string, error) {
	return 0, "", "", errors.New(UnsupportedComputerLine)
}
