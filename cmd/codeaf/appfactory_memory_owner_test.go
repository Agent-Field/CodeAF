package main

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestMemoryRecognizesOldJobOwnership(t *testing.T) {
	places, profile, conversation := bringBackFixture(t)
	floor, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := conversation("old-stage")
	conversation("ordinary")
	if err := markFactoryChat(floor, factory.Item{ID: 41, Repo: "acme/api"}, old); err != nil {
		t.Fatal(err)
	}
	journals, err := session.KnowledgeHistory(places, profile).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, journal := range journals {
		if journal.ID == old {
			if !journal.Job {
				t.Fatal("old job transcript with factory-owner.json was admitted as ordinary chat")
			}
			return
		}
	}
	t.Fatal("old job transcript missing from inventory")
}
