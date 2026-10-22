package syncsetup

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// withoutTakePromise is an old service's response: all existing routes work,
// but it makes no promise about conditioning a forced take.
type withoutTakePromise struct{ http.ResponseWriter }

func (w withoutTakePromise) WriteHeader(status int) {
	w.Header().Del(directory.ConditionalTakeHeader)
	w.ResponseWriter.WriteHeader(status)
}

// TestC7ContinueChecksTheServiceBeforeTaking proves C7 against the real Go
// service and its old-service shape: an unsafe service receives no take.
func TestC7ContinueChecksTheServiceBeforeTaking(t *testing.T) {
	for _, old := range []bool{true, false} {
		t.Run(map[bool]string{true: "old service", false: "current Go service"}[old], func(t *testing.T) {
			var takes atomic.Int32
			h := newTwoHomes(t, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/acquire") {
						takes.Add(1)
					}
					if old {
						w = withoutTakePromise{w}
					}
					next.ServeHTTP(w, r)
				})
			})
			a := h.openA()
			a.mustSay("C7 initial uploaded turn")
			h.durable(h.cell.ID, h.cell)
			before := takes.Load()
			_, err := h.continuerB().Take(context.Background(), h.cell.ID)
			if old {
				if !errors.Is(err, directory.ErrConditionalTake) {
					t.Fatalf("old service take = %v", err)
				}
				if takes.Load() != before {
					t.Fatal("a take reached the old service")
				}
				if _, err := h.b.Dir.List(context.Background()); err != nil {
					t.Fatalf("old service listing stopped working: %v", err)
				}
			} else if err != nil || takes.Load() != before+1 {
				t.Fatalf("current Go service take = %v, requests %d", err, takes.Load()-before)
			}
		})
	}
}
