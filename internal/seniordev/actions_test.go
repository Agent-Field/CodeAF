//go:build !windows

package seniordev

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/seniordev/app"
)

// EVERY STEP OF senior-dev's PROCESS HAS ONE WORD ITS PAGE PRINTS, and every
// word is one plain lowercase word with no machinery in it — the same word leads
// its task's row while it is in that step.
func TestEveryStepHasOnePlainWord(t *testing.T) {
	banned := []string{"auditor", "audit", "verdict", "verified", "refuted", "runtime", "contract", "router"}
	words := map[string]bool{setupWord: true, finishWord: true}
	for _, step := range app.Steps {
		word := stepWords[step]
		if word == "" {
			t.Errorf("step %q has no word", step)
		}
		words[word] = true
	}
	for word := range words {
		if word != strings.ToLower(word) || strings.ContainsAny(word, " \t") {
			t.Errorf("step word %q is not one lowercase word", word)
		}
		for _, bad := range banned {
			if strings.Contains(word, bad) {
				t.Errorf("step word %q says %q", word, bad)
			}
		}
	}
}

// actionLog is a run as senior-dev reports it, each record received a second
// after the one before.
func actionLog(records ...delegate.Action) []delegate.Action {
	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	for i := range records {
		records[i].At = start.Add(time.Duration(i) * time.Second)
	}
	return records
}

func stage(name, status string, data map[string]any) delegate.Action {
	raw, _ := json.Marshal(data)
	if data == nil {
		raw = nil
	}
	return delegate.Action{Kind: delegate.ActionStage, Stage: name, Status: status, Data: raw}
}

func step(tool, step, command string, exit ...int) delegate.Action {
	action := delegate.Action{Kind: delegate.ActionStep, Tool: tool, Step: step, Command: tool + ": " + command}
	if len(exit) > 0 {
		action.Exit = &exit[0]
	}
	return action
}

// A RUN READS AS senior-dev's ACTIONS, EACH UNDER THE STEP OF ITS PROCESS IT
// SERVED: set up, the brief written down as its spec, what it read and ran and
// changed, the hand-in with its size and ticks, its own check command by
// command and the result, what it did to the tree and the change it measured,
// and its ending in its own sentence. Machinery — the run's contract, a model
// turn being configured, the usage rollup, the submit call itself — is left
// out; a nudge is senior-dev steering its model, and a retry of the same
// attempt is not a second nudge.
func TestARunReadsAsSeniorDevsActionsUnderItsSteps(t *testing.T) {
	log := actionLog(
		stage("bootstrap", "ready", map[string]any{"recorder": "git"}),
		stage("run-contract", "ready", nil),
		stage("intake", "captured", map[string]any{"spec_bytes": 42}),
		stage("landing", "start-captured", nil),
		stage("implement", "running", map[string]any{"attempt": 0}),
		stage("agent-runtime", "configured", nil),
		step("read", app.StepBrief, "/copy/.senior-dev/spec.md"),
		step("read", app.StepExplore, "internal/auth/middleware.go"),
		step("bash", app.StepExplore, "go test ./internal/auth/...", 1),
		step("write", app.StepPin, ".senior-dev/pinned.txt"),
		step("write", app.StepChecklist, ".senior-dev/checklist.md"),
		step("edit", app.StepImplement, "internal/auth/middleware.go"),
		stage("compaction", "summarized", map[string]any{"summary_status": "valid"}),
		stage("model-switch", "switched", map[string]any{"from": "openrouter/vendor/one", "to": "openrouter/vendor/two", "reason": "previous-rate-limited"}),
		stage("implement", "running", map[string]any{"attempt": 1}),
		stage("implement", "transport-retry", map[string]any{"attempt": 1, "retry": 1, "max_retries": 3}),
		stage("implement", "running", map[string]any{"attempt": 1}),
		step("bash", app.StepImplement, "go test ./internal/auth/...", 0),
		step("submit", app.StepSubmit, "tests pass"),
		stage("submit", "frozen", map[string]any{"patch_files": 4, "checklist_items": 5, "checklist_ticked": 5}),
		stage("implement", "submitted", nil),
		stage("verification", "running", map[string]any{"commands": 2}),
		step("bash", app.StepVerify, "go build ./...", 0),
		step("bash", app.StepVerify, "go test ./...", 2),
		stage("verification", "fail", map[string]any{"commands": 2}),
		stage("ship", "unchanged", nil),
		stage("patch-summary", "completed", map[string]any{"files": 4, "additions": 120, "deletions": 30}),
		stage("agent-summary", "completed", nil),
		delegate.Action{Kind: delegate.ActionEnd, Status: delegate.StatusFail, Message: "submitted a change that the project's own build or tests do not pass"},
	)
	read := Program.Reader()
	type line struct{ step, text, outcome string }
	var got []line
	steers := 0
	for _, action := range log {
		shown, ok := read(action)
		if !ok {
			continue
		}
		if !shown.At.Equal(action.At) {
			t.Fatalf("%q lost its moment: %v, want %v", shown.Text, shown.At, action.At)
		}
		if shown.Steer {
			steers++
		}
		got = append(got, line{shown.Step, shown.Text, shown.Outcome})
	}
	want := []line{
		{"setup", "set up its workspace", "git"},
		{"spec", "wrote your brief down as its spec", ""},
		{"spec", "read its spec", ""},
		{"explore", "read internal/auth/middleware.go", ""},
		{"explore", "ran go test ./internal/auth/...", "fails · exit 1"},
		{"pin", "pinned its check", ""},
		{"checklist", "wrote its checklist", ""},
		{"implement", "edited internal/auth/middleware.go", ""},
		{"", "compacted its memory", ""},
		{"", "switched to two", ""},
		{"implement", "told its model what it found, and to finish and hand in (nudge 1)", ""},
		{"", "the call to its model dropped; started a fresh turn (retry 1 of 3)", ""},
		{"implement", "ran go test ./internal/auth/...", "passes"},
		{"submit", "handed in its work", "4 files · 5 of 5 ticked"},
		{"verify", "checked its work itself, with the project's own build and tests", ""},
		{"verify", "go build ./...", "passes"},
		{"verify", "go test ./...", "fails · exit 2"},
		{"verify", "the project's own build or tests fail", "2 commands"},
		{"finish", "measured its change", "4 files · +120 -30"},
		{"finish", "submitted a change that the project's own build or tests do not pass", ""},
	}
	if len(got) != len(want) {
		t.Fatalf("read %d lines, want %d:\n%+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if steers != 2 {
		t.Fatalf("%d lines are senior-dev steering its model, want the nudge and the retry", steers)
	}
}

// A SWITCH NAMES THE MODEL AND WHY, and a compaction whose summary failed says
// the run kept its own record instead.
func TestASwitchAndACompactionSayWhatHappened(t *testing.T) {
	read := Program.Reader()
	switched, ok := read(stage("model-switch", "switched", map[string]any{"to": "openrouter/vendor/two", "reason": "previous-rate-limited"}))
	if !ok || switched.Model != "openrouter/vendor/two" || switched.Reason != "the last one was rate-limited" {
		t.Fatalf("the switch read %+v", switched)
	}
	fallback, ok := read(stage("compaction", "fallback", nil))
	if !ok || !fallback.Memory || fallback.Outcome != "kept its own record" {
		t.Fatalf("the fallback compaction read %+v", fallback)
	}
}
