package factory

import "testing"

// THE SEEN REVISION ONLY MOVES FORWARD, AND IS EACH PERSON'S OWN. A lower
// revision (an older page that answered late) keeps the higher one, and the
// handle is read as GitHub reads it.
func TestSeeBriefIsMonotonicPerPerson(t *testing.T) {
	var it Item
	if BriefSeenBy(it, "alice") != 0 {
		t.Fatal("nobody has seen a brief yet")
	}
	for _, step := range []struct {
		who     string
		rev     int
		changed bool
		want    int
	}{
		{"@Alice", 3, true, 3},
		{"alice", 2, false, 3},
		{"alice", 3, false, 3},
		{" ALICE ", 5, true, 5},
	} {
		if got := SeeBrief(&it, step.who, step.rev); got != step.changed || BriefSeenBy(it, "alice") != step.want {
			t.Fatalf("%s saw %d: changed %v, seen %d; want %v, %d", step.who, step.rev, got, BriefSeenBy(it, "alice"), step.changed, step.want)
		}
	}
	if BriefSeenBy(it, "bob") != 0 || SeeBrief(&it, "", 9) || SeeBrief(&it, "bob", 0) {
		t.Fatalf("seen = %v", it.BriefSeen)
	}
}
