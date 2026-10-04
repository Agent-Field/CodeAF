package relayserve

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// retiredDir holds one tiny file per deleted identity: the tombstone that keeps
// the id answering "gone" and keeps anyone from quietly making it again.
const retiredDir = "retired"

func (n *namespaces) tombstone(id string) string { return filepath.Join(n.root, retiredDir, id) }

// gone says whether the identity was replaced and deleted.
func (n *namespaces) gone(id string) bool {
	_, err := os.Stat(n.tombstone(id))
	return err == nil
}

// sweep deletes every identity whose retirement is due. The tombstone is
// written before anything is removed, so a crash half way leaves an identity
// that already answers "gone" and that the next sweep finishes erasing.
func (n *namespaces) sweep() error {
	ids, err := n.known()
	if err != nil {
		return err
	}
	var failed error
	for _, id := range ids {
		failed = errors.Join(failed, n.sweepOne(id))
	}
	return failed
}

func (n *namespaces) sweepOne(id string) error {
	if !n.gone(id) {
		due, err := n.due(id)
		if err != nil || !due {
			return err
		}
		if err := n.markGone(id); err != nil {
			return err
		}
	}
	return n.erase(id)
}

// due says whether the identity's retirement time has come by the relay's clock.
func (n *namespaces) due(id string) (bool, error) {
	d, err := n.sqlite(id)
	if err != nil {
		return false, err
	}
	r := d.Rotated()
	return r != nil && r.State == directory.StateRetired && n.now().UnixMilli() >= r.RetireAt, nil
}

func (n *namespaces) markGone(id string) error {
	if err := os.MkdirAll(filepath.Join(n.root, retiredDir), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(n.tombstone(id), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(n.now().UTC().Format("2006-01-02T15:04:05Z") + "\n")
	return errors.Join(werr, f.Sync(), f.Close())
}

// erase closes and removes everything the identity had.
func (n *namespaces) erase(id string) error {
	n.forget(id)
	db := filepath.Join(n.root, "directory", id+".db")
	var failed error
	for _, path := range []string{db, db + "-wal", db + "-shm"} {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failed = errors.Join(failed, err)
		}
	}
	return errors.Join(failed, os.RemoveAll(filepath.Join(n.root, "blobs", id)))
}

// forget closes the identity's open directory, if it has one.
func (n *namespaces) forget(id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if d, ok := n.dirs[id]; ok {
		_ = d.Close()
		delete(n.dirs, id)
	}
}

// known lists every identity that has a directory file or a tombstone.
func (n *namespaces) known() ([]string, error) {
	seen := map[string]bool{}
	for dir, suffix := range map[string]string{"directory": ".db", retiredDir: ""} {
		entries, err := os.ReadDir(filepath.Join(n.root, dir))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		for _, e := range entries {
			if id, ok := strings.CutSuffix(e.Name(), suffix); ok && checkedID(id) == nil {
				seen[id] = true
			}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids, nil
}
