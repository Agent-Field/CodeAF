package main

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestIdleRelease_R6_ReleasedWindowOfflineSendSaysWhy(t *testing.T) {
	var down atomic.Bool
	f := newIdleAppContract(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if down.Load() {
				http.Error(w, "fixture offline", http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	released := f.released(t)
	var calls atomic.Int32
	original := f.model.server.Config.Handler
	f.model.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			calls.Add(1)
		}
		original.ServeHTTP(w, r)
	})
	path := appTranscriptOf(f.b, f.agent.ID)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	down.Store(true)
	t.Cleanup(func() { down.Store(false) })
	const words = "answer these kept words when sync returns"
	_, err = f.b.Send(f.agent.ID, words, "")
	var wire *appwire.Error
	const want = "the sync service is not answering, so this chat cannot be taken back yet; your words are still in the box"
	if !errors.As(err, &wire) || wire.Code != appwire.CodeUnreachable || wire.Message != want {
		t.Fatalf("released offline send = %v; want one unreachable refusal: %q", err, want)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || calls.Load() != 0 {
		t.Fatalf("offline send wrote or called the model: journal unchanged=%v model calls=%d err=%v", bytes.Equal(before, after), calls.Load(), err)
	}
	down.Store(false)
	turn, err := f.b.Send(f.agent.ID, words, "")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, f.rec, turn)
	view, err := f.own.Dir.Cell(t.Context(), f.agent.ID)
	if err != nil || view.Cell.Lease.Device != f.own.Device.ID() || view.Cell.Lease.Fence <= released.Cell.Lease.Fence || calls.Load() == 0 {
		t.Fatalf("retry did not take a new fence and answer: %+v calls=%d err=%v", view, calls.Load(), err)
	}
}
