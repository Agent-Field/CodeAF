// Package sessionid answers one question for the whole product: is this string
// the id of a session. It is a leaf so that both the session package and the
// packages it depends on (teams, spend attribution) can ask it without a cycle.
package sessionid

import (
	"regexp"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// legacyShape is the id a session folder carried before cells: 16 lowercase
// hex characters (session.NewSessionID).
var legacyShape = regexp.MustCompile(`^[0-9a-f]{16}$`)

// Valid reports whether s names a session: a cell ULID or a legacy 16-hex id.
func Valid(s string) bool {
	return cell.ValidID(s) || legacyShape.MatchString(s)
}
