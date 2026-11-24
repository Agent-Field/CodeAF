//go:build windows

package livedoor

import (
	"context"
	"net"
)

// unixTransport uses AF_UNIX on Windows the same way as on Unix platforms.
// Its behaviour on Windows remains unproven until a manual check on a Windows
// host, and a loopback-TCP-plus-token transport can replace it behind the
// Transport interface if AF_UNIX proves unreliable there.
type unixTransport struct{}

// UnixTransport returns a Transport over AF_UNIX sockets on Windows. Its
// behaviour on Windows is unproven until a hand check, and a loopback-TCP
// plus token transport would replace it behind Transport if needed.
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
