package session

// The lock over one conversation folder's identity file.
//
// meta.json is replaced atomically, so every reader sees a whole identity, but
// replacing the file does not make the read before it part of the same act. A
// title stamp and a spend stamp can both read the old identity, change their own
// field, and then each rename a complete file over the other. An Agent mutex
// closed that hole for one window and left it open for a second Agent and every
// second process on the same conversation.
//
// The serialization belongs to the FOLDER, not to an Agent. A process-local
// per-directory mutex is the cheap first gate for this process's goroutines,
// and a file lock beside the identity makes every process meet on the same
// inode before any of them reads it.

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/filelock"
)

const (
	// metaLockPoll is how often a waiter re-asks for a held identity lock. The
	// guarded work is only a read, a marshal and a rename of a few hundred bytes,
	// so one millisecond is already longer than an ordinary holder needs.
	metaLockPoll = 1 * time.Millisecond
)

// Five seconds is far past any real metadata transaction and short enough that
// a wedged holder never parks a person's turn. metaLockPatience is a variable
// only so the bounded wait's own law can be tested.
var metaLockPatience = 5 * time.Second

// metaLocks is keyed by the cleaned conversation directory, giving every
// goroutine in this process that uses the same spelling one mutex. Clean is
// deliberate instead of EvalSymlinks: two symlink spellings still meet on the
// lock file's inode, and a stamp must not pay filesystem-resolution syscalls.
//
// It never gives an entry back, which is the right trade at this size: one
// mutex per conversation folder a process has written is bounded by the
// conversations that process has had, and a registry that pruned would have to
// prove nobody was mid-transaction to do it.
var metaLocks sync.Map

// withMetaLock runs one complete read, patch and rename while this process and
// every other process are excluded from that conversation's identity file.
//
// GOING AHEAD IS ALWAYS BETTER THAN REFUSING. A filesystem that cannot flock,
// a lock file that cannot be created, and a holder that outlasts the bounded
// wait all run the transaction unlocked. That is exactly what these stamps did
// before this lock existed, and placemeta.go's EVERY FAILURE IS SILENCE law
// makes a best-effort citation better than a refused conversation.
//
// The only error returned is the transaction's own. Locking is coordination,
// not a new failure the caller has to explain.
func withMetaLock(dir string, transaction func() error) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return transaction()
	}
	dir = filepath.Clean(dir)
	value, _ := metaLocks.LoadOrStore(dir, &sync.Mutex{})
	local := value.(*sync.Mutex)
	local.Lock()
	defer local.Unlock()

	file := claimMetaLock(dir)
	if file != nil {
		defer func() {
			// The unlock is belt-and-braces, as task_lock.go's is: closing the
			// descriptor drops the flock on its own. Keeping the order explicit
			// makes the release visible at the place it happens.
			_ = filelock.Unlock(file)
			_ = file.Close()
		}()
	}
	return transaction()
}

// claimMetaLock takes the cross-process half of the identity lock and returns
// nil when no lock can be taken before the bounded wait ends.
func claimMetaLock(dir string) *os.File {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil
	}
	file, err := os.OpenFile(filepath.Join(dir, placeMetaLock), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil
	}
	deadline := time.Now().Add(metaLockPatience)
	for {
		err := filelock.Lock(file, true, true)
		if err == nil {
			return file
		}
		if !filelock.IsBusy(err) || time.Now().After(deadline) {
			_ = file.Close()
			return nil
		}
		time.Sleep(metaLockPoll)
	}
}
