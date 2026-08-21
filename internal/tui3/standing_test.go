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

// THE ONE-TIME QUESTION IS ASKED IN PLACE AND ANSWERED ONCE. The engine is
// waiting on a single answer, so both halves — the yes and the preference —
// travel in it.
func TestTheWatchFollowUpCollectsBothAnswersInOneCall(t *testing.T) {
	a, agent, _ := standApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run", OfferWatch: true,
	})})
	drive(t, a, key2("1"))
	if len(agent.answered) != 0 {
		t.Fatalf("the yes was sent before the follow-up was answered: %+v", agent.answered)
	}
	text := standText(a)
	for _, want := range []string{standWatchAsk, "[ 1 " + standAlwaysWord + " ]", "[ 2 " + standWindowWord + " ]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the follow-up is missing %q:\n%s", want, text)
		}
	}
	drive(t, a, key2("1"))
	if len(agent.answered) != 1 {
		t.Fatalf("the card resolved %d times, want exactly 1", len(agent.answered))
	}
	answer := agent.answered[0].answer
	if !answer.Approved || answer.KeepWatch == nil || !*answer.KeepWatch {
		t.Fatalf("the follow-up sent %+v, want approved with KeepWatch true", answer)
	}

	// AND THE OTHER ANSWER IS THE ONE THAT INSTALLS NOTHING.
	b, other, _ := standApp(t)
	drive(t, b, streamEventMsg{gen: b.gen, ev: standProposal(b, session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run", OfferWatch: true,
	})})
	drive(t, b, key2("1"))
	drive(t, b, key2("2"))
	if len(other.answered) != 1 {
		t.Fatalf("the card resolved %d times, want exactly 1", len(other.answered))
	}
	if answer := other.answered[0].answer; !answer.Approved || answer.KeepWatch == nil || *answer.KeepWatch {
		t.Fatalf("the follow-up sent %+v, want approved with KeepWatch false", answer)
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
