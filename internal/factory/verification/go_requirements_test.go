package verification

import (
	"strings"
	"testing"
)

func TestGoCoverageReconcilesCorrectedRunsWithoutWeakeningChecks(t *testing.T) {
	at := Snapshot{Head: strings.Repeat("a", 40), Fingerprint: strings.Repeat("b", 40)}
	check := func(args string, exit int, order int64) Check {
		return Check{Args: strings.Fields(args), Cwd: "/repo", Context: make([]string, 8), Exit: &exit, Before: at, After: at, order: order, Command: args, Source: "chat", ID: args}
	}
	for _, tc := range []struct {
		name, old, next string
		clear           bool
	}{
		{"build parallelism is not coverage", "go test -count=1 ./...", "go test -count=1 -p 8 ./...", true},
		{"JSON presentation is not coverage", "go test -json ./...", "go test -count=1 -v ./...", true},
		{"full suite covers package group", "go test -count=1 ./internal/a/ ./internal/b/", "go test -count=1 -p 8 ./...", true},
		{"full package covers selected tests", "go test -run TestBroken ./internal/a", "go test -count=1 ./internal/a", true},
		{"subtree covers child", "go test ./internal/a", "go test ./internal/...", true},
		{"stronger repetition covers", "go test -count=1 ./...", "go test -count=2 ./...", true},
		{"subset cannot cover full", "go test ./...", "go test ./internal/a", false},
		{"selected test cannot cover full", "go test ./...", "go test -run TestFine ./...", false},
		{"other selection cannot cover", "go test -run TestBroken ./...", "go test -run TestFine ./...", false},
		{"race cannot be dropped", "go test -race ./...", "go test ./...", false},
		{"tags cannot be dropped", "go test -tags e2e ./...", "go test ./...", false},
		{"weaker repetitions cannot cover", "go test -count=2 ./...", "go test ./...", false},
		{"unknown options stay exact", "go test -future-option ./...", "go test ./...", false},
		{"binary args stay exact", "go test ./... -args -custom", "go test ./...", false},
		{"no tests is not a pass", "go test ./...", "go test -count=0 ./...", false},
		{"compile only is not a test", "go test ./...", "go test -c ./...", false},
		{"list is not a test", "go test ./...", "go test -list . ./...", false},
		{"dry run is not a test", "go test ./...", "go test -n ./...", false},
		{"other timeout stays exact", "go test -timeout 1m ./...", "go test -timeout 2m ./...", false},
		{"directory escape is not contained", "go test ./../other", "go test ./...", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old, next := check(tc.old, 1, 1), check(tc.next, 0, 2)
			resolved := Resolve([]Check{old, next}, at, true)
			latest, _ := RequiredChecks(resolved, at, true)
			_, present := latest[old.Key()]
			if !present != tc.clear {
				t.Fatalf("cleared=%v want %v", !present, tc.clear)
			}
			if tc.clear && (resolved[0].Exit == nil || *resolved[0].Exit != 1 || resolved[0].SupersededBy == "" || resolved[0].SupersessionReason == "") {
				t.Fatal("lost original failure or disposition")
			}
		})
	}
	old := check("go test -count=1 ./...", 1, 1)
	for _, mode := range []string{"earlier pass", "other cwd", "other GOOS", "other GOFLAGS", "stale", "mutating", "unknown exit", "corrupt", "diagnostic", "later failure"} {
		t.Run(mode, func(t *testing.T) {
			next := check("go test -count=1 -p 8 ./...", 0, 2)
			switch mode {
			case "earlier pass":
				next.order = 0
			case "other cwd":
				next.Cwd = "/other"
			case "other GOOS":
				next.Context[0] = "linux"
			case "other GOFLAGS":
				next.Context[2] = "-tags=e2e"
			case "stale":
				next.Before.Head = strings.Repeat("c", 40)
				next.After = next.Before
			case "mutating":
				next.After.Fingerprint = "changed"
			case "unknown exit":
				next.Exit = nil
			case "corrupt":
				next.Problem = "bad receipt"
			case "diagnostic":
				next.Diagnostic = true
			}
			checks := []Check{old, next}
			if mode == "later failure" {
				checks = append(checks, check("go test -count=1 -p 8 ./...", 1, 3))
			}
			latest, _ := RequiredChecks(checks, at, true)
			if _, ok := latest[old.Key()]; !ok {
				t.Fatal("unrelated or incomplete evidence erased failure")
			}
		})
	}
}
