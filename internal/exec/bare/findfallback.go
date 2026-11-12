package bare

import (
	"context"
	"fmt"
	"io/fs"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// A missing search binary never requires an install. The walking fallback
// keeps the tool available and states the ignore rules it can actually apply.
func findToolDescription(caps Caps) string {
	if _, err := exec.LookPath("fd"); err == nil {
		return findDescription(caps)
	}
	return fmt.Sprintf("Search file paths by glob. No fd: ignores .gitignore, skips .git/node_modules/vendor; follows no directory symlinks. Returns relative paths, capped at 1000 results or %s.", sizeWord(caps.MaxBytes))
}

// findGlob treats ** as zero or more path components. A basename pattern
// matches at every depth, as fd's --glob does.
func findGlob(pattern, name string) bool {
	for _, p := range braceGlobs(pattern) {
		if findOneGlob(p, name) {
			return true
		}
	}
	return false
}

func findOneGlob(pattern, name string) bool {
	if !strings.Contains(pattern, "/") {
		ok, _ := path.Match(pattern, path.Base(name))
		return ok
	}
	var match func([]string, []string) bool
	match = func(p, n []string) bool {
		if len(p) == 0 {
			return len(n) == 0
		}
		if p[0] == "**" {
			return match(p[1:], n) || len(n) > 0 && match(p, n[1:])
		}
		if len(n) == 0 {
			return false
		}
		ok, _ := path.Match(p[0], n[0])
		return ok && match(p[1:], n[1:])
	}
	if !strings.HasPrefix(pattern, "**/") {
		pattern = "**/" + pattern
	}
	return match(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func findByWalking(ctx context.Context, cwd, pattern string, dir *string, limit *int, caps Caps) (string, bool, error) {
	for _, component := range strings.Split(pattern, "/") {
		if _, err := path.Match(component, ""); err != nil {
			return RejectArguments(ctx, "Invalid pattern: "+err.Error()), true, nil
		}
	}
	root := cwd
	if dir != nil {
		root = resolveToCwd(*dir, cwd)
	}
	max := 1000
	if limit != nil && *limit > 0 {
		max = *limit
	}
	caps = caps.resolve()
	var names []string
	bytes, sizeHit := 0, false
	err := filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		if file == root {
			return nil
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == "vendor") {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if findGlob(pattern, rel) {
			if entry.IsDir() {
				rel += "/"
			}
			names = append(names, rel)
			bytes += len(rel) + 1
			if bytes >= caps.MaxBytes {
				sizeHit = true
				return fs.SkipAll
			}
			if len(names) >= max {
				return fs.SkipAll
			}
		}
		return nil
	})
	if ctx.Err() != nil {
		return "Operation aborted", true, nil
	}
	if err != nil {
		return err.Error(), true, nil
	}
	if len(names) == 0 {
		return "No files found matching pattern", false, nil
	}
	out := truncateHeadNoLineLimit(strings.Join(names, "\n"), caps)
	text := out.content
	if len(names) >= max {
		text += fmt.Sprintf("\n\n[%d results limit reached. Refine pattern or increase limit]", max)
	}
	if out.truncated || sizeHit {
		text += "\n\n[" + formatSize(caps.MaxBytes) + " limit reached]"
	}
	return text, false, nil
}

// braceGlobs expands bounded comma alternatives while preserving ordinary glob syntax.
func braceGlobs(pattern string) []string {
	out := []string{pattern}
	for count := 0; count < 8; count++ {
		next := []string{}
		changed := false
		for _, p := range out {
			end := strings.IndexByte(p, '}')
			if end < 0 {
				next = append(next, p)
				continue
			}
			start := strings.LastIndexByte(p[:end], '{')
			if start < 0 || !strings.Contains(p[start:end], ",") {
				next = append(next, p)
				continue
			}
			for _, alt := range strings.Split(p[start+1:end], ",") {
				next = append(next, p[:start]+alt+p[end+1:])
				if len(next) > 128 {
					return []string{pattern}
				}
			}
			changed = true
		}
		out = next
		if !changed {
			return out
		}
	}
	return out
}
