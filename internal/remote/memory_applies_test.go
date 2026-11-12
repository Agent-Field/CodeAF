package remote

import (
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
	"path/filepath"
	"testing"
)

func TestApplicableMemoryUsesTheRealRemoteSessionsConfiguredOwners(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer brain.Close()
	for _, owner := range []string{store.OwnerUser, store.OwnerMachine, store.OwnerProject("held"), store.OwnerProject("foreign")} {
		if _, err := brain.Write(store.WriteRequest{Owner: owner, Text: "remember this " + owner}); err != nil {
			t.Fatal(err)
		}
	}
	far, err := session.New(session.Config{Workspace: t.TempDir(), Model: "stub/no-model-call", APIKey: "fixture", BaseURL: "http://127.0.0.1:1/v1", System: "Fixture.", PromptProfile: "full", Memory: brain, MemoryProjectKey: "held"})
	if err != nil {
		t.Fatal(err)
	}
	defer far.Close()
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	got, err := loop.Client.Agent().ApplicableMemory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.Basis != "configured_live" || got.RidingKnown || got.Counts == nil || len(got.Counts.Scopes) != 3 {
		t.Fatalf("snapshot=%+v", got)
	}
	for n, owner := range []string{store.OwnerUser, store.OwnerMachine, store.OwnerProject("held")} {
		if got.Counts.Scopes[n].Owner != owner || got.Counts.Scopes[n].Count != 1 {
			t.Fatalf("scopes=%+v", got.Counts.Scopes)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loop.Client.Agent().ApplicableMemory(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestApplicableMemoryWithoutStoreDoesNotReportZeroApplicableRows(t *testing.T) {
	far, err := session.New(session.Config{Workspace: t.TempDir(), Model: "stub/no-model-call", APIKey: "fixture", BaseURL: "http://127.0.0.1:1/v1", System: "Fixture.", PromptProfile: "full"})
	if err != nil {
		t.Fatal(err)
	}
	defer far.Close()
	got, err := far.ApplicableMemory(context.Background())
	if err != nil || got.Available || got.Counts != nil || got.RidingKnown {
		t.Fatalf("snapshot=%+v err=%v", got, err)
	}
}
