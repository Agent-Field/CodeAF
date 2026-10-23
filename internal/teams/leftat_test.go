package teams

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A removal is the end of a membership: [File.RemoveMember] stamps LeftAt
// while keeping JoinedAt, so the team's total ([TeamTotal]) keeps the member's
// spend while they were here and nothing after. SetManager and SetHome never
// touch the field, because a replaced manager or a moved home is still a
// member.
func TestRemoveMemberStampsLeftAt(t *testing.T) {
	joined := time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)
	f := managed()
	f.Teams[0].Members = append(f.Teams[0].Members, Member{Key: "k", JoinedAt: joined})
	f.Teams[3].Members = append(f.Teams[3].Members, Member{Key: "k", Started: true})
	tidy(f.Teams)
	if err := f.RemoveMember("aaaaaaaaaaaa", "k"); err != nil {
		t.Fatal(err)
	}
	harbor := f.Teams[0]
	if len(harbor.Members) != len(managed().Teams[0].Members) || len(harbor.FormerMembers) != 1 {
		t.Fatalf("roster: %+v", harbor)
	}
	former := harbor.FormerMembers[0]
	if !former.JoinedAt.Equal(joined) || former.LeftAt.IsZero() || !former.LeftAt.After(joined) {
		t.Fatalf("former: %+v", former)
	}
	left := former.LeftAt
	if err := f.SetManager("aaaaaaaaaaaa", "lead"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetHome("k", "dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	if after := f.Teams[0].FormerMembers[0].LeftAt; !after.Equal(left) {
		t.Fatalf("SetManager/SetHome moved LeftAt: %v", after)
	}
}

// LeftAt survives the wire and unknown fields do too: an old build that never
// heard of left_at keeps reading new files, and a new build keeps a removed
// member's boundary after an old build rewrote the file.
func TestMemberLeftAtRoundTripKeepsUnknownFields(t *testing.T) {
	left := time.Date(2026, 9, 24, 13, 0, 0, 0, time.UTC)
	var member Member
	raw := `{"key":"k","joined_at":"2026-09-24T10:00:00Z","left_at":"2026-09-24T13:00:00Z","future_authority":{"mode":"explicit"}}`
	must(t, json.Unmarshal([]byte(raw), &member))
	if !member.LeftAt.Equal(left) {
		t.Fatalf("left_at: %v", member.LeftAt)
	}
	data, err := json.Marshal(member)
	must(t, err)
	if !strings.Contains(string(data), `"future_authority":{"mode":"explicit"}`) || !strings.Contains(string(data), "left_at") {
		t.Fatalf("round trip: %s", data)
	}
	var back Member
	must(t, json.Unmarshal(data, &back))
	if !back.LeftAt.Equal(left) {
		t.Fatalf("left_at lost: %v", back.LeftAt)
	}
	// A member without a removal leaves no field at all, so an old build
	// writes nothing new for an open membership.
	var open Member
	data, err = json.Marshal(open)
	must(t, err)
	if strings.Contains(string(data), "left_at") {
		t.Fatalf("open membership wrote left_at: %s", data)
	}
}
