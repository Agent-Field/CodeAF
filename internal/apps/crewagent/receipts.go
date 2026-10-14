package crewagent

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

// LogReceipt keeps the approval cards and changed records alongside the crew's readable log.
type LogReceipt struct {
	ID     string         `json:"id"`
	From   string         `json:"from"`
	At     time.Time      `json:"at"`
	Text   string         `json:"text"`
	Member string         `json:"member"`
	Badge  bridge.Badge   `json:"badge"`
	Answer svc.CrewAnswer `json:"answer"`
}

func SaveReceipt(profile, crew string, receipt LogReceipt) error {
	dir := filepath.Join(profile, "teams", crew)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "app-agent-receipts.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer filelock.Unlock(lock)
	file, err := os.OpenFile(filepath.Join(dir, "app-agent-receipts.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(receipt); err != nil {
		return err
	}
	return file.Sync()
}

func Receipts(profile, crew string) ([]LogReceipt, error) {
	file, err := os.Open(filepath.Join(profile, "teams", crew, "app-agent-receipts.jsonl"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var receipts []LogReceipt
	dec := json.NewDecoder(file)
	for {
		var receipt LogReceipt
		err := dec.Decode(&receipt)
		if err == io.EOF {
			return receipts, nil
		}
		if err != nil {
			return nil, err
		}
		receipts = append(receipts, receipt)
	}
}
