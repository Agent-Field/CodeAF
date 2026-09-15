package session

import (
	"path/filepath"
	"testing"
	"time"
)

// WHAT WAS MADE WHILE A PERSON WAS AWAY is the files that landed after their
// last look, newest first — and nothing at all on a machine with no look yet,
// because the first look marks nothing as news (look.go).
func TestArtifactsSinceKeepsOnlyWhatLandedAfterTheLook(t *testing.T) {
	now := time.Now()
	path := filepath.Join(t.TempDir(), ArtifactsIndexName)
	for _, made := range []Artifact{
		{Path: "/work/reports/fleet.md", Title: "Fleet audit", Created: now.Add(-3 * time.Hour)},
		{Path: "/work/reports/apartments-minto-street.md", Title: "Apartments near Minto", Created: now.Add(-1 * time.Hour)},
		{Path: "/work/pricing.png", Title: "Pricing chart", Created: now.Add(-10 * time.Minute)},
	} {
		RecordArtifact(path, made)
	}

	since := ArtifactsSince(path, now.Add(-2*time.Hour))
	if len(since) != 2 || since[0].Title != "Pricing chart" || since[1].Title != "Apartments near Minto" {
		t.Fatalf("since the look reads %+v, want the two newer files, newest first", since)
	}
	if never := ArtifactsSince(path, time.Time{}); never != nil {
		t.Fatalf("a machine with no look called %d files news", len(never))
	}
}

// WHAT LANDED WHILE A PERSON WAS AWAY is every finished row across every
// project, newest first, each with the conversation that ran it. Work still
// running or queued is `running`'s to draw, and work that landed before the
// look has been seen.
func TestLandedSinceIsTheWorkThatFinishedWhileYouWereAway(t *testing.T) {
	look := time.Now().Add(-12 * time.Hour)
	world := &World{Projects: []Project{
		{Name: "codeaf", Sessions: []SessionRow{{ID: "aaaa1111aaaa1111", Title: "Spark fleet", Tasks: TaskRollup{Rows: []TaskIndexEntry{
			{ID: "1", Title: "Spark fleet ssh audit", Status: string(TaskDone), EndedAt: look.Add(2 * time.Hour)},
			{ID: "2", Title: "Still going", Status: string(TaskRunning)},
			{ID: "3", Title: "Seen yesterday", Status: string(TaskDone), EndedAt: look.Add(-time.Hour)},
			{ID: "4", Title: "Not started", Status: string(TaskQueued)},
		}}}}},
		{Name: "pricing-site", Sessions: []SessionRow{{ID: "bbbb2222bbbb2222", Title: "Pricing", Tasks: TaskRollup{Rows: []TaskIndexEntry{
			{ID: "1", Title: "Rebuild the pricing table", Status: string(TaskFailed), EndedAt: look.Add(5 * time.Hour)},
		}}}}},
	}}

	landed := LandedSince(world, look)
	if len(landed) != 2 {
		t.Fatalf("landed since the look reads %+v, want the two that finished after it", landed)
	}
	if landed[0].Entry.Title != "Rebuild the pricing table" || landed[0].Session.ID != "bbbb2222bbbb2222" {
		t.Fatalf("the newest landing reads %+v", landed[0])
	}
	if landed[1].Entry.Title != "Spark fleet ssh audit" || landed[1].Session.Title != "Spark fleet" {
		t.Fatalf("the older landing reads %+v", landed[1])
	}
	if never := LandedSince(world, time.Time{}); never != nil {
		t.Fatalf("a machine with no look called %d tasks news", len(never))
	}
	if none := LandedSince(nil, look); none != nil {
		t.Fatal("no world answered something")
	}
}
