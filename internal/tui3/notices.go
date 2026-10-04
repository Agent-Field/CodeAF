package tui3

import "sync"

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
	mu    sync.Mutex
	lines []string
	said  map[string]bool // sentences SayOnce has put on the desk
	bell  *doorbell
}

// NewNotices makes an empty desk that no surface listens to yet.
func NewNotices() *Notices { return &Notices{} }

// Say puts a sentence on the desk and asks the loop to read it. An empty
// sentence says nothing.
func (n *Notices) Say(line string) {
	if n == nil || line == "" {
		return
	}
	n.mu.Lock()
	n.lines = append(n.lines, line)
	bell := n.bell
	n.mu.Unlock()
	bell.ring()
}

// SayOnce is Say for a sentence that is true for the rest of the run, such as
// "this computer was removed": however many roads find out, the person reads it
// once.
func (n *Notices) SayOnce(line string) {
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
		n.Say(line)
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
func (n *Notices) take() []string {
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
		a.note(line)
	}
	a.touch()
}
