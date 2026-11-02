package syncsetup

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestUnusedChatWaitsForConversationBeforeDirectoryPublication(t *testing.T) {
	r := newTwoHomes(t)
	ctx := t.Context()
	appendTo(t, transcriptOf(r.cell), `{"type":"session","version":1,"id":"unused"}`+"\n")
	d, err := r.a.Drive(ctx, r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	// The window seals a real bookkeeping head at startup and notes it again
	// when its asynchronous drive becomes ready.
	_, err = d.Store(r.engine).Seal(ctx, r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	d.CatchUp()
	mustNil(t, d.Close(ctx))
	if view, err := r.b.Dir.Cell(ctx, r.cell.ID); !errors.Is(err, directory.ErrNotFound) {
		t.Errorf("unused chat has a directory record: head=%s, err=%v", view.Cell.Head, err)
	}
	rows, err := r.b.Rows(ctx)
	mustNil(t, err)
	if len(rows) != 0 {
		t.Errorf("another computer lists an unused chat: %+v", rows)
	}
	d, err = r.a.Drive(ctx, r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"user","content":"hello"}`+"\n")
	_, err = d.Store(r.engine).Seal(ctx, r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	r.durable(r.cell.ID, r.cell)
	view, err := r.b.Dir.Cell(ctx, r.cell.ID)
	mustNil(t, err)
	if view.Cell.Head == "" || view.Cell.Lease.Device != r.a.Device.ID() {
		t.Errorf("used chat was not published and held: %+v", view)
	}
	mustNil(t, d.Close(ctx))
	view, err = r.b.Dir.Cell(ctx, r.cell.ID)
	mustNil(t, err)
	if view.Cell.Lease.Expires != 0 {
		t.Error("used chat did not release on close")
	}
}

func TestUnsealedFirstMessageDoesNotPublishTheStartupHead(t *testing.T) {
	r := newTwoHomes(t)
	ctx := t.Context()
	d, err := r.a.Drive(ctx, r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	_, err = r.engine.Seal(ctx, r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	// The first message can arrive before the asynchronous drive catches up
	// with the startup seal. That seal still has no conversation to continue.
	appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"user","content":"not saved yet"}`+"\n")
	d.CatchUp()
	mustNil(t, d.Close(ctx))
	if view, err := r.b.Dir.Cell(ctx, r.cell.ID); !errors.Is(err, directory.ErrNotFound) {
		t.Errorf("startup-only head was published using an unsealed message: head=%s, err=%v", view.Cell.Head, err)
	}
}
