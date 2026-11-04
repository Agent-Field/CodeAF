package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

func newSavedSpendDisk(t *testing.T, layout string) *unreadDisk {
	t.Helper()
	d := newUnreadDisk(t)
	if layout == "cell" {
		meta, err := session.LoadMeta(d.dir)
		if err != nil {
			t.Fatal(err)
		}
		state := filepath.Join(d.dir, ".cell")
		if err := os.MkdirAll(state, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(d.dir, "transcript.jsonl"), filepath.Join(state, "transcript.jsonl")); err != nil {
			t.Fatal(err)
		}
		if err := session.SaveMeta(d.dir, meta); err != nil {
			t.Fatal(err)
		}
	}
	conv := &fakeConversation{cost: 9}
	d.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	t.Cleanup(d.backend.watchAgentSpend())
	return d
}

// flushSavedSpend joins the restates already handed off by the save hook.
func flushSavedSpend(t *testing.T, b *appBackend) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		b.spendRestates.settle()
		close(done)
	}()
	waitSavedSpend(t, done, "saved-spend restates")
}

func waitSavedSpend(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func savedSpendChanges(rec *appRecorder, from int) []appwire.Agent {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var changes []appwire.Agent
	for i := from; i < len(rec.events); i++ {
		if rec.events[i] == appwire.EventAgentChanged {
			changes = append(changes, rec.data[i].(appwire.AgentChanged).Agent)
		}
	}
	return changes
}

// Contract (a): open and list quote meta.json, including for a held agent whose
// live meter disagrees. A held open reads an exact lower save before any list.
func TestAgentsOpenAndListAgreeOnSavedSpend(t *testing.T) {
	for _, layout := range []string{"legacy", "cell"} {
		t.Run(layout, func(t *testing.T) {
			d := newSavedSpendDisk(t, layout)
			meta, err := session.LoadMeta(d.dir)
			if err != nil {
				t.Fatal(err)
			}
			meta.SpentUSD = 1.25
			if err := session.SaveMeta(d.dir, meta); err != nil {
				t.Fatal(err)
			}
			opened, err := d.backend.OpenAgent(d.id)
			if err != nil || opened.SpentUSD != 1.25 {
				t.Fatalf("cold agents.open = %+v, %v; want saved spend 1.25", opened, err)
			}
			if listed := d.listed(); listed.SpentUSD != opened.SpentUSD {
				t.Fatalf("agents.list = %v, agents.open = %v", listed.SpentUSD, opened.SpentUSD)
			}
			meta.SpentUSD = .26
			if err := session.SaveMeta(d.dir, meta); err != nil {
				t.Fatal(err)
			}
			opened, err = d.backend.OpenAgent(d.id)
			listed := d.listed()
			if err != nil || opened.SpentUSD != .26 || opened.SpentUSD != listed.SpentUSD {
				t.Fatalf("held agents.open = %+v, %v; agents.list = %+v; want saved spend .26", opened, err, listed)
			}
		})
	}
}

// Contract (a), method-answer rule: a writer stalled inside the tap cannot
// hold Rename up. Its answer omits spend; the later held open agrees with list.
func TestSavedSpendContractAMethodAnswerDoesNotWaitForAStalledEventWriter(t *testing.T) {
	for _, layout := range []string{"legacy", "cell"} {
		t.Run(layout, func(t *testing.T) {
			d := newSavedSpendDisk(t, layout)
			emit := &savedSpendStalledEmitter{next: d.rec, entered: make(chan struct{}), release: make(chan struct{})}
			d.backend.emit.(*appInboxTap).next = emit
			var release sync.Once
			letGo := func() { release.Do(func() { close(emit.release) }) }
			t.Cleanup(letGo)
			meta, err := session.LoadMeta(d.dir)
			if err != nil {
				t.Fatal(err)
			}
			meta.SpentUSD = 1.25
			if err := session.SaveMeta(d.dir, meta); err != nil {
				t.Fatal(err)
			}
			if _, err := d.backend.OpenAgent(d.id); err != nil {
				t.Fatal(err)
			}
			if listed := d.listed(); listed.SpentUSD != 1.25 {
				t.Fatalf("agents.list = %+v; want saved spend 1.25", listed)
			}
			var view appwire.Agent
			var box *appwire.Outbox
			d.backend.locked(func() {
				a := d.backend.agents[d.id]
				view, box = a.view, a.live.box
			})
			emit.armed.Store(true)
			box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
			waitSavedSpend(t, emit.entered, "event writer inside the inbox tap")
			meta.SpentUSD = .26
			if err := session.SaveMeta(d.dir, meta); err != nil {
				t.Fatal(err)
			}
			var renamed appwire.Agent
			var renameErr error
			done := make(chan struct{})
			go func() {
				renamed, renameErr = d.backend.Rename(d.id, "renamed while the writer waits")
				close(done)
			}()
			t.Cleanup(func() { letGo(); waitSavedSpend(t, done, "Rename after releasing the writer") })
			waitSavedSpend(t, done, "Rename answer while the event writer is stalled")
			if renameErr != nil || renamed.Title != "renamed while the writer waits" {
				t.Fatalf("Rename = %+v, %v", renamed, renameErr)
			}
			raw, err := json.Marshal(renamed)
			if err != nil {
				t.Fatal(err)
			}
			var answer map[string]json.RawMessage
			if err := json.Unmarshal(raw, &answer); err != nil {
				t.Fatal(err)
			}
			if _, present := answer["spent_usd"]; present {
				t.Fatalf("Rename answer carries spent_usd: %s", raw)
			}
			letGo()
			d.rec.waitFor(t, 10*time.Second, func(event string, data any) bool {
				changed, ok := data.(appwire.AgentChanged)
				return event == appwire.EventAgentChanged && ok && changed.Agent.ID == d.id && changed.Agent.Title == renamed.Title && changed.Agent.SpentUSD == .26
			})
			opened, err := d.backend.OpenAgent(d.id)
			listed := d.listed()
			if err != nil || opened.SpentUSD != .26 || opened.SpentUSD != listed.SpentUSD {
				t.Fatalf("held agents.open = %+v, %v; agents.list = %+v; want saved spend .26", opened, err, listed)
			}
			reopened, err := d.backend.Reopen(d.id)
			if err != nil || reopened.SpentUSD != 0 {
				t.Fatalf("Reopen = %+v, %v; want an answer without saved spend", reopened, err)
			}
		})
	}
}

// Contract (a), busy-open rule: an already-opening answer reads the saved
// figure without waiting for the tap or recording it as said to the window.
func TestSavedSpendContractABusyOpenDoesNotWaitForAStalledEventWriter(t *testing.T) {
	for _, layout := range []string{"legacy", "cell"} {
		t.Run(layout, func(t *testing.T) {
			d := newSavedSpendDisk(t, layout)
			meta, err := session.LoadMeta(d.dir)
			if err != nil {
				t.Fatal(err)
			}
			meta.SpentUSD = 1.25
			if err := session.SaveMeta(d.dir, meta); err != nil {
				t.Fatal(err)
			}
			d.listed()
			emit := &savedSpendStalledEmitter{next: d.rec, entered: make(chan struct{}), release: make(chan struct{})}
			d.backend.emit.(*appInboxTap).next = emit
			opening, resume := make(chan struct{}), make(chan struct{})
			open := d.backend.open
			d.backend.open = func(ask appOpenAsk) (*appConn, error) {
				close(opening)
				<-resume
				return open(ask)
			}
			var release sync.Once
			letGo := func() {
				release.Do(func() {
					close(emit.release)
					close(resume)
				})
			}
			t.Cleanup(letGo)
			var opened appwire.Agent
			var openErr error
			openedDone := make(chan struct{})
			go func() { opened, openErr = d.backend.OpenAgent(d.id); close(openedDone) }()
			t.Cleanup(func() { letGo(); waitSavedSpend(t, openedDone, "original open") })
			waitSavedSpend(t, opening, "conversation opener")
			box := appwire.NewOutbox(d.backend.emit)
			t.Cleanup(func() { letGo(); box.Close() })
			emit.armed.Store(true)
			box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: appwire.Agent{ID: d.id}})
			waitSavedSpend(t, emit.entered, "event writer inside the inbox tap")
			meta.SpentUSD = .26
			if err := session.SaveMeta(d.dir, meta); err != nil {
				t.Fatal(err)
			}
			var busy appwire.Agent
			var busyErr error
			done := make(chan struct{})
			go func() { busy, busyErr = d.backend.OpenAgent(d.id); close(done) }()
			t.Cleanup(func() { letGo(); waitSavedSpend(t, done, "busy open after releasing the writer") })
			waitSavedSpend(t, done, "busy open answer while the event writer is stalled")
			if busyErr != nil || busy.SpentUSD != .26 {
				t.Fatalf("busy agents.open = %+v, %v; want saved spend .26", busy, busyErr)
			}
			var said float64
			d.backend.locked(func() { said = d.backend.agents[d.id].saidSpend })
			if said != 1.25 {
				t.Fatalf("busy open recorded spend %v as said; want the stalled event's 1.25", said)
			}
			letGo()
			waitSavedSpend(t, openedDone, "original open after release")
			listed := d.listed()
			if openErr != nil || opened.SpentUSD != .26 || opened.SpentUSD != listed.SpentUSD {
				t.Fatalf("agents.open = %+v, %v; agents.list = %+v; want saved spend .26", opened, openErr, listed)
			}
		})
	}
}

// Contract (b), (c): save news restates the disk figure once. Duplicate and
// delayed older news, and news for another conversation, send nothing more.
func TestSavedSpendNewsChangesTheAgentOnceAndOpenAgreesWithList(t *testing.T) {
	for _, layout := range []string{"legacy", "cell"} {
		t.Run(layout, func(t *testing.T) {
			d := newSavedSpendDisk(t, layout)
			meta, err := session.LoadMeta(d.dir)
			if err != nil {
				t.Fatal(err)
			}
			meta.SpentUSD = .25
			if err := session.SaveMeta(d.dir, meta); err != nil {
				t.Fatal(err)
			}
			if _, err := d.backend.OpenAgent(d.id); err != nil {
				t.Fatal(err)
			}
			from := appRecorded(d.rec)
			meta.SpentUSD = .375
			if err := session.SaveMeta(d.dir, meta); err != nil {
				t.Fatal(err)
			}
			saved := session.SpendSaved{Dir: d.dir, SpentUSD: .375}
			d.backend.agentSpendSaved(saved)
			flushSavedSpend(t, d.backend)
			d.backend.agentSpendSaved(saved)
			d.backend.agentSpendSaved(session.SpendSaved{Dir: d.dir, SpentUSD: .25})
			// Another conversation's news cannot change this agent or create one.
			d.backend.agentSpendSaved(session.SpendSaved{Dir: d.dir + "-other", SpentUSD: 7})
			flushSavedSpend(t, d.backend)
			changes := savedSpendChanges(d.rec, from)
			if len(changes) != 1 || changes[0].ID != d.id || changes[0].SpentUSD != .375 || changes[0].Budget == nil {
				t.Fatalf("spend news sent %+v, want one dressed agent.changed with .375", changes)
			}
			opened, err := d.backend.OpenAgent(d.id)
			if err != nil || opened.SpentUSD != .375 {
				t.Fatalf("agents.open after news = %+v, %v; want .375", opened, err)
			}
			if listed := d.listed(); listed.SpentUSD != opened.SpentUSD {
				t.Fatalf("agents.list = %v, agents.open = %v", listed.SpentUSD, opened.SpentUSD)
			}
		})
	}
}

// Contract (a), (c), review 2 finding 2: an older higher inventory row cannot
// undo an exact lower save, including after resume, in any outgoing view.
func TestSavedSpendSurvivesAnOlderHigherInventoryRow(t *testing.T) {
	for _, layout := range []string{"legacy", "cell"} {
		for _, held := range []bool{false, true} {
			name := layout + "/cold"
			if held {
				name = layout + "/held"
			}
			t.Run(name, func(t *testing.T) {
				d := newSavedSpendDisk(t, layout)
				meta, err := session.LoadMeta(d.dir)
				if err != nil {
					t.Fatal(err)
				}
				meta.SpentUSD = .375
				if err := session.SaveMeta(d.dir, meta); err != nil {
					t.Fatal(err)
				}
				d.listed()
				if held {
					if _, err := d.backend.OpenAgent(d.id); err != nil {
						t.Fatal(err)
					}
				}
				// Pause the list between its disk read and rememberRowLocked.
				stale, ok := appRowOnDisk(d.id)
				if !ok || stale.Spend != .375 {
					t.Fatalf("inventory read = %+v, %v; want .375", stale, ok)
				}
				meta.SpentUSD = .26
				if err := session.SaveMeta(d.dir, meta); err != nil {
					t.Fatal(err)
				}
				saved := session.SpendSaved{Dir: d.dir, SpentUSD: .26}
				from := appRecorded(d.rec)
				d.backend.agentSpendSaved(saved)
				flushSavedSpend(t, d.backend)
				d.backend.locked(func() {
					a := d.backend.agents[d.id]
					d.backend.rememberRowLocked(a, stale, nil, "")
				})
				changes := savedSpendChanges(d.rec, from)
				if len(changes) != 1 || changes[0].SpentUSD != .26 {
					t.Fatalf("spend news sent %+v; want one .26 change despite the stale higher row", changes)
				}
				opened, err := d.backend.OpenAgent(d.id)
				if err != nil || opened.SpentUSD != .26 {
					t.Fatalf("agents.open after stale inventory = %+v, %v; want .26", opened, err)
				}
				from = appRecorded(d.rec)
				d.backend.sayAgent(d.backend.agent(d.id))
				changes = savedSpendChanges(d.rec, from)
				if len(changes) != 1 || changes[0].SpentUSD != .26 || d.listed().SpentUSD != .26 {
					t.Fatalf("next agent.changed or fresh list did not retain .26: %+v", changes)
				}
				from = appRecorded(d.rec)
				d.backend.agentSpendSaved(saved)
				flushSavedSpend(t, d.backend)
				if changes := savedSpendChanges(d.rec, from); len(changes) != 0 {
					t.Fatalf("duplicate news sent %+v; want no further change", changes)
				}
			})
		}
	}
}

// Contract (a), (c), review 2 finding 1: an ordinary producer pauses between
// copying the view and enqueueing it. Both changes must quote the saved figure.
func TestSavedSpendDressesAProducerSnapshotTakenBeforeTheSave(t *testing.T) {
	for _, layout := range []string{"legacy", "cell"} {
		t.Run(layout, func(t *testing.T) {
			d := newSavedSpendDisk(t, layout)
			meta, err := session.LoadMeta(d.dir)
			if err != nil {
				t.Fatal(err)
			}
			meta.SpentUSD = .25
			if err := session.SaveMeta(d.dir, meta); err != nil {
				t.Fatal(err)
			}
			if _, err := d.backend.OpenAgent(d.id); err != nil {
				t.Fatal(err)
			}
			from := appRecorded(d.rec)
			captured, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var release sync.Once
			letGo := func() { release.Do(func() { close(resume) }) }
			go func() {
				var view appwire.Agent
				var box *appwire.Outbox
				d.backend.locked(func() {
					a := d.backend.agents[d.id]
					view, box = a.view, a.live.box
				})
				close(captured)
				<-resume
				box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
				box.Close()
				close(done)
			}()
			t.Cleanup(func() { letGo(); waitSavedSpend(t, done, "ordinary producer") })
			waitSavedSpend(t, captured, "producer snapshot")
			meta.SpentUSD = .375
			if err := session.SaveMeta(d.dir, meta); err != nil {
				t.Fatal(err)
			}
			d.backend.agentSpendSaved(session.SpendSaved{Dir: d.dir, SpentUSD: .375})
			flushSavedSpend(t, d.backend)
			letGo()
			waitSavedSpend(t, done, "ordinary producer delivery")
			changes := savedSpendChanges(d.rec, from)
			if len(changes) != 2 || changes[0].SpentUSD != .375 || changes[1].SpentUSD != .375 {
				t.Fatalf("delivered changes = %+v; want .375 followed by .375", changes)
			}
		})
	}
}

type savedSpendStalledEmitter struct {
	next    appwire.Emitter
	armed   atomic.Bool
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (e *savedSpendStalledEmitter) Emit(event string, data any) {
	if e.armed.Load() {
		e.once.Do(func() { close(e.entered) })
		<-e.release
	}
	e.next.Emit(event, data)
}

// Contract (a), (c), review 2 finding 1: a released live outbox can drain after
// a newer restate took another road. Its old view must carry the new disk figure.
func TestSavedSpendDressesAnOldOutboxAfterRelease(t *testing.T) {
	d := newSavedSpendDisk(t, "cell")
	meta, err := session.LoadMeta(d.dir)
	if err != nil {
		t.Fatal(err)
	}
	meta.SpentUSD = .375
	if err := session.SaveMeta(d.dir, meta); err != nil {
		t.Fatal(err)
	}
	if _, err := d.backend.OpenAgent(d.id); err != nil {
		t.Fatal(err)
	}
	a := d.backend.agent(d.id)
	emit := &savedSpendStalledEmitter{next: d.backend.emit, entered: make(chan struct{}), release: make(chan struct{})}
	emit.armed.Store(true)
	var release sync.Once
	letGo := func() { release.Do(func() { close(emit.release) }) }
	defer letGo()
	detached, done := make(chan struct{}), make(chan struct{})
	var old *appwire.Outbox
	d.backend.locked(func() {
		old = a.live.box
		a.live.box = appwire.NewOutbox(emit)
		a.live.conn.Release = func() { close(detached) }
	})
	old.Close()
	from := appRecorded(d.rec)
	d.backend.locked(func() { a.live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: a.view}) })
	waitSavedSpend(t, emit.entered, "old outbox before the inbox tap")
	go func() {
		a.gate.Lock()
		defer a.gate.Unlock()
		d.backend.release(a)
		close(done)
	}()
	t.Cleanup(func() { letGo(); waitSavedSpend(t, done, "released outbox") })
	waitSavedSpend(t, detached, "live conversation detached")
	meta.SpentUSD = .5
	if err := session.SaveMeta(d.dir, meta); err != nil {
		t.Fatal(err)
	}
	d.backend.agentSpendSaved(session.SpendSaved{Dir: d.dir, SpentUSD: .5})
	flushSavedSpend(t, d.backend)
	letGo()
	waitSavedSpend(t, done, "old outbox delivery")
	changes := savedSpendChanges(d.rec, from)
	if len(changes) != 2 || changes[0].SpentUSD != .5 || changes[1].SpentUSD != .5 {
		t.Fatalf("outbox transition delivered %+v; want .5 followed by .5", changes)
	}
}

// Contract (b), (c): the real stamp and hook return while the emitter, unread
// lock and backend lock are stalled. After release only one new figure is said.
func TestSavedSpendStampDoesNotWaitForAStalledEmitter(t *testing.T) {
	for _, held := range []bool{false, true} {
		name := "cold"
		if held {
			name = "held"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
			id, workspace := session.NewSessionID(), t.TempDir()
			dir := filepath.Join(t.TempDir(), id)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := session.SaveMeta(dir, session.Meta{ID: id, Workspace: workspace}); err != nil {
				t.Fatal(err)
			}
			transcript := filepath.Join(dir, "transcript.jsonl")
			journal := "{\"type\":\"session\",\"version\":1,\"id\":\"" + id + "\"}\n" +
				"{\"type\":\"usage\",\"usage\":{\"input\":10,\"output\":5,\"costUsd\":0.375,\"calls\":1}}\n"
			if err := os.WriteFile(transcript, []byte(journal), 0o600); err != nil {
				t.Fatal(err)
			}
			profile := t.TempDir()
			rec := newAppRecorder()
			emit := &savedSpendStalledEmitter{next: rec, entered: make(chan struct{}), release: make(chan struct{})}
			var release sync.Once
			letGo := func() { release.Do(func() { close(emit.release) }) }
			defer letGo()
			b := newFakeBackend(emit, func(ask appOpenAsk) (*appConn, error) {
				return &appConn{Conv: &fakeConversation{}, Transcript: ask.Transcript, Workspace: ask.Workspace, Release: func() {}}, nil
			})
			t.Cleanup(func() { _ = b.Close() })
			stop := b.watchAgentSpend()
			t.Cleanup(stop)
			b.remember(session.SessionRow{ID: id, Dir: dir, Transcript: transcript, Workspace: workspace})
			if held {
				if _, err := b.OpenAgent(id); err != nil {
					t.Fatal(err)
				}
			}
			box := appwire.NewOutbox(b.emit)
			t.Cleanup(box.Close)
			emit.armed.Store(true)
			box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: appwire.Agent{ID: id}})
			select {
			case <-emit.entered:
			case <-time.After(10 * time.Second):
				t.Fatal("emitter did not stall")
			}
			// The hook must hand off without taking the backend lock either.
			b.mu.Lock()
			var unlock sync.Once
			unlockBackend := func() { unlock.Do(b.mu.Unlock) }
			defer unlockBackend()
			// Replaying a priced journal backfills metadata through a real spend
			// stamp, without a provider request or a separate engine binary.
			var engine *session.Agent
			var err error
			done := make(chan struct{})
			go func() {
				engine, err = session.New(session.Config{
					Workspace: workspace, Place: session.Place{Dir: dir}, SessionFile: transcript,
					ProfileDir: profile, Model: "test/model", APIKey: "fixture", BaseURL: "https://example.invalid",
					Routing: provider.RoutingOff, DeterministicNames: true,
				})
				close(done)
			}()
			t.Cleanup(func() {
				unlockBackend()
				letGo()
				select {
				case <-done:
					if engine != nil {
						_ = engine.Close()
					}
				case <-time.After(10 * time.Second):
					t.Error("session did not finish after releasing the emitter")
				}
			})
			select {
			case <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("session spend stamp waited for stalled stdout")
			}
			meta, err := session.LoadMeta(dir)
			if err != nil || meta.SpentUSD != .375 {
				t.Fatalf("session stamp saved %+v, %v; want .375", meta, err)
			}
			hookDone := make(chan struct{})
			go func() {
				b.agentSpendSaved(session.SpendSaved{Dir: dir, SpentUSD: .375})
				close(hookDone)
			}()
			waitSavedSpend(t, hookDone, "duplicate hook while emitter and backend are stalled")
			unlockBackend()
			letGo()
			flushSavedSpend(t, b)
			stop()
			rec.mu.Lock()
			defer rec.mu.Unlock()
			changes := 0
			for i, event := range rec.events {
				if event == appwire.EventAgentChanged && rec.data[i].(appwire.AgentChanged).Agent.SpentUSD == .375 {
					changes++
				}
			}
			if changes != 1 {
				t.Fatalf("released emitter received %d spend changes; want one", changes)
			}
		})
	}
}
