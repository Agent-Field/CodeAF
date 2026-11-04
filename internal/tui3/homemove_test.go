package tui3

import (
	"fmt"
	"strings"
	"testing"
)

// The continue's line says what it is doing and how much has come through.
func TestMoveSaid(t *testing.T) {
	for _, c := range []struct {
		p    MoveProgress
		want string
	}{
		{MoveProgress{Step: "preparing"}, continuing},
		{MoveProgress{Step: "copying", Done: 412 << 20, Total: 1433 << 20}, "copying to this computer: 412 of 1,433 MB…"},
		{MoveProgress{Step: "copying", Done: 3 << 20}, "copying to this computer: 3 MB so far…"},
		{MoveProgress{Step: "copying", Done: 900 << 10, Total: 1000 << 10}, "copying to this computer: 900 of 1,000 KB…"},
		{MoveProgress{Step: "opening", Done: 5}, "opening it here…"},
	} {
		if got := moveSaid(c.p); got != c.want {
			t.Errorf("moveSaid(%+v) = %q, want %q", c.p, got, c.want)
		}
	}
}

// movingTaker is a fakeTaker that says how far its continue has got.
type movingTaker struct {
	fakeTaker
	at MoveProgress
}

func (m *movingTaker) MoveProgress(string) (MoveProgress, bool) { return m.at, true }

// Home's line follows a running continue, and only while home still shows it.
func TestHomeFollowsAMove(t *testing.T) {
	taker := &movingTaker{at: MoveProgress{Step: "copying", Done: 412 << 20, Total: 1433 << 20}}
	a := continueHome(t, taker, continueRows, 200)
	a.homeTaking, a.homeTakeSeq = "c-off", 7
	a.home.say(continuing, "")
	if next := a.followMove(homeMoveTickMsg{seq: 7}); next == nil {
		t.Fatal("a running continue stopped being followed")
	}
	if a.home.msg != "copying to this computer: 412 of 1,433 MB…" {
		t.Fatalf("home says %q", a.home.msg)
	}
	a.home.say("something else", "")
	_ = a.followMove(homeMoveTickMsg{seq: 7})
	if a.home.msg != "something else" {
		t.Fatalf("progress wrote over another line: %q", a.home.msg)
	}
	if next := a.followMove(homeMoveTickMsg{seq: 6}); next != nil {
		t.Fatal("an older continue's tick kept running")
	}
}

// A continue that stalled says so, and where the chat still is.
func TestAStalledContinueSaysWhy(t *testing.T) {
	a := continueHome(t, &fakeTaker{}, continueRows, 200)
	got := a.continueFailure(homeTakenMsg{row: continueRows[0], err: fmt.Errorf("%w: handoff: fetch", ErrMoveStalled)})
	if !strings.Contains(got, "stopped: nothing came through for 2 minutes; the chat is still on host-a") {
		t.Fatalf("a stalled continue says %q", got)
	}
}
