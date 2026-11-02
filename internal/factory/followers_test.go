package factory

import (
	"slices"
	"testing"
)

// FOLLOWING IS EACH PERSON'S OWN SWITCH. On twice is one follower, off takes
// only that person away, and the handle is read as GitHub reads it.
func TestSetFollowingIsPerPersonAndIdempotent(t *testing.T) {
	var it Item
	for _, step := range []struct {
		who     string
		on      bool
		changed bool
		want    []string
	}{
		{"@Alice", true, true, []string{"alice"}},
		{"alice", true, false, []string{"alice"}},
		{"bob", true, true, []string{"alice", "bob"}},
		{" ALICE ", false, true, []string{"bob"}},
		{"alice", false, false, []string{"bob"}},
		{"", true, false, []string{"bob"}},
	} {
		if got := SetFollowing(&it, step.who, step.on); got != step.changed || !slices.Equal(it.Followers, step.want) {
			t.Fatalf("%q on=%v: changed %v, followers %v; want %v, %v", step.who, step.on, got, it.Followers, step.changed, step.want)
		}
	}
	if IsFollowing(it, "alice") || !IsFollowing(it, "@BOB") || IsFollowing(it, "") {
		t.Fatalf("followers %v", it.Followers)
	}
}
