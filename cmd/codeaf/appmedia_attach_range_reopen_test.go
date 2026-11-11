package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// STAGE, SEND THROUGH THE ORDINARY DOOR, REOPEN. A real conversation against a
// scripted local model keeps a recorded audio's span as typed data on the
// person's message, beside their exact words and client id; a NEW backend that
// shares nothing with the first reads the same span back, the model was sent
// the file as words and never as a picture, and an old plain message beside it
// shows no source.
func TestAppMediaAttachRangeSurvivesAReopenedRecord(t *testing.T) {
	root := resolvedTempDir(t)
	model, workspace := wakeEnvironment(t, root)
	index := filepath.Join(root, "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	t.Cleanup(func() { appMediaIndex = old })

	rec := newAppRecorder()
	first := newAppBackend(rec, appOpenInProcess)
	closed := false
	defer func() {
		if !closed {
			first.Close()
		}
	}()
	agent, err := first.CreateAgent(appwire.CreateAsk{Workspace: workspace, Text: "hello there", ClientID: "c-0"})
	if err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 30*time.Second, appTurnDone)
	agent.Workspace = workspace
	data := rangeWAVBytes(2000)
	source, version, original := recordRange(t, first, agent, "call.wav", "audio", data, -1)

	chip, err := first.AttachMediaRange(agent.ID, source, version, 400, 1600)
	if err != nil {
		t.Fatal(err)
	}
	words := "what does the speaker say here?"
	appTurnDoneFor(t, rec, appSendWithWhenIdle(t, first, agent.ID, words, "c-range", []string{chip.ID}))
	plain := "and an ordinary follow up"
	appTurnDoneFor(t, rec, appSendWithWhenIdle(t, first, agent.ID, plain, "c-plain", nil))
	transcript := appTranscriptOf(first, agent.ID)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true

	// The model was sent the file as words, once, and no picture.
	asked := false
	for _, request := range appModelRequests(model) {
		all := strings.Join(request.texts(), "\n")
		if strings.Contains(all, words+"\n\nAttached file: "+chip.Path) {
			asked = true
		}
		for _, message := range request.Messages {
			if strings.Contains(string(message.Content), "image_url") {
				t.Fatalf("an audio range reached the model as a picture: %.200s", message.Content)
			}
		}
	}
	if !asked {
		t.Fatal("the model was not sent the snapshot's path in the person's words")
	}

	second := newAppBackend(newAppRecorder(), appOpenInProcess)
	defer second.Close()
	entries, _, err := second.Entries(agent.ID, "", appwire.MaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	users := appUsersOf(entries)
	if len(users) != 3 {
		t.Fatalf("the reopened record holds %d messages of the person's: %+v", len(users), users)
	}
	ranged, ordinary := users[1], users[2]
	if ranged.Text != words || ranged.ClientID != "c-range" || len(ranged.Attachments) != 1 {
		t.Fatalf("the ranged message reopened as %+v", ranged)
	}
	got := ranged.Attachments[0]
	if got.Kind != appwire.AttachFile || got.Path != chip.Path || got.Title != "call.range-400-1600.wav" || got.Media == nil || got.Media.Source == nil {
		t.Fatalf("the attachment reopened as %+v", got)
	}
	src := got.Media.Source
	if src.SourceID != source || src.Version != version || src.Path != original || src.FromMS == nil || *src.FromMS != 400 || src.ToMS == nil || *src.ToMS != 1600 || src.AtMS != nil {
		t.Fatalf("the typed range reopened as %+v", src)
	}
	if ordinary.Text != plain || len(ordinary.Attachments) != 0 {
		t.Fatalf("the plain message reopened as %+v", ordinary)
	}

	// The record itself: the descriptor, the person's words and the id.
	var described []session.DisplayEntry
	for _, item := range session.ReadTranscript(transcript).Entries {
		if item.Role == "user" {
			described = append(described, item)
		}
	}
	person := described[1].Person
	if len(described) != 3 || person == nil || !person.Authored || described[1].Text != words || person.ClientID != "c-range" ||
		len(person.Attachments) != 1 || person.Attachments[0].Source == nil || person.Composed == nil || len(person.Composed.Media) != 0 {
		t.Fatalf("the record reads %+v", described)
	}
	if _, err := os.Stat(chip.Path); err != nil {
		t.Fatal("an accepted snapshot was deleted")
	}
	if kept, _ := os.ReadFile(original); string(kept) != string(data) {
		t.Fatal("the original changed")
	}
}
