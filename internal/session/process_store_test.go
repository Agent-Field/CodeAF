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
	root, head, err := s.open()
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
	root, head, err := s.open()
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
	root, head, _ := s.open()
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
	root, head, _ := s.open()
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
	if err != nil || !page.Incomplete || len(page.Rows) == 0 || page.Rows[len(page.Rows)-1].ID != 1 {
		t.Fatalf("oversize legacy: %+v %v", page, err)
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

func TestProcessLogReadsTailAndForwardByOffsetAtLineEnds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "1.log")
	var b strings.Builder
	for i := 0; i < 2000; i++ {
		b.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	b.WriteString("partial")
	_ = os.WriteFile(path, []byte(b.String()), 0o600)
	tail, err := ReadProcessLog(path, ProcessLogAsk{Limit: 100}, nil)
	if err != nil || !strings.HasPrefix(tail.Text, "line ") || !strings.HasSuffix(tail.Text, "line 1999\n") || !tail.EOF || !tail.MoreBefore {
		t.Fatalf("tail: %+v %v", tail, err)
	}
	zero := int64(0)
	head, err := ReadProcessLog(path, ProcessLogAsk{Offset: &zero, Limit: 20}, strings.ToUpper)
	if err != nil || head.Text != "LINE 0\nLINE 1\n" || head.Next != 14 || head.EOF || head.MoreBefore {
		t.Fatalf("head: %+v %v", head, err)
	}
	next := head.Next
	again, err := ReadProcessLog(path, ProcessLogAsk{Offset: &next, Limit: 7, Revision: head.Revision}, nil)
	if err != nil || again.Text != "line 2\n" {
		t.Fatalf("forward: %+v %v", again, err)
	}
	past := int64(1 << 30)
	if _, err := ReadProcessLog(path, ProcessLogAsk{Offset: &past}, nil); !errors.Is(err, ErrProcessLogStale) {
		t.Fatalf("offset past the end: %v", err)
	}
	if _, err := ReadProcessLog(path, ProcessLogAsk{Offset: &zero, Revision: "other"}, nil); !errors.Is(err, ErrProcessLogStale) {
		t.Fatalf("revision mismatch: %v", err)
	}
	if _, err := ReadProcessLog(path, ProcessLogAsk{Limit: ProcessLogMaxBytes + 1}, nil); err == nil {
		t.Fatal("limit over the maximum accepted")
	}
}

func TestProcessLogSplitsOnlyALineLongerThanTheWindowAtARune(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "1.log")
	_ = os.WriteFile(path, []byte(strings.Repeat("é", 100)+"\n"), 0o600)
	zero := int64(0)
	got, err := ReadProcessLog(path, ProcessLogAsk{Offset: &zero, Limit: 11}, nil)
	if err != nil || got.Text != strings.Repeat("é", 5) || got.Next != 10 {
		t.Fatalf("split: %+v %v", got, err)
	}
}

func TestProcessLogRefusesLinksAndSpecialFilesAndSaysGone(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link.log")
	_ = os.Symlink("/etc/passwd", link)
	if _, err := ReadProcessLog(link, ProcessLogAsk{}, nil); err == nil {
		t.Fatal("followed a link")
	}
	fifo := filepath.Join(dir, "fifo.log")
	if err := syscall.Mkfifo(fifo, 0o600); err == nil {
		if _, err := ReadProcessLog(fifo, ProcessLogAsk{}, nil); err == nil {
			t.Fatal("read a FIFO")
		}
	}
	gone, err := ReadProcessLog(filepath.Join(dir, "expired.log"), ProcessLogAsk{}, nil)
	if err != nil || !gone.Gone || gone.Text != "" {
		t.Fatalf("gone: %+v %v", gone, err)
	}
}
