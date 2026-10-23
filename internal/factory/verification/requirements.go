package verification

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
)

// RequiredChecks retains measured attempts but judges corrected Cargo invocations
// by their test scope. Invalid argv is never proof that a test actually failed.
// Unknown commands, target flags and environments retain their exact identity.
func RequiredChecks(checks []Check, at Snapshot, fresh bool) (map[string]Check, map[string]bool) {
	latest := map[string]Check{}
	for _, c := range checks {
		if c.Diagnostic || c.SupersededBy != "" {
			continue
		}
		// The observer publishes argv before starting the executable. An empty
		// in-flight placeholder is not an executed check or a permanent obligation.
		// Native job ownership still blocks stage closure; the shell journal keeps
		// an unexecuted/failed tool call honest. Committed corruption stays blocking.
		if len(c.Args) == 0 && c.Exit == nil && c.Problem == "" {
			continue
		}
		latest[c.Key()] = c
	}
	superseded := map[string]bool{}
	for key, old := range latest {
		scope, names, invalid, ok := cargoRequirement(old)
		if !ok {
			continue
		}
		covered := map[string]bool{}
		for _, c := range checks {
			if c.Diagnostic || c.SupersededBy != "" || c.Problem != "" {
				continue
			}
			if c.order < old.order || c.Exit == nil || *c.Exit != 0 || (fresh && !c.Fresh(at)) || (!fresh && (c.Before.Head == "" || c.Before != c.After)) {
				continue
			}
			other, selected, bad, recognized := cargoRequirement(c)
			if !recognized || bad || other != scope {
				continue
			}
			if len(selected) == 0 {
				for _, n := range names {
					covered[n] = true
				}
			} else {
				for _, n := range names {
					for _, filter := range selected {
						if strings.Contains(n, filter) {
							covered[n] = true
						}
					}
				}
			}
		}
		if len(names) > 0 && len(covered) == len(names) && (invalid || old.Exit == nil || *old.Exit != 0 || (fresh && !old.Fresh(at))) {
			delete(latest, key)
			superseded[old.Command] = true
		}
	}
	return latest, superseded
}

// InvocationProblem recognizes only Cargo's unambiguous extra TESTNAME syntax.
// A compiler error or an arbitrary nonzero exit must never receive this label.
func (c Check) InvocationProblem() string {
	_, _, invalid, ok := cargoRequirement(c)
	if ok && invalid && c.Exit != nil && *c.Exit != 0 {
		return "Cargo rejected multiple test-name arguments; use one filter or put test names after --"
	}
	return ""
}

func cargoRequirement(c Check) (string, []string, bool, bool) {
	a := c.Args
	if len(a) < 2 || filepath.Base(a[0]) != "cargo" || a[1] != "test" {
		return "", nil, false, false
	}
	scope := []string{"cargo", "test"}
	var names []string
	tail := false
	invalid := false
	values := []string{"--test", "--bin", "--bench", "--example", "--package", "-p", "--features", "-F", "--target", "--manifest-path", "--config", "--profile", "--exclude", "--target-dir", "--jobs", "-j", "--message-format", "-Z"}
	flags := []string{"--lib", "--tests", "--bins", "--benches", "--examples", "--all-targets", "--workspace", "--all", "--all-features", "--no-default-features", "--release", "--locked", "--frozen", "--offline", "--no-fail-fast"}
	for i := 2; i < len(a); i++ {
		s := a[i]
		if s == "--" {
			if tail {
				return "", nil, false, false
			}
			tail = true
			continue
		}
		if s == "-v" || s == "--verbose" || s == "-q" || s == "--quiet" {
			continue
		}
		if strings.HasPrefix(s, "-") {
			if tail {
				return "", nil, false, false
			}
			name, _, eq := strings.Cut(s, "=")
			if slices.Contains(values, name) {
				scope = append(scope, s)
				if !eq {
					i++
					if i == len(a) {
						return "", nil, false, false
					}
					scope = append(scope, a[i])
				}
				continue
			}
			if slices.Contains(flags, s) {
				scope = append(scope, s)
				continue
			}
			return "", nil, false, false
		}
		if !tail && len(names) > 0 {
			invalid = true
		}
		names = append(names, s)
	}
	b, _ := json.Marshal([]any{c.Cwd, c.Context, scope})
	return string(b), names, invalid, true
}
