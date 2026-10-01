package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func openingLab(t *testing.T) *app {
	t.Helper()
	lab := newHomeLab(t)
	mine := lab.session("mine", "aaaa000000000001", "old conversation", lab.workspace("mine"), time.Now())
	a := lab.app(mine)
	a.openHome()
	t.Cleanup(a.doorLine.close)
	return a
}

// The callback stays held while the update loop draws and accepts a key.
// No timing threshold stands in for that ordering.
func TestHomeOpeningDoesNotHoldInputAndCancelledDraftIsNotSent(t *testing.T) {
	a := openingLab(t)
	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	next := &fakeAgent{model: "m"}
	a.start = func(string) (Conversation, error) {
		close(entered)
		<-release
		return Conversation{Agent: next, SessionFile: "/next/transcript.jsonl"}, nil
	}
	typeHome(a, "original draft")
	cmd := a.homeEnter()
	<-entered
	if !a.conversationOpening || !strings.Contains(a.home.msg, "opening conversation") {
		t.Fatal("no immediate opening feedback")
	}
	a.frame()
	request := a.conversationRequest
	_, _ = a.Update(key("enter"))
	if a.conversationRequest != request {
		t.Fatal("repeated enter queued another open")
	}
	_, _ = a.Update(key("x"))
	if a.conversationOpening || !strings.Contains(a.home.box.String(), "x") {
		t.Fatal("typing was blocked or did not cancel the stale submission")
	}
	close(release)
	spend(t, a, cmd)
	if len(next.sent) != 0 || a.file == "/next/transcript.jsonl" {
		t.Fatal("cancelled opening stole the screen or sent the old draft")
	}
	if !a.at(pageHome) {
		t.Fatal("cancelled opening left home")
	}
}

func TestHomeOpeningCommitsBeforeSendingItsDraft(t *testing.T) {
	for _, target := range []string{"", "/other-project"} {
		t.Run(target, func(t *testing.T) {
			a := openingLab(t)
			old := a.file
			a.turn = 0
			a.input.setText("previous conversation's draft")
			a.chips = []chip{{path: "previous.png"}}
			a.target.where = target
			next := &fakeAgent{model: "m"}
			a.start = func(string) (Conversation, error) {
				return Conversation{Agent: next, SessionFile: "/next/transcript.jsonl"}, nil
			}
			typeHome(a, "send only to the next conversation")
			cmd := a.homeEnter()
			if a.file == "/next/transcript.jsonl" || len(next.sent) != 0 {
				t.Fatal("opening changed the screen before its result")
			}
			spend(t, a, cmd)
			if a.file != "/next/transcript.jsonl" || len(next.sent) != 1 || next.sent[0] != "send only to the next conversation" || len(a.chips) != 0 {
				t.Fatalf("wrong destination or draft: %q %v %+v", a.file, next.sent, a.chips)
			}
			back, refusal := a.openSession(Session{File: old})
			if refusal != "" {
				t.Fatal(refusal)
			}
			spend(t, a, back)
			if a.input.String() != "previous conversation's draft" || len(a.chips) != 1 || a.chips[0].path != "previous.png" {
				t.Fatal("opening from Home lost the previous conversation's draft or attachments")
			}
		})
	}
}

func TestHomeOpeningRefusalKeepsTheDraft(t *testing.T) {
	a := openingLab(t)
	before := a.file
	a.input.setText("previous draft")
	a.chips = []chip{{path: "previous.png"}}
	a.home.chips = []chip{{path: "new.png"}}
	a.start = func(string) (Conversation, error) { return Conversation{}, errors.New("not available") }
	typeHome(a, "keep these words")
	spend(t, a, a.homeEnter())
	if a.file != before || a.home.box.String() != "keep these words" || a.home.msg != "not available" || a.input.String() != "previous draft" || len(a.chips) != 1 || len(a.home.chips) != 1 {
		t.Fatal("a refused open lost the conversation, draft, or error")
	}
}

func TestHomeReopeningWaitsOutsideTheUpdateLoop(t *testing.T) {
	a := openingLab(t)
	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	next := &fakeAgent{model: "m"}
	a.open = func(workspace, file string) (Conversation, error) {
		close(entered)
		<-release
		return Conversation{Agent: next, Workspace: workspace, SessionFile: file}, nil
	}
	line := homeLine{row: a.home.world.Projects[0].Sessions[0]}
	line.row.Transcript = "/different/transcript.jsonl"
	cmd := a.homeOpenDoor(line)
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("opening never reached its callback")
	}
	if !a.conversationOpening {
		t.Fatal("opening was not pending")
	}
	a.frame()
	_, _ = a.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	close(release)
	spend(t, a, cmd)
	if a.file != line.row.Transcript {
		t.Fatalf("opened %q", a.file)
	}
}
