package automation

import (
	"context"
	"testing"
)

func madeIn(title string, team *OriginTeam) Automation {
	a := routine("15m")
	a.Title = title
	a.Origin = Origin{SessionID: "s", Transcript: "t", Team: team}
	return a
}

// THE READ IS EXACT ON THE RECORDED TEAM, PAGED, AND SAYS HOW MANY ROWS NEVER
// HAD ONE — across the store being closed and opened again.
func TestMadeInTeamReadsTheRecordedTeamPagedAndCountsUnknown(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	var wantA []string
	for _, row := range []struct {
		title string
		team  *OriginTeam
	}{
		{"legacy", nil},
		{"none", &OriginTeam{Known: true}},
		{"a1", &OriginTeam{Known: true, ID: "teamA"}},
		{"a2", &OriginTeam{Known: true, ID: "teamA"}},
		{"a3", &OriginTeam{Known: true, ID: "teamA"}},
		{"b1", &OriginTeam{Known: true, ID: "teamB"}},
		{"forbidden", &OriginTeam{ID: "teamA"}},
	} {
		made, err := s.Create(madeIn(row.title, row.team))
		if err != nil {
			t.Fatal(err)
		}
		if row.title[0] == 'a' {
			wantA = append(wantA, made.ID)
		}
	}
	// A document nobody can read is neither a team's nor silently ignored.
	if _, err := s.db.Exec(`INSERT INTO automations (id, doc, status) VALUES ('zz-damaged', '{not json', 'active')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()

	first, more, err := s.MadeInTeam(ctx, "teamA", "", 2)
	if err != nil || !more || len(first) != 2 {
		t.Fatalf("first page = %d rows, more %v, %v", len(first), more, err)
	}
	second, more, err := s.MadeInTeam(ctx, "teamA", first[1].ID, 2)
	if err != nil || more || len(second) != 1 {
		t.Fatalf("second page = %d rows, more %v, %v", len(second), more, err)
	}
	var got []string
	for _, a := range append(first, second...) {
		got = append(got, a.ID)
		if a.Origin.Team == nil || a.Origin.Team.ID != "teamA" {
			t.Fatalf("a row from team %+v came back for teamA", a.Origin.Team)
		}
	}
	if len(got) != 3 || got[0] >= got[1] || got[1] >= got[2] {
		t.Fatalf("rows %v are not three ascending ids; want the ids of %v", got, wantA)
	}
	if none, _, err := s.MadeInTeam(ctx, "teamC", "", 10); err != nil || len(none) != 0 {
		t.Fatalf("an unused team read %d rows, %v", len(none), err)
	}
	// legacy, the forbidden not-known row and the damaged document; NONE is a
	// known fact and is not unknown.
	if n, err := s.TeamUnknownCount(ctx); err != nil || n != 3 {
		t.Fatalf("unknown count = %d, %v; want 3", n, err)
	}
	for _, bad := range []int{0, MadeInTeamMax + 1} {
		if _, _, err := s.MadeInTeam(ctx, "teamA", "", bad); err == nil {
			t.Fatalf("limit %d was served", bad)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := s.MadeInTeam(cancelled, "teamA", "", 5); err == nil {
		t.Fatal("a cancelled read answered")
	}
}
