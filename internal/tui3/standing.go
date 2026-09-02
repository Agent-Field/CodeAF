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
// the same stem, the same walkable answer row, the same draining meter — with
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
	// answers is the proposal's chip row, in order, as THE ENGINE named it
	// ([session.StandingNotice.Options] via [standAnswerWords]). It is held on
	// the card rather than worked out at draw time because the keyboard asks
	// which digits belong to this question before any frame has been painted.
	answers []string
	// deadline is when the card stops asking, and zero when the engine is
	// holding it open indefinitely. born is when it arrived, which the meter
	// needs for the other end of its span.
	deadline, born time.Time

	// choice is which chip has the keyboard, and typing says the person asked
	// for the box so that the letters that would otherwise answer are text
	// again.
	choice int
	typing bool

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
	// The three numbered answers, and they carry their own keys the way the
	// models row does: a digit rather than an initial, because these answers
	// have nothing to pick an initial out of that a person would guess
	// ([taskModelChip] made the same trade for the same reason).
	//
	// EVERY ONE OF THEM NAMES ITS OUTCOME IN WORDS A STRANGER READS COLD. They
	// used to be written in this build's own vocabulary — `once, not standing`
	// assumes the reader has met the product noun "standing" and knows that a
	// thing which stands is a thing that keeps happening — and a person meeting
	// their first card said, in as many words, that they did not understand the
	// options. So the words say what will HAPPEN: it gets set up, you change
	// something about it, it happens once, or nothing does.
	standYesWord    = "yes, set it up"
	standChangeWord = "change when or where"
	standOnceWord   = "just once"
	// standNoWordChip is the way out, ON the card. It used to be `esc` and a `0`
	// named in the hint slot under the message box and nowhere else — a decline
	// a person had to already know about, which is the one trade
	// docs/DESIGN-LANGUAGE.md refuses by name: every chord keeps a visible,
	// clickable door beside it. So the decline is a chip like the others, under
	// [session.StandingNoKey], and it is the chip that is never dropped for want
	// of room ([app.pickRow]).
	standNoWordChip = "no"

	// The short spellings, for a card in a column with no room for the long ones
	// ([pickChoice]). A narrow pane says every answer briefly rather than some of
	// them fully, and the line under the row still says what the one under the
	// cursor would do.
	standYesShort    = "yes"
	standChangeShort = "change"
	standOnceShort   = "once"

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
	// written. IT NAMES BOTH THINGS THE CORRECTION MAY BE ABOUT, because the one
	// door covers both ([standChangeWord]): a lane that asked only for a time
	// would be the card offering to change the reach and then refusing to hear
	// about it.
	standChangeLane = "say the time or the place instead… (enter sends it, esc leaves it alone)"
	// standNoEscWord is how the way out is named in the hint slot IN A
	// CONVERSATION, where `0` and `esc` do exactly the same thing and each is
	// the only one of the two that exists somewhere: `esc` is the dismiss key
	// everywhere in a conversation, and `0` is the decline that also works from
	// home and from the errand pane, where esc is spent on something else
	// ([session.StandingNoKey]). The card itself draws the `0` as a chip, so
	// naming it here is a reminder rather than the only place it is said — and
	// the errand pane names the decline as `0` alone, because esc there hands
	// the keyboard back to the list (homeexchange.go's [exchangeHint]).
	//
	// IT IS BUILT FROM [standNoWordChip] rather than respelling `no`, which is
	// the same one-source-of-truth law the hint line itself is now built under
	// ([standHintFields]).
	standNoEscWord = "or esc, " + standNoWordChip
	// The verdicts a settled card keeps. They are sentences and not states,
	// because the row is read once, later, by somebody reconstructing what
	// happened.
	standSetWord     = "set up"
	standChangedWord = "you asked for something different"
	standOnceDone    = "done now, nothing kept"
	standNoWord      = "not set up"
	standExpiredWord = "ended · nothing was set up"

	// The frames [app.standSays] fills in. THEY ARE FRAMES AND NOT COPY: every
	// fact in the sentence a person reads comes off the proposal itself, and
	// what is written here is only the grammar that holds those facts together
	// and the part no field can supply — that a yes runs until somebody stops
	// it, that a once leaves nothing, that a change settles nothing yet.
	standSaysKeep   = "I'll keep doing this"
	standSaysUntil  = ", until you stop it"
	standSaysOnce   = "I'll do it now, this once — nothing is kept and nothing happens later"
	standSaysChange = "nothing is set up yet — type the time or the place you want, then enter"
	standSaysNo     = "nothing is set up and nothing happens later"
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
// ── AND THE STORE'S TWO MARKS ARE RE-SPELLED FOR THIS SURFACE ───────────────
//
// [standing.Item.Glyph] answers WHICH STATE a row is in, and that answer is the
// store's to give: every surface has to agree about it, including the resident,
// which is a different product in the same repository and draws the same rows.
// WHICH CHARACTER stands for that state is a question about a SCREEN, and this
// screen's answer is the design's ([homeAskGlyph] and [homeLiveGlyph] carry the
// reasoning and the owner's signature).
//
// So the two are translated here rather than changed there. Reaching into
// internal/standing to spell `?` would have moved the resident's glyphs too —
// CLAUDE.md forbids exactly that kind of travel between the two products — and
// the ASCII tier was already applied at this seam for the same reason: what a
// terminal can draw is not the store's business either.
func standGlyph(item standing.Item, running, news bool, ascii bool) string {
	glyph := standSurfaceGlyph(item.Glyph(running))
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

// standSurfaceGlyph is that translation, and it is the ONE table where the
// store's alphabet and this surface's meet. The store's own comment names its
// four ([standing.Item.Glyph]); the two that have a different character here are
// listed, and everything else passes through untouched because the two alphabets
// agree about it.
func standSurfaceGlyph(stored string) string {
	switch stored {
	case standStoreAskGlyph:
		return homeAskGlyph
	case standStoreLiveGlyph:
		return homeLiveGlyph
	}
	return stored
}

// The two characters internal/standing writes for the two states this surface
// re-spells. They are quoted here rather than reached for because the store does
// not hand its alphabet out as constants, and a literal that drifted from it
// would leave the translation silently doing nothing —
// TestThePlaceMarksAreTheDesignsOwn is what would catch that.
const (
	standStoreAskGlyph  = "▲"
	standStoreLiveGlyph = "●"
)

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

// standBackgroundWord is the update the engine sends when the first thing that
// ever stands turns this machine's background checks on
// (internal/session's standingBackgroundUpdate). It carries the whole sentence
// in its text and draws as that sentence and nothing else.
const standBackgroundWord = "background"

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
	if card.update == standBackgroundWord {
		// THE ONE ROW WITH NO GLYPH AND NO NAME IN FRONT OF IT. This line is not
		// news about the item — it is the machine saying what it just switched
		// on for the person and where the switch is — and leading it with the
		// item's own mark would file a fact about their laptop as one more thing
		// a reminder did.
		return pal.dim(fit(card.text, width))
	}
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
		// The word the standing page's own receipt uses, said once
		// (standingpage.go): one event read by a person in two places may not
		// be two different words.
		return standResumedWord
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

// The three numbered chips of the proposal. They are indexes into
// [standChoiceWords], and the digit a person presses is the index plus one. The
// way out sits after them and is NOT one of them — see [standingCard.declineAt].
const (
	standYes = iota
	standChange
	standOnce
)

var standChoiceWords = [...]string{standYesWord, standChangeWord, standOnceWord}

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
	if len(c.answers) > 0 {
		return c.answers
	}
	return standChoiceWords[:]
}

// declineAt is where the way out sits on the drawn answer row: after every
// numbered chip, always last.
//
// IT IS A POSITION AND NOT A FOURTH DIGIT. The numbered chips are keyed by
// their place in the row — 1, 2, 3 — so a decline taking `4` would move the day
// a card drew one chip fewer, and the hand that learned the keys on a watch
// would decline a reminder. Its key is `0`, off both ends of that numbering
// ([session.StandingNoKey]); this is only where the cursor and the pointer find
// it.
func (c *standingCard) declineAt() int { return len(c.chips()) }

// picks is how many places the cursor has to stand on: every numbered chip, and
// the way out after them.
func (c *standingCard) picks() int { return c.declineAt() + 1 }

// answerKey is the key one drawn position is answered with, which is what lets
// a click on the errand pane's copy of this row reach the same answer a
// keystroke would ([app.answerCard]).
func (c *standingCard) answerKey(at int) string {
	if at == c.declineAt() {
		return session.StandingNoKey
	}
	return itoa(at + 1)
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
//	empty box   1, 2, 3 pick a chip outright · 0 says no
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
		// esc is the dismiss key everywhere on this surface, so it is the
		// outright no here.
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
	// THE DECLINE IS A KEY AND NOT A CHIP, and it is read here rather than off
	// the chip row because the row is numbered by position: `0` is deliberately
	// off both ends of that numbering so that it means the same thing on a card
	// with three chips and a card with two ([session.StandingNoKey]). It is
	// `esc` said with a digit, for the surfaces that have no esc to spare, and
	// it is claimed under exactly the guards the digits are claimed under — an
	// empty box, no list open, no correction being typed — because "0900" is a
	// when somebody might write.
	if msg.String() == session.StandingNoKey {
		a.answerStanding(session.StandingAnswer{}, standNoWord, "")
		return nil, true
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
		if at < len(standChoiceWords) {
			return nil, true
		}
	}
	return nil, false
}

// moveStanding walks the chips and STOPS at their ends rather than wrapping,
// which is [app.moveChoice]'s law and its reason: a cursor that reappeared at
// the far end would put the decline under a key pressed to reach yes.
func (a *app) moveStanding(delta int) {
	card := a.stand
	if card == nil || card.settled() {
		return
	}
	at := card.choice + delta
	switch {
	case at < 0:
		at = 0
	case at >= card.picks():
		at = card.picks() - 1
	}
	card.choice = at
	// Landing on "change when" is asking for the box, exactly as pressing 2 is.
	card.typing = at == standChange
	a.markStandStale(card)
	a.touch()
}

// takeStanding acts on one chip, whether a digit, an arrow's enter or a click
// asked for it.
//
// ONE QUESTION AND ONE CALL. The card used to ask a second thing after a yes —
// keep checking when no window is open? — and it no longer asks anybody:
// background checks go on with the first item that stands and the switch is a
// settings row (internal/session's standingBackgroundOn). So an answer here
// resolves the card and nothing follows it.
func (a *app) takeStanding(at int) tea.Cmd {
	card := a.stand
	if card == nil || card.settled() || at < 0 || at >= card.picks() {
		return nil
	}
	card.choice = at
	text := strings.TrimSpace(a.input.String())
	if at == card.declineAt() {
		// THE WAY OUT, AND IT IS THE SAME ANSWER `esc` AND `0` ALREADY SENT: a
		// zero [session.StandingAnswer], which the engine reads as nothing being
		// set up. A sentence left in the box is not a correction here — the
		// person pressed the answer that says they want none of this — so it is
		// cleared with the card exactly as every other answer clears it.
		return a.answerStanding(session.StandingAnswer{}, standNoWord, "")
	}
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
		return a.answerStanding(session.StandingAnswer{Change: a.spoken(text)}, standChangedWord, standChangeWord)
	}
	// YES, AND A CORRECTION IN THE BOX IS STILL A CORRECTION. The two chips
	// converge the moment something is typed, which is the behaviour the task
	// card's bare lane always had.
	if text != "" {
		return a.answerStanding(session.StandingAnswer{Change: a.spoken(text)}, standChangedWord, standChangeWord)
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
	a.pastes = nil
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
//	│ where · for this project
//	│ costs · about $0.02 a run, at most once a day
//	│ [ 1 yes, set it up ]  [ 2 change when or where ]  [ 3 just once ]  [ 0 no ]
//	│ I'll keep doing this Mondays at 9am, for this project, until you stop it
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
	if card.words != "" {
		for _, line := range wrap(card.words, room) {
			out = append(out, stem+a.pal.ink(line))
		}
	}
	for _, line := range a.standBands(card, room) {
		out = append(out, stem+line)
	}
	chips, spans := a.standChips(card, ansi.StringWidth(a.blockStem()), room)
	card.choiceRow, card.spans = len(out), spans
	out = append(out, stem+chips)
	// AND WHAT THE ANSWER UNDER THE CURSOR WOULD DO, in one dim line built out
	// of this very proposal ([app.standSays]). It is what makes walking the row
	// a way of reading the question rather than a way of guessing at it.
	for _, line := range wrap(a.standSays(card), room) {
		out = append(out, stem+a.pal.dim(line))
	}
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
//
// AND A RULE DRAWS NEITHER, whatever the notice carried. A hold has no moment,
// no rhythm and no condition, so a `when ·` band under one would be the card
// reading a cadence into the word "always"; and it never wakes, so it never runs
// a probe, buys a judgment or launches work, and a `costs ·` band would be
// asking somebody to weigh a figure nothing can ever draw on. What is left is
// the person's sentence and how far it reaches, which is the whole of what they
// are agreeing to.
func (a *app) standBands(card *standingCard, width int) []string {
	var out []string
	if card.when != "" && card.item.When.Kind != standing.WhenHold {
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
	// AND HOW FAR IT REACHES, ALWAYS SAID, which is the one band here that is
	// never dropped. The other two can be empty because a notice may carry no
	// words for them; a reach cannot — [standing.Item.Level] resolves the zero
	// value to a real answer — and an order whose reach was not on the card is
	// an order somebody agreed to without knowing where it applies
	// (docs/STANDING-ORDERS.md: the card always names it before anything
	// stands). It is drawn in the person's own words and never the field's
	// ([standLevelWord]).
	for _, line := range wrap(standWhereTag+standLevelWord(card.item.Level()), width) {
		out = append(out, a.pal.dim(line))
	}
	// AND A RULE HAS NO COST BAND AT ALL. A hold never wakes, so it never runs a
	// probe, never buys a judgment and never launches work ([standing.Item.Spends]
	// is where that is decided) — and the emptiness law reaches a whole band: an
	// allowance quoted on a card for something that can never draw on it is a
	// figure the person has to weigh and nothing will ever spend.
	cost := ""
	if card.item.Spends() {
		cost = card.cost
		if checked := standChecked(card.item.When.Kind); checked != "" {
			if cost == "" {
				cost = checked
			} else {
				cost += " · " + checked
			}
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
// THE ROW IS THE SHARED ONE (pickrow.go) and this function is only what belongs
// to this card: which answers there are, which one the keyboard is on, and which
// of them is the way out. A chip that does not fit is DROPPED rather than
// truncated — half an answer is an answer somebody presses by mistake — and the
// decline is exempt from that, which is why it is named as the row's `keep`.
func (a *app) standChips(card *standingCard, left, width int) (string, []choiceSpan) {
	return a.pickRow(card.row(), card.choice, card.declineAt(), left, width)
}

// row is every place on the answer row, in the order it is drawn: the numbered
// chips the engine named, then the way out.
func (c *standingCard) row() []pickChoice {
	words := c.chips()
	out := make([]pickChoice, 0, len(words)+1)
	for i, word := range words {
		out = append(out, pickChoice{key: itoa(i + 1), word: word, short: standShortWord(word)})
	}
	return append(out, pickChoice{key: session.StandingNoKey, word: standNoWordChip})
}

// standShortWord is one answer's brief spelling. The decline has none — `no` is
// as short as a word gets.
func standShortWord(word string) string {
	switch word {
	case standYesWord:
		return standYesShort
	case standChangeWord:
		return standChangeShort
	case standOnceWord:
		return standOnceShort
	}
	return ""
}

// standSays is the one line under the answers: what the answer the cursor is on
// will ACTUALLY DO, said in the person's own terms.
//
// ── IT IS BUILT FROM THE PROPOSAL AND NEVER WRITTEN DOWN PER CARD ──
//
// The sentence reads out the very fields the engine is about to act on — the
// person's own words, the cadence in the model's own phrasing, the reach the
// `where` band names — so it cannot drift from what happens: there is no copy
// here beyond the frame each answer needs, and a card whose reach changed says
// the new reach without anybody editing a string. That is the whole reason a
// line like this is worth a row. A canned "this will be set up" would be a
// sentence that stayed true while the card underneath it changed.
//
// ── AND IT IS THE ANSWER'S CONSEQUENCE, NOT THE CARD'S CONTENTS ──
//
// The bands above already state the proposal. This line states what PRESSING
// THIS does, which is the thing the bands cannot say: that a yes goes on until
// somebody stops it, that a once leaves nothing behind, that a change settles
// nothing yet.
func (a *app) standSays(card *standingCard) string {
	if card == nil {
		return ""
	}
	if card.choice == card.declineAt() {
		return standSaysNo
	}
	switch card.choice {
	case standChange:
		return standSaysChange
	case standOnce:
		return standSaysOnce
	}
	// THE YES, AND IT IS ASSEMBLED RATHER THAN CHOSEN. A rule has no cadence to
	// read out — nothing wakes it, it is simply true from now on (standBands
	// drops its `when` band for the same reason) — so the clause is left out
	// rather than replaced by a word standing in for one.
	where := standLevelWord(card.item.Level())
	if card.item.When.Kind == standing.WhenHold || card.when == "" {
		return standSaysKeep + ", " + where + standSaysUntil
	}
	return standSaysKeep + " " + card.when + ", " + where + standSaysUntil
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

// ── THE HINT SLOT, READ OFF THE CHIPS ───────────────────────────────────────

// standHintFields is the hint slot's line as ranked facts: ONE FIELD PER CHIP
// THE CARD ACTUALLY DREW, in the order it drew them, under the digit that takes
// it.
//
// THE LINE IS DERIVED AND IS NEVER WRITTEN DOWN. It used to be written down —
// twice here and a third time in the errand pane — and the third copy named `3
// just once` under a one-off reminder's card, which draws no such chip
// ([standAnswerWords] says why it does not). A sentence somebody typed cannot
// know what was drawn; a sentence built from [standingCard.row] cannot name an
// answer that is absent, because an absent answer is not in the list it walks.
// That is the one-source-of-truth law applied to a sentence rather than to a
// number, and it is the same law the chips themselves are drawn under: the row
// this reads is the row [app.standChips] paints (#189).
//
// AND EVERY FIELD CARRIES ITS BRIEF SPELLING, which is the chip's own — so a
// narrow frame gives up a whole word rather than half of one, through the
// shared fitter (rowfit.go): `2 change when or where` becomes `2 change`, and
// no verb is ever left cut. rowfit's law 2 is the constrained-space rule and
// this line obeys it like every other row on the surface.
//
// decline is how the way out is named, and it belongs to the caller because it
// differs by pane: a conversation says `0 or esc, no` ([standNoEscWord]) and the
// errand pane says `0 no`, since esc there hands the keyboard back to the list
// rather than declining anything ([app.answerCard]).
func standHintFields(card *standingCard, decline string) []rowField {
	row := card.row()
	fields := make([]rowField, 0, len(row))
	for _, choice := range row {
		word, short := standHintWord(choice.word), choice.short
		if choice.key == session.StandingNoKey {
			// The way out is named in the caller's words rather than the chip's,
			// and its brief spelling is the chip's own — `no` is as short as a
			// decline gets.
			word, short = decline, standNoWordChip
		}
		field := rowField{full: choice.key + " " + word}
		if short != "" && short != word {
			field.short = choice.key + " " + short
		}
		fields = append(fields, field)
	}
	return fields
}

// standHintWord is how one drawn chip is NAMED in the hint slot: its word up to
// the first comma.
//
// A CHIP'S WORD IS AN ANSWER AND THE HINT IS A LIST OF ANSWERS, joined with `·`
// and read left to right. `yes, set it up` dropped into that list reads as two
// entries, because the comma inside it is the same break the separator is — and
// the half before the comma is the whole answer anyway. What follows it is the
// chip telling a person what pressing `1` DOES, which is the chip's work and
// the answer line's under it, not this reminder's. Every other answer a standing
// card draws has no comma in it and is named whole.
func standHintWord(word string) string {
	if at := strings.IndexByte(word, ','); at >= 0 {
		return strings.TrimSpace(word[:at])
	}
	return word
}

// standAskHint is the hint slot's line for the question a card is asking right
// now in a conversation: the digits the card drew, and never one more.
func standAskHint(card *standingCard) string {
	return rowAll(standHintFields(standHintCard(card), standNoEscWord))
}

// standAskHintShort is that same line with every answer in its brief spelling —
// the rung the legend offers when the frame cannot hold the long one
// (steer.go's [app.hintShorter], render.go's [app.legend]). The slot is all or
// nothing there, so the choice on a narrow frame is between every answer said
// briefly and no answer named at all.
func standAskHintShort(card *standingCard) string {
	return rowShort(standHintFields(standHintCard(card), standNoEscWord))
}

// standHintCard is the card the hint is read off, and a nil one is the full
// row — the kind's whole set of answers, which is what a surface asking for the
// line with no card up is asking about ([standingCard.chips] takes the same
// reading of a card whose notice narrowed nothing).
func standHintCard(card *standingCard) *standingCard {
	if card == nil {
		return &standingCard{}
	}
	return card
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
		id:       notice.ID,
		item:     notice.Item,
		name:     name,
		words:    standSub(name, words),
		when:     strings.TrimSpace(notice.WhenWords),
		cost:     strings.TrimSpace(notice.CostWords),
		guessed:  notice.Guessed,
		answers:  standAnswerWords(notice),
		deadline: notice.Deadline,
		born:     a.now(),
		// THE CARD OPENS ON "YES", which is [app.proposeTask]'s law and not a
		// claim about what silence does: a cursor parked on the answer that
		// undoes the proposal makes the ordinary answer the one you have to aim
		// at. Silence here still declines, and the meter says so.
		choice:    standYes,
		choiceRow: -1,
	}
}
