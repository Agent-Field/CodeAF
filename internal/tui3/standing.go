package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// THE STANDING SIDE, IN THE CONVERSATION: ONE CARD, AND ONE LINE AFTERWARDS.
//
// A STANDING ITEM IS A TASK WITH A WHEN (internal/session's standing_contract.go
// says so first). That sentence is the whole of this file's shape: the block
// below is [app.taskCardRows]'s block — the same corner, the same question hue,
// the same stem, the same three-chip answer row, the same draining meter — with
// TWO MORE BANDS between the words and the answers:
//
//	when   the model's own phrasing of the cadence, and never the spec. A cron
//	       line read back to a person is a spec nobody can check.
//	costs  what one firing may spend and how often it may fire, plus — on the
//	       three kinds that are actually LOOKED at on a clock — how often the
//	       looking happens.
//
// AND IT DECLINES ON THE CLOCK, WHICH IS THE ONE PLACE IT PARTS FROM THE TASK
// CARD. A task proposal approves on silence because its work is bounded and
// watched; a standing item spends forever with nobody in the room, so an
// unanswered card ends as nothing (standing_contract.go states the law). The
// meter therefore says `ends in` rather than `auto-starts in`, and the settled
// row says nothing was set up. THE ENGINE OWNS THAT CLOCK: this surface stops
// asking when the deadline passes and never answers for it, exactly as
// [app.tickTasks] does for the other card.
//
// AFTERWARDS THERE IS ONE LINE AND NEVER TWO. Everything a standing item does
// once it stands — it was set up, it said something, it needs somebody, it
// stopped — arrives as EventStandingUpdate and draws exactly one dim row in the
// transcript ([standUpdateRow]). QUIET IS THE DESIGN: a routine that checked and
// found nothing writes nothing here at all, and the surface must never be the
// place that turns a quiet mechanism into a chatty one.

// standingAgent is the slice of the engine this file needs, and it is asserted
// rather than added to [Agent] — the standing contract is OPTIONAL, exactly as
// the task contract is ([taskAgent] says why at more length). A scripted agent
// that has never heard of a standing item is a session with the ambient side
// off, and it must stay representable.
type standingAgent interface {
	// ResolveStanding answers one proposal: set it up, set it up once, or a
	// correction in the person's own words. It is called ONCE per card, with
	// both answers in it when the card asked two questions.
	ResolveStanding(id uint64, answer session.StandingAnswer)
}

// stander is the agent under this surface, when it has one that can be answered.
func (a *app) stander() (standingAgent, bool) {
	agent, ok := a.agent.(standingAgent)
	return agent, ok
}

// ── the card's state ────────────────────────────────────────────────────────

// standStage is which question the card is asking. A card asks at most two, and
// it asks them ONE AT A TIME in the same block: the second is drawn where the
// first one's chips were, so a person answering a follow-up is looking at the
// same card they just said yes to rather than at a second one underneath it.
type standStage uint8

const (
	// standAsking is the proposal itself: yes, change when, or once.
	standAsking standStage = iota
	// standWatching is the one-time follow-up a first item earns
	// ([session.StandingNotice.OfferWatch]): keep checking with no window open?
	standWatching
)

// standingCard is one standing proposal, from the question to what it came to —
// or, when [standingCard.update] is set, one line of news about an item that
// already stands.
//
// It is a pointer held in two places — the transcript entry that draws it and
// [app.stand], the lane that answers it — so the row and the answer can never
// disagree about what was decided. That is [taskCard]'s own arrangement and it
// is here for the same reason.
type standingCard struct {
	id   uint64
	item standing.Item
	// name is the short cut of the person's words the head wears, and words is
	// the whole sentence, drawn under it only when the cut lost something. Both
	// are derived once: a card is read on every frame while its clock runs.
	name, words string
	// when and cost are the two bands, in the model's own words at proposal
	// time. THE SURFACE QUOTES THEM AND NEVER COMPUTES THEM — a spec read back
	// as cron is a spec nobody can check (standing_contract.go).
	when, cost string
	// guessed says the model invented the cadence because the person gave none,
	// so the when band ASKS instead of stating.
	guessed bool
	// offerWatch says a yes is followed by the one-time question rather than by
	// an answer.
	offerWatch bool
	// answers is the proposal's chip row, in order, as THE ENGINE named it
	// ([session.StandingNotice.Options] via [standAnswerWords]). It is held on
	// the card rather than worked out at draw time because the keyboard asks
	// which digits belong to this question before any frame has been painted.
	answers []string
	// deadline is when the card stops asking, and zero when the engine is
	// holding it open indefinitely. born is when it arrived, which the meter
	// needs for the other end of its span.
	deadline, born time.Time

	// stage is which question is on the chips right now, choice is which chip
	// has the keyboard, and typing says the person asked for the box so that
	// the letters that would otherwise answer are text again.
	stage  standStage
	choice int
	typing bool
	// approved is the yes already given, waiting on the follow-up. It is not an
	// answer yet: [app.answerStanding] is called ONCE, with both halves.
	approved bool

	// choiceRow is where the chips landed inside this card's rendered rows, or
	// -1, and spans are the columns each chip occupies on it. Written by the
	// render and read by the hit-testing, so a click can never answer a
	// question the frame drew somewhere else ([choiceSpan]).
	choiceRow int
	spans     []choiceSpan

	// verdict is what was decided, in the words the row keeps afterwards, and
	// answer is the chip that settled it. Both are empty for exactly as long as
	// the question is open.
	verdict, answer string

	// update is set on a NEWS row rather than on a card: the block is then one
	// dim line and nothing else (see this file's header). text is what that line
	// carries.
	update, text string
	glyph        string
}

// settled reports whether this proposal has been answered.
func (c *standingCard) settled() bool { return c.verdict != "" }

// news reports whether this block is one line of news rather than a question.
func (c *standingCard) news() bool { return c.update != "" }

// ── the words ───────────────────────────────────────────────────────────────

// The sentences this file says. Every one of them is quoted in
// internal/manual/chat/home.md exactly as it is spelled here.
const (
	// The three answers, and they carry their own keys the way the models row
	// does: a digit rather than an initial, because "yes, set it up" and "once,
	// not standing" have nothing to pick an initial out of that a person would
	// guess ([taskModelChip] made the same trade for the same reason).
	standYesWord    = "yes, set it up"
	standChangeWord = "change when"
	standOnceWord   = "once, not standing"

	// The follow-up's two answers ([session.StandingNotice.OfferWatch]).
	standAlwaysWord = "yes, always"
	standWindowWord = "only while a window is open"
	// standWatchAsk is the follow-up's own question, drawn where the sub line
	// was. It is one sentence and it is asked ONCE, ever.
	standWatchAsk = "keep checking when no window is open?"

	// The two band labels. They are lower-case nouns and not headings: this is
	// a card in a conversation, and a card with a heading on every row is a form.
	standWhenTag = "when · "
	standCostTag = "costs · "
	// standGuessTag is what the when band adds when the model made the cadence
	// up. THE CARD ASKS RATHER THAN STATES — a guess presented as a fact is the
	// one thing on this block a person cannot audit afterwards
	// (docs/AMBIENT.md's card).
	standGuessTag = " — you didn't say, so that's my guess. Right?"
	// standCheckTag is how often the world is actually LOOKED at, and it is
	// derived from [standing.Interval] rather than written down, because a
	// number that appears in two places drifts.
	standCheckTag = "checked every "

	// standEndsWord is the meter's label. It is `ends in` and NOT
	// `auto-starts in`: silence declines this card (standing_contract.go).
	standEndsWord = "ends in "
	// standChangeLane is the box's placeholder while a correction is being
	// written.
	standChangeLane = "say when instead… (enter sends it, esc leaves it alone)"
	// standProposalHint is the hint slot's line while the card is up, and
	// standTwoHint is the same line for a card with no `once` on it — a one-off
	// reminder's ([standAnswerWords]). THE HINT NAMES THE KEYS THE CARD DREW and
	// never one more: a hint offering a digit the chips do not is the same
	// defect as a chip that does nothing.
	standProposalHint = "1 yes · 2 change when · 3 once · esc no"
	standTwoHint      = "1 yes · 2 change when · esc no"
	// standWatchHint is the same slot during the follow-up.
	standWatchHint = "1 always · 2 only while a window is open"

	// The verdicts a settled card keeps. They are sentences and not states,
	// because the row is read once, later, by somebody reconstructing what
	// happened.
	standSetWord     = "set up"
	standAlwaysDone  = "set up · checking even with no window open"
	standWindowDone  = "set up · only while a window is open"
	standChangedWord = "you asked for a different when"
	standOnceDone    = "once, not standing"
	standNoWord      = "not set up"
	standExpiredWord = "ended · nothing was set up"
)

// The glyphs a standing row wears, and their stand-ins on a terminal that
// cannot draw them.
//
// THEY ARE [standing.Item.Glyph]'S OWN THREE PLUS ONE. The store decides what a
// row leads with so that every surface agrees (its Glyph method is the
// authority); what belongs here is only the ASCII tier, which is a fact about
// the terminal rather than about the item. `●` and `▲` already have stand-ins on
// this surface ([homeLiveASCII], [homeAskASCII]) and keep them.
const (
	standWaitGlyph = "◦"
	standWaitASCII = "-"
	standOffGlyph  = "∙"
	standOffASCII  = "."
	// standNewsGlyph is home's fourth reading and NOT the store's: news since
	// you last looked (docs/AMBIENT.md's `◆`). It is derived and never asserted
	// — see [standNews].
	standNewsGlyph = "◆"
	standNewsASCII = "+"
)

// standGlyph is one item's mark on this surface: the store's own answer, with
// the ASCII tier applied and home's news reading folded in.
//
// THE STORE'S ORDER IS KEPT WHOLE. Needs-you and running both outrank news —
// a person looking at `▲` is looking at the one row that costs a keystroke to
// unblock, and turning it into `◆` because something also fired would be this
// surface losing a fact to say something weaker.
func standGlyph(item standing.Item, running, news bool, ascii bool) string {
	glyph := item.Glyph(running)
	if news && glyph == standWaitGlyph {
		glyph = standNewsGlyph
	}
	if !ascii {
		return glyph
	}
	switch glyph {
	case homeAskGlyph:
		return homeAskASCII
	case homeLiveGlyph:
		return homeLiveASCII
	case standNewsGlyph:
		return standNewsASCII
	case standOffGlyph:
		return standOffASCII
	}
	return standWaitASCII
}

// standChecked is the "checked every …" clause, and it is drawn only on the
// three kinds that are actually LOOKED at on a clock.
//
// A reminder and a routine are not checked, they are DUE: nothing is examined
// between now and Monday morning, and a card telling a person their 9am
// reminder is "checked every 5 minutes" would be describing the ticker's own
// housekeeping as though it were work done on their behalf.
func standChecked(kind standing.WhenKind) string {
	switch kind {
	case standing.WhenProbe, standing.WhenFile, standing.WhenIdle:
		return standCheckTag + everyWord(standing.Interval)
	}
	return ""
}

// everyWord spells a cadence the way a person says it. It is only ever handed
// [standing.Interval], and it reads that constant rather than repeating the
// figure, because a number in two places is a number that drifts.
func everyWord(d time.Duration) string {
	switch {
	case d >= time.Hour:
		hours := int(d / time.Hour)
		return itoa(hours) + plural(" hour", hours)
	case d >= time.Minute:
		minutes := int(d / time.Minute)
		return itoa(minutes) + plural(" minute", minutes)
	default:
		seconds := int(d / time.Second)
		return itoa(seconds) + plural(" second", seconds)
	}
}

// standNameWords is how much of the person's sentence the head wears. Six is
// longer than a task's name ([taskTitleWords]) because a standing item's words
// ARE its identity — "remind me every Sunday to water the plants" is the thing,
// where a task's title is a label somebody wrote for one — and the whole
// sentence is drawn underneath whenever the cut lost anything.
const standNameWords = 6

// standName is the head's cut of the person's words.
func standName(words string) string {
	if name := firstWords(leadSentence(words), standNameWords); name != "" {
		return name
	}
	return strings.TrimSpace(words)
}

// standSub is the whole sentence, drawn under the head only when the head's cut
// is not already all of it. A card that said the same words twice, once in ink
// and once in dim, would be spending a row on nothing ([taskSubtitleOf] follows
// the same rule).
func standSub(name, words string) string {
	words = strings.TrimSpace(words)
	if words == "" || strings.EqualFold(strings.TrimRight(words, "."), strings.TrimRight(name, ".")) {
		return ""
	}
	return words
}

// ── the proposal arriving ───────────────────────────────────────────────────

// proposeStanding draws the decision moment (session.EventStandingProposal).
//
// One question at a time is the engine's own serialization — its ask blocks the
// tool call that raised it — and a second card arriving anyway is not dropped:
// the older one settles as ended, because a question that can no longer be
// answered must stop looking like one.
func (a *app) proposeStanding(ev session.Event) {
	notice := ev.Standing
	if notice == nil {
		return
	}
	if a.stand != nil && !a.stand.settled() {
		a.stand.verdict = standExpiredWord
	}
	card := a.standingCardFor(*notice)
	a.stand = card
	a.closeLive()
	// The typed lists follow the draft, and the draft is now the correction
	// lane: a completion list left open under it would be answering keys that
	// belong to the question ([app.proposeTask] makes the same call).
	a.closeLists()
	a.closeSettings()
	a.entries = append(a.entries, entry{kind: entryStanding, turn: a.turn, stand: card})
	a.follow()
	a.touch()
}

// standingUpdate draws ONE DIM LINE about an item that already stands
// (session.EventStandingUpdate).
//
// It is never a card and never two rows. The quiet law is the whole of the
// design here: a routine that checked thirty mornings and found nothing has
// written nothing into this conversation, and the day it does have something to
// say it says it in one line that scrolls with everything else.
func (a *app) standingUpdate(ev session.Event) {
	notice := ev.Standing
	if notice == nil {
		return
	}
	word := strings.TrimSpace(notice.Update)
	if word == "" {
		return
	}
	a.closeLive()
	a.entries = append(a.entries, entry{kind: entryStanding, turn: a.turn, stand: &standingCard{
		item:   notice.Item,
		name:   standName(strings.TrimSpace(notice.Item.Words)),
		update: word,
		text:   strings.TrimSpace(notice.Text),
		glyph:  standUpdateGlyph(word),
	}})
	a.follow()
	a.touch()
}

// standUpdateGlyph is the mark one line of news leads with, and it is decided
// by WHAT HAPPENED rather than by what the item is now: an item that needs
// somebody wears the triangle even after it goes quiet again, because the line
// is a record of the moment it wrote.
func standUpdateGlyph(update string) string {
	switch update {
	case "needs-you":
		return homeAskGlyph
	case "paused", "stopped", "retired", "failed":
		return standOffGlyph
	}
	return standWaitGlyph
}

// standUpdateRow is one line of news, drawn.
//
//	◦ every Monday at 9 · set up
//	◦ every Monday at 9 · said: the weekly update is in notes/week-34.md
//	▲ keep main green · needs your look: the fix touches migrations
//	∙ remind me at 6 to leave · stopped
//
// FOUR SHAPES AND NO FIFTH. Everything an item can do maps onto one of them,
// and the mapping is here rather than in the engine because these are words a
// person reads: "retired" and "stopped" are the same news to whoever asked for
// the thing, and only one of them is a word anybody says out loud.
func standUpdateRow(pal palette, card *standingCard, width int) string {
	glyph := card.glyph
	if pal.ascii {
		switch glyph {
		case homeAskGlyph:
			glyph = homeAskASCII
		case standOffGlyph:
			glyph = standOffASCII
		default:
			glyph = standWaitASCII
		}
	}
	line := glyph + " " + card.name + " · " + standUpdateWord(card.update, card.text)
	return pal.dim(fit(line, width))
}

// standUpdateWord is the tail of that line: what happened, and the one sentence
// it carried when it carried one.
func standUpdateWord(update, text string) string {
	switch update {
	case "stood":
		return standSetWord
	case "fired":
		if text == "" {
			// AN EMPTY FIRING IS STILL A FIRING, and it says the smallest true
			// thing rather than an empty "said: ". The emptiness law applies to
			// the sentence and not to the event.
			return "ran"
		}
		return "said: " + text
	case "needs-you":
		if text == "" {
			return "needs your look"
		}
		return "needs your look: " + text
	case "paused":
		return "paused"
	case "resumed":
		return "going again"
	case "failed":
		if text == "" {
			return "stopped"
		}
		return "stopped: " + text
	default:
		return "stopped"
	}
}

// ── the keyboard ────────────────────────────────────────────────────────────

// awaitingStanding reports whether a standing card owns the answer lane.
func (a *app) awaitingStanding() bool { return a.stand != nil && !a.stand.settled() }

// The three chips of the proposal, and the two of the follow-up. They are
// indexes into [standChoiceWords] and [standWatchWords], and the digit a person
// presses is the index plus one.
const (
	standYes = iota
	standChange
	standOnce
)

var (
	standChoiceWords = [...]string{standYesWord, standChangeWord, standOnceWord}
	standWatchWords  = [...]string{standAlwaysWord, standWindowWord}
)

// standAnswerWords is the proposal's chips for one notice, and it READS THE
// ENGINE'S LIST rather than deciding anything.
//
// THE CARD DOES NOT ALWAYS HAVE THREE. A one-off reminder offers no `once, not
// standing`, because "do it once, now" for a line that was meant for six
// o'clock says the wrong thing at the wrong moment or says nothing at all
// (session's [StandingOptions] holds the whole law and the story behind it).
// The engine states which answers a card has on the notice; this turns that
// into the words this surface draws, in the same order.
//
// `change when` is on every row and is never in the engine's list: it is a
// request for the box under the card, and the list it comes from is the one
// home draws from, where there is no box.
func standAnswerWords(notice session.StandingNotice) []string {
	words := []string{standYesWord, standChangeWord}
	for _, option := range notice.Options {
		if option.Key == session.StandingOnceKey {
			return append(words, standOnceWord)
		}
	}
	if len(notice.Options) == 0 {
		// A NOTICE THAT NAMED NOTHING IS THE KIND'S FULL ROW. The zero value of
		// the field is "the engine did not narrow this", which is what every
		// card was before it existed.
		return append(words, standOnceWord)
	}
	return words
}

// standChips is the row of answers the card is currently asking with.
func (c *standingCard) chips() []string {
	if c.stage == standWatching {
		return standWatchWords[:]
	}
	if len(c.answers) > 0 {
		return c.answers
	}
	return standChoiceWords[:]
}

// offers reports whether one digit has a chip under it on this card. A digit
// that does not is NOT AN ANSWER: it belongs to the box, exactly as any other
// character does.
func (c *standingCard) offers(key string) bool {
	if c == nil {
		return false
	}
	at, ok := taskModelKey(key)
	return ok && at < len(c.chips())
}

// standingKey is the card's claim on the keyboard, and it is NOT modal — the
// box under it is the correction lane, exactly as the task proposal's box is the
// redirect lane (task.go's [app.taskKey] states the two tiers and why).
//
//	always      enter answers the focused chip · esc says no · ←/→ move
//	empty box   1, 2, 3 pick a chip outright
//
// The digits are given back the moment there is a sentence in the box, which is
// the guard the letters need there and the numbers need here: "9am on Mondays"
// begins with a 9, and "2 hours later" begins with a 2.
func (a *app) standingKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.awaitingStanding() {
		return nil, false
	}
	card := a.stand
	switch msg.String() {
	case "enter":
		return a.takeStanding(card.choice), true
	case "esc":
		// esc is the dismiss key everywhere on this surface, so it stays the
		// outright no. DURING THE FOLLOW-UP IT IS NOT A NO: the yes has been
		// given, and the only conservative reading of "get this off my screen"
		// there is the answer that installs nothing on the host.
		if card.stage == standWatching {
			return a.answerStanding(session.StandingAnswer{Approved: true, KeepWatch: standWatch(false)},
				standWindowDone, standWindowWord), true
		}
		a.answerStanding(session.StandingAnswer{}, standNoWord, "")
		return nil, true
	}
	if !a.input.empty() || a.pick.open {
		return nil, false
	}
	switch msg.String() {
	case "left":
		a.moveStanding(-1)
		return nil, true
	case "right":
		a.moveStanding(1)
		return nil, true
	}
	if card.typing {
		return nil, false
	}
	if at, ok := taskModelKey(msg.String()); ok {
		if at < len(card.chips()) {
			return a.takeStanding(at), true
		}
		// A DIGIT THE PROPOSAL DID NOT DRAW IS INERT, AND IS NOT TEXT EITHER.
		// The card owns 1, 2 and 3 while it is asking with an empty box — that
		// is the trade this lane already makes, and a correction cannot begin
		// with one of them whatever this line says. So `3` on a one-off
		// reminder, which has no third chip ([standAnswerWords]), does NOTHING:
		// letting it through would put a stray character in the box and turn
		// the `1` after it into a correction rather than a yes.
		if card.stage == standAsking && at < len(standChoiceWords) {
			return nil, true
		}
	}
	return nil, false
}

// standWatch is a bool on the heap, which is what
// [session.StandingAnswer.KeepWatch] is: a THIRD state — nobody was asked — is
// the whole reason that field is a pointer.
func standWatch(keep bool) *bool { return &keep }

// moveStanding walks the chips and STOPS at their ends rather than wrapping,
// which is [app.moveChoice]'s law and its reason: a cursor that reappeared at
// the far end would put "once, not standing" under a key pressed to reach yes.
func (a *app) moveStanding(delta int) {
	card := a.stand
	if card == nil || card.settled() {
		return
	}
	at := card.choice + delta
	switch {
	case at < 0:
		at = 0
	case at >= len(card.chips()):
		at = len(card.chips()) - 1
	}
	card.choice = at
	// Landing on "change when" is asking for the box, exactly as pressing 2 is.
	card.typing = card.stage == standAsking && at == standChange
	a.markStandStale(card)
	a.touch()
}

// takeStanding acts on one chip, whether a digit, an arrow's enter or a click
// asked for it.
//
// TWO ANSWERS, ONE CALL. A card that offers the watch question collects the yes
// and holds it ([standingCard.approved]) rather than resolving twice: the engine
// is waiting on ONE answer, and a surface that sent the approval and then the
// preference would have the second one ignored as late (ResolveStanding drops an
// id nobody is waiting on).
func (a *app) takeStanding(at int) tea.Cmd {
	card := a.stand
	if card == nil || card.settled() || at < 0 || at >= len(card.chips()) {
		return nil
	}
	card.choice = at
	if card.stage == standWatching {
		keep := at == 0
		word, verdict := standWindowWord, standWindowDone
		if keep {
			word, verdict = standAlwaysWord, standAlwaysDone
		}
		return a.answerStanding(session.StandingAnswer{Approved: true, KeepWatch: standWatch(keep)}, verdict, word)
	}
	text := strings.TrimSpace(a.input.String())
	switch at {
	case standOnce:
		a.answerStanding(session.StandingAnswer{Once: true}, standOnceDone, standOnceWord)
		return nil
	case standChange:
		if text == "" {
			// A REQUEST FOR THE BOX AND NOT AN ANSWER. The placeholder is already
			// down there saying what the box is for, so the chip takes the focus
			// and waits; the enter that follows carries the words.
			card.typing = true
			a.markStandStale(card)
			a.touch()
			return nil
		}
		return a.answerStanding(session.StandingAnswer{Change: text}, standChangedWord, standChangeWord)
	}
	// YES, AND A CORRECTION IN THE BOX IS STILL A CORRECTION. The two chips
	// converge the moment something is typed, which is the behaviour the task
	// card's bare lane always had.
	if text != "" {
		return a.answerStanding(session.StandingAnswer{Change: text}, standChangedWord, standChangeWord)
	}
	if card.offerWatch {
		// THE ONE-TIME QUESTION, ASKED IN PLACE. The yes is banked and the chips
		// become the follow-up's two; nothing has been sent yet.
		card.approved = true
		card.stage = standWatching
		card.choice = 0
		card.typing = false
		a.markStandStale(card)
		a.touch()
		return nil
	}
	return a.answerStanding(session.StandingAnswer{Approved: true}, standSetWord, standYesWord)
}

// answerStanding resolves the open card and annotates its row. It is the ONE
// place [standingAgent.ResolveStanding] is called from.
func (a *app) answerStanding(answer session.StandingAnswer, verdict, chosen string) tea.Cmd {
	card := a.stand
	if card == nil || card.settled() {
		return nil
	}
	card.verdict, card.answer = verdict, chosen
	card.typing = false
	if agent, ok := a.stander(); ok {
		agent.ResolveStanding(card.id, answer)
	}
	// The draft is cleared either way: the sentence in the box was about this
	// question, and leaving it there would make the next enter send it to the
	// model.
	cleared := !a.input.empty()
	a.input.reset()
	a.endRecall()
	a.closeLists()
	a.markStandStale(card)
	a.touch()
	if cleared {
		return a.edited()
	}
	return nil
}

// markStandStale drops the cached rows of the entry that draws this card.
func (a *app) markStandStale(card *standingCard) {
	for i := range a.entries {
		if a.entries[i].kind == entryStanding && a.entries[i].stand == card {
			a.entries[i].stale = true
			return
		}
	}
}

// tickStanding is the countdown, on the frame clock that is already turning.
//
// AT THE DEADLINE THE CARD STOPS ASKING AND DOES NOT ANSWER. The clock belongs
// to the engine, which declines on it (standing_contract.go); a surface that
// raced it would be a second authority on the same question.
func (a *app) tickStanding() {
	if !a.awaitingStanding() || a.stand.deadline.IsZero() {
		return
	}
	if a.now().Before(a.stand.deadline) {
		return
	}
	a.stand.verdict = standExpiredWord
	a.markStandStale(a.stand)
	a.touch()
}

// standingPress resolves a click on the card's chips row against the columns
// that row was drawn at, and reports whether it took the click.
//
// A press anywhere on that ROW is the row's, whether or not it landed on a chip
// — the same call [app.choicePress] makes, for the same reason: a click in the
// gap between two answers falling through to the block would make the row a
// place where missing costs you something.
func (a *app) standingPress(x, y int) (tea.Cmd, bool) {
	if a.roomOpen() || a.welcome.open {
		return nil, false
	}
	r, ok := a.rowAt(y)
	if !ok || r.hit != hitStandChoice || r.entry < 0 || r.entry >= len(a.entries) {
		return nil, false
	}
	card := a.entries[r.entry].stand
	if card == nil || card != a.stand || card.settled() {
		return nil, true
	}
	for _, span := range card.spans {
		if x >= span.from && x < span.to {
			return a.takeStanding(span.at), true
		}
	}
	return nil, true
}

// ── the card, drawn ─────────────────────────────────────────────────────────

// StandingCardRows draws one standing proposal, or one line of news.
//
//	╭─ ? ◦ every Monday at 9, draft the ───────────────────────────
//	│ every Monday at 9, draft the weekly update from the git log
//	│ when · Mondays at 9am
//	│ costs · about $0.02 a run, at most once a day
//	│ [ 1 yes, set it up ]  [ 2 change when ]  [ 3 once, not standing ]
//	│ ████████████░░░░░░░░  ends in 24s
//	╰──────────────────────────────────────────────────────────────
//
// IT IS PACKAGE-LEVEL AND EXPORTED ON PURPOSE. Home's errand box — a person
// typing "remind me at 6" at home and getting a card in the right pane — is a
// different lane's work, and the one thing that must not happen is a second
// card growing there. So the renderer is a function anything in this package can
// call with a width and a card, rather than a method buried in the transcript's
// own machinery, and the app it is handed supplies only the palette and the
// clock.
//
// A card is never given a width under four cells: below that there is no room
// for a corner and a glyph, and half a question is worse than none.
func StandingCardRows(a *app, card *standingCard, width int, sel bool) []string {
	if a == nil || card == nil || width < 4 {
		return nil
	}
	if card.news() {
		// ONE LINE, AND THE BLOCK IS THE LINE. No frame: a frame around a
		// sentence is furniture claiming to be structure, and this is not a
		// question.
		return []string{standUpdateRow(a.pal, card, width)}
	}
	// The hit targets are rebuilt with the rows that carry them, and cleared
	// first: a settled card has no chips, and a stale span is a click that
	// answers a question nobody is asking.
	card.choiceRow, card.spans = -1, nil
	head := a.standHead(card, width, sel)
	if card.settled() {
		return []string{head, a.standFoot(card, width)}
	}
	stem := a.pal.ask(a.blockStem())
	room := width - ansi.StringWidth(a.blockStem())
	out := []string{head}
	if card.stage == standWatching {
		// THE FOLLOW-UP REPLACES THE BANDS RATHER THAN JOINING THEM. The when
		// and the cost were read to decide the yes that has already been given;
		// leaving them under a different question would be a card asking one
		// thing and showing the evidence for another.
		out = append(out, stem+a.pal.ink(fit(standWatchAsk, room)))
	} else {
		if card.words != "" {
			for _, line := range wrap(card.words, room) {
				out = append(out, stem+a.pal.ink(line))
			}
		}
		for _, line := range a.standBands(card, room) {
			out = append(out, stem+line)
		}
	}
	chips, spans := a.standChips(card, ansi.StringWidth(a.blockStem()), room)
	card.choiceRow, card.spans = len(out), spans
	out = append(out, stem+chips)
	// A CARD WITH NO CLOCK DRAWS NO ROW WHERE THE CLOCK WOULD BE. The engine
	// holds a watched session's card open indefinitely, and the emptiness law
	// reaches a whole row: a bar with nothing to drain toward would be an
	// animation inventing a deadline, and a word standing in for one would be a
	// line spent saying that a thing is absent.
	if meter := a.standMeter(card, room); meter != "" {
		out = append(out, stem+meter)
	}
	return append(out, a.standFoot(card, width))
}

// standBands is the two bands that make this card a standing card rather than a
// task card: when it wakes, and what it costs.
//
// THE EMPTINESS LAW REACHES BOTH OF THEM. A notice that carried no words for
// one of them draws no row for it — a band reading `when ·` and nothing else is
// a label admitting it has nothing to label.
func (a *app) standBands(card *standingCard, width int) []string {
	var out []string
	if card.when != "" {
		word := standWhenTag + card.when
		if card.guessed {
			// THE GUESS IS SAID OUT LOUD, in the card's own sentence from
			// docs/AMBIENT.md. A cadence the model invented and the card stated
			// flatly is the one thing on this block a person cannot audit
			// afterwards, because it looks exactly like something they said.
			word += standGuessTag
		}
		for _, line := range wrap(word, width) {
			out = append(out, a.pal.dim(line))
		}
	}
	cost := card.cost
	if checked := standChecked(card.item.When.Kind); checked != "" {
		if cost == "" {
			cost = checked
		} else {
			cost += " · " + checked
		}
	}
	if cost != "" {
		for _, line := range wrap(standCostTag+cost, width) {
			out = append(out, a.pal.dim(line))
		}
	}
	return out
}

// standHead is the block's top: the corner, the question glyph, the item's own
// mark, the words, and the rule out to the frame's edge. It is [app.taskHead]'s
// row with one cell changed — the mark is the ITEM's glyph and not a task
// ident, because a standing item has no id-keyed hue and its state is the thing
// worth marking.
func (a *app) standHead(card *standingCard, width int, sel bool) string {
	paint, rule := a.standPaint(card), a.blockRule()
	corner := taskHeadCorner
	if a.pal.ascii {
		corner = taskCornerASCII
	}
	head := corner + " " + glyphAsk + " "
	mark := standGlyph(card.item, false, false, a.pal.ascii)
	if sel {
		mark = a.pal.bold(mark)
	}
	mark = a.pal.dim(mark) + " "
	title := fit(card.name, width-ansi.StringWidth(head)-3)
	line := paint(head) + mark
	if card.settled() {
		line += a.pal.muted(title)
	} else {
		line += a.pal.askBold(title)
	}
	if fill := width - ansi.StringWidth(head) - ansi.StringWidth(title) - 3; fill > 0 {
		line += paint(" " + strings.Repeat(rule, fill))
	}
	return line
}

// standPaint is the hue the frame takes: the question hue while it is a
// question, and the furniture grey the moment it is not ([app.blockPaint] says
// the whole of it).
func (a *app) standPaint(card *standingCard) func(string) string {
	if card.settled() {
		return a.pal.dim
	}
	return a.pal.ask
}

// standFoot closes the block — and, once the question is answered, IS the
// answer, which is [app.taskFoot]'s arrangement and its reason.
func (a *app) standFoot(card *standingCard, width int) string {
	paint, rule := a.standPaint(card), a.blockRule()
	corner := taskFootCorner
	if a.pal.ascii {
		corner = taskCornerASCII
	}
	if !card.settled() {
		if fill := width - ansi.StringWidth(corner); fill > 0 {
			return paint(corner + strings.Repeat(rule, fill))
		}
		return paint(corner)
	}
	word := card.verdict
	if card.answer != "" {
		word = card.answer + " · " + card.verdict
	}
	return paint(corner+" ") + a.pal.dim(fit(word, width-ansi.StringWidth(corner)-1))
}

// standChips draws the row of answers and reports what each occupies, in screen
// columns, so a click can be resolved to the answer under it.
//
// A chip that does not fit is DROPPED rather than truncated, which is the rule
// [app.taskChoices] and [app.taskModels] both follow for the same reason: half
// an answer is an answer somebody presses by mistake.
func (a *app) standChips(card *standingCard, left, width int) (string, []choiceSpan) {
	var line string
	var spans []choiceSpan
	at, end := left, left+width
	for i, word := range card.chips() {
		key := itoa(i + 1)
		chip := "[ " + key + " " + word + " ]"
		gap := 0
		if i > 0 {
			gap = 2
		}
		if at+gap+ansi.StringWidth(chip) > end {
			break
		}
		if gap > 0 {
			line += strings.Repeat(" ", gap)
			at += gap
		}
		line += a.taskModelChip(key, word, i == card.choice)
		spans = append(spans, choiceSpan{from: at, to: at + ansi.StringWidth(chip), at: i})
		at += ansi.StringWidth(chip)
	}
	return line, spans
}

// standMeter is the countdown, as a countdown — [app.taskMeter]'s bar with the
// other label on it. The bar drains toward NOTHING BEING SET UP, which is why
// the word beside it is `ends in`; a card that borrowed `auto-starts in` would
// be promising the opposite of what the engine does.
//
// AND A CARD WITH NO DEADLINE HAS NO METER AT ALL. It answers "" and the caller
// leaves the row out entirely.
func (a *app) standMeter(card *standingCard, width int) string {
	if card.deadline.IsZero() {
		// NOTHING, and the caller draws no row for it. A zero deadline is a
		// clock that is off: the card waits, and [app.tickStanding] never
		// expires it.
		return ""
	}
	left := card.deadline.Sub(a.now())
	word := standEndsWord + countdownFine(left)
	cells := taskMeterCells
	if room := width - ansi.StringWidth(word) - 2; cells > room {
		cells = room
	}
	if cells < 1 {
		return a.pal.dim(fit(word, width))
	}
	span := card.deadline.Sub(card.born)
	frac := 0.0
	if span > 0 {
		frac = float64(left) / float64(span)
	}
	return a.progress(frac, cells) + "  " + a.pal.dim(word)
}

// standAskHint is the hint slot's line for the question a card is asking right
// now: the follow-up's two, or the proposal's own row of digits.
func standAskHint(card *standingCard) string {
	if card == nil {
		return standProposalHint
	}
	if card.stage == standWatching {
		return standWatchHint
	}
	if len(card.chips()) <= standOnce {
		return standTwoHint
	}
	return standProposalHint
}

// standingAnimating reports whether the frame clock has to keep turning for the
// ambient side: a card's meter is draining toward the moment the engine
// declines it, or a firing is in flight and the status segment is breathing.
//
// THE COUNT IS THE CACHED ONE ([app.keepingCount]), which is what makes this
// safe to ask thirty times a second.
func (a *app) standingAnimating() bool {
	if a.awaitingStanding() && !a.stand.deadline.IsZero() {
		return true
	}
	_, firing := a.keepingCount()
	return firing
}

// standingCardFor is the one place a notice becomes a card, so the card the
// conversation draws and the card home's errand pane draws (homeexchange.go) are
// the same object read the same way.
func (a *app) standingCardFor(notice session.StandingNotice) *standingCard {
	words := strings.TrimSpace(notice.Item.Words)
	name := standName(words)
	return &standingCard{
		id:         notice.ID,
		item:       notice.Item,
		name:       name,
		words:      standSub(name, words),
		when:       strings.TrimSpace(notice.WhenWords),
		cost:       strings.TrimSpace(notice.CostWords),
		guessed:    notice.Guessed,
		offerWatch: notice.OfferWatch,
		answers:    standAnswerWords(notice),
		deadline:   notice.Deadline,
		born:       a.now(),
		// THE CARD OPENS ON "YES", which is [app.proposeTask]'s law and not a
		// claim about what silence does: a cursor parked on the answer that
		// undoes the proposal makes the ordinary answer the one you have to aim
		// at. Silence here still declines, and the meter says so.
		choice:    standYes,
		choiceRow: -1,
	}
}
