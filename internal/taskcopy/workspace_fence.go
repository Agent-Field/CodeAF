package taskcopy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

// LockWorkspaceCheckpoint coordinates checkpoint writes with captures.
// The lock lives in private chat machinery, outside the portable .cell state.
// Every profile takes the fence before examining portable workspace state.
func LockWorkspaceCheckpoint(ctx context.Context, c cell.Cell) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(c.Root) {
		return nil, errors.New("workspace checkpoint fence requires an owned absolute chat root")
	}
	file, err := openCheckpointLock(c.Root)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			file.Close()
			return nil, err
		}
		err := filelock.Lock(file, true, true)
		if err == nil {
			var once sync.Once
			return func() { once.Do(func() { filelock.Unlock(file); file.Close() }) }, nil
		}
		if !filelock.IsBusy(err) {
			file.Close()
			return nil, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func openCheckpointLock(chat string) (*os.File, error) {
	root, err := os.OpenRoot(chat)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, directory := range []string{cell.TaskDropping, filepath.Join(cell.TaskDropping, "workspace")} {
		if err := root.Mkdir(directory, 0700); err != nil && !os.IsExist(err) {
			return nil, err
		}
		info, err := root.Lstat(directory)
		if err != nil || !info.IsDir() {
			return nil, errors.New("workspace checkpoint lock directory is not private chat machinery")
		}
	}
	name := filepath.Join(cell.TaskDropping, "workspace", "checkpoint.lock")
	file, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		info, statErr := root.Lstat(name)
		if statErr != nil || !info.Mode().IsRegular() || info.Size() != 0 {
			return nil, errors.New("workspace checkpoint lock is not an empty regular file")
		}
		file, err = root.OpenFile(name, os.O_RDWR, 0)
	}
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	bound, pathErr := root.Lstat(name)
	if err != nil || pathErr != nil || !opened.Mode().IsRegular() || opened.Size() != 0 || !bound.Mode().IsRegular() || !os.SameFile(opened, bound) {
		file.Close()
		return nil, errors.New("workspace checkpoint lock binding changed")
	}
	return file, nil
}
