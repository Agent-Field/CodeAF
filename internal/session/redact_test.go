package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// theShapeOfAToken is GitHub's own documented example: the right prefix, the
// right thirty-six characters, and nobody's credential.
const theShapeOfAToken = "gho_16C7e42F292c6912E7710c838347Ae178B4a"

// A token that comes back from a real shell call is gone from all three copies
// a tool result makes — the transcript the model reads, the row the surface
// draws, and the journal on disk — and it is gone because it passed the ONE
// chokepoint, not because this test called the redactor itself.
//
// This is the incident that put internal/redact here, run end to end: a worker
// asked a program for a credential, and the credential was in two task journals
// in plain text afterwards.
func TestATokenInAToolResultIsGoneFromEveryCopyOfIt(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AFORGE_HOME", root)
	journal := filepath.Join(root, "session.jsonl")

	// THE TOKEN IS NEVER IN THE COMMAND. It is on the disk and the shell reads
	// it, which is exactly how it reached the journal in the incident — a
	// credential is something a call FINDS, not something the model types — and
	// it is what lets the assertion below say the whole file is clean rather
	// than counting occurrences.
	secret := filepath.Join(root, "token.txt")
	if err := os.WriteFile(secret, []byte(theShapeOfAToken+"\n"), 0o600); err != nil {
		t.Fatalf("write the fixture: %v", err)
	}

	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "bash", `{"command":"cat `+secret+`"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("read it"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = journal
	})

	collected := collect(t, mustSubmit(t, agent, "print the token file"))

	// ── what the model reads ──
	agent.mu.Lock()
	var toolText string
	for _, message := range agent.messages {
		if message.Role == "tool" && message.ToolCallID == "call-1" {
			toolText = messageContentText(message)
		}
	}
	agent.mu.Unlock()
	if toolText == "" {
		t.Fatalf("no tool result reached the transcript; roles were %v", transcriptRoles(agent))
	}
	if strings.Contains(toolText, theShapeOfAToken) {
		t.Fatalf("the model was sent the token itself")
	}
	if !strings.Contains(toolText, "[redacted token · gho_…]") {
		t.Fatalf("the model should read the marker instead:\n%q", toolText)
	}

	// ── what the surface draws ──
	var drew bool
	for _, event := range collected {
		if event.Kind != EventToolEnd && event.Kind != EventToolFailed {
			continue
		}
		if strings.Contains(event.Output, theShapeOfAToken) {
			t.Fatalf("the token was sent to the surface on a %v event", event.Kind)
		}
		if strings.Contains(event.Output, "[redacted token · gho_…]") {
			drew = true
		}
	}
	if !drew {
		t.Fatalf("no tool event carried the redacted output; events were %v", kinds(collected))
	}

	// ── what is kept on disk ──
	//
	// The whole file, not one line of it: a journal that holds the token
	// ANYWHERE is a journal somebody can grep, and which record leaked it is a
	// detail the person whose token it was does not care about.
	written, err := os.ReadFile(journal)
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	if strings.Contains(string(written), theShapeOfAToken) {
		t.Fatalf("the token was written to the journal at %s", journal)
	}
	if !strings.Contains(string(written), "[redacted token") {
		t.Fatalf("the journal kept the result but not the marker:\n%s", written)
	}
}

// And a token the MODEL typed into a command never becomes a stored fix. The
// patch a fix store keeps is the one string in this package that outlives the
// session it was learned in, so it gets the same treatment as a result even
// though it never passes the result's door.
func TestAStoredFixNeverKeepsAToken(t *testing.T) {
	patch := `curl -H "Authorization: Bearer ` + theShapeOfAToken + `" https://api.github.com/user`
	got := fixCleanPatch(patch)
	if strings.Contains(got, theShapeOfAToken) {
		t.Fatalf("the store would have kept the token: %q", got)
	}
	if !strings.Contains(got, "[redacted token · gho_…]") {
		t.Fatalf("the patch should still read as a command with a marker in it: %q", got)
	}
}
