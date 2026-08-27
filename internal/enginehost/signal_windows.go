//go:build windows

package enginehost

// signal_windows.go is unreachable and is here so the tree compiles: Windows
// has no unix socket for a host to listen on and no peer credentials to name
// one with (peer_other.go refuses first), so nothing ever gets this far.

import "errors"

func signalHost(int) error {
	return errors.New("this machine cannot stop a host it did not start")
}
