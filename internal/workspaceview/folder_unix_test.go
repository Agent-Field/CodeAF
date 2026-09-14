//go:build unix

package workspaceview

import (
	"context"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// A NAMED PIPE A FOLDER NAMES IS REFUSED, AND THE REFUSAL DOES NOT WAIT for a
// writer that will never come. On an engine a blocked open would stall every
// later call on the connection, so this is a liveness law and not a nicety.
func TestAnArtifactPreviewRefusesANamedPipeWithoutWaiting(t *testing.T) {
	home := newHome(t)
	ctx := context.Background()
	folder := home.collection(t, "Product")
	pipe := filepath.Join(home.root, "pipe")
	must(syscall.Mkfifo(pipe, 0o600))
	must(home.collections.Add(ctx, folder, workspace.Ref{Kind: workspace.ArtifactKind, ID: pipe}))
	refused := make(chan error, 1)
	go func() { _, err := home.resolver().Artifact(ctx, home.collections, pipe); refused <- err }()
	select {
	case err := <-refused:
		if err == nil || !strings.Contains(err.Error(), "not a plain file") {
			t.Fatalf("a named pipe previewed as %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("previewing a named pipe blocked")
	}
}
