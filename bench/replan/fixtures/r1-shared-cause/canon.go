package contacts

import "strings"

// canonical is the one spelling every comparison in this package goes
// through, so that "Ada" and "ada" are treated as the same name.
func canonical(s string) string {
	return strings.ToLower(s)
}
