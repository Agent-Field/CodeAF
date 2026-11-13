package main

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/sqlitefile"
	"github.com/Agent-Field/codeaf/internal/store"
)

// realActionHarness is the wire server over a backend whose conversations are
// the REAL engine (remote.Loopback over session.Agent), so the producers called
// are the terminal's own. The model is the local stub wakeEnvironment starts:
// nothing leaves the machine and nothing is paid for.
type realActionHarness struct {
	t     *testing.T
	b     *appBackend
	w     *actionWire
	rec   *appRecorder
	cwd   string
	agent string
}

func newRealActionHarness(t *testing.T) *realActionHarness {
	return newRealActionHarnessSaying(t, "say done")
}

// newRealActionHarnessSaying is the harness whose conversation was opened with
// first as its opening words; "" is a conversation nobody has said anything in.
func newRealActionHarnessSaying(t *testing.T, first string) *realActionHarness {
	t.Helper()
	_, workspace := wakeEnvironment(t, resolvedTempDir(t))
	h := &realActionHarness{t: t, cwd: workspace}
	h.start()
	// SPOKEN IN, as a person's conversation is. A conversation nobody said
	// anything in is taken away when the app quits (appblank.go), so a restart
	// could only ever find a different one — which is how this harness once
	// read a project's memory through a conversation with no project at all.
	a, err := h.b.CreateAgent(appwire.CreateAsk{Workspace: workspace, Text: first})
	if err != nil {
		t.Fatal(err)
	}
	if first != "" {
		h.rec.waitFor(t, 30*time.Second, appTurnDone)
	}
	h.agent = a.ID
	return h
}

// start builds a backend and a wire over the same HOME: a process.
func (h *realActionHarness) start() {
	h.rec = newAppRecorder()
	h.b = newAppBackend(h.rec, appOpenInProcess)
	b := h.b
	h.t.Cleanup(func() { b.Close() })
	h.w = newActionWire(h.t, h.b)
}

// restart is a lost process and a new one. The backend, its wire and the
// engine's once-per-process half are closed and forgotten, the working
// directory moves away from the project so it cannot stand in for one, and the
// new backend is told NOTHING: it finds the conversation on disk by its id,
// with the workspace its folder records, as a real process does.
func (h *realActionHarness) restart() {
	h.t.Helper()
	h.w.close()
	h.b.Close()
	freshEngineProcess(h.t)
	h.t.Chdir(h.t.TempDir())
	row, ok := appRowOnDisk(h.agent)
	if !ok || filepath.Clean(row.Workspace) != filepath.Clean(h.cwd) {
		h.t.Fatalf("the conversation was not kept on disk with its project: found %v, workspace %q, want %q", ok, row.Workspace, h.cwd)
	}
	h.start()
}

// memories is the lines the real conversation shows for query.
func (h *realActionHarness) memories(query string) []session.MemoryLine {
	h.t.Helper()
	_, live, err := h.b.heldLive(h.agent)
	if err != nil {
		h.t.Fatal(err)
	}
	found, err := live.conn.Conv.(memoriesReader).Memories(query)
	if err != nil {
		h.t.Fatal(err)
	}
	return found
}

// journal counts, IN THE STORE'S OWN DURABLE JOURNAL read through a handle of
// its own, the writes that kept text and the writes the duplicate door turned
// away. A producer called again with the same line keeps no second line — the
// store dedupes it — but it journals a skip, so the skip count is what proves
// a repeat never reached the producer.
func (h *realActionHarness) journal(text string) (added, skipped int) {
	h.t.Helper()
	brain, err := store.Open(defaultChatDB())
	if err != nil {
		h.t.Fatal(err)
	}
	defer brain.Close()
	events, err := brain.Events(0, 0)
	if err != nil {
		h.t.Fatal(err)
	}
	for _, e := range events {
		if e.Kind != store.EventMemoryAdd && e.Kind != store.EventMemorySkipped {
			continue
		}
		var payload map[string]any
		if json.Unmarshal(e.Payload, &payload) != nil || (payload["Text"] != text && payload["text"] != text) {
			continue
		}
		if e.Kind == store.EventMemoryAdd {
			added++
		} else {
			skipped++
		}
	}
	return added, skipped
}

// capabilities is session.actions for the harness's conversation.
func (h *realActionHarness) capabilities() appwire.ActionCapabilities {
	h.t.Helper()
	raw, e := h.w.call("session.actions", appwire.ActionsParams{Agent: h.agent})
	if e != nil {
		h.t.Fatal(e)
	}
	var caps appwire.ActionCapabilities
	if err := jsonUnmarshal(raw, &caps); err != nil {
		h.t.Fatal(err)
	}
	return caps
}

// taskRows is how many task rows the real conversation holds.
func (h *realActionHarness) taskRows() int {
	h.t.Helper()
	_, live, err := h.b.heldLive(h.agent)
	if err != nil {
		h.t.Fatal(err)
	}
	views, err := live.conn.Conv.AppTasks()
	if err != nil {
		h.t.Fatal(err)
	}
	return len(views)
}

// A REAL TASK START THROUGH THE WIRE ADMITS EXACTLY ONE TASK, RETURNS THE ID THE
// TASK LIST KNOWS IT BY, AND A DUPLICATE — IN THE SAME PROCESS OR AFTER A
// RESTART — ADMITS NONE.
func TestRealTaskStartAdmitsOnceAcrossRetriesAndRestart(t *testing.T) {
	h := newRealActionHarness(t)
	before := h.taskRows()
	r, e := h.w.act(taskAsk(h.agent, "real-t1", "write a haiku about the tide into notes.txt"))
	if e != nil {
		t.Fatal(e)
	}
	if r.State != appwire.ActionDone || r.Task == nil || !strings.HasPrefix(r.Task.ID, h.agent+":") || r.Task.Number == 0 || r.Task.Title == "" {
		t.Fatalf("a real start: %+v %+v", r, r.Task)
	}
	deadline := time.Now().Add(20 * time.Second)
	for h.taskRows() != before+1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := h.taskRows(); got != before+1 {
		t.Fatalf("task rows %d, want %d", got, before+1)
	}
	again, e := h.w.act(taskAsk(h.agent, "real-t1", "write a haiku about the tide into notes.txt"))
	if e != nil || !again.Replayed || again.Task.ID != r.Task.ID {
		t.Fatalf("duplicate: %+v %v", again, e)
	}
	h.restart()
	after, e := h.w.act(taskAsk(h.agent, "real-t1", "write a haiku about the tide into notes.txt"))
	if e != nil || !after.Replayed || after.State != appwire.ActionDone || after.Task.ID != r.Task.ID {
		t.Fatalf("after restart: %+v %v", after, e)
	}
	if got := h.taskRows(); got != before+1 {
		t.Fatalf("a repeat admitted another task: %d rows", got)
	}
	_, e = h.w.act(taskAsk(h.agent, "real-t1", "a different sentence"))
	wantCode(t, e, appwire.CodeConflict)
}

// THE REAL PROGRAM CATALOGUE IS THE ONE VALIDATED AGAINST: the build's programs
// are the capability, an unknown name is refused before any claim, and a program
// without a default brief wants words.
func TestRealProgramCatalogueIsTheCapability(t *testing.T) {
	h := newRealActionHarness(t)
	raw, e := h.w.call("session.actions", appwire.ActionsParams{Agent: h.agent})
	if e != nil {
		t.Fatal(e)
	}
	var caps appwire.ActionCapabilities
	if err := jsonUnmarshal(raw, &caps); err != nil {
		t.Fatal(err)
	}
	programs := builtin.All()
	got := caps.Actions[1]
	if len(programs) == 0 {
		if got.Available || got.Why == "" {
			t.Fatalf("a build with no programs: %+v", got)
		}
		return
	}
	if !got.Available || len(got.Programs) != len(programs) {
		t.Fatalf("programs: %+v, build carries %d", got, len(programs))
	}
	for i, p := range programs {
		if got.Programs[i].Name != p.Name || got.Programs[i].BriefOptional != (strings.TrimSpace(p.DefaultBrief) != "") {
			t.Errorf("program %d: %+v vs %s", i, got.Programs[i], p.Name)
		}
	}
	_, e = h.w.act(programAsk(h.agent, "real-p0", "not-a-program", "x"))
	wantCode(t, e, appwire.CodeBadParams)
	for _, p := range got.Programs {
		if !p.BriefOptional {
			_, e = h.w.act(programAsk(h.agent, "real-p1", p.Name, " "))
			wantCode(t, e, appwire.CodeBadParams)
			break
		}
	}
	if files := (&actionHarness{t: t, b: h.b}).claimFiles(); len(files) != 0 {
		t.Fatalf("refusals before the claim left %v", files)
	}
}

// A REAL REMEMBER, THROUGH THE REAL PRODUCER, KEPT ONCE FOR EVER. The line is
// kept for this project; a NEW PROCESS, standing somewhere else and told
// nothing, finds the same conversation, the same project and the same line
// before anything is repeated; and a repeat after the restart is answered from
// the record with the store's journal showing one write and no second call.
func TestRealRememberKeepsTheLineOnceAcrossRetries(t *testing.T) {
	h := newRealActionHarness(t)
	if m := h.capabilities().Actions[2]; !m.Available {
		t.Fatalf("the real conversation keeps no memory: %s", m.Why)
	}
	const line = "the staging database is reset every Monday"
	r, e := h.w.act(memoryAsk(h.agent, "real-m1", line, ""))
	if e != nil || r.State != appwire.ActionDone || r.Memory == nil || r.Memory.Receipt != "remembered · "+line+" · for this project" {
		t.Fatalf("%+v %v", r, e)
	}
	held := func(when string) {
		t.Helper()
		found := h.memories("staging database")
		if len(found) != 1 || found[0].Text != line || found[0].Where != "for this project" {
			t.Fatalf("%s: the conversation shows %+v; want the one line, for this project", when, found)
		}
		if added, skipped := h.journal(line); added != 1 || skipped != 0 {
			t.Fatalf("%s: the store journalled %d writes and %d turned-away repeats of the line; want 1 and 0", when, added, skipped)
		}
	}
	held("before the restart")
	again, e := h.w.act(memoryAsk(h.agent, "real-m1", line, appwire.ScopeProject))
	if e != nil || !again.Replayed || again.State != appwire.ActionDone || again.Memory == nil || again.Memory.Receipt != "" {
		t.Fatalf("a repeat in the same process: %+v %v", again, e)
	}
	h.restart()
	held("after the restart, before any repeat")
	after, e := h.w.act(memoryAsk(h.agent, "real-m1", line, appwire.ScopeProject))
	if e != nil || !after.Replayed || after.State != appwire.ActionDone || after.Memory == nil || after.Memory.Receipt != "" {
		t.Fatalf("a repeat after the restart: %+v %v", after, e)
	}
	got, e := h.w.get(h.agent, "real-m1")
	if e != nil || got.State != appwire.ActionDone || !got.Replayed {
		t.Fatalf("a read after the restart: %+v %v", got, e)
	}
	held("after the repeats")
}

// memoriesReader is the read half of the memory door a real conversation has.
type memoriesReader interface {
	Memories(query string) ([]session.MemoryLine, error)
}

func jsonUnmarshal(raw []byte, v any) error { return json.Unmarshal(raw, v) }

// A REAL PRODUCER ERROR IS UNCERTAIN, NEVER A REFUSAL, AND IS NEVER CALLED AGAIN
// — EVEN ONCE THE SAME CALL WOULD SUCCEED. The real store refuses a line that
// does not fit beside what the project already keeps (ErrMemoryFull, inside
// Remember). Then the room is made, so the very same call WOULD keep the line
// now; the repeat, after a restart, must still be the record's uncertain
// answer, and the store's journal must show the line never written. A NEW
// client_id then keeps it, which proves the repeat would have been seen.
func TestRealRememberErrorIsUncertainAndNeverRetried(t *testing.T) {
	h := newRealActionHarness(t)
	if m := h.capabilities().Actions[2]; !m.Available {
		t.Fatalf("the real conversation keeps no memory: %s", m.Why)
	}
	// E5: 0d82108605 (1892-contextual-memory.md) removed storage capacity
	// refusals. Fail the real SQLite write instead, then repair it before replay.
	line := "the deploy notes say ship small and often"
	// The engine holds this database open: wait out its write lock the way the
	// store does, rather than failing with SQLITE_BUSY on a loaded machine.
	busy := make(url.Values)
	busy.Add("_pragma", "busy_timeout(10000)")
	raw, err := sql.Open("sqlite", sqlitefile.URI(defaultChatDB(), busy.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`CREATE TRIGGER e5_refuse_memory BEFORE INSERT ON memories
BEGIN SELECT RAISE(ABORT, 'scripted memory write failure'); END`); err != nil {
		t.Fatal(err)
	}
	ask := memoryAsk(h.agent, "real-m-full", line, "")
	r, e := h.w.act(ask)
	if e != nil || r.State != appwire.ActionUncertain || r.ProducerError == nil || !strings.Contains(r.ProducerError.Message, "scripted memory write failure") || r.Memory != nil || r.Replayed {
		t.Fatalf("a real producer error: %+v %v", r, e)
	}
	if added, skipped := h.journal(line); added != 0 || skipped != 0 {
		t.Fatalf("a refused line was journalled: %d %d", added, skipped)
	}

	// Repair the store so a retry reaching the producer would now succeed.
	if _, err := raw.Exec("DROP TRIGGER e5_refuse_memory"); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	h.restart()
	for _, when := range []string{"a repeat after the restart", "a second repeat"} {
		again, e := h.w.act(ask)
		if e != nil || again.State != appwire.ActionUncertain || !again.Replayed || again.ProducerError == nil || again.ProducerError.Message != r.ProducerError.Message {
			t.Fatalf("%s: %+v %v", when, again, e)
		}
	}
	got, e := h.w.get(h.agent, "real-m-full")
	if e != nil || got.State != appwire.ActionUncertain || got.ProducerError == nil {
		t.Fatalf("a read after the restart: %+v %v", got, e)
	}
	if found := h.memories("deploy notes"); len(found) != 0 {
		t.Fatalf("a repeat reached the producer: the line is kept %+v", found)
	}
	if added, skipped := h.journal(line); added != 0 || skipped != 0 {
		t.Fatalf("a repeat reached the producer: %d writes, %d turned away", added, skipped)
	}

	// The observation is not blind: a NEW client_id asks again and keeps it.
	fresh, e := h.w.act(memoryAsk(h.agent, "real-m-full-2", line, ""))
	if e != nil || fresh.State != appwire.ActionDone || fresh.Replayed {
		t.Fatalf("a new client_id after the store is repaired: %+v %v", fresh, e)
	}
	if added, _ := h.journal(line); added != 1 || len(h.memories("deploy notes")) != 1 {
		t.Fatalf("a kept line is not seen: %d writes", added)
	}
}

// A REAL CONVERSATION'S HANDLE IS A FAR ONE, AND ITS TYPE PROVES NOTHING. Every
// conversation the app holds is a remote.Agent, which has StartTaskEffort
// whatever the engine behind it can do, and no far fact says that engine can
// choose a task's crew: so no effort is offered, one asked for is refused
// before anything is claimed or called, and the same client_id then runs once
// without it. The memory scopes ride the far memory door, which answers both.
func TestRealFarHandleOffersNoEffortItCannotShow(t *testing.T) {
	h := newRealActionHarness(t)
	_, live, err := h.b.heldLive(h.agent)
	if err != nil {
		t.Fatal(err)
	}
	if _, far := live.conn.Conv.(*remote.Agent); !far {
		t.Fatalf("the app's conversation is a %T, not the far handle this test is about", live.conn.Conv)
	}
	caps := h.capabilities()
	if task := caps.Actions[0]; !task.Available || len(task.Efforts) != 0 {
		t.Fatalf("task.start over a far handle: %+v", task)
	}
	if m := caps.Actions[2]; !m.Available || strings.Join(m.Scopes, ",") != "project,everywhere" {
		t.Fatalf("memory.remember over a far handle: %+v", m)
	}
	before := h.taskRows()
	ask := taskAsk(h.agent, "real-effort", "write a haiku about the tide into notes.txt")
	ask.Task.Effort = appwire.EffortBest
	_, e := h.w.act(ask)
	wantCode(t, e, appwire.CodeBadParams)
	if _, e := h.w.get(h.agent, "real-effort"); e == nil || e.Code != appwire.CodeNotFound {
		t.Fatalf("a refused effort left a record: %+v", e)
	}
	if got := h.taskRows(); got != before {
		t.Fatalf("a refused effort admitted a task: %d rows, was %d", got, before)
	}
	r, e := h.w.act(taskAsk(h.agent, "real-effort", "write a haiku about the tide into notes.txt"))
	if e != nil || r.State != appwire.ActionDone || r.Replayed {
		t.Fatalf("the same client_id without the effort: %+v %v", r, e)
	}
}

// A TASK STARTED IN A CONVERSATION NOBODY HAS SPOKEN IN, THEN A CLEAN RESTART.
// No seeded message: the conversation is created empty and only the typed
// action touches it. The task start is what keeps the conversation on disk
// (restart fails if it is reaped as blank), and a repeat after the restart
// never admits a second task.
func TestRealTaskStartInAnEmptyConversationAcrossARestart(t *testing.T) {
	h := newRealActionHarnessSaying(t, "")
	r, e := h.w.act(taskAsk(h.agent, "empty-t1", "write a haiku about the tide into notes.txt"))
	if e != nil || r.State != appwire.ActionDone || r.Task == nil {
		t.Fatalf("a real start in an empty conversation: %+v %v", r, e)
	}
	deadline := time.Now().Add(20 * time.Second)
	for h.taskRows() != 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := h.taskRows(); got != 1 {
		t.Fatalf("task rows %d, want 1", got)
	}
	h.restart()
	after, e := h.w.act(taskAsk(h.agent, "empty-t1", "write a haiku about the tide into notes.txt"))
	if e != nil || !after.Replayed || after.State != appwire.ActionDone || after.Task == nil || after.Task.ID != r.Task.ID {
		t.Fatalf("after restart: %+v %v", after, e)
	}
	if got := h.taskRows(); got != 1 {
		t.Fatalf("after the restart the conversation holds %d task rows, want the one that was started", got)
	}
}
