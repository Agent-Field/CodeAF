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
// rename, both synced with their folder, and one is read through a bound.
// Claims are never swept: one small file per accepted action.

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
type appActionClaims struct{ appStartClaims }

// actionClaims is the backend's claims, beside its state file.
func (b *appBackend) actionClaims() appActionClaims {
	return appActionClaims{appStartClaims{dir: filepath.Join(filepath.Dir(b.state.where()), appActionClaimsName)}}
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
	name := appClaimName(clientID)
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return appActionClaim{}, false, nil
	}
	if err != nil {
		return appActionClaim{}, false, err
	}
	if !info.Mode().IsRegular() {
		return appActionClaim{}, false, errors.New("the record of that client_id is not a plain file")
	}
	file, err := root.OpenFile(name, os.O_RDONLY, 0)
	if err != nil {
		return appActionClaim{}, false, err
	}
	defer file.Close()
	if opened, err := file.Stat(); err != nil || !os.SameFile(info, opened) {
		return appActionClaim{}, false, errors.New("the record of that client_id changed while it was read")
	}
	raw, err := io.ReadAll(io.LimitReader(file, appActionClaimMax+1))
	if err != nil {
		return appActionClaim{}, false, err
	}
	if len(raw) > appActionClaimMax {
		return appActionClaim{}, false, errors.New("the record of that client_id is larger than any record")
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

// create claims claim.ClientID, durably. made is false when it is already
// claimed, by this process or another; the claim on disk is then the other
// attempt's and is left as it is.
func (c appActionClaims) create(claim appActionClaim) (made bool, err error) {
	root, err := c.open(true)
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
