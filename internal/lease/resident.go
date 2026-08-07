// Package lease elects the one process currently serving as a resident for a
// durable Aforge store. The lock is only coordination; the journal remains the
// source of truth and another process may take the role as soon as it is free.
package lease

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	residentLockName = "resident.lock"
	maxPayloadBytes  = 16 << 10
)

// Resident describes the process whose open file descriptor currently holds
// resident.lock. A payload left behind without a live flock is stale and is
// deliberately ignored by ProbeResident.
type Resident struct {
	PID        int       `json:"pid"`
	Host       string    `json:"host"`
	Surface    string    `json:"surface"`
	AcquiredAt time.Time `json:"acquired_at"`
}

// AcquireResident attempts to become the resident for the store directory.
// On success heldBy is nil and release relinquishes the role. If another live
// process holds the lock, release is nil and heldBy describes that process.
func AcquireResident(dir, surface string) (release func() error, heldBy *Resident, err error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, nil, fmt.Errorf("acquire resident: empty store directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("acquire resident: create store directory: %w", err)
	}
	path := filepath.Join(dir, residentLockName)
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire resident: open lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if !lockBusy(err) {
			_ = file.Close()
			return nil, nil, fmt.Errorf("acquire resident: lock: %w", err)
		}
		holder, readErr := readResident(file)
		_ = file.Close()
		if readErr != nil {
			return nil, nil, fmt.Errorf("acquire resident: read holder: %w", readErr)
		}
		return nil, holder, nil
	}

	host, _ := os.Hostname()
	resident := Resident{
		PID:        os.Getpid(),
		Host:       strings.TrimSpace(host),
		Surface:    strings.TrimSpace(surface),
		AcquiredAt: time.Now().UTC(),
	}
	if resident.Surface == "" {
		resident.Surface = "unknown"
	}
	if err := writeResident(file, resident); err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, nil, fmt.Errorf("acquire resident: write holder: %w", err)
	}

	var once sync.Once
	var releaseErr error
	release = func() error {
		once.Do(func() {
			unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			closeErr := file.Close()
			releaseErr = errors.Join(unlockErr, closeErr)
		})
		return releaseErr
	}
	return release, nil, nil
}

// ProbeResident reports the live holder of resident.lock. It never trusts the
// JSON by itself: if a non-blocking flock succeeds, any payload is stale and
// the resident role is free.
func ProbeResident(dir string) (*Resident, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("probe resident: empty store directory")
	}
	path := filepath.Join(dir, residentLockName)
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("probe resident: open lock: %w", err)
	}
	defer file.Close()

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		if unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); unlockErr != nil {
			return nil, fmt.Errorf("probe resident: unlock probe: %w", unlockErr)
		}
		return nil, nil
	} else if !lockBusy(err) {
		return nil, fmt.Errorf("probe resident: lock: %w", err)
	}
	holder, err := readResident(file)
	if err != nil {
		return nil, fmt.Errorf("probe resident: read holder: %w", err)
	}
	return holder, nil
}

func lockBusy(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}

func readResident(file *os.File) (*Resident, error) {
	reader := io.NewSectionReader(file, 0, maxPayloadBytes+1)
	payload, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if len(payload) > maxPayloadBytes {
		return nil, fmt.Errorf("lock payload exceeds %d bytes", maxPayloadBytes)
	}
	var resident Resident
	if err := json.Unmarshal(payload, &resident); err != nil {
		return nil, err
	}
	if resident.PID <= 0 {
		return nil, fmt.Errorf("lock payload has invalid pid %d", resident.PID)
	}
	return &resident, nil
}

func writeResident(file *os.File, resident Resident) error {
	payload, err := json.Marshal(resident)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := file.Write(payload); err != nil {
		return err
	}
	return file.Sync()
}
