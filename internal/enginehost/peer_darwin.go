//go:build darwin

package enginehost

// peer_darwin.go answers "which process is on the other end of this socket".
//
// IT IS THE KERNEL'S ANSWER AND NOT A FILE ANYBODY WROTE, which is the whole
// reason it is worth three small files. The host it has to name is, by
// definition, one that cannot be asked anything — a build from before the
// version exchange existed, still holding the socket after the binary under it
// was replaced. Anything that host would have had to write down for us (a pid
// file, a version file) is exactly what an older build never wrote.

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
	var pid int
	var asked error
	if err := raw.Control(func(fd uintptr) {
		pid, asked = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	}); err != nil {
		return 0, err
	}
	if asked != nil {
		return 0, asked
	}
	if pid <= 0 {
		return 0, errors.New("the other end of that socket has no process")
	}
	return pid, nil
}
