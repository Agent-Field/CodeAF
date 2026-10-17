package session

// share_operation.go is the durable claim for one share_deliverable operation.
//
// An operation_id is an opaque INTENT key the caller chooses and reuses only to
// retry the same publication. It is NOT a provider call id: those can repeat
// inside one run and must never be the sole idempotency key.
//
// THE RECEIPT IS WRITTEN BEFORE ANY PUBLICATION SIDE EFFECT. It stores the
// normalised payload digest, the original provenance, and an immutable list of
// planned deliverable ids. Same key + same digest replays that original
// receipt; same key + a different digest conflicts before effect; a live lock
// held by another owner is busy. There is no in-process historical map.
//
// THE LOCK FILE AND THE RECEIPT ARE SEPARATE. Cross-process admission takes a
// non-blocking exclusive flock on a stable lock file that is never renamed.
// The receipt itself is written through a sibling temp file, synced, and
// renamed into place. Flocking the inode that rename replaces would lose the
// lock mid-write.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

const (
	shareOpVersion  = 1
	shareOpIDMin    = 1
	shareOpIDMax    = 128
	shareOpPathsMax = 8
	shareOpPathMax  = 4096
	shareOpSlotCap  = 4
	shareOpLockName = "lock"
	shareOpReceipt  = "receipt"
	shareOpDirName  = "share-ops"
)

// Share operation settlement words. Pending is durable before any index append.
const (
	ShareOpPending  = "pending"
	ShareOpAccepted = "accepted"
	ShareOpRefused  = "refused"
	ShareOpUnknown  = "unknown"
)

// ShareClaimOutcome is what admission answered without looking at the index.
const (
	ShareClaimAdmitted = "admitted"
	ShareClaimReplay   = "replay"
	ShareClaimConflict = "conflict"
	ShareClaimBusy     = "busy"
	ShareClaimInvalid  = "invalid"
)

// Errors a caller can distinguish without reading Outcome.
var (
	ErrShareOpInvalid  = errors.New("share operation arguments are invalid")
	ErrShareOpBusy     = errors.New("share operation is busy")
	ErrShareOpConflict = errors.New("share operation payload conflicts with an existing receipt")
	ErrShareOpMissing  = errors.New("share operation receipt is absent")
)

// shareOpSlots bounds how many share operations this process may hold at once.
// A full set refuses immediately: there is no waiting queue.
var shareOpSlots = make(chan struct{}, shareOpSlotCap)

// ShareProvenance is who handed the operation over when the receipt was first
// written. A replay returns this original record, never the latest caller.
type ShareProvenance struct {
	Session string `json:"session"`
	Run     string `json:"run,omitempty"`
	Turn    string `json:"turn,omitempty"`
	Call    string `json:"call,omitempty"`
}

// SharePlannedRow is one immutable planned deliverable id for a normalised
// path. Ids are minted when the pending receipt is written, before any append.
type SharePlannedRow struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// ShareReceipt is the durable operation record scoped to one session and
// operation_id. WrittenAt is when this receipt was last settled on disk; it is
// not authorship of the file being shared.
type ShareReceipt struct {
	Version       int               `json:"version"`
	OperationID   string            `json:"operation_id"`
	Session       string            `json:"session"`
	Status        string            `json:"status"`
	PayloadDigest string            `json:"payload_digest"`
	Planned       []SharePlannedRow `json:"planned"`
	Provenance    ShareProvenance   `json:"provenance"`
	RecordedIDs   []string          `json:"recorded_ids,omitempty"`
	WrittenAt     time.Time         `json:"written_at"`
}

// ShareClaim is one admitted (or replayed) hold on an operation. The process
// slot and the cross-process lock stay held until Release or Settle finishes.
type ShareClaim struct {
	Outcome string
	Receipt ShareReceipt

	dir         string
	lock        *os.File
	releaseSlot func()
	once        sync.Once
}

// ValidateShareOperationID checks the opaque intent key: 1..128 bytes, exact
// trim, UTF-8, no control characters.
func ValidateShareOperationID(id string) error {
	if id == "" || strings.TrimSpace(id) != id {
		return fmt.Errorf("%w: operation_id must be non-empty without surrounding space", ErrShareOpInvalid)
	}
	if len(id) < shareOpIDMin || len(id) > shareOpIDMax {
		return fmt.Errorf("%w: operation_id must be %d..%d bytes", ErrShareOpInvalid, shareOpIDMin, shareOpIDMax)
	}
	if !utf8.ValidString(id) || strings.ContainsFunc(id, shareOpForbiddenRune) {
		return fmt.Errorf("%w: operation_id holds a control character", ErrShareOpInvalid)
	}
	return nil
}

// NormalizeSharePayload validates 1..8 paths for spelling bounds and answers
// the cleaned paths plus a stable digest of that normalised list.
func NormalizeSharePayload(paths []string) (normalised []string, digest string, err error) {
	if len(paths) == 0 {
		return nil, "", fmt.Errorf("%w: paths must name at least one file", ErrShareOpInvalid)
	}
	if len(paths) > shareOpPathsMax {
		return nil, "", fmt.Errorf("%w: paths takes at most %d entries", ErrShareOpInvalid, shareOpPathsMax)
	}
	seen := make(map[string]bool, len(paths))
	normalised = make([]string, 0, len(paths))
	for _, raw := range paths {
		switch {
		case strings.TrimSpace(raw) == "":
			return nil, "", fmt.Errorf("%w: paths holds an empty path", ErrShareOpInvalid)
		case len(raw) > shareOpPathMax:
			return nil, "", fmt.Errorf("%w: a path is over %d bytes", ErrShareOpInvalid, shareOpPathMax)
		case !utf8.ValidString(raw) || strings.ContainsFunc(raw, shareOpForbiddenRune):
			return nil, "", fmt.Errorf("%w: a path holds a control character", ErrShareOpInvalid)
		}
		path := filepath.Clean(raw)
		if base := filepath.Base(path); base == "." || base == string(filepath.Separator) || base == ".." {
			return nil, "", fmt.Errorf("%w: a path must name a file", ErrShareOpInvalid)
		}
		if seen[path] {
			return nil, "", fmt.Errorf("%w: paths names the same path twice", ErrShareOpInvalid)
		}
		seen[path] = true
		normalised = append(normalised, path)
	}
	sum := sha256.Sum256([]byte("share-payload/v1\x00" + strings.Join(normalised, "\x00")))
	return normalised, hex.EncodeToString(sum[:]), nil
}

// AdmitShareOperation takes the process slot and the cross-process lock for
// (session, operation_id) under root, writes a pending receipt when none exists,
// and answers a claim whose Outcome is admitted, replay, conflict, busy, or
// invalid. On admitted/replay the lock stays held until Release or Settle.
func AdmitShareOperation(root, session, operationID string, paths []string, prov ShareProvenance, deadline time.Time) (*ShareClaim, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(session) == "" {
		return &ShareClaim{Outcome: ShareClaimInvalid}, ErrShareOpInvalid
	}
	if err := ValidateShareOperationID(operationID); err != nil {
		return &ShareClaim{Outcome: ShareClaimInvalid}, err
	}
	normalised, digest, err := NormalizeSharePayload(paths)
	if err != nil {
		return &ShareClaim{Outcome: ShareClaimInvalid}, err
	}
	if !deadline.IsZero() && time.Now().After(deadline) {
		return &ShareClaim{Outcome: ShareClaimBusy}, ErrShareOpBusy
	}
	select {
	case shareOpSlots <- struct{}{}:
	default:
		return &ShareClaim{Outcome: ShareClaimBusy}, ErrShareOpBusy
	}
	slotHeld := true
	releaseSlot := func() {
		if slotHeld {
			<-shareOpSlots
			slotHeld = false
		}
	}

	dir := shareOpDir(root, session, operationID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		releaseSlot()
		return &ShareClaim{Outcome: ShareClaimBusy}, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, shareOpLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		releaseSlot()
		return &ShareClaim{Outcome: ShareClaimBusy}, err
	}
	if err := filelock.Lock(lock, true, true); err != nil {
		_ = lock.Close()
		releaseSlot()
		if filelock.IsBusy(err) {
			return &ShareClaim{Outcome: ShareClaimBusy}, ErrShareOpBusy
		}
		return &ShareClaim{Outcome: ShareClaimBusy}, err
	}
	if !deadline.IsZero() && time.Now().After(deadline) {
		_ = filelock.Unlock(lock)
		_ = lock.Close()
		releaseSlot()
		return &ShareClaim{Outcome: ShareClaimBusy}, ErrShareOpBusy
	}

	existing, readErr := readShareReceipt(dir)
	switch {
	case readErr == nil:
		claim := &ShareClaim{dir: dir, lock: lock, releaseSlot: releaseSlot, Receipt: existing}
		if existing.PayloadDigest != digest {
			claim.Outcome = ShareClaimConflict
			claim.Release()
			return claim, ErrShareOpConflict
		}
		// Replay the ORIGINAL receipt and provenance; do not rewrite Created
		// and do not attribute the operation to the latest caller.
		claim.Outcome = ShareClaimReplay
		return claim, nil
	case errors.Is(readErr, ErrShareOpMissing):
		// Write the pending manifest below.
	default:
		_ = filelock.Unlock(lock)
		_ = lock.Close()
		releaseSlot()
		return &ShareClaim{Outcome: ShareClaimBusy}, readErr
	}

	planned := make([]SharePlannedRow, len(normalised))
	for i, path := range normalised {
		planned[i] = SharePlannedRow{ID: NewDeliverableID(), Path: path}
	}
	prov.Session = session
	receipt := ShareReceipt{
		Version:       shareOpVersion,
		OperationID:   operationID,
		Session:       session,
		Status:        ShareOpPending,
		PayloadDigest: digest,
		Planned:       planned,
		Provenance:    prov,
		WrittenAt:     time.Now().UTC(),
	}
	if err := writeShareReceipt(dir, receipt); err != nil {
		_ = filelock.Unlock(lock)
		_ = lock.Close()
		releaseSlot()
		return &ShareClaim{Outcome: ShareClaimBusy}, err
	}
	return &ShareClaim{
		Outcome:     ShareClaimAdmitted,
		Receipt:     receipt,
		dir:         dir,
		lock:        lock,
		releaseSlot: releaseSlot,
	}, nil
}

// Settle writes the final status and recorded ids under the held lock, then
// releases the claim. A replay of an already-settled receipt leaves it intact.
func (c *ShareClaim) Settle(status string, recordedIDs []string) error {
	if c == nil || c.lock == nil {
		return ErrShareOpInvalid
	}
	switch status {
	case ShareOpAccepted, ShareOpRefused, ShareOpUnknown, ShareOpPending:
	default:
		return fmt.Errorf("%w: unknown share status %q", ErrShareOpInvalid, status)
	}
	receipt := c.Receipt
	if c.Outcome == ShareClaimReplay && receipt.Status != ShareOpPending {
		c.Release()
		return nil
	}
	receipt.Status = status
	if recordedIDs != nil {
		receipt.RecordedIDs = append([]string(nil), recordedIDs...)
	}
	receipt.WrittenAt = time.Now().UTC()
	if err := writeShareReceipt(c.dir, receipt); err != nil {
		c.Release()
		return err
	}
	c.Receipt = receipt
	c.Release()
	return nil
}

// Release drops the cross-process lock and the process slot. Safe to call more
// than once. A pending receipt is preserved as written.
func (c *ShareClaim) Release() {
	if c == nil {
		return
	}
	c.once.Do(func() {
		if c.lock != nil {
			_ = filelock.Unlock(c.lock)
			_ = c.lock.Close()
			c.lock = nil
		}
		if c.releaseSlot != nil {
			c.releaseSlot()
			c.releaseSlot = nil
		}
	})
}

// LoadShareReceipt reads the durable receipt for (session, operation_id)
// without taking the lock. Absence is ErrShareOpMissing.
func LoadShareReceipt(root, session, operationID string) (ShareReceipt, error) {
	if err := ValidateShareOperationID(operationID); err != nil {
		return ShareReceipt{}, err
	}
	if strings.TrimSpace(root) == "" || strings.TrimSpace(session) == "" {
		return ShareReceipt{}, ErrShareOpInvalid
	}
	return readShareReceipt(shareOpDir(root, session, operationID))
}

// ShareOpPaths returns the engine-owned lock and receipt paths. They are
// hashed names under root, never renderer paths.
func ShareOpPaths(root, session, operationID string) (lockPath, receiptPath string) {
	dir := shareOpDir(root, session, operationID)
	return filepath.Join(dir, shareOpLockName), filepath.Join(dir, shareOpReceipt)
}

func shareOpForbiddenRune(r rune) bool {
	return unicode.IsControl(r) || r == 0x202e
}

func shareOpDir(root, session, operationID string) string {
	sum := sha256.Sum256([]byte("share-op/v1\x00" + session + "\x00" + operationID))
	return filepath.Join(root, shareOpDirName, hex.EncodeToString(sum[:16]))
}

func readShareReceipt(dir string) (ShareReceipt, error) {
	raw, err := os.ReadFile(filepath.Join(dir, shareOpReceipt))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ShareReceipt{}, ErrShareOpMissing
		}
		return ShareReceipt{}, err
	}
	var receipt ShareReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return ShareReceipt{}, err
	}
	if receipt.OperationID == "" || receipt.PayloadDigest == "" || len(receipt.Planned) == 0 {
		return ShareReceipt{}, fmt.Errorf("%w: receipt is incomplete", ErrShareOpInvalid)
	}
	return receipt, nil
}

// writeShareReceipt atomically replaces the receipt under dir. The lock file in
// the same directory is never this write's target.
func writeShareReceipt(dir string, receipt ShareReceipt) error {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".receipt-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	_, writeErr := tmp.Write(raw)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(dir, shareOpReceipt)); err != nil {
		return err
	}
	return syncShareOpDir(dir)
}

func syncShareOpDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return err
	}
	return nil
}
