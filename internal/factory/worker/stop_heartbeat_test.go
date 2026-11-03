package worker_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestAStopSurvivesALostHeartbeatReply(t *testing.T) {
	var armed, lost, subsequent atomic.Bool
	captured, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseReply := func() { releaseOnce.Do(func() { close(release) }) }
	g, seen := compatRig(t, func(next http.Handler, _ *reportLog) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if lost.Load() && !subsequent.Load() && strings.HasSuffix(r.URL.Path, "/report") {
				http.Error(w, "report unavailable", http.StatusServiceUnavailable)
				return
			}
			if !strings.HasSuffix(r.URL.Path, "/heartbeat") {
				next.ServeHTTP(w, r)
				return
			}
			reply := httptest.NewRecorder()
			next.ServeHTTP(reply, r)
			if armed.Load() && lost.CompareAndSwap(false, true) {
				close(captured)
				<-release
				// The successful renewal predates Stop, but its reply is lost after Stop.
				panic(http.ErrAbortHandler)
			}
			if lost.Load() {
				subsequent.Store(true)
			}
			for key, values := range reply.Header() {
				w.Header()[key] = values
			}
			w.WriteHeader(reply.Code)
			_, _ = w.Write(reply.Body.Bytes())
		})
	})
	t.Cleanup(releaseReply)
	next := check("echo paid-next-step")
	next.Name = "next"
	id := g.add(t, check("sleep 30"), next)
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		it, _ := g.st.Get(id)
		if logHas(it, "check: sleep 30") > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker never began the leased step")
		}
	}
	armed.Store(true)
	select {
	case <-captured:
	case <-time.After(5 * time.Second):
		t.Fatal("heartbeat never entered")
	}
	if err := g.floor.Stop(id); err != nil {
		releaseReply()
		t.Fatal(err)
	}
	releaseReply()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if subsequent.Load() && !g.run.Running(id) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("next heartbeat did not end runner: next=%t running=%t", subsequent.Load(), g.run.Running(id))
		}
	}
	if seen.steps.Load() != 1 {
		t.Fatalf("Stop admitted %d further paid steps; want zero", seen.steps.Load()-1)
	}
	it, err := g.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !factory.StoppedByPerson(it) || it.Stream.Spent != compatSpend || it.Stream.Phases[1].State != factory.PhasePending || logHas(it, "check: echo paid-next-step") != 0 {
		t.Fatalf("Stop admitted further paid steps: %+v", it)
	}
}
