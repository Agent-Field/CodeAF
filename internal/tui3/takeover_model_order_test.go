package tui3

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Only the arrival and selection doors are added to the ordinary surface
// interface, so unrelated standing task lanes do not participate in this test.
type heldResumeModelAgent struct {
	Agent
	engine      *session.Agent
	ctx         context.Context
	entered     chan struct{}
	release     chan struct{}
	refused     bool
	resumeCalls atomic.Int32
}

func (a *heldResumeModelAgent) ConfiguredModel() string { return a.engine.ConfiguredModel() }

func (a *heldResumeModelAgent) UseModelForNow(model string) error {
	close(a.entered)
	select {
	case <-a.release:
	case <-a.ctx.Done():
		return a.ctx.Err()
	}
	if a.refused {
		return errors.New("model selection refused")
	}
	return a.engine.UseModelForNow(model)
}

func (a *heldResumeModelAgent) AttachReplay() ([]session.DisplayEntry, <-chan session.Event, func()) {
	return a.engine.AttachReplay()
}

func (a *heldResumeModelAgent) ResumeStoppedTurn(ctx context.Context) (<-chan session.Event, bool) {
	a.resumeCalls.Add(1)
	return a.engine.ResumeStoppedTurn(ctx)
}

// C1, C4, C6; fix round 5: an unanswered arrival waits for its owning model
// selection before resuming once. Refusal permits the current model; canceling
// the wait preserves the journal's resume opportunity for a later attempt.
func TestTakeoverReplayResumeFollowsItsOrderedModel(t *testing.T) {
	for _, outcome := range []string{"accepted", "refused", "canceled"} {
		t.Run(outcome, func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			server := sourcestub.New("replacement")
			defer server.Close()
			sources := modelsource.NewSet(testDefaultService(""), testDirectService(server.URL()))
			dir := t.TempDir()
			path := filepath.Join(dir, "transcript.jsonl")
			const question = "answer the question already in the journal"
			journal := `{"type":"session","version":1,"id":"resume-model","model":"deepseek-direct/configured"}` + "\n" +
				`{"type":"message","role":"user","content":"` + question + `"}` + "\n" +
				`{"type":"error","error":{"door":"taken over","message":"turn ended: taken over"}}` + "\n" +
				`{"type":"title","title":"An unanswered question"}` + "\n"
			if err := os.WriteFile(path, []byte(journal), 0600); err != nil {
				t.Fatal(err)
			}
			engine, err := session.New(session.Config{Workspace: dir, SessionFile: path,
				Model: "deepseek-direct/configured", Sources: sources, System: "SYSTEM", DeterministicNames: true})
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			agent := &heldResumeModelAgent{Agent: engine, engine: engine, ctx: ctx,
				entered: make(chan struct{}), release: make(chan struct{}), refused: outcome == "refused"}
			unblock := sync.OnceFunc(func() { close(agent.release) })
			defer unblock()
			a := newApp(ctx, Options{Agent: &fakeAgent{model: "deepseek-direct/replacement"},
				ProfileDir: t.TempDir(), Workspace: dir, Sources: sources, RequireListedModel: true,
				ModelsForService: func(modelsource.Connected) []Model { return []Model{{ID: "replacement"}} },
			})
			defer a.doorLine.close()
			a.detachConversation()
			resumeCtx, cancelResume := context.WithCancel(ctx)
			defer cancelResume()
			a.ctx = resumeCtx
			attached := a.attachConversation(Conversation{Agent: agent, SessionFile: path, Workspace: dir, Resumed: true}, nil)
			select {
			case <-agent.entered:
			case <-ctx.Done():
				t.Fatal("arrival never asked for the listed replacement")
			}
			var resume tea.Cmd
			for _, msg := range runCmd(attached) {
				if answer, ok := msg.(doorMsg); ok {
					resume = a.doorSaid(answer)
				}
			}
			if a.hostReplayLoading || resume == nil || pendingModelSelection(agent) == nil ||
				a.model != "deepseek-direct/replacement" || engine.Model() != "deepseek-direct/configured" {
				t.Fatal("idle replay did not leave a resume behind the pending replacement")
			}
			if got := strings.TrimSpace(transcriptText(a)); got != question {
				t.Fatalf("arrival replay = %q, want the one journal question", got)
			}
			// Fold the replay without consuming the separate selection receipt:
			// completing the engine ask alone must release this command.
			done := make(chan []tea.Msg, 1)
			go func() { done <- runCmd(resume) }()
			select {
			case <-done:
				t.Fatal("resume command finished before model selection")
			case <-time.After(30 * time.Millisecond):
			}
			if agent.resumeCalls.Load() != 0 || len(completionRequests(server)) != 0 {
				t.Fatal("resume entered the engine before model selection completed")
			}
			if outcome == "canceled" {
				cancelResume()
				select {
				case msgs := <-done:
					if len(msgs) != 0 || agent.resumeCalls.Load() != 0 || pendingModelSelection(agent) == nil {
						t.Fatal("canceled wait consumed the resume opportunity or the independent selection")
					}
				case <-ctx.Done():
					t.Fatal("canceled resume did not leave the model wait")
				}
			}
			unblock()
			var msgs []tea.Msg
			if outcome == "canceled" {
				acknowledgeModelChanges(t, a)
				if agent.resumeCalls.Load() != 0 || len(completionRequests(server)) != 0 {
					t.Fatal("selection receipt resurrected a canceled resume")
				}
				a.ctx = ctx
				msgs = runCmd(a.resumeStoppedTurn())
			} else {
				select {
				case msgs = <-done:
				case <-ctx.Done():
					t.Fatal("resume did not follow the completed model ask")
				}
			}
			if len(msgs) != 1 || agent.resumeCalls.Load() != 1 {
				t.Fatalf("resume calls = %d, messages = %v", agent.resumeCalls.Load(), msgs)
			}
			resumed, ok := msgs[0].(resumedTurnMsg)
			if !ok || resumed.ch == nil {
				t.Fatalf("arrival did not start its unanswered turn: %v", msgs)
			}
			drainResumedModelTurn(t, ctx, resumed.ch)
			acknowledgeModelChanges(t, a)
			if again := runCmd(a.resumeStoppedTurn()); len(again) != 0 {
				t.Fatalf("arrival resumed the same question twice: %v", again)
			}
			want := "replacement"
			if outcome == "refused" {
				want = "configured"
			}
			requests := completionRequests(server)
			if len(requests) != 1 || completionModel(t, requests[0]) != want {
				t.Fatalf("resumed provider requests = %+v, want one on %q", requests, want)
			}
			var said []string
			saved, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range strings.Split(strings.TrimSpace(string(saved)), "\n") {
				var entry struct {
					Type, Role, Content string
					Note                bool
				}
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatal(err)
				}
				if entry.Type == "message" && entry.Role == "user" && !entry.Note {
					said = append(said, entry.Content)
				}
			}
			if len(said) != 1 || said[0] != question || engine.ConfiguredModel() != "deepseek-direct/configured" {
				t.Fatalf("resume duplicated the journal question or saved the replacement: %q", said)
			}
		})
	}
}

func drainResumedModelTurn(t *testing.T, ctx context.Context, events <-chan session.Event) {
	t.Helper()
	finished := false
	for {
		select {
		case event, ok := <-events:
			if !ok {
				if !finished {
					t.Fatal("resumed turn closed without finishing")
				}
				return
			}
			if event.Kind == session.EventError {
				t.Fatalf("resumed turn failed: %+v", event)
			}
			finished = finished || event.Kind == session.EventTurnDone
		case <-ctx.Done():
			t.Fatal("resumed turn exceeded its deadline")
		}
	}
}
