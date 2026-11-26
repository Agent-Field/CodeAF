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

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

const versionFile = "version.json"

// The Builder may keep its sample database beside the draft definition.
var draftFiles = []string{"sample.db", "sample.db-journal", "sample.db-shm", "sample.db-wal"}

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
	if def.Meta.Version != m.Version {
		return Draft{}, fmt.Errorf("%w: revert to working version %d before opening a draft", ErrConflict, def.Meta.Version)
	}
	d.base = def.Meta.Version
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
	f, err := readDefinition(d.dir, draftFiles...)
	if err != nil {
		return Meta{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	meta, err := s.publish(m, f, request, requestSummary(request), nil, false)
	if err != nil {
		return Meta{}, err
	}
	// The commit has happened. Reporting a cleanup error as a failed Keep
	// would invite callers to retry a change that is already live.
	_ = os.RemoveAll(d.dir)
	return meta, nil
}

// PublishBuild publishes the exact checked snapshot a durable editing job
// holds. The job keeps its directory and uses the resulting version as its
// next base. Callers use the draft ID as the prospective ID of a new app.
func (s *Store) PublishBuild(id, app string, base int, def schema.Definition, request string) (Meta, error) {
	if !cell.ValidID(id) || (app != "" && !cell.ValidID(app)) {
		return Meta{}, ErrBadInput
	}
	unlock, err := s.lock(true, true)
	if err != nil {
		return Meta{}, err
	}
	defer unlock()
	m := Meta{ID: id, CreatedAt: time.Now().UTC()}
	if app != "" {
		m, err = s.liveMeta(app)
		if err != nil {
			return Meta{}, err
		}
		if m.Version != base {
			return Meta{}, ErrConflict
		}
	}
	return s.publish(m, files(def.Files), request, requestSummary(request), nil, false)
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
	return s.publish(m, d.files, msg, msg, d.types, true)
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
	if s.openData != nil {
		d, err := s.openData()
		if err != nil {
			return Meta{}, err
		}
		var raw []byte
		if _, ok, err := d.Publication(id); err != nil {
			return Meta{}, err
		} else if !ok && !deleted {
			def, err := s.loadSnapshot(m, m.Version)
			if err != nil {
				return Meta{}, err
			}
			raw = def.App
			m.digest = def.files.digest()
		}
		if err := d.PublishApp(publication(m, raw), m.Version); err != nil {
			return Meta{}, err
		}
		// The filesystem metadata is a compatibility copy after the DB commit.
		_ = fsreplace.Rename(next, filepath.Join(dir, "meta.json"))
		_ = fsreplace.SyncDir(dir)
		return m, nil
	}
	if err := fsreplace.Rename(next, filepath.Join(dir, "meta.json")); err != nil {
		return Meta{}, err
	}
	_ = fsreplace.SyncDir(dir)
	return m, nil
}

func (s *Store) publish(m Meta, f files, request, summary string, existing schema.TypeSet, restore bool) (Meta, error) {
	if m.Version == math.MaxInt {
		return Meta{}, ErrCorrupt
	}
	n := m.Version + 1
	if !restore {
		if err := f.stamp(m.ID, n); err != nil {
			return Meta{}, fmt.Errorf("%w: %w", ErrInvalid, err)
		}
	}
	a, err := f.header()
	if err != nil {
		return Meta{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if strings.TrimSpace(a.Name) == "" {
		return Meta{}, fmt.Errorf("%w: app.json needs an app name", ErrInvalid)
	}
	if s.types != nil {
		existing, err = s.types(m.ID)
		if err != nil {
			return Meta{}, fmt.Errorf("read record type definitions: %w", err)
		}
	}
	if err := validateDefinition(f, existing); err != nil {
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
	receipt := versionReceipt{Version: Version{n, next.UpdatedAt, request, summary}, Types: existing}
	if restore {
		receipt.ManifestVersion = a.Version
	}
	if err := writeJSON(filepath.Join(snapshot, versionFile), receipt); err != nil {
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
	if s.openData != nil {
		next.digest = f.digest()
		if err := s.commitPublication(m, next, f[appFile], app, tx, snapshot); err != nil {
			return Meta{}, err
		}
		return next, nil
	}
	if m.Version == 0 {
		if _, err := os.Lstat(app); err == nil {
			return Meta{}, ErrConflict
		} else if !errors.Is(err, fs.ErrNotExist) {
			return Meta{}, err
		}
	}
	if m.Version == 0 {
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

// commitPublication makes the numbered snapshot durable before the data
// writer points to it. An interrupted or refused write leaves only an
// uncommitted snapshot, which a later attempt can replace.
func (s *Store) commitPublication(before, next Meta, raw []byte, app, tx, snapshot string) error {
	d, err := s.openData()
	if err != nil {
		return err
	}
	if before.Version == 0 {
		// A prepared first version may leave a directory without metadata.
		if _, err := s.readMeta(before.ID); err == nil {
			return ErrConflict
		} else if !errors.Is(err, ErrNotFound) && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := os.Mkdir(app, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if err := checkDir(app); err != nil {
			return err
		}
		if err := os.Mkdir(filepath.Join(app, "versions"), 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if err := fsreplace.SyncDir(s.root); err != nil {
			return err
		}
	}
	if err := stageVersion(app, tx, snapshot); err != nil {
		return err
	}
	if err := d.PublishApp(publication(next, raw), before.Version); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	// Readers of an authoritative publication use the numbered snapshot.
	// The compatibility copy can lag without changing the published version.
	_ = installCurrent(app, tx)
	return nil
}

// installVersion moves a complete snapshot and definition into place under
// the filesystem lock. Meta is the commit point. If interrupted before that
// rename, readers can still load the previous committed snapshot.
func installVersion(app, tx, snapshot string) error {
	if err := stageVersion(app, tx, snapshot); err != nil {
		return err
	}
	return installCurrent(app, tx)
}

func stageVersion(app, tx, snapshot string) error {
	versions := filepath.Join(app, "versions")
	if err := checkDir(versions); err != nil {
		return err
	}
	next := filepath.Join(versions, filepath.Base(snapshot))
	// The number may be left by an interrupted write. Keep its bytes out of
	// the numbered history in case the metadata itself was rolled back.
	abandoned := filepath.Join(versions, filepath.Base(tx)+"-"+filepath.Base(snapshot))
	if err := moveIfPresent(next, abandoned); err != nil {
		return err
	}
	if err := fsreplace.Rename(snapshot, next); err != nil {
		return err
	}
	if err := fsreplace.SyncDir(versions); err != nil {
		return err
	}
	return fsreplace.SyncDir(app)
}

func installCurrent(app, tx string) (err error) {
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
	if err := fsreplace.SyncDir(app); err != nil {
		return err
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

func (s *Store) readReceipt(id string, n int) (versionReceipt, error) {
	dir := s.snapshot(id, n)
	if err := checkDir(filepath.Dir(dir)); err != nil {
		return versionReceipt{}, err
	}
	if err := checkDir(dir); err != nil {
		return versionReceipt{}, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return versionReceipt{}, err
	}
	defer root.Close()
	raw, err := readRegular(root, versionFile)
	if err != nil {
		return versionReceipt{}, err
	}
	var v versionReceipt
	if err := json.Unmarshal(raw, &v); err != nil {
		return versionReceipt{}, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	if v.N != n || v.At.IsZero() || v.ManifestVersion < 0 || v.ManifestVersion >= n {
		return versionReceipt{}, ErrCorrupt
	}
	return v, nil
}
