package worker_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestAHoldSurvivesALostHeartbeatReply(t *testing.T) {
	var lost, subsequent atomic.Int32
	g, _ := compatRig(t, func(next http.Handler, _ *reportLog) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/heartbeat") {
				next.ServeHTTP(w, r)
				return
			}
			captured := httptest.NewRecorder()
			next.ServeHTTP(captured, r)
			var beat api.WorkerHeartbeat
			if err := json.Unmarshal(captured.Body.Bytes(), &beat); err != nil {
				t.Error(err)
			}
			for _, signal := range beat.Signals {
				if signal.Verb == "hold" && signal.Yes && lost.CompareAndSwap(0, 1) {
					// The factory drained the hold, but its reply never reached the worker.
					panic(http.ErrAbortHandler)
				}
			}
			if lost.Load() == 1 {
				subsequent.Add(1)
			}
			for key, values := range captured.Header() {
				w.Header()[key] = values
			}
			w.WriteHeader(captured.Code)
			_, _ = w.Write(captured.Body.Bytes())
		})
	})
	id := g.add(t, check("sleep 30"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		it, _ := g.st.Get(id)
		if logHas(it, "check: sleep 30") > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the worker never started the check")
		}
	}
	if err := g.floor.Hold(id, true); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		it, err := g.st.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if logHas(it, "paused") > 0 {
			if lost.Load() != 1 {
				t.Fatal("the hold heartbeat reply was not lost")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("hold was not delivered after a lost heartbeat reply: lost=%d subsequentHeartbeats=%d state=%s factoryPaused=%t runnerRunning=%t pausedLog=%d", lost.Load(), subsequent.Load(), it.State, it.Stream != nil && it.Stream.Paused, g.run.Running(id), logHas(it, "paused"))
		}
	}
}
