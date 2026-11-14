package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

type r5cSilentDirectory struct {
	directory.Client
	t *testing.T
}

func (d r5cSilentDirectory) Cell(ctx context.Context, _ string) (directory.CellView, error) {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 2500*time.Millisecond {
		d.t.Error("free-open directory read must have a two-second deadline")
		return directory.CellView{}, context.DeadlineExceeded
	}
	<-ctx.Done()
	return directory.CellView{}, ctx.Err()
}

func TestIdleOpen_R5c_FreeTakeBoundsInitialRead(t *testing.T) {
	for _, roots := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "workspace choices"}[roots], func(t *testing.T) {
			s := &syncsetup.Sync{Dir: r5cSilentDirectory{t: t}}
			root := t.TempDir()
			c := s.Continuer(cellstore.Engine{}, syncsetup.TakeOptions{RootFor: func(string) string { return root }})
			var err error
			if roots {
				_, err = c.OpenAvailableWithWorkspaceRoots(t.Context(), "saved-chat", nil, map[string]string{"root": root})
			} else {
				_, err = c.OpenAvailable(t.Context(), "saved-chat", nil)
			}
			if !errors.Is(err, directory.ErrUnreachable) || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("silent directory must report an unreachable timeout: %v", err)
			}
		})
	}
}
