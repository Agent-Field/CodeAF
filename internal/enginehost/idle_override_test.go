package enginehost

import (
	"testing"
	"time"
)

// The contract, in the words of the operator who starts hosted chats:
//
//	C7  an engine started with an idle window keeps a quiet chat for exactly
//	    that long, waits no longer than that before an empty host leaves, and
//	    looks for idle chats often enough to honour it; an engine started with
//	    none, or with a window too short to be meant, behaves as it always did.

// pristineIdlePolicy puts the three policy values back to the product's own
// numbers for the length of one test and restores whatever was there.
func pristineIdlePolicy(t *testing.T) {
	t.Helper()
	wasSession, wasHost, wasSweep := sessionIdle, hostIdle, sweepEvery.Load()
	t.Cleanup(func() {
		sessionIdle, hostIdle = wasSession, wasHost
		sweepEvery.Store(wasSweep)
	})
	sessionIdle, hostIdle = 30*time.Minute, 2*time.Minute
	sweepEvery.Store(0)
}

func TestC7_EngineIdleOverrideShrinksTheWindows(t *testing.T) {
	t.Run("none given leaves the usual windows", func(t *testing.T) {
		pristineIdlePolicy(t)
		if sessionIdle != 30*time.Minute || hostIdle != 2*time.Minute || sweepSpan() != 30*time.Second {
			t.Fatalf("defaults moved: chat %v, host %v, sweep %v; want 30m, 2m, 30s", sessionIdle, hostIdle, sweepSpan())
		}
	})
	t.Run("eight seconds shrinks all three together", func(t *testing.T) {
		pristineIdlePolicy(t)
		if !ApplyIdleOverride(8 * time.Second) {
			t.Fatal("eight seconds was refused")
		}
		if sessionIdle != 8*time.Second || SessionIdleAfter() != 8*time.Second {
			t.Fatalf("a quiet chat is kept %v, want 8s", sessionIdle)
		}
		if hostIdle != 8*time.Second {
			t.Fatalf("an empty host waits %v before leaving, want 8s (the smaller of 2m and the window)", hostIdle)
		}
		if sweepSpan() != 2*time.Second {
			t.Fatalf("the sweep looks every %v, want 2s (a quarter of the window)", sweepSpan())
		}
	})
	t.Run("a long window never makes the host wait or look more slowly", func(t *testing.T) {
		pristineIdlePolicy(t)
		if !ApplyIdleOverride(3 * time.Hour) {
			t.Fatal("three hours was refused")
		}
		if sessionIdle != 3*time.Hour {
			t.Fatalf("a quiet chat is kept %v, want 3h", sessionIdle)
		}
		if hostIdle != 2*time.Minute || sweepSpan() != 30*time.Second {
			t.Fatalf("host wait %v and sweep %v grew past their usual 2m and 30s", hostIdle, sweepSpan())
		}
	})
	t.Run("below the floor is ignored", func(t *testing.T) {
		for _, d := range []time.Duration{4 * time.Second, time.Millisecond, 0, -time.Hour} {
			pristineIdlePolicy(t)
			if ApplyIdleOverride(d) {
				t.Fatalf("%v was applied, but it is below the floor of %v", d, MinIdleOverride)
			}
			if sessionIdle != 30*time.Minute || hostIdle != 2*time.Minute || sweepSpan() != 30*time.Second {
				t.Fatalf("%v changed the policy anyway: chat %v, host %v, sweep %v", d, sessionIdle, hostIdle, sweepSpan())
			}
		}
	})
	t.Run("the floor itself is accepted and never sweeps faster than twice a second", func(t *testing.T) {
		pristineIdlePolicy(t)
		if !ApplyIdleOverride(MinIdleOverride) {
			t.Fatal("the floor itself was refused")
		}
		if sweepSpan() < 500*time.Millisecond {
			t.Fatalf("the sweep looks every %v, faster than twice a second", sweepSpan())
		}
	})
}
