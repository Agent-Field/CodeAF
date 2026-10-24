package syncsetup

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
)

type copyDirectory struct {
	directory.Client
	acquires int
}

func (d *copyDirectory) Acquire(ctx context.Context, id string, opt directory.AcquireOpts) (directory.CellView, error) {
	d.acquires++
	return d.Client.Acquire(ctx, id, opt)
}

// TestC29BackgroundCopyCannotTakeAChatThePersonOpened proves C29.2/C31.3:
// publication never acquires or releases an existing foreground conversation.
func TestC29BackgroundCopyCannotTakeAChatThePersonOpened(t *testing.T) {
	home := t.TempDir()
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	memory := directory.NewMemory(time.Now)
	dir := &copyDirectory{Client: memory.For("dev_a")}
	c := cell.Cell{ID: "01AAAAAAAAAAAAAAAAAAAAAAAA"}
	before, err := dir.Create(t.Context(), c.ID, directory.CellInit{Head: strings.Repeat("a", 64), Class: string(cell.HostBound)})
	if err != nil {
		t.Fatal(err)
	}
	s := Sync{Home: home, Identity: id, Dir: dir}
	if err := s.PublishCopy(t.Context(), cellstore.Engine{}, c, "computer", "title"); err != nil {
		t.Fatal(err)
	}
	after, err := dir.Cell(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dir.acquires != 0 || before.Cell.Lease.Fence != after.Cell.Lease.Fence || after.Cell.Lease.Expires == 0 {
		t.Fatal("background copy displaced or released the foreground chat")
	}
}
