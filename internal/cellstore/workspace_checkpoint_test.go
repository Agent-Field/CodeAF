package cellstore_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

type workspaceFenceTransport func(context.Context, cellstore.Target, cellstore.Op) ([]byte, error)

func (f workspaceFenceTransport) Do(ctx context.Context, target cellstore.Target, op cellstore.Op) ([]byte, error) {
	return f(ctx, target, op)
}

type workspaceFenceComposer func(cell.Cell) error

func (f workspaceFenceComposer) Compose(c cell.Cell) error { return f(c) }

func TestWorkspaceCheckpointFenceCoversCompositionAndNativeCapture(t *testing.T) {
	for _, capture := range []bool{false, true} {
		t.Run(map[bool]string{false: "seal", true: "capture"}[capture], func(t *testing.T) {
			t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
			c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
			if err != nil {
				t.Fatal(err)
			}
			assertWriterWaits := func(at string) {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
				defer cancel()
				unlock, err := taskcopy.LockWorkspaceCheckpoint(ctx, c)
				if unlock != nil {
					unlock()
				}
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("checkpoint writer could change state during %s: %v", at, err)
				}
			}
			composed, captured := false, false
			engine := cellstore.Engine{DataRoot: t.TempDir(), Workspace: t.TempDir(),
				Compose: workspaceFenceComposer(func(cell.Cell) error {
					composed = true
					assertWriterWaits("composition")
					return nil
				}),
				Transport: workspaceFenceTransport(func(_ context.Context, _ cellstore.Target, op cellstore.Op) ([]byte, error) {
					if op.Verb() == "seal" {
						captured = true
						assertWriterWaits("native capture")
					}
					if op.Verb() == "capture-plan" {
						return []byte(`{"token":"fixture-plan","full":true,"paths":[]}`), nil
					}
					return []byte(`{"snapshot":"` + strings.Repeat("a", 64) + `","durable":true}`), nil
				}),
			}
			if capture {
				_, err = engine.Capture(context.Background(), c)
			} else {
				_, err = engine.Seal(context.Background(), c, cellstore.TurnInfo{})
			}
			if err != nil || !composed || !captured {
				t.Fatalf("capture path not exercised: composed=%v captured=%v error=%v", composed, captured, err)
			}
		})
	}
}
