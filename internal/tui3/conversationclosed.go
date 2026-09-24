package tui3

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/session"
)

// saveConversationClosed gives tab gestures the same saved state as Home and
// Sessions. Unsaved tabs have no metadata, and a remote path never names a
// folder on this machine. This runs on navigation, never on a frame.
func (a *app) saveConversationClosed(key string, closed bool) error {
	if key == "" || a.hosted() {
		return nil
	}
	dir := filepath.Dir(key)
	meta, err := session.LoadMeta(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if meta.ID == "" || meta.ID != filepath.Base(dir) || meta.Archived == closed {
		return nil
	}
	return a.writeHomeArchived(session.SessionRow{Dir: dir, ID: meta.ID}, closed)
}
