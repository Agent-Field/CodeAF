//go:build !darwin && !linux

package enginehost

// peer_other.go is the honest nothing. A platform whose kernel will not name
// the process on the other end of a socket cannot end a host it is unable to
// ask, and saying so is the whole of what this file does — the caller turns
// that into a sentence rather than into a signal aimed at a guess.

import (
	"errors"
	"net"
)

func peerPID(net.Conn) (int, error) {
	return 0, errors.New("this machine cannot say which process holds that socket")
}
