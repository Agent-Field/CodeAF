package handoff

import (
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// arrivalOf is the file a takeover holds a lock on while the chat at root is on
// its way here. It sits beside the root and not inside it, because the root is
// replaced whole by the takeover.
func arrivalOf(root string) string { return root + ".arriving" }

// Arrive says the chat at root is being taken over on this machine, until the
// returned release runs. It is what keeps a window from opening the chat in the
// middle of its own takeover: a session booted then reads the journal as it was
// before the swap and keeps writing to the folder that is about to be replaced,
// and the window the person lands in is bound to a chat that has already moved
// on (every tool call refused as `continued elsewhere`, no turn saved).
//
// THE CLAIM IS A LOCK AND NOT A MARKER FILE, so a takeover that dies leaves
// nothing behind: the kernel drops the lock with the process, and the chat opens
// again without anyone cleaning up. A claim that cannot be made says nothing
// stops the takeover; it only runs without the fence, as it did before.
func Arrive(root string) (release func()) {
	path := arrivalOf(root)
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return func() {}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}
	}
	if filelock.Lock(file, true, true) != nil {
		_ = file.Close()
		return func() {}
	}
	return func() {
		_ = os.Remove(path)
		_ = filelock.Unlock(file)
		_ = file.Close()
	}
}

// Arriving says a takeover of the chat at root is running now, in this process
// or another one. It is asked by whatever opens a session on the chat.
func Arriving(root string) bool {
	file, err := os.Open(arrivalOf(root))
	if err != nil {
		return false
	}
	defer file.Close()
	if err := filelock.Lock(file, true, true); err != nil {
		return filelock.IsBusy(err)
	}
	_ = filelock.Unlock(file)
	return false
}
