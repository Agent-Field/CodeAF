package syncsetup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

// backfillMu keeps one backfill pass at a time in a process. Another process
// of the same home may run its own: every step is idempotent, so the worst it
// costs is a Has request that finds the frame already there.
var backfillMu sync.Mutex

// BackfillLive is what happens to chats that were shared live when the
// account's storage is approved: each frame in this computer's live store that
// the relay does not hold is uploaded, once, so a computer can then take them
// while this one is off. Until it finishes, a take keeps asking this computer
// for what the relay still lacks (see [liveStore]).
//
// It does nothing while storage is closed, and when it has uploaded everything
// it removes the marker that kept the door open for the purpose. It stops at the
// first failure and leaves the marker, so the next look finishes the work.
func (s *Sync) BackfillLive(ctx context.Context) error {
	if !s.StorageAllowed() || !livePending(s.Home) {
		return nil
	}
	backfillMu.Lock()
	defer backfillMu.Unlock()
	disk, err := s.liveDisk()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(liveStoreDir(s.Home), "frames"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if err := s.backfillFrame(ctx, disk, entry.Name()); err != nil {
			return err
		}
	}
	if err := os.Remove(liveMarkerPath(s.Home)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// backfillFrame sends one frame unless the relay already holds all its objects.
func (s *Sync) backfillFrame(ctx context.Context, disk *blobstore.Disk, id string) error {
	frame, err := disk.GetFrame(ctx, id)
	if err != nil {
		return err
	}
	header, _, err := blobstore.Decode(frame)
	if err != nil {
		return err
	}
	rids := make([]string, len(header.Objects))
	for i, ref := range header.Objects {
		rids[i] = ref.RID
	}
	for len(rids) > 0 {
		n := min(blobstore.MaxHas, len(rids))
		have, err := s.Store.Has(ctx, rids[:n])
		if err != nil {
			return err
		}
		for _, held := range have {
			if !held {
				_, err := s.Store.PutFrame(ctx, frame)
				if errors.Is(err, blobstore.ErrConflict) {
					// The relay holds one of these objects with other bytes, which
					// a content-addressed store has only when the same chat was
					// written twice. Keep the relay's copy and go on: one frame
					// that cannot be uploaded must not hold back the rest of the
					// chats a computer was waiting for.
					return nil
				}
				return err
			}
		}
		rids = rids[n:]
	}
	return nil
}
