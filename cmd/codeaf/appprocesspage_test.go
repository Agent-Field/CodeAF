package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/service"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// letGo makes the backend treat the agent as not held, reading its history
// from transcript, and puts the conversation back for the backend's close.
func letGo(t *testing.T, backend *appBackend, id, transcript string) {
	t.Helper()
	a, err := backend.find(id)
	if err != nil {
		t.Fatal(err)
	}
	var held *appLive
	var was string
	backend.locked(func() { held, was = a.live, a.transcript; a.live, a.transcript = nil, transcript })
	t.Cleanup(func() { backend.locked(func() { a.live, a.transcript = held, was }) })
}

// seedRecords writes a conversation's process store the way the engine
// writes it: one record per job and a head.
func seedRecords(t *testing.T, dir string, rows ...session.JobNotice) {
	t.Helper()
	store := filepath.Join(dir, "processes")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	high := 0
	for _, n := range rows {
		raw, _ := json.Marshal(n)
		if err := os.WriteFile(filepath.Join(store, strconv.Itoa(n.ID)+".json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		high = max(high, n.ID)
	}
	head, _ := json.Marshal(map[string]any{"epoch": "e1", "high": high})
	if err := os.WriteFile(filepath.Join(store, "head.json"), head, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A CONVERSATION NOBODY HERE HOLDS IS READ OFF ITS STORE, and a record saved as
// running is unknown — no exit code, no ending — while a settled one keeps its
// folder, its ending and who started it, a task by the app's own task id.
func TestProcessPageOfANotHeldConversationIsTruthfulAndPaged(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{})
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	jobs := filepath.Join(dir, "logs", "jobs")
	_ = os.MkdirAll(jobs, 0o700)
	log := filepath.Join(jobs, "2.log")
	_ = os.WriteFile(log, []byte("compiling\nlistening on localhost:3000\n"), 0o600)
	ended := filepath.Join(jobs, "3.log")
	_ = os.WriteFile(ended, []byte("built\nPassword:"), 0o600)
	started := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	var rows []session.JobNotice
	for i := 3; i <= 60; i++ {
		rows = append(rows, session.JobNotice{ID: i, Kind: session.JobKindCommand, Command: "make " + strconv.Itoa(i), State: session.JobDone, Started: started.Add(time.Duration(i) * time.Minute), Ended: started.Add(time.Duration(i)*time.Minute + time.Second), LogPath: filepath.Join(jobs, strconv.Itoa(i)+".log")})
	}
	rows = append(rows,
		session.JobNotice{ID: 2, Kind: session.JobKindCommand, Command: "npm run dev", State: session.JobRunning, Started: started, Elapsed: time.Hour, LogPath: log, Dir: "/work/site", StartedBy: session.JobStartedByTask, Task: 4},
		session.JobNotice{ID: 1, Kind: session.JobKindCommand, Command: "go test", State: session.JobFailed, ExitCode: 1, Started: started, Ended: started.Add(time.Minute), StartedBy: session.JobStartedByChat, LogPath: filepath.Join(jobs, "1.log")},
	)
	seedRecords(t, dir, rows...)
	letGo(t, backend, agent.ID, transcript)

	ctx := context.Background()
	first, err := backend.PageProcesses(ctx, appwire.ProcessPageAsk{Agent: agent.ID})
	if err != nil || len(first.Processes) != appwire.ProcessPageLimitDefault || first.Next == "" || first.Revision != "e1" {
		t.Fatalf("first page: n=%d next=%q err=%v", len(first.Processes), first.Next, err)
	}
	second, err := backend.PageProcesses(ctx, appwire.ProcessPageAsk{Agent: agent.ID, Cursor: first.Next})
	if err != nil || second.Next != "" || len(second.Processes) != 10 {
		t.Fatalf("second page: n=%d next=%q err=%v", len(second.Processes), second.Next, err)
	}
	running, failed := second.Processes[8], second.Processes[9]
	if running.State != appwire.ProcessUnknown || running.UnknownReason != appwire.ProcessUnknownNotHeld || running.EndedAt != nil || running.ExitCode != nil {
		t.Fatalf("saved running row: %+v", running)
	}
	if running.Dir != "/work/site" || running.LogPath != log || running.LastLine != "listening on localhost:3000" || running.StartedBy == nil || running.StartedBy.Task != agent.ID+":4" {
		t.Fatalf("running facts: %+v starter=%+v", running, running.StartedBy)
	}
	if failed.State != appwire.ProcessFailed || failed.ExitCode == nil || *failed.ExitCode != 1 || failed.EndedAt == nil || !failed.EndedAt.Equal(started.Add(time.Minute)) || failed.StartedBy.Kind != "chat" || !failed.LogGone {
		t.Fatalf("failed row: %+v", failed)
	}
	if _, err := os.Stat(filepath.Join(dir, "processes.json")); !os.IsNotExist(err) {
		t.Fatal("a read wrote the legacy shelf")
	}

	// The log is read from the record by exact id, bounded, by offset.
	out, err := backend.ReadProcessLog(ctx, appwire.ProcessLogAsk{ID: running.ID})
	if err != nil || out.Text != "compiling\nlistening on localhost:3000\n" || !out.EOF || out.Next != out.Size {
		t.Fatalf("log tail: %+v %v", out, err)
	}
	ten := 10
	zero := int64(0)
	part, err := backend.ReadProcessLog(ctx, appwire.ProcessLogAsk{ID: running.ID, Offset: &zero, LimitBytes: &ten})
	if err != nil || part.Text != "compiling\n" || part.Next != 10 || part.EOF {
		t.Fatalf("log window: %+v %v", part, err)
	}
	three := appProcessID(agent.ID, rows[0])
	last, err := backend.ReadProcessLog(ctx, appwire.ProcessLogAsk{ID: three})
	if err != nil || last.Text != "built\nPassword:" || !last.EOF || last.Partial {
		t.Fatalf("an ended process's last unterminated line: %+v %v", last, err)
	}
	running2, err := backend.ReadProcessLog(ctx, appwire.ProcessLogAsk{ID: running.ID, Offset: &zero, LimitBytes: &ten})
	if err != nil || running2.Partial {
		t.Fatalf("running window: %+v %v", running2, err)
	}
	gone, err := backend.ReadProcessLog(ctx, appwire.ProcessLogAsk{ID: failed.ID})
	if err != nil || !gone.LogGone {
		t.Fatalf("gone log: %+v %v", gone, err)
	}
	other := agent.ID + ":p2.zzz"
	if _, err := backend.ReadProcessLog(ctx, appwire.ProcessLogAsk{ID: other}); !isCode(err, appwire.CodeNotFound) {
		t.Fatalf("another run of the same number: %v", err)
	}
	_ = os.Remove(log)
	_ = os.Symlink("/etc/passwd", log)
	if out, err := backend.ReadProcessLog(ctx, appwire.ProcessLogAsk{ID: running.ID}); err == nil {
		t.Fatalf("followed a link the record names: %+v", out)
	}
	// A record naming a file outside its conversation's logs folder is refused.
	seedRecords(t, dir, append(rows, session.JobNotice{ID: 61, Kind: session.JobKindCommand, State: session.JobDone, Started: started, LogPath: "/etc/passwd"})...)
	if out, err := backend.ReadProcessLog(ctx, appwire.ProcessLogAsk{ID: appProcessID(agent.ID, session.JobNotice{ID: 61, Started: started})}); !isCode(err, appwire.CodeEngine) {
		t.Fatalf("an unmanaged log path was read: %+v %v", out, err)
	}
}

// THE WHOLE LIST SAYS TOO LARGE RATHER THAN ANSWERING PART OF A LONG HISTORY.
func TestProcessListOfALongHistoryIsTooLargeNotCut(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{})
	dir := t.TempDir()
	var rows []session.JobNotice
	for i := 1; i <= 1005; i++ {
		rows = append(rows, session.JobNotice{ID: i, Kind: session.JobKindCommand, State: session.JobDone, Started: time.Now()})
	}
	seedRecords(t, dir, rows...)
	letGo(t, backend, agent.ID, filepath.Join(dir, "transcript.jsonl"))
	if list, err := backend.ListProcesses(agent.ID); !isCode(err, appwire.CodeTooLarge) {
		t.Fatalf("a cut list: %d %v", len(list), err)
	}
	page, err := backend.PageProcesses(context.Background(), appwire.ProcessPageAsk{Agent: agent.ID})
	if err != nil || len(page.Processes) != appwire.ProcessPageLimitDefault || page.Next == "" {
		t.Fatalf("the page still reads it: %d %v", len(page.Processes), err)
	}
}

func isCode(err error, code string) bool {
	wire, ok := err.(*appwire.Error)
	return ok && wire.Code == code
}

// A HELD CONVERSATION WHOSE ENGINE CANNOT PAGE IS REFUSED, NEVER PAGED FROM
// ITS WHOLE LIST: processes.page and processes.log answer unknown_method and
// the old engine's list is not read at all; processes.list still lists it.
func TestProcessPageOfAnEngineThatCannotPageIsRefusedWithoutItsList(t *testing.T) {
	conv := &fakeConversation{jobs: []session.JobNotice{{ID: 1, Kind: session.JobKindCommand, State: session.JobDone, Started: time.Now()}}}
	backend, _, agent := workBackend(t, conv)
	ctx := context.Background()
	if _, err := backend.PageProcesses(ctx, appwire.ProcessPageAsk{Agent: agent.ID}); !isCode(err, appwire.CodeUnknownMethod) {
		t.Fatalf("page of an engine that cannot page: %v", err)
	}
	if _, err := backend.ReadProcessLog(ctx, appwire.ProcessLogAsk{ID: appProcessID(agent.ID, conv.jobs[0])}); !isCode(err, appwire.CodeUnknownMethod) {
		t.Fatalf("log of an engine that cannot page: %v", err)
	}
	conv.mu.Lock()
	calls := conv.jobListCalls
	conv.mu.Unlock()
	if calls != 0 {
		t.Fatalf("a bounded door read the whole list %d times", calls)
	}
	if list, err := backend.ListProcesses(agent.ID); err != nil || len(list) != 1 {
		t.Fatalf("the legacy list changed: %+v %v", list, err)
	}
	if _, err := backend.PageProcesses(ctx, appwire.ProcessPageAsk{Agent: agent.ID, Crew: "c1"}); !isCode(err, appwire.CodeBadParams) {
		t.Fatalf("crew scope: %v", err)
	}
}

// blockingPager is an owner engine that pages only when its caller's
// deadline allows: it waits for the context.
type blockingPager struct {
	*fakeConversation
	asked chan struct{}
}

func (b blockingPager) ProcessPage(ctx context.Context, _ session.ProcessPageAsk) (session.ProcessPage, error) {
	close(b.asked)
	<-ctx.Done()
	return session.ProcessPage{}, ctx.Err()
}

// THE OWNER IS ASKED WITHIN THE PAGE'S DEADLINE: a cancelled or expired page
// stops at its context and never falls back to the whole list.
func TestProcessPageStopsAtItsContextWithAnOwnerThatDoesNotAnswer(t *testing.T) {
	conv := &fakeConversation{}
	pager := blockingPager{fakeConversation: conv, asked: make(chan struct{})}
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: pager, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	t.Cleanup(func() { backend.Close() })
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = backend.PageProcesses(ctx, appwire.ProcessPageAsk{Agent: agent.ID})
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("an unanswering owner held the page: %v after %v", err, time.Since(start))
	}
	conv.mu.Lock()
	calls := conv.jobListCalls
	conv.mu.Unlock()
	if calls != 0 {
		t.Fatal("an unanswering owner sent the page to the whole list")
	}
}

// THE ROW BOUND IS ON THE ENCODED BYTES: text that JSON writes six bytes per
// character is cut until the row really is under its bound, and says cut.
func TestProcessPageRowBoundHoldsUnderJSONEscaping(t *testing.T) {
	nasty := strings.Repeat("<>&\x01\x1f\"\\", 400<<10)
	p := appwire.Process{ID: "a:p1", Agent: "a", State: appwire.ProcessEnded,
		Title: nasty, Command: nasty, LastLine: nasty, Reason: nasty, Dir: nasty, LogPath: nasty, URL: nasty,
		Service: &appwire.Service{Name: nasty, Dir: nasty, OpenURL: nasty, Reason: nasty, Episode: nasty, EpisodeReason: nasty, StoppedBy: nasty,
			StartedBy: appwire.ServiceOrigin{Label: nasty, CallID: nasty},
			Health:    appwire.ServiceHealth{Kind: "cmd", Cmd: nasty, URL: nasty, Words: nasty},
			Check:     &appwire.ServiceCheck{Detail: nasty}},
	}
	bounded, n, ok := appBoundRow(p)
	raw, _ := json.Marshal(bounded)
	if !ok || !bounded.Cut || len(raw) != n || len(raw) > appProcessRowMax {
		t.Fatalf("escaped row: ok=%v cut=%v bytes=%d claimed=%d bound=%d", ok, bounded.Cut, len(raw), n, appProcessRowMax)
	}
	// And a page of such rows still walks to its end, every page under 4 MiB.
	svc := &fakeServices{ids: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}, command: nasty}
	eng := &fakeEngine{rows: engineRows(1, 2), rev: "e1"}
	sameIDs(t, "escaped rows", walkComposed(t, svc, eng, 100, appwire.ProcessPageReplyMax), wantComposed(svc, eng))
}

// A PAGED SERVICE OPENS NO FILE: its log path may be a FIFO or a link to
// anything, and the page neither blocks on it nor carries a byte of it.
func TestProcessPageServiceRowReadsNoTail(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	_ = os.WriteFile(marker, []byte("SECRET-MARKER\n"), 0o600)
	link := filepath.Join(dir, "link.log")
	_ = os.Symlink(marker, link)
	fifo := filepath.Join(dir, "fifo.log")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, fifo} {
		done := make(chan appwire.Process, 1)
		go func() {
			done <- appServiceProcessBase(service.Row{ID: "s1", Owner: "a", Name: "web", LogPath: path}, false)
		}()
		select {
		case p := <-done:
			raw, _ := json.Marshal(p)
			if p.LastLine != "" || strings.Contains(string(raw), "SECRET-MARKER") {
				t.Fatalf("%s: a paged service read its tail: %s", path, raw)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: a paged service opened its log", path)
		}
	}
}

// fakeServices is an agent's services in id order, paged as the supervisor
// pages them, with a membership revision.
type fakeServices struct {
	ids     []int
	command string
}

func (f *fakeServices) page(after string, n int) ([]appwire.Process, bool, string) {
	rev := fmt.Sprint(f.ids)
	start := 0
	if after != "" {
		a, _ := strconv.Atoi(strings.TrimPrefix(after, "s"))
		for start < len(f.ids) && f.ids[start] <= a {
			start++
		}
	}
	var out []appwire.Process
	for k := start; k < len(f.ids); k++ {
		if len(out) == n {
			return out, true, rev
		}
		out = append(out, appwire.Process{ID: "a:s" + strconv.Itoa(f.ids[k]), Agent: "a", Command: f.command, State: appwire.ProcessRunning, Service: &appwire.Service{Name: "svc"}})
	}
	return out, false, rev
}

// fakeEngine answers rows in walk order and honours exactly the cursors the
// page and the engine itself give: the one that begins with a row, in its
// phase.
type fakeEngine struct {
	rows []session.ProcessPageRow
	rev  string
}

func (f *fakeEngine) at(cursor string) (int, bool) {
	if cursor == "" {
		return 0, true
	}
	for k, r := range f.rows {
		if appEngineCursorAt(f.rev, r) == cursor {
			return k, true
		}
	}
	return 0, false
}

func (f *fakeEngine) page(_ context.Context, ask session.ProcessPageAsk) (session.ProcessPage, error) {
	k, ok := f.at(ask.Cursor)
	if !ok {
		return session.ProcessPage{}, fmt.Errorf("that cursor is not one a page gave: %q", ask.Cursor)
	}
	page := session.ProcessPage{Revision: f.rev}
	for ; k < len(f.rows); k++ {
		if len(page.Rows) == ask.Limit {
			page.Next = appEngineCursorAt(f.rev, f.rows[k])
			break
		}
		page.Rows = append(page.Rows, f.rows[k])
	}
	return page, nil
}

func engineRows(live, saved int) []session.ProcessPageRow {
	var rows []session.ProcessPageRow
	for id := 1000 + live; id > 1000; id-- {
		rows = append(rows, session.ProcessPageRow{JobNotice: session.JobNotice{ID: id, State: session.JobRunning}, Live: true})
	}
	for id := saved; id > 0; id-- {
		rows = append(rows, session.ProcessPageRow{JobNotice: session.JobNotice{ID: id, State: session.JobDone}})
	}
	return rows
}

func rowOfTest(row session.ProcessPageRow) appwire.Process {
	return appProcessBase("a", row.JobNotice)
}

// walkComposed walks every page and answers the ids in order, checking each
// page's row count and encoded size.
func walkComposed(t *testing.T, svc *fakeServices, eng *fakeEngine, limit, budget int) []string {
	t.Helper()
	var got []string
	cursor := ""
	for pages := 0; pages < 2000; pages++ {
		out, err := appComposeProcessPage(context.Background(), cursor, limit, budget, svc.page, eng.page, rowOfTest)
		if err != nil {
			t.Fatalf("limit %d budget %d page %d: %v", limit, budget, pages, err)
		}
		raw, _ := json.Marshal(out)
		if len(out.Processes) > limit || len(raw) > budget {
			t.Fatalf("limit %d budget %d: page of %d rows, %d bytes", limit, budget, len(out.Processes), len(raw))
		}
		if len(out.Processes) == 0 && out.Next != "" && out.Next == cursor {
			t.Fatalf("a page that moved nowhere at %q", cursor)
		}
		for _, p := range out.Processes {
			got = append(got, p.ID)
		}
		if out.Next == "" {
			return got
		}
		cursor = out.Next
	}
	t.Fatal("pages never ended")
	return nil
}

func wantComposed(svc *fakeServices, eng *fakeEngine) []string {
	var want []string
	for _, id := range svc.ids {
		want = append(want, "a:s"+strconv.Itoa(id))
	}
	for _, r := range eng.rows {
		want = append(want, appProcessID("a", r.JobNotice))
	}
	return want
}

func sameIDs(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d rows, want %d: %v", label, len(got), len(want), got)
	}
	for k := range want {
		if got[k] != want[k] {
			t.Fatalf("%s: row %d is %s, want %s", label, k, got[k], want[k])
		}
	}
}

// EVERY ROW COUNTS, SERVICES INCLUDED: more services than a page, a page that
// ends exactly on the last service, on a live row and on a saved row — the
// whole walk is every service, then every live process, then the history,
// once each, every page within its limit.
func TestProcessPageServicesLiveAndHistoryAreOneBoundedWalk(t *testing.T) {
	var ids []int
	for k := 1; k <= 130; k++ {
		ids = append(ids, k)
	}
	svc := &fakeServices{ids: ids}
	eng := &fakeEngine{rows: engineRows(5, 20), rev: "e1"}
	want := wantComposed(svc, eng)
	for _, limit := range []int{1, 2, 7, 50, 65, 100} {
		sameIDs(t, fmt.Sprintf("limit %d", limit), walkComposed(t, svc, eng, limit, appwire.ProcessPageReplyMax), want)
	}
	exact := &fakeServices{ids: ids[:50]}
	sameIDs(t, "services exactly a page", walkComposed(t, exact, eng, 50, appwire.ProcessPageReplyMax), wantComposed(exact, eng))
	none := &fakeServices{}
	sameIDs(t, "no services", walkComposed(t, none, eng, 3, appwire.ProcessPageReplyMax), wantComposed(none, eng))
}

// THE BYTES COUNT TOO: services alone larger than a reply are paged by bytes,
// never sent over the budget and never dropped, and the engine's rows follow.
func TestProcessPageServicesArePagedByBytesNotDropped(t *testing.T) {
	svc := &fakeServices{ids: []int{1, 2, 3, 4, 5, 6, 7, 8, 9}, command: strings.Repeat("x", 100<<10)}
	eng := &fakeEngine{rows: engineRows(2, 3), rev: "e1"}
	budget := appProcessPageEnvelope + 250<<10
	sameIDs(t, "byte-paged", walkComposed(t, svc, eng, 100, budget), wantComposed(svc, eng))
	huge := &fakeServices{ids: []int{1, 2}, command: strings.Repeat("y", 8<<20)}
	out, err := appComposeProcessPage(context.Background(), "", 100, appwire.ProcessPageReplyMax, huge.page, eng.page, rowOfTest)
	raw, _ := json.Marshal(out)
	if err != nil || len(out.Processes) == 0 || !out.Processes[0].Cut || len(raw) > appwire.ProcessPageReplyMax {
		t.Fatalf("an oversize service: rows=%d bytes=%d err=%v", len(out.Processes), len(raw), err)
	}
	sameIDs(t, "oversize rows cut, not dropped", walkComposed(t, huge, eng, 100, appwire.ProcessPageReplyMax), wantComposed(huge, eng))
}

// A SERVICE SET THAT CHANGED UNDER A READER STILL IN THE SERVICES IS SAID;
// ONE THAT CHANGES AFTER THE READER HAS LEFT THEM IS NEWS FOR process.changed.
func TestProcessPageServiceMembershipChangeIsStaleOnlyWhileInServices(t *testing.T) {
	svc := &fakeServices{ids: []int{1, 2, 3, 4}}
	eng := &fakeEngine{rows: engineRows(1, 4), rev: "e1"}
	first, err := appComposeProcessPage(context.Background(), "", 2, appwire.ProcessPageReplyMax, svc.page, eng.page, rowOfTest)
	if err != nil || first.Next == "" {
		t.Fatalf("first: %+v %v", first, err)
	}
	svc.ids = []int{1, 2, 3, 4, 5}
	if _, err := appComposeProcessPage(context.Background(), first.Next, 2, appwire.ProcessPageReplyMax, svc.page, eng.page, rowOfTest); !errors.Is(err, session.ErrProcessStale) {
		t.Fatalf("a changed service set was paged on: %v", err)
	}
	if err := appProcessPageError(session.ErrProcessStale); !isCode(err, appwire.CodeStaleCursor) {
		t.Fatalf("stale on the wire: %v", err)
	}
	second, _ := appComposeProcessPage(context.Background(), "", 5, appwire.ProcessPageReplyMax, svc.page, eng.page, rowOfTest)
	svc.ids = []int{1}
	if _, err := appComposeProcessPage(context.Background(), second.Next, 5, appwire.ProcessPageReplyMax, svc.page, eng.page, rowOfTest); err != nil {
		t.Fatalf("a reader past the services went stale on a service change: %v", err)
	}
	if _, err := appComposeProcessPage(context.Background(), "garbage", 5, appwire.ProcessPageReplyMax, svc.page, eng.page, rowOfTest); err == nil {
		t.Fatal("a garbage cursor was accepted")
	}
}
