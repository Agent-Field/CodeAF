package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ── WHAT A LANDED ROW SAYS IT WROTE ─────────────────────────────────────────

func TestALandedRowNamesTheFilesBehindItsCount(t *testing.T) {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{
		graph:   graph,
		id:      7,
		spec:    taskSpec{title: "fix the nil-map crash"},
		state:   TaskDone,
		report:  "Added the guard and the regression test.",
		changed: []string{"internal/parse/row.go", "internal/parse/row_test.go"},
	}
	graph.mu.Lock()
	entry := node.indexEntryLocked("aaaa1111aaaa1111")
	graph.mu.Unlock()

	if entry.FilesChanged != 2 {
		t.Fatalf("the count is %d, want the two files the node wrote", entry.FilesChanged)
	}
	if len(entry.Files) != 2 || entry.Files[0] != "internal/parse/row.go" || entry.Files[1] != "internal/parse/row_test.go" {
		t.Fatalf("the row names %v, want the paths in the order the node wrote them", entry.Files)
	}

	// And it survives the file, which is the whole point of writing it down.
	path := filepath.Join(t.TempDir(), taskIndexName)
	appendTaskIndex(path, entry)
	rows := ReadTaskIndex(path)
	if len(rows) != 1 {
		t.Fatalf("read back %d rows, want one", len(rows))
	}
	if rows[0].FilesChanged != 2 || len(rows[0].Files) != 2 || rows[0].Files[1] != "internal/parse/row_test.go" {
		t.Fatalf("the row came back as %d files %v", rows[0].FilesChanged, rows[0].Files)
	}
}

func TestALandedRowThatWroteNothingNamesNothing(t *testing.T) {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 3, spec: taskSpec{title: "read the parser"}, state: TaskDone}
	graph.mu.Lock()
	entry := node.indexEntryLocked("aaaa1111aaaa1111")
	graph.mu.Unlock()

	if entry.Files != nil {
		t.Fatalf("a node that wrote nothing named %v", entry.Files)
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	// omitempty, so the emptiness costs nothing on disk and nothing in a reader.
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if _, ok := back["files"]; ok {
		t.Fatalf("an empty list was written to the file: %s", raw)
	}
}

func TestTheFileListIsCappedAndTheCountStaysHonest(t *testing.T) {
	written := make([]string, 0, taskFilesLimit+50)
	for i := range taskFilesLimit + 50 {
		written = append(written, fmt.Sprintf("internal/sweep/file%03d.go", i))
	}
	files, count := taskFileCitations(written)
	if count != taskFilesLimit+50 {
		t.Fatalf("the count is %d, want the honest total %d", count, taskFilesLimit+50)
	}
	if len(files) != taskFilesLimit {
		t.Fatalf("the list is %d long, want it capped at %d", len(files), taskFilesLimit)
	}
	// THE TAIL IS WHAT GOES. The first paths are the ones the node wrote first,
	// and they are the ones kept.
	if files[0] != written[0] || files[taskFilesLimit-1] != written[taskFilesLimit-1] {
		t.Fatalf("the cap dropped the wrong end: kept %q…%q", files[0], files[len(files)-1])
	}
	// The copy is the row's own: a caller mutating the node's list afterwards
	// must not reach into a row that was already built.
	written[0] = "somewhere/else.go"
	if files[0] == "somewhere/else.go" {
		t.Fatal("the row shares its list with the node it was built from")
	}
}

func TestAnIndexRowWrittenBeforeFilesExistedStillReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), taskIndexName)
	// Exactly the shape this build wrote before the field existed: a count and
	// no list at all.
	old := `{"id":"4","name":"port-the-parser","label":"Port the parser","title":"Port the parser",` +
		`"status":"done","outcome":"it landed","filesChanged":9,"endedAt":"2026-01-02T03:04:05Z","sessionId":"bbbb2222bbbb2222"}`
	if err := os.WriteFile(path, []byte(old+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rows := ReadTaskIndex(path)
	if len(rows) != 1 {
		t.Fatalf("read %d rows from a file this build did not write", len(rows))
	}
	if rows[0].FilesChanged != 9 {
		t.Fatalf("the old count came back as %d, want 9", rows[0].FilesChanged)
	}
	if rows[0].Files != nil {
		t.Fatalf("a row that never named files came back naming %v", rows[0].Files)
	}
	// AND THE SILENCE IS UNKNOWN, NOT "IT TOUCHED NOTHING": nine files went
	// somewhere, and this row cannot say where.
	touching, unknown := LandedTouching(rows, []string{"internal/parse/row.go"}, time.Time{})
	if len(touching) != 0 {
		t.Fatalf("a row that names no files was called a hit: %+v", touching)
	}
	if len(unknown) != 1 {
		t.Fatalf("the unanswerable row was dropped instead of reported: %+v", unknown)
	}
}

// ── WHAT A RUNNING NODE SAYS IT HAS WRITTEN SO FAR ──────────────────────────

func TestARunningNodeSaysWhatItHasWrittenSoFar(t *testing.T) {
	bucket := t.TempDir()
	agent, dir := newPresenceSession(t, bucket, "cccc3333cccc3333")
	waitForPresence(t, dir)

	graph := agent.graph()
	node := &TaskNode{
		graph: graph, id: 7,
		spec:    taskSpec{title: "fix the flaky auth test"},
		state:   TaskRunning,
		started: time.Now().Add(-time.Minute),
	}
	graph.mu.Lock()
	graph.nodes[node.id] = node
	graph.order = append(graph.order, node.id)
	graph.mu.Unlock()

	if tasks := agent.presenceSnapshot(time.Now()).RunningTasks; len(tasks) != 1 || tasks[0].Files != nil {
		t.Fatalf("a node that has written nothing yet claimed %+v", tasks)
	}

	node.noteWrote("internal/auth/session.go")
	node.noteWrote("internal/auth/session_test.go")
	// The same path again is the same file: a repair round rewriting it does not
	// make it two.
	node.noteWrote("internal/auth/session.go")

	tasks := agent.presenceSnapshot(time.Now()).RunningTasks
	if len(tasks) != 1 {
		t.Fatalf("presence carries %d tasks, want the one that is running", len(tasks))
	}
	if len(tasks[0].Files) != 2 || tasks[0].Files[0] != "internal/auth/session.go" {
		t.Fatalf("the running node claims %v", tasks[0].Files)
	}

	// And another window reads it off the disk, whole.
	raw, err := json.Marshal(agent.presenceSnapshot(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(bucket, "dddd4444dddd4444")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, presenceName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	presence, ok := ReadSessionPresence(other, time.Now())
	if !ok {
		t.Fatal("the refresh did not read back")
	}
	if len(presence.RunningTasks) != 1 || len(presence.RunningTasks[0].Files) != 2 {
		t.Fatalf("the claim did not survive the file: %+v", presence.RunningTasks)
	}
}

func TestALiveClaimIsCappedTheSameWayARowIs(t *testing.T) {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "sweep the imports"}, state: TaskRunning}
	for i := range taskFilesLimit + 50 {
		node.noteWrote(fmt.Sprintf("internal/sweep/file%03d.go", i))
	}
	graph.mu.Lock()
	held := len(node.wrote)
	graph.mu.Unlock()
	if held != taskFilesLimit {
		t.Fatalf("a running node is holding %d paths, want them capped at %d", held, taskFilesLimit)
	}
}

func TestAPresenceRowWrittenBeforeFilesExistedStillReads(t *testing.T) {
	dir := t.TempDir()
	old := fmt.Sprintf(`{"schema":%d,"sessionId":"eeee5555eeee5555","workspace":"/work/aforge","pid":42,`+
		`"updatedAt":%q,"state":"working","runningTasks":[{"id":"3","title":"Port the parser","state":"running"}]}`,
		presenceSchema, time.Now().Format(time.RFC3339Nano))
	if err := os.WriteFile(filepath.Join(dir, presenceName), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	presence, ok := ReadSessionPresence(dir, time.Now())
	if !ok {
		t.Fatal("a presence file from an older build was refused")
	}
	if len(presence.RunningTasks) != 1 || presence.RunningTasks[0].Files != nil {
		t.Fatalf("the older row read as %+v", presence.RunningTasks)
	}
}

// ── THE COMPARISON ──────────────────────────────────────────────────────────

func TestSharedFilesIsExactPathsInTheFirstListsOrder(t *testing.T) {
	one := []string{"b.go", "a.go", " a.go ", "", "c.go"}
	two := []string{"a.go", "c.go", "d.go"}
	got := SharedFiles(one, two)
	if len(got) != 2 || got[0] != "a.go" || got[1] != "c.go" {
		t.Fatalf("shared = %v, want a.go and c.go in the first list's order, said once", got)
	}
	if SharedFiles(nil, two) != nil || SharedFiles(one, nil) != nil {
		t.Fatal("a comparison against nothing answered something")
	}
	// NO PATTERNS. A directory is not the files under it and a different
	// spelling is a different file.
	if got := SharedFiles([]string{"internal/session/task.go"}, []string{"internal/session"}); got != nil {
		t.Fatalf("a directory matched a file under it: %v", got)
	}
	if got := SharedFiles([]string{"A.go"}, []string{"a.go"}); got != nil {
		t.Fatalf("two spellings were folded together: %v", got)
	}
}

func TestTouchingKeepsWhatOverlapsApartFromWhatCannotBeAsked(t *testing.T) {
	bucket := t.TempDir()
	writeWindow(t, bucket, "aaaa1111aaaa1111", "the parser window", time.Second, PresenceTask{
		ID: "3", Title: "Port the parser", State: string(TaskRunning),
		Files: []string{"internal/parse/row.go", "internal/parse/doc.go"},
	})
	writeWindow(t, bucket, "bbbb2222bbbb2222", "the docs window", time.Second, PresenceTask{
		ID: "1", Title: "Rewrite the manual", State: string(TaskRunning),
		Files: []string{"internal/manual/chat/tasks.md"},
	})
	// A window on a build too old to say what it is writing — or a node that has
	// not written anything yet. The two are indistinguishable, and both are
	// UNKNOWN and never "nowhere near your files".
	writeWindow(t, bucket, "cccc3333cccc3333", "the quiet window", time.Second, PresenceTask{
		ID: "9", Title: "Something unspoken", State: string(TaskRunning),
	})
	// And a window that went away three heartbeats ago says nothing at all.
	writeWindow(t, bucket, "dddd4444dddd4444", "the dead window", 10*presenceWindow, PresenceTask{
		ID: "2", Title: "Work nobody is doing", State: string(TaskRunning),
		Files: []string{"internal/parse/row.go"},
	})

	elsewhere := ReadElsewhere(bucket, time.Now(), "zzzz9999zzzz9999")
	touching, unknown := elsewhere.Touching([]string{"internal/parse/row.go"})
	if len(touching) != 1 {
		t.Fatalf("touching = %+v, want the one window already in that file", touching)
	}
	if touching[0].SessionID != "aaaa1111aaaa1111" || touching[0].Session != "the parser window" {
		t.Fatalf("the overlap names %+v, want the parser window", touching[0])
	}
	if len(unknown) != 1 || unknown[0].SessionID != "cccc3333cccc3333" {
		t.Fatalf("unknown = %+v, want the one window that said nothing", unknown)
	}

	// THE READING ALREADY EXCLUDED THIS SESSION, so a window never finds itself
	// in its own way.
	mine := ReadElsewhere(bucket, time.Now(), "aaaa1111aaaa1111")
	if touching, _ := mine.Touching([]string{"internal/parse/row.go"}); len(touching) != 0 {
		t.Fatalf("a window found itself: %+v", touching)
	}

	// Asking about no files is asking nothing, and it answers nothing rather
	// than handing back every window on the project.
	if touching, unknown := elsewhere.Touching(nil); len(touching) != 0 || len(unknown) != 0 {
		t.Fatalf("an empty question answered %+v / %+v", touching, unknown)
	}
}

func TestLandedTouchingAnswersWhatChangedUnderYou(t *testing.T) {
	now := time.Now()
	since := now.Add(-time.Hour)
	rows := []TaskIndexEntry{
		{
			ID: "9", Title: "Rename the row type", Status: string(TaskDone),
			SessionID: "bbbb2222bbbb2222", EndedAt: now.Add(-10 * time.Minute),
			FilesChanged: 2, Files: []string{"internal/parse/row.go", "internal/parse/doc.go"},
		},
		{
			ID: "8", Title: "Work still going", Status: string(TaskRunning),
			SessionID: "bbbb2222bbbb2222", EndedAt: now.Add(-20 * time.Minute),
			FilesChanged: 1, Files: []string{"internal/parse/row.go"},
		},
		{
			ID: "7", Title: "Something that said nothing", Status: string(TaskDone),
			SessionID: "bbbb2222bbbb2222", EndedAt: now.Add(-30 * time.Minute),
		},
		{
			ID: "6", Title: "Somewhere else entirely", Status: string(TaskDone),
			SessionID: "bbbb2222bbbb2222", EndedAt: now.Add(-40 * time.Minute),
			FilesChanged: 1, Files: []string{"internal/manual/chat/tasks.md"},
		},
		{
			ID: "5", Title: "Landed before you looked", Status: string(TaskDone),
			SessionID: "bbbb2222bbbb2222", EndedAt: now.Add(-3 * time.Hour),
			FilesChanged: 1, Files: []string{"internal/parse/row.go"},
		},
	}

	touching, unknown := LandedTouching(rows, []string{"internal/parse/row.go"}, since)
	if len(touching) != 1 || touching[0].ID != "9" {
		t.Fatalf("touching = %+v, want only the row that landed in that file since", touching)
	}
	if len(unknown) != 1 || unknown[0].ID != "7" {
		t.Fatalf("unknown = %+v, want the one landed row that named no files", unknown)
	}

	// A ZERO MOMENT IS THE WHOLE FILE.
	touching, _ = LandedTouching(rows, []string{"internal/parse/row.go"}, time.Time{})
	if len(touching) != 2 {
		t.Fatalf("touching = %+v, want both landed rows in that file", touching)
	}
	// And nothing asked is nothing answered.
	if touching, unknown := LandedTouching(rows, nil, since); len(touching) != 0 || len(unknown) != 0 {
		t.Fatalf("an empty question answered %+v / %+v", touching, unknown)
	}
}
