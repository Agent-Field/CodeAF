package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
	"github.com/Agent-Field/codeaf/internal/session"
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
	t.Helper()
	_, workspace := wakeEnvironment(t, resolvedTempDir(t))
	h := &realActionHarness{t: t, cwd: workspace}
	h.start()
	a, err := mustCreate(t, h.b, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
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

func (h *realActionHarness) restart() {
	h.w.close()
	var transcript string
	h.b.locked(func() { transcript = h.b.agents[h.agent].transcript })
	h.b.Close()
	h.start()
	// A conversation never spoken in has no row to be found by; the new
	// process is told where the old one's was, as a real one reads it off disk.
	h.b.locked(func() { h.b.agentLocked(h.agent).transcript = transcript })
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

// A REAL REMEMBER, THROUGH THE REAL PRODUCER: the receipt is the producer's
// sentence, a repeat is not a second write, and the line is in the memory.
func TestRealRememberKeepsTheLineOnceAcrossRetries(t *testing.T) {
	h := newRealActionHarness(t)
	raw, e := h.w.call("session.actions", appwire.ActionsParams{Agent: h.agent})
	if e != nil {
		t.Fatal(e)
	}
	var caps appwire.ActionCapabilities
	if err := jsonUnmarshal(raw, &caps); err != nil {
		t.Fatal(err)
	}
	if !caps.Actions[2].Available {
		t.Skipf("this conversation keeps no memory here: %s", caps.Actions[2].Why)
	}
	r, e := h.w.act(memoryAsk(h.agent, "real-m1", "the staging database is reset every Monday", ""))
	if e != nil || r.State != appwire.ActionDone || r.Memory == nil || !strings.HasPrefix(r.Memory.Receipt, "remembered · the staging database is reset every Monday") {
		t.Fatalf("%+v %v", r, e)
	}
	_, live, err := h.b.heldLive(h.agent)
	if err != nil {
		t.Fatal(err)
	}
	lines := func() int {
		found, err := live.conn.Conv.(memoriesReader).Memories("staging database")
		if err != nil {
			t.Fatal(err)
		}
		return len(found)
	}
	if lines() != 1 {
		t.Fatalf("the memory holds %d such lines", lines())
	}
	h.restart()
	again, e := h.w.act(memoryAsk(h.agent, "real-m1", "the staging database is reset every Monday", appwire.ScopeProject))
	if e != nil || !again.Replayed || again.Memory == nil || again.Memory.Receipt != "" {
		t.Fatalf("%+v %v", again, e)
	}
	_, live, err = h.b.heldLive(h.agent)
	if err != nil {
		t.Fatal(err)
	}
	if lines() != 1 {
		t.Fatalf("a repeat kept the line again")
	}
}

// memoriesReader is the read half of the memory door a real conversation has.
type memoriesReader interface {
	Memories(query string) ([]session.MemoryLine, error)
}

func jsonUnmarshal(raw []byte, v any) error { return json.Unmarshal(raw, v) }

// A REAL PRODUCER ERROR IS UNCERTAIN, NEVER A REFUSAL. A line longer than the
// memory block holds is refused by the real store inside Remember; the gateway
// cannot know by type where in the producer that came, so it says uncertain,
// keeps the producer's words, and never calls it again — across a restart too.
func TestRealRememberErrorIsUncertainAndNeverRetried(t *testing.T) {
	h := newRealActionHarness(t)
	raw, e := h.w.call("session.actions", appwire.ActionsParams{Agent: h.agent})
	if e != nil {
		t.Fatal(e)
	}
	var caps appwire.ActionCapabilities
	if err := jsonUnmarshal(raw, &caps); err != nil {
		t.Fatal(err)
	}
	if !caps.Actions[2].Available {
		t.Skipf("this conversation keeps no memory here: %s", caps.Actions[2].Why)
	}
	huge := "oversized " + strings.Repeat("word ", 1300)
	ask := memoryAsk(h.agent, "real-m-full", huge, "")
	r, e := h.w.act(ask)
	if e != nil || r.State != appwire.ActionUncertain || r.ProducerError == nil || r.ProducerError.Message == "" || r.Memory != nil {
		t.Fatalf("a real producer error: %+v %v", r, e)
	}
	_, live, err := h.b.heldLive(h.agent)
	if err != nil {
		t.Fatal(err)
	}
	count := func() int {
		found, err := live.conn.Conv.(memoriesReader).Memories("oversized")
		if err != nil {
			t.Fatal(err)
		}
		return len(found)
	}
	before := count()
	h.restart()
	again, e := h.w.act(ask)
	if e != nil || again.State != appwire.ActionUncertain || !again.Replayed || again.ProducerError == nil || again.ProducerError.Message != r.ProducerError.Message {
		t.Fatalf("after restart: %+v %v", again, e)
	}
	got, e := h.w.get(h.agent, "real-m-full")
	if e != nil || got.State != appwire.ActionUncertain {
		t.Fatalf("read after restart: %+v %v", got, e)
	}
	_, live, err = h.b.heldLive(h.agent)
	if err != nil {
		t.Fatal(err)
	}
	if after := count(); after != before {
		t.Fatalf("a repeat reached the producer: %d lines, was %d", after, before)
	}
}
