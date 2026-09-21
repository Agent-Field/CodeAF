// Package notify formats notification recipients.
package notify

import "strings"

// Recipient is the domain an address delivers to — everything after the
// last @. An address with no @ is not deliverable and answers "".
func Recipient(address string) string {
	i := strings.Index(address, "@")
	if i < 0 {
		return ""
	}
	return address[:i]
}
