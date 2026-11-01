package session

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/knowledge"
)

// L4-H4b, L4-H4c: only a completed local drain and transcript close produce local durability.
func TestL4LocalDurabilityRequiresSuccessfulClose(t *testing.T) {
	for _, fail := range []bool{false, true} {
		place := newPlace(t, false)
		agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
			c.Place, c.SessionFile = place, place.Transcript()
			c.knowledgeReadVault = knowledge.Open(t.TempDir())
			var cause error
			if fail {
				cause = context.DeadlineExceeded
			}
			c.Seat = failedSavingSeat{Seat: executor.Host, err: cause}
		})
		if agent.LocalDurable() {
			t.Fatal("an open agent claimed its final local receipt")
		}
		err := agent.Close()
		if (err != nil) != fail || agent.LocalDurable() == fail {
			t.Fatalf("failure=%v local=%v err=%v", fail, agent.LocalDurable(), err)
		}
	}
}
