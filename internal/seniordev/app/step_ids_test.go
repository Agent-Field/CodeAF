//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
)

// EVERY FINISHED TOOL CALL NAMES THE STEP OF THE PROCESS IT SERVED, from the
// tool, what it was aimed at and the run's progress — and the progress only
// ever moves forward: the first successful edit to a project file turns
// exploring into implementing, and an accepted submit turns everything after
// it into handing in. A submit call itself moves nothing, because a refused
// one completes exactly as an accepted one does; the freeze's stage record,
// written inside the call before its step, is what moved the progress of the
// accepted one (TestOnlyTheFreezeSaysASubmitWasAccepted).
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
		{"a submit moves nothing, since a refused one completes too", stepAction{tool: "submit"}, changed, StepSubmit, changed},
		{"a submit that failed moves nothing either", stepAction{tool: "submit", failed: true}, changed, StepSubmit, changed},
		{"an accepted submit, after its freeze", stepAction{tool: "submit"}, submitted, StepSubmit, submitted},
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

// ONLY THE FREEZE SAYS A SUBMIT WAS ACCEPTED: its `submit · frozen` record
// moves the progress, and a refusal's `submit · refused` and every other stage
// leave it where it was. Nothing moves it back.
func TestOnlyTheFreezeSaysASubmitWasAccepted(t *testing.T) {
	changed := stepProgress{changed: true}
	submitted := stepProgress{changed: true, submitted: true}
	for _, tc := range []struct {
		stage, status string
		progress      stepProgress
		want          stepProgress
	}{
		{"submit", "frozen", changed, submitted},
		{"submit", "refused", changed, changed},
		{"implement", "running", changed, changed},
		{"verification", "pass", changed, changed},
		{"submit", "refused", submitted, submitted},
	} {
		if got := tc.progress.afterStage(tc.stage, tc.status); got != tc.want {
			t.Fatalf("%+v after %s · %s = %+v; want %+v", tc.progress, tc.stage, tc.status, got, tc.want)
		}
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

// A REFUSED SUBMIT MOVES NOTHING. The submit tool tells its model why it was
// refused and lets it keep working, so a refusal settles as a completed call
// exactly as an acceptance does (tool/submit.go); only an accepted submit
// freezes the tree, and only after one is everything the submit step. Here the
// model submits an unchanged tree, then a change with no checklist, and is
// refused both times; what it does after each refusal is still the part of the
// process it was in, and only what follows the third, accepted submit is
// handing in.
func TestOnlyAnAcceptedSubmitTurnsWhatFollowsIntoHandingIn(t *testing.T) {
	runner, state, _, events := soloPipeline(t)
	checklist := filepath.Join(runner.workspace, ".senior-dev", "checklist.md")
	if err := os.Remove(checklist); err != nil {
		t.Fatal(err)
	}
	calls := 0
	// finish reports one tool call the way the step loop settles it
	// (engine/steploop/processor.go): a call whose tool returned an error
	// fails, and every other call completes.
	finish := func(tool string, input map[string]any, result steploop.ToolResult, err error) {
		calls++
		callID := fmt.Sprintf("c%d", calls)
		if err != nil {
			runner.events.busEvent(toolPartPayload(callID, tool, "error", input, "", err.Error()))
			return
		}
		runner.events.busEvent(toolPartPayload(callID, tool, "completed", input, result.Output, ""))
	}
	did := func(tool string, input map[string]any) {
		finish(tool, input, steploop.ToolResult{Output: "ok"}, nil)
	}
	submit := func() steploop.ToolResult {
		input := map[string]any{"reason": "done", "evidence": "make test: exit 0", "checklist_satisfied": true}
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		result, err := runner.runtime.registry.Execute(context.Background(), steploop.ToolCall{
			ID: "submit", Name: "submit", Input: raw, SessionID: "ses_solo",
		})
		finish("submit", input, result, err)
		return result
	}

	did("read", map[string]any{"filePath": "README.md"})
	if refused := submit(); refused.Title != "submit refused" || state.candidate() != nil {
		t.Fatalf("a submit of an unchanged tree was not refused: %+v", refused)
	}
	did("grep", map[string]any{"pattern": "base"})
	if err := writeFile(filepath.Join(runner.workspace, "feature.txt"), "implemented\n"); err != nil {
		t.Fatal(err)
	}
	did("write", map[string]any{"filePath": "feature.txt"})
	if refused := submit(); refused.Title != "submit refused" || state.candidate() != nil {
		t.Fatalf("a submit with no checklist was not refused: %+v", refused)
	}
	if err := writeFile(checklist, "- [x] the feature\n"); err != nil {
		t.Fatal(err)
	}
	did("write", map[string]any{"filePath": checklist})
	did("edit", map[string]any{"filePath": "feature.txt"})
	did("bash", map[string]any{"command": "make test"})
	if accepted := submit(); accepted.Title != "submitted" || state.candidate() == nil {
		t.Fatalf("a submit with a change and a checklist was not accepted: %+v", accepted)
	}
	did("edit", map[string]any{"filePath": "feature.txt"})

	var got []string
	for _, step := range streamSteps(t, events.Bytes()) {
		got = append(got, step.Tool+" "+step.Step)
	}
	want := []string{
		"read " + StepExplore,
		"submit " + StepSubmit,
		"grep " + StepExplore,
		"write " + StepImplement,
		"submit " + StepSubmit,
		"write " + StepChecklist,
		"edit " + StepImplement,
		"bash " + StepImplement,
		"submit " + StepSubmit,
		"edit " + StepSubmit,
	}
	if len(got) != len(want) {
		t.Fatalf("steps = %q, want %q", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("steps = %q, want %q", got, want)
		}
	}
}
