package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// ── THE DURABLE INTENT OF A SESSION ACTION ──────────────────────────────────
//
// An action's client_id is CLAIMED ON DISK, in a file of its own, BEFORE the
// producer is called. So whatever this process loses — its answer, its memory,
// its life — the claim says what can have happened, and a client_id that was
// claimed is never taken for a new one:
//
//   - no claim: the producer was not called by an engine that shares this
//     state folder. (It says NOTHING about another folder, profile or host.)
//   - intent: the producer may have run. Nothing re-runs it. A claim at intent
//     whose attempt is not running in THIS process is uncertain, forever.
//   - done: the producer answered success, and the claim holds its ids.
//   - failed: the producer answered an ERROR, and the claim holds its words.
//     An error is not proof of no effect, so this reads uncertain too, for
//     ever, and is never run again.
//
// The claim holds the request's DIGEST and never its words: a brief or a
// remembered line is the person's, and a record kept for correlation is not a
// second place for it to live.
//
// The folder is a plain one beside the app's state file, a link in its place
// is refused, a name is the sha256 of the client_id (a client_id never becomes
// a path), a claim is created with an exclusive link and rewritten whole by
// rename, both synced with their folder, and one is read through a bound from
// a descriptor proven to be the plain file that was looked at.
// Claims are never swept: one small file per accepted action.
//
// A FILESYSTEM CALL CANNOT BE CANCELLED. A read, a link, a rename or a sync of
// a claim runs on the request's own goroutine until the disk answers, and the
// admission slot it holds is held until then: a hung disk holds the slot, and
// the gate answers busy to the next caller rather than queueing it. Nothing
// starts a goroutine or a timer to abandon such a call, because an abandoned
// write is one whose outcome nobody would know.

// appActionClaimsName is the claims' folder, beside the app's state file.
const appActionClaimsName = "session-actions"

// appActionClaimMax bounds one claim on disk: a few ids, a title, the
// producer's error words. Anything larger is not one of ours.
const appActionClaimMax = 16 << 10

const appActionClaimV = 1

// The states of a claim.
const (
	appActionIntent = "intent"
	appActionDone   = "done"
	appActionFailed = "failed"
)

// appActionClaim is one client_id's record.
type appActionClaim struct {
	V        int       `json:"v"`
	ClientID string    `json:"client_id"`
	Action   string    `json:"action"`
	Agent    string    `json:"agent"`
	Digest   string    `json:"digest"`
	State    string    `json:"state"`
	At       time.Time `json:"at"`
	DoneAt   time.Time `json:"done_at,omitzero"`

	Task          *appwire.ActionTask          `json:"task,omitempty"`
	Memory        *appwire.ActionMemory        `json:"memory,omitempty"`
	ProducerError *appwire.ActionProducerError `json:"producer_error,omitempty"`
}

// appActionClaims is the claims' folder.
type appActionClaims struct {
	appStartClaims
	// syncFolder makes the entries a folder holds durable. Nil is
	// [appSyncFolder]; a test replaces it to see the order.
	syncFolder func(folder string) error
}

// actionClaims is the backend's claims, beside its state file.
func (b *appBackend) actionClaims() appActionClaims {
	return appActionClaims{appStartClaims: appStartClaims{dir: filepath.Join(filepath.Dir(b.state.where()), appActionClaimsName)}}
}

// appSyncFolder syncs a folder through a root, the way a claims folder is.
func appSyncFolder(folder string) error {
	root, err := os.OpenRoot(folder)
	if err != nil {
		return err
	}
	defer root.Close()
	return appSyncRoot(root)
}

// openToClaim is the claims folder opened for a claim, with the chain that
// holds it made durable FIRST, on every claim: the claims folder's entry in the
// app's state folder, and the state folder's entry in its own parent. A claim
// synced inside a folder the disk does not yet keep can vanish whole with the
// folder after a power loss, and the producer would then run again for a
// client_id it already ran for. It is every claim, not only the one that made
// the folder, because a folder another process has just made can be seen
// before that process has synced it. Above the state folder's parent nothing
// is synced here: that is the codeaf home, which exists before any
// conversation does, and its own entry is not this record's to vouch for.
// A failure is returned before any claim exists, so the producer is never
// called on it. The state folder keeps its authority: it is created as it
// always was, never widened or chosen by the caller.
func (c appActionClaims) openToClaim() (*os.Root, error) {
	state := filepath.Dir(c.dir)
	if err := os.MkdirAll(state, appStateFolderMode); err != nil {
		return nil, err
	}
	if err := os.Mkdir(c.dir, appStateFolderMode); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	sync := c.syncFolder
	if sync == nil {
		sync = appSyncFolder
	}
	for _, folder := range []string{state, filepath.Dir(state)} {
		if err := sync(folder); err != nil {
			return nil, fmt.Errorf("the claims folder could not be made durable: %w", err)
		}
	}
	return c.open(false)
}

// read is clientID's claim. found is false only when the folder or the file is
// provably not there; ANY OTHER FAILURE IS AN ERROR, never "no claim", because
// a claim that could not be read may say its action ran.
func (c appActionClaims) read(clientID string) (claim appActionClaim, found bool, err error) {
	root, err := c.open(false)
	if errors.Is(err, errNoClaims) {
		return appActionClaim{}, false, nil
	}
	if err != nil {
		return appActionClaim{}, false, err
	}
	defer root.Close()
	raw, found, err := appReadClaimFile(root, appClaimName(clientID), appActionClaimMax)
	if err != nil || !found {
		return appActionClaim{}, false, err
	}
	if err := json.Unmarshal(raw, &claim); err != nil {
		return appActionClaim{}, false, fmt.Errorf("the record of that client_id cannot be read: %w", err)
	}
	if claim.V != appActionClaimV || claim.ClientID != clientID || claim.Agent == "" || claim.Digest == "" ||
		(claim.State != appActionIntent && claim.State != appActionDone && claim.State != appActionFailed) {
		return appActionClaim{}, false, errors.New("the record of that client_id is not one this engine wrote")
	}
	return claim, true, nil
}

// appReadClaimFile reads name in root through limit. found is false only when
// the name is provably not there.
//
// IT NEVER BLOCKS IN OPEN. The name is looked at first, and anything but a
// plain file — a link, a pipe, a device — is refused unread, as before. A pipe
// put in its place AFTER that look would hold an ordinary open until a writer
// came, and with it this request's admission slot; so the open is
// non-blocking, and the DESCRIPTOR it answers must itself be a plain file and
// the very file that was looked at. A link swapped in is resolved only inside
// the claims' folder (os.Root), and is then refused by that same check.
func appReadClaimFile(root *os.Root, name string, limit int64) (raw []byte, found bool, err error) {
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, errors.New("the record of that client_id is not a plain file")
	}
	raw, err = appReadClaimAt(root, name, info, limit)
	if err != nil {
		return nil, false, err
	}
	return raw, true, nil
}

// appReadClaimAt opens name without waiting and reads it through limit, only
// if the descriptor is a plain file and the very file seen was.
func appReadClaimAt(root *os.Root, name string, seen fs.FileInfo, limit int64) ([]byte, error) {
	file, err := root.OpenFile(name, appClaimReadFlag, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if opened, err := file.Stat(); err != nil || !opened.Mode().IsRegular() || !os.SameFile(seen, opened) {
		return nil, errors.New("the record of that client_id changed while it was read")
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errors.New("the record of that client_id is larger than any record")
	}
	return raw, nil
}

// create claims claim.ClientID, durably. made is false when it is already
// claimed, by this process or another; the claim on disk is then the other
// attempt's and is left as it is.
func (c appActionClaims) create(claim appActionClaim) (made bool, err error) {
	root, err := c.openToClaim()
	if err != nil {
		return false, err
	}
	defer root.Close()
	temp, err := appActionTemp(root, claim)
	if err != nil {
		return false, err
	}
	defer root.Remove(temp)
	name := appClaimName(claim.ClientID)
	// THE LINK IS THE CLAIM: it publishes a whole, synced file under the
	// client_id's name, or fails because that name is taken.
	if err := root.Link(temp, name); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, err
	}
	_ = root.Remove(temp)
	if err := appSyncRoot(root); err != nil {
		// A claim the disk may not keep is not one to run on.
		_ = root.Remove(name)
		return false, err
	}
	return true, nil
}

// settle rewrites claim whole, in the state it carries. An error means it may
// or may not be on disk.
func (c appActionClaims) settle(claim appActionClaim) error {
	root, err := c.open(false)
	if err != nil {
		return err
	}
	defer root.Close()
	temp, err := appActionTemp(root, claim)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	if err := root.Rename(temp, appClaimName(claim.ClientID)); err != nil {
		return err
	}
	return appSyncRoot(root)
}

// appActionTemp writes claim to a new, synced file of its own in root and
// answers its name.
func appActionTemp(root *os.Root, claim appActionClaim) (string, error) {
	raw, err := json.Marshal(claim)
	if err != nil {
		return "", err
	}
	if len(raw) > appActionClaimMax {
		return "", errors.New("the record would be larger than any record")
	}
	name := ".new-" + rand.Text()
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, appStateFileMode)
	if err != nil {
		return "", err
	}
	_, err = file.Write(raw)
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = root.Remove(name)
		return "", err
	}
	return name, nil
}
