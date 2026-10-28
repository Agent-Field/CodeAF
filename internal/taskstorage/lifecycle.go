// Package taskstorage retains inactive owned task copies as exact local archives.
// Policy belongs to the session owner; this module never chooses a task to retire.
package taskstorage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

type Phase string

const (
	Resident  Phase = "resident"
	Retiring  Phase = "retiring"
	Cold      Phase = "cold"
	Restoring Phase = "restoring"
)

// State is a device-local recovery receipt. Path is relative to the owner's trees.
type State struct {
	Version  uint16 `json:"V"`
	Path     string `json:"path"`
	Snapshot string `json:"snapshot"`
	Phase    Phase  `json:"phase"`
	Check    string `json:"check,omitempty"`
}

// Vault captures complete local state; it is deliberately separate from screened relay content.
// Implementations must join every writer before returning, including on cancellation.
type Vault interface {
	Capture(context.Context, string) (string, error)
	Restore(context.Context, string, string) error
	Verify(context.Context, string, string) error
}

// RemainderVault proves that an interrupted removal left only unchanged saved entries.
type RemainderVault interface {
	VerifyRemainder(context.Context, string, string) error
}

// Manager serializes retirement and reopen for copies strictly below Trees.
// Busy must account for every harness worker and process that could still write.
type Manager struct {
	Root, Trees   string
	Vault         Vault
	Busy          func(string) bool
	BeforeCapture func(context.Context, string) error
	BeforeRetire  func(context.Context, string, string) error
	AfterRestore  func(context.Context, string) error
}

func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// Existing system aliases on macOS are trusted; task-controlled links are not.
func validateAncestors(path string) error {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	current := volume + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(clean, current), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if runtime.GOOS == "darwin" && (current == "/var" || current == "/tmp" || current == "/etc") {
				target, err := filepath.EvalSymlinks(current)
				if err == nil && target == "/private"+current {
					continue
				}
			}
			return errors.New("task storage root traverses a symbolic link")
		}
		if !info.IsDir() {
			return errors.New("task storage ancestor is not a directory")
		}
	}
	return nil
}

func (m Manager) scope() (string, error) {
	if m.Root == "" || m.Trees == "" || !filepath.IsAbs(m.Root) || !filepath.IsAbs(m.Trees) {
		return "", errors.New("task storage needs absolute owned roots")
	}
	if err := validateAncestors(m.Root); err != nil {
		return "", err
	}
	if err := validateAncestors(m.Trees); err != nil {
		return "", err
	}
	return filepath.Join(m.Root, "receipts", digest(filepath.Clean(m.Trees))), nil
}
func (m Manager) owned(path string) (string, error) {
	if err := validateAncestors(m.Trees); err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("task copy path must be absolute")
	}
	rel, e := filepath.Rel(m.Trees, filepath.Clean(path))
	if e != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("task copy is outside its owner")
	}
	current := filepath.Clean(m.Trees)
	parts := append([]string{""}, strings.Split(rel, string(filepath.Separator))...)
	for _, part := range parts {
		if part != "" {
			current = filepath.Join(current, part)
		}
		info, e := os.Lstat(current)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return "", e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("task storage cannot traverse a symbolic link")
		}
		if !info.IsDir() {
			return "", errors.New("task copy ancestor is not a directory")
		}
	}
	return filepath.ToSlash(rel), nil
}
func (m Manager) acquire(ctx context.Context, path string) (string, string, *os.File, func(), error) {
	rel, e := m.owned(path)
	if e != nil {
		return "", "", nil, nil, e
	}
	scope, e := m.scope()
	if e != nil {
		return "", "", nil, nil, e
	}
	if e = os.MkdirAll(scope, 0700); e != nil {
		return "", "", nil, nil, e
	}
	key := digest(rel)
	f, e := os.OpenFile(filepath.Join(scope, key+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return "", "", nil, nil, e
	}
	for {
		if e = ctx.Err(); e != nil {
			f.Close()
			return "", "", nil, nil, e
		}
		e = filelock.Lock(f, true, true)
		if e == nil {
			break
		}
		if !filelock.IsBusy(e) {
			f.Close()
			return "", "", nil, nil, e
		}
		select {
		case <-ctx.Done():
			f.Close()
			return "", "", nil, nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return rel, filepath.Join(scope, key+".json"), f, func() { _ = filelock.Unlock(f); _ = f.Close() }, nil
}
func read(path, rel string) (State, error) {
	raw, e := os.ReadFile(path)
	if errors.Is(e, os.ErrNotExist) {
		return State{Version: 1, Path: rel, Phase: Resident}, nil
	}
	if e != nil {
		return State{}, e
	}
	var s State
	if e = json.Unmarshal(raw, &s); e != nil {
		return State{}, e
	}
	if s.Version != 1 || s.Path != rel {
		return State{}, errors.New("unsupported or mismatched task recovery receipt")
	}
	switch s.Phase {
	case Resident, Retiring, Cold, Restoring:
	default:
		return State{}, errors.New("unknown task storage state")
	}
	if s.Phase != Resident && s.Snapshot == "" {
		return State{}, errors.New("task recovery snapshot is missing")
	}
	if err := validCheck(s); err != nil {
		return State{}, err
	}
	return s, nil
}
func durable(path string, s State) error {
	raw, e := json.Marshal(s)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".receipt-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(raw); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	return syncDir(filepath.Dir(path))
}
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func present(path string) (bool, error) {
	_, e := os.Lstat(path)
	if errors.Is(e, os.ErrNotExist) {
		return false, nil
	}
	return e == nil, e
}
func side(path, kind string) string {
	return filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".storage-"+kind+"-"+digest(path)[:16])
}
func (m Manager) rename(from, to string) error {
	if _, err := m.owned(from); err != nil {
		return err
	}
	if _, err := m.owned(to); err != nil {
		return err
	}
	root, err := os.OpenRoot(m.Trees)
	if err != nil {
		return err
	}
	defer root.Close()
	a, err := filepath.Rel(m.Trees, from)
	if err != nil {
		return err
	}
	b, err := filepath.Rel(m.Trees, to)
	if err != nil {
		return err
	}
	return root.Rename(a, b)
}

// removeOwnedTree changes permissions only on detached or verification copies.
// Descriptor-relative traversal prevents a link from redirecting cleanup elsewhere.
func removeOwnedTree(path string) error {
	if err := validateAncestors(filepath.Dir(path)); err != nil {
		return err
	}
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parent.Close()
	name := filepath.Base(path)
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return parent.Remove(name)
	}
	tree, err := parent.OpenRoot(name)
	if err != nil {
		return err
	}
	actual, err := tree.Stat(".")
	if err != nil {
		tree.Close()
		return err
	}
	if !os.SameFile(info, actual) {
		tree.Close()
		return errors.New("cleanup tree changed")
	}
	err = fs.WalkDir(tree.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		f, err := tree.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil {
			return err
		}
		original, err := entry.Info()
		if err != nil {
			return err
		}
		if !os.SameFile(original, stat) {
			return errors.New("cleanup directory changed")
		}
		return f.Chmod(stat.Mode() | 0700)
	})
	tree.Close()
	if err != nil {
		return err
	}
	return parent.RemoveAll(name)
}

func (m Manager) vault() error {
	if m.Vault == nil {
		return errors.New("task archive backend is unavailable")
	}
	// This precedes receipt acquisition and journal cleanup. An old reservation
	// is not safe to remove when its native writer cannot inherit our lease.
	if backend, ok := m.Vault.(interface{ archiveRecoveryReady() error }); ok {
		return backend.archiveRecoveryReady()
	}
	return nil
}

// Inspect reads one receipt under the same lock used by retirement and reopen.
func (m Manager) Inspect(ctx context.Context, path string) (State, error) {
	if e := ctx.Err(); e != nil {
		return State{}, e
	}
	rel, receipt, _, unlock, e := m.acquire(ctx, path)
	if e != nil {
		return State{}, e
	}
	defer unlock()
	return read(receipt, rel)
}

// Paths lists retained identities, including copies whose expanded folder is absent.
func (m Manager) Paths(ctx context.Context) ([]string, error) {
	scope, e := m.scope()
	if e != nil {
		return nil, e
	}
	entries, e := os.ReadDir(scope)
	if errors.Is(e, os.ErrNotExist) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var out []string
	for _, entry := range entries {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, e := os.ReadFile(filepath.Join(scope, entry.Name()))
		if e != nil {
			return nil, e
		}
		var s State
		if e = json.Unmarshal(raw, &s); e != nil {
			return nil, e
		}
		path := filepath.Join(m.Trees, filepath.FromSlash(s.Path))
		rel, e := m.owned(path)
		if e != nil {
			return nil, e
		}
		if _, e = read(filepath.Join(scope, entry.Name()), rel); e != nil {
			return nil, e
		}
		out = append(out, path)
	}
	return out, nil
}

// EnsureAll is used only when an operation needs every retained working copy.
func (m Manager) EnsureAll(ctx context.Context) error {
	paths, e := m.Paths(ctx)
	if e != nil {
		return e
	}
	for _, path := range paths {
		if e = m.Ensure(ctx, path); e != nil {
			return e
		}
	}
	return nil
}

// Park captures, independently restores and verifies, then retires one idle copy.
// Any failure before durable retirement leaves the original or its recoverable sibling.
func (m Manager) Park(ctx context.Context, path string) (err error) {
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := m.vault(); e != nil {
		return e
	}
	rel, receipt, lease, unlock, e := m.acquire(ctx, path)
	if e != nil {
		return e
	}
	defer unlock()
	ctx = context.WithValue(ctx, receiptLeaseKey{}, lease)
	if m.Busy == nil || m.Busy(path) {
		return errors.New("task copy may still have a writer")
	}
	s, e := read(receipt, rel)
	if e != nil {
		return e
	}
	if e = clearCheck(receipt, &s); e != nil {
		return e
	}
	exists, e := present(path)
	if e != nil {
		return e
	}
	if !exists && s.Phase == Cold {
		return nil
	}
	if s.Phase != Resident {
		return errors.New("task storage transition needs recovery before retirement")
	}
	if !exists {
		return errors.New("task working copy is missing")
	}
	aside := side(path, "retiring")
	if exists, e = present(aside); e != nil {
		return e
	} else if exists {
		return errors.New("a retired task sibling needs recovery")
	}
	if m.BeforeCapture != nil {
		if e = m.BeforeCapture(ctx, path); e != nil {
			return e
		}
	}
	snapshot := s.Snapshot
	if snapshot == "" || m.Vault.Verify(ctx, snapshot, path) != nil {
		snapshot, e = m.Vault.Capture(ctx, path)
		if e != nil {
			return fmt.Errorf("save task copy: %w", e)
		}
	}
	container, e := newCheck(receipt, &s, snapshot)
	if e != nil {
		return e
	}
	defer func() {
		if cleanupErr := clearCheck(receipt, &s); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("clean verification copy: %w", cleanupErr))
		}
	}()
	scratch := filepath.Join(container, "verified")
	if e = m.Vault.Restore(ctx, snapshot, scratch); e != nil {
		return fmt.Errorf("check task recovery: %w", e)
	}
	if e = m.Vault.Verify(ctx, snapshot, scratch); e != nil {
		return e
	}
	if e = m.Vault.Verify(ctx, snapshot, path); e != nil {
		return fmt.Errorf("task changed during capture: %w", e)
	}
	if m.Busy(path) {
		return errors.New("task copy acquired a writer")
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if m.BeforeRetire != nil {
		if e = m.BeforeRetire(ctx, snapshot, path); e != nil {
			return e
		}
	}
	if e = m.Vault.Verify(ctx, snapshot, path); e != nil {
		return fmt.Errorf("task changed preparing retirement: %w", e)
	}
	s.Snapshot, s.Phase = snapshot, Retiring
	if e = durable(receipt, s); e != nil {
		return e
	}
	if _, e = m.owned(path); e != nil {
		return e
	}
	if e = m.rename(path, aside); e != nil {
		return e
	}
	if e = syncDir(filepath.Dir(path)); e != nil {
		return e
	}
	// Verify after detaching too; a writer racing capture must not lose new bytes.
	if e = m.Vault.Verify(ctx, snapshot, aside); e != nil {
		if restoreErr := m.rename(aside, path); restoreErr == nil {
			s.Phase = Resident
			_ = durable(receipt, s)
		}
		return fmt.Errorf("detached task copy changed: %w", e)
	}
	if e = removeOwnedTree(aside); e != nil {
		return e
	}
	if e = syncDir(filepath.Dir(path)); e != nil {
		return e
	}
	s.Phase = Cold
	return durable(receipt, s)
}

// Ensure restores before a caller may fall back to carving a new working copy.
// A retained receipt with unavailable bytes is an error, never a fresh workspace.
func (m Manager) Ensure(ctx context.Context, path string) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := m.vault(); e != nil {
		return e
	}
	rel, receipt, lease, unlock, e := m.acquire(ctx, path)
	if e != nil {
		return e
	}
	defer unlock()
	ctx = context.WithValue(ctx, receiptLeaseKey{}, lease)
	s, e := read(receipt, rel)
	if e != nil {
		return e
	}
	if e = clearCheck(receipt, &s); e != nil {
		return e
	}
	if s.Phase == Resident {
		exists, err := present(path)
		if err != nil {
			return err
		}
		if !exists && s.Snapshot != "" {
			return errors.New("saved task copy is missing; recovery is required before continuing")
		}
		if m.AfterRestore != nil {
			return m.AfterRestore(ctx, path)
		}
		return nil
	}
	exists, e := present(path)
	if e != nil {
		return e
	}
	aside := side(path, "retiring")
	old, e := present(aside)
	if e != nil {
		return e
	}
	if exists {
		if e = m.Vault.Verify(ctx, s.Snapshot, path); e != nil {
			return fmt.Errorf("existing task copy conflicts with saved work: %w", e)
		}
		if old {
			return errors.New("task has both an expanded copy and retirement sibling")
		}
		if m.AfterRestore != nil {
			if e = m.AfterRestore(ctx, path); e != nil {
				return e
			}
		}
		s.Phase = Resident
		return durable(receipt, s)
	}
	if old {
		if s.Phase != Retiring {
			return errors.New("unexpected retirement sibling")
		}
		if err := m.Vault.Verify(ctx, s.Snapshot, aside); err == nil {
			if e = m.rename(aside, path); e != nil {
				return e
			}
			if e = syncDir(filepath.Dir(path)); e != nil {
				return e
			}
			if m.AfterRestore != nil {
				if e = m.AfterRestore(ctx, path); e != nil {
					return e
				}
			}
			s.Phase = Resident
			return durable(receipt, s)
		}
		remainder, ok := m.Vault.(RemainderVault)
		if !ok {
			return errors.New("retired task copy needs a recovery-capable archive engine")
		}
		if e = remainder.VerifyRemainder(ctx, s.Snapshot, aside); e != nil {
			return fmt.Errorf("retired task has newer or unsupported data; it was retained: %w", e)
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if e = removeOwnedTree(aside); e != nil {
			return e
		}
		if e = syncDir(filepath.Dir(path)); e != nil {
			return e
		}
	}
	staging := side(path, "restoring")
	left, e := present(staging)
	if e != nil {
		return e
	}
	if left {
		if s.Phase != Restoring {
			return errors.New("unexpected task restore sibling")
		}
		if _, e = m.owned(staging); e != nil {
			return e
		}
		if e = removeOwnedTree(staging); e != nil {
			return e
		}
	}
	s.Phase = Restoring
	if e = durable(receipt, s); e != nil {
		return e
	}
	if e = m.Vault.Restore(ctx, s.Snapshot, staging); e != nil {
		_ = removeOwnedTree(staging)
		s.Phase = Cold
		_ = durable(receipt, s)
		return fmt.Errorf("restore saved task: %w", e)
	}
	if e = m.Vault.Verify(ctx, s.Snapshot, staging); e != nil {
		_ = removeOwnedTree(staging)
		s.Phase = Cold
		_ = durable(receipt, s)
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if exists, e = present(path); e != nil {
		return e
	} else if exists {
		return errors.New("task copy appeared during restore")
	}
	if e = m.rename(staging, path); e != nil {
		return e
	}
	if e = syncDir(filepath.Dir(path)); e != nil {
		return e
	}
	if m.AfterRestore != nil {
		if e = m.AfterRestore(ctx, path); e != nil {
			return e
		}
	}
	s.Phase = Resident
	return durable(receipt, s)
}
