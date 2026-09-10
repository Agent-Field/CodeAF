package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE LINK-AWARE FRAME CLOCK ──────────────────────────────────────────────
//
// Every test here states the same bargain from a different side: over a
// connection this surface BUILDS less and SHOWS the same thing. The cadence
// changes; the wall-clock behaviour of everything counted on it does not.

func TestTheFrameClockReadsTheLinkFromTheEnvironmentAlone(t *testing.T) {
	for _, probe := range []struct {
		name string
		env  map[string]string
		want bool
	}{
		{name: "a terminal on this machine", env: map[string]string{"TERM": "xterm-256color"}},
		{name: "the connection's own four figures", env: map[string]string{
			"SSH_CONNECTION": "10.0.0.2 51234 10.0.0.9 22"}, want: true},
		{name: "the terminal it was given", env: map[string]string{"SSH_TTY": "/dev/pts/3"}, want: true},
		// A word that is present and empty is a word nobody set: an exported
		// variable with nothing in it is not a connection.
		{name: "an empty word", env: map[string]string{"SSH_CONNECTION": "  "}},
	} {
		t.Run(probe.name, func(t *testing.T) {
			got := remoteLink(func(key string) string { return probe.env[key] })
			if got != probe.want {
				t.Fatalf("the link reads remote=%v, want %v, from %v", got, probe.want, probe.env)
			}
		})
	}
	// No environment at all is a surface that assumes it is local, which is the
	// assumption that costs nothing when it is wrong.
	if remoteLink(nil) {
		t.Fatal("a surface with no environment called itself remote")
	}
}

func TestTheCadenceIsTheLocalOneUntilThereIsALink(t *testing.T) {
	a := newTestApp(&fakeAgent{})

	if got := a.frameEvery(); got != frameInterval {
		t.Fatalf("a local surface asks for a frame every %s, want %s", got, frameInterval)
	}
	if got := a.frameStride(); got != 1 {
		t.Fatalf("a local frame covers %d slots, want one", got)
	}

	a.remote = true
	if got := a.frameEvery(); got != remoteFrameInterval {
		t.Fatalf("a remote surface asks for a frame every %s, want %s", got, remoteFrameInterval)
	}
	if got := a.frameStride(); got != 3 {
		t.Fatalf("a remote frame covers %d slots, want three", got)
	}
	// THE STRIDE IS THE CADENCE, and the pair has to stay one fact: an animation
	// stepping by a different number of slots than the clock waited for is an
	// animation running at the wrong speed.
	if want := time.Duration(a.frameStride()) * frameInterval; a.frameEvery() != want {
		t.Fatalf("the clock waits %s and the animation steps %s", a.frameEvery(), want)
	}
	// And the frame is under a tenth of a second either way: below that a
	// spinner stops reading as rotation.
	if a.frameEvery() > 100*time.Millisecond {
		t.Fatalf("the remote frame is %s, which is slower than motion survives", a.frameEvery())
	}
}

// A SLOWER CLOCK IS NOT A SLOWER SURFACE. Everything on this screen with a
// deadline is measured against the wall clock, so the only thing fewer frames
// can cost a countdown is the fraction of a frame between the deadline and the
// next tick — and at the deadline it pauses and waits, never answers.
func TestACountdownPausesOnWallTimeAtTheSlowCadence(t *testing.T) {
	at := time.Now()
	agent, a := wired([]session.Event{
		toolBegin("bash", "bash rm -rf build"),
		consentEvent(9, "bash", "bash rm -rf build", `bash pattern "rm -rf *"`),
	})
	a.remote = true
	// Two seconds rather than the setting's ten: this test delivers every frame
	// of the wait by hand, and the fact it is about — a deadline in wall time —
	// is the same fact at twenty frames as at a hundred.
	a.clock, a.askWait = func() time.Time { return at }, 2*time.Second
	typeLine(t, a, "clean it")
	if !a.asking() {
		t.Fatal("the question is not up")
	}

	// The frames are delivered at the cadence the link earns, and the clock
	// moves with them — which is what a real ten seconds looks like from here.
	deadline := at.Add(a.askWait)
	for frames := 0; !askHeld(a) && frames < 200; frames++ {
		at = at.Add(a.frameEvery())
		drive(t, a, frameMsg{})
	}
	if len(agent.answers) != 0 {
		t.Fatalf("the countdown answered %+v at the slow cadence, want a pause", agent.answers)
	}
	if !askHeld(a) {
		t.Fatal("the countdown never paused at the slow cadence")
	}
	if !a.asking() {
		t.Fatal("the question went away at the deadline instead of waiting")
	}
	if late := at.Sub(deadline); late > a.frameEvery() {
		t.Fatalf("the pause landed %s past the deadline, want inside one frame", late)
	}
	if !strings.Contains(plain(frame(a)), "paused") {
		t.Fatalf("the paused row is not annotated:\n%s", plain(frame(a)))
	}
}

// THE ONCE-A-THIRD-OF-A-SECOND WORK IS STILL DONE THREE TIMES A SECOND. The
// usage ask is gated on slots crossed rather than on frames drawn, so the lock
// is taken on the same wall-clock period on both links.
func TestTheUsageAskKeepsItsWallTimePeriodOverALink(t *testing.T) {
	const slots = 300 // ten seconds of the 33ms clock

	count := func(remote bool) int {
		a := newTestApp(&fakeAgent{})
		a.remote = remote
		due := 0
		for a.paints < slots {
			a.paints += a.frameStride()
			if a.dueEvery(usageEvery) {
				due++
			}
		}
		return due
	}
	local, far := count(false), count(true)
	if want := slots / usageEvery; local != want {
		t.Fatalf("a local surface asked for usage %d times in %d slots, want %d", local, slots, want)
	}
	if far != local {
		t.Fatalf("a remote surface asked %d times and a local one %d — the period moved", far, local)
	}
	// A period nobody named is a period nobody keeps: zero must not divide.
	a := newTestApp(&fakeAgent{})
	if a.dueEvery(0) {
		t.Fatal("a period of nothing came due")
	}
}

// THE ARRIVAL TAKES ITS SECOND AND A QUARTER EITHER WAY. The welcome box is
// counted in slots, so a link costs it frames and not time — a box that took
// three times as long to open over ssh would be a box that has to be waited out.
func TestTheWelcomeBoxArrivesInTheSameTimeOverALink(t *testing.T) {
	settle := func(remote bool) (frames int) {
		a := newTestApp(&fakeAgent{})
		a.remote = remote
		a.welcome = welcome{open: true, sel: -1}
		for a.welcome.animating() && frames < 500 {
			a.welcome.tick(a.frameStride())
			frames++
		}
		return frames
	}
	local, far := settle(false), settle(true)
	if local != welcomeFrames {
		t.Fatalf("the box locally took %d frames, want %d", local, welcomeFrames)
	}
	if want := (welcomeFrames + 2) / 3; far != want {
		t.Fatalf("the box over a link took %d frames, want %d — the same wall time", far, want)
	}
	// It still STOPS, and it stops exactly where the still frame is: a stride
	// that overshot would leave the animation past its last frame.
	a := newTestApp(&fakeAgent{})
	a.welcome = welcome{open: true, sel: -1, step: welcomeFrames - 1}
	a.welcome.tick(3)
	if a.welcome.step != welcomeFrames {
		t.Fatalf("the box settled at step %d, want %d", a.welcome.step, welcomeFrames)
	}
}

// ── THE STEADY BURN FIGURE ──────────────────────────────────────────────────

// A rate that moves every frame is a status line rewritten every frame, to show
// a person digits their eye never resolved. The figure is held for half a second
// and rounded to a step worth reading — and the accounting behind it is
// untouched, which is what the last assertion here is about.
func TestTheBurnFigureStandsStillLongEnoughToBeRead(t *testing.T) {
	a, _, now := hudApp(t)
	a.state = stateWorking
	a.turnBegan, a.turnOutStart = *now, 0
	*now = now.Add(10 * time.Second)

	a.outputTokens = 600 // 60 tok/s avg
	first := a.burnSegment()
	if first != "60 tok/s avg" {
		t.Fatalf("the burn opened at %q, want 60 tok/s avg", first)
	}

	// Inside the hold the string does not move, however the tokens arrive.
	*now = now.Add(200 * time.Millisecond)
	a.outputTokens = 900
	if got := a.burnSegment(); got != first {
		t.Fatalf("the burn moved to %q inside the hold, want %q", got, first)
	}
	*now = now.Add(200 * time.Millisecond)
	a.outputTokens = 1_200
	if got := a.burnSegment(); got != first {
		t.Fatalf("the burn moved to %q inside the hold, want %q", got, first)
	}

	// Past it, the person sees the rate the turn is actually running at.
	*now = now.Add(200 * time.Millisecond)
	if got := a.burnSegment(); got == first {
		t.Fatalf("the burn is still reading %q after the hold ran out", got)
	}

	// AND THE METERS NEVER SAW ANY OF THIS: what was damped is the string.
	if a.outputTokens != 1_200 || a.turnOutStart != 0 {
		t.Fatalf("the hold reached the accounting: %d written from %d", a.outputTokens, a.turnOutStart)
	}

	// A turn that ends takes its rate with it, and nothing is held into the next
	// one.
	a.state = stateIdle
	if got := a.burnSegment(); got != "" {
		t.Fatalf("an idle surface is still burning at %q", got)
	}
	if a.burnShown != "" || !a.burnAt.IsZero() {
		t.Fatalf("the hold outlived the turn: %q at %s", a.burnShown, a.burnAt)
	}
}

func TestTheBurnRateIsRoundedToAStepAPersonReads(t *testing.T) {
	for _, probe := range []struct {
		rate, want int
	}{
		{rate: 0, want: 0},
		{rate: 3, want: 5},
		{rate: 62, want: 60},
		{rate: 64, want: 65},
		{rate: 99, want: 100},
		{rate: 137, want: 140},
		{rate: 999, want: 1000},
		{rate: 1_234, want: 1_200},
		{rate: 12_345, want: 12_000},
	} {
		if got := burnStep(probe.rate); got != probe.want {
			t.Fatalf("%d tok/s rounds to %d, want %d", probe.rate, got, probe.want)
		}
	}
}
