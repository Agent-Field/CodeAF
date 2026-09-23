package session

// A RUN STOPPED ON ITS OWN QUESTION IS NOT WORKING.
//
// A sub-harness run that asks a question blocks until somebody answers it, and
// until this the session went on telling every other window it was `working`:
// the question was registered and reachable through OpenQuestions, but the
// predicate behind presence never read that lane, and the lane raised its
// question without banking a row at the desk. So no row appeared on home, no
// mark was drawn, and the work stopped in silence until somebody happened to
// open the conversation.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec"
)

func TestASessionStoppedOnASubharnessQuestionSaysItIsWaitingAndWhy(t *testing.T) {
	root := t.TempDir()
	agent, err := newAgent(Config{
		Workspace: root, Place: Place{Dir: root, Workspace: root},
		Model: "test/model", Interactive: true, AskConsent: true,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })

	env := agent.subharnessEnv(nil, nil, &subharnessJournal{agent: agent}, nil,
		exec.Manifest{SubharnessInfo: exec.SubharnessInfo{Name: "upgrade-adapters"}}, "test/model")

	const question = "which branch should the upgrade land on?"
	events, stop := agent.WatchQuestions()
	t.Cleanup(stop)
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, _ = env.Ask(ctx, question, exec.AskOptions{})
	}()
	t.Cleanup(func() {
		cancel()
		<-finished
	})

	// The lane registers before publication. The event proves its presence row
	// is banked too, so this assertion cannot race the publication goroutine.
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
waitingForQuestion:
	for {
		select {
		case event := <-events:
			if event.Kind == EventQuestion && event.Question != nil && event.Question.Head == question {
				break waitingForQuestion
			}
		case <-deadline.C:
			t.Fatal("the run never published its question")
		}
	}

	waiting := agent.waitingOnPerson()
	if !waiting.waiting {
		t.Fatalf("a session stopped on a sub-harness question says it is working: %+v", waiting)
	}
	// AND THE ROW CARRIES A SENTENCE. A mark with nothing under it is the defect
	// one lane over, so counting the lane without banking the desk row would be
	// half a fix and this is what refuses it.
	if !strings.Contains(waiting.reason, question) {
		t.Fatalf("the row says a person is needed but not what for: %q", waiting.reason)
	}
}
