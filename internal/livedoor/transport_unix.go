//go:build !windows

package livedoor

import (
	"context"
	"net"
)

type unixTransport struct{}

// UnixTransport returns a Transport over AF_UNIX domain sockets with a 103-byte
// sun_path length check.
func UnixTransport() Transport {
	return unixTransport{}
}

func (unixTransport) Listen(addr string) (net.Listener, error) {
	if err := checkUnixSocketPath(addr); err != nil {
		return nil, err
	}
	return net.Listen("unix", addr)
}

func (unixTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	if err := checkUnixSocketPath(addr); err != nil {
		return nil, err
	}
	var d net.Dialer
	return d.DialContext(ctx, "unix", addr)
}
