package taskdoor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

const copyCarrier = cell.StateDir + "/task-copy"

// Input retains the origin's conflict fingerprint locally; only Head travels
// on the wire. Result uses the same task-only cell after the work finishes.
type Input = Head
type Result = Head

// CopyDeps keeps task transport separate from chat heads and leases. Engine's
// shared object store may be the profile's ordinary store for identity-wide
// deduplication; Root holds only task cells, locks and transfer scratch.
type CopyDeps struct {
	Root     string
	Service  string
	Identity identity.Identity
	Device   identity.Dev
	Engine   cellstore.Engine
	Store    blobstore.Store
	Measure  func(CopyMeasure)
	// BeforeInstall observes the prepared result before filesystem replacement.
	// Tests use this boundary to reproduce an editor racing installation.
	BeforeInstall func()
}

type CopyMeasure struct {
	Seal, Upload, Fetch, Restore time.Duration
	Bytes                        int64
}

func (d CopyDeps) engine() cellstore.Engine {
	e := d.Engine
	e.Workspace, e.WorkspaceOf, e.Compose = "", nil, nil
	e.Identity = cellstore.Identity{Device: d.Device.ID()}
	return e
}
func (d CopyDeps) sync() cellstore.SyncEngine {
	s := d.engine().Sync(cellstore.SyncKeys{CellKey: d.Identity.CellKey(), Dedup: d.Identity.DedupSecret()}, cellstore.LedgerName(d.Service, d.Identity.ID()))
	// Small source files need bounded frames too: the engine's default can
	// exceed the blob wire's header bound before reaching its payload bound.
	s.MaxFrame = 64 << 10
	return s
}
func (d CopyDeps) cell(id string) (cell.Cell, error) {
	if !cell.ValidID(id) {
		return cell.Cell{}, errors.New("the task copy's cell is not readable")
	}
	return cell.Cell{ID: id, Root: filepath.Join(d.Root, "cells", id)}, nil
}
func (d CopyDeps) measure(m CopyMeasure) {
	if d.Measure != nil {
		d.Measure(m)
	}
}

func (d CopyDeps) lock(ctx context.Context, name string) (func(), error) {
	if d.Root == "" || d.Store == nil || d.Service == "" {
		return nil, errors.New("task copies need private storage and a relay blob store")
	}
	dir := filepath.Join(d.Root, "locks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			f.Close()
			return nil, err
		}
		if err := filelock.Lock(f, true, true); err == nil {
			return func() { _ = filelock.Unlock(f); _ = f.Close() }, nil
		} else if !filelock.IsBusy(err) {
			_ = f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// SendCopy seals a carrier in its own cell. Upload deliberately has no
// directory client: this road cannot publish a chat head or acquire a lease.
func SendCopy(ctx context.Context, d CopyDeps, copyDir string) (Input, error) {
	path, err := allowedCopy(copyDir)
	if err != nil {
		return Input{}, err
	}
	sum := sha256.Sum256([]byte(path))
	key := hex.EncodeToString(sum[:])
	unlock, err := d.lock(ctx, "source-"+key)
	if err != nil {
		return Input{}, err
	}
	defer unlock()
	ref := filepath.Join(d.Root, "sources", key+".json")
	var previous Input
	err = readJSON(ref, &previous)
	var c cell.Cell
	if errors.Is(err, os.ErrNotExist) {
		c, err = cell.CreateIn(filepath.Join(d.Root, "cells"), cell.Options{Class: cell.Sandboxed, KeyID: d.Identity.CellKeyID()})
	} else if err == nil {
		c, err = d.cell(previous.Cell)
	}
	if err != nil {
		return Input{}, err
	}
	out, err := sendCopy(ctx, d, path, c, "")
	if err != nil {
		return Input{}, err
	}
	if err := os.MkdirAll(filepath.Dir(ref), 0700); err != nil {
		return Input{}, err
	}
	return out, atomicJSON(ref, out)
}

func sendCopy(ctx context.Context, d CopyDeps, path string, c cell.Cell, baseHead string) (Input, error) {
	unlock, err := d.lock(ctx, c.ID)
	if err != nil {
		return Input{}, err
	}
	defer unlock()
	if baseHead != "" {
		// A retry or another attempt using the same input must still make
		// this result a delta from that input, never from its sibling's work.
		if err := d.sync().Materialize(ctx, c, baseHead); err != nil {
			return Input{}, err
		}
	}
	c, err = cell.OpenAt(c.Root, c.ID)
	if err != nil {
		return Input{}, err
	}
	before, err := copyFingerprint(ctx, path)
	if err != nil {
		return Input{}, err
	}
	start := time.Now()
	if err := taskcopy.CaptureCopy(path, filepath.Join(c.Root, copyCarrier)); err != nil {
		return Input{}, err
	}
	after, err := copyFingerprint(ctx, path)
	if err != nil {
		return Input{}, err
	}
	if before != after {
		return Input{}, errors.New("the task copy changed while it was being sent; try again")
	}
	sealed, err := d.engine().Seal(ctx, c, cellstore.TurnInfo{Trigger: cellstore.AgentRun})
	if err != nil {
		return Input{}, err
	}
	m := CopyMeasure{Seal: time.Since(start)}
	start = time.Now()
	publisher := cellsync.Publisher{Engine: d.sync(), Store: d.Store, CheckHeld: true}
	exported, err := publisher.Upload(ctx, c, sealed.Turn.ID)
	if err != nil {
		return Input{}, err
	}
	m.Upload = time.Since(start)
	for _, frame := range exported.Frames {
		m.Bytes += frame.Bytes
	}
	d.measure(m)
	return Input{Head: sealed.Turn.ID, Cell: c.ID, Fingerprint: before}, nil
}

// ReceiveCopy restores only the task carrier; capture metadata stays beside
// the work folder so landing cannot mistake it for the task's changes.
func ReceiveCopy(ctx context.Context, d CopyDeps, in Input, dest string) error {
	return receiveCopy(ctx, d, in, dest, "")
}

func receiveCopy(ctx context.Context, d CopyDeps, in Head, dest, fingerprint string) error {
	prepared, err := prepareCopy(ctx, d, in, dest, fingerprint)
	if err != nil {
		return err
	}
	defer prepared.Close()
	return prepared.Install(ctx)
}

// PreparedResult owns fetched files and their cell lock until installation or discard.
// Preparing never replaces the origin copy, so an abandonment can still fence installation.
type PreparedResult struct {
	deps                                         CopyDeps
	path, stage, prepared, previous, fingerprint string
	measure                                      CopyMeasure
	restoreStarted                               time.Time
	unlock                                       func()
	keep                                         bool
}

func prepareCopy(ctx context.Context, d CopyDeps, in Head, dest, fingerprint string) (_ *PreparedResult, err error) {
	path, err := allowedCopy(dest)
	if err != nil {
		return nil, err
	}
	c, err := d.cell(in.Cell)
	if err != nil {
		return nil, err
	}
	if raw, err := hex.DecodeString(in.Head); err != nil || len(raw) != sha256.Size {
		return nil, errors.New("the task copy's head is not readable")
	}
	unlock, err := d.lock(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	result := &PreparedResult{deps: d, path: path, fingerprint: fingerprint, unlock: unlock}
	defer func() {
		if err != nil {
			result.Close()
		}
	}()
	if fingerprint != "" {
		if err := unchangedCopy(ctx, path, fingerprint); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(c.Root, 0700); err != nil {
		return nil, err
	}
	start := time.Now()
	syncer := d.sync()
	fetcher := cellsync.Fetcher{Engine: syncer, Store: d.Store, Inbox: syncer.Inbox}
	if err := fetcher.Complete(ctx, c, in.Head); err != nil {
		return nil, err
	}
	result.measure.Fetch = time.Since(start)
	result.restoreStarted = time.Now()
	if err := syncer.Materialize(ctx, c, in.Head); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	result.stage, err = os.MkdirTemp(filepath.Dir(path), ".task-arrival-")
	if err != nil {
		return nil, err
	}
	result.prepared = filepath.Join(result.stage, "copy")
	if err := taskcopy.RestoreCopy(filepath.Join(c.Root, copyCarrier), result.prepared); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fingerprint != "" {
		if err := taskcopy.ImportCopyHistory(path, result.prepared); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// Install holds no network work; the caller can guard this final boundary with its abandonment lock.
func (r *PreparedResult) Install(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.fingerprint != "" {
		if err := unchangedCopy(ctx, r.path, r.fingerprint); err != nil {
			return err
		}
	} else if entries, err := os.ReadDir(r.path); err == nil && len(entries) > 0 {
		return errors.New("the task's destination already contains files")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if r.deps.BeforeInstall != nil {
		r.deps.BeforeInstall()
	}
	// A sibling keeps relative Git worktree pointers based at the same parent.
	previous, err := os.MkdirTemp(filepath.Dir(r.path), ".task-previous-")
	if err != nil {
		return err
	}
	// Some filesystems reject renaming over even the empty reservation.
	if err := os.Remove(previous); err != nil {
		return err
	}
	moved := false
	if err := os.Rename(r.path, previous); err == nil {
		moved = true
		r.previous = previous
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// The moved original is now the installation boundary's authoritative snapshot.
	// Check it before binding the returned branch or deleting any earlier files.
	undo := func() error { return nil }
	if r.fingerprint != "" {
		if err := unchangedCopy(ctx, previous, r.fingerprint); err != nil {
			return r.rollback(previous, moved, err)
		}
		var err error
		undo, err = taskcopy.BindLinkedResult(previous, r.prepared)
		if err != nil {
			return r.rollback(previous, moved, err)
		}
	}
	if err := os.Rename(r.prepared, r.path); err != nil {
		return r.rollback(previous, moved, errors.Join(err, undo()))
	}
	r.measure.Restore = time.Since(r.restoreStarted)
	r.deps.measure(r.measure)
	return nil
}

func (r *PreparedResult) rollback(previous string, moved bool, cause error) error {
	if !moved {
		return cause
	}
	if err := os.Rename(previous, r.path); err != nil {
		r.keep = true
		return fmt.Errorf("could not put the result in place; the earlier copy is kept at %s: %w", previous, errors.Join(cause, err))
	}
	r.previous = ""
	return cause
}

// Close discards an uninstalled result, but preserves a previous copy whose rollback failed.
func (r *PreparedResult) Close() {
	if !r.keep {
		if r.stage != "" {
			_ = os.RemoveAll(r.stage)
		}
		if r.previous != "" {
			_ = os.RemoveAll(r.previous)
		}
	}
	if r.unlock != nil {
		r.unlock()
		r.unlock = nil
	}
}

func SendResult(ctx context.Context, d CopyDeps, workDir string, in Input) (Result, error) {
	path, err := allowedCopy(workDir)
	if err != nil {
		return Result{}, err
	}
	c, err := d.cell(in.Cell)
	if err != nil {
		return Result{}, err
	}
	return sendCopy(ctx, d, path, c, in.Head)
}

var ErrCopyChanged = errors.New("the task copy changed here while the other computer worked; both copies were kept")

func ReceiveResult(ctx context.Context, d CopyDeps, result Result, localCopyDir, sentFingerprint string) error {
	prepared, err := PrepareResult(ctx, d, result, localCopyDir, sentFingerprint)
	if err != nil {
		return err
	}
	defer prepared.Close()
	return prepared.Install(ctx)
}

// PrepareResult fetches and materializes outside the session's abandonment mutex.
func PrepareResult(ctx context.Context, d CopyDeps, result Result, localCopyDir, sentFingerprint string) (*PreparedResult, error) {
	if sentFingerprint == "" {
		return nil, errors.New("the sent task copy's fingerprint is missing")
	}
	return prepareCopy(ctx, d, result, localCopyDir, sentFingerprint)
}
func unchangedCopy(ctx context.Context, path, fingerprint string) error {
	now, err := copyFingerprint(ctx, path)
	if err != nil {
		return err
	}
	if now != fingerprint {
		return ErrCopyChanged
	}
	return nil
}

func allowedCopy(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// Resolve existing parents too, so a missing destination through a symlink
	// cannot bypass the same home-folder refusal the capture engine applies.
	parent := absolute
	for {
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			rel, _ := filepath.Rel(parent, absolute)
			absolute = filepath.Join(resolved, rel)
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", err
		}
		parent = next
	}
	house, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(house); err == nil {
		house = resolved
	}
	rel, err := filepath.Rel(absolute, house)
	if filepath.Dir(absolute) == absolute || err == nil && filepath.IsLocal(rel) {
		return "", errors.New("a home folder or a folder above it cannot be sent as a task copy")
	}
	return absolute, nil
}

func copyFingerprint(ctx context.Context, root string) (string, error) {
	hash := sha256.New()
	for _, args := range [][]string{{"rev-parse", "HEAD"}, {"symbolic-ref", "-q", "HEAD"}, {"diff", "--cached", "--binary"}} {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
		out, err := cmd.Output()
		if err != nil && args[0] != "symbolic-ref" {
			return "", err
		}
		fmt.Fprintf(hash, "%d:", len(out))
		_, _ = hash.Write(out)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	state, err := taskcopy.InspectPortableState(root)
	if err != nil {
		return "", err
	}
	digest, err := state.Digest()
	if err != nil {
		return "", err
	}
	_, _ = hash.Write([]byte(digest))
	return hex.EncodeToString(hash.Sum(nil)), ctx.Err()
}

// CheckCopy fences a person's local replacement before any remote abandonment
// is recorded or stop is sent. It uses the same fingerprint as result restore.
func CheckCopy(ctx context.Context, path, fingerprint string) error {
	if fingerprint == "" {
		return errors.New("the sent task copy's fingerprint is missing")
	}
	return unchangedCopy(ctx, path, fingerprint)
}

// CopyFingerprint lets a failed transfer retain a local replacement check
// even when no input head reached the other computer.
func CopyFingerprint(ctx context.Context, path string) (string, error) {
	return copyFingerprint(ctx, path)
}
