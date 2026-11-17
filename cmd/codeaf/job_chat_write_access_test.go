package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Job chat without write access refuses the turn before a model call.
func TestStrangerJobChatFollowUpIsRefused(t *testing.T) {
	st, talk := chatSpendFloor(t)
	if err := os.MkdirAll(filepath.Dir(talk), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create(factory.Item{Title: "outside issue", Repo: "acme/api", Kind: factory.KindIssue, Tier: factory.TierStranger, Talk: talk, Cap: 5}); err != nil {
		t.Fatal(err)
	}
	var writable atomic.Bool
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		var body struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for _, tool := range body.Tools {
			if tool.Function.Name == "write" || tool.Function.Name == "bash" {
				writable.Store(true)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	a, err := session.New(session.Config{Workspace: t.TempDir(), ProfileDir: t.TempDir(), SessionFile: talk, Model: "fixture/manager", APIKey: "fixture", BaseURL: server.URL, OneModel: true, DeterministicNames: true, JobChat: jobChatDoorOf(st)})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, door := range []string{"FollowUp", "FollowUpMessage", "FollowUpRequiring", "Submit"} {
		t.Run(door, func(t *testing.T) {
			refused := false
			var events <-chan session.Event
			var err error
			switch door {
			case "FollowUp":
				events, err = a.FollowUp("Explain the job.")
			case "FollowUpMessage":
				events, err = a.FollowUpMessage(session.Submission{Text: "Explain the job."})
			case "FollowUpRequiring":
				events, err = a.FollowUpRequiring("Explain the job.", "factory_status")
			case "Submit":
				events, err = a.Submit(context.Background(), "Explain the job.")
			}
			if err != nil {
				if err.Error() != (factoryrun.WriteAccessRefusal{}).Error() {
					t.Fatal(err)
				}
				refused = true
			}
			if events != nil {
				for event := range events {
					if event.Kind == session.EventError {
						if event.Err == nil || !strings.Contains(event.Err.Error(), (factoryrun.WriteAccessRefusal{}).Error()) {
							t.Fatalf("chat refusal=%v", event.Err)
						}
						refused = true
					}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := a.WaitIdle(ctx); err != nil {
				t.Fatal(err)
			}
			if writable.Load() {
				t.Fatal("job chat must not expose writing tools")
			}
			if !refused || requests.Load() != 0 {
				t.Fatalf("refused=%v requests=%d", refused, requests.Load())
			}
		})
	}
	items, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if factory.JobSpent(items[0]) != 0 {
		t.Fatal("refused chat spent money")
	}
}

// The app refuses chat before making or opening a conversation.
func TestAppJobChatWithoutWriteAccessIsDefinite(t *testing.T) {
	s := newLocalSetup(t)
	opened := 0
	s.backend.open = func(ask appOpenAsk) (*appConn, error) { opened++; return nil, errors.New("unexpected conversation") }
	status := s.start()
	floor := s.backend.local.host.floor
	it, err := floor.Create(factory.Item{Repo: "acme/api", Title: "outside job", Kind: factory.KindIssue, Tier: factory.TierStranger, State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.backend.FactoryJobSend(context.Background(), appwire.FactoryJobSendParams{Factory: status.Factory, Job: it.ID, Text: "Explain this job."})
	var refusal *appwire.Error
	if !errors.As(err, &refusal) || refusal.Code != appwire.CodeConflict || refusal.Message != (factoryrun.WriteAccessRefusal{}).Error() {
		t.Fatalf("app refusal=%v", err)
	}
	saved, err := floor.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if opened != 0 || saved.Talk != "" || factory.JobSpent(saved) != 0 {
		t.Fatal("refused chat started work")
	}
	var chats appwire.FactoryJobChats
	s.must("factory.job_chats", map[string]any{"factory": status.Factory, "job": it.ID}, &chats)
	if chats.Agent != "" || opened != 0 {
		t.Fatal("empty chat read opened a conversation")
	}
}

// The manager's durable read views remain available without starting a turn.
func TestStrangerManagerReadViewsRemainAvailable(t *testing.T) {
	st, web, _ := talkLab(t)
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	it, err := st.Create(factory.Item{Repo: "web", Title: "outside job", Kind: factory.KindIssue, Tier: factory.TierStranger, Talk: file})
	if err != nil {
		t.Fatal(err)
	}
	before, err := st.Revision(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	door := runDoor(st, web, "").(session.ManagerStatusDoor)
	status, err := door.ManagerStatus(context.Background(), file)
	if err != nil || status.Text == "" {
		t.Fatalf("manager status=%+v error=%v", status, err)
	}
	after, err := st.Revision(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("manager read changed the job")
	}
}

// A manager turn needs write access before its conversation opens.
func TestStrangerManagerTurnIsRefusedBeforeOpening(t *testing.T) {
	var asked []string
	opened := 0
	turns := &shapeTurns{open: func(context.Context, factory.Item, session.RunDoor, bool) (managerTurn, error) {
		opened++
		return &fakeManagerTurn{door: &keptDoor{}, asked: &asked}, nil
	}}
	_, err := turns.through(context.Background(), factory.Item{Tier: factory.TierStranger}, "Explain this job.", nil)
	var refusal factoryrun.WriteAccessRefusal
	if !errors.As(err, &refusal) || opened != 0 || len(asked) != 0 {
		t.Fatalf("manager refusal=%v opened=%d turns=%d", err, opened, len(asked))
	}
}
