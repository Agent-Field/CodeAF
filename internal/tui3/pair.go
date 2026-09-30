package tui3

// ── /pair — SHARING YOUR CHATS WITH ANOTHER COMPUTER ────────────────────────
//
// TWO ERRANDS, ONE PANEL. `/pair` on the computer that has the chats shows a
// code and hands the chats over once a person there has said the three words
// match; `/pair <code>` on the other computer types that code and receives them.
// Both are the same conversation between two screens, so both are the same panel
// here and every sentence in it is the one internal/pair wrote, quoted and never
// retyped: a person learns pairing once, and the terminal says the same words.
//
// THE DOOR IS AN INTERFACE AND NIL IS ABSENT ([Pairing]), the way an update or a
// takeover is: the surface knows nothing of relays or identities. The work runs
// off the frame in a goroutine of its own. What the goroutine has to tell the
// screen (a code was shown, a wrong one was typed, a person is asked) travels
// as messages down one channel that a command re-arms after each; what the
// screen has to tell the goroutine (yes or no) travels back down a channel the
// question carries. Nothing here holds a lock and nothing draws off the loop.

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/pair"
)

// Pairing is the door onto pairing this computer with another. Offer shows a
// code and shares this computer's chats with the device that types it; Join
// types another computer's code and receives its chats. Nil in [Options] is a
// connection that cannot pair.
type Pairing interface {
	Offer(ctx context.Context, ui pair.OfferUI) (label string, err error)
	Join(ctx context.Context, typed string, ui pair.JoinUI) (pair.Joined, error)
}

// The panel's own words. Everything a person is TOLD comes from internal/pair;
// these are only the panel's furniture.
const (
	// pairUnavailableWord is the one honest sentence for a connection with no
	// door: a person is told why, and told nothing about how it is built.
	pairUnavailableWord = "pairing is not available here"
	// pairJoiningWord is all the joining side has to say before the other
	// computer answers.
	pairJoiningWord = "joining…"
	// pairSharingKeys, pairAskKeys and pairDoneKeys are the hint under the box.
	pairSharingKeys = "esc stop"
	pairAskKeys     = pair.AskChoice + " · esc is n"
	pairDoneKeys    = "esc close"
)

// pairRun is one pairing in flight: the context that ends it and the road its
// news travels to the screen. The panel holds at most one.
type pairRun struct {
	ctx    context.Context
	cancel context.CancelFunc
	events chan pairEvent
}

func newPairRun(parent context.Context) *pairRun {
	ctx, cancel := context.WithCancel(parent)
	return &pairRun{ctx: ctx, cancel: cancel, events: make(chan pairEvent, 8)}
}

// send puts one event on the road, and gives it up when the pairing has been
// closed: nobody is reading a road that was shut on purpose.
func (r *pairRun) send(event pairEvent) {
	select {
	case r.events <- event:
	case <-r.ctx.Done():
	}
}

// wait is the command that hands the next event to the loop. It answers nothing
// when the pairing is closed, so a closed panel leaves no command waiting.
func (r *pairRun) wait() tea.Cmd {
	return func() tea.Msg {
		select {
		case event := <-r.events:
			return pairMsg{run: r, event: event}
		case <-r.ctx.Done():
			return nil
		}
	}
}

// pairMsg is one event arriving. The run names the pairing it belongs to, so a
// late event from one that was closed is dropped and never drawn over the next.
type pairMsg struct {
	run   *pairRun
	event pairEvent
}

// pairEvent is one thing the door tells the screen. Each kind knows how it
// changes the panel, so the loop has no switch over them.
type pairEvent interface{ apply(p *pairPanel) }

type (
	// pairShown is a fresh code on screen, with the lines that say what to do.
	pairShown struct {
		code  *pair.Code
		lines string
	}
	// pairBurned is the code being gone: a wrong one was typed.
	pairBurned struct{}
	// pairAsked is a person being put a question. The answer goes down reply.
	pairAsked struct {
		label, words string
		reply        chan bool
	}
	// pairWaiting is the joining side waiting on the other computer's person.
	pairWaiting struct{ words string }
	// pairEnded is the last line: it worked, or the sentence for why not.
	pairEnded struct {
		line string
		ok   bool
	}
)

func (e pairShown) apply(p *pairPanel)   { p.code, p.lines, p.ask = e.code, e.lines, nil }
func (pairBurned) apply(p *pairPanel)    { p.code, p.lines, p.burned = nil, "", true }
func (e pairAsked) apply(p *pairPanel)   { p.ask = &e }
func (e pairWaiting) apply(p *pairPanel) { p.waiting = pair.WaitingChatsLine(e.words) }
func (e pairEnded) apply(p *pairPanel)   { p.result, p.done, p.ok, p.ask = e.line, true, e.ok, nil }

// pairUI is the screen as the two pairing devices see it: the one object that
// satisfies both [pair.OfferUI] and [pair.JoinUI], writing to a run's road.
type pairUI struct{ run *pairRun }

func (u pairUI) Show(code *pair.Code, lines string) { u.run.send(pairShown{code: code, lines: lines}) }
func (u pairUI) Burned()                            { u.run.send(pairBurned{}) }
func (u pairUI) Waiting(words string)               { u.run.send(pairWaiting{words: words}) }

// Ask blocks the door's goroutine and never the frame. It answers no when the
// context ends, which is how silence becomes a no.
func (u pairUI) Ask(ctx context.Context, label, words string) bool {
	reply := make(chan bool, 1)
	u.run.send(pairAsked{label: label, words: words, reply: reply})
	select {
	case yes := <-reply:
		return yes
	case <-ctx.Done():
		return false
	}
}

// pairPanel is the overlay's whole state. The zero value is closed and idle.
type pairPanel struct {
	open bool
	run  *pairRun

	// What the person sees, in the order it is drawn.
	burned  bool
	code    *pair.Code
	lines   string
	waiting string
	ask     *pairAsked
	result  string
	done    bool
	// ok says the pairing worked, which the first-run screen needs to dress
	// the last line as news or as trouble.
	ok bool
}

// close ends whatever is running, which is what deletes a code that is no
// longer on any screen, and puts the panel away.
func (p *pairPanel) close() {
	if p.run != nil {
		p.run.cancel()
	}
	*p = pairPanel{}
}

// start opens the panel on a new pairing.
func (p *pairPanel) start(run *pairRun) {
	p.close()
	*p = pairPanel{open: true, run: run}
}

// hide puts the panel away and lets the pairing go on: a code is this
// computer's and not a conversation's, so leaving a conversation must not spend
// it. /pair brings the same panel back.
func (p *pairPanel) hide() { p.open = false }

// answer gives the pending question its answer and takes it off the screen. The
// reply channel holds one, so this never waits on a goroutine that has left.
func (p *pairPanel) answer(yes bool) {
	if p.ask == nil {
		return
	}
	select {
	case p.ask.reply <- yes:
	default:
	}
	p.ask = nil
}

// rows is everything the panel says, wrapped to width and dressed.
func (p *pairPanel) rows(width int, now time.Time, pal palette) []string {
	var out []string
	say := func(text string, dress func(string) string) {
		for _, line := range wrap(text, max(width, 4)) {
			out = append(out, dress(line))
		}
	}
	if p.burned {
		say(pair.BurnLine, pal.accent)
	}
	if p.lines != "" {
		say(strings.TrimRight(p.lines, "\n"), pal.ink)
		say("  "+pairCountdown(p.code, now), pal.dim)
	}
	if p.waiting != "" {
		say(p.waiting, pal.dim)
	}
	if p.ask != nil {
		say(pair.AskChatsLine(p.ask.label, p.ask.words), pal.ink)
	}
	if p.result != "" {
		say(p.result, pal.ink)
	}
	return out
}

// pairCountdown is how long the code on screen has left, in whole minutes
// counted up so that the last minute reads as one and never as zero. The pulse's
// beat repaints it; no timer of its own runs while the panel is open.
func pairCountdown(code *pair.Code, now time.Time) string {
	if code == nil {
		return ""
	}
	left := pair.CodeValidFor - now.Sub(code.Born())
	if left <= 0 {
		return "the code has run out"
	}
	return fmt.Sprintf("%d min left", int((left+time.Minute-1)/time.Minute))
}

func (p *pairPanel) height(width int, now time.Time, pal palette) int {
	if !p.open {
		return 0
	}
	return len(p.rows(width, now, pal))
}

func (p *pairPanel) draw(width, n int, now time.Time, pal palette) []string {
	if n <= 0 || !p.open {
		return nil
	}
	lines := p.rows(width, now, pal)
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// hint is the keys the panel takes right now, for the slot under the box.
func (p *pairPanel) hint() string {
	switch {
	case p.ask != nil:
		return pairAskKeys
	case p.done:
		return pairDoneKeys
	}
	return pairSharingKeys
}

// ── the app's side ──────────────────────────────────────────────────────────

// runPair is /pair and /pair <code>. A pairing already in flight is shown again
// and not started twice: the process-wide guard in internal/pair would refuse
// the second, but the person's answer should be the panel they already had.
func (a *app) runPair(typed string) tea.Cmd {
	door := a.pairing
	if door == nil {
		a.note(pairUnavailableWord)
		return nil
	}
	if a.pair.run != nil && !a.pair.done {
		a.pair.open = true
		a.touch()
		return nil
	}
	a.closeLists()
	a.dismissWelcome()
	if typed == "" {
		return a.startPair("", func(run *pairRun) {
			label, err := door.Offer(run.ctx, pairUI{run})
			run.send(pairEnded{line: sharedLine(label, err), ok: err == nil})
		})
	}
	return a.startPair(pairJoiningWord, joinWork(door, typed))
}

// joinWork is the joining side's errand: type the code, and end on the line for
// how it went. It is the same errand from /pair <code> and from the first-run
// screen's field, so the two cannot end in different words.
func joinWork(door Pairing, typed string) func(*pairRun) {
	return func(run *pairRun) {
		joined, err := door.Join(run.ctx, typed, pairUI{run})
		run.send(pairEnded{line: joinedLine(joined, err), ok: err == nil})
	}
}

// startPair opens the panel on a new run, saying first what it says before the
// door has any news, and returns the two commands that drive it: the door's
// work, and the wait for its first event.
func (a *app) startPair(first string, work func(*pairRun)) tea.Cmd {
	run := newPairRun(a.ctx)
	a.pair.start(run)
	a.pair.waiting = first
	a.touch()
	return tea.Batch(func() tea.Msg { work(run); return nil }, run.wait())
}

// sharedLine is what the sharing side ends on: the device that was let in, or
// the sentence for why nobody was.
func sharedLine(label string, err error) string {
	if err != nil {
		return err.Error()
	}
	return pair.PairedChatsLine(label)
}

// joinedLine is what the joining side ends on. A computer that held these chats
// already gets that fact's own sentence and not a success it did not earn.
func joinedLine(joined pair.Joined, err error) string {
	switch {
	case err != nil:
		return err.Error()
	case joined.Already:
		return pair.ErrAlreadyPaired.Error()
	}
	return pair.JoinedLine
}

// tookPair files one event from the door and asks for the next, until the
// pairing has ended. An event from a pairing that was closed is dropped.
func (a *app) tookPair(msg pairMsg) tea.Cmd {
	if msg.run != a.pair.run {
		return nil
	}
	msg.event.apply(&a.pair)
	a.touch()
	if a.pair.done {
		return nil
	}
	return msg.run.wait()
}

// pairKey is the panel's whole claim on the keyboard. While a person is asked,
// only y and n and esc mean anything: no default, so a stray enter never lets a
// device in. Otherwise esc, or enter once it is over, puts the panel away.
func (a *app) pairKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.pair
	switch key := msg.String(); {
	case p.ask != nil && key == "y":
		p.answer(true)
	case p.ask != nil && (key == "n" || key == "esc"):
		p.answer(false)
	case p.ask != nil:
	case key == "esc" || (p.done && key == "enter"):
		p.close()
	}
	a.touch()
	return nil
}
