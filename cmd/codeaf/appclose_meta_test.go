package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

// closeOtherWindow uses a real session's journal and deferred metadata writer,
// then releases its journal as another window closing would. Its presence is
// separate from the left-behind claim each recovery test installs afterwards.
func closeOtherWindow(t *testing.T, r *closeRig, note bool) session.Meta {
	t.Helper()
	model := newRecordingModel(t)
	sources := modelsource.NewSet(modelsource.Connected{
		Source: modelsource.DefaultSource(model.server.URL), Key: "test-key", Address: model.server.URL,
	})
	engine, err := session.New(session.Config{
		Workspace: r.p.Workspace, Place: session.Place{Dir: filepath.Dir(r.file)},
		SessionFile: r.file, Model: "openai/gpt-4.1-mini", Sources: sources, System: "SYSTEM",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	var events <-chan session.Event
	if note {
		wakes, stop := engine.WatchWakes()
		defer stop()
		if !engine.ResumeAfterClose() {
			t.Fatal("other window refused its session-authored note")
		}
		select {
		case events = <-wakes:
		case <-time.After(5 * time.Second):
			t.Fatal("other window did not start its wake")
		}
	} else {
		events, err = engine.Submit(t.Context(), "different direction")
		if err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				engine.SettleWrites()
				if err := engine.Close(); err != nil {
					t.Fatal(err)
				}
				meta, err := session.LoadMeta(filepath.Dir(r.file))
				if err != nil || meta.ID == "" {
					t.Fatalf("other window metadata=%+v, %v", meta, err)
				}
				return meta
			}
			if event.Kind == session.EventError {
				t.Fatalf("other window turn failed: %+v", event)
			}
		case <-deadline:
			t.Fatal("other window did not finish")
		}
	}
}

// J1, F7, G2: after a copied window is rewound away, another window's genuine
// person message still resets the streak and cancels the original promise.
// The production compaction writer is exercised by the session metadata test;
// this backend fixture pins the exact journal shape the round-4 scan misread.
func TestAppClosePersonAfterCompactedWindowRewind(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "claim", true: "pending"}[pending], func(t *testing.T) {
			r := newCloseRig(t, session.PresenceWorking, time.Minute)
			last := time.Now().Add(-time.Minute)
			r.clock.at = last.Add(2 * time.Minute)
			r.p.UpdatedAt = last
			if err := r.b.state.write(func(f *appStateFile) error {
				f.CloseResumes[r.id] = 2
				f.CloseResumedAt[r.id] = last
				if pending {
					f.ClosePending[r.id] = "owed"
					f.ClosePendingAt[r.id] = last
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			for _, entry := range []map[string]any{
				{"type": "message", "role": "assistant", "content": "first file is ready"},
				{"type": "compaction", "window": 2},
				{"type": "message", "role": "user", "content": "finish the work"},
				{"type": "message", "role": "assistant", "content": "first file is ready"},
				{"type": "rewind", "dropped": 2},
			} {
				entry["timestamp"] = last.Add(time.Second)
				appendCloseMessage(t, r.file, entry)
			}
			meta := closeOtherWindow(t, r, false)
			if !meta.LastUserAt.After(last) {
				t.Fatalf("genuine direction did not stamp metadata: %v <= %v", meta.LastUserAt, last)
			}
			if pending {
				r.p.State = session.PresenceIdle
			}
			r.write(t, r.p)
			r.start()
			r.clock.advance(t, 0)
			f := closeDisk(t, r)
			if pending {
				if n, _ := r.conv.counts(); n != 0 || f.ClosePending[r.id] != "" || f.CloseResumes[r.id] != 0 || len(f.CloseStarts) != 0 || len(closeFooters(r.entries(t))) != 0 {
					t.Fatalf("rewind hid the person's cancellation: resumes=%d state=%+v", n, f)
				}
			} else if n, _ := r.conv.counts(); n != 1 || f.CloseResumes[r.id] != 1 {
				t.Fatalf("rewind hid the person's streak reset: resumes=%d state=%+v", n, f)
			}
		})
	}
}

// J1, C6: unreadable or blank metadata cannot authorize a pending replay or a
// new attempt. Missing and corrupt files are LoadMeta's blank-meta cases.
func TestAppCloseMetaUnavailableRefusesAutomaticWork(t *testing.T) {
	for _, pending := range []bool{false, true} {
		for _, kind := range []string{"missing", "corrupt", "no identity", "unreadable"} {
			t.Run(map[bool]string{false: "claim/", true: "pending/"}[pending]+kind, func(t *testing.T) {
				r := newCloseRig(t, session.PresenceWorking, time.Minute)
				if pending {
					r.p.State = session.PresenceIdle
					r.write(t, r.p)
				}
				if err := r.b.state.write(func(f *appStateFile) error {
					f.CloseResumes[r.id] = 1
					f.CloseResumedAt[r.id] = r.clock.now().Add(-time.Minute)
					if pending {
						f.ClosePending[r.id] = "owed"
						f.ClosePendingAt[r.id] = r.clock.now().Add(-time.Minute)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				// Select while the conversation is still discoverable; a later
				// metadata failure must leave already-selected work untouched.
				r.b.snapshotCloseClaims()
				a, err := r.b.find(r.id)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(filepath.Dir(r.file), "meta.json")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "corrupt", "no identity":
					raw := "{"
					if kind == "no identity" {
						raw = "{}"
					}
					if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
						t.Fatal(err)
					}
				case "unreadable":
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				}
				if reset, err := r.b.resetCloseAfterPerson(a); err == nil || reset {
					t.Fatalf("unavailable metadata did not refuse: reset=%v, err=%v", reset, err)
				}
				if pending {
					r.b.replayClosePending(r.b.closeRecovery.pending[0])
				} else {
					claim := r.b.closeRecovery.claims[filepath.Dir(r.file)]
					r.b.checkCloseClaim(filepath.Dir(r.file), claim)
				}
				if n, _ := r.conv.counts(); n != 0 {
					t.Fatal("unavailable metadata permitted automatic work")
				}
				f := closeDisk(t, r)
				if f.CloseResumes[r.id] != 1 || len(f.CloseStarts) != 0 || len(closeFooters(r.entries(t))) != 0 || (pending && f.ClosePending[r.id] != "owed") {
					t.Fatalf("unavailable metadata changed recovery state: %+v", f)
				}
			})
		}
	}
}
