package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func traceWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "media"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "media", "shot.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func traceFeedModel(t *testing.T, dir string) *Model {
	t.Helper()
	commander := &countingResolver{
		fakeCommander: &fakeCommander{current: map[string]string{}}, workspace: dir,
	}
	model := NewWithCommander(&fakeBackend{}, "feed", commander)
	model.nodeViewID = "worker"
	model.inspectedNode = store.Node{ID: "worker", Brief: "do a thing", Status: store.Running}
	model.setSize(90, 30)
	return model
}

// traceChunks is a worker's log arriving the way it actually arrives: a line
// at a time, sometimes half a line at a time.
var traceChunks = []string{
	"── turn 1 finish=stop in=10 out=20 ──\n",
	"text: thinking about ⏎the first move\n",
	"call read_file {\"path\":\"main.go\"}\n",
	"  → ok, 40 lines\n",
	"wrote media/shot.png for you\n",
	"steered: try the other branch\n",
	"── turn 2 finish=stop in=30 out=40 [boosted] ──\n",
	"text: " + strings.Repeat("a long deliberate thought⏎", 9) + "\n",
	"call read_file {\"path\":\"main.go\"}\n",
	"  → ok, 40 lines\n",
	"a half written li",
	"ne that finishes later\n",
}

// The worker only ever appends to its log, so everything up to its last
// complete line has already been parsed, rendered, keyed and scanned. Reading
// it a line at a time has to land exactly where reading it whole lands —
// including the identities the expansions are keyed by, which carry an
// occurrence number for blocks whose content repeats.
func TestGrowingTraceLandsWhereAFullReparseLands(t *testing.T) {
	dir := traceWorkspace(t)
	grown := traceFeedModel(t, dir)
	for index := range traceChunks {
		grown.nodeTraceText = strings.Join(traceChunks[:index+1], "")
		document := grown.renderActivityFeed(88)

		cold := traceFeedModel(t, dir)
		cold.nodeTraceText = grown.nodeTraceText
		cold.forgetFeedTrace()
		want := cold.renderActivityFeed(88)
		if document != want {
			t.Fatalf("chunk %d: grown feed differs from a cold one:\n--- grown\n%q\n--- cold\n%q",
				index, document, want)
		}
		if !reflect.DeepEqual(grown.feedKeys, cold.feedKeys) {
			t.Fatalf("chunk %d: block identities differ:\n grown %v\n cold  %v",
				index, grown.feedKeys, cold.feedKeys)
		}
		if !reflect.DeepEqual(grown.feedRows, cold.feedRows) {
			t.Fatalf("chunk %d: click rows differ", index)
		}
	}

	// The thread under the log is not settled either: a message arriving after
	// the log has stopped still lands in the document.
	grown.nodeMessages = []store.Message{{Seq: 4, Role: store.RoleUser, Body: "please check the edge case"}}
	if feed := grown.renderActivityFeed(88); !strings.Contains(feed, "please check the edge case") {
		t.Fatal("a message arriving after the log did not reach the feed")
	}

	// A log whose head has fallen off its byte budget is not the log the kept
	// prefix was read from, so all of it is read again.
	grown.nodeTraceText = strings.Join(traceChunks[4:], "")
	document := grown.renderActivityFeed(88)
	cold := traceFeedModel(t, dir)
	cold.nodeTraceText = grown.nodeTraceText
	cold.nodeMessages = grown.nodeMessages
	if want := cold.renderActivityFeed(88); document != want {
		t.Fatalf("a truncated head did not force a full reparse:\n--- got\n%q\n--- want\n%q",
			document, want)
	}
}

// A narrower pane is a different document, and the kept prefix is laid out for
// the pane it was parsed at.
func TestFeedWidthChangeReparsesTheTrace(t *testing.T) {
	dir := traceWorkspace(t)
	model := traceFeedModel(t, dir)
	model.nodeTraceText = strings.Join(traceChunks, "")
	wide := model.renderActivityFeed(88)
	narrow := model.renderActivityFeed(50)
	if wide == narrow {
		t.Fatal("a narrower pane drew the same document")
	}
	cold := traceFeedModel(t, dir)
	cold.nodeTraceText = model.nodeTraceText
	if want := cold.renderActivityFeed(50); narrow != want {
		t.Fatalf("the narrowed feed differs from a cold one at that width:\n got %q\nwant %q",
			narrow, want)
	}
}
