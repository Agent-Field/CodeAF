package cellstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
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
//
// ONE GROUP COMMIT SAVES EVERY CHAT THAT IS WAITING. The slot is one per
// process, and each flush used to sync only its own chat's records while the
// rest queued behind it, so a quit closing 48 chats at once paid 48 rounds one
// after another; on macOS every sync is a full flush of the drive's cache, and
// the seventh live A+B run's closes stepped up three seconds at a time until
// the desk killed the engine at thirty. Whoever holds the slot now syncs every
// chat's pending records, each path once however many chats share it, and a
// chat whose records that round covered returns without a round of its own.
// Nothing is acknowledged that was not synced: a chat whose engine could not
// confirm its snapshot keeps every record owed and is the only one told.
func (q *commitQueue) flush(ctx context.Context, owner string) error {
	select {
	case q.flushing <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-q.flushing }()
	for {
		q.mu.Lock()
		batch := make(map[commitPath]uint64, len(q.pending))
		owed := false
		for p, version := range q.pending {
			batch[p] = version
			owed = owed || owner == "" || p.owner == owner
		}
		q.mu.Unlock()
		if !owed {
			return nil
		}
		failed := map[string]error{}
		// A durable local turn must never outrun the engine snapshot it names.
		// The background group and explicit boundaries share this dependency (C28).
		if q.beforeFlush != nil {
			for o := range ownersOf(batch) {
				if err := q.beforeFlush(ctx, o); err != nil {
					failed[o] = err
				}
			}
		}
		// File contents precede directory entries. A rename must not outlive the
		// replacement WAL's data after power loss (C28).
		for _, dirs := range []bool{false, true} {
			// Each path is synced once a pass, however many chats owe it.
			synced := map[string]error{}
			for p, version := range batch {
				if failed[p.owner] != nil {
					continue
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				err, done := synced[p.path]
				if !done {
					err = q.syncOne(p.path, dirs)
					synced[p.path] = err
				}
				switch {
				case errors.Is(err, errOtherKind):
				case err != nil:
					failed[p.owner] = err
				default:
					q.clear(p, version)
				}
			}
		}
		if owner != "" {
			if err := failed[owner]; err != nil {
				return err
			}
			continue
		}
		if err := firstFailure(failed); err != nil {
			return err
		}
	}
}

// errOtherKind is a path the current pass leaves for the other one: a file
// during the folders' pass, or a folder during the files'.
var errOtherKind = errors.New("synced in the other pass")

// syncOne syncs one record path in the pass for its kind. A removed record
// is cleared: its rename or removal is the parent folder's to keep.
func (q *commitQueue) syncOne(path string, dirs bool) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := missingRecordParent(path); err != nil {
			return fmt.Errorf("save call records: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("save call records: %w", err)
	}
	if info.IsDir() != dirs {
		return errOtherKind
	}
	if err := q.syncPath(path); err != nil {
		return fmt.Errorf("save call records: %w", err)
	}
	return nil
}

func ownersOf(batch map[commitPath]uint64) map[string]bool {
	owners := map[string]bool{}
	for p := range batch {
		owners[p.owner] = true
	}
	return owners
}

// firstFailure is one of the failures, the same one for the same set, for a
// flush of every chat.
func firstFailure(failed map[string]error) error {
	first := ""
	for o := range failed {
		if first == "" || o < first {
			first = o
		}
	}
	if first == "" {
		return nil
	}
	return failed[first]
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
// forces its capture first; this final boundary also covers standalone verbs.
// Held calls between steps are captured, while an open step stays in the WAL
// without making exit wait for its tool.
func Flush(ctx context.Context) error {
	// A seat may have started warming before opening its agent failed. Such a
	// capture still belongs to this process and must finish before exit (E2).
	captures.Lock()
	active := make(map[string]bool, len(captures.cells)+len(captures.held))
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
		active[root] = true
	}
	for root := range captures.held {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			delete(captures.held, root)
			continue
		}
		active[root] = true
	}
	captures.Unlock()
	var fault error
	for root := range active {
		fault = errors.Join(fault, Captured(ctx, cell.Cell{Root: root}))
	}
	return errors.Join(fault, durable(ctx, ""))
}
