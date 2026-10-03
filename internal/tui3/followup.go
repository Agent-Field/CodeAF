package tui3

import (
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE FOLLOW-UP: ctrl+enter, "and after that, do this".
//
// Plain enter steers into the running turn when eligible, and otherwise parks
// the message here on the surface (steer.go, park.go). ctrl+enter hands a
// follow-up to the session to run after the current turn. It queues only from
// this conversation's own box, with non-empty words that are not a /command;
// everywhere else it is plain enter, including on the new-chat start page.
//
// A decoded ctrl+enter is honoured wherever the terminal sends it, including
// modifyOtherKeys terminals that never answer the keyboard-enhancement query.
// THE HINT AND TIP REQUIRE THE TERMINAL'S REPLY before advertising the chord.
// A terminal that cannot distinguish it sends enter on many keyboards and
// ctrl+j on some; those remain the ordinary send and newline respectively.
//
// A parked message is still the person's: it can be edited, taken back with ↑,
// or sent at once by stopping the turn (ctrl+shift+enter, bargein.go). A locally
// queued follow-up comes back only after the session says it was removed, and
// it merges into this conversation's composer rather than replacing a new
// draft. A message whose turn already started cannot be taken back. An
// interrupt drops queued messages because a drain never restarts stopped work.
//
// Both queues drain at the stream's close and the SESSION'S goes first (app.go's
// streamClosedMsg): a follow-up was handed over before the parked message was
// typed in the ordinary case, and a surface that let the newer sentence jump it
// would be reordering the person's own words.
//
// THE QUEUE IS DRAWN MESSAGE BY MESSAGE, and the drawing is the difference
// between waiting and sent: each row sits above the box in the queued glyph and
// dim ink, a register nothing in the transcript wears, and the rows only leave
// when their turn starts — where the message lands as an ordinary sent line. A
// count alone ("after yield · 2", the row this block replaced) made a person
// guess which of their sentences were still queued and which were being read.

// queued is one follow-up: what was typed, and the stream the turn it starts
// will speak on. The channel exists from the moment the message is queued —
// session hands it back immediately — so there is nothing to wait for later.
//
// THE DRAFT TRAVELS WITH IT so a take-back can put the box back exactly as it
// was. The pasted documents are the half that has to: queueing unfolds them
// into the words the model reads and spends the chips ([app.composed]), so
// without them a click would restore the tokens as dead text and the next send
// would carry `[paste 1 · 4 lines]` to the model.
type queued struct {
	covered func() bool
	text    string
	pastes  []pasteChip
	demoted []segment
	ch      <-chan session.Event
	// recallable says this surface queued it and holds its take-back receipt.
	// Streams admitted from another window or a fallen-through steer do not.
	recallable bool
	// taking holds the pending answer, so the row cannot be clicked or started
	// while the session is deciding whether it ran.
	taking *followRecall
	// woken says nobody typed this one: it is a turn THE SESSION STARTED ON ITS
	// OWN (see the wake lane below). It rides the same queue because the queue
	// is about streams waiting for the one being pumped, and that is exactly
	// what it is — but it is not a message, so it is not counted above the box
	// and it writes no user line when it starts.
	woken bool
}

// followMsg carries the session's answer back into the program loop. FollowUp
// takes the agent's lock, and the Update loop is not a place to wait.
type followMsg struct {
	call    *hostCall
	text    string
	pastes  []pasteChip
	demoted []segment
	ch      <-chan session.Event
	err     error
}

// followUp is the queue key (input.go's `ctrl+enter` case). An empty draft does
// nothing at all: there is no message
// to queue, and a key that queued a blank one would be a key that spends a turn.
//
// THE QUEUE CARRIES WORDS ALONE, so a tray holding anything else is refused
// rather than split. [Agent.FollowUp] takes text: queueing over a picture or a
// picked harness would send the words later and leave the rest on the tray to
// ride out with whatever was typed next — one message quietly becoming two.
// Nothing is queued and the draft is untouched, the refusal the standing chord
// this key replaced made on the same ground.
func (a *app) followUp() tea.Cmd {
	line := strings.TrimSpace(a.input.String())
	if line == "" {
		return nil
	}
	if !a.trayEmptyForQueue() {
		a.note(queueWordsOnly)
		return nil
	}
	a.noticeEvent(eventQueued)
	// The model reads the paste and the queue's row keeps the tag (pastechip.go);
	// the chips are kept BESIDE the tag so a take-back can put the draft back
	// whole ([app.recallQueuedAt]).
	pastes := append([]pasteChip(nil), a.pastes...)
	demoted := append([]segment(nil), a.input.demotedTags...)
	// The queued words have always been trimmed. Plain-tag positions belong
	// to those same words, so removing the leading whitespace shifts them by
	// its rune count rather than silently making the slash words live again.
	raw := a.input.String()
	trimmed := len([]rune(raw)) - len([]rune(strings.TrimLeftFunc(raw, unicode.IsSpace)))
	for i := range demoted {
		demoted[i].from -= trimmed
		demoted[i].to -= trimmed
	}
	spoken, _ := a.composed(line)
	a.input.reset()
	a.endRecall()
	a.closeLists()
	// A queued message is a submitted one for every purpose the person has: it
	// is remembered by ↑, and the draft file it came from is done with.
	a.remember(line)
	a.dropDraft()
	a.stick = true
	a.touch()
	return a.sendFollowFrom(a.agent, spoken, line, pastes, demoted)
}

func (a *app) sendFollow(spoken, line string, pastes []pasteChip) tea.Cmd {
	return a.sendFollowFrom(a.agent, spoken, line, pastes, nil)
}

func (a *app) sendFollowFrom(agent Agent, spoken, line string, pastes []pasteChip, demoted []segment) tea.Cmd {
	if a.deferHosted(func() tea.Cmd { return a.sendFollowFrom(agent, spoken, line, pastes, demoted) }) {
		return nil
	}
	call := a.hostCallStarted()
	return func() tea.Msg {
		ch, err := agent.FollowUp(spoken)
		return followMsg{text: line, pastes: pastes, demoted: demoted, ch: ch, err: err, call: call}
	}
}

// queueFollow takes the session's answer.
//
// A follow-up queued while NOTHING is running starts immediately — session says
// so, and it is the right answer: there is no turn end coming to drain it. So a
// stream we are not already pumping is adopted here rather than at the next
// close, which would never arrive.
func (a *app) queueFollow(msg followMsg) (cmd tea.Cmd) {
	defer func() { cmd = tea.Batch(cmd, a.hostCallSettled(msg.call)) }()
	if msg.err != nil {
		a.note("follow-up failed: " + msg.err.Error())
		return nil
	}
	if msg.ch == nil {
		return nil
	}
	a.follows = append(a.follows, queued{text: msg.text, pastes: msg.pastes, demoted: msg.demoted, recallable: true, ch: msg.ch, covered: a.hostStreamCovered(msg.ch)})
	a.touch()
	if a.stream != nil {
		return nil
	}
	return a.startFollow()
}

// startFollow begins the next queued message's turn: the person's line lands in
// the transcript, and the channel session already handed us becomes the stream.
//
// It is [app.submit] without the submit — the turn was started by the session
// when the last one ended, so there is nothing to ask for and nothing to wait on.
func (a *app) startFollow() tea.Cmd {
	if a.hostReplayLoading || a.hostReplayWaiting || a.stream != nil || len(a.follows) == 0 {
		return nil
	}
	for len(a.follows) > 0 && a.follows[0].taking == nil && a.follows[0].covered != nil && a.follows[0].covered() {
		a.follows = a.follows[1:]
	}
	if len(a.follows) == 0 || a.follows[0].taking != nil {
		return nil
	}
	next := a.follows[0]
	a.follows = a.follows[1:]

	a.closeLive()
	a.turn++
	a.sel = -1
	// NO USER LINE FOR A TURN NOBODY ASKED FOR. A woken turn is the session
	// speaking because work landed while the room was idle, and a "›" row above
	// it would be this surface putting words in a person's mouth. Everything
	// else about the turn is identical: it is the next thing that happens in the
	// conversation, and it is drawn as one.
	if !next.woken {
		// The context the turn runs in rides with it, for [app.submitting]'s reason
		// (turncontext.go): a follow-up is the person's own sentence arriving one
		// turn late, and where it goes is the same fact about it either way.
		value := []rune(next.text)
		plain := restingDoorWords(value)
		// A DEMOTION TRAVELS WITH THE QUEUED WORDS. The fallback knows only
		// resting door words, so the queue's own annotations must join it. Match
		// against the same spans the transcript paints, including quoted names,
		// to drop stale ranges and duplicates.
		for _, s := range paintedCommandSpans(value, true) {
			if containsSegment(next.demoted, s) && !containsSegment(plain, s) {
				plain = append(plain, s)
			}
		}
		a.entries = append(a.entries, entry{
			kind: entryUser, text: next.text, turn: a.turn, context: a.turnContext(),
			plainTags: plain,
		})
	}
	a.state = stateWorking
	a.lastDelta = time.Now()
	// The turn is open and the first request is out with nothing back from it.
	a.awaited = time.Now()
	// The generation is bumped for the same reason [app.adopt] bumps it: the
	// stream that just closed may still have messages in flight, and they belong
	// to a turn that is over.
	a.gen++
	a.stream = next.ch
	a.follow()
	a.touch()
	return tea.Batch(waitEvent(next.ch, a.gen), a.wake())
}

// dropFollows forgets everything queued. The session drops its own queue on an
// interrupt — a stop followed by the session working again is not a stop — so
// the surface must not keep showing a count for turns that will never run, and
// must say that it dropped them: the person typed those words.
func (a *app) dropFollows() {
	// The COUNT is of messages, and a woken stream is not one: an interrupt
	// drops it with the rest — the session it belonged to has been stopped — but
	// a surface that counted it would report a queued message the person never
	// typed.
	n := a.followWaiting()
	if len(a.follows) == 0 {
		return
	}
	a.follows = nil
	if n == 0 {
		return
	}
	if n == 1 {
		a.note("1 queued message dropped")
	} else {
		a.note(itoa(n) + " queued messages dropped")
	}
}

// followWaiting is how many QUEUED MESSAGES are waiting — the person's words,
// and not the woken streams that share the queue with them.
func (a *app) followWaiting() int {
	n := 0
	for _, q := range a.follows {
		if !q.woken {
			n++
		}
	}
	return n
}

// followHeight is the block's height: one row per row a queued message wraps
// over, or none.
func (a *app) followHeight() int {
	width, _ := a.size()
	return len(a.followRows(width))
}

// queueWordsOnly is the refusal when the tray holds something the queue cannot
// carry ([app.followUp]).
const queueWordsOnly = "ctrl+enter queues words alone — take the pictures or the shape of work off first"

// trayEmptyForQueue reports whether the tray holds nothing the queue would have
// to leave behind: no picture and no picked harness.
func (a *app) trayEmptyForQueue() bool {
	return len(a.chips) == 0 && a.harnChip == ""
}

// queueFootWord is what the running foot calls the queue key: the short
// key-then-noun form every clause on that row keeps, not the queued block's
// sentence — the foot names the key, the block says what happened.
const queueFootWord = "ctrl+enter queue"

// queueSendOffered is the key's predicate: words for this conversation's
// running turn, from its own composer. Commands and live send-door tags take
// enter's ordinary road, so queueing cannot discard a tag's meaning. The tray
// refusal remains [app.followUp]'s answer, rather than a send that quietly
// splits one message in two.
func (a *app) queueSendOffered() bool {
	line := strings.TrimSpace(a.input.String())
	return a.runSendOffered() && !a.startingChat() && a.composerOwner == mainRecipient &&
		line != "" && !strings.HasPrefix(line, "/") && len(a.liveTags()) == 0
}

// queueFootOffered advertises the chord only after the terminal says it can
// distinguish keys, and only when the queue can carry the whole draft. The key
// itself accepts a decoded chord without that reply, because modifyOtherKeys
// can send it too.
func (a *app) queueFootOffered() bool {
	return a.keysDisambiguated && a.queueSendOffered() && a.trayEmptyForQueue()
}

// followRows draws the queued block: the messages the SESSION is holding, each
// led by the return arrow, and nothing else.
//
// THE REGISTER IS THE POINT. A sent message is the person's accent hue with
// their own glyph, and a parked one is the same hue held between the answer and
// the box; these rows are dim and led by the return key's arrow
// (tokens.GFollowUp — the key, held with ctrl, that put them there), so a
// person scanning the foot can see at a glance which of their sentences the
// session is holding and which of them are already being worked. They leave
// the block when their turn starts — where the words land in the transcript as
// the ordinary sent line they become.
//
// THERE IS NO LINE UNDER THE BLOCK, by the owner's call (2026-09-30). It said
// `queued for after this turn · click takes one back · ctrl+enter queues the
// draft`; the arrow says the first, the hover says the second, and the running
// foot says the third, so the sentence was three things already on the frame.
func (a *app) followRows(width int) []string {
	if a.followWaiting() == 0 || width < 5 {
		return nil
	}
	out := make([]string, 0, a.followWaiting())
	// A ROW LIGHTS ONLY WHERE A PRESS WOULD TAKE IT. With no line under the
	// block to say so, the hover is the whole of the take-back's advertisement,
	// and an agent that cannot unqueue must not be shown offering to.
	for i, q := range a.follows {
		if q.woken {
			continue
		}
		// THE WHOLE MESSAGE LIGHTS, NOT THE ROW THE POINTER IS ON — park.go's
		// rule, because a sentence that wrapped over three rows with one of them
		// banded would read as three things.
		hot := a.queuedTakesBackAt(i) && a.hoveringQueued(i)
		for j, line := range followLines(q.text, width) {
			lead := "   "
			if j == 0 {
				lead = "  " + a.pal.dim(a.icon(tokens.GFollowUp)) + " "
			}
			text := lead + a.pal.dim(line)
			if hot {
				text = a.hoverRow(text, width)
			}
			out = append(out, text)
		}
	}
	return out
}

// followLines is the one width calculation for drawing and hit testing. The
// first lead uses four cells and continuations use three, so every text line
// leaves room for the longer lead. Narrow frames do not borrow wrap's four-cell
// minimum, which would make the complete queued row wider than the frame.
func followLines(text string, width int) []string {
	if width < 5 {
		return nil
	}
	text = strings.ReplaceAll(text, "\t", "    ")
	lines := strings.Split(ansi.Wrap(text, width-4, ""), "\n")
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width-4, "")
	}
	return lines
}

// followMark is the pointer's answer for one row of the block: which queued
// message that row belongs to, so a click can take that one back.
func (a *app) followMark(row, width int) chromeRow {
	at := 0
	for i, q := range a.follows {
		if q.woken {
			continue
		}
		height := len(followLines(q.text, width))
		if row >= at && row < at+height {
			return chromeRow{kind: chromeQueued, index: i}
		}
		at += height
	}
	return chromeRow{}
}

// followPress is a click on the queued block: that message comes out of the
// session's queue and back into the box whole, draft and all.
//
// THE POINTER IS THE ONLY WAY TO THIS QUEUE. ↑ belongs to the parked block and
// to history and never reaches a queued row (input.go), so the click is the one
// gesture that takes an individual message back.
//
// THE CLICK IS ANSWERED AT ONCE and the words arrive with the fold
// (offloop.go): the press is claimed here — the pointer named a message — and
// what the session said about it lands on the next pass. A false from the
// agent leaves the row exactly where it was, which is the answer the row was
// promised (recallQueuedAt).
func (a *app) followPress(y int) (tea.Cmd, bool) {
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeQueued {
		return nil, false
	}
	if cmd, asked := a.recallQueuedAt(mark.index); asked {
		return cmd, true
	}
	return nil, false
}

// followUnqueuer is the agent's half of the take-back (session's
// [session.Agent.UnqueueFollowUp]). It is asserted rather than added to [Agent]
// for the reason [wakeAgent] is: a surface driven by a scripted agent must stay
// representable, and an agent that cannot unqueue is one whose queued rows this
// surface does not offer to take.
type followUnqueuer interface {
	UnqueueFollowUp(ch <-chan session.Event) bool
}

// queuedTakesBack reports whether any row holds this window's own receipt and
// is not already waiting for an answer. Hover and press ask the same per-row
// predicate, so a row without a working gesture never lights.
func (a *app) queuedTakesBack() bool {
	for i := range a.follows {
		if a.queuedTakesBackAt(i) {
			return true
		}
	}
	return false
}

func (a *app) queuedTakesBackAt(i int) bool {
	if _, ok := a.agent.(followUnqueuer); !ok || i < 0 || i >= len(a.follows) {
		return false
	}
	q := a.follows[i]
	return q.recallable && !q.woken && q.taking == nil
}

// followRecall keeps one click's snapshot and answer until earlier clicks have
// folded. The door line asks in order; Bubble Tea may deliver the answers in a
// different order, and restoring both sentences must still keep click order.
type followRecall struct {
	one      queued
	front    int
	answered bool
	out      bool
}

// recallQueuedAt marks the row before asking the session off the loop. The mark
// prevents another click and prevents startFollow from drawing a user line for
// a message the session may already have removed. Only the answer settles it.
func (a *app) recallQueuedAt(i int) (tea.Cmd, bool) {
	if !a.queuedTakesBackAt(i) {
		return nil, false
	}
	one := a.follows[i]
	pending := &followRecall{one: one, front: a.frontGen}
	a.follows[i].taking = pending
	a.followRecalls = append(a.followRecalls, pending)
	a.touch()
	unqueuer := a.agent.(followUnqueuer)
	cmd := a.offLoop(func() func(here bool) tea.Cmd {
		out := unqueuer.UnqueueFollowUp(one.ch)
		return func(here bool) tea.Cmd {
			pending.answered, pending.out = true, out
			if here {
				if out {
					a.removeQueuedByStream(one.ch)
				} else {
					// FALSE KEEPS THE TURN. Release the mark so a parent that
					// closed while the answer was crossing can adopt this turn.
					for i := range a.follows {
						if a.follows[i].taking == pending {
							a.follows[i].taking = nil
						}
					}
				}
				a.touch()
			}
			cmd := a.foldFollowRecalls()
			if here {
				cmd = tea.Batch(cmd, a.startFollow(), a.sendParked())
			}
			return cmd
		}
	})
	return cmd, true
}

// foldFollowRecalls restores successful answers in click order, always to this
// conversation's main composer even when a task room has the keyboard. If the
// conversation was replaced, no words go into its replacement: they remain
// reachable through ↑ history, where queueing already remembered them.
func (a *app) foldFollowRecalls() tea.Cmd {
	changed := false
	for len(a.followRecalls) > 0 && a.followRecalls[0].answered {
		one := a.followRecalls[0]
		a.followRecalls[0] = nil
		a.followRecalls = a.followRecalls[1:]
		if !one.out || one.front != a.frontGen {
			continue
		}
		a.atMainComposer(func(state *composerState) { mergeQueuedDraft(state, one.one) })
		changed = true
	}
	if !changed {
		return nil
	}
	a.stick = true
	a.touch()
	if a.composerOwner == mainRecipient {
		return a.edited()
	}
	return a.armDraftKeep()
}

// mergeQueuedDraft appends the taken-back sentence without spending anything
// the composer already held. Its paste tokens are renamed together, so a new
// number cannot collide with another old token, and plain-tag ranges follow
// the same replacements before being shifted past the existing draft.
func mergeQueuedDraft(state *composerState, one queued) {
	text := one.text
	demoted := append([]segment(nil), one.demoted...)
	pastes := append([]pasteChip(nil), one.pastes...)
	if len(state.box.value) > 0 || len(state.pastes) > 0 {
		n := 0
		for _, held := range state.pastes {
			n = max(n, held.n)
		}
		pairs := make([]string, 0, len(pastes)*2)
		for i := range pastes {
			n++
			lines := pasteLineCount(pastes[i].text)
			pairs = append(pairs, pasteToken(pastes[i].n, lines), pasteToken(n, lines))
			pastes[i].n = n
		}
		if len(pairs) > 0 {
			rewrite := strings.NewReplacer(pairs...)
			runes := []rune(text)
			for i, span := range demoted {
				demoted[i] = segment{
					from: len([]rune(rewrite.Replace(string(runes[:span.from])))),
					to:   len([]rune(rewrite.Replace(string(runes[:span.to])))),
				}
			}
			text = rewrite.Replace(text)
		}
	}
	value := state.box.String()
	if len(state.box.value) > 0 {
		value += "\n"
	}
	offset := len([]rune(value))
	plain := append([]segment(nil), state.box.demotedTags...)
	for _, span := range demoted {
		plain = append(plain, segment{from: span.from + offset, to: span.to + offset})
	}
	state.box.setText(value + text)
	state.box.demotedTags = plain
	state.pastes = append(state.pastes, pastes...)
}

// removeQueuedByStream takes the queued message whose stream is ch off the
// surface's queue, and reports whether it was there. It is the fold's half of
// the take-back: the session answered by the stream, so the surface answers by
// the stream too, and the message that goes is the one the person named.
func (a *app) removeQueuedByStream(ch <-chan session.Event) bool {
	for i, q := range a.follows {
		if q.ch == ch && !q.woken {
			a.follows = append(a.follows[:i], a.follows[i+1:]...)
			return true
		}
	}
	return false
}

// ── THE WAKE LANE ───────────────────────────────────────────────────────────
//
// A TURN NOBODY ASKED FOR IS STILL A TURN, AND IT IS DRAWN.
//
// Work handed to a task lands minutes later, usually into an idle room: the
// session takes the news off its steering queue and STARTS A TURN OF ITS OWN to
// say what it makes of it (session's agent.go). That turn has no caller — that
// is what makes it a wake — so its events go to the journal and, without this,
// nowhere else: the person would see the completion card this surface draws and
// never the sentence the model wrote about it.
//
// [session.Agent.Wakes] hands one channel per woken turn, before its first
// event, and the adoption is the follow-up's own ([app.startFollow]) MINUS the
// user line: same generation bump, same pump, same close. The wake NOTE itself
// is not drawn here — it is a line the model was told, and it stays where it
// is; what lands in the conversation is the reply.

// wakeAgent is the standing subscription to woken turns, asserted rather than
// added to [Agent] for the reason [taskAgent] is (task.go): a surface driven by
// a scripted agent that never wakes must stay representable.
type wakeAgent interface {
	Wakes() <-chan (<-chan session.Event)
}

// watchWakes opens the lane and starts pumping it. It runs where [app.watchTasks]
// runs and for the same reason — once at boot, again wherever the agent is
// REPLACED — because the channel belongs to the agent that handed it over.
func (a *app) watchWakes() tea.Cmd {
	agent, ok := a.agent.(wakeAgent)
	if !ok {
		return nil
	}
	a.wakeGen++
	if leavable, ok := agent.(leavableWaker); ok {
		a.wakeLane, a.stops.wakes = leavable.WatchWakes()
	} else {
		a.wakeLane, a.stops.wakes = agent.Wakes(), nil
	}
	return waitWake(a.wakeLane, a.wakeGen)
}

// leavableWaker is the wake lane WITH A WAY OUT OF IT (session's agent.go). It
// is asserted separately from [wakeAgent] for that interface's own reason, and
// a nil stop is an agent that can only be abandoned (switcher.go's [laneStops]).
type leavableWaker interface {
	WatchWakes() (<-chan (<-chan session.Event), func())
}

// waitWake takes one woken turn's stream off the lane and asks for the next.
func waitWake(lane <-chan (<-chan session.Event), gen int) tea.Cmd {
	return func() tea.Msg {
		ch, ok := <-lane
		if !ok {
			return wakeLaneClosedMsg{gen: gen}
		}
		return wokenMsg{gen: gen, ch: ch}
	}
}

// adoptWake takes one woken turn.
//
// IT GOES THROUGH THE FOLLOW-UP QUEUE, and that is the whole of the ordering
// rule: a wake is handed over BEFORE its first event, and a surface that is
// still pumping the tail of another stream would otherwise have two turns
// speaking into one transcript. Queued, it is adopted at the next close by the
// same [app.startFollow] that starts a queued message — and on the ordinary
// path, with nothing being pumped, that is right now.
//
// The lane is re-armed FIRST: a second landing while this one is drawn is a
// second turn, and the pump is what hears about it.
func (a *app) adoptWake(msg wokenMsg) tea.Cmd {
	if msg.gen != a.wakeGen {
		return nil
	}
	next := waitWake(a.wakeLane, a.wakeGen)
	if msg.ch == nil {
		return next
	}
	a.follows = append(a.follows, queued{ch: msg.ch, woken: true})
	a.touch()
	if a.stream != nil {
		return next
	}
	return tea.Batch(next, a.startFollow())
}
