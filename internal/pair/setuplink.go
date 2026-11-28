package pair

import (
	"net/url"
	"strings"
)

const (
	setupLinkPrefix  = "workaf://setup?"
	setupLinkVersion = "1"
)

// SetupLink builds the phone setup link `workaf://setup?v=1&sync=<percent-encoded address>`
// with any trailing slash trimmed from syncAddress (design 3.8b).
func SetupLink(syncAddress string) string {
	clean := strings.TrimRight(strings.TrimSpace(syncAddress), "/")
	return setupLinkPrefix + "v=" + setupLinkVersion + "&sync=" + url.QueryEscape(clean)
}

// ParseSetupLink parses a phone setup link according to design 3.8b: the outer
// URL must be workaf://setup?<query> with no user info, path or fragment; the
// query must carry the keys v (equal to "1") and sync once each and nothing
// else; and sync must be an absolute http or https URL with a host and no user
// info, query or fragment. The returned sync address has its trailing slash trimmed.
func ParseSetupLink(text string) (string, error) {
	raw := strings.TrimSpace(text)
	if !strings.HasPrefix(raw, setupLinkPrefix) || strings.Contains(raw, "#") {
		return "", ErrSetupLinkShape
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "workaf" || u.Host != "setup" || u.Path != "" ||
		u.User != nil || u.Fragment != "" || u.Opaque != "" || u.RawQuery == "" {
		return "", ErrSetupLinkShape
	}

	parts := strings.Split(u.RawQuery, "&")
	if len(parts) != 2 {
		return "", ErrSetupLinkShape
	}
	seen := make(map[string]string, 2)
	for _, part := range parts {
		if strings.Contains(part, ";") {
			return "", ErrSetupLinkShape
		}
		kRaw, vRaw, ok := strings.Cut(part, "=")
		if !ok || kRaw == "" {
			return "", ErrSetupLinkShape
		}
		k, err1 := url.QueryUnescape(kRaw)
		v, err2 := url.QueryUnescape(vRaw)
		if err1 != nil || err2 != nil {
			return "", ErrSetupLinkShape
		}
		if k != "v" && k != "sync" {
			return "", ErrSetupLinkShape
		}
		if _, dup := seen[k]; dup {
			return "", ErrSetupLinkShape
		}
		seen[k] = v
	}
	if seen["v"] != setupLinkVersion {
		return "", ErrSetupLinkShape
	}
	syncRaw := seen["sync"]
	if syncRaw == "" || strings.TrimSpace(syncRaw) != syncRaw ||
		strings.Contains(syncRaw, "?") || strings.Contains(syncRaw, "#") {
		return "", ErrSetupLinkShape
	}
	su, err := url.Parse(syncRaw)
	if err != nil || (su.Scheme != "http" && su.Scheme != "https") ||
		su.Host == "" || su.Hostname() == "" || su.User != nil ||
		su.RawQuery != "" || su.Fragment != "" || su.Opaque != "" {
		return "", ErrSetupLinkShape
	}
	// Check the authority slice directly so an empty userinfo like "https://@host"
	// that url.Parse might normalize is also refused.
	if _, rest, ok := strings.Cut(syncRaw, "://"); !ok || strings.Contains(authorityOf(rest), "@") {
		return "", ErrSetupLinkShape
	}
	return strings.TrimRight(syncRaw, "/"), nil
}

func authorityOf(afterScheme string) string {
	if i := strings.IndexByte(afterScheme, '/'); i >= 0 {
		return afterScheme[:i]
	}
	return afterScheme
}
