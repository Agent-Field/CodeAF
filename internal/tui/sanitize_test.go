package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// TestPollSanitizesHostileMessageBody is the end-to-end proof the build
// brief asked for: a message carrying an OSC 52 clipboard-write payload,
// read through the real poll() -> applyPoll path (not the sanitize package
// in isolation), lands in the model's thread as its visible text only. The
// hostile bytes never reach m.messages, so nothing downstream — the
// transcript renderer, the clipboard copy, the card fold — ever sees them.
func TestPollSanitizesHostileMessageBody(t *testing.T) {
	hostile := "here's the summary\x1b]52;c;ZXZpbCBwYXlsb2Fk\x07 done"
	backend := &fakeBackend{messages: []store.Message{{
		Seq: 1, Time: time.Now(), SessionID: "sanitize-poll",
		Role: store.RoleAgent, Body: hostile,
	}}}
	model := New(backend, "sanitize-poll")
	model.applyPoll(model.poll()().(pollResultMsg))

	if len(model.messages) != 1 {
		t.Fatalf("expected exactly one message, got %d", len(model.messages))
	}
	got := model.messages[0].Body
	if strings.Contains(got, "\x1b") {
		t.Fatalf("hostile ESC sequence survived the poll seam: %q", got)
	}
	want := "here's the summary done"
	if got != want {
		t.Fatalf("message body after poll = %q, want %q", got, want)
	}

	// The transcript render must not be able to resurface the escape
	// either — proving the sanitize happened before rendering, not that
	// some later renderer happens to also be safe.
	transcript := model.renderMessages()
	if strings.Contains(transcript, "\x1b]52") {
		t.Fatalf("rendered transcript still carries the OSC 52 sequence: %q", transcript)
	}
}

// TestPollLeavesBenignMessageBodyUnchanged proves the flip side of the same
// seam: ordinary text and normal SGR styling survive the poll -> applyPoll
// path byte-for-byte. The old TUI's whole benign corpus must render
// identically after this wave, not just avoid executing hostile bytes.
func TestPollLeavesBenignMessageBodyUnchanged(t *testing.T) {
	benign := "an ordinary reply with \x1b[1mstyled\x1b[0m text and a café emoji 🎉"
	backend := &fakeBackend{messages: []store.Message{{
		Seq: 1, Time: time.Now(), SessionID: "sanitize-poll-benign",
		Role: store.RoleAgent, Body: benign,
	}}}
	model := New(backend, "sanitize-poll-benign")
	model.applyPoll(model.poll()().(pollResultMsg))

	if len(model.messages) != 1 {
		t.Fatalf("expected exactly one message, got %d", len(model.messages))
	}
	if got := model.messages[0].Body; got != benign {
		t.Fatalf("benign message body changed by the poll seam: got %q, want %q", got, benign)
	}
}

// TestPollSanitizesQuestionText proves the second store type this
// chokepoint covers: an AgentQuestion's free-text Text field, drawn
// directly by the question dock rather than through a store.Message.
func TestPollSanitizesQuestionText(t *testing.T) {
	hostile := "rename the file to \x1b]0;pwned\x07evil.txt?"
	backend := &fakeBackend{agentQuestions: []store.AgentQuestion{{
		Seq: 1, SessionID: "sanitize-poll-question", Text: hostile,
		Urgency: store.QuestionWhenever, Status: store.QuestionPending,
	}}}
	model := New(backend, "sanitize-poll-question")
	model.applyPoll(model.poll()().(pollResultMsg))

	if len(model.agentQuestions) != 1 {
		t.Fatalf("expected exactly one open question, got %d", len(model.agentQuestions))
	}
	got := model.agentQuestions[0].Text
	if strings.Contains(got, "\x1b") {
		t.Fatalf("hostile ESC sequence survived into the question dock: %q", got)
	}
	want := "rename the file to evil.txt?"
	if got != want {
		t.Fatalf("question text after poll = %q, want %q", got, want)
	}
}
