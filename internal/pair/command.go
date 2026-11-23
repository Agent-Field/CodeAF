package pair

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// Command builds every instruction to pair a new device through this service.
func Command(address, code string) string {
	command := "codeaf pair"
	if code != "" {
		command += " " + code
	}
	if address != "" && !syncwords.Hosted(address) {
		command += " --via " + address
	}
	return command
}

// CheckLinkService refuses before reading a request on another service. Tokens
// name no service and deliberately use the one selected on this computer.
func CheckLinkService(typed, address string) error {
	via := linkService(typed)
	if via == "" {
		return nil
	}
	if canonicalService(via) == canonicalService(address) {
		return nil
	}
	return fmt.Errorf("that link is waiting on %s, but this computer syncs through %s — on the new device run: %s", syncwords.Host(via), syncwords.Host(address), Command(address, ""))
}

// linkService is the service a pasted link waits on. LinkRelay leaves this
// build's own hosted service unnamed, because a hosted computer keeps its
// configured service; a computer on its own service must still see that a
// hosted link is waiting somewhere else, however the origin was spelled.
func linkService(typed string) string {
	u, err := url.Parse(strings.TrimSpace(typed))
	if err != nil {
		return ""
	}
	if u.Scheme == "codeaf" {
		if via := u.Query().Get("via"); via != "" {
			return via
		}
		return syncwords.HostedURL
	}
	if webRelay(u) && strings.HasPrefix(u.Path, "/p/") && LinkOrigin != "" &&
		canonicalService(u.Scheme+"://"+u.Host) == canonicalService(LinkOrigin) {
		return syncwords.HostedURL
	}
	return LinkRelay(typed)
}

// canonicalService normalizes URL spelling without erasing a service's path or protocol.
func canonicalService(address string) string {
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil || u.Host == "" {
		return strings.TrimRight(address, "/")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if u.Scheme == "https" && port == "443" || u.Scheme == "http" && port == "80" {
		port = ""
	}
	u.Host = host
	if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = strings.TrimRight(u.RawPath, "/")
	return u.String()
}
