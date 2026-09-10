package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
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
	// deadline is when the card stops asking, and zero when the engine is
	// holding it open indefinitely. born is when it arrived, which the meter
	// needs for the other end of its span.
	deadline, born time.Time

	// THE ANSWERS ARE NOT HERE ANY MORE. A chip row, the cursor on it, the
	// columns each chip landed in and the digits that took them were all this
	// card's own until the question block took every decision this engine hands
	// a person (docs/design/questions/DESIGN.md). What is left below is the head,
	// the bands, the meter and the news line — which are what this card SHOWS,
	// as against what it ASKS.

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
	// The verdicts a settled card keeps. They are sentences and not states,
	// because the row is read once, later, by somebody reconstructing what
	// happened.
	standSetWord     = "set up"
	standChangedWord = "you asked for something different"
	standOnceDone    = "done now, nothing kept"
	standNoWord      = "not set up"
	standExpiredWord = "ended · nothing was set up"

	// WHAT EACH ANSWER COSTS, one clause apiece, drawn beside its own word on
	// the block's card ([session.AnswerOption.Consequence]).
	//
	// THEY SAY WHAT THE BANDS CANNOT AND NOTHING THE BANDS ALREADY SAY. The line
	// these replaced was assembled out of the proposal — "I'll keep doing this
	// Mondays at 9am, for this project, until you stop it" — and read out the
	// cadence and the reach, which is exactly what the `when ·` and `where ·`
	// bands two rows above are for. Under a card that draws both, that sentence
	// was the two-renderings defect one size smaller. What no band can say is how
	// LONG each answer lasts, so that is what is left.
	standYesCost    = "it keeps happening until you stop it"
	standOnceCost   = "it happens now, and nothing is kept"
	standNoCost     = "nothing happens, now or later"
	standChangeCost = "say the time or the place you want instead"
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
	// AND THE QUESTION IS RAISED FROM HERE TOO, dressed with the lane's own hand
	// on an answer. It arrives on the questions lane as well and the block
	// replaces by token, so whichever gets here first draws and the second is the
	// same decision rather than a second one — consent.go's and task.go's own
	// bargain, kept.
	a.raiseQuestion(a.standingShown(card, *notice))
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
//	▲ keep main green · your call: the fix touches migrations
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
		// ONE WORD FOR ONE READING, AND IT IS THE SURFACE'S ONE WORD. `needs your
		// look` was this file's own name for the fact every task row now calls
		// `your call` (tasktier.go's [tierYourCallWord]), and a standing item that
		// wanted somebody is the same news as a piece of work that does.
		if text == "" {
			return tierYourCallWord
		}
		return tierYourCallWord + ": " + text
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

// ── the question ────────────────────────────────────────────────────────────

// awaitingStanding reports whether a standing card is still asking.
func (a *app) awaitingStanding() bool { return a.stand != nil && !a.stand.settled() }

// standingShown is one proposal as the block holds it: the engine's own question
// object, plus the one thing it cannot carry — what this program does about an
// answer, which is the row in the transcript keeping what was decided
// ([app.standingAnswered]).
//
// THE DOOR IS THE ONE DOOR AND NOT THIS LANE'S OWN. An answer given here goes
// through [session.Agent.ResolveQuestion], which reads the lane off the answer
// and hands it to [standingAgent.ResolveStanding] — so a card answered from the
// block, from home's answer band and from another window all take one road and
// leave one record.
func (a *app) standingShown(card *standingCard, notice session.StandingNotice) questionShown {
	id := card.id
	return questionShown{
		question: a.standingQuestion(card, notice),
		answered: func(answer session.Answer) session.Answer {
			return a.standingAnswered(id, answer)
		},
	}
}

// standingQuestion is the proposal as the object every surface draws, and it is
// the engine's own builder said again (session's [Agent.standingAsk]).
//
// The two must stay ONE SENTENCE, for consent.go's reason: the block keys a
// question by its lane and its id, so this one and the one that arrives on the
// questions lane a moment later are the SAME question, and two builders that
// drifted would make them two — one replacing the other on screen while somebody
// was part-way through reading it. The head and the reason are therefore the
// engine's own exported constants.
//
// THE ANSWERS ARE THE ENGINE'S LIST WITH THE CARD'S WORDS ON THEM. Which answers
// a card has is a decision the engine already made per item
// ([session.StandingOptions] — a one-off reminder offers no `just once` and says
// why at length); what each one COSTS is a sentence about the arrangement in
// front of the person, which is this surface's to write. It is the same seam
// consent.go keeps for its widening answer.
//
// AND IT ASKS FOR WORDS. `change when or where` was a chip that took the box
// rather than an answer, and on the block that is what `[c] change` is
// everywhere ([questionOwnsBox] is what lets the sentence land here instead of
// in the conversation). [session.InputText] is how the question says so.
func (a *app) standingQuestion(card *standingCard, notice session.StandingNotice) session.Question {
	options := notice.Options
	if len(options) == 0 {
		// A NOTICE THAT NARROWED NOTHING IS THE KIND'S WHOLE ROW, which is the
		// reading [standingCard.chips] took of the same zero value: the field is
		// newer than the lane, and its absence means "the engine did not narrow
		// this" rather than "there are no answers".
		options = session.StandingOptions(card.item)
	}
	dressed := make([]session.AnswerOption, 0, len(options))
	for _, option := range options {
		option.Label, option.Consequence = standAnswerWord(option.Key), standAnswerCost(option.Key)
		dressed = append(dressed, option)
	}
	return session.Question{
		ID:      card.id,
		Kind:    session.QuestionStanding,
		Ask:     session.AskChoice,
		Form:    session.FormCard,
		Asker:   session.Asker{Kind: session.AskerModel},
		Head:    session.StandingAskLead + strings.TrimSpace(card.item.Words),
		Reason:  session.StandingAskReason,
		Subject: session.SubjectRef{Kind: session.SubjectOrder, ID: card.id, Name: strings.TrimSpace(card.item.Words)},
		Options: dressed,
		Stakes:  session.StakesReversible,
		Scope:   []session.AnswerScope{session.ScopeOnce, session.ScopeAlways},
		// THE CARD'S BOX IS ITS CORRECTION LANE and always was: "make it 2pm" is
		// a real answer to this question, and the engine reads a standing answer
		// that carries words alone as a correction to re-propose on
		// (session's applyToLane).
		Input:    session.InputShape{Kind: session.InputText, Prompt: standChangeCost},
		Deadline: notice.Deadline,
		Asked:    a.now(),
	}
}

// standAnswerWord is what one answer is CALLED on this card, by the key that
// takes it.
//
// EVERY ONE OF THEM NAMES ITS OUTCOME IN WORDS A STRANGER READS COLD, which is
// why the card says more than the kind's own list does ([session.AnswerOptions]
// spells them `yes`, `just once`, `not set up` for home's chip row, where the
// column is scarce). A person meeting their first card said, in as many words,
// that they did not understand the options; these say what will HAPPEN.
func standAnswerWord(key string) string {
	switch key {
	case standYesKey:
		return standYesWord
	case session.StandingOnceKey:
		return standOnceWord
	case session.StandingNoKey:
		return standNoWordChip
	}
	return ""
}

// standAnswerCost is what one answer costs, by the key that takes it. A key the
// list does not carry has no clause, which is the emptiness law rather than a
// default.
func standAnswerCost(key string) string {
	switch key {
	case standYesKey:
		return standYesCost
	case session.StandingOnceKey:
		return standOnceCost
	case session.StandingNoKey:
		return standNoCost
	}
	return ""
}

// standYesKey is the digit that sets a standing card up. It is `1` on every
// question this surface asks and is named here so the clause beside it is not
// keyed off a literal ([standAnswerCost]).
const standYesKey = "1"

// standingAnswered is the lane's own hand on an answer: the card in the
// transcript keeps what was decided, in the words the row reads afterwards.
//
// IT DECIDES NOTHING. The answer has already been given by the time this runs
// and is on its way to [session.Agent.ResolveQuestion]; what happens here is the
// row being annotated, which is the one thing about a standing card that
// outlives the question ([app.answerStanding] is the shared half).
func (a *app) standingAnswered(id uint64, answer session.Answer) session.Answer {
	card := a.stand
	if card == nil || card.id != id || card.settled() {
		return answer
	}
	verdict, chosen := standVerdictOf(card, answer)
	a.settleStanding(card, verdict, chosen)
	return answer
}

// standVerdictOf is what a settled row says, read off the answer that settled
// it: the verdict sentence, and the answer's own word beside it.
//
// THE WORDS ARE THE ANSWER'S OWN ([standAnswerWord]), which is what keeps the
// row a person reads back and the row they answered from becoming two accounts
// of one decision: `yes, set it up · set up` is the chip they pressed and what
// it came to, in that order.
func standVerdictOf(card *standingCard, answer session.Answer) (string, string) {
	key := answer.FirstKey()
	if key == "" && strings.TrimSpace(answer.Words()) != "" {
		// A CORRECTION IS NOT A YES. The person said what is wrong with the
		// arrangement and the model re-proposes on those words; nothing stands
		// yet, and the row has to say so.
		return standChangedWord, standChangeWord
	}
	word := standAnswerWord(key)
	switch key {
	case session.StandingOnceKey:
		return standOnceDone, word
	case session.StandingNoKey:
		// THE DECLINE KEEPS NO WORD BESIDE ITS VERDICT. `not set up · no` is the
		// same fact twice, and the verdict is the half that says what happened.
		return standNoWord, ""
	case standYesKey:
		return standSetWord, word
	}
	return standNoWord, ""
}

// answerStanding resolves the open card from a door that is not the block —
// home's answer band, answering this window's own card with the engine's own
// mapping (homeband_answer.go). It is the ONE other place
// [standingAgent.ResolveStanding] is called from.
func (a *app) answerStanding(answer session.StandingAnswer, verdict, chosen string) tea.Cmd {
	card := a.stand
	if card == nil || card.settled() {
		return nil
	}
	if agent, ok := a.stander(); ok {
		agent.ResolveStanding(card.id, answer)
	}
	a.dropStandingQuestion(card.id)
	return a.settleStanding(card, verdict, chosen)
}

// settleStanding writes what was decided onto the row and clears the box the
// answer may have been typed into.
func (a *app) settleStanding(card *standingCard, verdict, chosen string) tea.Cmd {
	card.verdict, card.answer = verdict, chosen
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

// dropStandingQuestion takes this proposal's question off the block when the
// answer came from somewhere that is not the block: home's answer band, or the
// deadline the engine declines on. Nothing is resolved here — whatever called it
// has already done that, or nothing is being decided at all.
func (a *app) dropStandingQuestion(id uint64) {
	for _, open := range a.questions {
		if open.question.Kind != session.QuestionStanding || open.question.ID != id {
			continue
		}
		a.closeQuestion(open, session.Answer{})
		return
	}
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
	a.dropStandingQuestion(a.stand.id)
	a.markStandStale(a.stand)
	a.touch()
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
	// THE ANSWERS ARE NOT DRAWN HERE. They are the question, and every question
	// this engine hands a person is drawn once, above the box, by the block
	// (question.go). What is left on this card is what it SHOWS — the words, the
	// bands, and the meter draining toward the moment the engine declines it.
	//
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
	head := corner + " " + a.icon(tokens.GNeedsHuman) + " "
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
	cells := standMeterCells
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
		deadline: notice.Deadline,
		born:     a.now(),
	}
}

// standMeterCells is the meter's widest. Twenty cells is a bar a person reads as
// a proportion; past that it is a progress dialog, and this surface does not
// have those.
const standMeterCells = 20
