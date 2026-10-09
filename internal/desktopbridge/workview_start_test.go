package desktopbridge

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
)

// OPENING A CONVERSATION RECORDS ITS START COMMIT, once, before any diff is asked.
func TestOpeningAConversationRecordsItsDiffStart(t *testing.T) {
	starts := 0
	richFixture(t, func(c *Connection) {
		c.DiffStart = func() (remote.DiffStart, error) {
			starts++
			return remote.DiffStart{Git: true, Commit: "abc"}, nil
		}
	})
	if starts != 1 {
		t.Fatalf("start recorded %d times at open, want 1", starts)
	}
}
