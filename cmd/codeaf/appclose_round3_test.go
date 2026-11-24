package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

func closeConsumeTurn(t *testing.T, r *closeRig, key string, ok bool) {
	t.Helper()
	a := r.b.agent(r.id)
	events := make(chan session.Event, 2)
	if key != "" {
		events <- session.Event{Kind: session.EventCloseContinued, Text: key}
	}
	if ok {
		events <- session.Event{Kind: session.EventTurnDone}
	} else {
		events <- session.Event{Kind: session.EventError, Text: "unfinished turn"}
	}
	close(events)
	r.b.wokenTurn(a, r.b.liveOf(a), appTurnElsewhere{events: events})
}

// H2: an ordinary successful turn must not save app state just to clear absent
// recovery fields. A pending promise belongs to its consuming turn alone.
func TestAppCloseOrdinaryReplyDoesNotWriteState(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary reply", true: "different promise pending"}[pending], func(t *testing.T) {
			hold := make(chan struct{})
			b, rec, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}, hold: hold})
			t.Cleanup(func() {
				select {
				case <-hold:
				default:
					close(hold)
				}
			})
			turn := appSendMessageWhenIdle(t, b, agent.ID, appwire.AgentMessage{Text: "write the report"})
			at := time.Date(2000, 1, 1, 15, 0, 0, 0, time.UTC)
			if err := b.state.write(func(f *appStateFile) error {
				f.CloseResumes = map[string]int{"another-conversation": 1}
				f.CloseResumedAt = map[string]time.Time{"another-conversation": at}
				if pending {
					f.ClosePending = map[string]string{agent.ID: "owed-promise"}
					f.ClosePendingAt = map[string]time.Time{agent.ID: at}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			path := b.state.where()
			if err := os.Chtimes(path, at, at); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			close(hold)
			appTurnDoneFor(t, rec, turn)
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("ordinary completion rewrote the state file")
			}
			if err := b.state.read(func(f *appStateFile) {
				if f.CloseResumes["another-conversation"] != 1 || !f.CloseResumedAt["another-conversation"].Equal(at) {
					t.Fatal("completion changed another conversation's recovery state")
				}
				if pending && (f.ClosePending[agent.ID] != "owed-promise" || !f.ClosePendingAt[agent.ID].Equal(at)) {
					t.Fatal("unrelated completion acknowledged the pending promise")
				}
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// C7, C13 (G4): a deletion keeps its directory, but neither startup nor a
// previously selected callback may count it as work or spend an attempt.
func TestAppCloseDeletedConversationOwesNothing(t *testing.T) {
	for _, when := range []string{"startup", "after selection"} {
		for _, pendingJournal := range []bool{false, true} {
			t.Run(when+map[bool]string{false: "/done", true: "/pending"}[pendingJournal], func(t *testing.T) {
				r := newCloseRig(t, session.PresenceIdle, time.Minute)
				_ = r.b.state.write(func(f *appStateFile) error {
					f.ClosePending[r.id] = "owed"
					f.ClosePendingAt[r.id] = r.clock.now()
					f.CloseResumes[r.id] = 1
					f.CloseResumedAt[r.id] = r.clock.now()
					f.CloseHandled[r.id] = r.p.UpdatedAt
					f.CloseStarts = []time.Time{r.clock.now().Add(-time.Minute), r.clock.now().Add(-2 * time.Minute)}
					return nil
				})
				if when == "after selection" {
					r.b.snapshotCloseClaims()
				}
				if err := os.WriteFile(filepath.Join(filepath.Dir(r.file), teams.ConversationDeletedFile), nil, 0600); err != nil {
					t.Fatal(err)
				}
				if pendingJournal {
					if err := os.Rename(r.file, r.file+".delete-pending"); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Remove(r.file); err != nil {
						t.Fatal(err)
					}
				}
				r.b.open = func(appOpenAsk) (*appConn, error) {
					t.Error("deleted conversation opened")
					return nil, errors.New("deleted")
				}
				if when == "after selection" {
					r.b.replayClosePending(r.b.closeRecovery.pending[0])
				} else {
					r.start()
					r.clock.advance(t, 0)
				}
				f := closeDisk(t, r)
				if len(f.CloseStarts) != 2 {
					t.Fatal("deleted conversation counted toward breaker")
				}
				if n, _ := r.conv.counts(); n != 0 {
					t.Fatal("deleted conversation resumed")
				}
				// The next startup also removes every per-conversation recovery field.
				r.b.snapshotCloseClaims()
				f = closeDisk(t, r)
				if f.ClosePending[r.id] != "" || f.CloseResumes[r.id] != 0 || !f.CloseResumedAt[r.id].IsZero() || !f.ClosePendingAt[r.id].IsZero() || !f.CloseHandled[r.id].IsZero() {
					t.Fatalf("deleted history retained: %+v", f)
				}
			})
		}
	}
}

// C6 (G2): another retry cannot move the promise past a person's intervening
// message. Cancellation compares against the original promise, not attempt time.
func TestAppClosePersonAfterOriginalPromiseCancelsReplay(t *testing.T) {
	r := newCloseRig(t, session.PresenceIdle, time.Minute)
	original := time.Now().Add(-20 * time.Minute)
	r.clock.at = original.Add(21 * time.Minute)
	_ = r.b.state.write(func(f *appStateFile) error {
		f.ClosePending[r.id] = "owed"
		f.ClosePendingAt[r.id] = original
		f.CloseResumes[r.id] = 2
		f.CloseResumedAt[r.id] = r.clock.now()
		return nil
	})
	meta := closeOtherWindow(t, r, false)
	if !meta.LastUserAt.After(original) || !meta.LastUserAt.Before(r.clock.now()) {
		t.Fatalf("person stamp %v must be between promise %v and last attempt %v", meta.LastUserAt, original, r.clock.now())
	}
	r.write(t, r.p)
	r.start()
	r.clock.advance(t, 0)
	f := closeDisk(t, r)
	if n, _ := r.conv.counts(); n != 0 {
		t.Fatal("person's intervening message was replayed over")
	}
	if f.ClosePending[r.id] != "" || f.CloseResumes[r.id] != 0 || len(f.CloseStarts) != 0 || len(closeFooters(r.entries(t))) != 0 {
		t.Fatalf("cancelled promise left work: %+v", f)
	}
}

// C7, C13 (G4): a busy file beside a tombstone is not startup evidence of work.
func TestAppCloseDeletedBusyClaimIsExcludedFromSnapshot(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	if err := os.WriteFile(filepath.Join(filepath.Dir(r.file), teams.ConversationDeletedFile), nil, 0600); err != nil {
		t.Fatal(err)
	}
	r.start()
	r.clock.advance(t, 0)
	if len(r.b.closeRecovery.claims) != 0 || len(closeDisk(t, r).CloseStarts) != 0 {
		t.Fatal("deleted busy claim entered startup recovery")
	}
	if n, _ := r.conv.counts(); n != 0 {
		t.Fatal("deleted busy claim resumed")
	}
}
