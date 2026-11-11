//go:build !windows

package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// seedStore writes n settled records through the owner's own door.
func seedStore(t *testing.T, dir string, ids ...int) {
	t.Helper()
	s := processStore{dir: dir}
	root, head, err := s.open(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	base := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	for _, id := range ids {
		n := JobNotice{ID: id, Kind: JobKindCommand, Command: "echo " + strconv.Itoa(id), State: JobDone, Started: base.Add(time.Duration(id) * time.Minute), Ended: base.Add(time.Duration(id)*time.Minute + time.Second)}
		if _, err := s.put(root, &head, n, false); err != nil {
			t.Fatal(err)
		}
	}
}

func walkPages(t *testing.T, read func(cursor string) (ProcessPage, error)) ([]ProcessPageRow, int) {
	t.Helper()
	var all []ProcessPageRow
	cursor, pages := "", 0
	for {
		page, err := read(cursor)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		all = append(all, page.Rows...)
		if page.Next == "" {
			return all, pages
		}
		cursor = page.Next
		if pages > 1000 {
			t.Fatal("pages never ended")
		}
	}
}

func TestProcessPagesWalkWholeHistoryNewestFirstWithinLimit(t *testing.T) {
	dir := t.TempDir()
	var ids []int
	for i := 1; i <= 237; i++ {
		ids = append(ids, i)
	}
	seedStore(t, dir, ids...)
	file := filepath.Join(dir, "transcript.jsonl")
	first, err := ReadProcessPage(context.Background(), file, ProcessPageAsk{})
	if err != nil || len(first.Rows) != ProcessPageDefault || first.Next == "" || first.Rows[0].ID != 237 {
		t.Fatalf("first page: rows=%d next=%q err=%v", len(first.Rows), first.Next, err)
	}
	all, pages := walkPages(t, func(cursor string) (ProcessPage, error) {
		return ReadProcessPage(context.Background(), file, ProcessPageAsk{Limit: ProcessPageMax, Cursor: cursor})
	})
	if len(all) != 237 || pages != 3 {
		t.Fatalf("walk: rows=%d pages=%d", len(all), pages)
	}
	for i, row := range all {
		if row.ID != 237-i || row.Unknown != "" {
			t.Fatalf("row %d: %+v", i, row)
		}
	}
	if _, err := ReadProcessPage(context.Background(), file, ProcessPageAsk{Limit: ProcessPageMax + 1}); err == nil {
		t.Fatal("limit over the maximum was accepted")
	}
}

func TestProcessUpdateWritesOneRecordNotTheHistory(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir, 1, 2, 3, 4, 5)
	two := filepath.Join(dir, processStoreName, processFileName(2))
	old := time.Now().Add(-time.Hour)
	for _, id := range []int{1, 2, 3, 4, 5} {
		_ = os.Chtimes(filepath.Join(dir, processStoreName, processFileName(id)), old, old)
	}
	s := processStore{dir: dir}
	root, head, err := s.open(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.put(root, &head, JobNotice{ID: 6, Kind: JobKindCommand, Command: "sleep 1", State: JobRunning, Started: time.Now()}, true); err != nil {
		t.Fatal(err)
	}
	root.Close()
	for _, id := range []int{1, 2, 3, 4, 5} {
		info, _ := os.Stat(filepath.Join(dir, processStoreName, processFileName(id)))
		if info.ModTime().After(old.Add(time.Second)) {
			t.Fatalf("record %d rewritten by an update of another job", id)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "processes.json")); !os.IsNotExist(err) {
		t.Fatal("the legacy whole array was written")
	}
	if _, err := os.Stat(two); err != nil {
		t.Fatal(err)
	}
}

func TestProcessSettledRecordIsNeverOverwrittenByLateRunningNotice(t *testing.T) {
	dir := t.TempDir()
	started := time.Now()
	s := processStore{dir: dir}
	root, head, _ := s.open(time.Second)
	defer root.Close()
	_, _ = s.put(root, &head, JobNotice{ID: 3, Kind: JobKindCommand, State: JobFailed, ExitCode: 2, Started: started, StartedBy: JobStartedByChat}, true)
	_, _ = s.put(root, &head, JobNotice{ID: 3, Kind: JobKindCommand, State: JobRunning, Started: started, Name: "late name"}, true)
	n, _, _ := readRecord(root, 3)
	if n.State != JobFailed || n.ExitCode != 2 || n.StartedBy != JobStartedByChat {
		t.Fatalf("settled record regressed: %+v", n)
	}
}

func TestProcessSavedRunningIsUnknownNeverStoppedOnThePage(t *testing.T) {
	dir := t.TempDir()
	s := processStore{dir: dir}
	root, head, _ := s.open(time.Second)
	_, _ = s.put(root, &head, JobNotice{ID: 9, Kind: JobKindCommand, Command: "npm run dev", State: JobRunning, Started: time.Now(), Elapsed: time.Minute}, true)
	root.Close()
	page, err := ReadProcessPage(context.Background(), filepath.Join(dir, "t.jsonl"), ProcessPageAsk{})
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("page: %+v %v", page, err)
	}
	row := page.Rows[0]
	if row.Unknown != ProcessUnknownNotHeld || row.State != JobRunning || !row.Ended.IsZero() || row.Live {
		t.Fatalf("not-held running row: %+v", row)
	}
	// The engine holding it, with no such live job, says the owner lost it.
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = filepath.Join(dir, "transcript.jsonl"); c.Place = Place{Dir: dir} })
	owned, err := agent.ProcessPage(context.Background(), ProcessPageAsk{})
	if err != nil || len(owned.Rows) != 1 || owned.Rows[0].Unknown != ProcessUnknownOwnerLost {
		t.Fatalf("owner page: %+v %v", owned, err)
	}
	// The legacy list keeps its old word, and invents no ending.
	legacy, err := agent.JobNotices()
	if err != nil || len(legacy) != 1 || legacy[0].State != JobStopped || !legacy[0].Ended.IsZero() {
		t.Fatalf("legacy list: %+v %v", legacy, err)
	}
}

func TestProcessCursorFromAnotherStoreIsStale(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir, 1, 2, 3)
	file := filepath.Join(dir, "t.jsonl")
	page, err := ReadProcessPage(context.Background(), file, ProcessPageAsk{Limit: 1})
	if err != nil || page.Next == "" {
		t.Fatalf("page: %+v %v", page, err)
	}
	if err := os.RemoveAll(filepath.Join(dir, processStoreName)); err != nil {
		t.Fatal(err)
	}
	seedStore(t, dir, 1, 2, 3)
	if _, err := ReadProcessPage(context.Background(), file, ProcessPageAsk{Limit: 1, Cursor: page.Next}); !errors.Is(err, ErrProcessStale) {
		t.Fatalf("rebuilt store accepted an old cursor: %v", err)
	}
	if _, err := ReadProcessPage(context.Background(), file, ProcessPageAsk{Cursor: "not-a-cursor"}); err == nil {
		t.Fatal("garbage cursor accepted")
	}
}

func TestProcessHolesBeyondTheProbeBudgetResumeNeverEnd(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir, 1, processProbeBudget*2+10)
	file := filepath.Join(dir, "t.jsonl")
	all, pages := walkPages(t, func(cursor string) (ProcessPage, error) {
		return ReadProcessPage(context.Background(), file, ProcessPageAsk{Cursor: cursor})
	})
	if len(all) != 2 || all[1].ID != 1 || pages < 3 {
		t.Fatalf("holes: rows=%d pages=%d", len(all), pages)
	}
}

func TestProcessDeadlineStopsTheWalkWithACursor(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir, 1, 2, 3)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	page, err := ReadProcessPage(ctx, filepath.Join(dir, "t.jsonl"), ProcessPageAsk{})
	if err != nil || page.Next == "" || len(page.Rows) != 0 {
		t.Fatalf("expired walk claimed an end: %+v %v", page, err)
	}
}

func TestProcessUnreadableRecordMakesThePageIncomplete(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir, 1, 2, 3)
	if err := os.WriteFile(filepath.Join(dir, processStoreName, processFileName(2)), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, processStoreName, processFileName(3))
	_ = os.Remove(link)
	if err := os.Symlink("/etc/passwd", link); err != nil {
		t.Fatal(err)
	}
	page, err := ReadProcessPage(context.Background(), filepath.Join(dir, "t.jsonl"), ProcessPageAsk{})
	if err != nil || !page.Incomplete || len(page.Rows) != 1 || page.Rows[0].ID != 1 {
		t.Fatalf("damaged history: %+v %v", page, err)
	}
}

func TestProcessLegacyArraysAreReadThenFoldedByTheOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	var rows []JobNotice
	for i := 1; i <= 120; i++ {
		rows = append(rows, JobNotice{ID: i, Kind: JobKindCommand, Command: "legacy", State: JobDone, Started: time.Now()})
	}
	rows = append(rows, JobNotice{ID: 500, Kind: JobKindTask, State: JobDone})
	raw, _ := json.Marshal(rows)
	legacy := filepath.Join(dir, "processes.json")
	_ = os.WriteFile(legacy, raw, 0o600)
	_ = os.MkdirAll(filepath.Join(dir, placeNodeJournals), 0o700)
	worker, _ := json.Marshal([]JobNotice{{ID: 121, Kind: JobKindCommand, Command: "worker legacy", State: JobFailed, ExitCode: 1, Started: time.Now()}})
	_ = os.WriteFile(filepath.Join(dir, placeNodeJournals, "processes.json"), worker, 0o600)
	file := filepath.Join(dir, "transcript.jsonl")

	read := func(cursor string) (ProcessPage, error) {
		return ReadProcessPage(context.Background(), file, ProcessPageAsk{Cursor: cursor})
	}
	all, _ := walkPages(t, read)
	if len(all) != 121 || all[0].ID != 121 || all[0].StartedBy != "" {
		t.Fatalf("unfolded legacy read: rows=%d first=%+v", len(all), all[0])
	}
	if _, err := os.Stat(filepath.Join(dir, processStoreName)); !os.IsNotExist(err) {
		t.Fatal("a reader that holds nothing created the store")
	}

	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = file; c.Place = Place{Dir: dir} })
	owned, _ := walkPages(t, func(cursor string) (ProcessPage, error) {
		return agent.ProcessPage(context.Background(), ProcessPageAsk{Cursor: cursor})
	})
	if len(owned) != 121 {
		t.Fatalf("folded rows: %d", len(owned))
	}
	after, _ := os.ReadFile(legacy)
	if string(after) != string(raw) {
		t.Fatal("folding rewrote the legacy array older builds read")
	}
	// A legacy array changed after folding is reported until its owner folds it.
	more := append(rows, JobNotice{ID: 130, Kind: JobKindCommand, Command: "older build", State: JobDone, Started: time.Now()})
	raw, _ = json.Marshal(more)
	_ = os.WriteFile(legacy, raw, 0o600)
	future := time.Now().Add(time.Minute)
	_ = os.Chtimes(legacy, future, future)
	page, _ := read("")
	if !page.Incomplete {
		t.Fatal("a legacy array changed after folding read as complete")
	}
	owner, _ := agent.ProcessPage(context.Background(), ProcessPageAsk{})
	if owner.Incomplete || owner.Rows[0].ID != 130 {
		t.Fatalf("owner refold: incomplete=%v first=%d", owner.Incomplete, owner.Rows[0].ID)
	}
}

func TestProcessLegacyElementOverTheCapIsCountedLost(t *testing.T) {
	dir := t.TempDir()
	huge := JobNotice{ID: 2, Kind: JobKindCommand, Command: strings.Repeat("x", 3*processRecordCap), State: JobDone}
	raw, _ := json.Marshal([]JobNotice{{ID: 1, Kind: JobKindCommand, Command: "ok", State: JobDone}, huge, {ID: 3, Kind: JobKindCommand, Command: "after", State: JobDone}})
	_ = os.WriteFile(filepath.Join(dir, "processes.json"), raw, 0o600)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SessionFile = filepath.Join(dir, "transcript.jsonl")
		c.Place = Place{Dir: dir}
	})
	page, err := agent.ProcessPage(context.Background(), ProcessPageAsk{})
	if err != nil || !page.Incomplete || len(page.Rows) != 2 || page.Rows[0].ID != 3 || page.Rows[1].ID != 1 {
		t.Fatalf("oversize legacy element took its neighbours with it: %+v %v", page, err)
	}
}

func TestProcessOversizeCommandIsKeptCutAndNeverRunAgain(t *testing.T) {
	dir := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SessionFile = filepath.Join(dir, "transcript.jsonl")
		c.Place = Place{Dir: dir}
	})
	agent.keepProcessNotice(JobNotice{ID: 4, Kind: JobKindCommand, Command: strings.Repeat("y", 2*processRecordCap), State: JobDone, Started: time.Now()})
	n, found, err := agent.ProcessRecord(4)
	if err != nil || !found || !n.CommandCut || len(n.Command) >= 2*processRecordCap {
		t.Fatalf("cut record: found=%v cut=%v len=%d err=%v", found, n.CommandCut, len(n.Command), err)
	}
	if _, err := agent.RerunProcess(4); err == nil {
		t.Fatal("a cut command was run again")
	}
}

func TestProcessRecordKeepsFolderEndingAndChatOwner(t *testing.T) {
	dir := t.TempDir()
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SessionFile = filepath.Join(dir, "transcript.jsonl")
		c.Place = Place{Dir: dir}
	})
	_ = workspace
	agent.config.SessionFile = agent.config.Place.Transcript()
	lane := rosterLane(t, agent)
	id := startJob(t, agent, "printf 'hi\\n'")
	awaitJob(t, lane, JobDone)
	n, found, err := ReadProcessRecord(agent.SessionPath(), id)
	if err != nil || !found {
		t.Fatalf("record: %v", err)
	}
	if n.Dir != agent.config.Workspace || n.Ended.IsZero() || n.StartedBy != JobStartedByChat || n.Task != 0 {
		t.Fatalf("record facts: dir=%q ended=%v by=%q task=%d", n.Dir, n.Ended, n.StartedBy, n.Task)
	}
}

func TestProcessTaskWorkerJobIsInTheConversationHistoryWithItsTask(t *testing.T) {
	dir := t.TempDir()
	conversation, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Place = Place{Dir: dir}
		c.SessionFile = Place{Dir: dir}.Transcript()
	})
	worker, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SessionFile = filepath.Join(dir, placeNodeJournals, "x_7.jsonl")
		c.droppings = Place{Dir: dir}
		c.taskID = 7
	})
	t.Cleanup(func() { _ = worker.Close() })
	id := startJob(t, worker, "sleep 30")
	page, err := conversation.ProcessPage(context.Background(), ProcessPageAsk{})
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("conversation page: %+v %v", page, err)
	}
	row := page.Rows[0]
	if row.ID != id || !row.Live || row.Unknown != "" || row.StartedBy != JobStartedByTask || row.Task != 7 {
		t.Fatalf("worker row: %+v", row)
	}
	if _, err := os.Stat(filepath.Join(dir, placeNodeJournals, "processes.json")); !os.IsNotExist(err) {
		t.Fatal("the worker still wrote the shelf nothing reads")
	}
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
	n, _, _ := ReadProcessRecord(conversation.SessionPath(), id)
	if n.State != JobStopped || n.StartedBy != JobStartedByTask || n.Task != 7 {
		t.Fatalf("closed worker record: %+v", n)
	}
}

// ── the log ────────────────────────────────────────────────────────────────

// managedLog writes a job's log where codeaf writes it and answers the record
// that names it.
func managedLog(t *testing.T, dir string, id int, body string) JobNotice {
	t.Helper()
	jobs := filepath.Join(dir, placeLogs, droppingJobs)
	if err := os.MkdirAll(jobs, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(jobs, strconv.Itoa(id)+".log")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return JobNotice{ID: id, LogPath: path}
}

func TestProcessLogReadsTailAndForwardByOffsetAtLineEnds(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < 2000; i++ {
		b.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	b.WriteString("partial")
	n := managedLog(t, dir, 1, b.String())
	tail, err := ReadProcessLog(dir, n, ProcessLogAsk{Limit: 100}, nil)
	if err != nil || !strings.HasPrefix(tail.Text, "line ") || !strings.HasSuffix(tail.Text, "line 1999\n") || tail.EOF || !tail.Partial || !tail.MoreBefore || tail.Next != tail.Size-7 {
		t.Fatalf("tail of a running log: %+v %v", tail, err)
	}
	zero := int64(0)
	head, err := ReadProcessLog(dir, n, ProcessLogAsk{Offset: &zero, Limit: 20}, strings.ToUpper)
	if err != nil || head.Text != "LINE 0\nLINE 1\n" || head.Next != 14 || head.EOF || head.Partial || head.MoreBefore {
		t.Fatalf("head: %+v %v", head, err)
	}
	next := head.Next
	again, err := ReadProcessLog(dir, n, ProcessLogAsk{Offset: &next, Limit: 7, Revision: head.Revision}, nil)
	if err != nil || again.Text != "line 2\n" {
		t.Fatalf("forward: %+v %v", again, err)
	}
	past := int64(1 << 30)
	if _, err := ReadProcessLog(dir, n, ProcessLogAsk{Offset: &past}, nil); !errors.Is(err, ErrProcessLogStale) {
		t.Fatalf("offset past the end: %v", err)
	}
	if _, err := ReadProcessLog(dir, n, ProcessLogAsk{Offset: &zero, Revision: "other"}, nil); !errors.Is(err, ErrProcessLogStale) {
		t.Fatalf("revision mismatch: %v", err)
	}
	if _, err := ReadProcessLog(dir, n, ProcessLogAsk{Limit: ProcessLogMaxBytes + 1}, nil); err == nil {
		t.Fatal("limit over the maximum accepted")
	}
}

// AN ENDED PROCESS'S LAST LINE IS SHOWN WITHOUT ITS NEWLINE, and a log that is
// nothing but one unterminated line is not empty.
func TestProcessLogShowsAnEndedProcesssUnterminatedLastLine(t *testing.T) {
	dir := t.TempDir()
	n := managedLog(t, dir, 2, "ready\nPassword:")
	got, err := ReadProcessLog(dir, n, ProcessLogAsk{Final: true}, nil)
	if err != nil || got.Text != "ready\nPassword:" || !got.EOF || got.Partial || got.Next != got.Size {
		t.Fatalf("ended tail: %+v %v", got, err)
	}
	only := managedLog(t, dir, 3, "x")
	zero := int64(0)
	got, err = ReadProcessLog(dir, only, ProcessLogAsk{Offset: &zero, Final: true}, nil)
	if err != nil || got.Text != "x" || !got.EOF {
		t.Fatalf("printf x: %+v %v", got, err)
	}
	got, err = ReadProcessLog(dir, only, ProcessLogAsk{Offset: &zero}, nil)
	if err != nil || got.Text != "" || got.EOF || !got.Partial || got.Next != 0 {
		t.Fatalf("running unterminated line: %+v %v", got, err)
	}
}

func TestProcessLogSplitsOnlyALineLongerThanTheWindowAtARune(t *testing.T) {
	dir := t.TempDir()
	n := managedLog(t, dir, 1, strings.Repeat("é", 100)+"\n")
	zero := int64(0)
	got, err := ReadProcessLog(dir, n, ProcessLogAsk{Offset: &zero, Limit: 11}, nil)
	if err != nil || got.Text != strings.Repeat("é", 5) || got.Next != 10 {
		t.Fatalf("split: %+v %v", got, err)
	}
}

// A LONG LINE OF INVALID BYTES STILL MOVES: the reader never trims a window
// to nothing and leaves the caller polling the same offset forever.
func TestProcessLogInvalidBytesInALongLineAlwaysAdvance(t *testing.T) {
	dir := t.TempDir()
	n := managedLog(t, dir, 1, strings.Repeat("\xff", 64)+"\n")
	offset := int64(0)
	for reads := 0; ; reads++ {
		got, err := ReadProcessLog(dir, n, ProcessLogAsk{Offset: &offset, Limit: 10}, nil)
		if err != nil || got.Next <= offset && !got.EOF {
			t.Fatalf("read %d stuck at %d: %+v %v", reads, offset, got, err)
		}
		if got.EOF {
			break
		}
		offset = got.Next
		if reads > 20 {
			t.Fatal("never reached the end")
		}
	}
	tiny := managedLog(t, dir, 2, "é\n")
	zero := int64(0)
	got, err := ReadProcessLog(dir, tiny, ProcessLogAsk{Offset: &zero, Limit: 1}, nil)
	if err != nil || got.Next != 1 {
		t.Fatalf("a window smaller than a character: %+v %v", got, err)
	}
}

// ONLY THE LOG CODEAF WROTE FOR THAT JOB IS OPENED: a record naming another
// file, a link at any folder below the conversation, a link or FIFO at the log
// itself are all refused; a missing one is gone.
func TestProcessLogOpensOnlyTheManagedLogOfThatJob(t *testing.T) {
	dir := t.TempDir()
	n := managedLog(t, dir, 4, "ok\n")
	for _, path := range []string{"/etc/passwd", filepath.Join(dir, placeLogs, droppingJobs, "5.log"), filepath.Join(dir, "x", "..", placeLogs, droppingJobs, "../jobs/../../4.log")} {
		if _, err := ReadProcessLog(dir, JobNotice{ID: 4, LogPath: path}, ProcessLogAsk{}, nil); !ErrProcessLogUnmanaged(err) {
			t.Fatalf("record naming %s: %v", path, err)
		}
	}
	if got, err := ReadProcessLog(dir, n, ProcessLogAsk{}, nil); err != nil || got.Text != "ok\n" {
		t.Fatalf("managed: %+v %v", got, err)
	}
	// A logs folder swapped for a link to elsewhere is not followed.
	other := t.TempDir()
	managedLog(t, other, 4, "secret\n")
	moved := t.TempDir()
	if err := os.Rename(filepath.Join(dir, placeLogs), filepath.Join(moved, placeLogs)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(other, placeLogs), filepath.Join(dir, placeLogs)); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadProcessLog(dir, n, ProcessLogAsk{}, nil); err == nil {
		t.Fatalf("followed a linked logs folder: %+v", got)
	}
	_ = os.Remove(filepath.Join(dir, placeLogs))
	_ = os.Rename(filepath.Join(moved, placeLogs), filepath.Join(dir, placeLogs))
	_ = os.Remove(n.LogPath)
	if err := os.Symlink("/etc/passwd", n.LogPath); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadProcessLog(dir, n, ProcessLogAsk{}, nil); err == nil {
		t.Fatal("followed a link at the log")
	}
	_ = os.Remove(n.LogPath)
	if err := syscall.Mkfifo(n.LogPath, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := ReadProcessLog(dir, n, ProcessLogAsk{}, nil); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("read a FIFO")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked on a FIFO")
	}
	_ = os.Remove(n.LogPath)
	if gone, err := ReadProcessLog(dir, n, ProcessLogAsk{}, nil); err != nil || !gone.Gone {
		t.Fatalf("gone: %+v %v", gone, err)
	}
	if gone, err := ReadProcessLog(dir, JobNotice{ID: 9}, ProcessLogAsk{}, nil); err != nil || !gone.Gone {
		t.Fatalf("no log named: %+v %v", gone, err)
	}
}

// ── the review's blockers ──────────────────────────────────────────────────

// A RECORD SWAPPED FOR A FIFO NEVER HOLDS A READER (or the record lock).
func TestProcessRecordSwappedForAFIFOIsSkippedWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir, 1, 2, 3)
	record := filepath.Join(dir, processStoreName, processFileName(2))
	_ = os.Remove(record)
	if err := syscall.Mkfifo(record, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan ProcessPage, 1)
	go func() {
		page, _ := ReadProcessPage(context.Background(), filepath.Join(dir, "t.jsonl"), ProcessPageAsk{})
		done <- page
	}()
	select {
	case page := <-done:
		if !page.Incomplete || len(page.Rows) != 2 {
			t.Fatalf("fifo record: %+v", page)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a FIFO record blocked the page")
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = filepath.Join(dir, "transcript.jsonl"); c.Place = Place{Dir: dir} })
	wrote := make(chan struct{})
	go func() {
		agent.keepProcessNotice(JobNotice{ID: 2, Kind: JobKindCommand, State: JobDone, Started: time.Now()})
		close(wrote)
	}()
	select {
	case <-wrote:
	case <-time.After(5 * time.Second):
		t.Fatal("a FIFO record blocked the writer")
	}
	if n, found, err := ReadProcessRecord(agent.SessionPath(), 2); err != nil || !found || n.State != JobDone {
		t.Fatalf("the write did not replace the FIFO: %+v %v %v", n, found, err)
	}
}

// THE WHOLE LIST NEVER PRESENTS PART OF A HISTORY AS ALL OF IT, and never
// spins: too long, or damaged, it says so.
func TestProcessLegacyListRefusesAHistoryItCannotCarryWhole(t *testing.T) {
	dir := t.TempDir()
	var ids []int
	for i := 1; i <= processLegacyListMax+5; i++ {
		ids = append(ids, i)
	}
	seedStore(t, dir, ids...)
	file := filepath.Join(dir, "t.jsonl")
	if _, err := ReadProcessNotices(file); !errors.Is(err, ErrProcessListPartial) {
		t.Fatalf("a list over the cap: %v", err)
	}
	small := t.TempDir()
	seedStore(t, small, 1, 2, processProbeBudget*3)
	rows, err := ReadProcessNotices(filepath.Join(small, "t.jsonl"))
	if err != nil || len(rows) != 3 {
		t.Fatalf("holes walked: %d %v", len(rows), err)
	}
	_ = os.WriteFile(filepath.Join(small, processStoreName, processFileName(2)), []byte("{"), 0o600)
	if _, err := ReadProcessNotices(filepath.Join(small, "t.jsonl")); !errors.Is(err, ErrProcessListPartial) {
		t.Fatalf("a damaged history listed as whole: %v", err)
	}
}

// A FOLD CUT BY ITS BUDGET RESUMES FROM THE BYTE IT STOPPED AT; AN OVERSIZE
// ELEMENT IS SKIPPED, NOT FATAL; A CORRUPT TAIL IS SAID, NOT FORGOTTEN.
func TestProcessLegacyScanResumesSkipsAndSaysCorrupt(t *testing.T) {
	dir := t.TempDir()
	var rows []JobNotice
	for i := 1; i <= 40; i++ {
		rows = append(rows, JobNotice{ID: i, Kind: JobKindCommand, Command: "c " + strconv.Itoa(i), State: JobDone})
	}
	rows[10].Command = strings.Repeat("[\"x]", processRecordCap)
	raw, _ := json.MarshalIndent(rows, "", " ")
	path := filepath.Join(dir, "processes.json")
	_ = os.WriteFile(path, raw, 0o600)
	seen := map[int]bool{}
	from, passes, lost := int64(0), 0, 0
	for {
		scan, err := scanLegacy(path, from, 300, time.Time{}, func(n JobNotice) error { seen[n.ID] = true; return nil })
		passes++
		lost += scan.lost
		if scan.done {
			if err != nil || scan.corrupt {
				t.Fatalf("end: %+v %v", scan, err)
			}
			break
		}
		if !errors.Is(err, errLegacyBudget) || scan.next <= from {
			t.Fatalf("pass %d did not move: %+v %v", passes, scan, err)
		}
		from = scan.next
	}
	if len(seen) != 39 || seen[11] || lost != 1 || passes < 5 {
		t.Fatalf("resumed fold: kept=%d oversize kept=%v lost=%d passes=%d", len(seen), seen[11], lost, passes)
	}
	cut := raw[:len(raw)/2]
	_ = os.WriteFile(path, cut, 0o600)
	scan, err := scanLegacy(path, 0, 1<<30, time.Time{}, func(JobNotice) error { return nil })
	if err != nil || !scan.corrupt || scan.done {
		t.Fatalf("cut array: %+v %v", scan, err)
	}
}

// THE OWNER'S FOLD CARRIES ON ACROSS WRITES until it reaches the end; a page
// before then says incomplete, and the head records where it stopped.
func TestProcessOwnerFoldResumesAcrossOpens(t *testing.T) {
	dir := t.TempDir()
	var rows []JobNotice
	for i := 1; i <= 300; i++ {
		rows = append(rows, JobNotice{ID: i, Kind: JobKindCommand, Command: "c", State: JobDone})
	}
	raw, _ := json.Marshal(rows)
	_ = os.WriteFile(filepath.Join(dir, "processes.json"), raw, 0o600)
	s := processStore{dir: dir}
	root, head, err := s.open(0)
	if err != nil {
		t.Fatal(err)
	}
	root.Close()
	mark, _ := head.markFor("processes.json")
	if mark.Done || !s.legacyPending(head) {
		t.Fatalf("a fold with no time claimed the end: %+v", mark)
	}
	for i := 0; i < 50 && s.legacyPending(head); i++ {
		root, head, err = s.open(time.Second)
		if err != nil {
			t.Fatal(err)
		}
		root.Close()
	}
	page, err := ReadProcessPage(context.Background(), filepath.Join(dir, "t.jsonl"), ProcessPageAsk{Limit: 1})
	if err != nil || page.Incomplete || page.Rows[0].ID != 300 || head.High != 300 {
		t.Fatalf("folded: %+v high=%d %v", page, head.High, err)
	}
}

// AN UNREADABLE HEAD IS REBUILT FROM THE RECORDS, with a new epoch, so the
// store keeps recording and old cursors are stale.
func TestProcessUnreadableHeadIsRebuiltNotFatal(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir, 1, 2, 7)
	page, _ := ReadProcessPage(context.Background(), filepath.Join(dir, "t.jsonl"), ProcessPageAsk{Limit: 1})
	_ = os.WriteFile(filepath.Join(dir, processStoreName, processHeadName), []byte("garbage"), 0o600)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = filepath.Join(dir, "transcript.jsonl"); c.Place = Place{Dir: dir} })
	agent.keepProcessNotice(JobNotice{ID: 8, Kind: JobKindCommand, State: JobDone, Started: time.Now()})
	all, _ := walkPages(t, func(cursor string) (ProcessPage, error) {
		return ReadProcessPage(context.Background(), agent.SessionPath(), ProcessPageAsk{Cursor: cursor})
	})
	if len(all) != 4 || all[0].ID != 8 || all[1].ID != 7 {
		t.Fatalf("rebuilt: %+v", all)
	}
	if page, _ := ReadProcessPage(context.Background(), agent.SessionPath(), ProcessPageAsk{}); !page.Incomplete {
		t.Fatal("a rebuilt history claimed to be complete")
	}
	if _, err := ReadProcessPage(context.Background(), agent.SessionPath(), ProcessPageAsk{Cursor: page.Next}); !errors.Is(err, ErrProcessStale) {
		t.Fatalf("an old cursor survived the rebuild: %v", err)
	}
}

// LIVE ROWS ARE PAGED, AND NO LIVE JOB IS EVER ANSWERED TWICE. Every live job
// also has a saved record (its first notice writes one), so the whole walk —
// live pages, the turn to the saved history, every history page — is checked
// as one sequence, at limits that end a page exactly on the last live row, on
// a live row and on a history row.
func TestProcessLiveRowsArePagedBeforeTheHistory(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir, 1, 2, 3)
	s := processStore{dir: dir}
	root, head, _ := s.open(time.Second)
	for id := 21; id <= 30; id++ {
		_, _ = s.put(root, &head, JobNotice{ID: id, Kind: JobKindCommand, State: JobRunning, Started: time.Unix(int64(id), 0)}, false)
	}
	root.Close()
	var live []ProcessPageRow
	for id := 30; id > 20; id-- {
		live = append(live, ProcessPageRow{JobNotice: JobNotice{ID: id, State: JobRunning, Started: time.Unix(int64(id), 0)}, Live: true})
	}
	want := []int{30, 29, 28, 27, 26, 25, 24, 23, 22, 21, 3, 2, 1}
	for _, limit := range []int{1, 3, 4, 5, 10, 11, 13, 100} {
		var got []int
		cursor := ""
		for pages := 0; pages < 40; pages++ {
			page, err := s.page(context.Background(), ProcessPageAsk{Cursor: cursor}, limit, nil, live, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Rows) > limit {
				t.Fatalf("limit %d: page of %d", limit, len(page.Rows))
			}
			for _, r := range page.Rows {
				got = append(got, r.ID)
			}
			if page.Next == "" {
				break
			}
			cursor = page.Next
		}
		if len(got) != len(want) {
			t.Fatalf("limit %d: %v", limit, got)
		}
		for k := range want {
			if got[k] != want[k] {
				t.Fatalf("limit %d: %v", limit, got)
			}
		}
	}
}

// AN UNREADABLE HEAD IS REBUILT; A HEAD THAT MERELY COULD NOT BE OPENED IS
// LEFT ALONE, and a rebuild keeps to its deadline.
func TestProcessHeadIsRebuiltOnlyWhenItIsNotOne(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permissions do not refuse root")
	}
	dir := t.TempDir()
	seedStore(t, dir, 1, 2)
	s := processStore{dir: dir}
	path := filepath.Join(dir, processStoreName, processHeadName)
	before, _ := os.ReadFile(path)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.open(time.Second); err == nil {
		t.Fatal("an unopenable head was answered")
	}
	_ = os.Chmod(path, 0o600)
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("a good head was replaced: %v", err)
	}
	root, _ := s.openRoot(false)
	defer root.Close()
	start := time.Now()
	head, err := s.rebuildHead(root, time.Now().Add(-time.Second))
	if err != nil || head.Lost < 1 || time.Since(start) > time.Second {
		t.Fatalf("an expired rebuild: %+v %v %v", head, err, time.Since(start))
	}
}

// AN OWNER'S PAGE WAITS FOR THE RECORD LOCK ONLY WITHIN ITS SHARE OF THE
// DEADLINE, then reads what is on disk without creating or folding anything.
func TestProcessOwnerPageDoesNotQueueBehindAHeldRecordLock(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir, 1, 2)
	raw, _ := json.Marshal([]JobNotice{{ID: 9, Kind: JobKindCommand, State: JobDone}})
	_ = os.WriteFile(filepath.Join(dir, "processes.json"), raw, 0o600)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = filepath.Join(dir, "transcript.jsonl"); c.Place = Place{Dir: dir} })
	processRecordMu.Lock()
	defer processRecordMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	page, err := agent.ProcessPage(ctx, ProcessPageAsk{})
	if took := time.Since(start); took > time.Second {
		t.Fatalf("the page queued behind the lock for %v", took)
	}
	if err != nil || len(page.Rows) != 2 || !page.Incomplete {
		t.Fatalf("read-only page under a held lock: %+v %v", page, err)
	}
}

// A RECORD READ GOES THROUGH THE ROOT ALREADY OPENED: a store folder swapped
// for a link afterwards does not redirect it.
func TestProcessRecordReadsStayInTheOpenedStore(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir, 1)
	root, err := processStore{dir: dir}.openRoot(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	other := t.TempDir()
	s := processStore{dir: other}
	r2, head, _ := s.open(time.Second)
	_, _ = s.put(r2, &head, JobNotice{ID: 1, Kind: JobKindCommand, Command: "elsewhere", State: JobDone}, false)
	r2.Close()
	if err := os.Rename(filepath.Join(dir, processStoreName), filepath.Join(dir, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(other, processStoreName), filepath.Join(dir, processStoreName)); err != nil {
		t.Fatal(err)
	}
	n, found, err := readRecord(root, 1)
	if err != nil || !found || n.Command == "elsewhere" {
		t.Fatalf("a swapped store redirected the read: %+v %v %v", n, found, err)
	}
	if _, err := (processStore{dir: dir}).openRoot(false); err == nil {
		t.Fatal("a store that is a link was opened")
	}
}
