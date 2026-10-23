package cellstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CommitEvery bounds the power-loss window in C28. Commands write to the kernel
// in order; one background group commit supplies disk durability separately.
const CommitEvery = 250 * time.Millisecond

type commitPath struct{ owner, path string }

// commitQueue keeps generations, rather than open descriptors, because a WAL
// compaction replaces its inode. A concurrent write always needs another sync.
type commitQueue struct {
	mu          sync.Mutex
	pending     map[commitPath]uint64
	next        uint64
	timer       bool
	after       func(time.Duration, func())
	flushing    chan struct{}
	syncPath    func(string) error
	beforeFlush func(context.Context, string) error
}

var commits = func() *commitQueue {
	q := newCommitQueue(syncPath)
	q.beforeFlush = flushEngines
	return q
}()

func newCommitQueue(syncFile func(string) error) *commitQueue {
	return &commitQueue{pending: map[commitPath]uint64{}, flushing: make(chan struct{}, 1), syncPath: syncFile, after: func(d time.Duration, f func()) { time.AfterFunc(d, f) }}
}

func (q *commitQueue) mark(owner, path string, parent bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.next++
	q.pending[commitPath{owner, path}] = q.next
	if parent {
		q.pending[commitPath{owner, filepath.Dir(path)}] = q.next
		q.pending[commitPath{owner, filepath.Dir(filepath.Dir(path))}] = q.next
	}
	if !q.timer {
		q.timer = true
		q.after(CommitEvery, q.background)
	}
}

func (q *commitQueue) background() {
	// A broken chat must not prevent another chat's records from meeting the
	// same power-loss window. Each owner retries its own dependency separately.
	q.mu.Lock()
	owners := map[string]bool{}
	for path := range q.pending {
		owners[path.owner] = true
	}
	q.mu.Unlock()
	for owner := range owners {
		_ = q.flush(context.Background(), owner)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.timer = false
	// Failed writes remain owed. A later boundary reports their actual error;
	// retrying here must never acknowledge a record that did not reach disk.
	if len(q.pending) > 0 {
		q.timer = true
		q.after(CommitEvery, q.background)
	}
}

// flush joins the current group commit without holding up record writes on its
// mutex. Only explicit durability boundaries wait for sync; commands never do.
func (q *commitQueue) flush(ctx context.Context, owner string) error {
	select {
	case q.flushing <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-q.flushing }()
	for {
		q.mu.Lock()
		batch := map[commitPath]uint64{}
		for p, version := range q.pending {
			if owner == "" || p.owner == owner {
				batch[p] = version
			}
		}
		q.mu.Unlock()
		if len(batch) == 0 {
			return nil
		}
		// A durable local turn must never outrun the engine snapshot it names.
		// The background group and explicit boundaries share this dependency (C28).
		if q.beforeFlush != nil {
			if err := q.beforeFlush(ctx, owner); err != nil {
				return err
			}
		}
		// File contents precede directory entries. A rename must not outlive the
		// replacement WAL's data after power loss (C28).
		for _, dirs := range []bool{false, true} {
			for p, version := range batch {
				if err := ctx.Err(); err != nil {
					return err
				}
				info, err := os.Stat(p.path)
				if errors.Is(err, os.ErrNotExist) {
					if err := missingRecordParent(p.path); err != nil {
						return fmt.Errorf("save call records: %w", err)
					}
					q.clear(p, version)
					continue
				}
				if err != nil {
					return fmt.Errorf("save call records: %w", err)
				}
				if info.IsDir() != dirs {
					continue
				}
				if err := q.syncPath(p.path); err != nil {
					return fmt.Errorf("save call records: %w", err)
				}
				q.clear(p, version)
			}
		}
	}
}

func (q *commitQueue) clear(p commitPath, version uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.pending[p] == version {
		delete(q.pending, p)
	}
}

// missingRecordParent distinguishes a removed record from a corrupt path.
// Windows reports both a missing name and a regular-file ancestor as NOT_FOUND;
// only the former can be cleared as a renamed or removed queue entry (C28/U4).
func missingRecordParent(path string) error {
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		info, err := os.Stat(dir)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("%s is not a directory", dir)
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if filepath.Dir(dir) == dir {
			return nil
		}
	}
}

// Flush commits every local record before the CLI exits. Conversation close
// drains its capture first; this final boundary also covers standalone verbs.
func Flush(ctx context.Context) error {
	// A seat may have started warming before opening its agent failed. Such a
	// capture still belongs to this process and must finish before exit (E2).
	captures.Lock()
	active := make([]*captureState, 0, len(captures.cells))
	for root, state := range captures.cells {
		select {
		case <-state.done:
			// A removed temporary home no longer owns state in this process. Live
			// homes still report a completed failed capture at process exit (U1).
			if _, err := os.Stat(root); os.IsNotExist(err) {
				delete(captures.cells, root)
				continue
			}
		default:
		}
		active = append(active, state)
	}
	captures.Unlock()
	var fault error
	for _, state := range active {
		if err := awaitCapture(ctx, state.done); err != nil {
			fault = errors.Join(fault, err)
			continue
		}
		fault = errors.Join(fault, state.err)
	}
	return errors.Join(fault, durable(ctx, ""))
}
