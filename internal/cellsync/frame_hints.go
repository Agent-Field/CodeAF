package cellsync

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/directory"
	"os"
)

const frameHintBudget = directory.FrameHintBudget

// latestFrameHints uses actual final uploaded frame sizes, including one reserved
// EOF/oversize probe per frame. Larger uploads simply have no warm optimization.
func latestFrameHints(frames []FrameFile, ids []string) []directory.FrameHint {
	if len(frames) == 0 || len(frames) > directory.MaxFrameHints || len(frames) != len(ids) {
		return nil
	}
	out := make([]directory.FrameHint, 0, len(frames))
	remaining := frameHintBudget
	for i, f := range frames {
		if f.Bytes <= 0 || f.Bytes >= remaining || !blobstore.ValidRID(ids[i]) {
			return nil
		}
		remaining -= f.Bytes + 1
		out = append(out, directory.FrameHint{ID: ids[i], Bytes: f.Bytes})
	}
	return out
}
func validWarmHints(hints []directory.FrameHint) bool {
	if len(hints) == 0 || len(hints) > directory.MaxFrameHints {
		return false
	}
	remaining := frameHintBudget
	seen := map[string]bool{}
	for _, hint := range hints {
		if hint.Bytes <= 0 || hint.Bytes >= remaining || !blobstore.ValidRID(hint.ID) || seen[hint.ID] {
			return false
		}
		remaining -= hint.Bytes + 1
		seen[hint.ID] = true
	}
	return true
}
func (f *Fetcher) primeWarm(ctx context.Context, c cell.Cell, head, inbox string) error {
	want, err := f.Engine.Want(ctx, c, head)
	if err != nil {
		return err
	}
	if len(want) == 0 {
		return nil
	}
	view, err := f.Dir.Cell(ctx, c.ID)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || view.Cell.Head != head || !validWarmHints(view.Cell.FrameHints) {
		return nil
	}
	for _, hint := range view.Cell.FrameHints {
		raw, err := blobstore.ReadFrameBounded(ctx, f.Store, hint.ID, hint.Bytes)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return nil
		}
		_, objects, err := blobstore.Decode(raw)
		if err != nil {
			return nil
		}
		if err := keep(inbox, objects); err != nil {
			return err
		}
	}
	_, err = f.Engine.ImportPrimed(ctx, c, head, inbox)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		clearInbox(inbox)
	}
	return nil
}

// ensureHintInbox gives every attempt an exclusive directory. No attempt can
// clear another fetcher's primed files while the native import reads them.
func ensureHintInbox(parent string) (string, error) { return os.MkdirTemp(parent, ".frame-prime-") }
