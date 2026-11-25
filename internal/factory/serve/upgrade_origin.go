package serve

import (
	"net/http"
	"net/url"
	"strings"
)

// crossSiteUpgradeWords is what a page on another site reads when it tries to
// open a connection that upgrades the request (the hosted chat's wire).
const crossSiteUpgradeWords = "This page is from another site, so it cannot open a chat connection here."

// crossSiteUpgrade says the request upgrades the connection (a WebSocket) and
// carries an Origin that is not this server's own.
//
// A BROWSER IS THE ONLY CALLER THAT SENDS AN ORIGIN, AND THE ONLY ONE THAT CAN
// BE MADE TO CARRY A PERSON'S SIGN-IN COOKIE ONTO A CONNECTION THE PERSON DID
// NOT OPEN. A WebSocket is exempt from the same-origin policy: a hostile page
// can open one to this server and read everything it says, with the cookie
// attached when the browser sends it. The cookie is SameSite=Strict, which
// holds it back from another site and not from a page on a sibling name of
// the same site, so the server refuses on its own. The command line, the
// desktop app's engine and every other program that is not a browser send no
// Origin, so a request without one is never refused here.
//
// The server's own origin is the host the request was sent to (what the
// browser used to reach it, and so the host of any page the server serves) or
// the host of the server's public address, which a proxy that does not pass
// the Host header through still names. The scheme is not compared: a proxy
// that does not say it served HTTPS would otherwise refuse the server's own
// page, and the cookie's Secure flag already keeps it off a plain connection.
// A name that points at this server (DNS rebinding) would pass the host test,
// and holds no cookie for it, because a cookie belongs to the name that set it.
func (h *Handler) crossSiteUpgrade(r *http.Request) bool {
	if r.Header.Get("Upgrade") == "" {
		return false
	}
	origins := r.Header.Values("Origin")
	if len(origins) == 0 {
		return false
	}
	if len(origins) > 1 {
		return true
	}
	origin, err := url.Parse(origins[0])
	if err != nil || origin.Host == "" || origin.Scheme != "http" && origin.Scheme != "https" || origin.Path != "" || origin.User != nil {
		// "null" (a sandboxed frame or a local file) and anything that is not a
		// web origin name no server of ours.
		return true
	}
	from := originHost(origin.Scheme, origin.Host)
	if from == originHost(origin.Scheme, r.Host) {
		return false
	}
	if public, err := url.Parse(h.config.Address); err == nil && public.Host != "" && from == originHost(public.Scheme, public.Host) {
		return false
	}
	return true
}

// originHost is host in lower case without the port its scheme implies, so
// `https://org.example.com` and a Host of `org.example.com:443` are one name.
func originHost(scheme, host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	switch {
	case scheme == "https" && strings.HasSuffix(host, ":443"):
		return strings.TrimSuffix(host, ":443")
	case scheme == "http" && strings.HasSuffix(host, ":80"):
		return strings.TrimSuffix(host, ":80")
	}
	return host
}
