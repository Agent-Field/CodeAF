package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func TestModelMetaC1RemembersProviderBeforeTheFirstCall(t *testing.T) {
	dir := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
		cfg.Place = Place{Dir: dir}
		cfg.Sources = modelsource.NewSet(
			modelsource.Connected{Source: modelsource.Source{ID: "openrouter", Written: "openrouter"}, Key: "router"},
			modelsource.Connected{Source: modelsource.Source{ID: "custom-local", Written: "local-models"}, Key: "local"},
		)
	})
	agent.SetModel("local-models/never-called")
	agent.SettleWrites()
	raw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["model"] != "local-models/never-called" || saved["modelProvider"] != "custom-local" {
		t.Fatalf("C1 saved model/provider = %v/%v", saved["model"], saved["modelProvider"])
	}
}

// A different window must see a chosen model even before another turn runs.
func TestModelChoicePublishesSavedConfigurationWithoutAnotherTurn(t *testing.T) {
	dir := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Place = Place{Dir: dir}
	})
	for _, model := range []string{"vendor/first", "vendor/second"} {
		agent.SetModel(model)
		agent.SettleWrites()
		meta, err := LoadMeta(dir)
		if err != nil || meta.Model != agent.Model() || meta.Model != model {
			t.Fatalf("saved model = %q, live = %q, error = %v", meta.Model, agent.Model(), err)
		}
	}
}

// A held metadata lock must never hold the model picker or lose a user stamp.
func TestModelChoiceDoesNotWaitForMetadataAndPreservesTheUserStamp(t *testing.T) {
	dir := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Place = Place{Dir: dir}
	})
	value, _ := metaLocks.LoadOrStore(dir, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	locked := true
	defer func() {
		if locked {
			lock.Unlock()
		}
	}()
	agent.mu.Lock()
	agent.stampUserLocked("Preserve this opening")
	agent.mu.Unlock()
	done := make(chan struct{})
	go func() {
		agent.SetModel("vendor/first")
		agent.SetModel("vendor/newest")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("model choice waited for the metadata lock")
	}
	lock.Unlock()
	locked = false
	agent.SettleWrites()
	meta, err := LoadMeta(dir)
	if err != nil || meta.Model != "vendor/newest" || meta.LastUserAt.IsZero() || meta.Title != "Preserve this opening" {
		t.Fatalf("model choice lost saved configuration or user stamp: %+v, %v", meta, err)
	}
}
