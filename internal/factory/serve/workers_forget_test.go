package serve

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// A computer's quiet registrations are forgotten when it attaches again; its
// recent ones, one a lease names, and every other computer's and person's stay.
func TestAnAttachForgetsOnlyItsOwnComputersQuietRegistrations(t *testing.T) {
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	s := &workerService{now: func() time.Time { return now }, credentials: map[[32]byte]registeredWorker{}}
	row := func(key, id, handle, machine string, quiet time.Duration) [32]byte {
		k := sha256.Sum256([]byte(key))
		s.credentials[k] = registeredWorker{Last: now.Add(-quiet), Worker: lease.Worker{ID: id, Handle: handle, Machine: machine}}
		return k
	}
	fresh := row("fresh", "w-new", "sam", "mbp", 0)
	old := row("old", "w-old", "sam", "mbp", 2*time.Minute)
	older := row("older", "w-older", "sam", "mbp", 72*time.Hour)
	recent := row("recent", "w-recent", "sam", "mbp", 30*time.Second)
	working := row("working", "w-working", "sam", "mbp", time.Hour)
	other := row("other", "w-other", "sam", "studio", time.Hour)
	theirs := row("theirs", "w-theirs", "mei", "mbp", time.Hour)

	s.forgetQuietLocked(lease.Worker{ID: "w-new", Handle: "sam", Machine: "mbp"}, []lease.Lease{{Worker: "w-working"}}, time.Minute)

	for name, k := range map[string][32]byte{"old": old, "older": older} {
		if _, ok := s.credentials[k]; ok {
			t.Errorf("the %s quiet registration of the same computer was kept", name)
		}
	}
	for name, k := range map[string][32]byte{"the new one": fresh, "a recent one": recent, "one a lease names": working, "another computer's": other, "another person's": theirs} {
		if _, ok := s.credentials[k]; !ok {
			t.Errorf("%s was forgotten", name)
		}
	}
}
