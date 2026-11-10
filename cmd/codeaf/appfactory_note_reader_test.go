package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The scripted model calls the real tool and keeps the tool message on its
// next request. Its final answer is fixed; only the tool's words are tested.
func TestAppFactoryNoteNamesItsNextReader(t *testing.T) {
	for _, c := range []struct {
		name                     string
		remaining, deterministic bool
	}{
		{name: "all steps done"},
		{name: "review remains", remaining: true},
		{name: "only check and publish remain", deterministic: true},
		{name: "check before review", remaining: true, deterministic: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			remaining := c.remaining
			s := newLocalSetup(t)
			status := s.start()
			phases := []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhaseDone}, {Name: "review", Kind: factory.StageChat, State: factory.PhaseDone}, {Name: "proof", Kind: factory.StageChat, State: factory.PhaseDone}}
			if remaining {
				phases[1].State = factory.PhasePending
				phases[2].State = factory.PhasePending
			}
			stages := []factory.Stage{{Name: "write", On: true}, {Name: "review", On: true}, {Name: "proof", On: true}}
			if c.deterministic {
				stages = append(stages, factory.Stage{Name: "ci", Kind: factory.StageCheck, On: true}, factory.Stage{Name: "publish", Kind: factory.StagePost, On: true}, factory.Stage{Name: "approve", Kind: factory.StageGate, On: true})
				// Kind comes from the saved stage, as the runner resolves it, even
				// when the phase's old kind is absent.
				phases = append([]factory.Phase{{Name: "ci", State: factory.PhasePending}}, phases...)
				phases = append(phases, factory.Phase{Name: "publish", Kind: factory.StagePost, State: factory.PhasePending}, factory.Phase{Name: "approve", Kind: factory.StageGate, State: factory.PhaseWaiting})
			}
			it, err := s.backend.local.host.floor.Create(factory.Item{Tier: factory.TierOwner, Repo: "acme/api", Num: 21, Kind: factory.KindIssue, Title: "Fix the ledger", State: factory.StateNeedsYou, Stages: stages, Stream: &factory.Stream{Phases: phases}, PullRequest: &factory.PullRequest{Number: 42, URL: "https://github.com/acme/api/pull/42"}})
			if err != nil {
				t.Fatal(err)
			}
			received := make(chan string, 4)
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					Messages []struct {
						Role    string          `json:"role"`
						Content json.RawMessage `json:"content"`
					} `json:"messages"`
					Stream bool `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
				result := ""
				for _, m := range in.Messages {
					if m.Role == "tool" {
						result = string(m.Content)
					}
				}
				message := `"content":"done"`
				finish := "stop"
				if result == "" {
					args := fmt.Sprintf(`{"item":%d,"note":"Please explain the retry limit","why":"keep the person's words"}`, it.ID)
					message = `"content":"","tool_calls":[{"index":0,"id":"note-call","type":"function","function":{"name":"factory_item","arguments":` + strconv.Quote(args) + `}}]`
					finish = "tool_calls"
				} else {
					select {
					case received <- result:
					default:
					}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",%s},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", message, finish)
			}))
			defer model.Close()
			s.backend.open = func(ask appOpenAsk) (*appConn, error) {
				a, err := session.New(session.Config{Workspace: ask.Workspace, SessionFile: ask.Transcript, Model: "test/model", APIKey: "fake-key", BaseURL: model.URL, Interactive: true, AskConsent: true, FactoryItem: itemDoor(s.backend.local.host.floor, ask.Workspace)})
				if err != nil {
					return nil, err
				}
				t.Cleanup(func() { _ = a.Close() })
				return &appConn{Conv: a, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
			}
			makeJobManager(t, s, it.ID)
			var chats appwire.FactoryJobChats
			s.must("factory.job_chats", map[string]any{"factory": status.Factory, "job": it.ID}, &chats)
			s.must("agents.send", map[string]any{"id": chats.Agent, "text": "Add a note about the retry limit"}, nil)
			deadline := time.Now().Add(10 * time.Second)
			var asks appwire.AsksResult
			for time.Now().Before(deadline) {
				s.must("asks.list", map[string]any{"agent": chats.Agent}, &asks)
				if len(asks.Asks) > 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if len(asks.Asks) != 1 {
				t.Fatalf("note approval never appeared: %+v", asks)
			}
			ask := asks.Asks[0]
			s.must("asks.answer", map[string]any{"agent": chats.Agent, "id": ask.ID, "option": ask.Options[0].ID}, nil)
			var result string
			select {
			case result = <-received:
			case <-time.After(10 * time.Second):
				t.Fatal("model never received the note tool result")
			}
			saved, err := s.backend.local.host.floor.Get(it.ID)
			if err != nil || len(saved.Notes) != 1 || saved.Notes[0] != "Please explain the retry limit" {
				t.Fatalf("saved notes = %q, %v", saved.Notes, err)
			}
			wants := []string{"saved on the job"}
			if remaining {
				wants = append(wants, "review reads it next")
			} else {
				wants = append(wants, "no remaining step will read it", "not in the pull request", "comment on the pull request in GitHub")
				if strings.Contains(ask.Title, "for the stages") {
					t.Errorf("approval implies a reader: %s", ask.Title)
				}
				if strings.Contains(result, "factory.issue_comment") {
					t.Errorf("source issue comment tool presented as PR comment: %s", result)
				}
			}
			for _, want := range wants {
				if !strings.Contains(result, want) {
					t.Errorf("model's note result missing %q: %s", want, result)
				}
			}
		})
	}
}
