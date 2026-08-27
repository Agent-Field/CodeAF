//go:build linux

package enginehost

// peer_linux.go is the same question as peer_darwin.go, asked the way Linux
// answers it: SO_PEERCRED hands back the pid, uid and gid of the process that
// connected, and only the pid is any of our business.

import (
	"errors"
	"net"

	"golang.org/x/sys/unix"
)

func peerPID(conn net.Conn) (int, error) {
	socket, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("that connection is not a unix socket")
	}
	raw, err := socket.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var asked error
	if err := raw.Control(func(fd uintptr) {
		cred, asked = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if asked != nil {
		return 0, asked
	}
	if cred == nil || cred.Pid <= 0 {
		return 0, errors.New("the other end of that socket has no process")
	}
	return int(cred.Pid), nil
}
