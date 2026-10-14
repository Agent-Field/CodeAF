package provider

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Concurrent request completions teach one process different facts. Once its
// scheduled writes have finished, a fresh process must recover every fact;
// atomic individual files alone do not establish that ordering.
func TestConcurrentQuirkSavesRetainEveryLearnedFactAtRestart(t *testing.T) {
	store := &quirksStore{mandatory: map[string]time.Time{}}
	path := filepath.Join(t.TempDir(), quirksFile)
	store.load(path)
	const facts = 32
	start := make(chan struct{})
	var learners sync.WaitGroup
	for i := 0; i < facts; i++ {
		learners.Add(1)
		go func(i int) {
			defer learners.Done()
			<-start
			if store.note(fmt.Sprintf("concurrent/model-%d", i), time.Now()) {
				store.persist()
			}
		}(i)
	}
	close(start)
	learners.Wait()
	store.settle()

	fresh := &quirksStore{mandatory: map[string]time.Time{}}
	fresh.load(path)
	for i := 0; i < facts; i++ {
		model := fmt.Sprintf("concurrent/model-%d", i)
		if !fresh.knows(model) {
			t.Errorf("a completed save lost %s at restart", model)
		}
	}
}
