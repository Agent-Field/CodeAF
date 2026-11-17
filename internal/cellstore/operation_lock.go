package cellstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// lockStoreOperation orders all codeaf transports by physical store, including
// different daemon sockets and processes. furrow 0.3.6's open-time missing-pack
// check otherwise combines a pre-seal absence with a post-seal frontier.
func lockStoreOperation(ctx context.Context, dataDir string) (func(), error) {
	// Targets without an explicit store let the engine choose its default. Real
	// Engine targets always name their store; transport health fixtures need none.
	if dataDir == "" {
		return func() {}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	// Keep the inode permanently: removing a locked file lets a new opener lock
	// a replacement while an earlier waiter still owns the unlinked inode.
	file, err := os.OpenFile(filepath.Join(canonicalTree(dataDir), ".codeaf-operation"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		err := filelock.Lock(file, true, true)
		if err == nil {
			return func() { _ = filelock.Unlock(file); _ = file.Close() }, nil
		}
		if !filelock.IsBusy(err) {
			_ = file.Close()
			return nil, fmt.Errorf("order saving operations: %w", err)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
