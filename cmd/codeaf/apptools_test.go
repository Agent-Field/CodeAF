package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// toolRow waits for the turn.tool row matching.
func toolRow(rec *appRecorder, t *testing.T, match func(appwire.TurnTool) bool) appwire.TurnTool {
	t.Helper()
	i := rec.waitFor(t, 3*time.Second, func(event string, data any) bool {
		row, ok := data.(appwire.TurnTool)
		return ok && match(row)
	})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.data[i].(appwire.TurnTool)
}

// EVERY CALL CARRIES THE FACTS ITS ROW IS GROUPED BY: its kind, its clock, the
// calls started with it, its exit code, the earlier run it repeats, the lines
// an edit changed, what it was asked, and the tail of what it printed, which
// tools.output reads whole after the turn.
func TestAppToolRowsCarryTheWorkLogFacts(t *testing.T) {
	exit := 1
	hold := make(chan struct{})
	defer close(hold)
	conv := &fakeConversation{script: []session.Event{
		{Kind: session.EventToolBegin, CallID: "c1", Tool: "bash", Args: `{"command":"go test ./api/...","timeout":120}`, Batch: "c1"},
		{Kind: session.EventToolBegin, CallID: "c2", Tool: "grep", Args: `{"pattern":"MaxAttempts","path":"api/"}`, Batch: "c1"},
		{Kind: session.EventToolOutput, CallID: "c1", Tool: "bash", Text: "ok acme/api/billing\n--- FAIL: TestRetry\nFAIL acme/api/webhooks\n"},
		{Kind: session.EventToolFinished, CallID: "c1", Tool: "bash", Took: 1200 * time.Millisecond},
		{Kind: session.EventToolFailed, CallID: "c1", Tool: "bash", Hint: "FAIL acme/api/webhooks", Exit: &exit},
		{Kind: session.EventToolEnd, CallID: "c2", Tool: "grep", Output: "api/retry.go:12: MaxAttempts = 5"},
		{Kind: session.EventToolBegin, CallID: "c3", Tool: "edit", Args: `{"path":"api/deliver.go","edits":[{"oldText":"a\nb\n","newText":"a\nc\nd\n"}]}`},
		{Kind: session.EventToolEnd, CallID: "c3", Tool: "edit"},
		{Kind: session.EventToolBegin, CallID: "c4", Tool: "bash", Args: `{"command":"go test ./api/..."}`},
		{Kind: session.EventToolEnd, CallID: "c4", Tool: "bash", Output: "ok acme/api/webhooks"},
		{Kind: session.EventToolBegin, CallID: "c5", Tool: "bash", Args: `{"command":"sleep 60"}`},
	}, hold: hold}
	backend, rec, agent := workBackend(t, conv)
	backend.toolQuiet = 0
	if _, err := backend.Send(agent.ID, "fix the retries", ""); err != nil {
		t.Fatal(err)
	}

	run := toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Call == "c1" && row.Phase == appwire.PhaseBegin })
	if run.Kind != appwire.ToolRun || run.Batch != "c1" || run.StartedAt == nil || run.Input == nil || run.Input.Text != "go test ./api/..." || run.Input.TimeoutS == nil || *run.Input.TimeoutS != 120 || run.Input.Cwd == "" {
		t.Fatalf("the command's row = %+v", run)
	}
	failed := toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Call == "c1" && row.Phase == appwire.PhaseEnd })
	if failed.ExitCode == nil || *failed.ExitCode != 1 || failed.EndedAt == nil || failed.EndedAt.Sub(*failed.StartedAt) != 1200*time.Millisecond || failed.Lines == nil || *failed.Lines != 3 || len(failed.Tail) != 3 || failed.Tail[2] != "FAIL acme/api/webhooks" {
		t.Fatalf("the failed command's row = %+v", failed)
	}
	search := toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Call == "c2" && row.Phase == appwire.PhaseEnd })
	if search.Kind != appwire.ToolSearch || search.Batch != "c1" || search.Input == nil || search.Input.Text == "" || search.ExitCode != nil || len(search.Tail) != 1 {
		t.Fatalf("the search's row = %+v", search)
	}
	edit := toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Call == "c3" && row.Phase == appwire.PhaseEnd })
	if edit.Kind != appwire.ToolEdit || len(edit.Files) != 1 || edit.Files[0].Path != "api/deliver.go" || edit.Files[0].Add != 2 || edit.Files[0].Del != 1 || edit.Batch != "" {
		t.Fatalf("the edit's row = %+v", edit)
	}
	retry := toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Call == "c4" && row.Phase == appwire.PhaseEnd })
	if retry.RetriedOf != "c1" || retry.ExitCode == nil || *retry.ExitCode != 0 {
		t.Fatalf("the rerun's row = %+v", retry)
	}

	// A long call is said again with a fresh tail, and a follower is told
	// each new line.
	toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Call == "c5" && row.Phase == appwire.PhaseBegin })
	if err := backend.FollowTool(agent.ID, "c5", true); err != nil {
		t.Fatal(err)
	}
	a, _ := backend.find(agent.ID)
	live := backend.liveOf(a)
	backend.callOutput(a, agent.ID, live, session.Event{Kind: session.EventToolOutput, CallID: "c5", Text: "tick 1\ntick 2\n"})
	rec.waitFor(t, 3*time.Second, func(event string, data any) bool {
		out, ok := data.(appwire.ToolOutputEvent)
		return ok && out.Call == "c5" && len(out.Lines) == 2 && out.Lines[1] == "tick 2"
	})
	fresh := toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Call == "c5" && len(row.Tail) == 2 })
	if fresh.Phase != appwire.PhaseBegin || fresh.Tail[0] != "tick 1" {
		t.Fatalf("the long call said again = %+v", fresh)
	}

	lines, next, total, err := backend.ToolOutput(agent.ID, "c1", 1, 1)
	if err != nil || len(lines) != 1 || lines[0] != "--- FAIL: TestRetry" || next != 2 || total != 3 {
		t.Fatalf("tools.output = %q %d %d %v", lines, next, total, err)
	}
	if _, _, _, err := backend.ToolOutput(agent.ID, "nothing", 0, 0); err == nil || !strings.Contains(err.Error(), "no output") {
		t.Fatalf("a call with no output = %v", err)
	}
}

// A COMMAND'S KIND COMES FROM THE ENGINE'S OWN FAMILIES: a page fetched is
// web, a task proposed is a task, an account's tool is a connector.
func TestAppToolKindsReadTheEnginesFamilies(t *testing.T) {
	for _, c := range []struct{ tool, service, want string }{
		{"read", "", appwire.ToolRead},
		{"find", "", appwire.ToolSearch},
		{"web_search", "", appwire.ToolWeb},
		{"web_fetch", "", appwire.ToolWeb},
		{"write", "", appwire.ToolEdit},
		{"bash", "", appwire.ToolRun},
		{"propose_task", "", appwire.ToolTask},
		{"gmail_search", "google", appwire.ToolConnector},
		{"use_service", "", appwire.ToolConnector},
		{"speak", "", appwire.ToolOther},
	} {
		if got := appToolKind(c.tool, c.service); got != c.want {
			t.Errorf("%s = %q, want %q", c.tool, got, c.want)
		}
	}
	if code := appExitOf("boom\n\nCommand exited with code 2"); code == nil || *code != 2 {
		t.Fatalf("the exit code = %v", code)
	}
}
