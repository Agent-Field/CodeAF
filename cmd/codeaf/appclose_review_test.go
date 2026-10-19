package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// closeEndDuringOpen models a process ending at the opener: nothing following
// open runs, including a stopped notice or a private resume wake.
func closeEndDuringOpen(t *testing.T, r *closeRig) {
	t.Helper()
	const ended = "test engine ended inside open"
	r.b.open = func(appOpenAsk) (*appConn, error) {
		disk := &appState{path: r.b.state.where()}
		if err := disk.read(func(f *appStateFile) {
			if f.CloseResumes[r.id] == 0 || f.ClosePending[r.id] == "" || f.ClosePendingAt[r.id].IsZero() || !f.CloseHandled[r.id].Equal(r.p.UpdatedAt) {
				t.Errorf("opener ran before durable admission: %+v", f)
			}
		}); err != nil {
			t.Fatal(err)
		}
		// A session can replace the old working claim before its wake starts.
		idle := r.p
		idle.State, idle.UpdatedAt = session.PresenceIdle, r.clock.now()
		r.write(t, idle)
		panic(ended)
	}
	r.b.snapshotCloseClaims()
	path := filepath.Dir(r.file)
	claim := r.b.closeRecovery.claims[path]
	if claim == nil {
		t.Fatal("missing death fixture")
	}
	func() {
		defer func() {
			if got := recover(); got != ended {
				t.Fatalf("ending = %v", got)
			}
		}()
		r.b.checkCloseClaim(path, claim)
		t.Fatal("opener did not end the engine")
	}()
}

func closeDisk(t *testing.T, r *closeRig) appStateFile {
	t.Helper()
	var file appStateFile
	if err := (&appState{path: r.b.state.where()}).read(func(f *appStateFile) { file = f.clone() }); err != nil {
		t.Fatal(err)
	}
	return file
}

// C6, C13 (F2a): even a death inside open spends its attempt. New holder stamps
// stand for successive processes, so the third start cannot evade the guard.
func TestAppCloseAttemptIsDurableBeforeOpen(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	for attempt := 1; attempt <= appCloseResumeLimit; attempt++ {
		closeEndDuringOpen(t, r)
		if got := closeDisk(t, r).CloseResumes[r.id]; got != attempt {
			t.Fatalf("attempts=%d, want %d", got, attempt)
		}
		_ = r.b.Close()
		r.clock.advance(t, 11*time.Minute)
		r.p.PID++
		r.p.UpdatedAt = r.clock.now().Add(-time.Minute)
		r.write(t, r.p)
		r.backend(t)
	}
	r.b.open = func(appOpenAsk) (*appConn, error) {
		t.Error("third start opened refused work")
		return nil, errors.New("unexpected open")
	}
	r.start()
	r.clock.advance(t, 0)
	r.wantFooter(t, appwire.FooterStoppedByClose, appCloseStoppedWord)
	if got := closeDisk(t, r).CloseResumes[r.id]; got != appCloseResumeLimit {
		t.Fatalf("attempts=%d", got)
	}
}

// C3, C6, C10 (F3): an opener's idle heartbeat does not lose an owed reply.
// Replaying spends the second attempt and retains the original footer key.
func TestAppClosePendingSurvivesDeathBeforeWake(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	closeEndDuringOpen(t, r)
	key := closeDisk(t, r).ClosePending[r.id]
	if n, _ := r.conv.counts(); n != 0 {
		t.Fatal("wake ran after the simulated death")
	}
	_ = r.b.Close()
	r.clock.advance(t, 11*time.Minute)
	r.backend(t)
	r.start()
	r.clock.advance(t, 0)
	if n, _ := r.conv.counts(); n != 1 {
		t.Fatalf("pending resumes=%d", n)
	}
	r.wantFooter(t, appwire.FooterResumedAfterClose, appCloseResumedWord)
	if f := closeDisk(t, r); f.CloseResumes[r.id] != 2 || f.ClosePending[r.id] != key {
		t.Fatalf("pending replay=%+v", f)
	}
	// This fake accepted the wake without starting a turn: it is still owed.
	_ = r.b.Close()
	r.clock.advance(t, 11*time.Minute)
	r.backend(t)
	r.start()
	r.clock.advance(t, 0)
	if n, _ := r.conv.counts(); n != 1 {
		t.Fatal("pending replay bypassed two-attempt limit")
	}
	footers := closeFooters(r.entries(t))
	if len(footers) != 2 || footers[0].ID != key || footers[1].Footer != appwire.FooterStoppedByClose {
		t.Fatalf("footers=%+v", footers)
	}
}

// C3, C10 (F3): a receipt kept before death is reused, and a pending marker
// belonging to the same busy claim is dispatched by only one startup road.
func TestAppClosePendingReusesFooterAndPresenceRoad(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "busy"}[busy], func(t *testing.T) {
			r := newCloseRig(t, session.PresenceWorking, time.Minute)
			r.start()
			r.clock.advance(t, 0)
			key := closeDisk(t, r).ClosePending[r.id]
			_ = r.b.Close()
			r.clock.advance(t, 11*time.Minute)
			if !busy {
				r.p.State = session.PresenceIdle
				r.write(t, r.p)
			}
			r.backend(t)
			r.start()
			r.clock.advance(t, 0)
			if n, _ := r.conv.counts(); n != 2 {
				t.Fatalf("resumes=%d", n)
			}
			r.wantFooter(t, appwire.FooterResumedAfterClose, appCloseResumedWord)
			if closeFooters(r.entries(t))[0].ID != key {
				t.Fatal("pending replay made a new receipt")
			}
			if closeDisk(t, r).CloseResumes[r.id] != 2 {
				t.Fatal("pending work ran twice")
			}
		})
	}
}

// C7, C9, C14 (F3): pending continuations obey every admission boundary too.
func TestAppClosePendingAdmission(t *testing.T) {
	for _, reason := range []string{"age", "breaker", "day limit"} {
		t.Run(reason, func(t *testing.T) {
			r := newCloseRig(t, session.PresenceIdle, time.Minute)
			at := r.clock.now().Add(-time.Minute)
			if reason == "age" {
				at = r.clock.now().Add(-appCloseResumeWithin - time.Second)
				writeCloseJSON(t, r.file, map[string]any{"type": "message", "role": "user", "content": "finish the work", "timestamp": at.Add(-time.Minute)})
				meta, err := session.LoadMeta(filepath.Dir(r.file))
				if err != nil {
					t.Fatal(err)
				}
				meta.LastUserAt = at.Add(-time.Minute)
				if err := session.SaveMeta(filepath.Dir(r.file), meta); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.b.state.write(func(f *appStateFile) error {
				f.ClosePending = map[string]string{r.id: "owed"}
				f.ClosePendingAt = map[string]time.Time{r.id: at}
				f.CloseResumes = map[string]int{r.id: 1}
				if reason == "breaker" {
					f.CloseStarts = []time.Time{at, at.Add(-time.Minute)}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if reason == "day limit" {
				path := config.BudgetConfigPath(config.ProfileDir())
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(`{"daily_budget_usd":null}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			r.b.open = func(appOpenAsk) (*appConn, error) {
				t.Error("refused pending continuation opened")
				return nil, errors.New("unexpected open")
			}
			r.start()
			r.clock.advance(t, 0)
			if n, _ := r.conv.counts(); n != 0 {
				t.Fatal("refused pending continuation ran")
			}
			if reason != "day limit" {
				r.wantFooter(t, appwire.FooterStoppedByClose, appCloseStoppedWord)
			}
		})
	}
}

// C3, C13 (F3): neither accepting a wake nor observing a turn alone clears the
// marker. Both arrival orders clear it without resetting the attempt count.
func TestAppClosePendingNeedsAcceptanceAndTurnStart(t *testing.T) {
	for _, turnFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "acceptance first", true: "turn first"}[turnFirst], func(t *testing.T) {
			r := newCloseRig(t, session.PresenceWorking, time.Minute)
			var started <-chan struct{}
			var events chan session.Event
			startTurn := func() {
				events = make(chan session.Event, 1)
				events <- session.Event{Kind: session.EventCloseContinued, Text: closeDisk(t, r).ClosePending[r.id]}
				done := make(chan struct{})
				started = done
				a := r.b.agent(r.id)
				live := r.b.liveOf(a)
				go func() { defer close(done); r.b.wokenTurn(a, live, appTurnElsewhere{events: events}) }()
				r.rec.waitFor(t, time.Second, func(name string, _ any) bool { return name == appwire.EventTurnStarted })
			}
			if turnFirst {
				r.conv.beforeResume = func() {
					startTurn()
					if closeDisk(t, r).ClosePending[r.id] == "" {
						t.Error("turn alone cleared pending")
					}
				}
			}
			r.start()
			r.clock.advance(t, 0)
			if !turnFirst {
				if closeDisk(t, r).ClosePending[r.id] == "" {
					t.Fatal("acceptance alone cleared pending")
				}
				startTurn()
			}
			events <- session.Event{Kind: session.EventError, Text: "test unfinished turn"}
			close(events)
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("turn did not finish")
			}
			f := closeDisk(t, r)
			if f.ClosePending[r.id] != "" || !f.ClosePendingAt[r.id].IsZero() || f.CloseResumes[r.id] != 1 {
				t.Errorf("turn admission=%+v", f)
			}
		})
	}
}

// C1, C2 (F4): the first expiry read sees idle, then the follow-up sees a new
// working heartbeat. That new holder dies without another client read.
func TestAppCloseIdleToWorkingFollowupKeepsWatch(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, 2*time.Second)
	_ = r.b.state.write(func(f *appStateFile) error { f.CloseResumes = map[string]int{r.id: 2}; return nil })
	r.start()
	_, _ = r.b.ListAgents("")
	idle := r.p
	idle.State, idle.UpdatedAt = session.PresenceIdle, r.clock.now()
	r.write(t, idle)
	// closeClaimDue reads idle before the follow-up asks for the time. At that
	// exact boundary the writer starts work and publishes its next heartbeat.
	r.b.now = func() time.Time {
		working := idle
		working.State, working.UpdatedAt = session.PresenceWorking, r.clock.now()
		r.write(t, working)
		r.b.now = r.clock.now
		return r.clock.now()
	}
	r.clock.advance(t, session.PresenceWindow+appClosePresenceMargin)
	if n, _ := r.conv.counts(); n != 0 {
		t.Fatal("fresh new work resumed")
	}
	r.clock.advance(t, session.PresenceWindow+appClosePresenceMargin)
	r.rec.waitFor(t, time.Second, func(_ string, d any) bool {
		e, ok := d.(appwire.AgentChanged)
		return ok && e.Agent.State == appwire.StateIdle
	})
	r.wantFooter(t, appwire.FooterStoppedByClose, appCloseStoppedWord)
}

// C8 (F5): ownership lost in open, or a lock refusal with no heartbeat, rolls
// admission back completely instead of recording a permanent stopped notice.
func TestAppCloseOwnershipConflictAbandonsAdmission(t *testing.T) {
	for _, outcome := range []string{"conflict", "person reset and conflict", "journal lock", "joined", "fresh after open", "other error"} {
		t.Run(outcome, func(t *testing.T) {
			r := newCloseRig(t, session.PresenceWorking, time.Minute)
			r.b.open = func(appOpenAsk) (*appConn, error) {
				switch outcome {
				case "conflict":
					return nil, appwire.Errorf(appwire.CodeConflict, "held elsewhere")
				case "person reset and conflict":
					r.b.resetCloseResumes(r.id)
					return nil, appwire.Errorf(appwire.CodeConflict, "held elsewhere")
				case "journal lock":
					return nil, &session.SessionLockedError{Path: r.file}
				case "other error":
					return nil, errors.New("cannot open record")
				case "fresh after open":
					p := r.p
					p.PID++
					p.State = session.PresenceWaiting
					p.UpdatedAt = r.clock.now()
					r.write(t, p)
				}
				return &appConn{Conv: r.conv, Transcript: r.file, Workspace: r.p.Workspace, Assembled: outcome != "joined", Release: func() {}}, nil
			}
			r.start()
			r.clock.advance(t, 0)
			if outcome == "other error" {
				r.wantFooter(t, appwire.FooterStoppedByClose, appCloseStoppedWord)
				return
			}
			f := closeDisk(t, r)
			if f.CloseResumes[r.id] != 0 || !f.CloseResumedAt[r.id].IsZero() || !f.CloseHandled[r.id].IsZero() || f.ClosePending[r.id] != "" || !f.ClosePendingAt[r.id].IsZero() || len(f.CloseStarts) != 0 {
				t.Errorf("conflict retained admission: %+v", f)
			}
			if len(closeFooters(r.entries(t))) != 0 {
				t.Fatal("holder received stopped notice")
			}
			if outcome == "fresh after open" {
				if a := r.b.agent(r.id); a.view.State != appwire.StateNeedsYou {
					t.Fatalf("holder state=%s", a.view.State)
				}
			}
		})
	}
}

// C8 (F5): guard, breaker and age refusals recheck the holder immediately before
// writing their notice, including an idle holder that needs no expiry watch.
func TestAppCloseRefusalRechecksForeignHolder(t *testing.T) {
	for _, reason := range []string{"guard", "breaker", "age"} {
		t.Run(reason, func(t *testing.T) {
			r := newCloseRig(t, session.PresenceWorking, time.Minute)
			if reason == "age" {
				r.p.UpdatedAt = r.clock.now().Add(-2 * time.Hour)
				r.write(t, r.p)
			}
			_ = r.b.state.write(func(f *appStateFile) error {
				if reason == "guard" {
					f.CloseResumes = map[string]int{r.id: 2}
				}
				if reason == "breaker" {
					f.CloseStarts = []time.Time{r.clock.now().Add(-time.Minute), r.clock.now().Add(-2 * time.Minute)}
				}
				return nil
			})
			r.b.snapshotCloseClaims()
			a, err := r.b.find(r.id)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.b.settleDayLimit(); err != nil {
				t.Fatal(err)
			}
			before := closeDisk(t, r)
			calls := 0
			r.b.now = func() time.Time {
				calls++
				// The first read checks the stale claim; admission reads the clock
				// once more. Replace ownership on the final outcome check.
				if calls == 3 {
					p := r.p
					p.PID++
					p.State = session.PresenceIdle
					p.UpdatedAt = r.clock.now()
					r.write(t, p)
				}
				return r.clock.now()
			}
			r.b.carryAfterClose(a, r.b.closeRecovery.claims[filepath.Dir(r.file)].presence)
			r.b.now = r.clock.now
			after := closeDisk(t, r)
			if after.CloseResumes[r.id] != before.CloseResumes[r.id] || !after.CloseHandled[r.id].IsZero() || after.ClosePending[r.id] != "" || len(after.CloseStarts) != len(before.CloseStarts) {
				t.Fatalf("refusal claimed a new holder: %+v", after)
			}
			if len(closeFooters(r.entries(t))) != 0 {
				t.Fatal("fresh holder got stopped notice")
			}
		})
	}
}

// C4 (F6): the checkpoint and index may lead their writer's heartbeat by one
// tick. Only an absent/stale holder or a later complete heartbeat proves loss.
func TestAppCloseTaskHeartbeatMustPostdateStart(t *testing.T) {
	for _, age := range []time.Duration{-time.Second, session.PresenceHeartbeat - time.Nanosecond, session.PresenceHeartbeat} {
		t.Run(age.String(), func(t *testing.T) {
			r := newCloseRig(t, session.PresenceWorking, 0)
			start := r.clock.now().Add(-age)
			nodes := map[string]any{"type": "tasks", "version": 1, "seq": 2, "nodes": []map[string]any{{"id": 1, "title": "checkpoint", "brief": "b", "acceptance": "a", "state": "running", "startedAt": start}}}
			writeCloseJSON(t, filepath.Join(filepath.Dir(r.file), "tasks.json"), nodes)
			raw, _ := json.Marshal(session.TaskIndexEntry{ID: "2", SessionID: r.id, Title: "index", Status: "running", StartedAt: start})
			if err := os.WriteFile(session.TaskIndexPath(r.file), append(raw, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			rows, err := r.b.ListTasks(r.id)
			if err != nil || len(rows) != 2 {
				t.Fatalf("tasks=%+v,%v", rows, err)
			}
			for _, row := range rows {
				want := appwire.TaskRunning
				if age >= session.PresenceHeartbeat {
					want = appwire.TaskFailed
				}
				if row.State != want {
					t.Fatalf("heartbeat age=%s task=%+v", age, row)
				}
			}
		})
	}
}

// C4 (F6): an engine-owned task wins even while the event lane and heartbeat
// still lag behind the checkpoint the listing just read.
func TestAppCloseTaskUsesAuthoritativeLocalState(t *testing.T) {
	r := newCloseRig(t, session.PresenceIdle, time.Minute)
	writeCloseJSON(t, filepath.Join(filepath.Dir(r.file), "tasks.json"), map[string]any{"type": "tasks", "version": 1, "seq": 2, "nodes": []map[string]any{{"id": 1, "title": "new work", "brief": "b", "acceptance": "a", "state": "running"}}})
	_, live, err := r.b.heldLive(r.id)
	if err != nil {
		t.Fatal(err)
	}
	r.conv.mu.Lock()
	r.conv.appTasks = []session.AppTaskView{{ID: "1", Title: "new work", State: "running"}}
	r.conv.mu.Unlock()
	r.b.locked(func() {
		live.tasks[appTaskID(r.id, 1)] = appwire.Task{ID: appTaskID(r.id, 1), State: appwire.TaskQueued}
	})
	rows, err := r.b.ListTasks(r.id)
	if err != nil || len(rows) != 1 || rows[0].State != appwire.TaskRunning {
		t.Fatalf("local tasks=%+v,%v", rows, err)
	}
}

// C6 (F7, J1): a real session in another window stamps the person's direction
// even while this app was absent. Its own resume aside never stamps activity.
func TestAppClosePersonInAnyWindowResetsStreak(t *testing.T) {
	for _, pending := range []bool{false, true} {
		for _, note := range []bool{false, true} {
			t.Run(map[bool]string{false: "claim", true: "pending"}[pending]+"/"+map[bool]string{false: "person", true: "aside"}[note], func(t *testing.T) {
				r := newCloseRig(t, session.PresenceWorking, time.Minute)
				last := time.Now().Add(-time.Minute)
				r.clock.at = last.Add(2 * time.Minute)
				r.p.UpdatedAt = last
				_ = r.b.state.write(func(f *appStateFile) error {
					f.CloseResumes = map[string]int{r.id: 2}
					f.CloseResumedAt = map[string]time.Time{r.id: last}
					if pending {
						f.ClosePending = map[string]string{r.id: "owed"}
						f.ClosePendingAt = map[string]time.Time{r.id: last}
					}
					return nil
				})
				meta := closeOtherWindow(t, r, note)
				if meta.LastUserAt.After(last) == note {
					t.Fatalf("person stamp=%v, attempt=%v, note=%v", meta.LastUserAt, last, note)
				}
				if pending {
					r.p.State = session.PresenceIdle
				}
				r.write(t, r.p)
				r.start()
				r.clock.advance(t, 0)
				want := 0
				if !note && !pending {
					want = 1
				}
				if n, _ := r.conv.counts(); n != want {
					t.Fatalf("resumes=%d want %d", n, want)
				}
				if !note && !pending && closeDisk(t, r).CloseResumes[r.id] != 1 {
					t.Fatal("new person input did not reset before admission")
				}
				if pending && !note {
					f := closeDisk(t, r)
					if f.ClosePending[r.id] != "" || f.CloseResumes[r.id] != 0 || len(f.CloseStarts) != 0 || len(closeFooters(r.entries(t))) != 0 {
						t.Fatal("person's newer message did not cancel the promise")
					}
				}
			})
		}
	}
}

func writeCloseJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func appendCloseMessage(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(append(raw, '\n'))
	end := f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if end != nil {
		t.Fatal(end)
	}
}

// C12 (F8): a preexisting fixed-name temporary file cannot be truncated by a
// footer write, and unique temporaries are cleaned up after publication.
func TestAppCloseFooterUsesUniqueTemporary(t *testing.T) {
	r := newCloseRig(t, session.PresenceIdle, 0)
	a, err := r.b.find(r.id)
	if err != nil {
		t.Fatal(err)
	}
	fixed := filepath.Join(filepath.Dir(r.file), appFootersName+".new")
	if err := os.WriteFile(fixed, []byte("another writer"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.b.keepAppFooter(a, "receipt", appwire.FooterResumedAfterClose, appCloseResumedWord); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(fixed); err != nil || string(raw) != "another writer" {
		t.Fatalf("fixed temporary overwritten: %q,%v", raw, err)
	}
	left, err := filepath.Glob(filepath.Join(filepath.Dir(r.file), appFootersName+".*.new"))
	if err != nil || len(left) != 0 {
		t.Fatalf("left temporary files=%v,%v", left, err)
	}
	r.wantFooter(t, appwire.FooterResumedAfterClose, appCloseResumedWord)
}

// C12 (F9): a receipt belongs to its interrupted entry through growth and
// paging. Rewind removes that anchor, even if replacement work reuses its id.
func TestAppCloseFooterAnchorsSurviveGrowthAndDropAfterRewind(t *testing.T) {
	r := newCloseRig(t, session.PresenceIdle, 0)
	prefix, err := os.ReadFile(r.file)
	if err != nil {
		t.Fatal(err)
	}
	appendCloseMessage(t, r.file, map[string]any{"type": "message", "role": "assistant", "content": "interrupted answer", "timestamp": r.clock.now()})
	a, err := r.b.find(r.id)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.b.keepAppFooter(a, "receipt", appwire.FooterResumedAfterClose, appCloseResumedWord); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"later work", "more work"} {
		appendCloseMessage(t, r.file, map[string]any{"type": "message", "role": "assistant", "content": text, "timestamp": r.clock.now().Add(time.Minute)})
	}
	_ = r.b.Close()
	r.backend(t)
	entries := r.entries(t)
	if len(entries) != 5 || entries[1].Text != "interrupted answer" || entries[2].ID != "receipt" || entries[3].Text != "later work" {
		t.Fatalf("anchored entries=%+v", entries)
	}
	page, more, err := r.b.Entries(r.id, "receipt", 1)
	if err != nil || !more || len(page) != 1 || page[0].Text != "interrupted answer" {
		t.Fatalf("anchor page=%+v,%v,%v", page, more, err)
	}
	if err := os.WriteFile(r.file, prefix, 0600); err != nil {
		t.Fatal(err)
	}
	if len(closeFooters(r.entries(t))) != 0 {
		t.Fatal("rewound receipt still visible")
	}
	appendCloseMessage(t, r.file, map[string]any{"type": "message", "role": "assistant", "content": "replacement answer", "timestamp": r.clock.now().Add(2 * time.Minute)})
	appendCloseMessage(t, r.file, map[string]any{"type": "message", "role": "assistant", "content": "replacement work", "timestamp": r.clock.now().Add(3 * time.Minute)})
	_ = r.b.Close()
	r.backend(t)
	if len(closeFooters(r.entries(t))) != 0 {
		t.Fatal("receipt drifted into replacement work with the same wire id")
	}
}

// C12 (F9): old sidecars retain positional placement, while a new receipt can
// explicitly anchor before everything, rather than using the old format.
func TestAppCloseFooterLegacyAndBeforeEverything(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "before everything", true: "legacy"}[legacy], func(t *testing.T) {
			r := newCloseRig(t, session.PresenceIdle, 0)
			if legacy {
				writeCloseJSON(t, filepath.Join(filepath.Dir(r.file), appFootersName), []appKeptFooter{{After: 1, Entry: appFooter("old", r.id, 0, appwire.FooterResumedAfterQuit, appResumedWord, r.clock.now())}})
			} else {
				if err := os.WriteFile(r.file, []byte(`{"type":"session","version":1}`+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				a, err := r.b.find(r.id)
				if err != nil {
					t.Fatal(err)
				}
				if err := r.b.keepAppFooter(a, "first", appwire.FooterResumedAfterClose, appCloseResumedWord); err != nil {
					t.Fatal(err)
				}
				appendCloseMessage(t, r.file, map[string]any{"type": "message", "role": "user", "content": "new message"})
			}
			entries := r.entries(t)
			at := 0
			if legacy {
				at = 1
			}
			if len(entries) != 2 || entries[at].Footer == "" {
				t.Fatalf("entries=%+v", entries)
			}
		})
	}
}

// C3, C13 (F3): a rejected wake is still owed even if a different turn started
// during the call. Acceptance is required before clearing its durable marker.
func TestAppCloseRejectedWakeRetainsPending(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	r.conv.resumeErr = errors.New("wake was not accepted")
	r.start()
	r.clock.advance(t, 0)
	a := r.b.agent(r.id)
	events := make(chan session.Event, 1)
	events <- session.Event{Kind: session.EventError, Text: "unrelated failed turn"}
	close(events)
	r.b.wokenTurn(a, r.b.liveOf(a), appTurnElsewhere{events: events})
	if closeDisk(t, r).ClosePending[r.id] == "" {
		t.Fatal("rejected wake lost its pending continuation")
	}
}

// C4 (F6): merely holding a conversation locally cannot keep an abandoned
// index-only task running. Only tasks actually held by its engine get overlaid.
func TestAppCloseTaskLocalHoldDoesNotKeepAbandonedIndexRunning(t *testing.T) {
	r := newCloseRig(t, session.PresenceIdle, time.Minute)
	raw, err := json.Marshal(session.TaskIndexEntry{ID: "99", SessionID: r.id, Title: "old work", Status: "running", StartedAt: r.clock.now().Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session.TaskIndexPath(r.file), append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.b.heldLive(r.id); err != nil {
		t.Fatal(err)
	}
	rows, err := r.b.ListTasks(r.id)
	if err != nil || len(rows) != 1 || rows[0].State != appwire.TaskFailed || rows[0].Result != "Interrupted" {
		t.Fatalf("abandoned index task=%+v,%v", rows, err)
	}
}
