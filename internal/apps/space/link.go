package space

import (
	"net"
	"net/url"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

func buildInvite(publicURL, id, secret string) string {
	return "codeaf://join/" + urlHost(publicURL) + "/" + id + "." + secret
}

func urlHost(publicURL string) string {
	u, err := url.Parse(publicURL)
	if err != nil || u.Host == "" {
		return publicURL
	}
	return u.Host
}

func parseInvite(link string) (host, id, secret string, err error) {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "codeaf" || u.Host != "join" {
		return "", "", "", invalid("use an invite link")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 {
		return "", "", "", invalid("use an invite link")
	}
	id, secret, ok := strings.Cut(parts[1], ".")
	if !ok || parts[0] == "" || id == "" || secret == "" {
		return "", "", "", invalid("use an invite link")
	}
	return parts[0], id, secret, nil
}

// originFor is how a link becomes a URL. Loopback is http because a local
// workspace has no TLS; every other host is https, terminated in front.
func originFor(host string) string {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	scheme := "https"
	if name == "localhost" || name == "127.0.0.1" || name == "::1" {
		scheme = "http"
	}
	return scheme + "://" + host
}

func bridgeURL(origin string) string {
	u, err := url.Parse(origin)
	if err != nil {
		return origin + bridge.Path
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = bridge.Path
	u.RawQuery, u.Fragment = "", ""
	return u.String()
}
