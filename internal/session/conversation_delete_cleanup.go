package session

import (
	"encoding/json"
	"errors"
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
	Rows    []TaskIndexEntry
	Aliases map[string]bool
}

func saveConversationTaskCleanup(file string, rows []TaskIndexEntry, aliases ...map[string]bool) error {
	receipt := conversationTaskCleanup{Rows: rows}
	if len(aliases) > 0 {
		receipt.Aliases = aliases[0]
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(filepath.Dir(file), conversationTaskCleanupFile), raw, 0600)
}

// The receipt survives every cleanup error. Retrying must recheck memberships
// before removing records, including a crash between marking and team removal.
func finishConversationTaskCleanup(profile, file, chat string, choices map[string]string, affected ...map[string][]string) error {
	receipt := filepath.Join(filepath.Dir(file), conversationTaskCleanupFile)
	raw, err := os.ReadFile(receipt)
	if os.IsNotExist(err) {
		return errors.New("this conversation was permanently deleted")
	}
	if err != nil {
		return err
	}
	var saved conversationTaskCleanup
	if err = json.Unmarshal(raw, &saved); err != nil {
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
		remaining := make(map[string]string)
		// The store write is atomic, but filesystem cleanup can be retried after
		// it succeeds. Keep scope checks only for memberships still present.
		for id, replacement := range choices {
			if t, ok := f.Team(id); ok && (t.Manager == file || t.Holds(file)) {
				remaining[id] = replacement
			}
		}
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
