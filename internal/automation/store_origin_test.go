package automation

import (
	"context"
	"fmt"
	"testing"
)

func madeIn(title string, team *OriginTeam) Automation {
	a := routine("15m")
	// The id is set so id order is title order.
	a.ID, a.Title = "m-"+title, title
	a.Origin = Origin{SessionID: "s", Transcript: "t", Team: team}
	return a
}

// walkMadeIn reads every page for teamID with limit, and answers the rows'
// titles, the summed unknown count and how many pages it took.
func walkMadeIn(t *testing.T, s *Store, teamID string, limit int) ([]string, int, int) {
	t.Helper()
	var titles []string
	seen := map[string]bool{}
	unknown, pages, after := 0, 0, ""
	for {
		page, err := s.MadeInTeam(context.Background(), teamID, after, limit)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if len(page.Rows) > limit {
			t.Fatalf("a page of %d rows over limit %d", len(page.Rows), limit)
		}
		for _, a := range page.Rows {
			if seen[a.ID] {
				t.Fatalf("%s came twice", a.ID)
			}
			seen[a.ID] = true
			if a.Origin.Team == nil || a.Origin.Team.ID != teamID {
				t.Fatalf("a row from team %+v came back for %s", a.Origin.Team, teamID)
			}
			titles = append(titles, a.Title)
		}
		unknown += page.Unknown
		if page.Next == "" {
			return titles, unknown, pages
		}
		if page.Next <= after {
			t.Fatalf("the cursor went from %q back to %q", after, page.Next)
		}
		after = page.Next
		if pages > 50 {
			t.Fatal("the walk never ended")
		}
	}
}

// THE READ IS EXACT ON THE RECORDED TEAM, AND A WALK SAYS HOW MANY ROWS NEVER
// HAD ONE — legacy, never marked known, not JSON, or not decodable — once each,
// across the store being closed and opened again. Known none is not unknown.
func TestMadeInTeamReadsTheRecordedTeamAndCountsUnknownOncePerWalk(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
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
		if _, err := s.Create(madeIn(row.title, row.team)); err != nil {
			t.Fatal(err)
		}
	}
	// A document nobody can read, and one that is JSON naming another team but
	// that this build cannot decode, are neither a team's nor silently ignored.
	for id, doc := range map[string]string{
		"zz-damaged":   `{not json`,
		"zz-undecoded": `{"title":5,"origin":{"team":{"known":true,"id":"teamB"}}}`,
	} {
		if _, err := s.db.Exec(`INSERT INTO automations (id, doc, status) VALUES (?, ?, 'active')`, id, doc); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	for _, limit := range []int{1, 2, MadeInTeamMax} {
		titles, unknown, _ := walkMadeIn(t, s, "teamA", limit)
		if fmt.Sprint(titles) != "[a1 a2 a3]" {
			t.Fatalf("limit %d: rows %v, want a1 a2 a3 in id order", limit, titles)
		}
		if unknown != 4 {
			t.Fatalf("limit %d: unknown summed to %d, want 4 (legacy, not known, not JSON, not decodable)", limit, unknown)
		}
	}
	if titles, unknown, _ := walkMadeIn(t, s, "teamC", 10); len(titles) != 0 || unknown != 4 {
		t.Fatalf("an unused team read %v with unknown %d", titles, unknown)
	}
	for _, bad := range []int{0, MadeInTeamMax + 1} {
		if _, err := s.MadeInTeam(context.Background(), "teamA", "", bad); err == nil {
			t.Fatalf("limit %d was served", bad)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.MadeInTeam(cancelled, "teamA", "", 5); err == nil {
		t.Fatal("a cancelled read answered")
	}
}

// ONE PAGE READS AT MOST [MadeInScanMax] STORED ROWS, however few match: a store
// of other teams' rows answers an empty page that says where it stopped, and
// the walk still finds the one row after them.
func TestMadeInTeamReadsABoundedWindowOfTheStore(t *testing.T) {
	s, _ := openStore(t)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := range MadeInScanMax + 5 {
		if _, err := tx.Exec(`INSERT INTO automations (id, doc, status) VALUES (?, ?, 'active')`, fmt.Sprintf("r%04d", i), `{"title":"other","origin":{"team":{"known":true,"id":"teamB"}}}`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO automations (id, doc, status) VALUES ('r9999', '{"title":"mine","origin":{"team":{"known":true,"id":"teamA"}}}', 'active')`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	first, err := s.MadeInTeam(context.Background(), "teamA", "", MadeInTeamMax)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Rows) != 0 || first.Next != fmt.Sprintf("r%04d", MadeInScanMax-1) || first.Unknown != 0 {
		t.Fatalf("first page = %d rows, next %q, unknown %d; want none, stopping at the window", len(first.Rows), first.Next, first.Unknown)
	}
	titles, unknown, pages := walkMadeIn(t, s, "teamA", MadeInTeamMax)
	if fmt.Sprint(titles) != "[mine]" || unknown != 0 || pages != 2 {
		t.Fatalf("the walk read %v, unknown %d, over %d pages", titles, unknown, pages)
	}
}

// A ROW THAT SAYS IT WAS MADE IN THE TEAM BUT CANNOT BE DECODED REFUSES THE
// PAGE: an empty or shorter list would be a list missing one of the team's own.
func TestMadeInTeamRefusesAMatchingRowItCannotRead(t *testing.T) {
	s, _ := openStore(t)
	if _, err := s.db.Exec(`INSERT INTO automations (id, doc, status) VALUES ('x1', '{"title":5,"origin":{"team":{"known":true,"id":"teamC"}}}', 'active')`); err != nil {
		t.Fatal(err)
	}
	if page, err := s.MadeInTeam(context.Background(), "teamC", "", 10); err == nil {
		t.Fatalf("an unreadable row of the team was answered as %+v", page)
	}
	if page, err := s.MadeInTeam(context.Background(), "teamA", "", 10); err != nil || page.Unknown != 1 || page.Next != "" {
		t.Fatalf("another team's read = %+v, %v; want the row counted unknown", page, err)
	}
}
