// This file covers the I/O shell ported from swe-pro/src/project at commit 3b25a1a.
package project

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestStoreCoalescesConcurrentLoadsAndBindsBootstrapContext(t *testing.T) {
	directory := t.TempDir()
	var discovers atomic.Int32
	var bootstraps atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	discover := DiscovererFunc(func(_ context.Context, directory string) (Info, string, error) {
		if discovers.Add(1) == 1 {
			close(started)
		}
		<-release
		return Info{ID: "p", Worktree: directory, Sandboxes: []string{directory}}, directory, nil
	})
	bootstrap := BootstrapFunc(func(ctx context.Context, instance InstanceContext) error {
		bootstraps.Add(1)
		got, ok := FromContext(ctx)
		if !ok || got.Directory != instance.Directory {
			t.Errorf("bootstrap context = %#v, %v", got, ok)
		}
		return nil
	})
	store := NewStore(discover, bootstrap)
	var wait sync.WaitGroup
	wait.Add(2)
	results := make(chan InstanceContext, 2)
	for index := range 2 {
		if index == 1 {
			<-started
		}
		go func() {
			defer wait.Done()
			value, err := store.Load(context.Background(), LoadInput{Directory: directory})
			if err != nil {
				t.Errorf("Load: %v", err)
				return
			}
			results <- value
		}()
	}
	close(release)
	wait.Wait()
	close(results)
	if discovers.Load() != 1 || bootstraps.Load() != 1 {
		t.Fatalf("duplicate work: discover=%d bootstrap=%d", discovers.Load(), bootstraps.Load())
	}
}
