package tui3

import (
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestCompactCommandReportsReductionAndExplainsProtectedHistory(t *testing.T) {
	for _, test := range []struct {
		message compactedMsg
		want    string
	}{
		{compactedMsg{before: 60000, after: 5000}, "compacted · about 60000 to 5000 tokens"},
		{compactedMsg{err: session.ErrNothingToCompact}, "nothing to compact — your messages and recent work are kept"},
		{compactedMsg{err: errors.New(session.ErrNothingToCompact.Error())}, "nothing to compact — your messages and recent work are kept"},
		// THE NO-OP SAYS WHY, from this engine and from a remote one whose
		// error arrives as its words alone.
		{compactedMsg{err: &session.NothingToCompact{Why: "only ~400 tokens since the last summary — too little to summarize"}},
			"nothing to compact — only ~400 tokens since the last summary — too little to summarize"},
		{compactedMsg{err: errors.New("session: nothing to compact: the summary was interrupted")},
			"nothing to compact — the summary was interrupted"},
	} {
		a := newTestApp(&fakeAgent{model: "m"})
		drive(t, a, test.message)
		if got := lastNote(t, a); !strings.Contains(got, test.want) {
			t.Fatalf("got %q, want %q", got, test.want)
		}
	}
}
