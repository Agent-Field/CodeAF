package bare

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/home"
)

// These are output locations, not a blanket exclusion of the state home:
// work/, trees/ and artifacts/ may contain the source the user wants searched.
var grepStateOutputs = grepRuntimePatterns()

func grepRuntimePatterns() []string {
	patterns := []string{
		"logs", "jobs", "stubs", "trace", "runs/*/tasks", "runs/*/logs", "runs/*/trace",
		"runs/*/transcript.jsonl", "v3/runs/*/*/*.jsonl", "v3/runs/*/*/logs",
		"v3/runs/*/*/tasks", "v3/tasks/*/*.jsonl", "v3/usage.jsonl", "v3/fixes.json",
		"v3/history.jsonl", "v3/projects/*/tasks.jsonl",
	}
	for _, place := range []string{"v3/projects/*/*", "v3/errands/*", "v3/automations/runs/*/*"} {
		for _, output := range []string{"logs", "tasks", "transcript.jsonl", "state.json", "tasks.json"} {
			patterns = append(patterns, place+"/"+output)
		}
	}
	return patterns
}

var grepLegacyOutputs = []string{"jobs", "logs", "stubs", "trace"}

func grepCanonical(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return resolved
	}
	return filepath.Clean(absolute)
}
func grepWithin(parent, child string) (string, bool) {
	relative, err := filepath.Rel(parent, child)
	return filepath.ToSlash(relative), err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

type grepRuntimePolicy struct{ state, temporary string }

func newGrepRuntimePolicy() grepRuntimePolicy {
	return grepRuntimePolicy{state: grepCanonical(home.Dir()), temporary: grepCanonical(os.TempDir())}
}
func grepPatternPrefix(pattern, path string) bool {
	expected, actual := strings.Split(pattern, "/"), strings.Split(path, "/")
	if len(actual) < len(expected) {
		return false
	}
	for i, part := range expected {
		if ok, _ := filepath.Match(part, actual[i]); !ok {
			return false
		}
	}
	return true
}
func (p grepRuntimePolicy) excludes(path string) bool {
	if filepath.Dir(path) == p.temporary {
		if matched, _ := filepath.Match("pi-bash-*.log", filepath.Base(path)); matched {
			return true
		}
	}
	if relative, inside := grepWithin(p.state, path); inside {
		for _, pattern := range grepStateOutputs {
			if grepPatternPrefix(pattern, relative) {
				return true
			}
		}
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i, part := range parts {
		if part != ".codeaf" || i+1 >= len(parts) {
			continue
		}
		for _, output := range grepLegacyOutputs {
			if parts[i+1] == output {
				return true
			}
		}
	}
	return false
}
func grepEscapeGlob(text string) string {
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[", "]", "\\]", "{", "\\{", "}", "\\}").Replace(text)
}

// Exclusions are rooted at rg's working directory and appended AFTER the
// user's glob. Starting within a runtime directory is rejected separately;
// starting within the state root simply shortens these same patterns.
func (p grepRuntimePolicy) rgGlobs(root string) []string {
	var globs []string
	add := func(pattern string) { globs = append(globs, "!"+pattern, "!"+pattern+"/**") }
	if relative, inside := grepWithin(root, p.temporary); inside {
		prefix := ""
		if relative != "." {
			prefix = grepEscapeGlob(relative) + "/"
		}
		add("/" + prefix + "pi-bash-*.log")
	}
	for _, output := range grepLegacyOutputs {
		add("**/.codeaf/" + output)
		if filepath.Base(root) == ".codeaf" {
			add("/" + output)
		}
	}
	if relative, inside := grepWithin(root, p.state); inside {
		prefix := ""
		if relative != "." {
			prefix = grepEscapeGlob(relative) + "/"
		}
		for _, pattern := range grepStateOutputs {
			add("/" + prefix + pattern)
		}
	} else if relative, inside := grepWithin(p.state, root); inside {
		actual := strings.Split(relative, "/")
		for _, pattern := range grepStateOutputs {
			expected := strings.Split(pattern, "/")
			if len(actual) >= len(expected) {
				continue
			}
			match := true
			for i, part := range actual {
				if ok, _ := filepath.Match(expected[i], part); !ok {
					match = false
					break
				}
			}
			if match {
				add("/" + strings.Join(expected[len(actual):], "/"))
			}
		}
	}
	return globs
}
