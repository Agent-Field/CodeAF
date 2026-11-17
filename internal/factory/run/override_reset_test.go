package run

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// AN OVERRIDE COVERS ONLY THE RUN IT WAS GIVEN FOR: the person tells Auto to
// carry on past a failing policy finding, then the job runs again (a rerun of
// a step, or a fresh launch after a failed publish) and lands on a new commit
// with the same finding failing. Auto pauses on it again instead of publishing.
func TestAnOverrideDoesNotCarryIntoTheNextRun(t *testing.T) {
	finding := factory.Claim{Text: "FINDING: secrets in config", Medium: "policy"}
	landed := func(head string) factory.Item {
		return factory.Item{ID: 7, Num: 7, Kind: factory.KindIssue, Autonomy: factory.AutonomyAuto, Assignee: "sam", Cap: 5,
			State: factory.StateLanded, HeadCommit: head,
			Proof:  []factory.Claim{{Text: "tests pass", OK: true}},
			Policy: []factory.Claim{finding},
			Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
				{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Result: &factory.StageResult{Done: true}},
				{Name: "write", Kind: factory.StageChat, State: factory.PhaseDone, Result: &factory.StageResult{Done: true}},
			}}}
	}
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	for name, again := range map[string]func(*factory.Item) error{
		"rerun": func(it *factory.Item) error { return StepControl(it, "write", true, "", now) },
		"launch": func(it *factory.Item) error {
			it.PublishStatus = &factory.Publishing{State: "failed"}
			return QueueItem(it, now)
		},
	} {
		t.Run(name, func(t *testing.T) {
			it := landed("1111111111111111111111111111111111111111")
			if _, err := factory.OverrideFindings(&it, "sam", "resume, that file is a fixture", now); err != nil {
				t.Fatal(err)
			}
			if why := factory.AutonomyBlockReason(it); why != "" {
				t.Fatalf("Auto still paused after the override: %q", why)
			}
			if err := again(&it); err != nil {
				t.Fatal(err)
			}
			if len(it.Overrides) != 0 {
				t.Fatalf("the new run kept the last run's overrides: %+v", it.Overrides)
			}
			// The new run lands on a new commit with the same finding failing.
			it.State, it.HeadCommit = factory.StateLanded, "2222222222222222222222222222222222222222"
			it.Proof, it.Policy = []factory.Claim{{Text: "tests pass", OK: true}}, []factory.Claim{finding}
			if why := factory.AutonomyBlockReason(it); !strings.HasPrefix(why, "Repository policy needs attention: FINDING: secrets in config") {
				t.Fatalf("Auto did not pause on the same finding in the new run: %q", why)
			}
			if !factory.OffersAutoResume(it) {
				t.Fatal("the new run's pause does not offer resume")
			}
		})
	}
}
