package vaultsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// magic leads every vault object; the store refuses any object without a known
// magic, so plaintext can never be stored by mistake.
var magic = []byte("AGEV\x01")

// VaultFile is the local vault as the syncer needs it (internal/keys adds both
// methods to *keys.Vault).
type VaultFile interface {
	Export() ([]byte, error) // the vault.enc envelope bytes
	Merge(enc []byte) error  // union by secret id; on the same id the newer document `updated` wins
}

// ErrTampered reports a vault object whose bytes do not hash to its id.
var ErrTampered = errors.New("vaultsync: the vault object does not match its id and was refused")

// Syncer moves one identity's vault between this device and the store. The
// vault object is the sealed vault.enc, so the store never sees a secret.
type Syncer struct {
	Store     blobstore.Store
	Dir       directory.Client
	Vault     VaultFile
	CellKeyID string // this identity's cell key id; empty on a machine with no identity
	// Notify, when set, receives one plain sentence about what Inject skipped.
	// A sentence names secrets, never their values.
	Notify func(string)
}

// Push uploads the local vault and points the directory at it. It merges what
// the directory names first, so a device that never pulled cannot bury another
// device's secrets. A push that still loses the compare-and-swap, because a
// third device pushed in between, pulls again and pushes once more; a second
// loss is returned, since looping would only hide a busy directory.
func (s Syncer) Push(ctx context.Context) error {
	err := s.pullThenPublish(ctx)
	if errors.Is(err, directory.ErrCAS) {
		err = s.pullThenPublish(ctx)
	}
	return err
}

func (s Syncer) pullThenPublish(ctx context.Context) error {
	old, err := s.pull(ctx)
	if err != nil {
		return err
	}
	return s.publish(ctx, old)
}

// publish stores the local vault and moves the directory from old to it.
func (s Syncer) publish(ctx context.Context, old string) error {
	enc, err := s.Vault.Export()
	if err != nil {
		return err
	}
	obj := append(bytes.Clone(magic), enc...)
	rid := ridOf(obj)
	if err := s.put(ctx, rid, obj); err != nil || rid == old {
		return err
	}
	return s.Dir.SetVault(ctx, old, rid)
}

func (s Syncer) put(ctx context.Context, rid string, obj []byte) error {
	frame, err := blobstore.Encode(s.CellKeyID, []blobstore.Object{{RID: rid, Bytes: obj}})
	if err != nil {
		return err
	}
	_, err = s.Store.PutFrame(ctx, frame)
	return err
}

// Pull fetches the vault the directory names, checks it hashes to that name and
// merges it. Nothing reaches the local vault before the check passes.
func (s Syncer) Pull(ctx context.Context) error {
	_, err := s.pull(ctx)
	return err
}

// pull is Pull that also answers the id it merged, empty when the directory
// names no vault yet.
func (s Syncer) pull(ctx context.Context) (string, error) {
	l, err := s.Dir.List(ctx)
	rid := l.Identity.Vault
	if err != nil || rid == "" {
		return "", err
	}
	obj, err := s.Store.Get(ctx, rid)
	if err != nil {
		return "", err
	}
	enc, err := verified(rid, obj)
	if err != nil {
		return "", err
	}
	return rid, s.Vault.Merge(enc)
}

// verified returns the vault.enc inside obj when obj hashes to rid.
func verified(rid string, obj []byte) ([]byte, error) {
	if ridOf(obj) != rid || !bytes.HasPrefix(obj, magic) {
		return nil, fmt.Errorf("%w: %s", ErrTampered, rid)
	}
	return obj[len(magic):], nil
}

func ridOf(obj []byte) string {
	sum := sha256.Sum256(obj)
	return hex.EncodeToString(sum[:])
}
