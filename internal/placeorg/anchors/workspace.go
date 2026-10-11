package anchors

import (
	"path/filepath"
	"regexp"
	"strings"
)

var uuidSuffix = regexp.MustCompile(`(?i)(?:^|[-_])[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// workspaceFamily compares caller-supplied absolute paths without opening
// them, following placegraph's cleanFolder rule. Only the basename survives:
// parents and user names are not project evidence. Ephemeral suffixes should
// not separate two worktrees belonging to the same workspace family.
func workspaceFamily(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" || !filepath.IsAbs(workspace) {
		return ""
	}
	clean := filepath.Clean(workspace)
	parent := strings.ToLower(filepath.Base(filepath.Dir(clean)))
	if parent == "home" || parent == "users" {
		return ""
	}
	base := strings.ToLower(filepath.Base(clean))
	base = uuidSuffix.ReplaceAllString(base, "")
	for {
		i := strings.LastIndexAny(base, "-_")
		if i < 0 || !numericSuffix(base[i+1:]) {
			break
		}
		base = base[:i]
	}
	base = strings.Trim(base, "-_")
	if genericRoot(base) || numericSuffix(base) || uuidSuffix.MatchString(base) {
		return ""
	}
	return base
}

// These are filesystem container names, never a list of chat subjects.
func genericRoot(base string) bool {
	switch base {
	case "", "/", ".", "home", "users", "root", "tmp", "temp", "var", "work", "workspace", "workspaces", "projects", "repos", "repositories", "src", "desktop", "documents", "downloads", "wt", "worktrees":
		return true
	}
	return false
}
