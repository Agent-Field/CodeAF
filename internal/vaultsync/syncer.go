package vaultsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/keys"
)

// magic leads every vault object; the store refuses any object without a known
// magic, so plaintext can never be stored by mistake.
var magic = []byte("AGEV\x01")

// VaultFile is the local vault as the syncer needs it (internal/keys adds both
// methods to *keys.Vault).
type VaultFile interface {
	Export() ([]byte, error) // the vault.enc envelope bytes
	Merge(enc []byte) error  // per secret id the newer slot wins, a delete included
	// Entries lists a scope's live slots in stable name order.
	Entries(project string) ([]keys.Entry, error)
	// Get, Put, Delete and Deleted address one slot by id; the credentials
	// file rides in one such slot (credentials.go).
	Get(id string) (keys.Entry, error)
	// IDs lists the slot ids under a prefix, tombstones included.
	IDs(prefix string) ([]string, error)
	PutAt(id string, e keys.Entry, at time.Time) error
	Delete(id string) error
	Deleted(id string) (bool, error)
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
	// Carry is the state that rides in the vault beside the secrets, each kept
	// here in its own Medium (files.go, credentials.go, and the provider keys of
	// the profile config).
	Carry []Carrier
}

// Push uploads the local vault and points the directory at it. It merges what
// the directory names first, so a device that never pulled cannot bury another
// device's secrets. A push that still loses the compare-and-swap, because a
// third device pushed in between, pulls again and pushes once more; a second
// loss is returned, since looping would only hide a busy directory.
func (s Syncer) Push(ctx context.Context) error {
	if err := s.Capture(); err != nil {
		return err
	}
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
	return s.PushSealed(ctx, enc, old)
}

// PushSealed stores enc, a vault envelope already sealed under this identity's
// key, and moves the directory from old to it. A rotation uses it to seed the
// new identity's relay with the vault resealed for it, which no local file
// holds yet.
func (s Syncer) PushSealed(ctx context.Context, enc []byte, old string) error {
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
// merges it. Nothing reaches the local vault before the check passes. What this
// machine edited is kept first, but a file it does not have is not a removal:
// only Capture and Push let a removal travel.
func (s Syncer) Pull(ctx context.Context) error {
	if err := s.each(Carrier.captureEdits); err != nil {
		return err
	}
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
	if err := s.Vault.Merge(enc); err != nil {
		return "", err
	}
	return rid, s.restoreCarried()
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

// Capture stores every changed carried medium in the vault without any network,
// so a caller that fingerprints the vault sees the change. A medium that holds
// nothing is a removal, and travels as one.
func (s Syncer) Capture() error { return s.each(Carrier.capture) }

// each applies step to every carried medium.
func (s Syncer) each(step func(Carrier, VaultFile) error) error {
	for _, c := range s.Carry {
		if err := step(c, s.Vault); err != nil {
			return err
		}
	}
	return nil
}

// restoreCarried writes what a merge brought in back to each medium.
func (s Syncer) restoreCarried() error { return s.each(Carrier.restore) }
