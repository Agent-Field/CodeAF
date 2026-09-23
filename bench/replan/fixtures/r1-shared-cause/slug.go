package contacts

import "strings"

// Slug turns a title into the lower-case, dash-separated form used in links.
func Slug(title string) string {
	return strings.ReplaceAll(canonical(title), " ", "-")
}
