package blobstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// CheckReferences reads the indexed head object and each declared frame before
// the relay attests an upload. The head's remote ID is supplied separately from
// its local turn ID: sealed object IDs are keyed and cannot be derived here.
// The encrypted closure remains the publishing client's responsibility; frame
// plans are incremental hints, so an empty plan never means an empty closure.
func CheckReferences(ctx context.Context, store Store, headRID string, frames []string) error {
	if durable, ok := store.(interface {
		ConfirmReferences(context.Context, string, []string) error
	}); ok {
		return durable.ConfirmReferences(ctx, headRID, frames)
	}
	locations, err := store.Locate(ctx, []string{headRID})
	if err != nil {
		return err
	}
	head, ok := locations[headRID]
	if !ok {
		return ErrNotFound
	}
	return checkReferences(ctx, store, headRID, frames, head.Frame)
}
func checkReferences(ctx context.Context, store Store, headRID string, frames []string, headFrame string) error {
	if _, err := store.Get(ctx, headRID); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, id := range append([]string{headFrame}, frames...) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		b, err := store.GetFrame(ctx, id)
		if err != nil {
			return err
		}
		if IDOf(b) != id {
			return ErrDamaged
		}
		_, objects, err := Decode(b)
		if err != nil {
			return err
		}
		for _, o := range objects {
			have, err := store.Get(ctx, o.RID)
			if err != nil {
				return err
			}
			if !bytes.Equal(have, o.Bytes) {
				return fmt.Errorf("%w: indexed upload", ErrDamaged)
			}
		}
	}
	return nil
}

// ConfirmReferences repairs a lost directory-flush completion before issuing a
// receipt. File contents precede every rename; syncing all ancestor directories
// also covers freshly created object shards and identity namespace directories.
func (d *Disk) ConfirmReferences(ctx context.Context, headRID string, frames []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := checkGet(headRID); err != nil {
		return err
	}
	line, err := os.ReadFile(d.pointerPath(headRID))
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	location, err := parsePointer(line)
	if err != nil {
		return err
	}
	if err := checkReferences(ctx, d, headRID, frames, location.Frame); err != nil {
		return err
	}
	paths := []string{d.frames, d.objects, filepath.Dir(d.pointerPath(headRID))}
	// Object paths have only 256 possible shards. Sync those directories
	// directly instead of retaining one duplicate path per referenced object.
	shards, err := os.ReadDir(d.objects)
	if err != nil {
		return err
	}
	for _, shard := range shards {
		if shard.IsDir() && isLowerHex(shard.Name(), 2) {
			paths = append(paths, filepath.Join(d.objects, shard.Name()))
		}
	}
	seen := map[string]bool{}
	var dirs []*os.File
	defer func() {
		for _, f := range dirs {
			_ = f.Close()
		}
	}()
	for _, path := range paths {
		for !seen[path] {
			seen[path] = true
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			dirs = append(dirs, f)
			parent := filepath.Dir(path)
			if parent == path {
				break
			}
			path = parent
		}
	}
	return d.sync(dirs)
}
