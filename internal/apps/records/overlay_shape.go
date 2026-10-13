package records

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

var draftOverlayLocks sync.Map

// DraftOverlayLock serializes snapshot refresh and removal with record access.
// Shape and Records must share the lock because replacing an open SQLite file
// would let a write commit to the old inode and disappear from the draft.
func DraftOverlayLock(root string, scope svc.Scope) *sync.Mutex {
	key := filepath.Join(root, scope.App, "drafts", scope.Caller.Member, "overlay.db")
	lock, _ := draftOverlayLocks.LoadOrStore(key, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func (s *Service) prepareShapeConnection(scope svc.Scope, c *connection, writing *bool, q url.Values) {
	if scope.Data != svc.DataDraft {
		return
	}
	lock := DraftOverlayLock(s.cfg.Root, scope)
	lock.Lock()
	previous := c.unlock
	c.unlock = func() { lock.Unlock(); previous() }
	// Draft reads retain the same lifetime lock as writers. Replacing a file
	// while a reader checkpoints its WAL can damage the new snapshot too.
	*writing = true
	// A concurrent Discard may remove the copy after path resolution.
	// Read/write mode refuses to recreate it as a fresh database.
	q.Set("mode", "rw")
}

func (s *Service) shapeDatabaseFilename(scope svc.Scope) (string, error) {
	switch scope.Data {
	case svc.DataReal:
		return "records.db", nil
	case svc.DataSample:
		return "sample.db", nil
	case svc.DataDraft:
		return s.draftDatabaseFilename(scope)
	default:
		return "", invalid("invalid data mode")
	}
}

func (s *Service) draftDatabaseFilename(scope svc.Scope) (string, error) {
	member := scope.Caller.Member
	if member == "" || member == "." || member == ".." || strings.ContainsAny(member, "/\\") || filepath.IsAbs(member) {
		return "", invalid("invalid draft member")
	}
	filename := filepath.Join("drafts", member, "overlay.db")
	info, err := os.Stat(filepath.Join(s.cfg.Root, scope.App, filename))
	if os.IsNotExist(err) {
		return "", bridge.Errorf(bridge.CodeNotFound, "no draft")
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", bridge.Errorf(bridge.CodeNotFound, "no draft")
	}
	// SQLite must open an existing scratch copy; opening a draft never seeds real data.
	return filename, nil
}
