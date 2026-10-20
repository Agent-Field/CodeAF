package session

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/roles"
)

// Keep these probes source-compatible with current dev: no new tag fields or helpers.
func r1845Node(t *testing.T, a *Agent, title string) *TaskNode {
	t.Helper()
	g := a.graph()
	id := g.reserve()
	g.admit(id, taskSpec{title: title, request: routeAsk, brief: title, acceptance: "done"})
	return g.node(id)
}

func TestOldFailedAttemptAndNewStopKeepDevRouting(t *testing.T) {
	c := &routeCompleter{answer: "The audit was stopped.", verdict: routeYes}
	a, _, _ := routeAgent(t, c)
	a.principal = NewSteward(routeAsk, Budget{Wall: time.Hour}, nil)
	a.mu.Lock()
	a.opened = false // Queue both real deliveries before any wake can consume either.
	a.personAsk = routeAsk
	a.mu.Unlock()
	n := r1845Node(t, a, "pricing audit")
	g := a.graph()
	g.mu.Lock()
	n.state, n.report = TaskFailed, "failed audit"
	g.mu.Unlock()
	a.reportTaskNode(n) // Real resultOf -> deliverTaskNote -> postTaskMessage.
	if err := a.RetryTask(n.id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Cancel(fmt.Sprint(n.id)); err != nil {
		t.Fatal(err)
	}
	if !n.wasStopped() {
		t.Fatal("fixture did not stop the retry")
	}
	a.drainSteering(nil)
	a.mu.Lock()
	ids := append([]uint64(nil), a.turnResults...)
	a.mu.Unlock()
	if len(ids) != 1 {
		t.Fatalf("unexpected task ids: %v", ids)
	}
	r := a.readRouteJudge(context.Background(), wakeNote("the results arrived"), false, c.answer)
	defer r.release()
	t.Logf("start=%t screen=%d confirm=%d question=%s", r.start, c.asked(), c.confirms(), c.sawQuestion())
	if r.start {
		a.applyRouteJudge(newEventHub(), r)
		t.Logf("admitted tasks after applying ruling=%d", admitted(g))
		if admitted(g) != 2 {
			t.Fatal("mixed attempts changed dev's admission")
		}
	}
	if !r.start {
		t.Fatal("mixed attempts changed dev's ruling")
	}
	assertDevRouteReadings(t, c, 1)
}

func TestRecoveryWithoutResultProvenanceKeepsDevRouting(t *testing.T) {
	journal, checkpoint := journalIn(t)
	writeCheckpoint(t, checkpoint, taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 1,
		Nodes: []taskRecord{{ID: 1, Title: "pricing audit", Request: routeAsk, Brief: "audit", Acceptance: "done", State: TaskFailed, Ending: TaskEndingStopped, Report: "stopped", Noted: false}},
	})
	c := &routeCompleter{answer: "The audit was stopped.", verdict: routeYes}
	a, _ := newTestAgent(t, c, func(config *Config) {
		config.SessionFile = journal
		config.AskConsent = true
		config.RolesSource = tierSettings(map[string]string{roles.TierKey(roles.TierLow): routeScreenModel, roles.TierKey(roles.TierMastermind): routeConfirmModel})
	})
	a.principal = NewSteward(routeAsk, Budget{Wall: time.Hour}, nil)
	a.mu.Lock()
	a.personAsk = routeAsk
	a.mu.Unlock()
	stubbedGraph(a, func(*TaskNode) {})
	a.drainSteering(nil) // Consume the actual non-waking recovery notice.
	// This is the same boundary drain used by a wake.
	a.mu.Lock()
	ids := append([]uint64(nil), a.turnResults...)
	a.mu.Unlock()
	t.Logf("restored stopped=%t, remembered result IDs=%v", a.graph().node(1).wasStopped(), ids)
	r := a.readRouteJudge(context.Background(), wakeNote("a background event wakes this session"), false, c.answer)
	defer r.release()
	t.Logf("start=%t screen=%d confirm=%d", r.start, c.asked(), c.confirms())
	if !r.start || len(ids) != 0 {
		t.Fatal("recovery fixture or dev ruling changed")
	}
	assertDevRouteReadings(t, c, 1)
}

func TestUnrevisedMixedResultsKeepDevRouteAsk(t *testing.T) {
	c := &routeCompleter{answer: "The UI audit stays stopped; finish the storage audit.", verdict: routeYes}
	a, _, _ := routeAgent(t, c)
	a.principal = NewSteward(routeAsk, Budget{Wall: time.Hour}, nil)
	ui := r1845Node(t, a, "UI audit")
	storage := r1845Node(t, a, "storage audit")
	g := a.graph()
	g.mu.Lock()
	ui.state, ui.stopped = TaskFailed, true
	ui.spec.brief = "Audit only pricing UI views and produce UI-ONLY-FINDINGS.md"
	storage.state = TaskFailed
	storage.spec.brief = "Audit only pricing storage transactions and produce STORAGE-ONLY-FINDINGS.md"
	storage.spec.acceptance = "STORAGE-ONLY-FINDINGS.md exists"
	g.mu.Unlock()
	note := wakeNote("one audit stopped; the other failed")
	note.replyTags = []TaskReplyTag{ui.resultTag(), storage.resultTag()}
	a.mu.Lock()
	a.rememberOwedLocked(note)
	a.mu.Unlock()
	r := a.readRouteJudge(context.Background(), note, false, c.answer)
	defer r.release()
	if !r.start {
		t.Fatal("mixed results changed dev's ruling")
	}
	assertDevRouteReadings(t, c, 1)
}

func TestFreshThanksAndMixedResultsKeepDevRouting(t *testing.T) {
	c := &routeCompleter{answer: "The cancelled UI audit stays cancelled. The failed storage audit needs repair.", verdict: routeYes}
	a, _, _ := routeAgent(t, c)
	a.principal = NewSteward(routeAsk, Budget{Wall: time.Hour}, nil)
	ui := r1845Node(t, a, "cancelled UI audit")
	storage := r1845Node(t, a, "failed storage audit")
	g := a.graph()
	g.mu.Lock()
	ui.state, ui.stopped = TaskFailed, true
	storage.state = TaskFailed
	g.mu.Unlock()
	c.onAnswer = func(round int) {
		if round != 1 {
			return
		}
		ch, err := a.Submit(context.Background(), "thanks")
		if err != nil {
			t.Error(err)
			return
		}
		// Finish the production boundary drain before this scripted answer
		// returns, so a discarded pre-steer reading cannot satisfy the counter.
		a.drainSteering(nil)
		go func() {
			for range ch {
			}
		}()
	}
	wakes, leave := a.WatchWakes()
	defer leave()
	note := wakeNote("The UI audit stopped; the storage audit failed.")
	note.replyTags = []TaskReplyTag{ui.resultTag(), storage.resultTag()}
	if !a.enqueueNote(note) {
		t.Fatal("wake refused")
	}
	select {
	case stream := <-wakes:
		collect(t, stream)
	case <-time.After(10 * time.Second):
		t.Fatal("no wake")
	}
	a.mu.Lock()
	heard, turn := a.personHeard, a.turnSeq
	a.mu.Unlock()
	t.Logf("personHeard=%d turn=%d screen=%d confirm=%d", heard, turn, c.asked(), c.confirms())
	if heard != turn {
		t.Fatal("fixture did not consume the human message")
	}
	assertDevRouteReadings(t, c, 2)
}

// The expected calls and complete reader input are measured on dev a4dff1168
// using these same production wake, Submit, retry, cancel and recovery paths.
// Compatibility controls deliberately pass on both versions; stop-only tests
// separately prove the guard changes behavior.
func assertDevRouteReadings(t *testing.T, c *routeCompleter, calls int) {
	t.Helper()
	c.mu.Lock()
	screen, confirm, question, confirmQ := c.judged, c.confirmed, c.question, c.confirmQ
	c.mu.Unlock()
	want := routeJudgeQuestion(routeAsk, c.answer)
	if screen != calls || confirm != calls || question != want || confirmQ != want {
		t.Fatalf("dev compatibility: screen=%d confirm=%d, want %d each\nquestion=%q\nconfirm=%q\nwant=%q", screen, confirm, calls, question, confirmQ, want)
	}
	t.Logf("dev compatibility: screen=%d confirm=%d ask=%q", screen, confirm, routeAsk)
}

func TestAmbiguousResultWakesKeepDevRouting(t *testing.T) {
	for _, scenario := range []string{
		"no provenance", "legacy tag", "unknown task", "background outcome",
		"untagged news batched first", "untagged news batched last", "nested batch",
		"untagged news consumed later", "other request", "mixed requests",
		"two stopped attempts", "non-settled tag", "answers earlier human",
	} {
		t.Run(scenario, func(t *testing.T) {
			c := &routeCompleter{answer: "Here is what happened.", verdict: routeYes}
			a, _, _ := routeAgent(t, c)
			a.principal = NewSteward(routeAsk, Budget{Wall: time.Hour}, nil)
			n := reviewResultNode(t, a, true, "stopped audit")
			note := wakeNote("the result arrived")
			note.replyTags = []TaskReplyTag{n.resultTag()}
			switch scenario {
			case "no provenance":
				note.replyTags = nil
			case "legacy tag":
				note.replyTags = []TaskReplyTag{{ID: n.id, Request: routeAsk}}
			case "unknown task":
				note.replyTags = append(note.replyTags, TaskReplyTag{ID: 999, Request: routeAsk})
			case "background outcome":
				note.otherResults = true
			case "answers earlier human":
				note.answersPerson = true
			case "untagged news batched first":
				note = batchSessionNotes([]userMessage{wakeNote("other news"), note})
			case "untagged news batched last":
				note = batchSessionNotes([]userMessage{note, wakeNote("other news")})
			case "nested batch":
				note = batchSessionNotes([]userMessage{batchSessionNotes([]userMessage{note, wakeNote("other news")})})
			case "other request", "mixed requests":
				g := a.graph()
				g.mu.Lock()
				n.spec.request = "implement headless screenshots across the app and add tests"
				g.mu.Unlock()
				if scenario == "other request" {
					note.replyTags = nil
				}
				note.replyTags = append(note.replyTags, n.resultTag())
			case "two stopped attempts":
				g := a.graph()
				g.mu.Lock()
				n.attempt++
				g.mu.Unlock()
				note.replyTags = append(note.replyTags, n.resultTag())
			case "non-settled tag":
				g := a.graph()
				g.mu.Lock()
				n.state = TaskRunning
				g.mu.Unlock()
				note.replyTags = []TaskReplyTag{n.resultTag()}
			}
			a.mu.Lock()
			a.rememberOwedLocked(note)
			if scenario == "untagged news consumed later" {
				a.rememberOwedLocked(wakeNote("other news"))
			}
			a.mu.Unlock()
			r := a.readRouteJudge(context.Background(), note, false, c.answer)
			defer r.release()
			if !r.start {
				t.Fatal("ambiguous wake changed dev's ruling")
			}
			assertDevRouteReadings(t, c, 1)
		})
	}
}

func TestFreshHumanWakesKeepDevFloors(t *testing.T) {
	for _, scenario := range []string{"Person", "short Steward ask", "trivial Steward ask"} {
		t.Run(scenario, func(t *testing.T) {
			c := &routeCompleter{answer: "I will report the result.", verdict: routeYes}
			a, _, _ := routeAgent(t, c)
			if scenario == "short Steward ask" {
				a.principal = NewSteward("thanks", Budget{Wall: time.Hour}, nil)
			} else if scenario == "trivial Steward ask" {
				a.principal = NewSteward("please commit all of the changes now", Budget{Wall: time.Hour}, nil)
			}
			n := reviewResultNode(t, a, true, "stopped audit")
			c.onAnswer = func(round int) {
				if round != 1 {
					return
				}
				stream, err := a.Submit(context.Background(), routeAsk)
				if err != nil {
					t.Error(err)
					return
				}
				a.drainSteering(nil)
				go func() {
					for range stream {
					}
				}()
			}
			reviewWake(t, a, []TaskReplyTag{n.resultTag()})
			a.mu.Lock()
			heard, turn := a.personHeard, a.turnSeq
			a.mu.Unlock()
			if heard == 0 || heard != turn || c.asked() != 0 || c.confirms() != 0 {
				t.Fatalf("dev floor changed: heard=%d turn=%d screen=%d confirm=%d", heard, turn, c.asked(), c.confirms())
			}
		})
	}
}
