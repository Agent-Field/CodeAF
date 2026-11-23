package worker_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestWorkerLostReplyAfterReportAppliedDoesNotRepeatCost(t *testing.T) {
	var lost atomic.Int32
	g, _ := compatRig(t, func(next http.Handler, seen *reportLog) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isReport(r) {
				body := peek(r)
				seen.add(body)
				if strings.Contains(body, `"cost":0.25`) && lost.CompareAndSwap(0, 1) {
					if r.Header.Get("Idempotency-Key") == "" {
						t.Error("report has no retry identity")
					}
					captured := httptest.NewRecorder()
					next.ServeHTTP(captured, r)
					if captured.Code != 200 {
						t.Errorf("first apply: %d %s", captured.Code, captured.Body.String())
					}
					panic(http.ErrAbortHandler)
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	id := g.add(t, check("true"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitFor(t, id, factory.StateShipped)
	if lost.Load() != 1 || it.Stream == nil || it.Stream.Spent != compatSpend {
		t.Fatalf("after lost reply: injected=%d spent=%+v", lost.Load(), it.Stream)
	}
}
