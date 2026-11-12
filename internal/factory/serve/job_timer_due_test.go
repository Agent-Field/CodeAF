package serve

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A deadline that has already passed is looked at again in two seconds, so a
// settle that cannot move the job never re-arms the timer at once.
func TestTheJobTimerNeverRearmsAtOnceForAPastDeadline(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	past, soon := now.Add(-time.Minute), now.Add(500*time.Millisecond)
	for _, c := range []struct {
		name string
		it   factory.Item
		want time.Time
	}{
		{"refused merge past its undo", factory.Item{Merging: &factory.MergeRun{State: factory.MergeAsked, UndoUntil: past}}, now.Add(2 * time.Second)},
		{"approval past its undo", factory.Item{Approval: &factory.Approval{State: "approving", UndoUntil: past}}, now.Add(2 * time.Second)},
		{"recovery past its deadline", factory.Item{Recovery: &factory.StepRecovery{State: "queued", Deadline: past}}, now.Add(2 * time.Second)},
		{"merge still in its undo", factory.Item{Merging: &factory.MergeRun{State: factory.MergeAsked, UndoUntil: soon}}, soon},
		{"approval still in its undo", factory.Item{Approval: &factory.Approval{State: "approving", UndoUntil: soon}}, soon},
	} {
		if got := jobTimerDue([]factory.Item{c.it}, now); !got.Equal(c.want) {
			t.Errorf("%s: due %v, want %v", c.name, got.Sub(now), c.want.Sub(now))
		}
	}
	if got := jobTimerDue([]factory.Item{{}}, now); !got.IsZero() {
		t.Fatalf("a floor with no deadline arms the timer at %v", got)
	}
}
