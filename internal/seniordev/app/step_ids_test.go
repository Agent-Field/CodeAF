//go:build !windows

package app

import "testing"

// EVERY FINISHED TOOL CALL NAMES THE STEP OF THE PROCESS IT SERVED, from the
// tool, what it was aimed at and the run's progress — and the progress only
// ever moves forward: the first successful edit to a project file turns
// exploring into implementing, and an accepted submit turns everything after
// it into handing in.
func TestAToolCallNamesTheStepItServed(t *testing.T) {
	fresh := stepProgress{}
	changed := stepProgress{changed: true}
	submitted := stepProgress{changed: true, submitted: true}
	for _, tc := range []struct {
		name     string
		action   stepAction
		progress stepProgress
		want     string
		after    stepProgress
	}{
		{"reading the spec", stepAction{tool: "read", target: ".senior-dev/spec.md"}, fresh, StepBrief, fresh},
		{"reading the spec by its absolute path", stepAction{tool: "read", target: "/copy/.senior-dev/spec.md"}, fresh, StepBrief, fresh},
		{"a shell printing the spec", stepAction{tool: "bash", target: "cat .senior-dev/spec.md"}, changed, StepBrief, changed},
		{"a file that only ends like the spec", stepAction{tool: "read", target: "my.senior-dev/spec.md"}, fresh, StepExplore, fresh},
		{"reading code before any change", stepAction{tool: "read", target: "internal/auth/middleware.go"}, fresh, StepExplore, fresh},
		{"a search before any change", stepAction{tool: "grep", target: "internal"}, fresh, StepExplore, fresh},
		{"a command before any change", stepAction{tool: "bash", target: "go test ./internal/auth/..."}, fresh, StepExplore, fresh},
		{"a fetch before any change", stepAction{tool: "webfetch", target: "https://go.dev/doc"}, fresh, StepExplore, fresh},
		{"writing the pinned check", stepAction{tool: "write", target: ".senior-dev/pinned.txt"}, fresh, StepPin, fresh},
		{"a shell writing the pinned check", stepAction{tool: "bash", target: "echo 'go test ./...' > .senior-dev/pinned.txt"}, fresh, StepPin, fresh},
		{"writing the checklist", stepAction{tool: "write", target: ".senior-dev/checklist.md"}, changed, StepChecklist, changed},
		{"ticking the checklist", stepAction{tool: "edit", target: "/copy/.senior-dev/checklist.md"}, changed, StepChecklist, changed},
		{"the first edit to a project file", stepAction{tool: "edit", target: "internal/auth/middleware.go"}, fresh, StepImplement, changed},
		{"an edit that failed changes nothing", stepAction{tool: "edit", target: "internal/x.go", failed: true}, fresh, StepImplement, fresh},
		{"a new file", stepAction{tool: "write", target: "internal/auth/store.go"}, fresh, StepImplement, changed},
		{"a patch to a project file", stepAction{tool: "apply_patch", target: "*** Begin Patch\n*** Update File: a.go\n@@\n-x\n+y\n*** End Patch"}, fresh, StepImplement, changed},
		{"a patch to the checklist alone", stepAction{tool: "apply_patch", target: "*** Begin Patch\n*** Update File: .senior-dev/checklist.md\n@@\n-[ ] a\n+[x] a\n*** End Patch"}, changed, StepChecklist, changed},
		{"a read after the first change", stepAction{tool: "read", target: "internal/auth/middleware.go"}, changed, StepImplement, changed},
		{"a command after the first change", stepAction{tool: "bash", target: "go test ./..."}, changed, StepImplement, changed},
		{"a question before any change", stepAction{tool: "question"}, fresh, StepExplore, fresh},
		{"a question after a change", stepAction{tool: "question"}, changed, StepImplement, changed},
		{"a refused submit", stepAction{tool: "submit", failed: true}, changed, StepSubmit, changed},
		{"an accepted submit", stepAction{tool: "submit"}, changed, StepSubmit, submitted},
		{"anything after an accepted submit", stepAction{tool: "edit", target: "a.go"}, submitted, StepSubmit, submitted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, after := stepOf(tc.action, tc.progress)
			if got != tc.want || after != tc.after {
				t.Fatalf("stepOf(%+v, %+v) = %q, %+v; want %q, %+v", tc.action, tc.progress, got, after, tc.want, tc.after)
			}
		})
	}
}

// THE STEPS ARE THE ONE LIST: every id the classifier can answer is in Steps,
// once, and verify — which no tool call is — is there for the run's own checks.
func TestEveryStepIdIsInTheOneList(t *testing.T) {
	seen := map[string]bool{}
	for _, id := range Steps {
		if seen[id] {
			t.Fatalf("step %q is listed twice", id)
		}
		seen[id] = true
	}
	for _, id := range []string{StepBrief, StepExplore, StepPin, StepChecklist, StepImplement, StepSubmit, StepVerify} {
		if !seen[id] {
			t.Fatalf("step %q is not in Steps", id)
		}
	}
}
