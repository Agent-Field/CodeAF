package session

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const taskCleanupFile = ".task-delete-cleanup.json"

type taskCleanup struct {
	IDs  map[string]bool
	Rows []TaskIndexEntry
}

func taskCleanupReceipts(file string) (map[string]taskCleanup, error) {
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), taskCleanupFile))
	if os.IsNotExist(err) {
		return map[string]taskCleanup{}, nil
	}
	if err != nil {
		return nil, err
	}
	var receipts map[string]taskCleanup
	if err = json.Unmarshal(raw, &receipts); err != nil {
		return nil, err
	}
	if receipts == nil {
		receipts = map[string]taskCleanup{}
	}
	return receipts, nil
}

func saveTaskCleanup(file, id string, ids map[string]bool, rows []TaskIndexEntry) error {
	receipts, err := taskCleanupReceipts(file)
	if err != nil {
		return err
	}
	receipts[id] = taskCleanup{IDs: ids, Rows: rows}
	return writeTaskCleanup(file, receipts)
}

func writeTaskCleanup(file string, receipts map[string]taskCleanup) error {
	path := filepath.Join(filepath.Dir(file), taskCleanupFile)
	if len(receipts) == 0 {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	raw, err := json.Marshal(receipts)
	if err != nil {
		return err
	}
	if err = os.WriteFile(path+".tmp", raw, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func finishTaskCleanup(owner *Agent, file, chat, id string) error {
	receipts, err := taskCleanupReceipts(file)
	if err != nil {
		return err
	}
	receipt := receipts[id]
	if err = finishTaskDeletion(owner, file, chat, receipt.IDs, receipt.Rows); err != nil {
		return err
	}
	delete(receipts, id)
	return writeTaskCleanup(file, receipts)
}

func pendingTaskCleanup(file, id string) (bool, error) {
	receipts, err := taskCleanupReceipts(file)
	if err != nil {
		return false, err
	}
	_, found := receipts[id]
	return found && taskDeletions(file)[id], nil
}
