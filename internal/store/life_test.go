package store

import (
	"path/filepath"
	"testing"
	"time"
)

// The property this file locks: a restart may not shorten a job's clock.
//
// The incident is in life.go's header. What is asserted here is the mechanism
// underneath it — that the admission stamp survives a release-and-reclaim that
// wipes started_at, and that the attempt counter comes back with it.

func lifeStore(t *testing.T, name string) *Store {
	t.Helper()
	return openTestStore(t, filepath.Join(t.TempDir(), name+".db"))
}

func spliceLifeJob(t *testing.T, graph *Store, id, title string) {
	t.Helper()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: id, Title: title, Brief: title, Stage: 1,
	}}}, Provenance{Origin: OriginUser, SessionID: "life", Intent: title}); err != nil {
		t.Fatalf("splice %q: %v", id, err)
	}
}

// A node restarted after a deadline keeps the clock it was admitted on. The
// start column is deliberately checked as well: the point is not that the
// attempt stamp is wrong, it is that it is a DIFFERENT fact, and both are
// available so no caller has to guess which one it got.
func TestARestartDoesNotShortenAJobsClock(t *testing.T) {
	graph := lifeStore(t, "restart")
	spliceLifeJob(t, graph, "bench", "SVM benchmark")

	first := runNode(t, graph, "bench")
	life, found, err := graph.NodeLife("bench")
	if err != nil || !found {
		t.Fatalf("node life found=%t err=%v", found, err)
	}
	if life.Attempt != 1 || life.Restarted() {
		t.Fatalf("a first go reads as attempt %d restarted=%t", life.Attempt, life.Restarted())
	}
	if life.AttemptWords() != "" {
		t.Fatalf("a first attempt announced itself as %q", life.AttemptWords())
	}
	admitted := life.Admitted
	if admitted.IsZero() {
		t.Fatal("the admission stamp is missing, so there is no clock to keep")
	}

	// The deadline restart: released back to pending, which clears started_at,
	// then claimed and started again.
	if err := graph.Release(first); err != nil {
		t.Fatalf("release: %v", err)
	}
	released, found, err := graph.Node("bench")
	if err != nil || !found {
		t.Fatalf("node found=%t err=%v", found, err)
	}
	if !released.StartedAt.IsZero() {
		t.Fatal("release left a start stamp behind, so this test is not exercising the defect")
	}
	runNode(t, graph, "bench")

	life, found, err = graph.NodeLife("bench")
	if err != nil || !found {
		t.Fatalf("node life after restart found=%t err=%v", found, err)
	}
	if !life.Admitted.Equal(admitted) {
		t.Fatalf("the admission stamp moved on a restart: %s then %s", admitted, life.Admitted)
	}
	if life.Attempt != 2 || !life.Restarted() {
		t.Fatalf("a restarted job reads as attempt %d restarted=%t", life.Attempt, life.Restarted())
	}
	if life.AttemptWords() != "second attempt" {
		t.Fatalf("attempt 2 is spelled %q", life.AttemptWords())
	}
	if life.AttemptStarted.Before(admitted) {
		t.Fatalf("the new attempt started before the job was admitted: %s vs %s",
			life.AttemptStarted, admitted)
	}
}

// The clock itself, with the stamps written by hand so the answer is knowable.
// Thirty-two minutes of job and forty seconds of attempt: the two figures a
// reader must never be handed in place of one another.
func TestAJobsElapsedIsMeasuredFromAdmissionAndNotFromItsCurrentAttempt(t *testing.T) {
	now := time.Date(2026, 8, 12, 15, 0, 0, 0, time.UTC)
	life := JobLife{
		NodeID:         "bench",
		Status:         Running,
		Admitted:       now.Add(-32 * time.Minute),
		Attempt:        2,
		AttemptStarted: now.Add(-40 * time.Second),
	}
	elapsed, known := life.Elapsed(now)
	if !known || elapsed != 32*time.Minute {
		t.Fatalf("elapsed = %s known=%t, want 32m", elapsed, known)
	}
	since, known := life.SinceRestart(now)
	if !known || since != 40*time.Second {
		t.Fatalf("since restart = %s known=%t, want 40s", since, known)
	}

	// A settled job measures to its finish rather than to now.
	life.Status, life.FinishedAt = Done, now.Add(-2*time.Minute)
	if elapsed, known := life.Elapsed(now); !known || elapsed != 30*time.Minute {
		t.Fatalf("a settled job's elapsed = %s known=%t, want 30m", elapsed, known)
	}

	// Absent is not zero: a node the journal has no admission stamp for has no
	// clock, and a caller must be able to tell that from a clock reading zero.
	if _, known := (JobLife{Status: Running}).Elapsed(now); known {
		t.Fatal("a job with no admission stamp reported a clock")
	}
	// A first attempt has no restart to date.
	if _, known := (JobLife{Attempt: 1, Admitted: now}).SinceRestart(now); known {
		t.Fatal("a first attempt reported a restart")
	}
}

// Presence, and the two ways a caller can ask for nothing.
func TestNodeLifeReportsAbsenceRatherThanAnEmptyJob(t *testing.T) {
	graph := lifeStore(t, "absent")
	if _, found, err := graph.NodeLife("never-spliced"); err != nil || found {
		t.Fatalf("an unknown node = found %t err=%v", found, err)
	}
	if _, _, err := graph.NodeLife("  "); err == nil {
		t.Fatal("an empty id was accepted")
	}
}
