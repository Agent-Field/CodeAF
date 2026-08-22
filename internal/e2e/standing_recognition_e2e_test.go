//go:build e2e

package e2e

// standing_recognition_e2e_test.go asks the real model which ordinary
// sentences deserve a standing-order card. It asserts only the proposal event
// and the structured shape on that event; the model's prose is not the
// recognition contract.
//
//	go test -tags e2e -run TestStandingRecognitionE2E -count=1 -v ./internal/e2e/

import (
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

func TestStandingRecognitionE2E(t *testing.T) {
	if strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")) == "" {
		t.Skip("no OPENROUTER_API_KEY: this lane drives a real model")
	}

	tests := []struct {
		name         string
		sentence     string
		wantProposal bool
		wantKinds    []standing.WhenKind
		wantAltitude standing.Altitude
	}{
		{
			name:         "1 a reminder is standing",
			sentence:     "remind me in two minutes to stretch",
			wantProposal: true,
			wantKinds:    []standing.WhenKind{standing.WhenAt},
		},
		{
			name:         "2 a weekly rhythm is standing",
			sentence:     "every monday at 9 summarize where this project stands",
			wantProposal: true,
			wantKinds:    []standing.WhenKind{standing.WhenEvery},
		},
		{
			name:         "3 a file watch is standing",
			sentence:     "tell me when the file README.md changes",
			wantProposal: true,
			wantKinds:    []standing.WhenKind{standing.WhenFile, standing.WhenProbe},
		},
		{
			// This case depends on the sibling lane's doctrine recognizing a persistent finishing habit.
			name:         "4 a finishing habit is standing",
			sentence:     "from now on, whenever you finish a task here, run gofmt before landing",
			wantProposal: true,
		},
		{
			name:     "5 immediate work is not standing",
			sentence: "run the tests",
		},
		{
			// This discharge test depends on the sibling lane's doctrine separating current-work acceptance from persistence.
			name:     "6 current work acceptance is not standing",
			sentence: "make sure the website you are building has exactly 3 pages",
		},
		{
			name:     "7 a question is not standing",
			sentence: "what time is it?",
		},
		{
			name:         "8 everywhere promotes the altitude",
			sentence:     "everywhere, not just this project: tell me when disk usage gets above 90%",
			wantProposal: true,
			wantAltitude: standing.AltitudeMachine,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := newWorld(t)
			for attempt := 1; attempt <= 2; attempt++ {
				agent, _ := w.open(t.TempDir(), nil)
				got, notice := waitForStandingProposal(w, agent, test.sentence)
				problem := standingRecognitionMismatch(test.wantProposal, test.wantKinds, test.wantAltitude, got, notice)
				if problem == "" {
					return
				}
				t.Logf("attempt %d did not match: %s", attempt, problem)
				if attempt == 2 {
					t.Fatal(problem)
				}
			}
		})
	}
}

// waitForStandingProposal submits one sentence through the shared bounded turn
// driver and declines any card so recognition alone cannot alter later facts.
func waitForStandingProposal(w *world, agent *session.Agent, sentence string) (bool, session.StandingNotice) {
	w.t.Helper()
	turn := w.say(agent, sentence, answerNo)
	if len(turn.Proposals) == 0 {
		return false, session.StandingNotice{}
	}
	return true, turn.Proposals[0]
}

func standingRecognitionMismatch(wantProposal bool, wantKinds []standing.WhenKind, wantAltitude standing.Altitude, got bool, notice session.StandingNotice) string {
	if got != wantProposal {
		return "standing proposal presence did not match the sentence"
	}
	if !got {
		return ""
	}
	if len(wantKinds) > 0 {
		matched := false
		for _, kind := range wantKinds {
			if notice.Item.When.Kind == kind {
				matched = true
				break
			}
		}
		if !matched {
			return "standing proposal carried the wrong waking kind"
		}
	}
	if wantAltitude != "" && notice.Item.Level() != wantAltitude {
		return "standing proposal carried the wrong altitude"
	}
	return ""
}
