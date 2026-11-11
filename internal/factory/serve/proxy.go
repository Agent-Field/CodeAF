package serve

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// hstsValue asks a browser to keep using HTTPS for this name for a year. It
// carries no includeSubDomains: the factory owns one name, never its siblings.
const hstsValue = "max-age=31536000"

// client is who is on the far end of a request and how they reached the
// factory, after the one decision about which headers to believe.
type client struct {
	// addr is the caller's address: the socket peer, or the first address a
	// trusted proxy forwarded. It is invalid when neither could be read.
	addr netip.Addr
	// secure says the caller's own connection was HTTPS, whether this process
	// terminated it or a trusted proxy did and said so.
	secure bool
	// forwarded says the request came through a proxy, trusted or not. An
	// untrusted forwarded request keeps its socket peer as addr but is never
	// treated as a caller on this machine.
	forwarded bool
	// viaTrusted says a trusted proxy named the caller, so addr is the
	// proxy's account rather than the socket's.
	viaTrusted bool
}

type clientKey struct{}

// forwardingHeaders are every header a proxy writes to say who it is
// forwarding for. Their presence alone marks a request as forwarded.
var forwardingHeaders = []string{"X-Forwarded-For", "X-Forwarded-Proto", "X-Real-Ip", "Forwarded"}

// resolveClient reads the socket peer, and believes X-Forwarded-For and
// X-Forwarded-Proto only when that peer is one of the host's trusted proxies.
//
// A HEADER CAN ONLY TAKE PRIVILEGE AWAY, NEVER GRANT IT. A request carrying
// forwarding headers from a peer that is not trusted is still marked
// forwarded, so a proxy on this machine that the host forgot to list cannot
// hand the open loopback floor to everyone on the internet.
func resolveClient(r *http.Request, trusted []netip.Prefix) client {
	c := client{secure: r.TLS != nil}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		if addr, err := netip.ParseAddr(host); err == nil {
			c.addr = addr.Unmap()
		}
	}
	for _, name := range forwardingHeaders {
		if r.Header.Get(name) != "" {
			c.forwarded = true
			break
		}
	}
	if !c.forwarded || !c.addr.IsValid() || !trustedPeer(c.addr, trusted) {
		return c
	}
	// A trusted proxy that names no readable caller leaves the caller unknown,
	// which keeps the peer's address for the record but is never local.
	if addr, ok := forwardedFor(r.Header.Values("X-Forwarded-For"), trusted); ok {
		c.addr, c.viaTrusted = addr, true
	}
	if proto := lastToken(r.Header.Values("X-Forwarded-Proto")); proto != "" {
		c.secure = strings.EqualFold(proto, "https")
	}
	return c
}

// forwardedFor walks X-Forwarded-For from the nearest hop outward and answers
// the first address that is not itself a trusted proxy, because everything
// before that hop could have been written by the caller.
func forwardedFor(values []string, trusted []netip.Prefix) (netip.Addr, bool) {
	var hops []string
	for _, v := range values {
		hops = append(hops, strings.Split(v, ",")...)
	}
	var last netip.Addr
	for i := len(hops) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return netip.Addr{}, false
		}
		last = addr.Unmap()
		if !trustedPeer(last, trusted) {
			return last, true
		}
	}
	return last, last.IsValid()
}

// lastToken is the value the nearest proxy appended to a comma list.
func lastToken(values []string) string {
	if len(values) == 0 {
		return ""
	}
	parts := strings.Split(values[len(values)-1], ",")
	return strings.TrimSpace(parts[len(parts)-1])
}

func trustedPeer(addr netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// withClient resolves the caller once per request, so the floor's loopback
// grant, the cookie's Secure flag and HSTS all read one answer.
func (h *Handler) withClient(r *http.Request) *http.Request {
	c := resolveClient(r, h.config.TrustedProxies)
	return r.WithContext(context.WithValue(r.Context(), clientKey{}, c))
}

// clientOf is the resolved caller, or the socket's own reading for a request
// that never passed through [Handler.ServeHTTP].
func clientOf(r *http.Request) client {
	if c, ok := r.Context().Value(clientKey{}).(client); ok {
		return c
	}
	return resolveClient(r, nil)
}

// ClientAddr is the caller's address after proxy trust, for any log the host
// keeps beside the handler. Empty means it could not be read.
func ClientAddr(r *http.Request) string {
	if addr := clientOf(r).addr; addr.IsValid() {
		return addr.String()
	}
	return ""
}

// onThisMachine says the caller is a process on this machine: a loopback
// peer that nobody forwarded, or a loopback caller a trusted proxy vouched for.
func (c client) onThisMachine() bool {
	return c.addr.IsValid() && c.addr.IsLoopback() && (!c.forwarded || c.viaTrusted)
}

// secureRequest says the caller reached the factory over HTTPS.
func secureRequest(r *http.Request) bool { return clientOf(r).secure }

// scheme is the word /v1/about reports for how this caller reached it.
func scheme(r *http.Request) string {
	if secureRequest(r) {
		return "https"
	}
	return "http"
}
