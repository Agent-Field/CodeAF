package worker_test

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestAuthorityOutageHoldsOnWorkerFloor(t *testing.T) {
	var reads atomic.Int32
	var recovered atomic.Bool
	g, seen := compatRig(t, func(next http.Handler, _ *reportLog) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/stop-after-step") && reads.Add(1) > 1 && !recovered.Load() {
				http.Error(w, "authority unavailable", http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	id := g.add(t, named("proof"), named("paid_next"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := g.floor.Load()
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range snapshot.Items {
			if it.ID != id {
				continue
			}
			explained := false
			if it.Stream != nil {
				for _, line := range it.Stream.Log {
					if strings.Contains(line.Text, "could not confirm Stop") {
						explained = true
					}
				}
			}
			if explained && (it.State == factory.StateNeedsYou || it.Stream.Paused) {
				if it.Failure != nil || factory.StoppedByPerson(it) || seen.steps.Load() != 1 {
					t.Fatalf("authority hold changed work: state=%s failure=%+v steps=%d", it.State, it.Failure, seen.steps.Load())
				}
				recovered.Store(true)
				if err := g.doors.Answer(id, true, "retry"); err != nil {
					t.Fatal(err)
				}
				for time.Now().Before(deadline) {
					cur, _ := g.st.Get(id)
					if cur.Stream != nil && len(cur.Stream.Phases) == 2 && cur.Stream.Phases[1].State == factory.PhaseDone {
						if seen.steps.Load() != 2 || cur.Stream.Phases[0].Round != 1 || cur.Stream.Phases[1].Round != 1 {
							t.Fatalf("retry repeated paid work: steps=%d phases=%+v", seen.steps.Load(), cur.Stream.Phases)
						}
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatal("person retry did not complete the next step")
			}
		}
		if time.Now().After(deadline) {
			it, _ := g.st.Get(id)
			t.Fatalf("worker did not expose authority hold: state=%s paused=%v failure=%+v steps=%d", it.State, it.Stream != nil && it.Stream.Paused, it.Failure, seen.steps.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
