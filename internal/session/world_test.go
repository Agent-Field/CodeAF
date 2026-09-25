package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A ROLLUP'S NEWEST MOMENT IS A LANDING MOMENT. Live rows contribute to the
// running and incomplete counts exactly as the conversation's presence says,
// but none can supply a clock for work that has not ended.
func TestARollupOfOnlyRunningRowsHasNoNewestMoment(t *testing.T) {
	rows := []TaskIndexEntry{
		{ID: "1", Title: "read the tariff table", Status: string(TaskRunning)},
		{ID: "2", Title: "read the invoice writer", Status: string(TaskRunning)},
	}
	held := SessionRow{
		Live:     true,
		Presence: SessionPresence{RunningTasks: []PresenceTask{{ID: "1"}}},
	}
	got := rollUp(rows, held)
	if !got.Newest.IsZero() {
		t.Fatalf("running rows gave the rollup a newest landing at %s", got.Newest)
	}
	if got.Running != 1 || got.Incomplete != 1 {
		t.Fatalf("the rollup counts running/incomplete as %d/%d, want 1/1", got.Running, got.Incomplete)
	}
}

// ROWS BY NAME ARE THE WALK'S ROWS FOR THOSE NAMES AND NOTHING ELSE. Each named
// conversation reads as [ReadWorld] reads it; a folder nobody spoke in, a name
// that is not a session's journal and a folder that is not there are absent;
// and the conversation beside them that nobody named is never read.
func TestReadRowsReadsTheNamedConversationsAlone(t *testing.T) {
	root := t.TempDir()
	bucket := filepath.Join(root, "-work-alpha")
	spoke := time.Now().Add(-time.Hour).Truncate(time.Second)
	folder := func(id string, spoken bool) string {
		dir := filepath.Join(bucket, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		transcript := filepath.Join(dir, placeTranscript)
		if err := os.WriteFile(transcript, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		meta := Meta{ID: id, Title: "t " + id, Workspace: "/work/alpha", Created: spoke}
		if spoken {
			meta.LastUserAt = spoke
		}
		if err := SaveMeta(dir, meta); err != nil {
			t.Fatal(err)
		}
		return transcript
	}
	named := folder("aaaa000000000001", true)
	other := folder("aaaa000000000002", true)
	empty := folder("aaaa000000000003", false)
	gone := filepath.Join(bucket, "aaaa000000000004", placeTranscript)

	rows := ReadRows([]string{named, " " + named + " ", empty, gone, filepath.Join(bucket, "notes.txt")})
	if len(rows) != 1 {
		t.Fatalf("read %d rows, want the one named conversation: %+v", len(rows), rows)
	}
	got, ok := rows[named]
	if !ok {
		t.Fatalf("the named conversation is not keyed by its transcript: %+v", rows)
	}
	var want SessionRow
	for _, p := range ReadWorld(root).Projects {
		for _, r := range p.Sessions {
			if r.Transcript == named {
				want = r
			}
		}
	}
	if got.ID != want.ID || got.Title != want.Title || !got.At.Equal(want.At) || got.Open != want.Open || got.Live != want.Live || got.Dir != want.Dir {
		t.Fatalf("the row by name differs from the walk's:\n got %+v\nwant %+v", got, want)
	}
	if _, read := rows[other]; read {
		t.Fatal("a conversation nobody named was read")
	}
}
