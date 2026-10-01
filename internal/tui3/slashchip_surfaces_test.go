package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestPickedHarnessKeepsDemotedDoorPlain(t *testing.T) {
	a, agent, _ := pickApp(t, demoHarness("review-diff", "read a diff"))
	a.harnChip = "review-diff"
	typeInto(t, a, "say /standing")
	drive(t, a, key("backspace"))
	typeInto(t, a, " and /compact")
	sameRuns(t, boxRuns(a), []string{"/compact"}, "the demoted draft")
	drive(t, a, key("enter"))
	if agent.runs != 1 || agent.text != "say /standing and /compact" {
		t.Fatalf("picked harness received %q in %d runs", agent.text, agent.runs)
	}
	e := lastUserEntry(t, a)
	sameRuns(t, chipRuns(a.renderEntry(0, e, 30)...), []string{"/compact"}, "the picked harness transcript")
}

func TestGuardSendKeepsRestingDoorsPlain(t *testing.T) {
	for _, revive := range []bool{false, true} {
		name := "send"
		if revive {
			name = "revive"
		}
		t.Run(name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			a.guard = &steerGuard{text: "later /compact and say /standing", title: "repair /task"}
			a.guardSend(revive)
			e := lastUserEntry(t, a)
			if revive && !strings.HasPrefix(e.text, "The task ") {
				t.Fatal("the revive wrapper was not displayed")
			}
			sameRuns(t, chipRuns(a.renderEntry(0, e, 30)...), []string{"/compact"}, "the guard transcript")
		})
	}
}

func TestReplayedMessageKeepsRestingDoorsPlain(t *testing.T) {
	for _, pictures := range []bool{false, true} {
		name := "words"
		if pictures {
			name = "attachment"
		}
		t.Run(name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			recorded := session.DisplayEntry{Role: "user", Text: "日本 later\t/compact and say /standing"}
			if pictures {
				recorded.ImageRefs = []string{"picture.png"}
			}
			blocks, turns := a.replayBlocks([]session.DisplayEntry{recorded}, chatReplay(0))
			if turns != 1 || len(blocks) != 1 || blocks[0].kind != entryUser {
				t.Fatalf("replay returned %d turns and entries %+v", turns, blocks)
			}
			sameRuns(t, chipRuns(a.renderEntry(0, &blocks[0], 30)...), []string{"/compact"}, "the reopened transcript")
		})
	}
}

func TestReplayedBriefKeepsRestingDoorsPlain(t *testing.T) {
	for _, role := range []string{"user", "aside"} {
		t.Run(role, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			raw := "THE WORK\n\nlater /compact and say /standing"
			blocks, _ := a.replayBlocks([]session.DisplayEntry{{Role: role, Text: raw}}, roomReplay(0))
			if len(blocks) != 1 || !blocks[0].brief || blocks[0].text != raw {
				t.Fatalf("the instruction did not retain its journal text: %+v", blocks)
			}
			blocks[0].full = true
			if requestDisplayText(&blocks[0]) == raw {
				t.Fatal("the fixture did not change display coordinates")
			}
			sameRuns(t, chipRuns(a.renderEntry(0, &blocks[0], 30)...), []string{"/compact"}, "the reshaped instruction")
		})
	}
}

func TestFollowUpKeepsRestingDoorsPlain(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.follows = []queued{{text: "later /compact and say /standing", ch: make(chan session.Event)}}
	a.startFollow()
	e := lastUserEntry(t, a)
	sameRuns(t, chipRuns(a.renderEntry(0, e, 30)...), []string{"/compact"}, "the follow-up transcript")
}

func TestQuestionReplacementKeepsRestingDoorsPlain(t *testing.T) {
	lab := newQuestionLab(t)
	agent := &replacingQuestionScript{questionScript: lab.agent}
	lab.a.agent = agent
	lab.raise(consentAsk())
	lab.tick(questionSettle * 2)
	lab.press("o")
	words := "later /compact and say /standing"
	lab.a.input.setText(words)
	lab.press("enter")
	if len(agent.replaced) != 1 || agent.replaced[0].Change != words {
		t.Fatalf("replacement received %+v", agent.replaced)
	}
	e := lastUserEntry(t, lab.a)
	sameRuns(t, chipRuns(lab.a.renderEntry(0, e, 30)...), []string{"/compact"}, "the replacement transcript")
}

func TestClarificationKeepsRestingDoorsPlain(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.discussionEvent(&session.QuestionDiscussion{ID: "one", Seq: 1, Words: "later /compact and say /task"})
	e := lastUserEntry(t, a)
	if !strings.HasPrefix(e.text, "clarify: ") {
		t.Fatal("the clarification prefix was not displayed")
	}
	sameRuns(t, chipRuns(a.renderEntry(0, e, 30)...), []string{"/compact"}, "the clarification transcript")
}

func TestSteeredCorrectionKeepsChipsAndRestingDoorsPlain(t *testing.T) {
	a, agent := steerableTurn(t, "reading the tree. ")
	typeInto(t, a, "say /standing")
	drive(t, a, key("backspace"))
	typeInto(t, a, " and /compact later")
	words := a.input.String()
	drive(t, a, key("enter"))
	if len(agent.steered) != 1 || agent.steered[0] != words {
		t.Fatalf("the running answer received %q", agent.steered)
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind:  session.EventSteerAccepted,
		Steer: &session.SteerNote{ID: 1, Words: words, At: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)},
	}})
	for i := range a.entries {
		if a.entries[i].kind == entrySteer {
			sameRuns(t, chipRuns(a.renderEntry(i, &a.entries[i], 30)...), []string{"/compact"}, "the steered correction")
			return
		}
	}
	t.Fatal("the accepted correction has no transcript block")
}

func TestReplayedCorrectionKeepsChipsAndRestingDoorsPlain(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	blocks, _ := a.replayBlocks([]session.DisplayEntry{{
		Role: "user", Text: "later /compact and say /standing",
		Steer: &session.SteerMark{Consumed: true},
	}}, chatReplay(0))
	if len(blocks) != 1 || blocks[0].kind != entrySteer {
		t.Fatalf("the replay did not rebuild the correction: %+v", blocks)
	}
	sameRuns(t, chipRuns(a.renderEntry(0, &blocks[0], 30)...), []string{"/compact"}, "the replayed correction")
}

func TestWaitingMessageKeepsChipsAndDemotedDoorsPlain(t *testing.T) {
	for _, pictures := range []bool{false, true} {
		name := "words"
		if pictures {
			name = "attachment"
		}
		t.Run(name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			words := "日本 later\t/compact and say /standing"
			at := len([]rune(words[:strings.Index(words, "/standing")]))
			p := parked{text: words, plain: []segment{{from: at, to: at + len("/standing")}}}
			if pictures {
				p.chips = []chip{{path: "picture.png"}}
			}
			a.parks = []parked{p}
			rows := a.parkedRows(30)
			sameRuns(t, chipRuns(rows...), []string{"/compact"}, "the waiting message")
			a.hot = hoverAt{kind: hoverParked, index: 0}
			hovered := a.parkedRows(30)
			if len(hovered) != len(rows) {
				t.Fatal("hover changed the waiting block's height")
			}
			for i := 0; i < len(rows)-1; i++ {
				if hovered[i] != a.hoverRow(rows[i], 30) || ansi.StringWidth(hovered[i]) != 30 {
					t.Fatalf("waiting row %d lost its hover band: %q", i, hovered[i])
				}
			}
			if hovered[len(rows)-1] != rows[len(rows)-1] {
				t.Fatal("hover changed the waiting hint")
			}
		})
	}
}
