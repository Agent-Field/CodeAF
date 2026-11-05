package orchestrate

import (
	"fmt"
	"path"
	"strings"
)

// ValidateScope checks a workspace-relative path or segment glob before a
// planner commits it. Validation precedes cleaning so traversal cannot disappear.
func ValidateScope(scope string) error {
	scope = strings.TrimSpace(scope)
	if path.IsAbs(scope) || scope == "" || path.Clean(scope) == "." {
		return fmt.Errorf("use a workspace-relative file, directory or glob")
	}
	for _, part := range strings.Split(scope, "/") {
		if part == ".." {
			return fmt.Errorf(".. segments are not allowed")
		}
		if strings.Contains(part, "**") {
			return fmt.Errorf("use *, ? or [...] within a segment; ** is not supported")
		}
		if _, err := path.Match(part, ""); err != nil {
			return fmt.Errorf("invalid glob: %w", err)
		}
	}
	return nil
}

// Covers reports whether a scope contains a workspace-relative path. Each glob
// segment matches one path segment; a matched directory covers its subtree.
func Covers(scope []string, target string) bool {
	if path.IsAbs(target) {
		return false
	}
	for _, part := range strings.Split(target, "/") {
		if part == ".." {
			return false
		}
	}
	target = path.Clean(target)
	if target == "." {
		return false
	}
	for _, allowed := range scope {
		if pathCovers(allowed, target) {
			return true
		}
	}
	return false
}

func pathCovers(pattern, target string) bool {
	if ValidateScope(pattern) != nil {
		return false
	}
	parts := strings.Split(path.Clean(strings.TrimSpace(pattern)), "/")
	targets := strings.Split(target, "/")
	if len(parts) > len(targets) {
		return false
	}
	for i, part := range parts {
		if !scopeSegmentMatches(part, targets[i]) {
			return false
		}
	}
	return true
}

// scopeSegmentMatches follows the shell: a wildcard or class never matches a
// leading dot, so only a pattern segment that itself starts with "." reaches
// .git, .env or .codeaf. Git hooks run code, and a scope that reads as narrow
// must not reach them.
func scopeSegmentMatches(pattern, target string) bool {
	if strings.HasPrefix(target, ".") && !strings.HasPrefix(pattern, ".") {
		return false
	}
	matched, err := path.Match(pattern, target)
	return err == nil && matched
}

// ScopesOverlap shares the write guard's segment matcher. Two pattern segments
// may intersect without matching each other's spelling, so those pairs are
// conservatively serialized rather than risking concurrent writes to one file.
func ScopesOverlap(left, right []string) bool {
	for _, one := range left {
		if ValidateScope(one) != nil {
			continue
		}
		for _, two := range right {
			if ValidateScope(two) != nil {
				continue
			}
			a := strings.Split(path.Clean(strings.TrimSpace(one)), "/")
			b := strings.Split(path.Clean(strings.TrimSpace(two)), "/")
			overlap := true
			for i := 0; i < len(a) && i < len(b); i++ {
				if !scopeSegmentMatches(a[i], b[i]) && !scopeSegmentMatches(b[i], a[i]) &&
					!(strings.ContainsAny(a[i], "*?[") && strings.ContainsAny(b[i], "*?[")) {
					overlap = false
					break
				}
			}
			if overlap {
				return true
			}
		}
	}
	return false
}
