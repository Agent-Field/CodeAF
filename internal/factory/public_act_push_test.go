package factory

import (
	"strings"
	"testing"
)

// A PUSH THE MANAGER ASKS FOR IS HELD TO THE RULE ITS APPROVE IS HELD TO.
// Where Auto itself stopped for the person, the manager's own approve is
// refused ([ManagerMay]); its `push` act is the same approval by another
// tool, and leaves it waiting on the person with why. The person's own ask
// still presses it.
func TestPushRoadIsNotAWayRoundWhatAutoStoppedAt(t *testing.T) {
	failing := pushWaiting(actJob(AutonomyAuto))
	failing.Policy = []Claim{{Text: "no secret is committed", OK: false, Medium: "policy"}}
	unowned := pushWaiting(actJob(AutonomyAuto))
	unowned.Assignee, unowned.Starter = "", ""
	for name, it := range map[string]Item{"a failing finding": failing, "a job with no owner": unowned} {
		why := AutonomyBlockReason(it)
		if why == "" {
			t.Fatalf("%s: the fixture is not a job Auto stopped at", name)
		}
		if err := ManagerMay(it, ManagerApprove, false); err == nil {
			t.Fatalf("%s: the manager's own approve is taken", name)
		}
		road, words, err := PushRoad(it, ActByMainAgent)
		if err != nil || road != RoadWaiting || !strings.Contains(words, "Auto stopped here for the person. "+why) {
			t.Errorf("%s: the manager's push took road %q with %q (%v)", name, road, words, err)
		}
		if road, _, err := PushRoad(it, ActByYou); err != nil || road != RoadPress {
			t.Errorf("%s: the person's own push took road %q (%v)", name, road, err)
		}
	}
}
