package session

// standing_absent_test.go confirms, for round 3b, the chat door's ten-run
// measurement of 2026-09-11 against wave 4's grant-shaped belts. Three of its
// ten runs (01, 02, 08) reached for `bash` to read file sizes and times —
// `stat -c '%s %Y'`, `ls -la`, `find -exec stat` — were refused "nobody to
// ask", and the refusal parked a report the run had already finished as
// waiting on the person. Under the shipped floor `bash` is not granted, so it
// is not on the belt: the same calls are answered as a tool the run does not
// have, and a finished report publishes.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// callingMany is one response that says text and then calls every tool in
// calls at once, in order.
func callingMany(text string, calls ...[2]string) step {
	return func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		if text != "" {
			provider.Emit(ctx, provider.StreamDelta, text)
		}
		response := toolResponseWithText("call_0", calls[0][0], calls[0][1], text)
		for i, call := range calls[1:] {
			response.Choices[0].Message.ToolCalls = append(response.Choices[0].Message.ToolCalls, ai.ToolCall{
				ID: "call_" + string(rune('1'+i)), Type: "function", Function: ai.ToolCallFunction{Name: call[0], Arguments: call[1]},
			})
		}
		return response, nil
	}
}

func shellCall(command string) [2]string {
	args, _ := json.Marshal(map[string]string{"command": command})
	return [2]string{"bash", string(args)}
}

// THE CHAT DOOR'S RUN 01, UNDER WAVE 4. The run wrote its whole report, then
// reached for the shell four ways to read a size and a time it already had in
// its evidence, then said so. None of it is on the belt; nothing is refused as
// a question; the finished report publishes.
func TestAFinishedReportIsNotParkedByTheShellCallsAfterIt(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	report := "<report>\n# Inbox\n- inbox/today.md — 148 bytes, changed 2026-09-10 23:56\n</report>"
	model := &scriptedCompleter{steps: []step{
		callingMany(report,
			shellCall("stat -c '%s %Y' inbox/today.md"),
			shellCall("ls -la inbox/"),
			shellCall("find inbox -name '*.md' -exec stat {} \\;"),
			shellCall("pwd; ls -la"),
		),
		saying("I could not run stat here; the sizes and times above are from the check's evidence."),
	}}
	runner, built := grantedRunner(t, root, model, shippedFloorForTest())
	outcome, err := runner.Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	if beltNameSet((*built).beltTools())["bash"] {
		t.Fatal("the shipped floor put a shell on an unattended run's belt")
	}
	if outcome.Kind != "landed" || outcome.NeedsPerson != "" || outcome.Withheld != "" || outcome.Published == nil {
		t.Fatalf("a finished report was parked by shell calls after it: %+v", outcome)
	}
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if !strings.Contains(string(raw), "148 bytes") {
		t.Fatalf("published %q", raw)
	}
	for _, seen := range model.seen {
		for _, message := range seen {
			if strings.Contains(messageText(message), "nobody to ask") {
				t.Fatalf("an absent tool was refused as a question: %q", messageText(message))
			}
		}
	}
}

// EVERY VERB THE FLOOR DOES NOT GRANT, AT ONCE. Whatever an unattended run
// reaches for that its person never allowed — the shell, the writers, a
// commit, a question, a fork — is a tool it does not have, answered as one,
// and none of those answers is a question for the person.
func TestNoUngrantedCallParksAFinishedReport(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	write, _ := json.Marshal(map[string]string{"path": "notes.md", "content": "x"})
	edit, _ := json.Marshal(map[string]string{"path": "notes.md", "old": "x", "new": "y"})
	model := &scriptedCompleter{steps: []step{
		callingMany("<report>\n# Inbox\n- one change\n</report>",
			shellCall("ls -la"),
			[2]string{"write", string(write)},
			[2]string{"edit", string(edit)},
			[2]string{"commit", `{"id":"b1"}`},
			[2]string{"ask", `{"question":"may I?"}`},
			[2]string{"fork", `{"brief":"look around"}`},
		),
		saying("Done."),
	}}
	runner, _ := grantedRunner(t, root, model, shippedFloorForTest())
	outcome, err := runner.Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.NeedsPerson != "" || outcome.Withheld != "" || outcome.Published == nil {
		t.Fatalf("an ungranted call parked a finished report: %+v", outcome)
	}
}

// A TOOL ARMED AFTER THE BELT WAS BUILT IS STILL ON THE BELT. A connected
// account's family arrives through use_service, at the tail, after the grant
// filter ran over everything else (wave 4's known limit): in a firing, an
// ungranted family tool would be present and refused, which is the parked
// report again. The grant is asked of arriving tools too.
func TestAToolArmedIntoAnUnattendedRunIsAskedForItsGrantToo(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.InTask = true
		config.ApprovalPolicy = shippedFloorForTest()
	})
	family := []bare.Tool{
		{Name: "slack_post", Description: "post a message", Schema: json.RawMessage(`{"type":"object"}`)},
		{Name: "grep", Description: "already held", Schema: json.RawMessage(`{"type":"object"}`)},
	}
	armed, err := agent.armFamily(family)
	if err != nil {
		t.Fatal(err)
	}
	if beltNameSet(agent.beltTools())["slack_post"] || len(armed) != 0 {
		t.Fatalf("an ungranted family tool was armed into an unattended run: %v", armed)
	}
	if got := agent.grantedOnly(family); len(got) != 1 || got[0].Name != "grep" {
		t.Fatalf("grantedOnly kept %v", toolNames(got))
	}
}
