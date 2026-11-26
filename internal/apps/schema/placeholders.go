package schema

import (
	"regexp"
	"strings"
)

// An unbound template marker is visible in the resulting draft. Markdown
// link text can have the same shape, so leave link labels alone.
var placeholder = regexp.MustCompile(`\[\s*[\pL][\pL\pN'’ _-]{0,79}\s*\]`)

func UnboundPlaceholder(text string) string {
	for _, index := range placeholder.FindAllStringIndex(text, -1) {
		after := strings.TrimLeft(text[index[1]:], " \t")
		if !strings.HasPrefix(after, "(") && !strings.HasPrefix(after, ":") && !strings.HasPrefix(after, "[") {
			return text[index[0]:index[1]]
		}
	}
	return ""
}

var templatePath = regexp.MustCompile(`^(?:/steps/[0-9]+)+/(?:prompt$|ai/(?:prompt$|example(?:/|$))|args/|records\.(?:create|update)/set/)`)
