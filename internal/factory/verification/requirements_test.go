package verification

import (
	"strings"
	"testing"
)

func TestCorrectedCargoRequirementsKeepCoverageAndCheckoutBoundaries(t *testing.T) {
	at := Snapshot{Head: strings.Repeat("a", 40), Fingerprint: strings.Repeat("b", 40)}
	makeCheck := func(args string, exit int, order int64) Check {
		return Check{Args: strings.Fields(args), Cwd: "/repo", Context: make([]string, 8), Exit: &exit, Before: at, After: at, order: order, Command: args}
	}
	for _, tc := range []struct {
		name    string
		old     string
		next    []string
		cleared bool
	}{
		{"invalid multiple names corrected individually", "cargo test --test cli alpha beta", []string{"cargo test --test cli alpha", "cargo test --test cli beta"}, true},
		{"invalid names corrected after separator", "cargo test --test cli alpha beta", []string{"cargo test --test cli -- alpha beta"}, true},
		{"one name missing stays blocked", "cargo test --test cli alpha beta", []string{"cargo test --test cli alpha"}, false},
		{"full same target covers failing named test", "cargo test --test cli alpha", []string{"cargo test --test cli"}, true},
		{"passing subset cannot cover full target", "cargo test --test cli", []string{"cargo test --test cli alpha"}, false},
		{"other target cannot cover", "cargo test --test cli alpha beta", []string{"cargo test --lib"}, false},
		{"test options cannot be weakened", "cargo test --test cli -- alpha --exact", []string{"cargo test --test cli alpha"}, false},
		{"other feature scope cannot cover", "cargo test --all-features --test cli alpha beta", []string{"cargo test --test cli"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := makeCheck(tc.old, 1, 1)
			checks := []Check{old}
			for i, s := range tc.next {
				checks = append(checks, makeCheck(s, 0, int64(i+2)))
			}
			latest, _ := RequiredChecks(checks, at, true)
			_, present := latest[old.Key()]
			if !present != tc.cleared {
				t.Fatalf("cleared=%v want %v", !present, tc.cleared)
			}
		})
	}
	old := makeCheck("cargo test --test cli alpha beta", 1, 2)
	for _, mode := range []string{"earlier", "different directory", "different environment", "stale", "mutating", "unknown exit"} {
		t.Run(mode, func(t *testing.T) {
			c := makeCheck("cargo test --test cli", 0, 3)
			switch mode {
			case "earlier":
				c.order = 1
			case "different directory":
				c.Cwd = "/other"
			case "different environment":
				c.Context[0] = "linux"
			case "stale":
				c.Before.Head = strings.Repeat("c", 40)
				c.After = c.Before
			case "mutating":
				c.After.Fingerprint = "changed"
			case "unknown exit":
				c.Exit = nil
			}
			latest, _ := RequiredChecks([]Check{old, c}, at, true)
			if _, ok := latest[old.Key()]; !ok {
				t.Fatal("unrelated or incomplete check cleared failure")
			}
		})
	}
}
