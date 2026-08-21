package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
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
}

type standReply struct {
	id     uint64
	answer session.StandingAnswer
}

func (f *standFake) ResolveStanding(id uint64, answer session.StandingAnswer) {
	f.answered = append(f.answered, standReply{id: id, answer: answer})
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
		standWhenTag + "Mondays at 9am",
		standCostTag + "about $0.02 a run, at most once a day",
		"[ 1 " + standYesWord + " ]",
		"[ 2 " + standChangeWord + " ]",
		"[ 3 " + standOnceWord + " ]",
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
		a, agent, _ := standApp(t)
		drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
			WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
		})})
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
	kept, keptAgent, _ := standApp(t)
	drive(t, kept, streamEventMsg{gen: kept.gen, ev: standProposal(kept, session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
		Deadline: kept.now().Add(30 * time.Second),
	})})
	drive(t, kept, key2("1"))
	if len(keptAgent.answered) != 1 {
		t.Fatalf("the card was not answered, the engine saw %v", keptAgent.answered)
	}
	if kept.stand == nil || !kept.stand.settled() {
		t.Fatal("the answered card left the transcript")
	}
	if page := standText(kept); !strings.Contains(page, standYesWord+" · "+standSetWord) {
		t.Fatalf("the settled card does not carry the answer and what it came to:\n%s", page)
	}

	if answer := yes("1"); !answer.Approved || answer.Once || answer.Change != "" {
		t.Fatalf("1 sent %+v, want a bare approval", answer)
	}
	if answer := yes("3"); answer.Approved || !answer.Once {
		t.Fatalf("3 sent %+v, want once and not standing", answer)
	}

	// 2 IS A REQUEST FOR THE BOX AND NOT AN ANSWER: nothing is resolved until
	// the person has said when instead.
	a, agent, _ := standApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
	})})
	drive(t, a, key2("2"))
	if len(agent.answered) != 0 {
		t.Fatalf("2 answered before anything was typed: %+v", agent.answered)
	}
	typeLine(t, a, "make it 8")
	if len(agent.answered) != 1 || agent.answered[0].answer.Change != "make it 8" {
		t.Fatalf("the correction did not travel: %+v", agent.answered)
	}
	if agent.answered[0].answer.Approved {
		t.Fatalf("a correction approved the item: %+v", agent.answered[0].answer)
	}
}

// A YES IS THE WHOLE ANSWER AND NOTHING FOLLOWS IT. The card used to ask a
// second question after a yes — keep checking when no window is open? — and it
// asks nobody now: background checks go on with the first item that stands and
// the switch is a settings row from then on.
func TestAYesOnAStandingCardResolvesItOutright(t *testing.T) {
	a, agent, _ := standApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
	})})
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
	a, agent, _ := standApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
	})})
	x, y := standChipAt(t, a, standYes)
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if len(agent.answered) != 1 || !agent.answered[0].answer.Approved {
		t.Fatalf("a click on the first chip did not approve: %+v", agent.answered)
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
			homeAskGlyph + " every Monday at 9, post the · needs your look: the fix"},
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

// standChipAt is the screen position of one chip on the open card's answers
// row — resolved through the SAME spans the click goes through, because a test
// that computed its own columns would be testing a second layout.
func standChipAt(t *testing.T, a *app, want int) (int, int) {
	t.Helper()
	body, _ := a.window(a.bodyWidth(), a.viewHeight())
	for i, r := range body {
		if r.hit != hitStandChoice {
			continue
		}
		for _, span := range a.stand.spans {
			if span.at == want {
				return span.from, a.bodyTop() + i
			}
		}
	}
	t.Fatalf("no visible answers row offers chip %d:\n%s", want, standText(a))
	return 0, 0
}

// key2 spells a key the surface's own way, for the digits the chips answer to.
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
	if !strings.Contains(text, "[ 1 "+standYesWord+" ]") {
		t.Fatalf("the card is not asking:\n%s", text)
	}
	for i := 0; i < 60; i++ {
		tick(time.Second)
		drive(t, a, frameMsg{})
	}
	if a.stand.settled() {
		t.Fatalf("the card ended by itself after a minute: %q", a.stand.verdict)
	}
	if after := standText(a); !strings.Contains(after, "[ 1 "+standYesWord+" ]") {
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
		When:      standing.When{Kind: standing.WhenAt, Words: "in 1 minute — 07:35", At: time.Now().Add(time.Minute)},
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
// and were told aforge could not hold a one-minute timer. It can; a standing
// one-off IS the timer. The chip was the defect.
func TestAOneOffReminderCardDrawsTwoChips(t *testing.T) {
	a, agent, _ := standApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
		Item:      standReminder(),
		WhenWords: "in 1 minute — 07:35",
		CostWords: "about a cent, once",
		Options:   session.StandingOptions(standReminder()),
	})})

	text := standText(a)
	for _, want := range []string{"[ 1 " + standYesWord + " ]", "[ 2 " + standChangeWord + " ]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("a reminder's card is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, standOnceWord) {
		t.Fatalf("a one-off reminder's card still offers `%s`:\n%s", standOnceWord, text)
	}
	// AND THE DIGIT UNDER THE MISSING CHIP DOES NOTHING. A key that answered a
	// question the card never asked would be the same defect wearing no paint.
	drive(t, a, key2("3"))
	if len(agent.answered) != 0 {
		t.Fatalf("`3` answered a card that never offered it: %v", agent.answered)
	}
	if a.stand.settled() {
		t.Fatalf("`3` settled the card as %q", a.stand.verdict)
	}
	// The hint names the keys the card drew and not one more.
	if got := standAskHint(a.stand); got != standTwoHint {
		t.Fatalf("the hint is %q, want %q", got, standTwoHint)
	}
	// And the two it did draw still work.
	drive(t, a, key2("1"))
	if len(agent.answered) != 1 || !agent.answered[0].answer.Approved {
		t.Fatalf("`1` did not stand it up, the engine saw %v", agent.answered)
	}
}

// AND EVERYWHERE ELSE THE THIRD ANSWER STAYS. A watch is a thing a person may
// reasonably want done once, now, instead of kept an eye on forever.
func TestAWatchCardStillDrawsThreeChips(t *testing.T) {
	a, agent, _ := standApp(t)
	watch := standItem()
	watch.When = standing.When{Kind: standing.WhenProbe, Words: "every few minutes"}
	drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
		Item:      watch,
		WhenWords: "every few minutes",
		CostWords: "about $0.02 a check",
		Options:   session.StandingOptions(watch),
	})})

	text := standText(a)
	if !strings.Contains(text, "[ 3 "+standOnceWord+" ]") {
		t.Fatalf("a watch lost its `%s` chip:\n%s", standOnceWord, text)
	}
	if got := standAskHint(a.stand); got != standProposalHint {
		t.Fatalf("the hint is %q, want %q", got, standProposalHint)
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
	// And the words this surface draws follow the same list.
	if words := standAnswerWords(session.StandingNotice{Item: reminder, Options: session.StandingOptions(reminder)}); len(words) != 2 {
		t.Fatalf("a reminder's chips are %v, want two", words)
	}
	if words := standAnswerWords(session.StandingNotice{Item: watch, Options: session.StandingOptions(watch)}); len(words) != 3 {
		t.Fatalf("a watch's chips are %v, want three", words)
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
		a, agent, _ := standApp(t)
		drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
			Item:      item,
			WhenWords: "every few minutes",
			CostWords: "about $0.02 a check",
			Options:   session.StandingOptions(item),
		})})
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

	// THE HINT NAMES IT, on both shapes of card — the decline is the one key
	// that is on every standing card there is, so a person who only ever meets
	// one in a conversation still learns the key that works everywhere.
	for _, hint := range []string{standProposalHint, standTwoHint} {
		if !strings.Contains(hint, session.StandingNoKey) {
			t.Fatalf("the hint %q does not name the decline", hint)
		}
	}

	// AND IT IS A DIGIT BEFORE IT IS AN ANSWER, exactly as 1, 2 and 3 are: a
	// correction in the box is a sentence, and "0900" is a when somebody might
	// write. With anything typed the card lets the key go.
	a, agent, _ := standApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
	})})
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

	want := homeAskGlyph + " every Monday at 9, post the · needs your look: the fix touches migrations"
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
		homeAskGlyph + " keep main green · needs your look: the fix touches migrations",
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
