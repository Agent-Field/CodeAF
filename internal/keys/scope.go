package keys

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// ScopeOf names the project a cell's secrets belong to (SCHEMAS.md vault
// rulings): the normalized remote it was based on, else the cell's own id.
func ScopeOf(c cell.Cell) string {
	if base := c.Meta().Base; base != nil && base.Remote != "" {
		return NormalizeRemote(base.Remote)
	}
	return c.ID
}

// NormalizeRemote makes the same repository the same scope however it was
// cloned: no scheme, no credentials, no .git suffix, host in lower case.
func NormalizeRemote(remote string) string {
	r := strings.TrimSpace(remote)
	if _, rest, ok := strings.Cut(r, "://"); ok {
		r = rest
	}
	if _, rest, ok := strings.Cut(r, "@"); ok {
		r = rest
	}
	host, path, _ := strings.Cut(strings.Replace(r, ":", "/", 1), "/")
	return strings.ToLower(host) + "/" + strings.TrimSuffix(strings.Trim(path, "/"), ".git")
}
