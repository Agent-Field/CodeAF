package session

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/buildinfo"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Everything one conversation writes lands in one folder, and the project's
// task index lands in the directory ABOVE it — the bucket every window on the
// repository shares (docs/CHAT-V3.md, Decision 26).
func TestTheSidecarsFollowTheSessionFolder(t *testing.T) {
	bucket := filepath.Join(t.TempDir(), "projects", "-home-p-code")
	dir := filepath.Join(bucket, "0123456789abcdef")
	place := Place{Dir: dir, Workspace: "/home/p/code"}
	config := Config{Workspace: place.Workspace, SessionFile: place.Transcript(), Place: place}

	for _, row := range []struct{ what, got, want string }{
		{"the state file", config.stateFile(), filepath.Join(dir, "state.json")},
		{"the checkpoint", config.checkpointFile(), filepath.Join(dir, "tasks.json")},
		{"the task index", config.taskIndexFile(), filepath.Join(bucket, "tasks.jsonl")},
	} {
		if row.got != row.want {
			t.Fatalf("%s is %q, want %q", row.what, row.got, row.want)
		}
	}
}

// A caller holding only the path gets the same answer as one holding the
// folder: the journal that is called transcript.jsonl is a session folder's by
// construction, and nothing else can be.
func TestTheDerivationsRecogniseAFolderFromThePathAlone(t *testing.T) {
	transcript := "/home/p/.aforge/v3/projects/-home-p-code/0123456789abcdef/transcript.jsonl"
	folder := filepath.Dir(transcript)
	if got, want := statePath(transcript), filepath.Join(folder, "state.json"); got != want {
		t.Fatalf("statePath = %q, want %q", got, want)
	}
	if got, want := taskCheckpointPath(transcript), filepath.Join(folder, "tasks.json"); got != want {
		t.Fatalf("taskCheckpointPath = %q, want %q", got, want)
	}
	if got, want := TaskIndexPath(transcript), filepath.Join(filepath.Dir(folder), "tasks.jsonl"); got != want {
		t.Fatalf("TaskIndexPath = %q, want %q", got, want)
	}
}

// The legacy flat layout is what the zero Place spells, and it derives exactly
// what it always did — a session written before the folder existed opens as
// itself.
func TestTheZeroPlaceKeepsTheFlatDerivation(t *testing.T) {
	config := Config{SessionFile: "/home/p/.aforge/v3/sessions/-w/20260815-101112_ab12cd.jsonl"}
	if got, want := config.stateFile(), "/home/p/.aforge/v3/sessions/-w/20260815-101112_ab12cd.state.json"; got != want {
		t.Fatalf("the state file is %q, want %q", got, want)
	}
	if got, want := config.checkpointFile(), "/home/p/.aforge/v3/sessions/-w/20260815-101112_ab12cd.tasks.json"; got != want {
		t.Fatalf("the checkpoint is %q, want %q", got, want)
	}
	if got, want := config.taskIndexFile(), "/home/p/.aforge/v3/sessions/-w/tasks.jsonl"; got != want {
		t.Fatalf("the task index is %q, want %q", got, want)
	}
}

// The folder is named by the session, so the journal's header repeats that name
// rather than minting a second one: a picker reading the directory and a reader
// opening the file have to agree about which conversation this is.
func TestTheHeaderTakesTheFoldersName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	place := Place{Dir: dir, Workspace: t.TempDir()}
	journal, _, err := openSessionFile(place.Transcript(), place.Workspace, "test/model", place.ID())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = journal.Close() }()
	if got, want := journal.ID(), "0123456789abcdef"; got != want {
		t.Fatalf("the header names the session %q, want the folder's name %q", got, want)
	}
}

// Resume order is on when the PERSON last spoke, so the person speaking is what
// writes it down — and the folder learns the conversation's opening words at
// the same moment, so a picker has a row to draw before the session has earned
// a name of its own.
func TestASubmissionStampsTheFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	place := Place{Dir: dir}
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("noted"), nil },
		// The namer's call, refused: an unnamed session keeps the placeholder,
		// which is the state this test is about.
		func(context.Context, []ai.Message) (*ai.Response, error) { return nil, errors.New("no") },
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Place = Place{Dir: dir, Workspace: config.Workspace}
		config.SessionFile = place.Transcript()
	})

	before := time.Now()
	collect(t, mustSubmitTo(t, agent, "why does the box flicker?"))

	meta, err := LoadMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID != "0123456789abcdef" {
		t.Fatalf("meta.json names the session %q", meta.ID)
	}
	if meta.LastUserAt.Before(before) {
		t.Fatalf("the last-active stamp is %v, want the moment the person spoke", meta.LastUserAt)
	}
	if meta.Title != "why does the box flicker?" {
		t.Fatalf("the folder is called %q, want the person's opening line", meta.Title)
	}
	if meta.Model != "test/model" {
		t.Fatalf("the row names the model %q", meta.Model)
	}
	if meta.Build != buildinfo.String() {
		t.Fatalf("meta.json names build %q, want %q", meta.Build, buildinfo.String())
	}
}

// And the name the session earns replaces the placeholder, so a picker draws
// what the journal says rather than the first thing anybody typed.
func TestTheEarnedNameReachesTheFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	place := Place{Dir: dir}
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("noted"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the flickering box"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Place = Place{Dir: dir, Workspace: config.Workspace}
		config.SessionFile = place.Transcript()
	})
	collect(t, mustSubmitTo(t, agent, "why does the box flicker?"))

	meta, _ := LoadMeta(dir)
	if meta.Title != "the flickering box" {
		t.Fatalf("the folder is called %q, want the name the session gave itself", meta.Title)
	}
}

// A session with no folder stamps nothing and says nothing about it: the legacy
// layout has no meta.json, which is what the zero Place means.
func TestASessionWithNoFolderStampsNothing(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("noted"), nil },
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = filepath.Join(config.Workspace, "flat.jsonl")
	})
	collect(t, mustSubmitTo(t, agent, "hello"))
	if _, err := LoadMeta(workspace); err != nil {
		t.Fatalf("a flat session wrote something a folder reader choked on: %v", err)
	}
	meta, _ := LoadMeta(workspace)
	if meta.ID != "" {
		t.Fatalf("a flat session wrote a meta.json: %+v", meta)
	}
}

// mustSubmitTo is one turn started, with the error made fatal: every test here
// is about what the folder learned, not about whether a submission can fail.
func mustSubmitTo(t *testing.T, agent *Agent, text string) <-chan Event {
	t.Helper()
	events, err := agent.Submit(context.Background(), text)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	return events
}

// ARCHIVING IS A FACT ABOUT THE META AND NOTHING ELSE: SetArchived flips the
// one field through the same file every other fact rides, refuses a folder
// with no conversation in it, and the world's row carries the answer out.
func TestSetArchivedRoundTripsThroughTheMeta(t *testing.T) {
	dir := t.TempDir()
	if err := SetArchived(dir, true); err == nil {
		t.Fatal("a folder with no conversation accepted an archive mark")
	}
	if err := SaveMeta(dir, Meta{ID: "abcd000000000001", Workspace: dir, Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := SetArchived(dir, true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	meta, err := LoadMeta(dir)
	if err != nil || !meta.Archived {
		t.Fatalf("the mark did not land: %+v (%v)", meta, err)
	}
	if err := SetArchived(dir, false); err != nil {
		t.Fatalf("bring back: %v", err)
	}
	if meta, _ := LoadMeta(dir); meta.Archived {
		t.Fatalf("the mark did not lift: %+v", meta)
	}
}
