package tui3

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

type r6RefusedResumer struct{ *switchAgent }

func (*r6RefusedResumer) ResumeStoppedTurn(context.Context) (<-chan session.Event, bool) {
	events := make(chan session.Event, 1)
	events <- session.Event{Kind: session.EventError, Err: errors.New("chat held elsewhere")}
	close(events)
	return events, false
}

func TestR6_OwedTurnRefusalIsShownWithoutAskingAgain(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.app("")
	a.agent = &r6RefusedResumer{switchAgent: &switchAgent{fakeAgent: &fakeAgent{model: "m"}}}
	drain(t, a, a.resumeStoppedTurn())
	text := transcriptText(a)
	if strings.Contains(text, session.ResumedWord) || strings.Count(text, "chat held elsewhere") != 1 {
		t.Fatalf("resume refusal must appear once without asking again:\n%s", text)
	}
}
