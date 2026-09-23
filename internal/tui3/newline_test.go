package tui3

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The actual modified Enter reaches the same editor behavior on every message
// surface, including while work is running behind home or a task room.
func TestShiftEnterEditsEveryMessageBoxWithoutSending(t *testing.T) {
	for _, surface := range []string{"home", "conversation", "running", "room"} {
		for _, selected := range []bool{false, true} {
			t.Run(surface+map[bool]string{false: "/caret", true: "/selection"}[selected], func(t *testing.T) {
				var a *app
				var agent *fakeAgent
				if surface == "running" || surface == "room" {
					a, agent = bargeable(t, "still working")
				} else if surface == "home" {
					d, app := drafting(t)
					a, agent = app, d.fakeAgent
					a.showPage(pageHome)
				} else {
					agent = &fakeAgent{}
					a = newTestApp(agent)
				}
				if surface == "room" {
					a.room = a.newRoom(3, "port the parser")
				}
				box := &a.input
				if surface == "home" {
					box = &a.home.box
				}
				box.insert("one two")
				box.cursor = 3
				want := "one\n two"
				if selected {
					box.pickSpan(3, 4)
					want = "one\ntwo"
				}
				sent, state := len(agent.sent), a.state
				drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
				if got := box.String(); got != want || box.cursor != 4 {
					t.Fatalf("draft = %q at %d, want %q at 4", got, box.cursor, want)
				}
				if len(agent.sent) != sent || agent.stops != 0 || len(a.parks) != 0 || a.state != state {
					t.Fatalf("newline affected work: sent=%v stops=%d parks=%v state=%v", agent.sent, agent.stops, a.parks, a.state)
				}
			})
		}
	}
}

func TestEnterStillSendsTheWholeMultilineMessage(t *testing.T) {
	agent := &fakeAgent{}
	a := newTestApp(agent)
	typeInto(t, a, "first line")
	drive(t, a, key("shift+enter"))
	typeInto(t, a, "second line")
	drive(t, a, key("enter"))
	if len(agent.sent) != 1 || agent.sent[0] != "first line\nsecond line" {
		t.Fatalf("Enter did not send the whole draft once: %q", agent.sent)
	}
}

func TestHomeEnterStartsAConversationWithTheWholeMultilineMessage(t *testing.T) {
	_, a := drafting(t)
	a.showPage(pageHome)
	next := &fakeAgent{model: "m"}
	dir := t.TempDir()
	a.start = func(string) (Conversation, error) {
		return Conversation{Agent: next, SessionFile: dir + "/transcript.jsonl"}, nil
	}
	typeHome(a, "first line")
	drive(t, a, key("shift+enter"))
	typeHome(a, "second line")
	if !a.at(pageHome) || len(next.sent) != 0 {
		t.Fatal("newline left home or sent the draft")
	}
	drive(t, a, key("enter"))
	if len(next.sent) != 1 || next.sent[0] != "first line\nsecond line" {
		t.Fatalf("home Enter did not send the whole draft once: %q", next.sent)
	}
}

func TestShiftEnterOpensALineInAskHere(t *testing.T) {
	_, a := drafting(t)
	ex := &homeExchange{focused: true, onOffer: true}
	ex.box.insert("firstsecond")
	ex.box.cursor = 5
	a.exchangeKey(ex, key("shift+enter"))
	if got := ex.box.String(); got != "first\nsecond" || ex.onOffer {
		t.Fatalf("ask-here newline = %q, onOffer=%v", got, ex.onOffer)
	}
}
