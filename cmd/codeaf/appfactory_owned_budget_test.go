package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A competing manager remains job-owned even when another conversation wins Talk.
func TestFactoryOwnedManagerChargesAfterTalkMoves(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(strconv.FormatBool(collision), func(t *testing.T) {
			s := newLocalSetup(t)
			var calls atomic.Int64
			server := moneyProvider(t, &calls)
			st := s.start()
			floor := s.backend.local.host.floor
			s.backend.open = func(ask appOpenAsk) (*appConn, error) {
				return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
			}
			it, err := floor.Create(factory.Item{Repo: "acme/api", Num: 7, Title: "Owned manager", Kind: factory.KindIssue, State: factory.StateNeedsYou, Cap: .005})
			if err != nil {
				t.Fatal(err)
			}
			orphan, err := talkMaker(floor, t.TempDir(), filepath.Join(s.home, "profile"))(context.Background(), it)
			if err != nil {
				t.Fatal(err)
			}
			var chats appwire.FactoryJobChats
			s.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": it.ID}, &chats)
			if collision {
				mirror, err := store.Open(filepath.Join(floor.Root(), "workers", strconv.Itoa(os.Getpid()), "other-factory"))
				if err != nil {
					t.Fatal(err)
				}
				foreign := it
				foreign.Talk = filepath.Join(t.TempDir(), "foreign.jsonl")
				foreign.Cap = .0001
				foreign.ChatSpent = .0001
				if err := mirror.Save(foreign); err != nil {
					t.Fatal(err)
				}
			}

			a := moneyAgent(t, s, appOpenAsk{Workspace: filepath.Join(floor.Root(), "chats"), Transcript: orphan}, server.URL)
			defer a.Close()
			ask := session.ErrandAsk{Purpose: "ownership", Role: "brief", System: "Answer briefly.", Messages: []session.ErrandMessage{{Role: "user", Text: "Check ownership."}}, MaxTokens: 100}
			answer, err := a.RunErrand(context.Background(), ask)
			if err != nil || answer.Reason != "" {
				t.Fatalf("first call: %+v %v", answer, err)
			}
			moneyTotals(t, s, st.Factory, it, &calls, .01)
			_, _ = a.RunErrand(context.Background(), ask)
			if calls.Load() != 1 {
				t.Fatalf("job cap allowed %d provider calls, want 1", calls.Load())
			}

		})
	}
}
