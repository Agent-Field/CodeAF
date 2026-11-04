package tui3

import (
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

// MoveProgress is how far a continue has got: its step (preparing, copying,
// opening) and the bytes copied here, of the total when the chat says it.
type MoveProgress struct {
	Step        string
	Done, Total int64
}

// moveReporter is a Taker that says how far the continue of a chat has got.
type moveReporter interface {
	MoveProgress(cell string) (MoveProgress, bool)
}

// ErrMoveStalled is a continue that waited 2 minutes on the relay with nothing
// coming through. The Taker wraps its own stall in it.
var ErrMoveStalled = errors.New("nothing came through for 2 minutes")

// moveTickEvery is how often home's line follows a running continue.
var moveTickEvery = 500 * time.Millisecond

type homeMoveTickMsg struct{ seq uint64 }

func moveTick(seq uint64) tea.Cmd {
	return tea.Tick(moveTickEvery, func(time.Time) tea.Msg { return homeMoveTickMsg{seq: seq} })
}

// moveSaid is home's line for a continue that has got this far.
func moveSaid(p MoveProgress) string {
	switch {
	case p.Step == "opening":
		return "opening it here…"
	case p.Done <= 0:
		return continuing
	case p.Total > 0:
		return fmt.Sprintf("copying to this computer: %s…", movedBytes(p.Done, p.Total))
	}
	return fmt.Sprintf("copying to this computer: %s so far…", movedBytes(p.Done, 0))
}

// movedBytes reads "412 of 1,433 MB", in the unit the larger number reads best in.
func movedBytes(done, total int64) string {
	by, unit := int64(1<<10), "KB"
	if max(done, total) >= 1<<20 {
		by, unit = 1<<20, "MB"
	}
	if total > 0 {
		return fmt.Sprintf("%s of %s %s", grouped((done+by/2)/by), grouped((total+by/2)/by), unit)
	}
	return fmt.Sprintf("%s %s", grouped((done+by/2)/by), unit)
}

func grouped(n int64) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// followMove puts how far the running continue has got on home's line, while
// home still shows that continue's line.
func (a *app) followMove(msg homeMoveTickMsg) tea.Cmd {
	if msg.seq != a.homeTakeSeq || a.homeTaking == "" {
		return nil
	}
	reporter, ok := a.taker.(moveReporter)
	if !ok {
		return nil
	}
	if p, ok := reporter.MoveProgress(a.homeTaking); ok && a.at(pageHome) && a.home.msg == a.homeMoveLine() {
		a.homeMoveSaid = moveSaid(p)
		a.home.say(a.homeMoveSaid, "")
		a.touch()
	}
	return moveTick(msg.seq)
}

// homeMoveLine is the line home shows for the running continue.
func (a *app) homeMoveLine() string {
	if a.homeMoveSaid != "" {
		return a.homeMoveSaid
	}
	return continuing
}
