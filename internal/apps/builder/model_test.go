package builder

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestErrorsAreLabelledWithWhoAsked(t *testing.T) {
	tests := []struct{ role, want string }{{"", "builder"}, {"app agent", "app agent"}}
	for _, tc := range tests {
		if got := roleOf(svc.ModelCall{Role: tc.role}); got != tc.want {
			t.Errorf("roleOf(%q) = %q, want %q", tc.role, got, tc.want)
		}
	}
}

func TestTheReplySchemaReachesTheModel(t *testing.T) {
	call := svc.ModelCall{System: "Reply with JSON only.", Schema: []byte(`{"type":"object","required":["message","warn"]}`),
		Messages: []svc.ModelMessage{{Role: "user", Text: "Draft the reminder."}}}
	system := messagesOf(call)[0].Content[0].Text
	for _, want := range []string{`"required":["message","warn"]`, "every one of these keys: message, warn."} {
		if !strings.Contains(system, want) {
			t.Errorf("system message lacks %q:\n%s", want, system)
		}
	}
	if got := messagesOf(svc.ModelCall{Messages: call.Messages}); len(got) != 1 {
		t.Errorf("a call without system or schema sends %d messages, want 1", len(got))
	}
}
