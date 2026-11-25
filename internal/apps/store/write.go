package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

// Draft is the Builder's handle for an editable definition. The Builder must
// finish writing its files before calling Promote or Discard.
type Draft struct {
	id, dir, fromApp string
	base             int
}

// ID is the draft's ULID, which the Builder can use in its own requests.
func (d Draft) ID() string { return d.id }

// Dir is the only directory the Builder should edit through this handle.
func (d Draft) Dir() string { return d.dir }

// NewDraft copies an existing app's valid definition or opens an empty
// directory when fromApp is empty.
func (s *Store) NewDraft(fromApp string) (Draft, error) {
	unlock, err := s.lock(true, true)
	if err != nil {
		return Draft{}, err
	}
	defer unlock()
	id, err := cell.NewID(time.Now())
	if err != nil {
		return Draft{}, err
	}
	pool := filepath.Join(s.root, "_drafts")
	if err := os.Mkdir(pool, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return Draft{}, err
	}
	if err := checkDir(pool); err != nil {
		return Draft{}, err
	}
	d := Draft{id: id, dir: filepath.Join(pool, id), fromApp: fromApp}
	if fromApp == "" {
		if err := os.Mkdir(d.dir, 0o700); err != nil {
			return Draft{}, err
		}
		return d, nil
	}
	m, err := s.liveMeta(fromApp)
	if err != nil {
		return Draft{}, err
	}
	def, err := s.loadLive(m)
	if err != nil {
		return Draft{}, err
	}
	d.base = m.Version
	if err := def.files.write(d.dir); err != nil {
		return Draft{}, err
	}
	return d, nil
}

func (s *Store) validDraft(d Draft) bool {
	return cell.ValidID(d.id) && d.dir == filepath.Join(s.root, "_drafts", d.id)
}

// Discard removes the working files. It can be retried after a draft is gone.
func (s *Store) Discard(d Draft) error {
	if !s.validDraft(d) {
		return ErrBadInput
	}
	unlock, err := s.lock(true, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock()
	if err := checkDir(filepath.Dir(d.dir)); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return os.RemoveAll(d.dir)
}

// Promote publishes a checked draft. An edit opened before another Keep or
// Revert must be reopened so it cannot overwrite newer changes.
func (s *Store) Promote(d Draft, request string) (Meta, error) {
	if !s.validDraft(d) {
		return Meta{}, ErrBadInput
	}
	unlock, err := s.lock(true, false)
	if errors.Is(err, fs.ErrNotExist) {
		return Meta{}, ErrNotFound
	}
	if err != nil {
		return Meta{}, err
	}
	defer unlock()

	now := time.Now().UTC()
	m := Meta{ID: d.id, CreatedAt: now}
	if d.fromApp != "" {
		m, err = s.liveMeta(d.fromApp)
		if err != nil {
			return Meta{}, err
		}
		if m.Version != d.base {
			return Meta{}, ErrConflict
		}
	}
	if err := checkDir(filepath.Dir(d.dir)); err != nil {
		return Meta{}, err
	}
	if err := checkDir(d.dir); errors.Is(err, fs.ErrNotExist) {
		return Meta{}, ErrNotFound
	} else if err != nil {
		return Meta{}, err
	}
	f, err := readDefinition(d.dir)
	if err != nil {
		return Meta{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	meta, err := s.publish(m, f, request, requestSummary(request))
	if err != nil {
		return Meta{}, err
	}
	// The commit has happened. Reporting a cleanup error as a failed Keep
	// would invite callers to retry a change that is already live.
	_ = os.RemoveAll(d.dir)
	return meta, nil
}

// updatedAt keeps metadata ordered even if the machine clock moves backwards.
func updatedAt(m Meta, now time.Time) time.Time {
	if now.Before(m.CreatedAt) {
		return m.CreatedAt
	}
	if now.Before(m.UpdatedAt) {
		return m.UpdatedAt
	}
	return now.UTC()
}

func requestSummary(request string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(request), "\n")
	return strings.TrimSpace(line)
}

// Revert validates an old snapshot and copies it forward as a new version.
func (s *Store) Revert(id string, n int) (Meta, error) {
	if n < 1 {
		return Meta{}, ErrBadInput
	}
	unlock, err := s.lockExisting(id, true)
	if err != nil {
		return Meta{}, err
	}
	defer unlock()
	m, err := s.liveMeta(id)
	if err != nil {
		return Meta{}, err
	}
	if n > m.Version {
		return Meta{}, ErrNotFound
	}
	d, err := s.loadSnapshot(m, n)
	if errors.Is(err, fs.ErrNotExist) {
		return Meta{}, ErrNotFound
	}
	if err != nil {
		return Meta{}, err
	}
	msg := fmt.Sprintf("Revert to version %d", n)
	return s.publish(m, d.files, msg, msg)
}

// Archive keeps the definition available for inspection and marks its shelf
// row archived. Personal records are outside this store.
func (s *Store) Archive(id string) (Meta, error) {
	return s.setArchived(id, false)
}

// Delete hides the app from the shelf and prevents it from being opened.
// Both definition history and personal records remain on disk.
func (s *Store) Delete(id string) (Meta, error) {
	return s.setArchived(id, true)
}

func (s *Store) setArchived(id string, deleted bool) (Meta, error) {
	unlock, err := s.lockExisting(id, true)
	if err != nil {
		return Meta{}, err
	}
	defer unlock()
	m, err := s.readMeta(id)
	if err != nil {
		return Meta{}, err
	}
	if m.Deleted && !deleted {
		return Meta{}, ErrNotFound
	}
	if m.Archived && m.Deleted == deleted {
		return m, nil
	}
	m.Archived, m.Deleted = true, deleted
	m.UpdatedAt = updatedAt(m, time.Now())
	dir := filepath.Join(s.root, id)
	tmp, err := os.MkdirTemp(dir, ".meta-")
	if err != nil {
		return Meta{}, err
	}
	defer os.RemoveAll(tmp)
	next := filepath.Join(tmp, "meta.json")
	if err := writeJSON(next, m); err != nil {
		return Meta{}, err
	}
	if err := fsreplace.Rename(next, filepath.Join(dir, "meta.json")); err != nil {
		return Meta{}, err
	}
	_ = fsreplace.SyncDir(dir)
	return m, nil
}

func (s *Store) publish(m Meta, f files, request, summary string) (Meta, error) {
	if m.Version == math.MaxInt {
		return Meta{}, ErrCorrupt
	}
	n := m.Version + 1
	if err := f.stamp(m.ID, n); err != nil {
		return Meta{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	a, err := f.header()
	if err != nil {
		return Meta{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err := s.validateDefinition(f); err != nil {
		return Meta{}, err
	}
	next := m
	next.Name, next.Icon, next.Version, next.UpdatedAt = a.Name, a.Icon, n, updatedAt(m, time.Now())
	tx, err := os.MkdirTemp(s.root, ".publish-")
	if err != nil {
		return Meta{}, err
	}
	defer os.RemoveAll(tx)
	current := filepath.Join(tx, "current")
	if err := f.write(current); err != nil {
		return Meta{}, err
	}
	versions := filepath.Join(tx, "versions")
	if err := os.Mkdir(versions, 0o700); err != nil {
		return Meta{}, err
	}
	snapshot := filepath.Join(versions, strconv.Itoa(n))
	if err := f.write(snapshot); err != nil {
		return Meta{}, err
	}
	if err := writeJSON(filepath.Join(snapshot, "version.json"), Version{n, next.UpdatedAt, request, summary}); err != nil {
		return Meta{}, err
	}
	meta := filepath.Join(tx, "meta.json")
	if err := writeJSON(meta, next); err != nil {
		return Meta{}, err
	}
	for _, dir := range []string{snapshot, versions, tx} {
		if err := fsreplace.SyncDir(dir); err != nil {
			return Meta{}, err
		}
	}
	app := filepath.Join(s.root, next.ID)
	if m.Version == 0 {
		if _, err := os.Lstat(app); err == nil {
			return Meta{}, ErrConflict
		} else if !errors.Is(err, fs.ErrNotExist) {
			return Meta{}, err
		}
		if err := fsreplace.Rename(tx, app); err != nil {
			return Meta{}, err
		}
		_ = fsreplace.SyncDir(s.root)
		return next, nil
	}
	if err := installVersion(app, tx, snapshot); err != nil {
		return Meta{}, err
	}
	return next, nil
}

// installVersion moves a complete snapshot and definition into place under
// the filesystem lock. Meta is the commit point. If interrupted before that
// rename, readers can still load the previous committed snapshot.
func installVersion(app, tx, snapshot string) (err error) {
	versions := filepath.Join(app, "versions")
	if err := checkDir(versions); err != nil {
		return err
	}
	next := filepath.Join(versions, filepath.Base(snapshot))
	// Only an uncommitted snapshot from an interrupted write can use this
	// number. Move it out of the way without changing any committed history.
	if err := moveIfPresent(next, filepath.Join(tx, "abandoned")); err != nil {
		return err
	}
	if err := fsreplace.Rename(snapshot, next); err != nil {
		return err
	}
	current := filepath.Join(app, "current")
	previous := filepath.Join(tx, "previous")
	if err := moveIfPresent(current, previous); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			// The committed snapshot remains readable even if the filesystem
			// also refuses to restore the old current directory.
			_ = moveIfPresent(current, filepath.Join(tx, "failed"))
			_ = moveIfPresent(previous, current)
		}
	}()
	if err := fsreplace.Rename(filepath.Join(tx, "current"), current); err != nil {
		return err
	}
	for _, dir := range []string{versions, app} {
		if err := fsreplace.SyncDir(dir); err != nil {
			return err
		}
	}
	if err := fsreplace.Rename(filepath.Join(tx, "meta.json"), filepath.Join(app, "meta.json")); err != nil {
		return err
	}
	_ = fsreplace.SyncDir(app)
	return nil
}

func moveIfPresent(from, to string) error {
	if err := fsreplace.Rename(from, to); !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func writeSynced(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeSynced(path, append(raw, '\n'))
}

func readVersion(dir string, n int) (Version, error) {
	if err := checkDir(dir); err != nil {
		return Version{}, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Version{}, err
	}
	defer root.Close()
	raw, err := readRegular(root, "version.json")
	if err != nil {
		return Version{}, err
	}
	var v Version
	if err := json.Unmarshal(raw, &v); err != nil {
		return Version{}, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	if v.N != n || v.At.IsZero() {
		return Version{}, ErrCorrupt
	}
	return v, nil
}
