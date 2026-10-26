package syncsetup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

type sendLease struct {
	Cell  string `json:"cell"`
	Fence uint64 `json:"fence"`
}

func (s *Sync) sendLeasePath(id string) string {
	key := sha256.Sum256([]byte(s.Ledger + "/" + s.Device.ID() + "/" + id))
	return filepath.Join(s.Home, "v3", "sync", "holds", hex.EncodeToString(key[:]))
}

// SendLease names the hold the local session actually publishes under. It
// stays outside the saved chat so restoring another computer's copy cannot
// grant this window permission to send.
func (s *Sync) SendLease(id string) (string, uint64, error) {
	current, err := s.currentDisplaySync()
	if err != nil {
		return "", 0, err
	}
	if current != s {
		return current.SendLease(id)
	}
	raw, err := os.ReadFile(s.sendLeasePath(id))
	if err != nil {
		return "", 0, err
	}
	var held sendLease
	err = json.Unmarshal(raw, &held)
	return held.Cell, held.Fence, err
}

func (d *Drive) recordSendLease() error {
	fence := d.batcher.Fence()
	if fence == 0 || fence == d.recordedFence {
		return nil
	}
	if err := d.sync.recordSendLease(d.cell.ID, sendLease{Cell: d.directoryID, Fence: fence}); err != nil {
		return err
	}
	d.recordedFence = fence
	return nil
}

func (s *Sync) recordSendLease(id string, held sendLease) error {
	raw, err := json.Marshal(held)
	if err != nil {
		return err
	}
	path := s.sendLeasePath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hold-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.Write(raw)
	closeErr := tmp.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := fsreplace.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return nil
}
