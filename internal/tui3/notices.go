package tui3

import (
	"sync"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// ── THE NOTICE SEAM: A SENTENCE FROM OUTSIDE THE LOOP, SHOWN AT ONCE ────────
//
// Some things happen to a conversation while nobody is typing: another machine
// takes the chat over, a computer's clock is found to be wrong. The sentence
// for them is written on a goroutine of the sync side, and the person has to
// read it now, not when the next tool call is refused.
//
// The seam is one small desk and the doorbell (doorbell.go). A goroutine puts
// its sentence on the desk with [Notices.Say] and rings; the loop takes what is
// there and says each sentence as a note. Say never waits for the loop, for the
// reason the doorbell's own file gives, and a sentence said before any surface
// is listening waits on the desk for the first one.
//
// ONE SEAM FOR EVERY SUCH SENTENCE. The drive side and the take side both speak
// through it, so a new source of news is a new caller and not a new road.

// Notices is the desk. The zero value is not usable; make one with
// [NewNotices]. It is safe from any goroutine.
type Notices struct {
	mu          sync.Mutex
	lines       []outsideNotice
	said        map[string]bool // sentences SayOnce has put on the desk
	bell        *doorbell
	restoring   bool
	finalReport func([]string)
}

// outsideNotice keeps urgent account news visible on home as well as in chat.
type outsideNotice struct {
	text  string
	home  bool
	final bool
}

// saving keeps final sentences on the desk until the loop displays them or
// quitting hands them to the restored-terminal report. Both roads share this owner.
func (n *Notices) saving(lines []string) {
	if n == nil || len(lines) == 0 {
		return
	}
	report, bell, restoring := func() (func([]string), *doorbell, bool) {
		n.mu.Lock()
		defer n.mu.Unlock()
		if !n.restoring {
			for _, line := range lines {
				n.lines = append(n.lines, outsideNotice{text: line, final: true})
			}
		}
		return n.finalReport, n.bell, n.restoring
	}()
	if restoring {
		if report != nil {
			report(lines)
		}
	} else {
		bell.ring()
	}
}

// finishSaving transfers only sentences the loop has not shown, and redirects
// later completions before any close work leaves the update loop.
func (n *Notices) finishSaving(report func([]string)) {
	if n == nil {
		return
	}
	lines := func() []string {
		n.mu.Lock()
		defer n.mu.Unlock()
		n.restoring, n.finalReport = true, report
		var lines []string
		kept := n.lines[:0]
		for _, line := range n.lines {
			if line.final {
				lines = append(lines, line.text)
			} else {
				kept = append(kept, line)
			}
		}
		n.lines = kept
		return lines
	}()
	if len(lines) > 0 && report != nil {
		report(lines)
	}
}

// NewNotices makes an empty desk that no surface listens to yet.
func NewNotices() *Notices { return &Notices{} }

// Say puts a sentence on the desk and asks the loop to read it. An empty
// sentence says nothing.
func (n *Notices) Say(line string) {
	n.say(line, false)
}

func (n *Notices) say(line string, home bool) {
	if n == nil || line == "" {
		return
	}
	n.mu.Lock()
	n.lines = append(n.lines, outsideNotice{text: line, home: home})
	bell := n.bell
	n.mu.Unlock()
	bell.ring()
}

// SayOnce is Say for a sentence that is true for the rest of the run, such as
// "this computer was removed": however many roads find out, the person reads it
// once.
func (n *Notices) SayOnce(line string) {
	n.sayOnce(line, false)
}

// SayHomeOnce says lasting account news once, including on the home screen
// where a removed computer first opens without a conversation in front of it.
func (n *Notices) SayHomeOnce(line string) {
	n.sayOnce(line, true)
}

func (n *Notices) sayOnce(line string, home bool) {
	if n == nil {
		return
	}
	n.mu.Lock()
	again := n.said[line]
	if n.said == nil {
		n.said = map[string]bool{}
	}
	n.said[line] = true
	n.mu.Unlock()
	if !again {
		n.say(line, home)
	}
}

// attach makes bell the way the desk reaches a loop, and rings it at once when
// sentences were said before there was one.
func (n *Notices) attach(bell *doorbell) {
	if n == nil {
		return
	}
	n.mu.Lock()
	n.bell = bell
	waiting := len(n.lines) > 0
	n.mu.Unlock()
	if waiting {
		bell.ring()
	}
}

// take reads and clears the desk. Called on the loop only.
func (n *Notices) take() []outsideNotice {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	out := n.lines
	n.lines = nil
	return out
}

// noticesMsg is the message the door carries. Nothing is read out of it: the
// sentences are on the desk by the time it arrives.
type noticesMsg struct{}

// useNotices makes desk this surface's own; the loop is parked on its door by
// Init.
func (a *app) useNotices(desk *Notices) {
	a.outsideDesk = desk
	desk.attach(a.noticeBell)
}

// saidNotices says what the desk holds, each sentence as a note, and parks the
// loop on the door again.
func (a *app) saidNotices() {
	for _, line := range a.outsideDesk.take() {
		if chatlist.IsRemovedNotice(line.text) {
			a.removedComputer()
		}
		if line.home && !chatlist.IsRemovedNotice(line.text) {
			a.homeAccountNotice = firstLine(line.text)
		}
		before := a.echoHome
		a.echoHome = before || (line.home && !chatlist.IsRemovedNotice(line.text))
		a.note(line.text)
		a.echoHome = before
	}
	a.touch()
}
