package session

import (
	"strings"
	"testing"
)

// WHAT THE MODEL IS TOLD WHEN A NODE LANDS.
//
// The note is the model's only account of work it handed off, and it is about to
// become the model's sentence to a person — so the verb has to be the state's
// own word, and the three settled states have three different pieces of news
// (task_contract.go). These are assertions about that wording and nothing else.

func TestTaskNoteSaysWhichOfTheThreeItIs(t *testing.T) {
	uri := "file:///tmp/lab/.aforge/sessions/task-7.jsonl"
	for _, c := range []struct {
		what   string
		notice TaskNotice
		want   []string
		never  []string
	}{{
		what: "a verified landing",
		notice: TaskNotice{
			ID: 7, Title: "Port the parser", State: TaskDone,
			Report: auditVerified + " — go test ./... ok", Merge: mergeMerged, Branch: "task/parser",
		},
		// FINISHED, and the report says on whose word: the note does not spend
		// "verified" itself, because a done node also reaches this line by a
		// person accepting it and by the audit row being off.
		want: []string{
			"task 7 finished: Port the parser",
			auditVerified + " — go test ./... ok",
			"its branch task/parser merged into yours",
		},
		never: []string{"failed", "could not be verified"},
	}, {
		what: "a refuted landing",
		notice: TaskNotice{
			ID: 8, Title: "Mix audio", State: TaskFailed,
			Report: "REFUTED — the new case does not run",
		},
		want:  []string{"task 8 failed: Mix audio", "REFUTED — the new case does not run"},
		never: []string{"could not be verified"},
	}, {
		what: "a landing nobody could judge",
		notice: TaskNotice{
			ID: 9, Title: "Collect sources", State: TaskUnverified,
			Report: auditUnverified + " — asked twice and got no verdict either time",
		},
		// NOT "FAILED", and it says what is waiting on whom: the state exists
		// because "the work is wrong" and "nobody could tell me whether the work
		// is wrong" are different news.
		want: []string{
			"task 9 could not be verified: Collect sources",
			auditUnverified + " — asked twice",
			"it is neither done nor failed",
			"tasks id 9 resolve accept|reaudit|refute",
		},
		never: []string{"task 9 failed", "task 9 finished"},
	}} {
		note := taskNote(c.notice, uri)
		for _, want := range c.want {
			if !strings.Contains(note, want) {
				t.Fatalf("%s: the note is missing %q:\n%s", c.what, want, note)
			}
		}
		for _, never := range c.never {
			if strings.Contains(note, never) {
				t.Fatalf("%s: the note says %q:\n%s", c.what, never, note)
			}
		}
		// THE TRANSCRIPT RIDES THE FIRST LINE, whatever the outcome was: a node
		// that could not be judged is the one a person is most likely to want to
		// read for themselves.
		if first := strings.SplitN(note, "\n", 2)[0]; !strings.HasSuffix(first, " · transcript "+uri) {
			t.Fatalf("%s: the first line does not end in the transcript: %q", c.what, first)
		}
	}
}

// A NODE WITH NO JOURNAL POINTS AT NOTHING, and the line simply ends: a URI
// nobody can open is worse than no URI at all. A checkpoint does not carry a
// node's journal path, so this is the ordinary shape of a note replayed for a
// resumed session (task_store.go).
func TestTaskNoteWithNoTranscriptEndsAtTheTitle(t *testing.T) {
	note := taskNote(TaskNotice{ID: 3, Title: "Port the parser", State: TaskDone}, "")
	if first := strings.SplitN(note, "\n", 2)[0]; first != "task 3 finished: Port the parser" {
		t.Fatalf("the first line is %q", first)
	}
	if strings.Contains(note, "transcript") {
		t.Fatalf("a node with no journal was given one:\n%s", note)
	}
}
