package data

import (
	"strings"
	"testing"
	"time"
)

func TestMeetingCapturePublishesTheTranscriptWithTheCompletion(t *testing.T) {
	place := NewPlace(t.TempDir(), Options{NoRefresh: true})
	defer place.Close()
	store, err := place.Main()
	if err != nil {
		t.Fatal(err)
	}
	// The first desk builds registered these common types before capture
	// identifiers and calendar event links were projected on the rows.
	previous := strings.NewReplacer(`"capture_id":{"type":"text"},`, "", `"calendar_event":{"type":"ref","to":"meeting"},`, "").Replace(meetingCaptureRecords)
	if err := store.RegisterApp([]byte(previous)); err != nil {
		t.Fatal(err)
	}
	in := MeetingCapture{
		ID:    "7ceaa4b686094ab98de0be752b121ee0",
		Title: "Asterloom planning", Started: time.Unix(100, 0), Ended: time.Unix(200, 0),
		Transcript:  "Speaker 1: The review is on Friday.",
		PersonNotes: "Review is Thursday.",
		Notes:       MeetingNotes{Summary: "The transcript says Friday.", Tasks: []MeetingTask{{Title: "Book the review"}}},
	}
	first, err := store.IngestMeetingCapture(in)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := store.IngestMeetingCapture(in)
	if err != nil || first.Meeting != retry.Meeting || first.Transcript != retry.Transcript {
		t.Fatalf("retry returned different record links: %#v %#v (%v)", first, retry, err)
	}
	meeting, err := store.Get(Scope{}, "meeting", first.Meeting, false)
	if err != nil || meeting["capture_status"] != "ended" || meeting["transcript"] != first.Transcript {
		t.Fatalf("completion lacks its transcript link: %#v (%v)", meeting, err)
	}
	meta := meeting["_meta"].(RowMeta)
	if meta.By["person_notes"] != ByPerson || meta.By["capture_summary"] != ByAI {
		t.Fatalf("person and AI fields have the wrong provenance: %+v", meta.By)
	}
	document, err := store.Get(Scope{}, "document", first.Transcript, false)
	if err != nil || document.ID() != first.Transcript {
		t.Fatalf("completion points to an unreadable document: %#v (%v)", document, err)
	}
	typ, err := store.Type(Scope{}, "document")
	if err != nil || typ.Fields["body"].Class != "pii" {
		t.Fatalf("the transcript must carry the PII class: %v", err)
	}
}

func TestMeetingCapturePromptsOnlyForStartingConferences(t *testing.T) {
	now := time.Date(2026, time.November, 26, 16, 0, 0, 0, time.UTC)
	place := NewPlace(t.TempDir(), Options{NoRefresh: true, Now: func() time.Time { return now }})
	defer place.Close()
	s, err := place.Main()
	if err != nil {
		t.Fatal(err)
	}
	const def = `{"id":"calendar","records":{"meeting":{"common":"event","fields":{
		"title":{"type":"text"},"starts_at":{"type":"datetime"},"ends_at":{"type":"datetime"},"conference":{"type":"url"}
	}}},"capabilities":{"records":{"meeting":"rw"}}}`
	if err := s.RegisterApp([]byte(def)); err != nil {
		t.Fatal(err)
	}
	ending := now.Add(30 * time.Minute).Format(time.RFC3339)
	created := mustCreate(t, s, Scope{}, "meeting",
		map[string]any{"title": "Starting call", "starts_at": now.Format(time.RFC3339), "ends_at": ending, "conference": "https://meet.example/call"},
		map[string]any{"title": "No link", "starts_at": now.Format(time.RFC3339), "ends_at": ending},
		map[string]any{"title": "Later", "starts_at": now.Add(10 * time.Minute).Format(time.RFC3339), "ends_at": ending, "conference": "https://meet.example/later"},
	)
	rows, err := s.MeetingsStarting(now)
	if err != nil || len(rows) != 1 || rows[0].ID != created[0].ID() {
		t.Fatalf("starting meetings: %+v (%v)", rows, err)
	}
}
