package clientrun_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clientrun"
)

func TestRunnerBacksOffFailuresUsingItsClock(t *testing.T) {
	g := newRig(t)
	g.pair(t, "asks.answer")
	durations := make(chan time.Duration, 4)
	ticks := make(chan time.Time)
	r, err := clientrun.Open(clientrun.Config{Home: g.state, Relay: g.url, Signer: g.s, Directory: g.dir, Engine: g.e, Log: noopLog, Now: func() time.Time { return g.now }, HTTP: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })}, After: func(d time.Duration) <-chan time.Time { durations <- d; return ticks }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); r.Run(ctx) }()
	for _, want := range []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second} {
		select {
		case got := <-durations:
			if got != want {
				t.Fatalf("backoff = %v; want %v", got, want)
			}
		case <-ctx.Done():
			t.Fatal("runner never scheduled its next round")
		}
		if want != 40*time.Second {
			select {
			case ticks <- g.now:
			case <-ctx.Done():
				t.Fatal("runner did not wait on injected clock")
			}
		}
	}
	cancel()
	<-done
}
