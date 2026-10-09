package session

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// attachmentsOfUser finds the person's line by its opening words and answers
// what the transcript says it carried.
func attachmentsOfUser(entries []DisplayEntry, opening string) (DisplayEntry, bool) {
	for _, entry := range entries {
		if entry.Role == "user" && (strings.HasPrefix(entry.Text, opening) || opening == "") {
			return entry, true
		}
	}
	return DisplayEntry{}, false
}

// Files and pictures a person attached are structured on the user entry, the
// model-facing sentence is off the person's words, and a reopened session draws
// the identical entry.
func TestAttachedFilesAndPicturesAreStructuredLiveAndOnReplay(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "session.jsonl")
	first, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		withVision(config)
		config.SessionFile = path
	})
	photo := writeImage(t, workspace, "photo.png", "PHOTOBYTES")
	logFile := filepath.Join(workspace, "server.log")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events, err := first.SubmitAttached(ctx, "why does this fail", []string{logFile}, []Image{{Path: photo}})
	if err != nil {
		t.Fatalf("SubmitAttached: %v", err)
	}
	collect(t, events)

	want := []AttachmentRef{
		{Path: photo, Name: "photo.png", MIME: "image/png", Kind: AttachmentImage},
		{Path: logFile, Name: "server.log", MIME: guessMIME(logFile), Kind: AttachmentFile},
	}
	live, ok := attachmentsOfUser(first.Transcript(), "why does this fail")
	if !ok {
		t.Fatal("the message is not in the live transcript")
	}
	if live.Text != "why does this fail" {
		t.Fatalf("live Text = %q, want the person's words alone", live.Text)
	}
	if !reflect.DeepEqual(live.Attachments, want) {
		t.Fatalf("live attachments = %+v, want %+v", live.Attachments, want)
	}
	if len(live.ImageRefs) != 1 || live.ImageRefs[0] != photo {
		t.Fatalf("ImageRefs must stay as they were: %v", live.ImageRefs)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := newAgent(Config{
		Workspace: workspace, Model: "test/model", System: "SYSTEM", SessionFile: path,
		SupportsImages: func(string) bool { return true },
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	replayed, ok := attachmentsOfUser(second.Transcript(), "why does this fail")
	if !ok {
		t.Fatal("the message is not in the replayed transcript")
	}
	if replayed.Text != live.Text || !reflect.DeepEqual(replayed.Attachments, live.Attachments) {
		t.Fatalf("replay differs from live:\nlive   %+v\nreplay %+v", live, replayed)
	}
}

// A file attached with no words leaves an empty bubble text and one chip.
func TestAFileAloneLeavesNoWordsAndOneAttachment(t *testing.T) {
	first, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	data := filepath.Join(workspace, "data.csv")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events, err := first.SubmitAttached(ctx, "", []string{data}, nil)
	if err != nil {
		t.Fatalf("SubmitAttached: %v", err)
	}
	collect(t, events)
	entry, ok := attachmentsOfUser(first.Transcript(), "")
	if !ok {
		t.Fatal("no user entry")
	}
	if entry.Text != "" || len(entry.Attachments) != 1 || entry.Attachments[0].Kind != AttachmentFile {
		t.Fatalf("entry = %+v", entry)
	}
}

// Nothing says a sentence is a file record except the journal: a message from a
// file written before files were recorded keeps its words as they were and
// carries no attachments, and a person quoting the phrase is left alone.
func TestOnlyTheJournalsRecordMakesASentenceAnAttachment(t *testing.T) {
	typed := "attached file: /tmp/a.log"
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events, err := agent.Submit(ctx, typed)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	entry, ok := attachmentsOfUser(agent.Transcript(), "attached file")
	if !ok {
		t.Fatal("no user entry")
	}
	if entry.Text != typed || entry.Attachments != nil {
		t.Fatalf("an unrecorded sentence was parsed: %+v", entry)
	}
}

// The sentence the engine composes and the one the display strips are one.
func TestTheAttachedSentenceRoundTrips(t *testing.T) {
	for _, paths := range [][]string{{"/a/x.log"}, {"/a/x.log", "/b/y.csv"}} {
		for _, words := range []string{"", "look at this"} {
			said := AttachedSentence(words, paths)
			if got := withoutAttachedBlock(said, attachedFilesBlock(paths)); got != words {
				t.Fatalf("%q with %v came back as %q", words, paths, got)
			}
		}
	}
}
