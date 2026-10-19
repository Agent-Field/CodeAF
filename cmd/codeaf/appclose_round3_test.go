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

// C3, C6, C13 (G1): accepting a note behind a refused wake does not attach it
// to an unrelated buffered turn, even when that turn is adopted afterwards.
func TestAppCloseUnrelatedBufferedTurnLeavesContinuationOwed(t *testing.T) {
	for _, ok := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "normal ending"}[ok], func(t *testing.T) {
			r := newCloseRig(t, session.PresenceWorking, time.Minute)
			r.start()
			r.clock.advance(t, 0)
			key := closeDisk(t, r).ClosePending[r.id]
			closeConsumeTurn(t, r, "unrelated-key", ok)
			if closeDisk(t, r).ClosePending[r.id] != key {
				t.Fatal("unrelated turn completed the pending note")
			}
			r.p.State = session.PresenceIdle
			r.write(t, r.p)
			_ = r.b.Close()
			r.clock.advance(t, 11*time.Minute)
			r.backend(t)
			r.start()
			r.clock.advance(t, 0)
			if n, _ := r.conv.counts(); n != 2 {
				t.Fatalf("owed resumes=%d", n)
			}
			r.wantFooter(t, appwire.FooterResumedAfterClose, appCloseResumedWord)
		})
	}
}

// C6, C13 (G2): a person action after selection wins in the admission write,
// even if another continuation has replaced the one the callback selected.
func TestAppCloseAdmissionRequiresSelectedPendingKey(t *testing.T) {
	for _, replacement := range []string{"", "new-promise"} {
		t.Run("replacement="+replacement, func(t *testing.T) {
			r := newCloseRig(t, session.PresenceIdle, time.Minute)
			_ = r.b.state.write(func(f *appStateFile) error {
				f.ClosePending[r.id] = "old-promise"
				f.ClosePendingAt[r.id] = r.clock.now()
				f.CloseResumes[r.id] = 1
				return nil
			})
			r.b.snapshotCloseClaims()
			work := appPendingCloseWork(r.b.closeRecovery.pending[0])
			r.b.now = func() time.Time {
				r.b.resetCloseResumes(r.id)
				if replacement != "" {
					_ = r.b.state.write(func(f *appStateFile) error {
						f.ClosePending[r.id] = replacement
						f.ClosePendingAt[r.id] = r.clock.now()
						return nil
					})
				}
				return r.clock.now()
			}
			resume, undo, err := r.b.admitCloseWork(r.id, work)
			if resume || undo != nil || !errors.Is(err, errAppCloseCancelled) {
				t.Fatalf("admission=%v,%v", resume, err)
			}
			f := closeDisk(t, r)
			if f.ClosePending[r.id] != replacement || f.CloseResumes[r.id] != 0 || len(f.CloseStarts) != 0 || len(closeFooters(r.entries(t))) != 0 {
				t.Fatalf("cancelled admission wrote state: %+v", f)
			}
		})
	}
}

// C3, C6, C9, C10 (G3): tasks may publish newer busy evidence before the reply
// starts. That evidence belongs to the original promise and cannot renew its age.
func TestAppCloseNewBusyClaimKeepsPromiseIdentityAndAge(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "second attempt", true: "original hour expired"}[expired], func(t *testing.T) {
			r := newCloseRig(t, session.PresenceWorking, time.Minute)
			r.conv.beforeResume = func() { r.p.UpdatedAt = r.clock.now(); r.p.PID++; r.write(t, r.p) }
			r.start()
			r.clock.advance(t, 0)
			original := closeDisk(t, r)
			key, at := original.ClosePending[r.id], original.ClosePendingAt[r.id]
			_ = r.b.Close()
			delay := 11 * time.Minute
			if expired {
				delay = appCloseResumeWithin + time.Second
			}
			r.clock.advance(t, delay)
			r.p.UpdatedAt = r.clock.now().Add(-time.Minute)
			r.p.PID++
			r.write(t, r.p)
			r.conv.beforeResume = nil
			r.backend(t)
			r.start()
			r.clock.advance(t, 0)
			f := closeDisk(t, r)
			if f.ClosePending[r.id] != key || !f.ClosePendingAt[r.id].Equal(at) || !f.CloseHandled[r.id].Equal(r.p.UpdatedAt) {
				t.Fatalf("promise replaced: %+v", f)
			}
			want := 2
			if expired {
				want = 1
			}
			if n, _ := r.conv.counts(); n != want {
				t.Fatalf("resumes=%d want %d", n, want)
			}
			footers := closeFooters(r.entries(t))
			if expired {
				if len(footers) != 2 || footers[1].Footer != appwire.FooterStoppedByClose {
					t.Fatalf("footers=%+v", footers)
				}
			} else if len(footers) != 1 || footers[0].ID != key {
				t.Fatalf("footers=%+v", footers)
			}
			if !expired {
				_ = r.b.Close()
				r.clock.advance(t, 11*time.Minute)
				r.p.PID++
				r.p.UpdatedAt = r.clock.now().Add(-time.Minute)
				r.write(t, r.p)
				r.backend(t)
				r.start()
				r.clock.advance(t, 0)
				if n, _ := r.conv.counts(); n != 2 {
					t.Fatal("newer claims bought a third pending attempt")
				}
				footers = closeFooters(r.entries(t))
				if len(footers) != 2 || footers[1].ID != key+"-stopped" {
					t.Fatalf("pending notices=%+v", footers)
				}
			}
		})
	}
}

// C3, C10 (G3): find can discover busy evidence after an idle pending snapshot.
// Its delayed callback must remain consumed even after the first reply finishes.
func TestAppCloseDelayedClaimCannotDispatchConsumedPromise(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	r.start()
	r.clock.advance(t, 0)
	key := closeDisk(t, r).ClosePending[r.id]
	_ = r.b.Close()
	r.clock.advance(t, 11*time.Minute)
	r.p.State = session.PresenceIdle
	r.write(t, r.p)
	r.backend(t)
	r.b.snapshotCloseClaims()
	pending := r.b.closeRecovery.pending[0]
	r.p.State = session.PresenceWorking
	r.p.UpdatedAt = r.clock.now().Add(-time.Minute)
	r.p.PID++
	r.write(t, r.p)
	r.b.replayClosePending(pending)
	path := filepath.Dir(r.file)
	claim := r.b.closeRecovery.claims[path]
	if claim == nil {
		t.Fatal("find did not enroll the newer claim")
	}
	if f := closeDisk(t, r); !f.CloseHandled[r.id].Equal(r.p.UpdatedAt) {
		t.Fatal("replay did not consume newer claim")
	}
	closeConsumeTurn(t, r, key, true)
	r.b.checkCloseClaim(path, claim)
	if n, _ := r.conv.counts(); n != 2 {
		t.Fatalf("duplicate dispatch: %d", n)
	}
	r.wantFooter(t, appwire.FooterResumedAfterClose, appCloseResumedWord)
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
				if len(f.CloseStarts) != 2 || r.b.closeRecovery.counted {
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
	original := r.clock.now().Add(-20 * time.Minute)
	_ = r.b.state.write(func(f *appStateFile) error {
		f.ClosePending[r.id] = "owed"
		f.ClosePendingAt[r.id] = original
		f.CloseResumes[r.id] = 2
		f.CloseResumedAt[r.id] = r.clock.now().Add(-time.Minute)
		return nil
	})
	appendCloseMessage(t, r.file, map[string]any{"type": "message", "role": "user", "content": "different direction", "timestamp": original.Add(time.Minute)})
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
	if len(r.b.closeRecovery.claims) != 0 || r.b.closeRecovery.counted || len(closeDisk(t, r).CloseStarts) != 0 {
		t.Fatal("deleted busy claim entered startup recovery")
	}
	if n, _ := r.conv.counts(); n != 0 {
		t.Fatal("deleted busy claim resumed")
	}
}
