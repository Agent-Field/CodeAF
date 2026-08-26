package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// THE STORM LAWS, COUNTED.
//
// PERF.md's doctrine is that a gate counts work and never wall time, and these
// are written that way: the ceiling on a sweep is stated as ANSWERS — how many
// of six hundred motions the router was made to handle — and the promise about
// a key is stated as "before any command of the sweep's had to run", which is a
// fact about the order of the calls rather than about how long they took. Both
// hold on a loaded CI box and both mean the same thing on a machine three years
// faster, which a stopwatch would not.
//
// The numbers here are the ones PERF.md's "the storm laws" section names. A
// change to either changes that file in the same commit.

// stormApp is a settled conversation with rows a pointer can answer to, at a
// size a sweep has somewhere to sweep across.
func stormApp(t *testing.T) *app {
	t.Helper()
	a := hoverApp(t)
	a.frame()
	return a
}

// SIX HUNDRED MOTIONS FOLLOWED BY ONE KEY: THE KEY IS HANDLED WITHIN ONE FRAME
// OF ARRIVING, and the six hundred cost ONE answer between them.
//
// This is the law the whole of coalesce.go exists for. Over a link a single
// motion costs about twenty milliseconds of work, so six hundred answered in
// full is twelve seconds during which a typed character sits in the terminal's
// pipe behind them — which is exactly what a person sweeping a pointer over a
// long conversation and then typing used to get.
//
// It is counted rather than timed. `answered` is how many messages the router
// was actually made to handle; `folded` is how many were dropped as already
// false. And the key is proved not to have waited by the strongest statement
// there is: its character is in the draft the moment its own Update returns,
// with the sweep's own wakeup still unrun.
func TestSixHundredMotionsThenAKeyCostOneSweepAndTheKey(t *testing.T) {
	a := stormApp(t)
	top, bottom := sweepBetween(t, a)
	a.ptr = pointerFold{}

	var wakeups int
	for i := 0; i < 600; i++ {
		y := top + i%max(1, bottom-top)
		_, cmd := a.Update(tea.MouseMotionMsg{X: 4 + i%40, Y: y})
		if cmd != nil {
			wakeups++
		}
	}
	if a.ptr.answered != 1 {
		t.Fatalf("a sweep of six hundred motions was answered %d times, want the one it arrived with", a.ptr.answered)
	}
	if a.ptr.folded != 598 {
		t.Fatalf("a sweep of six hundred motions folded %d of them away, want 598", a.ptr.folded)
	}
	if wakeups != 1 {
		t.Fatalf("a sweep of six hundred motions asked for %d wakeups, want one", wakeups)
	}

	// And the key, arriving behind all six hundred of them.
	before := len(a.input.value)
	_, cmd := a.Update(key("z"))
	if got := string(a.input.value); !strings.HasSuffix(got, "z") || len(a.input.value) != before+1 {
		t.Fatalf("the key behind six hundred motions put %q in the draft", got)
	}
	if a.ptr.answered != 1 {
		t.Fatalf("the key made the surface answer %d motions on its way through", a.ptr.answered)
	}
	_ = cmd

	// The sweep is still owed, and the frame it was promised pays it: ONE answer,
	// at the position the pointer actually ended up at.
	drive(t, a, pointerMsg{})
	if a.ptr.have {
		t.Fatal("the fold is still holding a position a frame after it settled")
	}
	if a.ptr.answered != 2 {
		t.Fatalf("six hundred motions and a key cost %d answers in all, want two", a.ptr.answered)
	}
}

// A SWEEP IS ANSWERED AT THE PLACE IT ENDED AND NOWHERE ELSE. Folding is only
// honest if the answer is the newest one: a hover left on a row the pointer
// crossed three hundred cells ago would be the fold lying about where the
// pointer is. The sweep runs between the conversation's two tool rows, so the
// row it ends on is one that answers to a pointer and the claim is not vacuous.
func TestASweepIsAnsweredWhereItEnded(t *testing.T) {
	a := stormApp(t)
	first, last := sweepBetween(t, a)
	a.ptr = pointerFold{}

	for y := first; y <= last; y++ {
		a.Update(tea.MouseMotionMsg{X: 4, Y: y})
	}
	drive(t, a, pointerMsg{})

	want := a.hoverTarget(4, last)
	if want.kind != hoverEntry {
		t.Fatalf("the row a sweep was aimed to end on answers to nothing: %v", want)
	}
	if a.hot != want {
		t.Fatalf("a sweep that ended on row %d left the pointer recorded at %v, want %v", last, a.hot, want)
	}
}

// sweepBetween is the first and last screen row of the conversation's two tool
// calls: two rows a pointer means something different on.
func sweepBetween(t *testing.T, a *app) (int, int) {
	t.Helper()
	body, _ := a.window(a.bodyWidth(), a.viewHeight())
	first, last := -1, -1
	for i, r := range body {
		if r.hit != hitTool {
			continue
		}
		if first < 0 {
			first = a.bodyTop() + i
		}
		last = a.bodyTop() + i
	}
	if first < 0 || last <= first {
		t.Fatalf("the conversation drew tool rows at %d..%d, want two to sweep between", first, last)
	}
	return first, last
}

// A POINTER ARRIVING SOMEWHERE IS ANSWERED ON THE SPOT. The fold is for storms;
// a person moving onto a row and stopping must see the row light with no clock
// in between, which is what "the first motion is never folded" buys.
func TestAPointerArrivingIsAnsweredOnTheSpot(t *testing.T) {
	a := stormApp(t)
	top, _ := sweepBetween(t, a)
	a.ptr = pointerFold{}

	_, cmd := a.Update(tea.MouseMotionMsg{X: 4, Y: top})
	if a.ptr.answered != 1 || a.ptr.have {
		t.Fatalf("a pointer arriving was answered %d times and left %v folded", a.ptr.answered, a.ptr.have)
	}
	if a.hot != a.hoverTarget(4, top) {
		t.Fatalf("a pointer arriving on row %d recorded %v", top, a.hot)
	}
	if cmd != nil {
		t.Fatal("a pointer arriving asked for a clock it does not need")
	}
}

// KEYS ARE NEVER FOLDED AND NEVER REORDERED — not with respect to each other,
// and not with respect to the motions around them. A hundred characters typed
// through a storm spell the word they were typed in, in order, with none
// dropped and none swapped.
func TestKeysAreNeverFoldedNorReordered(t *testing.T) {
	a := stormApp(t)
	top, bottom := sweepBetween(t, a)
	a.ptr = pointerFold{}

	var want strings.Builder
	for i := 0; i < 100; i++ {
		// Six motions between every two characters, which is what a hand resting
		// on a trackpad while it types actually sends.
		for j := 0; j < 6; j++ {
			a.Update(tea.MouseMotionMsg{X: 4 + j, Y: top + (i+j)%max(1, bottom-top)})
		}
		letter := string(rune('a' + i%26))
		want.WriteString(letter)
		a.Update(key(letter))
	}
	if got := string(a.input.value); got != want.String() {
		t.Fatalf("a hundred characters typed through a storm spelled %q, want %q", got, want.String())
	}
}

// A KEY NEVER WAITS BEHIND A SWEEP, said as the count it is: every one of a
// hundred keys is handled without the router being made to answer a single
// motion on the way. The fold holds a position, not a queue, so there is
// nothing in front of a keystroke to spend.
func TestAKeyNeverWaitsBehindASweep(t *testing.T) {
	a := stormApp(t)
	top, bottom := sweepBetween(t, a)
	a.ptr = pointerFold{}

	// The storm, and then the keys, with no frame allowed in between.
	for i := 0; i < 600; i++ {
		a.Update(tea.MouseMotionMsg{X: 4, Y: top + i%max(1, bottom-top)})
	}
	answered := a.ptr.answered
	for i := 0; i < 100; i++ {
		a.Update(key("x"))
		if a.ptr.answered != answered {
			t.Fatalf("key %d was made to answer %d motions on its way through", i, a.ptr.answered-answered)
		}
		if got := len(a.input.value); got != i+1 {
			t.Fatalf("after %d keys the draft holds %d characters", i+1, got)
		}
	}
}

// A WHEEL RUN SCROLLS EXACTLY AS FAR AS ITS NOTCHES ASKED. Folding a scroll is
// only allowed if it is a distance kept whole: twelve notches folded must land
// on the same row as twelve notches answered one at a time, or the fold has
// quietly eaten somebody's scroll.
func TestAFoldedWheelRunScrollsExactlyAsFarAsAnUnfoldedOne(t *testing.T) {
	const notches = 12
	one := scrollApp()
	for i := 0; i < notches; i++ {
		// Answered singly: each notch is a gesture arriving, because the message
		// between them ends the run.
		one.Update(tea.MouseWheelMsg{X: 4, Y: one.bodyTop() + 1, Button: tea.MouseWheelUp})
		one.Update(frameMsg{})
	}
	if one.offset == 0 {
		t.Fatal("twelve notches up moved a transcript that was too short to scroll")
	}

	all := scrollApp()
	for i := 0; i < notches; i++ {
		all.Update(tea.MouseWheelMsg{X: 4, Y: all.bodyTop() + 1, Button: tea.MouseWheelUp})
	}
	if all.ptr.answered != 1 {
		t.Fatalf("a run of %d notches was answered %d times before its frame, want the one it arrived with", notches, all.ptr.answered)
	}
	drive(t, all, pointerMsg{})
	if all.ptr.answered != notches {
		t.Fatalf("a run of %d notches was answered %d times in all", notches, all.ptr.answered)
	}
	if one.offset != all.offset {
		t.Fatalf("%d notches folded landed at offset %d, %d notches answered singly landed at %d", notches, all.offset, notches, one.offset)
	}
}

// A WHEEL TURNED THE OTHER WAY IS A DIFFERENT GESTURE, and the run it
// interrupted is spent before it — in the order the two arrived, which is the
// only order that can land where the person aimed.
func TestAWheelChangingDirectionSpendsWhatItInterrupted(t *testing.T) {
	a := scrollApp()
	y := a.bodyTop() + 1
	for i := 0; i < 5; i++ {
		a.Update(tea.MouseWheelMsg{X: 4, Y: y, Button: tea.MouseWheelUp})
	}
	if a.ptr.notches != 4 {
		t.Fatalf("five notches up left %d owed, want four", a.ptr.notches)
	}

	a.Update(tea.MouseWheelMsg{X: 4, Y: y, Button: tea.MouseWheelDown})
	if a.ptr.notches != 0 {
		t.Fatalf("a turn the other way left %d notches of the last run owed", a.ptr.notches)
	}
	// Five up and one down, all of them spent: four notches' distance from the
	// bottom the transcript was pinned to.
	up := a.offset
	back := scrollApp()
	for i := 0; i < 4; i++ {
		back.Update(tea.MouseWheelMsg{X: 4, Y: y, Button: tea.MouseWheelUp})
		back.Update(frameMsg{})
	}
	if up != back.offset {
		t.Fatalf("five notches up then one down landed at offset %d, want the %d four notches up reach", up, back.offset)
	}
}

// scrollApp is a conversation with more rows in it than the window has, which
// is the only kind a scroll can be measured on.
func scrollApp() *app {
	a := benchApp(20)
	a.frame()
	return a
}

// A PRESS IS ANSWERED WHERE THE SWEEP ENDED. A drag begins from what is under
// the pointer, so a press that arrived behind a fold must find the fold already
// spent — otherwise the gesture starts from the row before it.
func TestAPressSpendsTheSweepInFrontOfIt(t *testing.T) {
	a := stormApp(t)
	top, bottom := sweepBetween(t, a)
	a.ptr = pointerFold{}

	for y := top; y <= bottom; y++ {
		a.Update(tea.MouseMotionMsg{X: 4, Y: y})
	}
	if !a.ptr.have {
		t.Fatal("a sweep across the body folded nothing")
	}
	a.Update(tea.MouseClickMsg{X: 4, Y: bottom, Button: tea.MouseLeft})
	if a.ptr.have {
		t.Fatal("a press was answered with a sweep still folded in front of it")
	}
	if a.hot != a.hoverTarget(4, bottom) {
		t.Fatalf("the press landed with the pointer recorded at %v, want the row the sweep ended on", a.hot)
	}
}

// A FOLDED MESSAGE BUILDS NO FRAME AT ALL. Bubble Tea asks the model for a
// frame after every message it delivers and writes one to the terminal sixty
// times a second, so nearly every frame BUILT during a storm is a frame nobody
// is shown. A motion that went into the fold and stopped there changed nothing
// [app.View] reads, so it declares the frame before it — and the difference is
// stated the way PERF.md states the rest of them, as allocations: a frame is
// tens of thousands of bytes and a folded motion is none of them.
func TestAFoldedMessageBuildsNoFrame(t *testing.T) {
	a := stormApp(t)
	top, bottom := sweepBetween(t, a)
	a.ptr = pointerFold{}
	a.Update(tea.MouseMotionMsg{X: 4, Y: top})
	a.View()

	var step int
	folded := testing.AllocsPerRun(600, func() {
		step++
		a.Update(tea.MouseMotionMsg{X: 4, Y: top + step%max(1, bottom-top)})
		a.View()
	})
	if folded > 4 {
		t.Fatalf("a folded motion and the frame after it allocate %.0f times, want the handful a stored position costs", folded)
	}

	// The frame it did NOT build, for scale: the one the fold's own clock pays
	// for when the sweep is finally answered.
	whole := testing.AllocsPerRun(20, func() {
		a.dirty = true
		a.frame()
	})
	if whole < 10*folded {
		t.Fatalf("a folded motion costs %.0f allocations against a frame's %.0f, which is not a fold", folded, whole)
	}

	drive(t, a, pointerMsg{})
	if !a.drawn {
		t.Fatal("the surface has no frame after a sweep settled")
	}
}
