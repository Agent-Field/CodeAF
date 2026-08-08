package tui

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// countingBackend is the ordinary fake plus a journal watermark and a tally of
// the heavy reads, so a test can assert what a poll did rather than only what
// it returned.
type countingBackend struct {
	*fakeBackend

	countMu   sync.Mutex
	seq       int64
	journal   int
	snapshots int
	threads   int
	usages    int
	questions int
}

func newCountingBackend(inner *fakeBackend) *countingBackend {
	return &countingBackend{fakeBackend: inner, seq: 1}
}

func (c *countingBackend) LatestEventSeq() (int64, error) {
	c.countMu.Lock()
	defer c.countMu.Unlock()
	c.journal++
	return c.seq, nil
}

// bump is one more event in the journal — the only thing that should ever
// re-open the heavy read set.
func (c *countingBackend) bump() {
	c.countMu.Lock()
	defer c.countMu.Unlock()
	c.seq++
}

func (c *countingBackend) heavyReads() int {
	c.countMu.Lock()
	defer c.countMu.Unlock()
	return c.snapshots + c.threads + c.usages + c.questions
}

func (c *countingBackend) Snapshot() (store.Snapshot, error) {
	c.countMu.Lock()
	c.snapshots++
	c.countMu.Unlock()
	return c.fakeBackend.Snapshot()
}

func (c *countingBackend) ActiveSnapshot() (store.Snapshot, error) {
	c.countMu.Lock()
	c.snapshots++
	c.countMu.Unlock()
	return c.fakeBackend.ActiveSnapshot()
}

func (c *countingBackend) Messages(sessionID string, afterSeq int64, limit int) ([]store.Message, error) {
	c.countMu.Lock()
	c.threads++
	c.countMu.Unlock()
	return c.fakeBackend.Messages(sessionID, afterSeq, limit)
}

func (c *countingBackend) SpendToday() (float64, error) {
	c.countMu.Lock()
	c.usages++
	c.countMu.Unlock()
	return c.fakeBackend.SpendToday()
}

func (c *countingBackend) TopLevelJobUsage() (map[string]store.JobUsage, error) {
	c.countMu.Lock()
	c.usages++
	c.countMu.Unlock()
	return c.fakeBackend.TopLevelJobUsage()
}

func (c *countingBackend) PendingQuestions(sessionID string, limit int) ([]store.AgentQuestion, error) {
	c.countMu.Lock()
	c.questions++
	c.countMu.Unlock()
	return c.fakeBackend.PendingQuestions(sessionID, limit)
}

func (c *countingBackend) OpenQuestions(sessionID string, limit int) ([]store.AgentQuestion, error) {
	c.countMu.Lock()
	c.questions++
	c.countMu.Unlock()
	return c.fakeBackend.OpenQuestions(sessionID, limit)
}

// quietModel is a primed lens on a counting backend with a controllable clock:
// the first poll is always full, so every later assertion is about the second.
func quietModel(t *testing.T, backend *countingBackend, now *time.Time) *Model {
	t.Helper()
	model := New(backend, "quiet")
	model.standingNow = func() time.Time { return *now }
	model.setSize(100, 30)
	model.applyPoll(model.poll()().(pollResultMsg))
	if !model.journalPrimed {
		t.Fatal("first poll left the lens unprimed")
	}
	return model
}

func TestUnchangedJournalPollReadsNothingButTheWatermark(t *testing.T) {
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	backend := newCountingBackend(&fakeBackend{
		messages: []store.Message{{Seq: 1, SessionID: "quiet", Role: store.RoleUser, Body: "hello", Time: now}},
		snapshot: store.Snapshot{Nodes: []store.Node{{ID: "job", Brief: "one job", Status: store.Running}}},
	})
	model := quietModel(t, backend, &now)

	before := backend.heavyReads()
	journalBefore := backend.journal
	result := model.poll()().(pollResultMsg)
	if !result.quiet {
		t.Fatal("an unchanged journal produced a full poll result")
	}
	if got := backend.heavyReads(); got != before {
		t.Fatalf("quiet poll performed %d heavy reads", got-before)
	}
	if backend.journal != journalBefore+1 {
		t.Fatalf("quiet poll read the watermark %d times, want 1", backend.journal-journalBefore)
	}
}

func TestJournalBumpRestoresTheFullRead(t *testing.T) {
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	backend := newCountingBackend(&fakeBackend{
		snapshot: store.Snapshot{Nodes: []store.Node{{ID: "job", Brief: "one job", Status: store.Running}}},
	})
	model := quietModel(t, backend, &now)
	model.applyPoll(model.poll()().(pollResultMsg))

	before := backend.heavyReads()
	backend.bump()
	result := model.poll()().(pollResultMsg)
	if result.quiet {
		t.Fatal("a bumped journal was still reported quiet")
	}
	if backend.heavyReads() <= before {
		t.Fatal("a bumped journal did not re-open the heavy read set")
	}
	model.applyPoll(result)
	if model.journalSeq != backend.seq {
		t.Fatalf("watermark %d did not catch up to %d", model.journalSeq, backend.seq)
	}
}

func TestPollCadenceDecaysWhenQuietAndSnapsBackOnChange(t *testing.T) {
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	backend := newCountingBackend(&fakeBackend{})
	model := quietModel(t, backend, &now)

	if got := model.pollCadence(); got != pollInterval {
		t.Fatalf("cadence just after a change is %s, want %s", got, pollInterval)
	}
	now = now.Add(pollQuietAfter + time.Second)
	model.applyPoll(model.poll()().(pollResultMsg))
	if got := model.pollCadence(); got != pollIdleInterval {
		t.Fatalf("cadence after a quiet stretch is %s, want %s", got, pollIdleInterval)
	}

	backend.bump()
	model.applyPoll(model.poll()().(pollResultMsg))
	if got := model.pollCadence(); got != pollInterval {
		t.Fatalf("cadence after a journal change is %s, want %s", got, pollInterval)
	}

	// A keypress is its own reason to be hot, and buys one full read: the key
	// may have opened a place the watermark cannot speak for.
	now = now.Add(pollQuietAfter + time.Second)
	model.applyPoll(model.poll()().(pollResultMsg))
	if got := model.pollCadence(); got != pollIdleInterval {
		t.Fatalf("cadence before the keypress is %s, want %s", got, pollIdleInterval)
	}
	model.noteActivity()
	if got := model.pollCadence(); got != pollInterval {
		t.Fatalf("cadence after a keypress is %s, want %s", got, pollInterval)
	}
	if result := model.poll()().(pollResultMsg); result.quiet {
		t.Fatal("the poll after a keypress skipped the read set")
	}
}

func TestQuietPollRepaintsTheClockWithoutRebuildingOrReading(t *testing.T) {
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	backend := newCountingBackend(&fakeBackend{
		messages: []store.Message{{
			Seq: 1, SessionID: "quiet", Role: store.RoleAgent,
			Body: "the answer", Time: now.Add(-30 * time.Second),
		}},
	})
	model := quietModel(t, backend, &now)
	if !strings.Contains(model.View(), "now") {
		t.Fatalf("fresh message did not render as now:\n%s", model.View())
	}

	// Inside the repaint window nothing is rebuilt: the cached pane still
	// carries the timestamp it was rendered with.
	reads := backend.heavyReads()
	now = now.Add(quietRepaintInterval / 2)
	model.applyPoll(model.poll()().(pollResultMsg))
	if strings.Contains(model.View(), "m ago") {
		t.Fatalf("quiet poll rebuilt the thread inside the repaint window:\n%s", model.View())
	}

	// Past it the cached pane is re-rendered from state already in hand — the
	// clock moves and the store is still never touched.
	now = now.Add(quietRepaintInterval)
	model.applyPoll(model.poll()().(pollResultMsg))
	if !strings.Contains(model.View(), "m ago") {
		t.Fatalf("quiet repaint left the clock frozen:\n%s", model.View())
	}
	if backend.heavyReads() != reads {
		t.Fatalf("quiet repaint performed %d heavy reads", backend.heavyReads()-reads)
	}
}

// An open node view reads exactly the one thing the watermark cannot speak
// for — the executor's trace file — and nothing else. Its SQL is journal-
// backed like every other pane, so the quiet path stays quiet underneath it.
func TestOpenNodeViewReadsOnlyTheTraceOnAQuietCycle(t *testing.T) {
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	backend := newCountingBackend(&fakeBackend{
		snapshot: store.Snapshot{Nodes: []store.Node{{ID: "job", Brief: "one job", Status: store.Running}}},
	})
	commander := &fakeCommander{trace: "step one\n", current: map[string]string{}}
	model := NewWithCommander(backend, "quiet", commander)
	model.standingNow = func() time.Time { return now }
	model.setSize(100, 30)
	model.applyPoll(model.poll()().(pollResultMsg))

	model.nodeViewID = "job"
	model.pollForce = false
	reads := backend.heavyReads()
	result := model.poll()().(pollResultMsg)
	if !result.quiet {
		t.Fatal("an open node view defeated the quiet poll")
	}
	if backend.heavyReads() != reads {
		t.Fatalf("a quiet cycle with a node view open performed %d heavy reads", backend.heavyReads()-reads)
	}
	if result.nodeTrace != "step one\n" || result.nodeID != "job" {
		t.Fatalf("the quiet cycle did not tail the trace file: %#v", result)
	}

	commander.trace = "step one\nstep two\n"
	model.applyPoll(model.poll()().(pollResultMsg))
	if model.nodeTraceText != "step one\nstep two\n" {
		t.Fatalf("a quiet cycle did not deliver the grown trace: %q", model.nodeTraceText)
	}
}

func TestBackendWithoutAWatermarkKeepsTheUnconditionalRead(t *testing.T) {
	backend := &fakeBackend{}
	model := New(backend, "legacy")
	model.setSize(100, 30)
	for range 3 {
		result := model.poll()().(pollResultMsg)
		if result.quiet {
			t.Fatal("a backend with no watermark was reported quiet")
		}
		model.applyPoll(result)
		if got := model.pollCadence(); got != pollInterval {
			t.Fatalf("cadence without a watermark is %s, want %s", got, pollInterval)
		}
	}
}
