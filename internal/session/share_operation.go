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
	"io"
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
	shareOpVersion    = 1
	shareOpIDMin      = 1
	shareOpIDMax      = 128
	shareOpPathsMax   = 8
	shareOpPathMax    = 4096
	shareOpSlotCap    = 4
	shareOpReceiptMax = 256 << 10
	shareOpTimeout    = 10 * time.Second
	shareOpLockName   = "lock"
	shareOpReceipt    = "receipt"
	shareOpDirName    = "share-ops"
)

// Share operation settlement words. Pending is durable before any inspection;
// planned is the FULL immutable plan, durable before any index append.
const (
	ShareOpPending  = "pending"
	ShareOpPlanned  = "planned"
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
//
// Row is the EXACT index row that id is appended as, written once by Plan
// from the inspected bytes and never rewritten. A pending receipt has none,
// which is how a replay knows no append can have happened yet.
type SharePlannedRow struct {
	ID   string    `json:"id"`
	Path string    `json:"path"`
	Row  *Artifact `json:"row,omitempty"`
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
//
// RECEIPT IS A VIEW, NEVER AN AUTHORITY. It is a fresh decode of the bytes on
// disk, handed out so a caller can read it; nothing the claim does next reads
// it back. Plan, Settle and [ShareClaim.PlannedRows] all start from durable —
// the exact bytes last read from or written to the receipt file, private to
// the claim — so a caller that edits Receipt, or any row it passed to Plan,
// cannot change what is settled or appended.
type ShareClaim struct {
	Outcome string
	Receipt ShareReceipt

	durable []byte

	dir         string
	lock        *os.File
	releaseSlot func()
	once        sync.Once
	deadline    time.Time
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
//
// It reads as its three phases: the arguments are checked before anything is
// held, the hold is taken without waiting, and only then is the durable receipt
// read and answered.
func AdmitShareOperation(root, session, operationID string, paths []string, prov ShareProvenance, deadline time.Time) (*ShareClaim, error) {
	normalised, digest, err := validShareAdmission(root, session, operationID, paths)
	if err != nil {
		return &ShareClaim{Outcome: ShareClaimInvalid}, err
	}
	claim, err := holdShareOperation(shareOpDir(root, session, operationID), root, boundedShareDeadline(deadline))
	if err != nil {
		return &ShareClaim{Outcome: ShareClaimBusy}, err
	}
	return claim.answer(session, operationID, normalised, digest, prov)
}

// validShareAdmission is every refusal that needs nothing held: the store,
// both keys and the payload.
func validShareAdmission(root, session, operationID string, paths []string) ([]string, string, error) {
	if strings.TrimSpace(root) == "" || ValidateShareOperationID(session) != nil {
		return nil, "", ErrShareOpInvalid
	}
	if err := ValidateShareOperationID(operationID); err != nil {
		return nil, "", err
	}
	normalised, digest, err := NormalizeSharePayload(paths)
	if err != nil {
		return nil, "", err
	}
	// A RECEIPT NAMES FILES, never spellings relative to a folder it does not
	// record: the caller resolves a relative path from its own captured base
	// before admission, so a plan row can be bound to exactly the path it is for.
	for _, path := range normalised {
		if !shareAbsolute(path) {
			return nil, "", fmt.Errorf("%w: a path must be absolute by the time it is admitted", ErrShareOpInvalid)
		}
	}
	return normalised, digest, nil
}

// holdShareOperation takes one process slot and the operation's stable lock,
// never waiting for either. Every failure gives back whatever it had taken, so
// a refused admission holds nothing.
func holdShareOperation(dir, root string, deadline time.Time) (*ShareClaim, error) {
	if time.Now().After(deadline) {
		return nil, ErrShareOpBusy
	}
	select {
	case shareOpSlots <- struct{}{}:
	default:
		return nil, ErrShareOpBusy
	}
	claim := &ShareClaim{dir: dir, deadline: deadline, releaseSlot: func() { <-shareOpSlots }}
	if err := makeShareOpDir(dir, root); err != nil {
		claim.Release()
		return nil, err
	}
	lock, err := openShareLock(filepath.Join(dir, shareOpLockName))
	if err != nil {
		claim.Release()
		return nil, err
	}
	if err := filelock.Lock(lock, true, true); err != nil {
		_ = lock.Close()
		claim.Release()
		if filelock.IsBusy(err) {
			return nil, ErrShareOpBusy
		}
		return nil, err
	}
	claim.lock = lock
	if time.Now().After(deadline) {
		claim.Release()
		return nil, ErrShareOpBusy
	}
	return claim, nil
}

// makeShareOpDir creates the operation's folder and syncs every parent entry
// it may have added, so a first claim survives a crash as a whole.
func makeShareOpDir(dir, root string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := syncShareOpDir(filepath.Dir(dir)); err != nil {
		return err
	}
	return syncShareOpDir(root)
}

// answer reads the durable receipt under the held lock and decides: replay
// the ORIGINAL, refuse a different payload, or write the pending manifest.
func (c *ShareClaim) answer(session, operationID string, normalised []string, digest string, prov ShareProvenance) (*ShareClaim, error) {
	existing, raw, err := readShareReceiptRaw(c.dir)
	if err == nil && (existing.Session != session || existing.OperationID != operationID) {
		err = ErrShareOpInvalid
	}
	switch {
	case err == nil && existing.PayloadDigest != digest:
		c.Receipt, c.Outcome = existing, ShareClaimConflict
		c.Release()
		return c, ErrShareOpConflict
	case err == nil:
		// Replay the ORIGINAL receipt and provenance; do not rewrite Created
		// and do not attribute the operation to the latest caller.
		c.Outcome = ShareClaimReplay
		return c, c.adopt(raw)
	case !errors.Is(err, ErrShareOpMissing):
		c.Release()
		return &ShareClaim{Outcome: ShareClaimBusy}, err
	}
	raw, err = writeShareReceipt(c.dir, pendingShareReceipt(session, operationID, normalised, digest, prov))
	if err == nil {
		err = c.adopt(raw)
	}
	if err != nil {
		c.Release()
		return &ShareClaim{Outcome: ShareClaimBusy}, err
	}
	c.Outcome = ShareClaimAdmitted
	return c, nil
}

// adopt makes raw — bytes just read from or written to the receipt file — the
// claim's only authority, and Receipt a fresh decode of them that shares no
// pointer or slice with anything the claim keeps.
func (c *ShareClaim) adopt(raw []byte) error {
	view, err := decodeShareReceipt(raw)
	if err != nil {
		return err
	}
	c.durable = append([]byte(nil), raw...)
	c.Receipt = view
	return nil
}

// frozen is a fresh decode of the durable bytes, owned by its caller alone.
func (c *ShareClaim) frozen() (ShareReceipt, error) {
	if c == nil || len(c.durable) == 0 {
		return ShareReceipt{}, ErrShareOpInvalid
	}
	return decodeShareReceipt(c.durable)
}

// pendingShareReceipt mints the immutable planned ids before any effect.
func pendingShareReceipt(session, operationID string, normalised []string, digest string, prov ShareProvenance) ShareReceipt {
	planned := make([]SharePlannedRow, len(normalised))
	for i, path := range normalised {
		planned[i] = SharePlannedRow{ID: NewDeliverableID(), Path: path}
	}
	prov.Session = session
	return ShareReceipt{
		Version:       shareOpVersion,
		OperationID:   operationID,
		Session:       session,
		Status:        ShareOpPending,
		PayloadDigest: digest,
		Planned:       planned,
		Provenance:    prov,
		WrittenAt:     time.Now().UTC(),
	}
}

// Settle writes the final status and recorded ids under the held lock, then
// releases the claim. Accepted and refused are terminal: settling a receipt
// that is already either leaves it intact.
//
// THE TRANSITIONS ARE THE LAW, not a convention of the caller. Refused is only
// reachable from pending, where no plan exists and so no append can have
// happened. Accepted and unknown need the plan, because only a planned id can
// have been appended. A refusal written over a plan would let a retry report
// "never shared" for a row that may already be on the index.
func (c *ShareClaim) Settle(status string, recordedIDs []string) error {
	if c == nil || c.lock == nil {
		return ErrShareOpInvalid
	}
	defer c.Release()
	receipt, err := c.frozen()
	if err != nil {
		return err
	}
	from := receipt.Status
	if from == ShareOpAccepted || from == ShareOpRefused {
		return nil
	}
	if !shareSettles(from, status) {
		return fmt.Errorf("%w: a %s receipt cannot settle %q", ErrShareOpInvalid, from, status)
	}
	if time.Now().After(c.deadline) {
		return ErrShareOpBusy
	}
	receipt.Status = status
	receipt.RecordedIDs = append([]string(nil), recordedIDs...)
	receipt.WrittenAt = time.Now().UTC()
	raw, err := writeShareReceipt(c.dir, receipt)
	if err != nil {
		return err
	}
	return c.adopt(raw)
}

func shareSettles(from, to string) bool {
	switch to {
	case ShareOpRefused:
		return from == ShareOpPending
	case ShareOpAccepted, ShareOpUnknown:
		return from == ShareOpPlanned || from == ShareOpUnknown
	}
	return false
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
	if strings.TrimSpace(root) == "" || ValidateShareOperationID(session) != nil {
		return ShareReceipt{}, ErrShareOpInvalid
	}
	receipt, err := readShareReceipt(shareOpDir(root, session, operationID))
	if err == nil && (receipt.Session != session || receipt.OperationID != operationID) {
		return ShareReceipt{}, ErrShareOpInvalid
	}
	return receipt, err
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
	receipt, _, err := readShareReceiptRaw(dir)
	return receipt, err
}

// readShareReceiptRaw answers the receipt and the exact bytes it was decoded
// from, bounded and validated before either is trusted.
func readShareReceiptRaw(dir string) (ShareReceipt, []byte, error) {
	file, err := openRegularNoFollow(filepath.Join(dir, shareOpReceipt))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ShareReceipt{}, nil, ErrShareOpMissing
		}
		return ShareReceipt{}, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ShareReceipt{}, nil, err
	}
	if info.Size() > shareOpReceiptMax {
		return ShareReceipt{}, nil, ErrShareOpInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(file, shareOpReceiptMax+1))
	if err != nil {
		return ShareReceipt{}, nil, err
	}
	receipt, err := decodeShareReceipt(raw)
	if err != nil {
		return ShareReceipt{}, nil, err
	}
	return receipt, raw, nil
}

// decodeShareReceipt is the one reading of receipt bytes: bounded, decoded
// into fresh memory, validated.
func decodeShareReceipt(raw []byte) (ShareReceipt, error) {
	if len(raw) > shareOpReceiptMax {
		return ShareReceipt{}, ErrShareOpInvalid
	}
	var receipt ShareReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return ShareReceipt{}, err
	}
	if err := validateShareReceipt(receipt); err != nil {
		return ShareReceipt{}, err
	}
	return receipt, nil
}

// writeShareReceipt atomically replaces the receipt under dir and answers the
// exact bytes written. The lock file in the same directory is never this
// write's target.
//
// WHAT IS VALIDATED IS WHAT IS WRITTEN: the receipt is marshalled first and
// those bytes are decoded and validated, so no value that only exists in the
// caller's memory can pass the check and differ on disk.
func writeShareReceipt(dir string, receipt ShareReceipt) ([]byte, error) {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	if _, err := decodeShareReceipt(raw); err != nil {
		return nil, err
	}
	if err := replaceShareReceipt(dir, raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func replaceShareReceipt(dir string, raw []byte) error {
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

// Create-exclusive refuses a link at a new lock path. Existing locks are
// opened regular/no-follow/nonblocking; flock does not require writing them.
func openShareLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrExist) {
		return openRegularNoFollow(path)
	}
	return file, err
}
