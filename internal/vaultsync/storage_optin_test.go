package vaultsync

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/wireauth"
	"testing"
)

func TestStorageOptInVaultDoesNotStartUploadRound(t *testing.T) {
	s := Syncer{StorageOn: func() bool { return false }}
	if err := s.Push(t.Context()); !errors.Is(err, wireauth.ErrStorageOff) {
		t.Fatalf("push: %v", err)
	}
	if err := s.PushSealed(t.Context(), []byte("sealed"), ""); !errors.Is(err, wireauth.ErrStorageOff) {
		t.Fatalf("push sealed: %v", err)
	}
}
