//go:build e2e

package e2e

// A NETWORK THAT CAN BE CUT FOR ONE PROCESS AND NO OTHER.
//
// The chat under test is started with this proxy as its HTTPS proxy, so every
// byte it sends to the relay goes through here, and cutting is one switch that
// refuses new tunnels and drops the open ones. No firewall rule, no change to a
// neighbour's network: the cut reaches exactly the process that was told to
// use the proxy. The model's endpoint is on loopback, which Go never proxies,
// so the scripted model stays reachable through a cut.

import (
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
)

type cutProxy struct {
	ln   net.Listener
	mu   sync.Mutex
	cut  bool
	open map[net.Conn]struct{}
}

func newCutProxy(t *testing.T) *cutProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &cutProxy{ln: ln, open: map[net.Conn]struct{}{}}
	srv := &http.Server{Handler: http.HandlerFunc(p.serve)}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close(); p.Restore() })
	return p
}

func (p *cutProxy) url() string { return "http://" + p.ln.Addr().String() }

// Cut refuses new tunnels and drops every open one.
func (p *cutProxy) Cut() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cut = true
	for c := range p.open {
		_ = c.Close()
	}
}

// Restore lets tunnels through again.
func (p *cutProxy) Restore() {
	p.mu.Lock()
	p.cut = false
	p.mu.Unlock()
}

// track keeps c so a cut can drop it, and answers false when the network is cut.
func (p *cutProxy) track(c net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cut {
		return false
	}
	p.open[c] = struct{}{}
	return true
}

func (p *cutProxy) forget(c net.Conn) {
	p.mu.Lock()
	delete(p.open, c)
	p.mu.Unlock()
	_ = c.Close()
}

// serve is a CONNECT tunnel: the only thing an https client asks of a proxy.
func (p *cutProxy) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		http.Error(w, "connect only", http.StatusMethodNotAllowed)
		return
	}
	far, err := net.Dial("tcp", r.Host)
	if err != nil || !p.track(far) {
		if far != nil {
			_ = far.Close()
		}
		http.Error(w, "network cut", http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		p.forget(far)
		return
	}
	near, _, err := hj.Hijack()
	if err != nil || !p.track(near) {
		p.forget(far)
		return
	}
	_, _ = near.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	go func() { _, _ = io.Copy(far, near); p.forget(far); p.forget(near) }()
	go func() { _, _ = io.Copy(near, far); p.forget(far); p.forget(near) }()
}
