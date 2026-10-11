package serve

import (
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
)

func (h *Handler) allowed(r *http.Request) bool {
	// Only the socket peer can grant loopback access. Forwarded headers and
	// hostnames come from callers and must never open the floor.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return true
		}
	}
	if h.config.AccessToken == "" {
		return false
	}
	token := bearer(r)
	if token == "" {
		return false
	}
	// Fixed-size hashes keep token length out of the comparison's timing.
	want, got := sha256.Sum256([]byte(h.config.AccessToken)), sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}
