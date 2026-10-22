package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestFirstMessageHasItsOwnScreenBeforeTheConversationOpens(t *testing.T) {
	for _, fromHome := range []bool{true, false} {
		t.Run(map[bool]string{true: "home", false: "new-tab"}[fromHome], func(t *testing.T) {
			a := openingLab(t)
			release := make(chan struct{})
			t.Cleanup(func() { close(release) })
			a.start = func(string) (Conversation, error) { <-release; return Conversation{Agent: &fakeAgent{model: "m"}}, nil }
			var text string
			if fromHome {
				text = "A prompt that must appear before session setup"
				typeHome(a, text)
				a.homeEnter()
			} else {
				a.closeHome()
				a.openChatStart()
				text = "A new tab acknowledges this message immediately"
				a.input.setText(text)
				a.startChatEnter()
			}
			frame := plain(mustFrame(a))
			if !strings.Contains(frame, "\nnew chat\n") || !strings.Contains(frame, text) || !strings.Contains(frame, conversationOpeningWord) {
				t.Fatalf("first message has no immediate screen: %s", frame)
			}
			if len(a.agent.(*fakeAgent).sent) != 0 {
				t.Fatal("the old conversation received the new message")
			}
			a.cancelConversationOpening()
			if a.conversationPreview != nil {
				t.Fatal("cancelled preview survived")
			}
		})
	}
}

func TestHomeReadsInTheBackgroundAndDropsAnEarlierVisitsAnswer(t *testing.T) {
	a := openingLab(t)
	a.closeHome()
	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	reads := 0
	world := a.homeReading.world
	a.world = func() (session.World, bool) { reads++; close(entered); <-release; return world, true }
	cmd := a.openHome()
	if !a.at(pageHome) || reads != 0 {
		t.Fatal("opening Home waited for its reading")
	}
	a.frame()
	if !a.homeReadingPending || a.refreshHomeLater() != nil {
		t.Fatal("Home did not coalesce its background reading")
	}
	// Only the reading command is driven: the surface clock is unrelated to the
	// scan's causal ordering and needs no real sleep in this regression.
	done := make(chan []any, 1)
	go func() {
		msgs := runCmd(cmd)
		values := make([]any, len(msgs))
		for i, m := range msgs {
			values[i] = m
		}
		done <- values
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("Home never asked for its background reading")
	}
	a.closeHome()
	a.world = nil
	a.openHome()
	before := a.homeGen
	close(release)
	for _, msg := range <-done {
		if door, ok := msg.(doorMsg); ok {
			a.doorSaid(door)
		}
	}
	if a.homeGen != before || !a.homeReadingPending {
		t.Fatal("an old visit changed the new visit's reading")
	}
}

// A cancelling pointer press belongs to the preview the person saw, never the
// old Home hit map that happens to remain underneath it.
func TestCancellingTheOpeningPreviewDoesNotClickThroughToAnotherPage(t *testing.T) {
	a := openingLab(t)
	a.width, a.height = 180, 42
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	a.start = func(string) (Conversation, error) { <-release; return Conversation{Agent: &fakeAgent{model: "m"}}, nil }
	typeHome(a, "A pending first message")
	a.homeEnter()
	frame := strings.Split(plain(mustFrame(a)), "\n")
	x := strings.Index(frame[0], "Settings")
	if x < 0 {
		t.Fatal("the preview has no settings navigation target")
	}
	a.Update(tea.MouseClickMsg{X: x, Y: 0, Button: tea.MouseLeft})
	if a.conversationOpening || !a.at(pageHome) || a.home.box.String() != "A pending first message" {
		t.Fatal("the cancelling press activated the underlying page or lost its draft")
	}
}
