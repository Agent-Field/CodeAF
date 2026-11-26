package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
)

func TestMeetingCompletionStartsAnAppRunOnce(t *testing.T) {
	place := data.NewPlace(t.TempDir(), data.Options{NoRefresh: true})
	defer place.Close()
	store, err := place.Main()
	if err != nil {
		t.Fatal(err)
	}
	capture := data.MeetingCapture{
		ID: "9de2a4b686094ab98de0be752b121ee0", Title: "First meeting",
		Started: time.Unix(100, 0), Ended: time.Unix(200, 0), Transcript: "Speaker 1: Let's ship Thursday.",
	}
	if _, err := store.IngestMeetingCapture(capture); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterApp([]byte(`{"id":"copilot","records":{
		"meeting":{"common":"event","uses":true,"fields":{}},
		"document":{"common":"document","uses":true,"fields":{}}
	},"capabilities":{"records":{"meeting":"r","document":"r"}}}`)); err != nil {
		t.Fatal(err)
	}
	adapter := runs.Data{Store: place.Main}
	cfg := runs.Config{
		Root: t.TempDir(), RecordInput: adapter.RecordInput,
		Load: func(_, _ string) (runs.Definition, error) {
			return runs.Parse([]byte("run: follow-up\nsteps:\n  - id: end\n    wait: {for: 5m}\n"))
		},
		Triggers: func() ([]runs.Trigger, error) {
			return []runs.Trigger{{App: "copilot", ID: "meeting-ended", Run: "follow-up", On: "record.changed", Type: "meeting",
				When: "changed('capture_status') && capture_status == 'ended'"}}, nil
		},
	}
	engine, err := runs.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if err := adapter.ConsumeOutbox(t.Context(), engine); err != nil {
		t.Fatal(err)
	}
	capture.ID = "9de2a4b686094ab98de0be752b121ee1"
	capture.Title = "Customer review"
	kept, err := store.IngestMeetingCapture(capture)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.IngestMeetingCapture(capture); err != nil {
		t.Fatal(err)
	}
	if err := adapter.ConsumeOutbox(t.Context(), engine); err != nil {
		t.Fatal(err)
	}
	got, err := engine.List("copilot", "follow-up")
	if err != nil || len(got) != 1 {
		t.Fatalf("want exactly one completion run for %s: %v, %v", kept.Meeting, got, err)
	}
	if got[0].State == runs.Incomplete {
		t.Fatalf("the declared trigger could not start: %s", fmt.Sprint(got[0]))
	}
}

func TestMeetingNotesRequireTypedOutput(t *testing.T) {
	for _, text := range []string{`{"summary":"Missing the remaining fields"}`, `{"summary":"ok","decisions":[],"tasks":[{"title":"update","unexpected":"invented"}],"subject":"s","draft":"d"}`} {
		if _, err := decodeMeetingNotes(text); err == nil {
			t.Fatalf("accepted unexpected output: %s", text)
		}
	}
	notes, err := decodeMeetingNotes(`{"summary":"Review on Thursday.","decisions":["Review Thursday"],"tasks":[{"title":"Book the review"}],"subject":"Review","draft":"See you Thursday."}`)
	if err != nil || len(notes.Tasks) != 1 || notes.Tasks[0].Title != "Book the review" {
		t.Fatalf("typed result = %+v, %v", notes, err)
	}
}
