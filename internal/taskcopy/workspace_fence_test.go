package taskcopy_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

func TestWorkspaceCheckpointFenceHonorsCancellationAndReleases(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	c := cell.Cell{Root: t.TempDir()}
	release, err := taskcopy.LockWorkspaceCheckpoint(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		unlock, err := taskcopy.LockWorkspaceCheckpoint(ctx, c)
		if unlock != nil {
			unlock()
		}
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("competing checkpoint writer returned %v; want canceled", err)
	}
	release()
	unlock, err := taskcopy.LockWorkspaceCheckpoint(context.Background(), c)
	if err != nil {
		t.Fatalf("released checkpoint stayed locked: %v", err)
	}
	unlock()
}

func TestWorkspaceCheckpointFenceCanceledBeforeIOAndOrdinaryProfileSerialized(t *testing.T) {
	c := cell.Cell{Root: t.TempDir()}
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := taskcopy.LockWorkspaceCheckpoint(ctx, c); !errors.Is(err, context.Canceled) {
		t.Fatalf("already canceled: %v", err)
	}
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "0")
	unlock, err := taskcopy.LockWorkspaceCheckpoint(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if _, err := os.Stat(filepath.Join(c.Root, cell.TaskDropping)); err != nil {
		t.Fatalf("ordinary profile has no checkpoint fence: %v", err)
	}
}
