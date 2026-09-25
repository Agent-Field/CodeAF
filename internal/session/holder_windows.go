//go:build windows

package session

import "errors"

// stopProcess is absent on a machine with no SIGTERM to send: the window is
// named, and the person closes it themselves.
func stopProcess(int) error {
	return errors.New("this machine cannot ask another window to stop — close it there")
}

func psField(int, string) (string, error) {
	return "", errors.New("no process table")
}
