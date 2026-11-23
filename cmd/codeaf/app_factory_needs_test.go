package main

import (
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A runner reads what it lacks once, when it attaches. The provider key the
// app hands over after launch must attach it again, and only that change may:
// a runner that lacked nothing, or still lacks it, keeps running its work.
func TestARunnerAttachesAgainOnlyWhenItGainsWhatItLacked(t *testing.T) {
	stored := func(needs string) *atomic.Value {
		v := &atomic.Value{}
		v.Store(needs)
		return v
	}
	asked := 0
	now := func(needs string) func() string {
		return func() string { asked++; return needs }
	}
	for _, c := range []struct {
		name     string
		attached *atomic.Value
		now      string
		want     bool
	}{
		{"lacked a provider, now has one", stored(api.RunnerNeedsProvider), "", true},
		{"still lacks a provider", stored(api.RunnerNeedsProvider), api.RunnerNeedsProvider, false},
		{"lacked nothing", stored(""), "", false},
		{"lacked nothing, now lacks a provider", stored(""), api.RunnerNeedsProvider, false},
		{"has not attached yet", &atomic.Value{}, "", false},
		{"no record", nil, "", false},
	} {
		if got := runnerGainedWhatItLacked(c.attached, now(c.now)); got != c.want {
			t.Errorf("%s: attach again = %v, want %v", c.name, got, c.want)
		}
	}
	if asked != 2 {
		t.Fatalf("the home's provider was read %d times; only runners that lacked one should ask", asked)
	}
}
