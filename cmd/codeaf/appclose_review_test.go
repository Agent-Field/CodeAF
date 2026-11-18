package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
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

// C1, C2 (F4): the first expiry read sees idle, then the follow-up sees a new
// working heartbeat. That new holder dies without another client read.
func TestAppCloseIdleToWorkingFollowupKeepsWatch(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, 2*time.Second)
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
	r.wantFooter(t, appwire.FooterInterrupted, appInterruptedWord)
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
				if n, _ := r.conv.counts(); n != 0 {
					t.Fatalf("carried on by itself %d times", n)
				}
				if !pending && !r.b.interrupted(r.id) {
					t.Fatal("the death was not left for Continue")
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
