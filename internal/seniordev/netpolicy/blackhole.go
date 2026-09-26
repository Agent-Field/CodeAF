//go:build !windows

package netpolicy

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

const noProxyHosts = "localhost,127.0.0.1,::1"

// ShellProxyEnv returns the environment entries that gate proxy-honoring
// network clients (curl, wget, pip, npm, git-over-HTTPS) in shell children,
// or nil when the policy leaves bash open. Entries are meant to be appended
// AFTER os.Environ(): exec dedup is last-entry-wins, so they override any
// proxy the parent environment carries.
//
// The proxy address is a local black-hole listener that answers every
// connection with an explicit 403 naming the policy, so a blocked command
// fails with a legible, non-retryable error instead of a hang. If the
// listener cannot be (re)established the entries point at 127.0.0.1:1
// instead — connection refused, still fail-closed.
func ShellProxyEnv(p Policy) []string {
	if !p.Restricted() {
		return nil
	}
	address, err := BlackholeAddr()
	if err != nil {
		address = "127.0.0.1:1"
	}
	proxy := "http://" + address
	return []string{
		"HTTP_PROXY=" + proxy,
		"HTTPS_PROXY=" + proxy,
		"http_proxy=" + proxy,
		"https_proxy=" + proxy,
		"NO_PROXY=" + noProxyHosts,
		"no_proxy=" + noProxyHosts,
	}
}

var blackhole struct {
	mu       sync.Mutex
	address  string
	listener net.Listener
}

// BlackholeAddr returns the address of the per-process black-hole listener,
// starting or restarting it as needed. Callers invoke this once per shell
// spawn, so a listener that died costs at most one command of ECONNREFUSED
// (still fail-closed) before the next spawn restores the legible 403.
func BlackholeAddr() (string, error) {
	blackhole.mu.Lock()
	defer blackhole.mu.Unlock()
	if blackhole.address != "" && blackholeAlive(blackhole.address) {
		return blackhole.address, nil
	}
	// Close a listener that failed its health check before replacing it, so
	// a spurious dial timeout cannot leak sockets and serving goroutines.
	if blackhole.listener != nil {
		_ = blackhole.listener.Close()
		blackhole.listener = nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		blackhole.address = ""
		return "", err
	}
	blackhole.address = listener.Addr().String()
	blackhole.listener = listener
	go serveBlackhole(listener)
	return blackhole.address, nil
}

func blackholeAlive(address string) bool {
	connection, err := net.DialTimeout("tcp", address, 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func serveBlackhole(listener net.Listener) {
	body := "[network-policy] network access is restricted for this run (" + EnvMode + "). " +
		"This proxy rejects all traffic. Do not retry, and do not attempt the same " +
		"access through other commands - work from local repository content instead.\n"
	response := fmt.Sprintf(
		"HTTP/1.1 403 Forbidden\r\nContent-Type: text/plain\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		len(body), body,
	)
	// Closing the listener on exit is load-bearing: the dial-based health
	// check in blackholeAlive succeeds against any open listen socket (the
	// kernel completes handshakes from the backlog even with no accept loop),
	// so a listener abandoned open would pass health checks forever while
	// hanging every proxied client instead of serving the fail-fast 403.
	defer listener.Close()
	backoff := 5 * time.Millisecond
	for {
		connection, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// Transient pressure (EMFILE and friends): back off and keep
			// serving rather than dying under exactly the load that most
			// needs the legible refusal. Mirrors net/http Server.Serve.
			time.Sleep(backoff)
			if backoff < time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 5 * time.Millisecond
		go func(c net.Conn) {
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(2 * time.Second))
			// Absorb the request (or CONNECT) line so clients that wait to
			// finish writing before reading do not see a send error.
			buffer := make([]byte, 1024)
			_, _ = c.Read(buffer)
			_, _ = c.Write([]byte(response))
		}(connection)
	}
}
