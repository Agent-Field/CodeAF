package factory

import (
	"regexp"
	"strings"
)

// The public export boundary must not carry workstation identities. Execution
// receipts stay exact and private; known factory checkouts become repo-relative.
// Both patterns start only where a path starts (group 1 keeps that boundary: a
// non-path character, an option such as -I or a file:// scheme), so a repository
// directory such as ./internal/users or src/home stays intact. A home
// name may contain spaces only when a separator follows it, and never starts like
// a route parameter, so prose such as "/users/:id" or "the /users/ endpoint" stays.
const pathStart = `(^|[^\w.~/\\-]|(?:^|\s)-{1,2}[A-Za-z][\w-]*|file://)`

var factoryCheckout = regexp.MustCompile(`(?i)` + pathStart + `(?:[A-Z]:[/\\]|/(?:Users|home|root|private|tmp|var|opt|srv|workspace|workspaces)/)[^\n\r\x60"';&|<>]*?[/\\]factory[/\\]work[/\\][^/\\\s\x60"';&|<>]+`)

const (
	homeNameStart = `[^/\\\s:{}\[\]<>()*$\x60"';&|]`
	homeNameRest  = `[^/\\\s\x60"';&|<>]`
	homeName      = homeNameStart + homeNameRest + `*`
)

var localHome = regexp.MustCompile(`(?i)` + pathStart + `((?:[A-Z]:)?[/\\](?:Users|home)[/\\](?:` +
	homeName + `(?:[ \t]+` + homeNameRest + `+)*([/\\])|` + homeName + `)|/root\b)`)

var publicURL = regexp.MustCompile(`https?://[^\s<>"'\x60]+`)

func PublicText(s string) string {
	clean := func(part string) string {
		part = factoryCheckout.ReplaceAllString(part, "${1}.")
		part = localHome.ReplaceAllString(part, "${1}[local home]${3}")
		return strings.ReplaceAll(part, "\x00", "")
	}
	var out strings.Builder
	start := 0
	for _, span := range publicURL.FindAllStringIndex(s, -1) {
		out.WriteString(clean(s[start:span[0]]))
		out.WriteString(s[span[0]:span[1]])
		start = span[1]
	}
	out.WriteString(clean(s[start:]))
	return out.String()
}
