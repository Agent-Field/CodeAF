package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// A KEYED NOTE TO A HANDED-OVER RUN ROW IS KEYED BY THE STORE, and read back by
// the id the store minted. The producer is the real action door, the store is a
// real SQLite file reopened, the read is a real trajectory line naming the
// note, and the projection is the app's own task page. Equal words with other
// keys stay other notes; an unkeyed send reads as unknown; and no read is ever
// attributed by position, text or time.
func TestARunRowNoteKeepsItsClientIDInTheStoreAndTheReadStepJoinsOnTheNoteID(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	registerBeltRunEngine(t, newBatchRunDouble())
	dir := t.TempDir()
	agent := batchAgent(t, dir)
	stand := taskStand{dir: agent.config.Workspace, mode: TaskModeWorktree}
	if err := agent.startKnownTaskRun(context.Background(), 61, "the run", "brief", nil, stand, ""); err != nil {
		t.Fatalf("start the run: %v", err)
	}
	send := func(key, text string) {
		t.Helper()
		if err := agent.AppTaskAction(AppTaskIntent{ID: "61", Action: "note", Text: text, ClientID: key}); err != nil {
			t.Fatalf("note %q: %v", key, err)
		}
	}
	send("cid-a", "same words")
	send("cid-b", "same words")
	send("", "same words")
	send(strings.Repeat("k", AppNoteClientIDMax+1), "same words")

	store := beltRunStoreAt(t, dir)
	notes := store.Notes("61", 0)
	_ = store.Close()
	if len(notes) != 4 {
		t.Fatalf("store notes = %#v, want four", notes)
	}
	for i, want := range []string{"cid-a", "cid-b", "", ""} {
		if notes[i].ClientID != want || notes[i].From != plandb.NoteFromPerson || notes[i].Body != "same words" {
			t.Fatalf("note %d = %#v, want key %q", i, notes[i], want)
		}
	}
	// The worker read the SECOND note only. The line names it by its own id.
	writePlanTrajectory(t, dir, "61",
		`{"kind":"step","step":1,"command":"ls"}`,
		`{"kind":"step","step":2,"command":"go build"}`,
		`{"kind":"notes","notes":["`+notes[1].ID+`"]}`,
	)

	// Reopened: a fresh store handle, as a restart reads it.
	view, found, err := agent.AppTask("61")
	if err != nil || !found {
		t.Fatalf("AppTask = %v %v", found, err)
	}
	var person []AppTaskNote
	for _, note := range view.Notes {
		if note.From == AppNoteFromPerson {
			person = append(person, note)
		}
	}
	if len(person) != 4 {
		t.Fatalf("page person notes = %#v", person)
	}
	for i, want := range []string{"cid-a", "cid-b", "", ""} {
		if person[i].ClientID != want {
			t.Fatalf("page note %d key = %q, want %q", i, person[i].ClientID, want)
		}
		read := person[i].ReadAtStep
		if (i == 1) != (read != nil) || (read != nil && *read != 2) {
			t.Fatalf("page note %d read = %v, want only note 1 read at step 2", i, read)
		}
	}
}

// A KEY IS COMMITTED WITH ITS NOTE OR NOT AT ALL: a note refused before the
// store writes (words that are only blank) leaves no key anywhere.
func TestARefusedRunRowNoteStoresNoKey(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	registerBeltRunEngine(t, newBatchRunDouble())
	dir := t.TempDir()
	agent := batchAgent(t, dir)
	stand := taskStand{dir: agent.config.Workspace, mode: TaskModeWorktree}
	if err := agent.startKnownTaskRun(context.Background(), 61, "the run", "brief", nil, stand, ""); err != nil {
		t.Fatalf("start the run: %v", err)
	}
	if err := agent.AppTaskAction(AppTaskIntent{ID: "61", Action: "note", Text: "   ", ClientID: "cid"}); err == nil {
		t.Fatal("an empty note was taken")
	}
	store := beltRunStoreAt(t, dir)
	defer store.Close()
	if notes := store.Notes("61", 0); len(notes) != 0 {
		t.Fatalf("a refused note left %#v", notes)
	}
}

// A PLAN TASK'S PAGE KEEPS THE STORE'S OWN NOTE IDS AND KEYS: equal text under
// two keys is two notes, a worker's note and an unkeyed one are unknown, and
// the page drops nothing it used to carry.
func TestAPlanTaskNoteKeepsItsIDAndClientIDOnThePage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "ask-chat", plandb.TaskSpec{ID: "child", Title: "Write handler", Description: "replace the lookup"})
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, nil)
	armPlanStore(t, agent, path, "ask-chat")

	for _, key := range []string{"cid-1", "cid-2", ""} {
		if err := agent.PlanNoteKeyed("child", "same words", key); err != nil {
			t.Fatal(err)
		}
	}
	if err := agent.PlanNote("child", "same words"); err != nil {
		t.Fatal(err)
	}
	if err := agent.PlanNoteFromChat("child", "same words"); err != nil {
		t.Fatal(err)
	}
	page, ok := agent.PlanTaskPage("t-child")
	if !ok {
		t.Fatal("no page")
	}
	want := []struct {
		key    string
		person bool
	}{{"cid-1", true}, {"cid-2", true}, {"", true}, {"", true}, {"", false}}
	if len(page.Notes) != len(want) {
		t.Fatalf("page notes = %#v", page.Notes)
	}
	seen := map[string]bool{}
	for i, note := range page.Notes {
		if note.ClientID != want[i].key || note.Person != want[i].person || note.ID == "" || seen[note.ID] {
			t.Fatalf("page note %d = %#v, want key %q person %v with a fresh id", i, note, want[i].key, want[i].person)
		}
		seen[note.ID] = true
	}
}

// A CLIENT ID IS OPAQUE AND BYTE-BOUNDED AT THE PRODUCER: both real stores
// keep the exact boundary and store malformed keys as unknown, not truncated.
func TestAPersonNoteKeepsOnlyValidClientIDsAcrossBothStores(t *testing.T) {
	cases := []struct{ name, key, want string }{
		{"exact byte limit", strings.Repeat("k", AppNoteClientIDMax), strings.Repeat("k", AppNoteClientIDMax)},
		{"past byte limit", strings.Repeat("k", AppNoteClientIDMax+1), ""},
		{"control character", "bad\x00key", ""},
		{"invalid UTF8", "bad\xffkey", ""},
	}
	t.Run("run", func(t *testing.T) {
		t.Setenv("CODEAF_TASK_BELT", "bash")
		registerBeltRunEngine(t, newBatchRunDouble())
		dir := t.TempDir()
		agent := batchAgent(t, dir)
		stand := taskStand{dir: agent.config.Workspace, mode: TaskModeWorktree}
		if err := agent.startKnownTaskRun(context.Background(), 61, "the run", "brief", nil, stand, ""); err != nil {
			t.Fatal(err)
		}
		for _, tc := range cases {
			if err := agent.AppTaskAction(AppTaskIntent{ID: "61", Action: "note", Text: "same words", ClientID: tc.key}); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
		}
		store := beltRunStoreAt(t, dir)
		defer store.Close()
		got := store.Notes("61", 0)
		if len(got) != len(cases) {
			t.Fatalf("notes=%d, want %d", len(got), len(cases))
		}
		for i, tc := range cases {
			if got[i].ClientID != tc.want {
				t.Errorf("%s: stored key=%q, want %q", tc.name, got[i].ClientID, tc.want)
			}
		}
	})
	t.Run("plan", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), planStoreFilename)
		seedPlanStore(t, path, "ask-chat", plandb.TaskSpec{ID: "child", Title: "Write handler", Description: "replace the lookup"})
		agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, nil)
		armPlanStore(t, agent, path, "ask-chat")
		for _, tc := range cases {
			if err := agent.PlanNoteKeyed("child", "same words", tc.key); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
		}
		store, err := plandb.Open(path, "", planRootID, "", "")
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		got := store.Notes("child", 0)
		if len(got) != len(cases) {
			t.Fatalf("notes=%d, want %d", len(got), len(cases))
		}
		for i, tc := range cases {
			if got[i].ClientID != tc.want {
				t.Errorf("%s: stored key=%q, want %q", tc.name, got[i].ClientID, tc.want)
			}
		}
	})
}
