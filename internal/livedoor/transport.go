package livedoor

import (
	"context"
	"fmt"
	"net"
)

// maxUnixSocketPath is the 103-byte limit before a path overflows sockaddr_un's
// 104-byte sun_path buffer (including NUL) on BSD and macOS systems.
const maxUnixSocketPath = 103

// Transport abstracts local holder discovery sockets so unix domain sockets
// and test pipes sit behind one seam.
type Transport interface {
	Listen(addr string) (net.Listener, error)
	Dial(ctx context.Context, addr string) (net.Conn, error)
}

func checkUnixSocketPath(addr string) error {
	if len(addr) > maxUnixSocketPath {
		return fmt.Errorf("livedoor: unix socket path %q is %d bytes (limit %d bytes for sun_path)", addr, len(addr), maxUnixSocketPath)
	}
	return nil
}
