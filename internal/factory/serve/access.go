package serve

import (
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
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
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return false
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	// Fixed-size hashes keep token length out of the comparison's timing.
	want, got := sha256.Sum256([]byte(h.config.AccessToken)), sha256.Sum256([]byte(parts[1]))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}
