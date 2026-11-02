package taskstorage

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Only one exact verification container is journaled per receipt. Its name is
// bound to that working copy; legacy and other copies' directories are untouched.
func validCheck(s State) error {
	if s.Check == "" {
		return nil
	}
	prefix := digest(s.Path) + ".check-"
	suffix := strings.TrimPrefix(s.Check, prefix)
	nonce, err := hex.DecodeString(suffix)
	if suffix == s.Check || len(nonce) != 16 || err != nil || hex.EncodeToString(nonce) != suffix || s.Snapshot == "" {
		return errors.New("invalid verification recovery container")
	}
	return nil
}

func newCheck(receipt string, s *State, snapshot string) (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	name := digest(s.Path) + ".check-" + hex.EncodeToString(nonce[:])
	container := filepath.Join(filepath.Dir(receipt), name)
	if _, err := os.Lstat(container); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("verification recovery container already exists or is unavailable")
	}
	s.Check, s.Snapshot = name, snapshot
	// The exact reservation is durable before even an empty container is made.
	// Recovery of a reservation whose directory was never created is idempotent.
	if err := durable(receipt, *s); err != nil {
		return "", err
	}
	if err := os.Mkdir(container, 0700); err != nil {
		// We did not create this path, so never remove it on a failed reservation.
		clean := *s
		clean.Check = ""
		if writeErr := durable(receipt, clean); writeErr != nil {
			return "", errors.Join(err, writeErr)
		}
		*s = clean
		return "", err
	}
	return container, nil
}

func clearCheck(receipt string, s *State) error {
	if s.Check == "" {
		return nil
	}
	if err := validCheck(*s); err != nil {
		return err
	}
	container := filepath.Join(filepath.Dir(receipt), s.Check)
	info, err := os.Lstat(container)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return errors.New("verification recovery container is not an owned directory")
	}
	if err := removeOwnedTree(container); err != nil {
		return fmt.Errorf("remove verification recovery container: %w", err)
	}
	// On removal or receipt-write failure, the durable record remains available
	// for retry. Clearing an already absent directory is idempotent.
	clean := *s
	clean.Check = ""
	if err := durable(receipt, clean); err != nil {
		return err
	}
	*s = clean
	return nil
}
