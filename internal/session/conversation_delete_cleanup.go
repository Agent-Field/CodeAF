package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

const conversationTaskCleanupFile = ".delete-task-cleanup.json"

// The normal transcript may have become a pending deletion already. Only a
// canonical saved folder with its durable deletion marker admits that retry.
func deletedConversationPath(file string) (string, error) {
	real, err := filepath.EvalSymlinks(filepath.Clean(file))
	if err == nil {
		return real, nil
	}
	if !os.IsNotExist(err) || filepath.Base(file) != placeTranscript {
		return "", err
	}
	dir, e := filepath.EvalSymlinks(filepath.Dir(file))
	if e != nil {
		return "", e
	}
	if _, e = os.Stat(filepath.Join(dir, conversationDeletedFile)); e != nil {
		return "", err
	}
	return filepath.Join(dir, placeTranscript), nil
}

type conversationTaskCleanup struct {
	Rows     []TaskIndexEntry
	Aliases  map[string]bool
	Choices  map[string]string
	Affected map[string][]string
}

func saveConversationTaskCleanup(file string, rows []TaskIndexEntry, aliases ...map[string]bool) error {
	receipt := conversationTaskCleanup{Rows: rows}
	if len(aliases) > 0 {
		receipt.Aliases = aliases[0]
	}
	return writeConversationTaskCleanup(file, receipt)
}

func writeConversationTaskCleanup(file string, receipt conversationTaskCleanup) error {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	dir := filepath.Dir(file)
	tmp, err := os.CreateTemp(dir, ".delete-task-cleanup-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(raw); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, conversationTaskCleanupFile))
}

// The receipt survives every cleanup error. Retrying must recheck memberships
// before removing records, including a crash between marking and team removal.
func finishConversationTaskCleanup(profile, file, chat string, choices map[string]string, affected ...map[string][]string) error {
	receipt := filepath.Join(filepath.Dir(file), conversationTaskCleanupFile)
	saved, err := readConversationTaskCleanup(file)
	if err != nil {
		return err
	}
	if err = validateDeletionPaths(file, chat, nil, saved.Rows); err != nil {
		return err
	}
	if err = finishDeletedMemberships(profile, file, choices, saved.Aliases, affected...); err != nil {
		return err
	}
	if err = purgeDeletedPlanTasks(file, chat, nil); err != nil {
		return err
	}
	if err = purgeTaskRecords(file, chat, nil, saved.Rows, true); err != nil {
		return err
	}
	if err = removeDeletedConversationJournal(file); err != nil {
		return err
	}
	if err = os.Remove(filepath.Join(filepath.Dir(file), taskCleanupFile)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Remove(receipt)
}

func finishDeletedMemberships(profile, file string, choices map[string]string, savedAliases map[string]bool, affected ...map[string][]string) error {
	return teams.Update(profile, func(f *teams.File) error {
		aliases := deletedMemberAliases(f, file)
		for key, value := range savedAliases {
			aliases[key] = value
		}
		canonicalizeDeletedMember(f, file, aliases)
		remaining := remainingDeletionChoices(f, file, choices)
		return f.RemoveConversation(file, remaining, time.Now(), affected...)
	})
}

func removeDeletedConversationJournal(file string) error {
	for _, path := range []string{file + ".delete-pending", file} {
		journal, err := os.OpenFile(path, os.O_RDWR, 0)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		err = journal.Truncate(0)
		if closeErr := journal.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if err = os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func readConversationTaskCleanup(file string) (conversationTaskCleanup, error) {
	var saved conversationTaskCleanup
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), conversationTaskCleanupFile))
	if os.IsNotExist(err) {
		return saved, errors.New("this conversation was permanently deleted")
	}
	if err == nil {
		err = json.Unmarshal(raw, &saved)
	}
	return saved, err
}

// A successful membership write need not be replayed on a filesystem retry.
func remainingDeletionChoices(f *teams.File, file string, choices map[string]string) map[string]string {
	remaining := make(map[string]string)
	for id, replacement := range choices {
		if t, ok := f.Team(id); ok && (t.Manager == file || t.Holds(file)) {
			remaining[id] = replacement
		}
	}
	return remaining
}

// Recovery follows only the saved-conversation layout, never a symlinked bucket.
// The durable intent is revalidated through the same deletion door as a person.
func RecoverConversationDeletions(root, profile string) error {
	buckets, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var failures []error
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		dirs, err := os.ReadDir(filepath.Join(root, bucket.Name()))
		if err != nil {
			failures = append(failures, err)
			continue
		}
		for _, dir := range dirs {
			if !dir.IsDir() {
				continue
			}
			path := filepath.Join(root, bucket.Name(), dir.Name())
			if _, err := os.Stat(filepath.Join(path, conversationDeletedFile)); err != nil {
				continue
			}
			if _, err := os.Stat(filepath.Join(path, conversationTaskCleanupFile)); err != nil {
				continue
			}
			if err := DeleteConversationUnder(root, profile, filepath.Join(path, placeTranscript), nil, nil); err != nil {
				failures = append(failures, fmt.Errorf("finish conversation deletion in %s: %w", path, err))
			}
		}
	}
	return errors.Join(failures...)
}
