package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// ── ONE PUBLISHER PER HOME ──────────────────────────────────────────────────
//
// The desktop engine and a terminal on the same home both offer this
// computer's existing chats to the relay. They once each ran their own pass:
// two scans, two exports and two uploads of the same chat, with only Create's
// absence check keeping one directory record. Older chats (a journal and no
// cell) were copied by the terminal alone, so with only the app running they
// were never shared.
//
// WHOEVER HOLDS THE HOME'S SHARING LOCK PUBLISHES. Each process asks for the
// lock on every round without waiting. The holder publishes the cell chats and
// copies at most one older chat a round; the other stays out of the way until
// the holder quits, when the lock is free and the next round takes it over. A
// finished copy is published by the ordinary pass (syncsetup.LocalChatRoots),
// under the same progress budget and order as every other chat.
//
// Counting what is not shared yet needs no lock: it reads only the disk.

// sharingLockName is the home's sharing lock, beside its projects.
const sharingLockName = "sharing.lock"

// homeSharing is one process's part in sharing a home's existing chats.
type homeSharing struct {
	mu    sync.Mutex
	home  string
	lock  *os.File
	older olderChats
	// counted and countedAt keep the older-chat count between recounts.
	counted   syncsetup.Unshared
	countedAt time.Time
	// shown is the last whole count, nil while sync is off; under mu.
	shown *syncsetup.Unshared
}

// unsharedRecount is how often the older chats on disk are counted again.
const unsharedRecount = 30 * time.Second

func newHomeSharing(root string) *homeSharing {
	return &homeSharing{home: root, older: olderChats{copy: func(ctx context.Context, dir string) (cell.Cell, error) {
		return copyOlderChatIn(ctx, root, dir)
	}}}
}

// claim answers whether this process publishes for the home, taking the lock
// when it is free. A held lock stays held until release.
func (h *homeSharing) claim() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lock != nil {
		return true
	}
	path := filepath.Join(h.home, "v3", sharingLockName)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return false
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return false
	}
	if err := filelock.Lock(f, true, true); err != nil {
		f.Close()
		return false
	}
	h.lock = f
	return true
}

// release gives the home's sharing to whichever process asks next.
func (h *homeSharing) release() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lock != nil {
		_ = filelock.Unlock(h.lock)
		_ = h.lock.Close()
		h.lock = nil
	}
}

// copyOne copies the first older chat that is due and answers whether it did.
// The ordinary pass publishes the copy on its next round.
func (h *homeSharing) copyOne(ctx context.Context) bool {
	for _, dir := range olderChatDirs(h.home) {
		if ctx.Err() != nil {
			return false
		}
		if _, _, ok := h.older.visit(ctx, dir); ok {
			h.countedAt = time.Time{}
			return true
		}
	}
	return false
}

// olderChatDirs are the chat folders a copy could be made from.
func olderChatDirs(home string) []string {
	dirs, _ := filepath.Glob(filepath.Join(home, "v3", "projects", "*", "*"))
	out := dirs[:0]
	for _, dir := range dirs {
		if strings.HasSuffix(dir, handoff.StagingSuffix) || strings.HasPrefix(filepath.Base(dir), ".") {
			continue
		}
		out = append(out, dir)
	}
	return out
}

// unshared counts every chat on this computer the other computers cannot list
// yet: the cell chats s has not shared, and the older chats not copied yet.
func (h *homeSharing) unshared(s *syncsetup.Sync, now time.Time) syncsetup.Unshared {
	cells, err := s.CountUnshared()
	if err != nil {
		cells = syncsetup.Unshared{}
	}
	if h.countedAt.IsZero() || now.Sub(h.countedAt) >= unsharedRecount {
		h.counted, h.countedAt = h.older.count(olderChatDirs(h.home), now), now
	}
	cells.Waiting += h.counted.Waiting
	cells.Failing += h.counted.Failing
	cells.KeptHere += h.counted.KeptHere
	return cells
}

// note counts again what is not shared and keeps it for last.
func (h *homeSharing) note(s *syncsetup.Sync, now time.Time) syncsetup.Unshared {
	u := h.unshared(s, now)
	h.mu.Lock()
	h.shown = &u
	h.mu.Unlock()
	return u
}

// forget drops the count while sync is off: nothing is waiting to be shared.
func (h *homeSharing) forget() {
	h.mu.Lock()
	h.shown = nil
	h.mu.Unlock()
}

// last is the newest count, and false before the first or while sync is off.
func (h *homeSharing) last() (syncsetup.Unshared, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.shown == nil {
		return syncsetup.Unshared{}, false
	}
	return *h.shown, true
}

// count sorts the older chats that have no copy yet: due for a copy, failed
// and waiting for the chat to change, or kept here by a rule
// (olderChatRecent, olderChatCopyable). A chat with a copy is counted with the
// cell chats, unless it is one the home-folder rule keeps here.
func (o *olderChats) count(dirs []string, now time.Time) syncsetup.Unshared {
	var out syncsetup.Unshared
	for _, dir := range dirs {
		journal, err := os.Stat(filepath.Join(dir, "transcript.jsonl"))
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, cell.MetaPath)); err == nil {
			continue
		}
		meta, err := session.LoadMeta(dir)
		if err != nil || meta.ID == "" {
			continue
		}
		if cell.Copied(dir) != dir {
			// The copy of a chat that may be shared is a cell chat; the copy
			// of one in the home folder stays here.
			if !olderChatCopyable(meta) {
				out.KeptHere++
			}
			continue
		}
		switch {
		case o.failed[dir] == olderChatStamp{size: journal.Size(), mod: journal.ModTime()}:
			out.Failing++
		case meta.LastUserAt.IsZero() || now.Sub(meta.LastUserAt) > olderChatRecent || !olderChatCopyable(meta):
			out.KeptHere++
		default:
			out.Waiting++
		}
	}
	return out
}

// terminalNotShared is the terminal's count for the screen (tui3.NotShared).
func terminalNotShared() (tui3.NotShared, bool) {
	h := terminalSharing.Load()
	if h == nil {
		return tui3.NotShared{}, false
	}
	u, ok := h.last()
	return tui3.NotShared{Waiting: u.Waiting, Failing: u.Failing, KeptHere: u.KeptHere}, ok
}
