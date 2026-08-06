package plan

import (
	"fmt"
	"sync"
	"testing"
)

func TestUseAnchorsConcurrent(t *testing.T) {
	UseAnchors("")
	t.Cleanup(func() { UseAnchors("") })

	const workers = 16
	start := make(chan struct{})
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			<-start
			for iteration := 0; iteration < 1_000; iteration++ {
				if worker%2 == 0 {
					UseAnchors(fmt.Sprintf("worker-%d-iteration-%d", worker, iteration))
					continue
				}
				if got := Anchors(); got == "" {
					errors <- fmt.Errorf("worker %d observed an empty ruler", worker)
					return
				}
			}
		}(worker)
	}
	close(start)
	group.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}
