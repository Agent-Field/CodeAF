package control

import (
	"testing"
	"time"
)

// ── A TEN-MINUTE `write` IS WRITING, NOT THINKING ───────────────────────────
//
// THE MEASUREMENT THIS PINS, from 2026-09-10: a `write` call streamed for ten
// minutes at its lane's ordinary rate, and the read loop reported every fragment
// of it as HIDDEN — a tool call's arguments carry no answer text, and hidden was
// the only other word it had. So the controller never left the thinking phase,
// measured the ten minutes against how long this MODEL usually thinks, found the
// "thought" far into that distribution's tail, and raised a rescue that wrote
// eighteen thousand tokens nobody kept.
//
// The fix is in the read loop (internal/provider's client.go): a call's
// arguments are the answer arriving and are reported as VISIBLE. What this file
// holds is the arithmetic that makes that the right word — the same ten minutes,
// read as visible, is an ordinary stream at ordinary gaps and is never acted on.

// writing is an interactive plan for a model that thinks for twenty seconds at
// the median and writes a token every twenty milliseconds, with an alternative
// cheap enough to be worth racing to — the conditions under which a wrong phase
// buys a rescue.
func writing() Plan {
	p := plan()
	p.Think = logNormal(20, 0.7)
	return p
}

// tenMinutesOf streams one token every twenty milliseconds — the plan's own
// median gap — for ten minutes, each one read as `reading` says, and answers the
// first act raised, or None.
func tenMinutesOf(p Plan, reading func(time.Time) Reading) Act {
	watch := New(p)
	now := epoch.Add(time.Second)
	for end := now.Add(10 * time.Minute); now.Before(end); now = now.Add(20 * time.Millisecond) {
		if act := watch.Note(reading(now)); act.Kind != None {
			return act
		}
	}
	return Act{}
}

// TestATenMinuteToolCallAtOrdinaryGapsRaisesNoAct is law one's arithmetic: the
// arguments of a call read as visible progress, at the lane's own rate, are a
// healthy stream for as long as they last. Nothing is raised — no hedge, no
// report, no ceiling — and the phase is writing.
func TestATenMinuteToolCallAtOrdinaryGapsRaisesNoAct(t *testing.T) {
	p := writing()
	if act := tenMinutesOf(p, func(at time.Time) Reading { return Reading{At: at, Visible: 1} }); act.Kind != None {
		t.Fatalf("a ten-minute call at ordinary gaps raised %+v", act)
	}
	watch := New(p)
	watch.Note(Reading{At: at(1_000), Visible: 1})
	if phase := watch.Phase(); phase != PhaseWriting {
		t.Fatalf("phase = %v after one fragment of a call, want writing", phase)
	}
}

// TestTheSameCallReadAsThoughtIsJudgedALongThink is why the word matters, kept
// so the next person who wonders whether a call "is really" thought can see what
// that reading costs: the identical stream reported as hidden is a run of
// thought thirty times this model's median, and the duration clock acts on it.
func TestTheSameCallReadAsThoughtIsJudgedALongThink(t *testing.T) {
	act := tenMinutesOf(writing(), func(at time.Time) Reading { return Reading{At: at, Hidden: 1} })
	if act.Kind == None {
		t.Fatal("ten minutes of hidden deltas raised nothing — this test no longer shows the defect it documents")
	}
	if act.Reason != "long think" {
		t.Fatalf("reason = %q, want the duration clock's long think", act.Reason)
	}
}
