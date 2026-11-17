package tui3

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// MoveProgress is how far a continue has got: its step (preparing, copying,
// opening, arriving, failed, done) and the bytes copied here, of the total when
// the chat says it.
type MoveProgress struct {
	Step        string
	Done, Total int64
	Err         string
}

// moveReporter is a Taker that says how far the continue of a chat has got.
type moveReporter interface {
	MoveProgress(cell string) (MoveProgress, bool)
}

// moveRetrier is a Taker that can retry a failed background workspace fetch.
type moveRetrier interface {
	RetryMove(cell string) bool
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

// filesArrivingSaid is the status-line segment while workspace files arrive in
// the background after the chat has opened, or when they failed and can be
// retried (C2, C4). It never renders 0 of 0 (the emptiness law).
func filesArrivingSaid(p MoveProgress) string {
	switch {
	case p.Err != "":
		return "files couldn't arrive: " + p.Err + " · retry"
	case p.Step == "failed":
		return "files couldn't arrive · retry"
	case p.Step == "" || p.Step == "done":
		return ""
	case p.Done <= 0:
		return "files arriving"
	case p.Total > 0 && p.Done <= p.Total:
		by, unit := int64(1<<10), "KB"
		if max(p.Done, p.Total) >= 1<<20 {
			by, unit = 1<<20, "MB"
		}
		d, t := (p.Done+by/2)/by, (p.Total+by/2)/by
		if d <= 0 {
			return "files arriving"
		}
		if t <= 0 {
			t = 1
		}
		return fmt.Sprintf("files arriving · %s of %s %s", grouped(d), grouped(t), unit)
	default:
		by, unit := int64(1<<10), "KB"
		if p.Done >= 1<<20 {
			by, unit = 1<<20, "MB"
		}
		d := (p.Done + by/2) / by
		if d <= 0 {
			return "files arriving"
		}
		return fmt.Sprintf("files arriving · %s %s", grouped(d), unit)
	}
}

func (a *app) activeMoveCell() string {
	if a.file != "" {
		if dir := session.DirOf(a.file); dir != "" {
			return filepath.Base(dir)
		}
	}
	if a.homeTakeResult != nil && a.homeTakeResult.row.Cell != "" {
		return a.homeTakeResult.row.Cell
	}
	return a.homeTaking
}

func (a *app) filesSegment() string {
	if a.at(pageHome) {
		return ""
	}
	reporter, ok := a.taker.(moveReporter)
	if !ok {
		return ""
	}
	id := a.activeMoveCell()
	if id == "" {
		return ""
	}
	p, ok := reporter.MoveProgress(id)
	if !ok {
		return ""
	}
	return filesArrivingSaid(p)
}

func (a *app) filesMoveFailed() bool {
	reporter, ok := a.taker.(moveReporter)
	if !ok {
		return false
	}
	id := a.activeMoveCell()
	if id == "" {
		return false
	}
	p, ok := reporter.MoveProgress(id)
	return ok && (p.Err != "" || p.Step == "failed")
}

func (a *app) retryFilesMove() tea.Cmd {
	retrier, ok := a.taker.(moveRetrier)
	if !ok {
		return nil
	}
	id := a.activeMoveCell()
	if id == "" || !retrier.RetryMove(id) {
		return nil
	}
	a.touch()
	return moveTick(a.homeTakeSeq)
}

// followMove puts how far the running continue has got on home's line, while
// home still shows that continue's line, and refreshes the status line while
// workspace files arrive in the background of the open chat (C2).
func (a *app) followMove(msg homeMoveTickMsg) tea.Cmd {
	if msg.seq != a.homeTakeSeq {
		return nil
	}
	reporter, ok := a.taker.(moveReporter)
	if !ok {
		return nil
	}
	if a.homeTaking != "" {
		if p, ok := reporter.MoveProgress(a.homeTaking); ok && a.at(pageHome) && a.home.msg == a.homeMoveLine() {
			a.homeMoveSaid = moveSaid(p)
			a.home.say(a.homeMoveSaid, "")
			a.touch()
		}
		return moveTick(msg.seq)
	}
	id := a.activeMoveCell()
	if id == "" {
		return nil
	}
	p, ok := reporter.MoveProgress(id)
	a.touch()
	if !ok || p.Step == "" || p.Step == "done" || p.Err != "" || p.Step == "failed" {
		return nil
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
