package syncsetup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/home"
)

type sendLease struct {
	Cell  string `json:"cell"`
	Fence uint64 `json:"fence"`
}

func (s *Sync) sendLeasePath(id string) (string, error) {
	if strings.TrimSpace(s.Home) == "" {
		return "", errors.New("sync: a home directory is required for chat holds")
	}
	key := sha256.Sum256([]byte(s.Ledger + "/" + s.Device.ID() + "/" + id))
	return filepath.Join(s.Home, "v3", "sync", "holds", hex.EncodeToString(key[:])), nil
}

// SendLease names the hold the local session actually publishes under. It
// stays outside the saved chat so restoring another computer's copy cannot
// grant this window permission to send.
func (s *Sync) SendLease(id string) (string, uint64, error) {
	path, err := s.sendLeasePath(id)
	if err != nil {
		return "", 0, err
	}
	current, err := s.currentDisplaySync()
	if err != nil {
		return "", 0, err
	}
	if current != s {
		return current.SendLease(id)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	var held sendLease
	err = json.Unmarshal(raw, &held)
	return held.Cell, held.Fence, err
}

func (d *Drive) recordSendLease() error {
	fence := d.batcher.Fence()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.lost || d.viewer.Line != "" || fence == 0 || fence == d.recordedFence {
		return nil
	}
	if err := d.sync.recordSendLease(d.cell.ID, sendLease{Cell: d.directoryID, Fence: fence}); err != nil {
		return err
	}
	d.recordedFence = fence
	return nil
}

func (d *Drive) recordSendLeaseWithin(ctx context.Context) error {
	if err := lockDrive(ctx, &d.receiptMu); err != nil {
		return err
	}
	defer d.receiptMu.Unlock()
	fence := d.batcher.Fence()
	d.mu.Lock()
	if d.lost || d.viewer.Line != "" || fence == 0 || fence == d.recordedFence {
		d.mu.Unlock()
		return nil
	}
	d.mu.Unlock()
	held := sendLease{Cell: d.directoryID, Fence: fence}
	if err := d.sync.recordSendLeaseWithin(ctx, d.cell.ID, held); err != nil {
		return err
	}
	d.mu.Lock()
	if d.stopped || d.lost || d.viewer.Line != "" || d.batcher.Fence() != fence {
		d.mu.Unlock()
		return d.sync.forgetSendLeaseWithin(ctx, d.cell.ID, held)
	}
	d.recordedFence = fence
	d.mu.Unlock()
	return nil
}

// reconcileSendLease removes a dead writer's receipt only after a fresh relay reading.
// Conditional removal preserves a newer writer that recorded its hold meanwhile.
func (s *Sync) reconcileSendLease(id, remoteID string, view directory.CellView) error {
	remote, fence, err := s.SendLease(id)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	lease := view.Cell.Lease
	if remote == remoteID && (lease.Device != s.Device.ID() || lease.Fence != fence || lease.Expires <= view.Now) {
		return s.forgetSendLease(id, sendLease{Cell: remote, Fence: fence})
	}
	return nil
}

func (s *Sync) recordSendLease(id string, held sendLease) error {
	lock, err := s.lockChat(id, "")
	if err != nil {
		return err
	}
	defer lock.Close()
	raw, err := json.Marshal(held)
	if err != nil {
		return err
	}
	path, err := s.sendLeasePath(id)
	if err != nil {
		return err
	}
	if err := home.MkdirAllUnder(s.Home, filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hold-*")
	if err != nil {
		return err
	}
	defer fsreplace.Remove(tmp.Name())
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

func (s *Sync) recordSendLeaseWithin(ctx context.Context, id string, held sendLease) error {
	lock, err := s.lockChatWithin(ctx, id, "")
	if err != nil {
		return err
	}
	defer lock.Close()
	raw, err := json.Marshal(held)
	if err != nil {
		return err
	}
	path, err := s.sendLeasePath(id)
	if err != nil {
		return err
	}
	if before, err := os.ReadFile(path); err == nil {
		var current sendLease
		if json.Unmarshal(before, &current) == nil && current.Cell == held.Cell && current.Fence > held.Fence {
			return directory.ErrFenceStale
		}
	}
	if err := home.MkdirAllUnder(s.Home, filepath.Dir(path), 0700); err != nil {
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

// forgetSendLease retires only this writer's fence, preserving a newer local owner.
func (s *Sync) forgetSendLease(id string, expected sendLease) error {
	lock, err := s.lockChat(id, "")
	if err != nil {
		return err
	}
	defer lock.Close()
	path, err := s.sendLeasePath(id)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var held sendLease
	if err := json.Unmarshal(raw, &held); err != nil {
		return err
	}
	if held != expected {
		return nil
	}
	err = fsreplace.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Sync) forgetSendLeaseWithin(ctx context.Context, id string, expected sendLease) error {
	lock, err := s.lockChatWithin(ctx, id, "")
	if err != nil {
		return err
	}
	defer lock.Close()
	path, err := s.sendLeasePath(id)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var held sendLease
	if err := json.Unmarshal(raw, &held); err != nil {
		return err
	}
	if held != expected {
		return nil
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Sync) lockChat(id, suffix string) (*os.File, error) {
	path, err := s.sendLeasePath(id)
	if err != nil {
		return nil, err
	}
	if err := home.MkdirAllUnder(s.Home, filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+suffix+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(lock, true, false); err != nil {
		lock.Close()
		return nil, err
	}
	return lock, nil
}

func (s *Sync) lockChatWithin(ctx context.Context, id, suffix string) (*os.File, error) {
	path, err := s.sendLeasePath(id)
	if err != nil {
		return nil, err
	}
	if err := home.MkdirAllUnder(s.Home, filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+suffix+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			lock.Close()
			return nil, err
		}
		err := filelock.Lock(lock, true, true)
		if err == nil {
			return lock, nil
		}
		if !filelock.IsBusy(err) {
			lock.Close()
			return nil, err
		}
		timer := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			lock.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
