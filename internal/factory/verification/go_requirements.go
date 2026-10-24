package verification

import (
	"encoding/json"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Resolve records why an earlier Go attempt no longer needs a rerun. Coverage
// is intentionally small: only known flags, the same directory/target context,
// local package containment and identical or unfiltered test selection. Unknown
// invocations keep exact identity and cannot authorize a broader success claim.
func Resolve(checks []Check, at Snapshot, fresh bool) []Check {
	out := slices.Clone(checks)
	sort.SliceStable(out, func(i, j int) bool { return out[i].order < out[j].order })
	scopes := make([]goScope, len(out))
	recognized := make([]bool, len(out))
	keys := make([]string, len(out))
	lastFailure := map[string]int{}
	for i, c := range out {
		scopes[i], recognized[i] = goRequirement(c)
		keys[i] = c.Key()
		if !c.Diagnostic && (c.Problem != "" || (c.Exit != nil && *c.Exit != 0)) {
			lastFailure[keys[i]] = i
		}
	}
	for i := range out {
		old := &out[i]
		if old.Diagnostic || old.SupersededBy != "" || old.Problem != "" {
			continue
		}
		oldScope := scopes[i]
		if !recognized[i] {
			continue
		}
		for j := len(out) - 1; j > i; j-- {
			next := out[j]
			if next.Diagnostic || next.SupersededBy != "" || next.Problem != "" || next.Exit == nil || *next.Exit != 0 {
				continue
			}
			if !next.Fresh(at) && fresh {
				continue
			}
			if next.Before.Head == "" || next.Before != next.After {
				continue
			}
			// A later definite failure of this covering invocation cannot be erased by
			// an older green run. An in-flight repeat is owned by the native executor.
			if failedAt, exists := lastFailure[keys[j]]; exists && failedAt > j {
				continue
			}
			if !recognized[j] || !scopes[j].covers(oldScope) {
				continue
			}
			// Exact successful attempts remain the current authority. A stale pass
			// may be replaced, while an unresolved or broader attempt keeps its record.
			if old.Exit != nil && *old.Exit == 0 && (!fresh || old.Fresh(at)) {
				continue
			}
			old.SupersededBy = next.Source + ":" + next.ID
			if next.ID == "" {
				old.SupersededBy = next.Key()
			}
			old.SupersessionReason = "later measured Go test pass covers the same target, test selection and all requested packages"
			break
		}
	}
	return out
}

type goScope struct {
	identity string
	packages []string
	run      string
	count    int
}

func (next goScope) covers(old goScope) bool {
	if next.identity != old.identity || next.count < old.count || (next.run != "" && next.run != old.run) {
		return false
	}
	for _, wanted := range old.packages {
		covered := false
		for _, got := range next.packages {
			if got == wanted {
				covered = true
				break
			}
			if strings.HasPrefix(got, "./") && strings.HasSuffix(got, "/...") && strings.HasPrefix(wanted, "./") {
				base := strings.TrimSuffix(got, "/...")
				target := strings.TrimSuffix(wanted, "/...")
				// ./... contains all local packages; ./pkg/... contains ./pkg itself.
				if base == "." || target == base || strings.HasPrefix(target, base+"/") {
					covered = true
					break
				}
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

func goRequirement(c Check) (goScope, bool) {
	var scope goScope
	a := c.Args
	if len(a) < 2 || a[0] != "go" || a[1] != "test" || c.Cwd == "" {
		return scope, false
	}
	scope.count = 1
	var invariant []string
	// Build, platform, race and test-binary semantics remain exact. Only output
	// presentation and build parallelism are ignored; neither selects tests.
	valueFlags := []string{"-tags", "-mod", "-modfile", "-overlay", "-coverpkg", "-covermode", "-coverprofile", "-timeout", "-parallel", "-shuffle", "-cpu", "-vet", "-gcflags", "-asmflags", "-ldflags", "-exec"}
	boolFlags := []string{"-race", "-cover", "-short", "-failfast", "-asan", "-msan", "-trimpath", "-work", "-x"}
	seen := map[string]bool{}
	for i := 2; i < len(a); i++ {
		arg := a[i]
		if arg == "-args" || arg == "--" {
			return scope, false
		}
		if !strings.HasPrefix(arg, "-") {
			// Do not infer containment for paths escaping this working directory.
			if strings.HasPrefix(arg, "./") && (path.Clean(strings.TrimSuffix(arg, "/...")) == ".." || strings.HasPrefix(path.Clean(strings.TrimSuffix(arg, "/...")), "../")) {
				return scope, false
			}
			scope.packages = append(scope.packages, arg)
			continue
		}
		flag, val, equal := strings.Cut(arg, "=")
		if seen[flag] {
			return scope, false
		}
		seen[flag] = true
		isBool := flag == "-v" || flag == "-json" || slices.Contains(boolFlags, flag)
		if isBool {
			if equal && val != "true" && val != "false" {
				return scope, false
			}
			if !equal {
				val = "true"
			}
			if flag != "-v" && flag != "-json" {
				invariant = append(invariant, flag+"="+val)
			}
			continue
		}
		if flag != "-p" && flag != "-count" && flag != "-run" && !slices.Contains(valueFlags, flag) {
			return scope, false
		}
		if !equal {
			i++
			if i >= len(a) {
				return scope, false
			}
			val = a[i]
		}
		switch flag {
		case "-p", "-count":
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				return scope, false
			}
			if flag == "-count" {
				scope.count = n
			}
		case "-run":
			scope.run = val
		default:
			invariant = append(invariant, flag+"="+val)
		}
	}
	if len(scope.packages) == 0 {
		scope.packages = []string{"."}
	}
	sort.Strings(invariant)
	raw, _ := json.Marshal([]any{c.Cwd, c.Context, invariant})
	scope.identity = string(raw)
	return scope, true
}
