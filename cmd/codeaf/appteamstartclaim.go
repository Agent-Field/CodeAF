package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ── THE DURABLE INTENT OF A TEAM START ──────────────────────────────────────
//
// A start's request_id is CLAIMED ON DISK, in a file of its own, BEFORE the
// team it makes is committed, and its goal is handed to the manager only after
// that claim has been rewritten to say it may be. So whatever this process
// loses — its answer, its memory, its life — the claim says what can have
// happened, and a request that was claimed is never taken for a new one:
//
//   - no claim: nothing was made for the request and nothing was sent;
//   - intent: a team may have been committed, and nothing was handed to its
//     manager, because the hand-over waits for the next state;
//   - committed: the goal may have been handed over. Whether it was accepted
//     is the manager's conversation's to say, by the client id on its line;
//   - withheld: the start made its team and decided, before any hand-over,
//     not to hand the goal over; it never will.
//
// An intent claim whose team exists is NOT proof that nothing was sent: the
// start that holds it may still be on its way to committing, here or in
// another process. Only withheld is that proof.
//
// The claim is created with an exclusive link (two starts of one request, in
// this process or another, cannot both hold it), rewritten whole by rename, and
// synced with its folder. The folder is a plain one beside the app's state, a
// link in its place is refused, and every name in it is a sha256 of the
// request, so a request id never becomes a path. A claim is at most
// appStartClaimMax bytes and is read through that bound.
//
// A claim is kept after its crew is deleted: the request that made it may have
// sent its goal, and a repeat of it is refused rather than taken for new. It is
// dropped only when its start was refused before anything was committed.

// appStartClaimsName is the claims' folder, beside the app's state file.
const appStartClaimsName = "crew-starts"

// appStartClaimMax bounds one claim on disk. A claim is a few short ids and
// one conversation path; anything larger is not one of ours.
const appStartClaimMax = 16 << 10

// appStartClaimV is the claim's shape.
const appStartClaimV = 1

// The states of a claim.
const (
	// appClaimIntent: the team may have been committed; the goal was not
	// handed to its manager.
	appClaimIntent = "intent"
	// appClaimCommitted: the goal may have been handed to the manager.
	appClaimCommitted = "committed"
	// appClaimWithheld: the goal was never handed to the manager, and never
	// will be by this start.
	appClaimWithheld = "withheld"
)

// appStartClaim is one request's claim: what it is, the crew it commits under
// the id chosen before the commit, and the manager conversation it made.
type appStartClaim struct {
	V           int    `json:"v"`
	RequestID   string `json:"request_id"`
	Fingerprint string `json:"fingerprint"`
	State       string `json:"state"`
	Crew        string `json:"crew"`
	Manager     string `json:"manager"`
	ManagerKey  string `json:"manager_key"`
	ClientID    string `json:"client_id,omitempty"`
	Goal        bool   `json:"goal,omitempty"`
}

// appStartClaims is the claims' folder.
type appStartClaims struct{ dir string }

// startClaims is the backend's claims, beside its state file.
func (b *appBackend) startClaims() appStartClaims {
	return appStartClaims{dir: filepath.Join(filepath.Dir(b.state.where()), appStartClaimsName)}
}

// appClaimName is a request's claim file: the request's sha256, never its words.
func appClaimName(request string) string {
	sum := sha256.Sum256([]byte(request))
	return hex.EncodeToString(sum[:]) + ".json"
}

// errNoClaims is a claims folder that does not exist yet: no request has ever
// been claimed in it.
var errNoClaims = errors.New("no team start has been recorded")

// open is the folder as a root no name can lead out of. It must be a plain
// folder: a link where it should be is refused, never followed.
func (c appStartClaims) open(create bool) (*os.Root, error) {
	if create {
		if err := os.MkdirAll(c.dir, appStateFolderMode); err != nil {
			return nil, err
		}
	}
	info, err := os.Lstat(c.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errNoClaims
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a plain folder", c.dir)
	}
	root, err := os.OpenRoot(c.dir)
	if err != nil {
		return nil, err
	}
	if opened, err := root.Stat("."); err != nil || !os.SameFile(info, opened) {
		_ = root.Close()
		return nil, fmt.Errorf("%s changed while it was opened", c.dir)
	}
	return root, nil
}

// read is request's claim. found is false only when the folder or the file is
// provably not there; ANY OTHER FAILURE IS AN ERROR, never "no claim", because
// a claim that could not be read may say its goal was sent.
func (c appStartClaims) read(request string) (claim appStartClaim, found bool, err error) {
	root, err := c.open(false)
	if errors.Is(err, errNoClaims) {
		return appStartClaim{}, false, nil
	}
	if err != nil {
		return appStartClaim{}, false, err
	}
	defer root.Close()
	name := appClaimName(request)
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return appStartClaim{}, false, nil
	}
	if err != nil {
		return appStartClaim{}, false, err
	}
	if !info.Mode().IsRegular() {
		return appStartClaim{}, false, fmt.Errorf("the record of request %s is not a plain file", request)
	}
	file, err := root.OpenFile(name, os.O_RDONLY, 0)
	if err != nil {
		return appStartClaim{}, false, err
	}
	defer file.Close()
	if opened, err := file.Stat(); err != nil || !os.SameFile(info, opened) {
		return appStartClaim{}, false, fmt.Errorf("the record of request %s changed while it was read", request)
	}
	raw, err := io.ReadAll(io.LimitReader(file, appStartClaimMax+1))
	if err != nil {
		return appStartClaim{}, false, err
	}
	if len(raw) > appStartClaimMax {
		return appStartClaim{}, false, fmt.Errorf("the record of request %s is larger than any record", request)
	}
	if err := json.Unmarshal(raw, &claim); err != nil {
		return appStartClaim{}, false, fmt.Errorf("the record of request %s cannot be read: %w", request, err)
	}
	if claim.V != appStartClaimV || claim.RequestID != request || claim.Crew == "" ||
		(claim.State != appClaimIntent && claim.State != appClaimCommitted && claim.State != appClaimWithheld) {
		return appStartClaim{}, false, fmt.Errorf("the record of request %s is not one this engine wrote", request)
	}
	return claim, true, nil
}

// create claims claim.RequestID, durably. made is false when the request is
// already claimed, by this process or another; the claim on disk is then the
// other start's, and is left as it is.
func (c appStartClaims) create(claim appStartClaim) (made bool, err error) {
	root, err := c.open(true)
	if err != nil {
		return false, err
	}
	defer root.Close()
	temp, err := appClaimTemp(root, claim)
	if err != nil {
		return false, err
	}
	defer root.Remove(temp)
	name := appClaimName(claim.RequestID)
	// THE LINK IS THE CLAIM: it publishes a whole, synced file under the
	// request's name, or fails because that name is taken.
	if err := root.Link(temp, name); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, err
	}
	_ = root.Remove(temp)
	if err := appSyncRoot(root); err != nil {
		// A claim the disk may not keep is not one to start on.
		_ = root.Remove(name)
		return false, err
	}
	return true, nil
}

// commit rewrites claim whole as committed: from here the goal may be handed
// over. An error means it may or may not be on disk, and the goal is not
// handed over.
func (c appStartClaims) commit(claim appStartClaim) error {
	return c.rewrite(claim, appClaimCommitted)
}

// withhold rewrites claim whole as withheld: its start will not hand the goal
// over. It is written only by a start that has not committed and is not going
// to, so it can never stand over a goal that went.
func (c appStartClaims) withhold(claim appStartClaim) error {
	return c.rewrite(claim, appClaimWithheld)
}

// rewrite replaces claim's file whole with claim in state.
func (c appStartClaims) rewrite(claim appStartClaim, state string) error {
	claim.State = state
	root, err := c.open(false)
	if err != nil {
		return err
	}
	defer root.Close()
	temp, err := appClaimTemp(root, claim)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	if err := root.Rename(temp, appClaimName(claim.RequestID)); err != nil {
		return err
	}
	return appSyncRoot(root)
}

// drop takes back the claim of a start that made nothing. It is best-effort:
// a claim left behind says intent with no crew, which is refused, never sent.
func (c appStartClaims) drop(request string) {
	root, err := c.open(false)
	if err != nil {
		return
	}
	defer root.Close()
	if root.Remove(appClaimName(request)) == nil {
		_ = appSyncRoot(root)
	}
}

// appClaimTemp writes claim to a new, synced file of its own in root and
// answers its name.
func appClaimTemp(root *os.Root, claim appStartClaim) (string, error) {
	raw, err := json.Marshal(claim)
	if err != nil {
		return "", err
	}
	if len(raw) > appStartClaimMax {
		return "", fmt.Errorf("the record of request %s would be larger than any record", claim.RequestID)
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
