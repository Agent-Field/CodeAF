package tui3

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
)

// ── THE AMBIENT SIDE, AS A PERSON MEETS IT ──────────────────────────────────
//
// Every test below asserts the FACT the feature exists for — what is on the
// screen, what reaches the engine, what the store is asked to write — rather
// than the shape of the code under it (standing.go, homestanding.go).

// standFake is a session that can be answered about a standing item. It embeds
// the scripted agent every other test here runs against, because the standing
// contract is a widening of that session and not a different one.
type standFake struct {
	*fakeAgent
	answered []standReply
	replaced []session.Answer
}

type standReply struct {
	id     uint64
	answer session.StandingAnswer
}

func (f *standFake) ResolveStanding(id uint64, answer session.StandingAnswer) {
	f.answered = append(f.answered, standReply{id: id, answer: answer})
}

func (f *standFake) ReplaceQuestion(_ context.Context, answer session.Answer) (<-chan session.Event, error) {
	f.replaced = append(f.replaced, answer)
	events := make(chan session.Event)
	close(events)
	return events, nil
}

// ResolveQuestion is the engine's ONE DOOR in miniature: it reads the lane off
// the answer and hands it to that lane's own resolver, which is what
// [session.Agent.applyToLane] does for a real engine. The card's answers go
// through it now (standing.go), so a fake that could only be told
// ResolveStanding would be a session no key on the block could reach.
func (f *standFake) ResolveQuestion(answer session.Answer) error {
	if answer.Kind != session.QuestionStanding {
		return nil
	}
	if words := strings.TrimSpace(answer.Words()); words != "" && answer.FirstKey() == "" {
		// A STANDING CARD ANSWERED IN WORDS IS A CORRECTION (session's
		// applyToLane says so in full).
		f.ResolveStanding(answer.ID, session.StandingAnswer{Change: words})
		return nil
	}
	action, ok := session.AnswerFromKey(session.QuestionStanding, answer.FirstKey())
	if !ok {
		return errors.New("no answer under that key")
	}
	f.ResolveStanding(answer.ID, action.Standing)
	return nil
}

// standApp is a surface that can be asked about a standing item, with a pinned
// clock over it: a countdown cannot be tested by waiting four seconds.
func standApp(t *testing.T) (*app, *standFake, func(time.Duration)) {
	t.Helper()
	agent := &standFake{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(agent)
	a.width, a.height = 200, 24
	now := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	return a, agent, func(d time.Duration) { now = now.Add(d) }
}

// standItem is one plausible item, as the engine would carry it on a card.
func standItem() standing.Item {
	return standing.Item{
		ID:        "abc",
		Words:     "every Monday at 9, post the standup note from the git log",
		Workspace: "/tmp/lab",
		When:      standing.When{Kind: standing.WhenEvery, Words: "Mondays at 9am", Every: "168h"},
		Does:      standing.Action{Kind: standing.ActionSay, Say: "standup"},
		Rails:     standing.Rails{PerRunUSD: 0.05, MaxPerDay: 1},
		Status:    standing.StatusActive,
	}
}

// standProposal is one EventStandingProposal, as the engine sends it.
func standProposal(a *app, notice session.StandingNotice) session.Event {
	if notice.Item.ID == "" {
		notice.Item = standItem()
	}
	if notice.ID == 0 {
		notice.ID = 11
	}
	return session.Event{Kind: session.EventStandingProposal, Standing: &notice}
}

// standText is the conversation as a reader sees it, at the width the transcript
// actually gets.
func standText(a *app) string {
	var out []string
	for _, r := range a.visible(a.bodyWidth()) {
		out = append(out, plain(r.text))
	}
	return strings.Join(out, "\n")
}

// THE TWO BANDS ARE WHAT MAKES IT A STANDING CARD. A task proposal says what the
// work is; this says that, and WHEN it wakes and WHAT IT COSTS — the two facts
// a person cannot find out afterwards about a thing that runs with nobody in the
// room.
func TestAStandingProposalDrawsWhenAndCost(t *testing.T) {
	a, _, _ := standApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
		WhenWords: "Mondays at 9am",
		CostWords: "about $0.02 a run, at most once a day",
		Deadline:  a.now().Add(30 * time.Second),
	})})

	text := standText(a)
	for _, want := range []string{
		taskHeadCorner + " " + glyphAsk + " " + standWaitGlyph,
		"Mondays at 9am",
		"about $0.02 a run, at most once a day",
		session.StandingHeadCheck,
		standEndsWord + "30s",
		taskFootCorner,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the standing card is missing %q:\n%s", want, text)
		}
	}
	// AND IT DOES NOT BORROW THE TASK CARD'S CLOCK WORD. Silence declines this
	// one; a card promising it would auto-start is promising the opposite of
	// what the engine does.
	if strings.Contains(text, taskAutoWord) {
		t.Fatalf("the standing meter claims it auto-starts:\n%s", text)
	}
	// AND THE ANSWERS ARE NOT ON IT. They are the question, drawn once, above the
	// box, by the block (question.go) — a card carrying them as well would be one
	// decision drawn twice on one screen.
	if strings.Contains(text, standYesWord) {
		t.Fatalf("the card in the transcript still draws its own answers:\n%s", text)
	}
	standDraw(a)
	if block := standBlock(a); !strings.Contains(block, "1  "+standYesWord) {
		t.Fatalf("the block is not asking:\n%s", block)
	}
}

// A CADENCE THE MODEL INVENTED IS SAID OUT LOUD AND ASKED ABOUT. A guess
// presented as a fact is the one thing on this block a person cannot audit
// afterwards, because it reads exactly like something they said.
func TestAGuessedCadenceAsksInsteadOfStating(t *testing.T) {
	a, _, _ := standApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
		WhenWords: "about every 2 minutes",
		CostWords: "about $0.01 a check",
		Guessed:   true,
	})})

	text := standText(a)
	if !strings.Contains(text, "about every 2 minutes"+standGuessTag) {
		t.Fatalf("a guessed cadence was stated rather than asked:\n%s", text)
	}
}

// THE "checked every …" CLAUSE BELONGS TO THE KINDS THAT ARE ACTUALLY LOOKED AT.
// A reminder is not examined between now and Monday, and telling somebody it is
// checked every five minutes would be describing the ticker's own housekeeping
// as work done on their behalf.
func TestOnlyAWatchSaysHowOftenItIsChecked(t *testing.T) {
	a, _, _ := standApp(t)
	item := standItem()
	item.When = standing.When{Kind: standing.WhenProbe, Words: "when CI on main goes red",
		Probe: standing.Probe{Command: "gh run list"}}
	drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
		Item: item, WhenWords: "when CI on main goes red", CostWords: "about $0.01 a check",
	})})
	if want := standCheckTag + "5 minutes"; !strings.Contains(standText(a), want) {
		t.Fatalf("a probe did not say %q:\n%s", want, standText(a))
	}

	b, _, _ := standApp(t)
	drive(t, b, streamEventMsg{gen: b.gen, ev: standProposal(b, session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
	})})
	if strings.Contains(standText(b), standCheckTag) {
		t.Fatalf("a routine claimed it is checked on a clock:\n%s", standText(b))
	}
}

// THE THREE KEYS ARE THE THREE ANSWERS, and each of them reaches the engine as
// the one field that means it.
func TestTheThreeKeysSendTheThreeAnswers(t *testing.T) {
	yes := func(key string) session.StandingAnswer {
		t.Helper()
		a, agent, tick := standApp(t)
		standAsk(t, a, tick, session.StandingNotice{
			WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
		})
		drive(t, a, key2(key))
		if len(agent.answered) != 1 {
			t.Fatalf("%q resolved %d times, want 1", key, len(agent.answered))
		}
		if agent.answered[0].id != 11 {
			t.Fatalf("%q answered id %d, want 11", key, agent.answered[0].id)
		}
		return agent.answered[0].answer
	}

	// AND THE CARD STAYS IN THE TRANSCRIPT, SETTLED. The block collapses to its
	// head and its foot, and the foot IS the answer — which is what makes a
	// conversation read back later say what was decided rather than that
	// something was once asked.
	kept, keptAgent, keptTick := standApp(t)
	standAsk(t, kept, keptTick, session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
		Deadline: kept.now().Add(30 * time.Second),
	})
	drive(t, kept, key2("1"))
	if len(keptAgent.answered) != 1 {
		t.Fatalf("the card was not answered, the engine saw %v", keptAgent.answered)
	}
	if kept.stand == nil || !kept.stand.settled() {
		t.Fatal("the answered card left the transcript")
	}
	// The chosen word is the kind's own yes, cadence included. The verdict sits
	// after it, so a later reading says both which button was pressed and what
	// that button did.
	wantFoot := standAnswerWord(standItem(), "1") + " · " + standSetWord
	if page := standText(kept); !strings.Contains(page, wantFoot) {
		t.Fatalf("the settled card does not carry the answer and what it came to:\n%s", page)
	}

	if answer := yes("1"); !answer.Approved || answer.Once || answer.Change != "" {
		t.Fatalf("1 sent %+v, want a bare approval", answer)
	}
	if answer := yes("3"); answer.Approved || !answer.Once {
		t.Fatalf("3 sent %+v, want once and not standing", answer)
	}

	// `c` IS A REQUEST FOR THE BOX AND NOT AN ANSWER: nothing is resolved until
	// the person has said when instead. It was `2 change when or where` on the
	// card's own chip row and it is the block's own key for the same act
	// everywhere now (questionkeys.go's table).
	a, agent, tick := standApp(t)
	standAsk(t, a, tick, session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
	})
	aimed(a)
	drive(t, a, key2(questionCommentKey))
	if len(agent.answered) != 0 {
		t.Fatalf("`c` answered before anything was typed: %+v", agent.answered)
	}
	if typed := a.input.String(); typed != "" {
		t.Fatalf("`c` was typed into the box instead of taken: %q", typed)
	}
	typeLine(t, a, "make it 8")
	if len(agent.replaced) != 1 || agent.replaced[0].Change != "make it 8" {
		t.Fatalf("updated request did not travel: %+v", agent.replaced)
	}
	if len(agent.answered) != 0 {
		t.Fatalf("other approved or answered the old item: %+v", agent.answered)
	}
}

// A YES IS THE WHOLE ANSWER AND NOTHING FOLLOWS IT. The card used to ask a
// second question after a yes — keep checking when no window is open? — and it
// asks nobody now: background checks go on with the first item that stands and
// the switch is a settings row from then on.
func TestAYesOnAStandingCardResolvesItOutright(t *testing.T) {
	a, agent, tick := standApp(t)
	standAsk(t, a, tick, session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
	})
	drive(t, a, key2("1"))
	if len(agent.answered) != 1 {
		t.Fatalf("the card resolved %d times, want exactly 1", len(agent.answered))
	}
	if answer := agent.answered[0].answer; !answer.Approved {
		t.Fatalf("the yes travelled as %+v", answer)
	}
	text := standText(a)
	if strings.Contains(text, "no window is open?") || strings.Contains(text, "yes, always") {
		t.Fatalf("the card asked a question this build no longer asks:\n%s", text)
	}
	if !strings.Contains(text, standSetWord) {
		t.Fatalf("the settled card does not say what it came to:\n%s", text)
	}
}

// AND THE LINE ABOUT THE BACKGROUND CHECKS IS THE SENTENCE AND NOTHING ELSE —
// no glyph, no item name in front of it. It is not news about the reminder; it
// is what this machine just switched on, and where the switch is.
func TestTheBackgroundNoticeDrawsAsItsOwnBareLine(t *testing.T) {
	a, _, _ := standApp(t)
	line := "checks every 5 minutes, window or not · background checks under /settings"
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventStandingUpdate,
		Standing: &session.StandingNotice{
			Item: standItem(), Update: standBackgroundWord, Text: line,
		},
	}})
	text := standText(a)
	if !strings.Contains(text, line) {
		t.Fatalf("the background line is not on the screen:\n%s", text)
	}
	if strings.Contains(text, standWaitGlyph+" "+standName(standItem().Words)) {
		t.Fatalf("the background line was drawn as news about the item:\n%s", text)
	}
}

// THE CLOCK DECLINES. A task proposal approves on silence because its work is
// bounded; this one spends forever with nobody in the room, so an unanswered
// card ends as nothing — and the surface says so without answering for the
// engine.
func TestAnUnansweredStandingCardEndsAsNothing(t *testing.T) {
	a, agent, tick := standApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
		Deadline: a.now().Add(10 * time.Second),
	})})
	tick(11 * time.Second)
	drive(t, a, frameMsg{})
	if !strings.Contains(standText(a), standExpiredWord) {
		t.Fatalf("the expired card does not say it ended:\n%s", standText(a))
	}
	if len(agent.answered) != 0 {
		t.Fatalf("the surface answered for the engine's own clock: %+v", agent.answered)
	}
}

// A CLICK ON THE ANSWERS ROW ANSWERS THE QUESTION, and it is resolved against
// that row's own columns.
func TestClickingAStandingChipAnswersIt(t *testing.T) {
	a, agent, tick := standApp(t)
	standAsk(t, a, tick, session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
	})
	x, y := standAnswerRowAt(t, a, "1")
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if len(agent.answered) != 1 || !agent.answered[0].answer.Approved {
		t.Fatalf("a click on the yes did not approve: %+v", agent.answered)
	}
}

// NEWS IS ONE LINE AND NEVER TWO. The quiet law is the whole of the ambient
// side's manners: everything an item says in a conversation it says in one row.
func TestAStandingUpdateIsExactlyOneLine(t *testing.T) {
	for _, probe := range []struct {
		update, text, want string
	}{
		{"stood", "", standWaitGlyph + " every Monday at 9, post the · " + standSetWord},
		{"fired", "the standup note is in notes/standup.md",
			standWaitGlyph + " every Monday at 9, post the · said: the standup note is in"},
		{"needs-you", "the fix touches migrations",
			homeAskGlyph + " every Monday at 9, post the · " + tierYourCallWord + ": the fix"},
		{"stopped", "", standOffGlyph + " every Monday at 9, post the · stopped"},
	} {
		a, _, _ := standApp(t)
		item := standItem()
		drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
			Kind:     session.EventStandingUpdate,
			Standing: &session.StandingNotice{Item: item, Update: probe.update, Text: probe.text},
		}})
		rows := a.visible(a.bodyWidth())
		var drawn []string
		for _, r := range rows {
			if text := strings.TrimSpace(plain(r.text)); text != "" {
				drawn = append(drawn, text)
			}
		}
		if len(drawn) != 1 {
			t.Fatalf("%q drew %d lines, want 1:\n%s", probe.update, len(drawn), strings.Join(drawn, "\n"))
		}
		if !strings.HasPrefix(drawn[0], probe.want) {
			t.Fatalf("%q drew %q, want it to start %q", probe.update, drawn[0], probe.want)
		}
	}
}

// standAnswerRowAt is the screen row one of the open question's answers landed
// on, resolved through the SAME bands the click goes through — a test that
// computed its own rows would be testing a second layout.
func standAnswerRowAt(t *testing.T, a *app, key string) (int, int) {
	t.Helper()
	standDraw(a)
	q, ok := a.questionHead()
	if !ok {
		t.Fatalf("nothing is on the block:\n%s", standText(a))
	}
	want := -1
	for i, option := range q.question.Options {
		if option.Key == key {
			want = i
		}
	}
	// The block sits above the box; the frame is what knows where that is.
	_ = a.View()
	for _, band := range a.questionBands {
		if band.at != want {
			continue
		}
		for y := range a.height {
			if mark, ok := a.chromeAt(y); ok && mark.kind == chromeQuestion && mark.index == band.row {
				return band.span.from, y
			}
		}
	}
	t.Fatalf("no answers row offers %q:\n%s", key, strings.Join(a.questionRows(a.width), "\n"))
	return 0, 0
}

// standDraw paints the chrome, which is what stamps a question as SEEN — the
// settle guard is a claim about the screen ([app.markQuestionShown]).
func standDraw(a *app) { _ = a.questionRows(a.width) }

// standAsk puts one proposal on the surface and leaves it where a person is
// before their first key: drawn, and [questionSettle] gone by.
func standAsk(t *testing.T, a *app, tick func(time.Duration), notice session.StandingNotice) {
	t.Helper()
	drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, notice)})
	standDraw(a)
	tick(questionSettle + time.Millisecond)
	standDraw(a)
}

// key2 spells a key the surface's own way, for the digits the answers take.
func key2(s string) tea.KeyPressMsg { return key(s) }

// A CARD WITH NO DEADLINE HAS NO CLOCK AT ALL: no bar, no `ends in`, and no
// hour of ticking that turns it into `ended · nothing was set up`.
//
// The engine holds a watched session's proposal open indefinitely — nobody is
// in the room to answer it on a thirty-second budget — and a surface that drew a
// draining bar there would be inventing a deadline, while one that expired the
// card itself would be a second authority on a clock it does not own.
func TestACardWithNoDeadlineDrawsNoMeterAndNeverEnds(t *testing.T) {
	a, agent, tick := standApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
		WhenWords: "Mondays at 9am",
		CostWords: "about $0.02 a run",
	})})

	text := standText(a)
	if strings.Contains(text, standEndsWord) || strings.Contains(text, meterFull) {
		t.Fatalf("a card with no deadline drew a countdown:\n%s", text)
	}
	if strings.Contains(text, taskWaitingWord) {
		t.Fatalf("a card with no deadline spent a row saying its clock is absent:\n%s", text)
	}
	// It is still a question, and it stays one through a minute of frames.
	standDraw(a)
	if block := standBlock(a); !strings.Contains(block, "1  "+standYesWord) {
		t.Fatalf("the card is not asking:\n%s", block)
	}
	for i := 0; i < 60; i++ {
		tick(time.Second)
		drive(t, a, frameMsg{})
	}
	if a.stand.settled() {
		t.Fatalf("the card ended by itself after a minute: %q", a.stand.verdict)
	}
	standDraw(a)
	if after := standBlock(a); !strings.Contains(after, "1  "+standYesWord) {
		t.Fatalf("the card stopped asking after a minute of ticks:\n%s", after)
	}
	// And it still answers.
	drive(t, a, key2("1"))
	if len(agent.answered) != 1 || !agent.answered[0].answer.Approved {
		t.Fatalf("a card with no clock could not be answered, the engine saw %v", agent.answered)
	}
}

// ── a one-off reminder's card has two answers ───────────────────────────────

// standReminder is a one-off reminder as the engine proposes one: a moment, and
// one line said at it.
func standReminder() standing.Item {
	return standing.Item{
		ID:        "abc",
		Words:     "remind me to sleep in 1 min",
		Workspace: "/tmp/lab",
		When:      standing.When{Kind: standing.WhenAt, Words: "in 1 minute · 07:35", At: time.Now().Add(time.Minute)},
		Does:      standing.Action{Kind: standing.ActionSay, Say: "time to sleep"},
		Rails:     standing.Rails{PerRunUSD: 0.05, MaxPerDay: 1},
		Status:    standing.StatusActive,
	}
}

// `ONCE, NOT STANDING` IS NOT AN ANSWER TO A ONE-OFF REMINDER, so the card does
// not draw the chip and the digit under it does nothing.
//
// Written from a person's transcript: they asked for a one-minute reminder, met
// three chips, pressed `3` because it was the answer that committed to nothing,
// and were told codeaf could not hold a one-minute timer. It can; a standing
// one-off IS the timer. The chip was the defect.
func TestAOneOffReminderCardDrawsTwoChips(t *testing.T) {
	a, agent, tick := standApp(t)
	standAsk(t, a, tick, session.StandingNotice{
		Item:      standReminder(),
		WhenWords: "in 1 minute · 07:35",
		CostWords: "about a cent, once",
		Options:   session.StandingOptions(standReminder()),
	})

	block := standBlock(a)
	for _, want := range []string{"1  Remind me", "0  Don't remind me"} {
		if !strings.Contains(block, want) {
			t.Fatalf("a reminder's card is missing %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, standOnceWord) {
		t.Fatalf("a one-off reminder's card still offers `%s`:\n%s", standOnceWord, block)
	}
	// AND THE DIGIT UNDER THE MISSING ANSWER RESOLVES NOTHING. A key that
	// answered a question the card never asked would be the same defect wearing
	// no paint.
	drive(t, a, key2("3"))
	if len(agent.answered) != 0 {
		t.Fatalf("`3` answered a card that never offered it: %v", agent.answered)
	}
	if a.stand.settled() {
		t.Fatalf("`3` settled the card as %q", a.stand.verdict)
	}
	// The derivation names the keys the card drew and not one more. (The slot
	// itself is quiet while the block draws them — hints pick A — and this is
	// the reading behind it, which is where the defect would be.)
	const twoHint = "1 Remind me in 1 minute · 07:35 · 0 Don't remind me · esc later"
	if got := a.questionHintFor(); got != twoHint {
		t.Fatalf("the hint is %q, want %q", got, twoHint)
	}
	// And the two it did draw still work. (The stray `3` is in the box, which is
	// the block's own law — a key with no answer under it belongs to the
	// composer — so it comes out before the yes is pressed.)
	a.input.reset()
	drive(t, a, key2("1"))
	if len(agent.answered) != 1 || !agent.answered[0].answer.Approved {
		t.Fatalf("`1` did not stand it up, the engine saw %v", agent.answered)
	}
}

// questionHintFor is the hint's own derivation for whatever question is open,
// which is what these tests are about: the slot that shows it is quiet while the
// block draws its own keys.
func (a *app) questionHintFor() string {
	head, ok := a.questionHead()
	if !ok {
		return ""
	}
	return a.questionHintOn(head)
}

// standBlock is the question block above the box, plain — which is where a
// standing card's answers are drawn (standing.go, question.go).
func standBlock(a *app) string {
	return plain(strings.Join(a.questionRows(a.width), "\n"))
}

// AND EVERYWHERE ELSE THE THIRD ANSWER STAYS. A watch is a thing a person may
// reasonably want done once, now, instead of kept an eye on forever.
func TestAWatchCardStillDrawsThreeChips(t *testing.T) {
	a, agent, tick := standApp(t)
	watch := standItem()
	watch.When = standing.When{Kind: standing.WhenProbe, Words: "every few minutes"}
	standAsk(t, a, tick, session.StandingNotice{
		Item:      watch,
		WhenWords: "every few minutes",
		CostWords: "about $0.02 a check",
		Options:   session.StandingOptions(watch),
	})

	block := standBlock(a)
	if !strings.Contains(block, "3  Check once now") {
		t.Fatalf("a watch lost its once answer:\n%s", block)
	}
	const threeHint = "1 Watch for it · 3 Check once now · 0 Don't watch · esc later"
	if got := a.questionHintFor(); got != threeHint {
		t.Fatalf("the hint is %q, want %q", got, threeHint)
	}
	drive(t, a, key2("3"))
	if len(agent.answered) != 1 || !agent.answered[0].answer.Once {
		t.Fatalf("`3` on a watch did not answer once: %v", agent.answered)
	}
}

// HOME DRAWS THE SAME CHIPS, because it draws the ones the waiting session
// offered ([session.PresenceQuestion.Options]) and the engine writes that list
// from the same place the card's comes from.
func TestHomesChipsAgreeWithTheCard(t *testing.T) {
	reminder, watch := standReminder(), standItem()
	if session.StandingOptions(reminder) == nil {
		t.Fatal("a reminder was left with no answers at all")
	}
	for _, option := range session.StandingOptions(reminder) {
		if option.Key == session.StandingOnceKey {
			t.Fatalf("home would draw %q %s on a one-off reminder", option.Key, option.Label)
		}
	}
	var found bool
	for _, option := range session.StandingOptions(watch) {
		found = found || option.Key == session.StandingOnceKey
	}
	if !found {
		t.Fatal("home would drop `once` from a card that offers it")
	}
	// And the question this surface draws follows the same list.
	a, _, tick := standApp(t)
	standAsk(t, a, tick, session.StandingNotice{
		Item: reminder, WhenWords: "in 1 minute", CostWords: "about a cent, once",
		Options: session.StandingOptions(reminder),
	})
	q, ok := a.questionHead()
	if !ok || len(q.question.Options) != 2 {
		t.Fatalf("a reminder's card offers %+v, want two answers", q.question.Options)
	}
	for _, option := range q.question.Options {
		if strings.TrimSpace(option.Label) == "" || strings.TrimSpace(option.Consequence) == "" {
			t.Fatalf("an answer with nothing beside it: %+v", option)
		}
	}
}

// `0` IS THE OUTRIGHT NO, AND IT IS THE SAME KEY ON EVERY CARD.
//
// In the conversation `esc` has always been the no and still is. It is the key
// the other two surfaces cannot spare — esc on home closes home, and esc in the
// errand pane hands the keyboard back to the list — so the decline needed a
// name a card could draw, and `0` is it: one keystroke, off both ends of the
// chip numbering, nowhere near `1` ([session.StandingNoKey]).
func TestZeroSaysNoToAStandingCardWhereverItIsDrawn(t *testing.T) {
	watch := standItem()
	watch.When = standing.When{Kind: standing.WhenProbe, Words: "every few minutes"}
	for _, item := range []standing.Item{watch, standReminder()} {
		a, agent, tick := standApp(t)
		standAsk(t, a, tick, session.StandingNotice{
			Item:      item,
			WhenWords: "every few minutes",
			CostWords: "about $0.02 a check",
			Options:   session.StandingOptions(item),
		})
		drive(t, a, key2(session.StandingNoKey))
		if len(agent.answered) != 1 {
			t.Fatalf("`%s` resolved %d times on a %s card, want once", session.StandingNoKey, len(agent.answered), item.When.Kind)
		}
		// NOTHING WAS SET UP AND NOTHING WAS RUN: the zero answer, which is what
		// the engine reads as a decline.
		if answer := agent.answered[0].answer; answer != (session.StandingAnswer{}) {
			t.Fatalf("`%s` sent %+v, want the decline", session.StandingNoKey, answer)
		}
		// And the row keeps the same words `esc` would have left on it.
		if a.stand == nil || a.stand.verdict != standNoWord {
			t.Fatalf("the declined card settled as %q, want %q", a.stand.verdict, standNoWord)
		}
	}

	// THE ANSWER ROW NAMES IT, on both shapes of card — the decline is the one
	// key that is on every standing card there is, so a person who only ever
	// meets one in a conversation still learns the key that works everywhere.
	for _, item := range []standing.Item{watch, standReminder()} {
		a, _, tick := standApp(t)
		standAsk(t, a, tick, session.StandingNotice{
			Item: item, WhenWords: "every few minutes", CostWords: "about $0.02 a check",
			Options: session.StandingOptions(item),
		})
		label := ""
		for _, option := range session.StandingOptions(item) {
			if option.Key == session.StandingNoKey {
				label = option.Label
			}
		}
		if block := standBlock(a); !strings.Contains(block, session.StandingNoKey+"  "+label) {
			t.Fatalf("the card does not draw the decline:\n%s", block)
		}
	}

	// AND IT IS A DIGIT BEFORE IT IS AN ANSWER, exactly as 1 and 3 are: a
	// correction in the box is a sentence, and "0900" is a when somebody might
	// write. With anything typed the card lets the key go.
	a, agent, tick := standApp(t)
	standAsk(t, a, tick, session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
	})
	drive(t, a, key("9"), key2(session.StandingNoKey), key("0"))
	if len(agent.answered) != 0 {
		t.Fatalf("the decline answered a card somebody was typing a when into: %v", agent.answered)
	}
	if typed := a.input.String(); typed != "900" {
		t.Fatalf("the box holds %q, want the digits that were typed", typed)
	}
}

// ── THE FIRING IS DRAWN WHERE IT LANDED ─────────────────────────────────────
//
// [TestAStandingUpdateIsExactlyOneLine] proves the renderer; this proves the
// ROAD, which is the half that was missing. A firing arrives when no turn is
// running — that is what ambient means — so it comes off the session's standing
// lane and has to travel the whole of the program loop to reach the transcript.
// The real-binary suite watched a reminder reach the conversation's journal and
// never appear on the screen, because nothing was ever put on this lane.

// firingApp is a surface over a session with the standing lane open, and
// nothing in flight: this is the state a person is in when a reminder fires —
// sitting there, not typing, no turn running.
func firingApp(t *testing.T) (*app, *taskFake, tea.Cmd) {
	t.Helper()
	agent := &taskFake{
		fakeAgent: &fakeAgent{model: "m"},
		updates:   make(chan session.Event, 8),
	}
	a := newTestApp(agent)
	a.width, a.height = 120, 24
	cmd := a.watchTasks()
	if cmd == nil {
		t.Fatal("the surface did not open the standing lane")
	}
	return a, agent, cmd
}

// A FIRING BETWEEN TURNS IS DRAWN AT ONCE, in the shape the manual promises:
// `◦ <words> · said: <text>`.
func TestAFiringOffTheStandingLaneIsDrawnInTheConversation(t *testing.T) {
	a, agent, cmd := firingApp(t)
	if a.state != stateIdle {
		t.Fatalf("this surface is mid-turn (%v); the point is that nothing is running", a.state)
	}
	agent.updates <- session.Event{Kind: session.EventStandingUpdate, Standing: &session.StandingNotice{
		Item:   standItem(),
		Update: "fired",
		Text:   "the standup note is in notes/standup.md",
	}}
	drive(t, a, runCmd(cmd)...)

	want := standWaitGlyph + " every Monday at 9, post the · said: the standup note is in notes/standup.md"
	if body := standText(a); !strings.Contains(body, want) {
		t.Fatalf("the firing was never drawn.\nwant a row %q\ngot:\n%s", want, body)
	}
}

// firingReplyAgent carries both lanes involved in a live firing: the standing
// event that draws the news and the turn the session wakes to answer it.
type firingReplyAgent struct {
	*taskFake
	wakes chan (<-chan session.Event)
}

func (f *firingReplyAgent) Wakes() <-chan (<-chan session.Event) { return f.wakes }

// THE REPLY MAY FOLD ITS OWN WORK AND NEVER THE NEWS THAT WOKE IT. The row is
// present before the first reply event and remains exactly once after the turn
// settles, which pins both halves of the ordering contract.
func TestAStandingFiringStaysDrawnAfterItsReply(t *testing.T) {
	agent := &firingReplyAgent{
		taskFake: &taskFake{
			fakeAgent: &fakeAgent{model: "m"},
			updates:   make(chan session.Event, 8),
		},
		wakes: make(chan (<-chan session.Event), 1),
	}
	a := newTestApp(agent)
	a.width, a.height = 120, 24
	tasks, wakes := a.watchTasks(), a.watchWakes()
	if tasks == nil || wakes == nil {
		t.Fatal("the surface did not open both standing firing lanes")
	}

	const words = "remind me in 1 minute to drink water"
	row := standName(words) + " · said: Time to drink water!"
	agent.updates <- session.Event{Kind: session.EventStandingUpdate, Standing: &session.StandingNotice{
		Item: standing.Item{
			ID:    "water",
			Words: words,
		},
		Update: "fired",
		Text:   "Time to drink water!",
	}}
	drive(t, a, runCmd(tasks)...)
	if body := standText(a); strings.Count(body, row) != 1 {
		t.Fatalf("before the reply the firing row occurs %d times, want once:\n%s", strings.Count(body, row), body)
	}

	agent.wakes <- woken(
		text(session.EventTextDelta, "Drink some water now."),
		session.Event{Kind: session.EventTurnDone},
	)
	drive(t, a, runCmd(wakes)...)
	drive(t, a, frameMsg{})
	body := standText(a)
	if !strings.Contains(body, "Drink some water now.") {
		t.Fatalf("the firing's reply was not drawn:\n%s", body)
	}
	if got := strings.Count(body, row); got != 1 {
		t.Fatalf("after the reply the firing row occurs %d times, want once:\n%s", got, body)
	}
}

// AND A RUN THAT STOPPED ON SOMEBODY WEARS THE ACCENT, on the same lane and
// with no turn to carry it either.
func TestAFiringThatNeedsSomebodyIsDrawnWithTheAskGlyph(t *testing.T) {
	a, agent, cmd := firingApp(t)
	agent.updates <- session.Event{Kind: session.EventStandingUpdate, Standing: &session.StandingNotice{
		Item:   standItem(),
		Update: "needs-you",
		Text:   "the fix touches migrations",
	}}
	drive(t, a, runCmd(cmd)...)

	want := homeAskGlyph + " every Monday at 9, post the · " + tierYourCallWord + ": the fix touches migrations"
	if body := standText(a); !strings.Contains(body, want) {
		t.Fatalf("the firing was never drawn.\nwant a row %q\ngot:\n%s", want, body)
	}
}

// THE FOLD IS DRAWN ON OPEN, one row per thing that was waiting. The engine
// hands them to the first subscriber of the lane ([session.Agent.TaskUpdates]),
// so from this side they are simply the first thing that arrives on it.
func TestWhatFiredWhileTheWindowWasShutIsDrawnWhenTheLaneOpens(t *testing.T) {
	a, agent, cmd := firingApp(t)
	for _, waiting := range []struct{ words, update, text string }{
		{"tell me when CI goes red", "fired", "the last run on main failed"},
		{"keep main green", "needs-you", "the fix touches migrations"},
	} {
		item := standItem()
		item.Words = waiting.words
		agent.updates <- session.Event{Kind: session.EventStandingUpdate, Standing: &session.StandingNotice{
			Item: item, Update: waiting.update, Text: waiting.text,
		}}
	}
	drive(t, a, runCmd(cmd)...)

	body := standText(a)
	for _, want := range []string{
		standWaitGlyph + " tell me when CI goes red · said: the last run on main failed",
		homeAskGlyph + " keep main green · " + tierYourCallWord + ": the fix touches migrations",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the fold is missing %q:\n%s", want, body)
		}
	}
}

// THE LANE KEEPS PUMPING. A conversation an item fires into twice draws two
// rows, or the second reminder of the day is one nobody is ever told about.
func TestTheStandingLaneRearmsAfterAFiring(t *testing.T) {
	a, agent, cmd := firingApp(t)
	for _, text := range []string{"the first thing it said", "the second thing it said"} {
		agent.updates <- session.Event{Kind: session.EventStandingUpdate, Standing: &session.StandingNotice{
			Item: standItem(), Update: "fired", Text: text,
		}}
	}
	drive(t, a, runCmd(cmd)...)

	body := standText(a)
	for _, said := range []string{"the first thing it said", "the second thing it said"} {
		if !strings.Contains(body, said) {
			t.Fatalf("the lane stopped before %q:\n%s", said, body)
		}
	}
}

// A RULE'S CARD IS ITS SENTENCE AND ITS REACH, AND NOTHING ELSE.
//
// A hold has no moment, no rhythm and no condition, so a `when ·` band under one
// would be the card reading a cadence into the word "always"; and it never wakes,
// so it never runs a probe, buys a judgment or launches work, and a `costs ·`
// band would be asking somebody to weigh a figure nothing can ever draw on. The
// notice here carries both anyway — the surface is what a person reads, and it
// refuses to draw either whoever filled it in.
func TestARulesCardDrawsNoCadenceAndNoCost(t *testing.T) {
	a, _, _ := standApp(t)
	rule := standItem()
	rule.Words = "never touch the public API"
	rule.When = standing.When{Kind: standing.WhenHold}
	rule.Does, rule.Rails = standing.Action{}, standing.Rails{}
	drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
		Item:      rule,
		WhenWords: "always",
		CostWords: "shares the day's $20.00 allowance",
	})})

	text := standText(a)
	if strings.Contains(text, standWhenTag) {
		t.Fatalf("a rule's card claims a cadence:\n%s", text)
	}
	if strings.Contains(text, standCostTag) {
		t.Fatalf("a rule's card quotes money it can never spend:\n%s", text)
	}
	// THE WHERE BAND IS UNCHANGED AND IS STILL NEVER DROPPED: an order whose
	// reach was not on the card is an order somebody agreed to without knowing
	// where it applies.
	if !strings.Contains(text, standWhereTag+standProjectWord) {
		t.Fatalf("a rule's card does not say how far it reaches:\n%s", text)
	}
	if !strings.Contains(text, "never touch the public API") {
		t.Fatalf("a rule's card lost the person's own sentence:\n%s", text)
	}
}

// ── THE CARD A STRANGER CAN ANSWER ──────────────────────────────────────────
//
// A person met their first standing card and said, in as many words, that they
// did not understand the options: they had expected a cancel, and they had
// expected to be able to change the reach the card was showing them. All three
// of those are one defect — the card stated facts and offered verbs without
// saying what any of them would do — and the three tests below are the three
// halves of the fix.

// THE WAY OUT IS ON THE CARD. It used to be `esc`, and a `0` named in the hint
// slot under the message box: a gesture whose only documentation is
// documentation, which docs/DESIGN-LANGUAGE.md refuses by name.
//
// AND `esc` IS NO LONGER ONE OF THE WAYS OUT. On the block esc is *later* — the
// question folds to the chip, the card stays open and nothing is decided — which
// is the one meaning that changed when this card moved (question.go's THE NEVER
// MODAL law). A standing card has no clock it loses by waiting and the safe
// answer is to leave it alone, so a person who reaches for the dismiss key gets
// exactly that.
func TestTheStandingCardDrawsTheWayOutAndTakesItTwoWays(t *testing.T) {
	a, agent, tick := standApp(t)
	standAsk(t, a, tick, session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
	})
	if block := standBlock(a); !strings.Contains(block, session.StandingNoKey+"  "+standNoWordChip) {
		t.Fatalf("the card draws no way to say no:\n%s", block)
	}

	// THE TWO DOORS ARE ONE ANSWER: the `0` and a click on its row both send the
	// zero answer the engine reads as a decline.
	for _, door := range []func(*app){
		func(a *app) { drive(t, a, key2(session.StandingNoKey)) },
		func(a *app) {
			x, y := standAnswerRowAt(t, a, session.StandingNoKey)
			drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
		},
	} {
		a, agent, tick := standApp(t)
		standAsk(t, a, tick, session.StandingNotice{WhenWords: "Mondays at 9am"})
		door(a)
		if len(agent.answered) != 1 || agent.answered[0].answer != (session.StandingAnswer{}) {
			t.Fatalf("one of the ways out sent %+v, want the decline", agent.answered)
		}
		if a.stand.verdict != standNoWord {
			t.Fatalf("the declined card settled as %q, want %q", a.stand.verdict, standNoWord)
		}
	}

	// AND esc PUTS IT OFF RATHER THAN ANSWERING IT: nothing reaches the engine,
	// the card is still open, and the chip goes on counting it.
	drive(t, a, key("esc"))
	if len(agent.answered) != 0 {
		t.Fatalf("esc answered the card: %+v", agent.answered)
	}
	if a.stand.settled() {
		t.Fatalf("esc settled the card as %q", a.stand.verdict)
	}
	if a.questionCount() != 1 {
		t.Fatalf("the folded question stopped being counted: %d", a.questionCount())
	}
}

// EVERY ANSWER SAYS WHAT IT DOES, ON ITS OWN ROW.
//
// The card used to say this one clause at a time, under a row of chips, about
// whichever answer the cursor happened to be on — so reading the question meant
// walking it. On the block each answer carries its own consequence beside its
// own word, which is what makes the whole decision readable at once.
//
// AND IT SAYS WHAT THE BANDS CANNOT. The clause used to read out the cadence and
// the reach — `I'll keep doing this Mondays at 9am, for this project, until you
// stop it` — which is exactly what the `when ·` and `where ·` bands two rows
// above already say. What no band can say is how long each answer lasts.
func TestEveryStandingAnswerSaysWhatItWillDo(t *testing.T) {
	a, _, tick := standApp(t)
	item := standItem()
	item.Altitude = standing.AltitudeMachine
	watch := item
	watch.When = standing.When{Kind: standing.WhenProbe, Words: "every few minutes"}
	standAsk(t, a, tick, session.StandingNotice{
		Item: watch, WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
		Options: session.StandingOptions(watch),
	})
	block := standBlock(a)
	for key, want := range map[string]string{
		"1":                     "It watches until you stop it.",
		session.StandingOnceKey: "Checks once now. Nothing keeps watching.",
		session.StandingNoKey:   "Nothing watches.",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("the answer under %q does not say %q:\n%s", key, want, block)
		}
	}
	// AND THE BANDS ARE NOT SAID TWICE. The cadence and the reach belong to the
	// card in the transcript; a consequence repeating them would be one fact on
	// two rows.
	for _, band := range []string{"Mondays at 9am", standEverywhereWord} {
		if strings.Contains(block, band) {
			t.Fatalf("the block repeats the card's %q band:\n%s", band, block)
		}
	}
}

// EACH ANSWER A STANDING CARD OFFERS IS SPELLED IN EXACTLY ONE PLACE.
//
// This is the one-source-of-truth law made into a build failure, and it is here
// because the drift it forbids had already happened. The hint under the box was
// a whole SENTENCE typed out three times — `standProposalHint`, `standTwoHint`,
// and a third copy in the errand pane — and the third named `3 just once` over
// a one-off reminder's card, which correctly draws no such chip. Nothing
// answered to that `3` (#189). Two of those copies were right and the third was
// wrong, which is exactly what a second copy is for.
//
// So the sentence is gone: the hint is built from the chips the card drew
// ([standHintFields]), and what remains written down is one constant per answer.
// This counts the STRING LITERALS, so a comment quoting the row — pickrow.go's
// header draws it — is not a spelling of it, and the sources are parsed rather
// than grepped ([TestEachVerbWordIsSpelledOnce] makes the same call for the same
// reason).
func TestEachStandingAnswerWordIsSpelledOnce(t *testing.T) {
	// The three numbered answers, in their long spellings. The decline is not on
	// this list: `no` is two letters that fall inside half the sentences on this
	// surface, so it is guarded by the constants that BUILD on it instead —
	// [standNoEscWord] is `"or esc, " + standNoWordChip` and not a second `no`.
	words := []string{standYesWord, standChangeWord, standOnceWord}
	seen := map[string]int{}
	fset := token.NewFileSet()
	for _, name := range placeSourceFiles(t) {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("could not parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			for _, word := range words {
				// CONTAINS AND NOT EQUALS, because the copy that was here did not
				// spell the answer on its own — it was a whole sentence with the
				// answer buried in it, which is how it drifted unnoticed. A test
				// asking for equality would have watched it go past.
				if !strings.Contains(text, word) {
					continue
				}
				seen[word]++
				if text != word || name != "standing.go" {
					t.Errorf("%q is spelled again inside %q at %s — a standing card's answers live in standing.go and nowhere else",
						word, text, fset.Position(lit.Pos()))
				}
			}
			return true
		})
	}
	for _, word := range words {
		if seen[word] != 1 {
			t.Errorf("%q is spelled %d times, want exactly one — its own constant", word, seen[word])
		}
	}
}

// ONE QUESTION ARRIVING BY TWO ROADS IS ONE QUESTION.
//
// A standing card crosses a link TWICE on purpose. The proposal itself is an
// event on the turn's stream — and, where nobody was attached when it was
// raised, out of the far machine's waiting room afterwards (internal/remote's
// held.go, replayed through [app.replayHeld] into the very same door) — while
// the questions lane sends the OBJECT whole to every surface that attaches
// ([session.Agent.WatchQuestions]). Both roads reach this block now that the
// standing lane is drawn here, and a block that appended rather than replaced
// would hand a person one decision as two.
//
// THE TOKEN IS WHAT MAKES THEM ONE: the lane and the lane's own id
// ([questionShown.token]), which both builders spell the same way because the
// surface's is the engine's builder said again ([app.standingQuestion]).
func TestAStandingQuestionArrivingByBothRoadsDrawsOnce(t *testing.T) {
	a, _, tick := standApp(t)
	notice := session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
	}
	standAsk(t, a, tick, notice)
	if len(a.questions) != 1 {
		t.Fatalf("the proposal raised %d questions", len(a.questions))
	}
	first := a.questions[0]

	// AND NOW THE SAME QUESTION OFF THE LANE, exactly as the engine sends it:
	// the bare object, with none of what the surface knows about it.
	bare := a.standingQuestion(a.stand, notice)
	bare.Options = session.StandingOptions(a.stand.item)
	drive(t, a, questionEventMsg{gen: a.questionGen, ev: session.Event{
		Kind: session.EventQuestion, Question: &bare,
	}})
	if len(a.questions) != 1 {
		t.Fatalf("the lane's copy became a second question: %d open", len(a.questions))
	}
	if got := a.questionCount(); got != 1 {
		t.Fatalf("the chip counts %d questions about one card", got)
	}
	// AND ONE CARD IN THE TRANSCRIPT, for the same reason from the other side.
	cards := 0
	for _, e := range a.entries {
		if e.kind == entryStanding && e.stand != nil {
			cards++
		}
	}
	if cards != 1 {
		t.Fatalf("the transcript drew %d cards about one proposal", cards)
	}
	// THE SHOWN STAMP SURVIVES THE REPLAY, which is what keeps a link that
	// reconnects from handing the question a fresh settle guard every few
	// seconds — a question that would then never become answerable at all.
	if !a.questions[0].shown.Equal(first.shown) {
		t.Fatalf("the replay restamped the question: %v then %v", first.shown, a.questions[0].shown)
	}
	// AND SO DO THE WORDS ON ITS ANSWERS. The bare object carries the kind's own
	// spellings; the card's are richer, and respelling every answer under a hand
	// is exactly what [questionSameAnswers] is for.
	block := standBlock(a)
	if !strings.Contains(block, "1  "+standYesWord) {
		t.Fatalf("the lane's copy respelled the answers:\n%s", block)
	}
}

// A NARROW BAND DROPS THE CADENCE BEFORE IT CUTS A CHARACTER. "Set it up ·
// every 3 hours" becomes "Set it up", and only a still-too-narrow stem is cut.
func TestANarrowStandingLabelDropsTheCadenceFirst(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	full := "Set it up · every 3 hours"
	if got := a.questionBandWord(full, "1", 24); got != "Set it up" {
		t.Fatalf("the cadence was not dropped: %q", got)
	}
	if got := a.questionBandWord("Don't set it up", "0", 80); got != "Don't set it up" {
		t.Fatalf("the no was rewritten: %q", got)
	}
}
