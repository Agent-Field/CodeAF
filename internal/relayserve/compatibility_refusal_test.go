package relayserve_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// R25 pins compatibility sentences separately from wire codes and presentation.
func TestR25CompatibilityRefusalSentences(t *testing.T) {
	for _, one := range []struct{ got, want string }{
		{cs.EnrollmentMessage(cs.UpdateRequired), "this codeaf build must be updated before it can join this cluster; your saved chats are still here"},
		{cs.LeadershipUnavailable, "this cluster cannot coordinate right now; reconnect enough of its voting computers and try again; your saved chats are still here"},
		{directory.ErrConditionalTake.Error(), "taking an open chat from another computer waits for a sync service update; all else works, so close it there and continue here"},
		{clusterctl.CommittedRefusal{Code: cs.Conflict, Sequence: 1}.Error(), "the leadership request could not complete; refresh cluster status and try again"},
	} {
		if one.got != one.want {
			t.Fatalf("compatibility refusal changed: %q", one.got)
		}
	}
}
