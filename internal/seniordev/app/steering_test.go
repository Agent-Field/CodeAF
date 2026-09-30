//go:build !windows

package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
)

// fakeListener is a host's inbox a test fills: what it holds is what the next
// read answers, and what the run heard and why it closed are kept.
type fakeListener struct {
	mu     sync.Mutex
	queue  []delegate.Message
	heard  []string
	closed []string
}

func (f *fakeListener) send(m delegate.Message) {
	f.mu.Lock()
	f.queue = append(f.queue, m)
	f.mu.Unlock()
}

func (f *fakeListener) Messages() []delegate.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.queue
	f.queue = nil
	return out
}

func (f *fakeListener) Heard(ids []string) {
	f.mu.Lock()
	f.heard = append(f.heard, ids...)
	f.mu.Unlock()
}

func (f *fakeListener) CloseInbox(reason string) {
	f.mu.Lock()
	f.closed = append(f.closed, reason)
	f.mu.Unlock()
}

// A MESSAGE WAITING WHEN THE MODEL STOPS IS ITS NEXT PROMPT, NOT A NUDGE. The
// model ends a turn without handing in while a message from the person is
// waiting: the next turn's prompt is that message, in the words the model is
// handed, the nudge count stays at zero, the receipt names it, it is kept for
// compaction, the page is told, and the inbox closes the moment it hands in.
func TestAMessageWaitingWhenTheModelStopsIsItsNextPromptNotANudge(t *testing.T) {
	runner, state, _, events := soloPipeline(t)
	listener := &fakeListener{}
	runner.inbox = listener
	outcome := soloOutcome{}
	turns := 0
	runner.turnForTest = func(_ context.Context, _, prompt string) (turnResult, error) {
		turns++
		switch turns {
		case 1:
			listener.send(delegate.Message{ID: "n1", From: delegate.FromPerson, Text: "the grader is in grade.sh"})
			return turnResult{}, nil
		case 2:
			if !strings.Contains(prompt, "- the person: the grader is in grade.sh") || strings.Contains(prompt, "nudge") {
				t.Fatalf("the turn after a message was prompted with %q", prompt)
			}
			if err := writeFile(filepath.Join(runner.workspace, "grade.sh"), "#!/bin/sh\n"); err != nil {
				t.Fatal(err)
			}
			if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("fixed the grader")); err != nil {
				t.Fatalf("freeze: %v", err)
			}
			return turnResult{}, nil
		default:
			t.Fatalf("turn %d should not run", turns)
			return turnResult{}, nil
		}
	}
	if err := runner.soloConverse(context.Background(), "fix it", state, &outcome); err != nil {
		t.Fatalf("converse: %v", err)
	}
	if outcome.TerminalTrigger != "submitted" || outcome.Nudges != 0 {
		t.Fatalf("trigger %q nudges %d, want submitted with no nudge", outcome.TerminalTrigger, outcome.Nudges)
	}
	if !reflect.DeepEqual(listener.heard, []string{"n1"}) {
		t.Fatalf("heard %v, want n1", listener.heard)
	}
	if len(listener.closed) != 1 || !strings.Contains(listener.closed[0], "handed in") || runner.inbox != nil {
		t.Fatalf("the inbox did not close once at the hand-in: %v", listener.closed)
	}
	kept, err := os.ReadFile(filepath.Join(runner.workspace, steeringFile))
	if err != nil || !strings.Contains(string(kept), "the person, ") || !strings.Contains(string(kept), "the grader is in grade.sh") {
		t.Fatalf("the steering file holds %q (%v)", kept, err)
	}
	steered := false
	for _, event := range soloStageEvents(t, events, "implement") {
		if event["status"] == steeringStage && event["messages"] == float64(1) {
			steered = true
		}
	}
	if !steered {
		t.Fatal("the page was not told a message was handed over")
	}
}

// A RUN NOBODY TALKS TO TAKES NOTHING, and a closed inbox is closed once.
func TestARunWithNoInboxTakesNothing(t *testing.T) {
	runner, _, _, _ := soloPipeline(t)
	if got := runner.takeSteering(); got != "" {
		t.Fatalf("a run with no inbox took %q", got)
	}
	listener := &fakeListener{}
	runner.inbox = listener
	runner.closeInbox("first")
	runner.closeInbox("second")
	listener.send(delegate.Message{ID: "n2", From: delegate.FromConversation, Text: "too late"})
	if got := runner.takeSteering(); got != "" || len(listener.closed) != 1 || len(listener.heard) != 0 {
		t.Fatalf("after closing: took %q, closed %v, heard %v", got, listener.closed, listener.heard)
	}
}

// injectorStore is the three store methods the reminder injector writes to.
type injectorStore struct {
	infos []msgmodel.Info
	parts []msgmodel.Part
}

func (s *injectorStore) Messages(context.Context, string) ([]msgmodel.WithParts, error) {
	return nil, nil
}
func (s *injectorStore) UpdateMessage(_ context.Context, info msgmodel.Info) error {
	s.infos = append(s.infos, info)
	return nil
}
func (s *injectorStore) UpdatePart(_ context.Context, part msgmodel.Part) error {
	s.parts = append(s.parts, part)
	return nil
}

// THE HOOK SAVES THE WORDS AS A MESSAGE OF THEIR OWN. senior-dev's steering
// fills turn.BetweenStepReminder; the injector it drives saves the words to the
// session as a user message and appends it to the request, and adds nothing
// when there are no words.
func TestTheBetweenStepHookSavesTheWordsAsAMessage(t *testing.T) {
	store := &injectorStore{}
	words := ""
	inject := turnReminderInjector(store, "ses_1", func() string { return words })
	user := msgmodel.User{MessageBase: msgmodel.MessageBase{ID: "msg_0", SessionID: "ses_1"}, Agent: "coder"}
	out, err := inject(context.Background(), nil, user)
	if err != nil || len(out) != 0 || len(store.parts) != 0 {
		t.Fatalf("no words still added %v (saved %v, %v)", out, store.parts, err)
	}
	words = "While you work, a message reached you: the grader is in grade.sh"
	out, err = inject(context.Background(), nil, user)
	if err != nil || len(out) != 1 || len(store.infos) != 1 || len(store.parts) != 1 {
		t.Fatalf("the words were not saved and appended: %v, %v, %v", out, store.parts, err)
	}
	if part, ok := store.parts[0].(msgmodel.TextPart); !ok || part.Text != words {
		t.Fatalf("saved part = %#v", store.parts[0])
	}
}

// A LISTENING HOST'S RUN HANDS EVERY CODER TURN THE HOOK. A whole in-process
// run on a host that listens: the coder's turn carries the between-steps hook,
// and calling it hands over the message waiting in the host's inbox.
func TestAListeningHostsRunHandsEveryCoderTurnTheHook(t *testing.T) {
	workspace := testRepoWithEntrypoints(t)
	t.Setenv("SENIOR_DEV_SCRATCH_ROOT", t.TempDir())
	host := &listeningTestHost{testHost: &testHost{workspace: workspace}}
	host.send(delegate.Message{ID: "n1", From: delegate.FromConversation, Text: "the grader is in grade.sh"})
	var handed string
	backend := &coderOnlyBackend{onCoder: func(call int, request turn) error {
		if call == 1 && request.BetweenStepReminder != nil {
			handed = request.BetweenStepReminder()
		}
		return nil
	}}
	var notes strings.Builder
	runWith(context.Background(), host, Options{Goal: "Implement the thing.", High: "provider/high"}, &notes, backend)
	if !strings.Contains(handed, "- the conversation that handed you this work: the grader is in grade.sh") {
		t.Fatalf("the coder's first turn was handed %q\n%s", handed, notes.String())
	}
	if !reflect.DeepEqual(host.heard, []string{"n1"}) || len(host.closed) != 1 {
		t.Fatalf("heard %v closed %v", host.heard, host.closed)
	}
}

// listeningTestHost is the test host with an inbox.
type listeningTestHost struct {
	*testHost
	fakeListener
}
