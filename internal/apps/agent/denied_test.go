package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

// A permission ask mid-turn is refused at once: the turn finishes, the model
// is told it is not allowed here, and the person reads one line.
func TestAPermissionAskIsDeniedAtOnceAndExplainedInOneLine(t *testing.T) {
	tests := []struct {
		name      string
		replies   []string
		wantKinds string
		wantCalls int
		wantLine  string
	}{
		{"tool off the belt",
			[]string{`{"tool":"shell.run","args":{"cmd":"ls"}}`, `{"say":"I can't run shell commands in this app."}`},
			"tool,delta,done", 2, "I can't run shell commands in this app."},
		{"capability checker refuses",
			[]string{`{"tool":"records.query","args":{"type":"ghost"}}`, `{"say":"I can't read ghost records in this app."}`},
			"tool,delta,done", 2, "I can't read ghost records in this app."},
		{"model asks again after the refusal",
			[]string{`{"tool":"bash","args":{}}`, `{"tool":"files.write","args":{}}`, `{"say":"never asked"}`},
			"tool,tool,delta,done", 2, "I can't use files write in this app."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, tc.replies...)
			events := r.askWithin(t, "do it", time.Second)
			if got := strings.Join(toStrings(kinds(events)), ","); got != tc.wantKinds {
				t.Fatalf("events = %s, want %s", got, tc.wantKinds)
			}
			if len(r.model.calls) != tc.wantCalls {
				t.Fatalf("model calls = %d, want %d", len(r.model.calls), tc.wantCalls)
			}
			feedback := r.model.calls[1].Messages[len(r.model.calls[1].Messages)-1].Text
			if !strings.Contains(feedback, `"code":"denied"`) || !strings.Contains(feedback, deniedNote) {
				t.Fatalf("feedback = %s", feedback)
			}
			line := events[len(events)-2]
			if line.Text != tc.wantLine || strings.Contains(line.Text, "\n") {
				t.Fatalf("reply = %q, want one line %q", line.Text, tc.wantLine)
			}
		})
	}
}

// Connector approvals keep their own path: they open an approval and are
// never answered as a refusal.
func TestConnectorApprovalsAreNotTreatedAsPermissionAsks(t *testing.T) {
	r := newRig(t, `{"tool":"gmail.send","args":{"to":"ada@example.com"}}`, `{"say":"Waiting for your approval."}`)
	events := r.ask(t, "email Ada")
	if got := strings.Join(toStrings(kinds(events)), ","); got != "tool,approval,delta,done" {
		t.Fatalf("events = %s", got)
	}
	feedback := r.model.calls[1].Messages[len(r.model.calls[1].Messages)-1].Text
	if strings.Contains(feedback, deniedNote) || len(r.approvals.opened) != 1 {
		t.Fatalf("feedback = %s, approvals = %+v", feedback, r.approvals.opened)
	}
	if events[1].Kind != bridge.AgentApproval {
		t.Fatalf("approval event = %+v", events[1])
	}
}
