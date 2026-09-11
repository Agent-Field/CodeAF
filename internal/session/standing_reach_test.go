package session

// standing_reach_test.go is validator S25a scripted: an unattended firing that
// went looking for "any policy you have been given" read aforge's own home and
// another conversation's transcript with `read`, `ls` and `grep`, while the
// read-only folder verbs that answer that question were refused it.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// reachingOut is one response calling several reading hands at once.
func reachingOut(calls ...[2]string) step {
	return func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		response := toolResponse("call_0", calls[0][0], calls[0][1])
		for index, call := range calls[1:] {
			response.Choices[0].Message.ToolCalls = append(response.Choices[0].Message.ToolCalls, ai.ToolCall{
				ID: "call_" + string(rune('1'+index)), Type: "function", Function: ai.ToolCallFunction{Name: call[0], Arguments: call[1]},
			})
		}
		return response, nil
	}
}

// A FIRING READS ITS PROJECT AND NOTHING ELSE. Every reading hand aimed out of
// the project — by `..`, by an absolute path, or through a symlink planted
// inside it — is refused with one line naming both sides, and the run goes on
// to publish: a refusal nobody could lift is not a question for the person.
// What is inside the project reads as it always did.
func TestAnUnattendedReadOutsideItsProjectIsRefusedAndTheRunGoesOn(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	workspace := filepath.Join(t.TempDir(), "project")
	secret := "SECRET-FROM-ANOTHER-CONVERSATION"
	for path, text := range map[string]string{
		filepath.Join(outside, "transcript.jsonl"): secret + "\n",
		filepath.Join(workspace, "inbox", "a.md"):  "Decision: ship Friday\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "elsewhere")); err != nil {
		t.Fatal(err)
	}
	model := &scriptedCompleter{steps: []step{
		reachingOut(
			[2]string{"read", `{"path":"inbox/a.md"}`},
			[2]string{"read", `{"path":"` + filepath.Join(outside, "transcript.jsonl") + `"}`},
			[2]string{"read", `{"path":"elsewhere/transcript.jsonl"}`},
			[2]string{"ls", `{"path":".."}`},
			[2]string{"grep", `{"pattern":"SECRET","path":"` + outside + `"}`},
			[2]string{"find", `{"pattern":"*.jsonl","path":"../"}`},
		),
		saying("<report>\n# Inbox\n- Decision: ship Friday\n</report>"),
	}}
	runner, _ := grantedRunner(t, root, model, shippedFloorForTest())
	outcome, err := runner.Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != "landed" || outcome.NeedsPerson != "" || outcome.Published == nil {
		t.Fatalf("a refused outside read held the run: %+v", outcome)
	}
	if len(model.seen) < 2 {
		t.Fatalf("the run made %d requests", len(model.seen))
	}
	var results []string
	for _, message := range model.seen[1] {
		if message.Role == "tool" {
			results = append(results, messageText(message))
		}
	}
	joined := strings.Join(results, "\n---\n")
	if strings.Contains(joined, secret) {
		t.Fatalf("a firing read outside its project:\n%s", joined)
	}
	if !strings.Contains(joined, "Decision: ship Friday") {
		t.Fatalf("a read inside the project was refused:\n%s", joined)
	}
	if refused := strings.Count(joined, "is outside this work's project"); refused != 5 {
		t.Fatalf("%d of 5 outside reads were refused in the confinement's words:\n%s", refused, joined)
	}
}

// THE FOLDER VERBS' READS ARE HOW A FIRING LEARNS WHAT APPLIES TO IT, so they
// are carried and run unattended; before, every one was refused "nobody to
// ask" and the run came to needs-you.
func TestAnUnattendedRunMayReadItsFolders(t *testing.T) {
	root, project := t.TempDir(), t.TempDir()
	db := filepath.Join(t.TempDir(), "collections.db")
	store, err := workspace.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), "Maintenance"); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	model := &scriptedCompleter{steps: []step{
		reachingOut(
			[2]string{"collections", `{"action":"list"}`},
			[2]string{"shared_context", `{"action":"list"}`},
		),
		saying("<report>\n# Upgrades\n- NO POLICY\n</report>"),
	}}
	runner, _ := grantedRunner(t, root, model, shippedFloorForTest())
	runner.parent.Organization = &Organization{Path: db}
	outcome, err := runner.Run(context.Background(), reporting(project), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != "landed" || outcome.NeedsPerson != "" {
		t.Fatalf("reading the folders held the run: %+v", outcome)
	}
	if len(model.seen) < 2 || !strings.Contains(messageText(model.seen[1][len(model.seen[1])-2]), "Maintenance") {
		t.Fatalf("the folder list did not reach the run")
	}
}
