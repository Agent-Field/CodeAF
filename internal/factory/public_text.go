package factory

import (
	"regexp"
	"strings"
)

// The public export boundary must not carry workstation identities. Execution
// receipts stay exact and private; known factory checkouts become repo-relative.
var factoryCheckout = regexp.MustCompile(`(?i)(?:[A-Z]:[/\\]|/(?:Users|home|root|private|tmp|var|opt|srv|workspace|workspaces)/)[^\n\r\x60"';&|<>]*?[/\\]factory[/\\]work[/\\][^/\\\s\x60"';&|<>]+`)
var localHome = regexp.MustCompile(`(?i)(?:[A-Z]:)?[/\\](?:Users|home)[/\\][^/\\\r\n\x60"';&|<>]+|/root\b`)

var publicURL = regexp.MustCompile(`https?://[^\s<>"'\x60]+`)

func PublicText(s string) string {
	clean := func(part string) string {
		part = factoryCheckout.ReplaceAllString(part, ".")
		part = localHome.ReplaceAllStringFunc(part, func(home string) string {
			trimmed := strings.TrimRight(home, " \t")
			return "[local home]" + home[len(trimmed):]
		})
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
