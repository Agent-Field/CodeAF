package session

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/store"
)

// A PERMISSION THAT WAS DENIED OR PUT AWAY IS AN EVENT, NEVER A RULE. The
// contract these tests state is about what a person and the next conversation
// can observe:
//
//   - C1: after a permission is dismissed, nothing that restricts tools is kept,
//     whatever the small reading model makes of the assistant's closing words.
//   - C2: the call that was dismissed is told the question was put away
//     unanswered, that nothing was decided, and that it may ask again.
//   - C3: an ordinary deny keeps its words, and keeps no memory either.
//   - C4: an ordinary fact the person states is still kept.

const noticeSystemPrefix = "You read one exchange between a person and an assistant"

// badReading is what a small reading model keeps from the assistant's closing
// words about a refused call, and is the leak these tests are about.
const badReading = "shell commands are denied"

// noticingTurns is a conversation whose noticing pass is answered by shape. A
// turn whose person line names a Tuesday deploy or short answers is read
// correctly; every other turn is read the way the leak read it.
type noticingTurns struct {
	agent *Agent
	brain *store.Store
	runs  chan string

	mu    sync.Mutex
	asked []bool
}

func newNoticingTurns(t *testing.T, steps []step) *noticingTurns {
	t.Helper()
	fixture := &noticingTurns{}
	completer := &scriptedCompleter{steps: steps}
	completer.aside = func(messages []ai.Message) (*ai.Response, bool) {
		system := ""
		if len(messages) > 0 {
			system = messageText(messages[0])
		}
		if !strings.HasPrefix(system, noticeSystemPrefix) {
			return nil, false
		}
		shown := ""
		if len(messages) > 1 {
			shown = messageText(messages[1])
		}
		fixture.mu.Lock()
		fixture.asked = append(fixture.asked, strings.Contains(system, "FIRST, keep"))
		fixture.mu.Unlock()
		switch {
		case strings.Contains(shown, "deploy on Tuesdays"):
			return textResponse(`{"keep":"deploys on Tuesdays","scope":"project","durability":"decision"}`), true
		case strings.Contains(shown, "short answers"):
			return textResponse(`{"keep":"prefers short answers","scope":"user","durability":"standing_rule"}`), true
		}
		return textResponse(`{"keep":"` + badReading + `","scope":"user","durability":"standing_rule"}`), true
	}
	agent, brain := brainAgent(t, completer, func(config *Config) {
		config.ApprovalPolicy = promptAll()
		config.AskConsent = true
	})
	fixture.agent, fixture.brain, fixture.runs = agent, brain, make(chan string, 4)
	agent.tools = append(agent.tools, countingTool("touch", fixture.runs, nil))
	return fixture
}

func (f *noticingTurns) readings() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool(nil), f.asked...)
}

func (f *noticingTurns) kept(t *testing.T) []string {
	t.Helper()
	settleMemory(f.agent)
	found, err := f.brain.MemoriesFor([]string{store.OwnerUser, store.OwnerMachine})
	if err != nil {
		t.Fatal(err)
	}
	texts := make([]string, 0, len(found))
	for _, memory := range found {
		texts = append(texts, memory.Text)
	}
	return texts
}

// refusedTurn is a turn that calls one tool and closes with words about it.
func refusedTurn(closing string) []step {
	return []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-touch0", "touch", "{}"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(closing), nil
		},
	}
}

func dismissedAnswer(id uint64) Answer {
	return Answer{
		Kind: QuestionConsent, ID: id, Key: "3", Picked: []string{"3"},
		Change: "Dismissed from the Inbox without an answer.", Dismissed: true,
	}
}

// C1, C2, C3: a permission turned down, either way, keeps nothing about the
// person and tells the call what actually happened. C4: the next turn, where the
// person states a fact, is read for memory again and keeps it.
func TestARefusedPermissionIsNeverKeptAsARuleAndTheNextTurnStillNotices(t *testing.T) {
	cases := []struct {
		name    string
		answer  func(*Agent, Event) error
		told    string
		notTold string
	}{
		{
			name: "a single deny",
			answer: func(agent *Agent, request Event) error {
				agent.ResolveConsent(request.ID, false)
				return nil
			},
			told:    "denied by the person",
			notTold: "put this permission question away",
		},
		{
			name: "a dismissal from the Inbox",
			answer: func(agent *Agent, request Event) error {
				return agent.ResolveQuestion(dismissedAnswer(request.ID))
			},
			told:    "put this permission question away without answering it",
			notTold: "denied by the person",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			steps := append(refusedTurn("I could not run it: the shell command was denied, so I cannot run commands here."),
				func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("ok"), nil })
			fixture := newNoticingTurns(t, steps)

			events := drainAnswering(t, mustSubmit(t, fixture.agent, "touch the file"), func(request Event) {
				if err := tc.answer(fixture.agent, request); err != nil {
					t.Errorf("answering the permission: %v", err)
				}
			})
			if len(fixture.runs) != 0 {
				t.Fatal("a call whose permission was refused ran")
			}
			failed, ok := firstOfKind(events, EventToolFailed)
			if !ok {
				t.Fatalf("no failure event; got %v", kinds(events))
			}
			if !strings.Contains(failed.Output, tc.told) || strings.Contains(failed.Output, tc.notTold) {
				t.Fatalf("the call read %q, want it to say %q and not %q", failed.Output, tc.told, tc.notTold)
			}
			if strings.Contains(tc.name, "dismissal") && !strings.Contains(failed.Output, "ask again") {
				t.Fatalf("a dismissed call was not told it may ask again: %q", failed.Output)
			}
			// C1 / C3: whatever the reading model made of the closing words, a
			// turn that had a permission refused keeps nothing.
			if got := fixture.kept(t); len(got) != 0 {
				t.Fatalf("a refused permission became a remembered line: %v", got)
			}
			if got := fixture.readings(); len(got) != 1 || got[0] {
				t.Fatalf("the refused turn was read for memory: %v", got)
			}

			// C4: the mark belongs to one turn. What the person states next is kept.
			collect(t, mustSubmit(t, fixture.agent, "I prefer short answers"))
			if got := fixture.kept(t); len(got) != 1 || got[0] != "prefers short answers" {
				t.Fatalf("the next turn kept %v, want the one fact the person stated", got)
			}
			if got := fixture.readings(); len(got) != 2 || !got[1] {
				t.Fatalf("the next turn was not read for memory: %v", got)
			}
		})
	}
}

// C4: a turn whose call was allowed is read for memory as always.
func TestAnAllowedPermissionLeavesNoticingAlone(t *testing.T) {
	fixture := newNoticingTurns(t, refusedTurn("Done."))
	events := drainAnswering(t, mustSubmit(t, fixture.agent, "I always deploy on Tuesdays, so touch the file"), func(request Event) {
		fixture.agent.ResolveConsent(request.ID, true)
	})
	if len(fixture.runs) != 1 {
		t.Fatalf("an allowed call did not run; events %v", kinds(events))
	}
	if got := fixture.kept(t); len(got) != 1 || got[0] != "deploys on Tuesdays" {
		t.Fatalf("an allowed turn kept %v, want what the person stated", got)
	}
	if got := fixture.readings(); len(got) != 1 || !got[0] {
		t.Fatalf("an allowed turn was not read for memory: %v", got)
	}
}

// C2: a dismissal decides nothing, so it leaves nothing in the decision record
// that the model carries beside the real decisions, and the call parked on it
// learns it was put away. A deny is a decision and stays one.
func TestADismissedPermissionLeavesNoDecisionAndADenyLeavesOne(t *testing.T) {
	for _, dismissed := range []bool{true, false} {
		name := "a deny"
		if dismissed {
			name = "a dismissal"
		}
		t.Run(name, func(t *testing.T) {
			agent, _ := questionSession(t, "dismiss-"+strings.ReplaceAll(name, " ", "-"), nil)
			hub := newEventHub()
			events := hub.subscribe()
			asks, stopAsking := agent.WatchQuestions()
			defer stopAsking()

			answers := make(chan consentAnswer, 1)
			go func() {
				answer, err := agent.askAnswer(context.Background(), hub,
					ai.ToolCall{ID: "c1", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"rm -rf build/"}`}},
					approval.Decision{Action: approval.ActionPrompt, Rule: "bash always asks"})
				if err != nil {
					t.Errorf("the gate ended in an error: %v", err)
				}
				answers <- answer
			}()
			request := <-events
			if request.Kind != EventConsentRequest {
				t.Fatalf("the gate sent %v first", request.Kind)
			}
			waitForAsk(t, asks, EventQuestion)

			answer := Answer{Kind: QuestionConsent, ID: request.ID, Key: "3", Picked: []string{"3"}}
			if dismissed {
				answer = dismissedAnswer(request.ID)
			}
			if err := agent.ResolveQuestion(answer); err != nil {
				t.Fatalf("ResolveQuestion: %v", err)
			}
			got := <-answers
			if got.allow || got.dismissed != dismissed {
				t.Fatalf("the call was handed %+v, want allow=false dismissed=%v", got, dismissed)
			}
			records := agent.Decisions()
			if dismissed && len(records) != 0 {
				t.Fatalf("a dismissal was recorded as a decision: %+v", records)
			}
			if !dismissed && (len(records) != 1 || records[0].Words() != "deny") {
				t.Fatalf("a deny left %+v, want it recorded as the person's deny", records)
			}
			if lines := agent.personCardAnswerLines(); dismissed && len(lines) != 0 {
				t.Fatalf("a dismissal reached the completion check as the person's answer: %v", lines)
			}
		})
	}
}
