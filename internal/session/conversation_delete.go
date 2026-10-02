package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// A tombstone prevents stale tabs and other windows from recreating a deleted
// journal. Its companions remain available as deliverables; the transcript goes.
const conversationDeletedFile = ".conversation-deleted"

// SessionPath identifies the journal a process owns without exposing its config.
func (a *Agent) SessionPath() string { return a.config.SessionFile }

// DeleteConversationUnder admits only a recorded conversation under this engine's
// places root. Work is stopped through its owner before the transcript's lock is
// claimed, and manager choices are rechecked in the same store write as removal.
func DeleteConversationUnder(root, profile, file string, choices map[string]string, stop func(string) error, affected ...map[string][]string) error {
	canonical := func(path string) (string, error) { return filepath.EvalSymlinks(filepath.Clean(path)) }
	resolved, err := deletedConversationPath(file)
	if err != nil {
		return err
	}
	resolvedRoot, err := canonical(root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.Base(resolved) != placeTranscript {
		return errors.New("that conversation is not in this machine's saved conversations")
	}
	meta, err := LoadMeta(filepath.Dir(resolved))
	if err != nil || meta.ID == "" || meta.ID != filepath.Base(filepath.Dir(resolved)) {
		return errors.New("that path is not a saved conversation")
	}
	deletionLock, err := os.OpenFile(filepath.Join(filepath.Dir(resolved), ".conversation-delete.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer deletionLock.Close()
	if err = filelock.Lock(deletionLock, true, true); err != nil {
		return errors.New("this conversation is already being deleted")
	}
	defer filelock.Unlock(deletionLock)
	if _, err := os.Stat(filepath.Join(filepath.Dir(resolved), conversationDeletedFile)); err == nil {
		return finishConversationTaskCleanup(profile, resolved, meta.ID, choices, affected...)
	}
	f, err := teams.Load(profile)
	if err != nil {
		return err
	}
	aliases := deletedMemberAliases(f, resolved)
	canonicalizeDeletedMember(f, resolved, aliases)
	if err = f.RemoveConversation(resolved, choices, time.Now(), affected...); err != nil {
		return err
	}
	if stop != nil {
		if err = stop(resolved); err != nil {
			return err
		}
	}
	journal, err := os.OpenFile(resolved, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer journal.Close()
	if err = claimDeletionJournal(journal, filepath.Dir(resolved)); err != nil {
		return err
	}
	defer filelock.Unlock(journal)
	taskRows, err := taskDeleteRows(resolved, meta.ID, nil)
	if err != nil {
		return err
	}
	if err = saveConversationTaskCleanup(resolved, taskRows, aliases); err != nil {
		return err
	}
	marker := filepath.Join(filepath.Dir(resolved), conversationDeletedFile)
	if err = os.WriteFile(marker, []byte("This conversation was permanently deleted.\n"), 0o600); err != nil {
		return err
	}
	pending := resolved + ".delete-pending"
	if err = os.Rename(resolved, pending); err != nil {
		_ = os.Remove(marker)
		return err
	}
	if err = teams.Update(profile, func(f *teams.File) error {
		canonicalizeDeletedMember(f, resolved, aliases)
		return f.RemoveConversation(resolved, choices, time.Now(), affected...)
	}); err != nil {
		if restoreErr := os.Rename(pending, resolved); restoreErr != nil {
			return fmt.Errorf("deletion failed: %v; transcript retained at %s: %w", err, pending, restoreErr)
		}
		_ = os.Remove(marker)
		return err
	}
	return finishConversationTaskCleanup(profile, resolved, meta.ID, choices, affected...)
}

// Hosted keys may retain a symlink spelling; every spelling names the same
// deletion and must be removed under the store's lock.
func canonicalizeDeletedMember(f *teams.File, target string, aliases map[string]bool) {
	for i := range f.Teams {
		t := &f.Teams[i]
		for j := range t.Members {
			m := &t.Members[j]
			dir, err := filepath.EvalSymlinks(filepath.Dir(m.Key))
			resolved := filepath.Join(dir, filepath.Base(m.Key))
			if aliases[m.Key] || err == nil && resolved == target {
				if t.Manager == m.Key {
					t.Manager = target
				}
				m.Key = target
			}
		}
	}
}

const conversationDeleteRequest = ".conversation-delete-request"

// An owner in another workspace host is asked to finish closing its journal.
// The request is local to the engine machine and is never written by the UI.
func claimDeletionJournal(journal *os.File, dir string) error {
	err := filelock.Lock(journal, true, true)
	if err == nil {
		return nil
	}
	if !filelock.IsBusy(err) {
		return err
	}
	request := filepath.Join(dir, conversationDeleteRequest)
	if err := os.WriteFile(request, []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o600); err != nil {
		return err
	}
	defer os.Remove(request)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-deadline.C:
			return errors.New("the conversation's owner could not finish stopping; its transcript and memberships are kept")
		case <-ticker.C:
			if err := filelock.Lock(journal, true, true); err == nil {
				return nil
			} else if !filelock.IsBusy(err) {
				return err
			}
		}
	}
}

// The presence beat hands closing to a separate goroutine because Close waits
// for that beat to leave. A stale request cannot stop work after its asker left.
func (a *Agent) drainConversationDeletion() {
	if a.config.InTask || a.config.Place.Dir == "" {
		return
	}
	path := filepath.Join(a.config.Place.Dir, conversationDeleteRequest)
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	at, err := time.Parse(time.RFC3339Nano, string(raw))
	if err != nil || time.Since(at) > time.Minute {
		_ = os.Remove(path)
		return
	}
	if os.Remove(path) != nil {
		return
	}
	guard.Go("stopping a permanently deleted conversation", func() { a.InterruptFor(StopByPerson); _ = a.Close() })
}

// Full-path aliases must be resolved while the original journal still exists.
func deletedMemberAliases(f *teams.File, target string) map[string]bool {
	aliases := map[string]bool{target: true}
	for _, t := range f.Teams {
		for _, m := range t.Members {
			if real, err := filepath.EvalSymlinks(m.Key); err == nil && real == target {
				aliases[m.Key] = true
			}
		}
	}
	return aliases
}
