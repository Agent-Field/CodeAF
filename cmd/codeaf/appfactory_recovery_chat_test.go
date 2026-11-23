package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestAppFactoryDismissedRecoveryRetriesFromChat(t *testing.T) {
	s := newLocalSetup(t)
	conv := &appStatusConversation{&fakeConversation{model: "test/model"}}
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	local := s.start()
	floor := s.backend.local.host.floor
	if err := floor.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	item, err := floor.Create(factory.Item{
		Kind: factory.KindIssue, Repo: "acme/api", Num: 23, Title: "Test retries", Tier: factory.TierOwner,
		State: factory.StateNeedsYou, QKind: factory.QKindOutOfAttempts, Question: "Test did not pass",
		Cap: 5, WorktreeMachine: "other-fixture-worker",
		Stages: []factory.Stage{{Name: "test", Kind: factory.StageChat, On: true, Max: 3}},
		Stream: &factory.Stream{Spent: .5, Phases: []factory.Phase{{Name: "test", Kind: factory.StageChat, State: factory.PhaseWaiting, Round: 3}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	makeJobManager(t, s, item.ID)
	params := map[string]any{"factory": local.Factory, "job": item.ID}
	var chats appwire.FactoryJobChats
	s.must("factory.job_chats", params, &chats)
	read := func() factory.Item {
		t.Helper()
		got, err := floor.Get(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	send := func(text, clientID string) {
		t.Helper()
		s.must("agents.send", map[string]any{"id": chats.Agent, "text": text, "client_id": clientID}, nil)
	}
	var before api.Job
	s.must("factory.job", params, &before)
	if !before.Can.Hold {
		t.Fatal("stuck Test did not offer pause")
	}
	params["expect"] = map[string]any{"version": before.Version, "state": before.State, "step": "test"}
	var acted appwire.FactoryJobActed
	s.must("factory.job_hold", params, &acted)
	paused := read()
	if got := factory.StatusForManager(paused, false, time.Now()).Text; !strings.Contains(got, "test is paused") || !strings.Contains(got, "Try again") || strings.Contains(got, "factory_answer") {
		t.Fatalf("paused manager status = %q", got)
	}
	// Older manager journals have no ownership sidecar. The job's saved
	// conversation still names the only chat that can resume it.
	if err := os.Remove(filepath.Join(filepath.Dir(paused.Talk), factoryOwnerFile)); err != nil {
		t.Fatal(err)
	}
	send("Is it paused?", "check")
	if got := read(); got.State != factory.StateNeedsYou || got.Question != item.Question || !factory.RecoveryPaused(got) {
		t.Fatalf("status cleared pause or question: %+v", got)
	}
	const words = "Try again: compare the focused tests on the base"
	send(words, "retry-1")
	got := read()
	if got.State != factory.StateQueued || got.Stream.Phases[0].Round != 4 || got.Stream.Spent != .5 || len(got.Notes) != 1 || got.Notes[0] != "compare the focused tests on the base" || factory.RecoveryPaused(got) {
		t.Fatalf("retry did not queue Test with its context: %+v", got)
	}
	send(words, "retry-1")
	if got := read(); got.Stream.Phases[0].Round != 4 || len(got.Notes) != 1 {
		t.Fatalf("replayed chat message repeated retry: %+v", got)
	}
	entries, _, err := s.backend.Entries(chats.Agent, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	var user, ack int
	for _, e := range entries {
		if e.ClientID == "retry-1" {
			user++
		}
		if e.NoticeKind == "factory_status" && strings.HasPrefix(e.Text, "Retry requested.") {
			ack++
		}
	}
	conv.mu.Lock()
	submits := conv.submits
	conv.mu.Unlock()
	if user != 1 || ack != 1 || submits != 0 {
		t.Fatalf("chat receipt: messages=%d acknowledgements=%d model calls=%d", user, ack, submits)
	}

	// Losing the chat receipt after the job commit cannot acknowledge
	// different instructions under the same message ID or repeat the retry.
	if err := os.Remove(filepath.Join(session.DirOf(got.Talk), appFootersName)); err != nil {
		t.Fatal(err)
	}
	s.mustFail("agents.send", map[string]any{"id": chats.Agent, "text": "Try again: changed note", "client_id": "retry-1"}, appwire.CodeConflict)
	send(words, "retry-1")
	if got := read(); got.Stream.Phases[0].Round != 4 || len(got.Notes) != 1 {
		t.Fatalf("missing chat receipt repeated retry: %+v", got)
	}
}

func TestAppFactoryDismissedMergeResumesFromChat(t *testing.T) {
	s := newLocalSetup(t)
	conv := &appStatusConversation{&fakeConversation{model: "test/model"}}
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	local := s.start()
	floor := s.backend.local.host.floor
	if err := floor.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	item, err := floor.Create(factory.Item{Kind: factory.KindIssue, Repo: "acme/api", Num: 23, Title: "Opened a PR",
		State: factory.StateShipped, Publish: "pr", HeadCommit: "head", WorktreeMachine: "other-fixture-worker",
		PullRequest: &factory.PullRequest{Number: 24, State: factory.PROpen, HeadSHA: "head"},
		FollowUp:    &factory.PRFollowUp{Kind: "pr", State: "watching", HeadSHA: "head"}, Cap: 5})
	if err != nil {
		t.Fatal(err)
	}
	makeJobManager(t, s, item.ID)
	params := map[string]any{"factory": local.Factory, "job": item.ID}
	var chats appwire.FactoryJobChats
	var before api.Job
	s.must("factory.job_chats", params, &chats)
	s.must("factory.job", params, &before)
	if !before.Can.Hold {
		t.Fatal("the open PR decision did not offer pause")
	}
	params["expect"] = map[string]any{"version": before.Version, "state": before.State}
	var acted appwire.FactoryJobActed
	s.must("factory.job_hold", params, &acted)
	s.must("agents.send", map[string]any{"id": chats.Agent, "text": "Is it paused?", "client_id": "status"}, nil)
	paused, err := floor.Get(item.ID)
	if err != nil || !factory.PausedDecision(paused) || paused.Merging != nil {
		t.Fatalf("status failed to preserve the paused merge decision: %v", err)
	}
	message := map[string]any{"id": chats.Agent, "text": "Resume", "client_id": "resume"}
	s.must("agents.send", message, nil)
	s.must("agents.send", message, nil)
	got, err := floor.Get(item.ID)
	if err != nil || factory.PausedDecision(got) || got.Merging != nil || got.State != factory.StateShipped ||
		got.FollowUp == nil || got.FollowUp.State != "watching" || got.PullRequest.State != factory.PROpen {
		t.Fatalf("Resume did not restore watching without merging: %v", err)
	}
	entries, _, err := s.backend.Entries(chats.Agent, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	var receipts int
	for _, e := range entries {
		if e.NoticeKind == "factory_status" && strings.HasPrefix(e.Text, "Pause lifted.") {
			receipts++
		}
	}
	if receipts != 1 {
		t.Fatalf("the same Resume produced %d acknowledgements", receipts)
	}
}

func TestRecoveryChatCommand(t *testing.T) {
	for _, c := range []struct {
		text string
		note string
		ok   bool
	}{
		{"Try again", "", true},
		{" Please resume! ", "", true},
		{"Retry: compare the base\nfirst", "compare the base\nfirst", true},
		{"Try again?", "", false},
		{"Why did it retry?", "", false},
		{"Do not try again", "", false},
		{"The error was: try again", "", false},
	} {
		if note, ok := recoveryChatCommand(c.text); note != c.note || ok != c.ok {
			t.Errorf("%q: got %q, %t, want %q, %t", c.text, note, ok, c.note, c.ok)
		}
	}
}
