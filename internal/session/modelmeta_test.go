package session

import (
	"sync"
	"testing"
	"time"
)

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
