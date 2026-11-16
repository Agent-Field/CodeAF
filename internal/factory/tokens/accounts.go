package tokens

// ── USERNAME + PASSWORD ACCOUNTS ────────────────────────────────────────────
//
// A person on an organization server is a handle. A GitHub person's handle is
// their GitHub login; a password person's handle is the username they chose
// when they signed up. Both live in ONE namespace: a username may not equal any
// handle the server already knows, and the server refuses a GitHub sign-in
// whose login is a password person's handle.
//
// WHAT IS KEPT. accounts.json beside tokens.json holds each password person's
// handle, display name and a PHC-encoded argon2id hash — never the password —
// and the one-time reset links an operator made, by the SHA-256 of their
// secret, never the secret. The file is 0600 in a 0700 folder, like tokens.json.
//
// ONE LOCK, ONE COMMIT. Every write here is made under the same cross-process
// lock as the access-link rows (lockRows). Signing up through a link spends the
// link's use and creates the account inside that one critical section: the
// account file is written first, then the link rows, and if the rows cannot be
// written the account is taken back out before the lock is released. Two
// people racing for the last place on a link therefore cannot both get an
// account; exactly one of them does. A reset link is used and the password
// replaced in a single rename of accounts.json, so it is atomic outright.

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Kinds of person, as a profile carries them. A profile with no kind is a
// GitHub person: every profile written before password accounts existed.
const (
	KindGitHub   = "github"
	KindPassword = "password"
)

// ResetLinkTTL is how long an operator's one-time password reset link lasts.
const ResetLinkTTL = time.Hour

// Typed refusals from the account writes. ErrHandleTaken is the one sign-up
// refusal a person reads as "that username is taken"; the rest are links.
var (
	ErrHandleTaken  = errors.New("that username is taken")
	ErrNoAccount    = errors.New("no such password account")
	ErrResetInvalid = errors.New("this reset link is not valid")
)

// Account is one password person. Hash is a PHC string; Changed is when the
// password was last set, which a session made before it does not outlive.
type Account struct {
	Handle  string    `json:"handle"`
	Name    string    `json:"name,omitempty"`
	Hash    string    `json:"hash"`
	Created time.Time `json:"created"`
	Changed time.Time `json:"changed"`
}

// Reset is one operator-made reset link. Only SecretHash is kept.
type Reset struct {
	ID         string    `json:"id"`
	SecretHash string    `json:"secret_hash"`
	Handle     string    `json:"handle"`
	CreatedBy  string    `json:"created_by,omitempty"`
	Created    time.Time `json:"created"`
	Expires    time.Time `json:"expires"`
	Used       time.Time `json:"used,omitempty"`
}

// Matches is true when secret is this reset link's, compared in constant time.
func (r Reset) Matches(secret string) bool {
	if secret == "" {
		return false
	}
	got := sha256.Sum256([]byte(secret))
	raw, err := hex.DecodeString(r.SecretHash)
	if err != nil || len(raw) != sha256.Size {
		return false
	}
	return subtle.ConstantTimeCompare(raw, got[:]) == 1
}

// Open is whether the reset link may still be used at now.
func (r Reset) Open(now time.Time) bool {
	return r.Used.IsZero() && now.Before(r.Expires)
}

// Accounts is the whole of accounts.json.
type Accounts struct {
	Accounts []Account `json:"accounts"`
	Resets   []Reset   `json:"resets"`
}

// Find is the account for handle, compared case-insensitively.
func (a Accounts) Find(handle string) (Account, bool) {
	handle = strings.ToLower(strings.TrimSpace(handle))
	for _, acct := range a.Accounts {
		if acct.Handle == handle {
			return acct, true
		}
	}
	return Account{}, false
}

// AccountsPath is accounts.json beside the tokens file.
func AccountsPath(tokensPath string) string {
	return filepath.Join(filepath.Dir(tokensPath), "accounts.json")
}

// LoadAccounts reads accounts.json. A missing file is no accounts.
func LoadAccounts(tokensPath string) (Accounts, error) {
	var doc Accounts
	if tokensPath == "" {
		return doc, nil
	}
	body, err := os.ReadFile(AccountsPath(tokensPath))
	if errors.Is(err, os.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return doc, err
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		// The error names the file, never its content: a hash is not a
		// password, but it is still nothing a log needs.
		return Accounts{}, fmt.Errorf("password accounts %s are not readable", AccountsPath(tokensPath))
	}
	return doc, nil
}

func saveAccounts(tokensPath string, doc Accounts) error {
	if doc.Accounts == nil {
		doc.Accounts = []Account{}
	}
	if doc.Resets == nil {
		doc.Resets = []Reset{}
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(AccountsPath(tokensPath), append(body, '\n'))
}

// TakenFunc reports a handle the server knows from somewhere the token files
// do not record (a signed-in session, the host's own login). It is asked
// under the row lock, so it must not take that lock itself.
type TakenFunc func(handle string) bool

// handleTaken is whether handle is any person the files know: a password
// account, anyone who used or was named by a link, or the operator.
func handleTaken(tokensPath string, accounts Accounts, rows []Token, handle string, taken TakenFunc) (bool, error) {
	if _, ok := accounts.Find(handle); ok {
		return true, nil
	}
	for _, row := range rows {
		if strings.EqualFold(row.InvitedLogin, handle) && row.Profile == nil {
			// A link made for one GitHub login keeps that login for its person.
			return true, nil
		}
		for _, j := range row.Members() {
			if strings.EqualFold(j.Profile.Handle, handle) {
				return true, nil
			}
		}
	}
	claim, err := LoadOperator(tokensPath)
	if err != nil {
		return false, err
	}
	if strings.EqualFold(claim.Login, handle) {
		return true, nil
	}
	return taken != nil && taken(handle), nil
}

// SignUp creates a password account through the access link id and spends
// that link's use, in one critical section under the row lock (see the file
// comment). profile is the person as the link records them. claimEvent is
// the operator claim's audit write, made before the claim is published, as
// [Join] makes it. A link made for one login admits only that username.
func SignUp(tokensPath, id string, acct Account, profile Profile, now time.Time, taken TakenFunc, claimEvent ...func() error) (Joined, error) {
	unlock, err := lockRows(tokensPath)
	if err != nil {
		return Joined{}, err
	}
	defer unlock()
	rows, err := Load(tokensPath)
	if err != nil {
		return Joined{}, err
	}
	accounts, err := LoadAccounts(tokensPath)
	if err != nil {
		return Joined{}, err
	}
	index := -1
	for i := range rows {
		if rows[i].ID == id {
			index = i
		}
	}
	if index < 0 || !(rows[index].AccessLink || rows[index].Operator) {
		return Joined{}, os.ErrPermission
	}
	row := &rows[index]
	// The link is judged before the name, so a used or ended link never
	// answers whether a username exists.
	if row.Human() {
		switch row.Reason(now) {
		case "revoked":
			return Joined{}, ErrRevoked
		case "expired":
			return Joined{}, ErrExpired
		case "used_up":
			return Joined{}, ErrUsedUp
		}
		if row.InvitedLogin != "" && row.InvitedLogin != profile.Handle {
			return Joined{}, ErrWrongPerson
		}
	} else if !bindingAllowed(*row, profile.Handle, now) {
		return Joined{}, os.ErrPermission
	}
	isTaken, err := handleTaken(tokensPath, accounts, rows, profile.Handle, taken)
	if err != nil {
		return Joined{}, err
	}
	// A link for exactly this login names the person it is for; that is not
	// someone else holding the name.
	if isTaken && !(row.InvitedLogin == profile.Handle && row.Profile == nil && !accountHeld(accounts, profile.Handle)) {
		return Joined{}, ErrHandleTaken
	}
	var out Joined
	if row.Human() {
		out, err = joinHuman(row, profile, now)
		if err != nil {
			return Joined{}, err
		}
	}
	acct.Handle, acct.Created, acct.Changed = profile.Handle, now, now
	before := accounts
	accounts.Accounts = append(append([]Account(nil), accounts.Accounts...), acct)
	if err := saveAccounts(tokensPath, accounts); err != nil {
		return Joined{}, err
	}
	undo := func() { _ = saveAccounts(tokensPath, before) }
	if row.Operator {
		if err := bindProfile(tokensPath, row, profile, now, claimEvent); err != nil {
			undo()
			return Joined{}, err
		}
	}
	if err := Save(tokensPath, rows); err != nil {
		undo()
		return Joined{}, err
	}
	return out, nil
}

func accountHeld(a Accounts, handle string) bool {
	_, ok := a.Find(handle)
	return ok
}

// SetPassword replaces handle's hash. It is the one door for a change made
// while signed in and for a transparent rehash at stronger parameters; only
// a change moves Changed, which ends the person's older sessions.
func SetPassword(tokensPath, handle, hash string, now time.Time, change bool) error {
	unlock, err := lockRows(tokensPath)
	if err != nil {
		return err
	}
	defer unlock()
	accounts, err := LoadAccounts(tokensPath)
	if err != nil {
		return err
	}
	for i := range accounts.Accounts {
		if accounts.Accounts[i].Handle == handle {
			accounts.Accounts[i].Hash = hash
			if change {
				accounts.Accounts[i].Changed = now
			}
			return saveAccounts(tokensPath, accounts)
		}
	}
	return ErrNoAccount
}

// AddReset records a reset link for an existing password person. Any older
// open reset link for that person is ended, so only the newest one works.
func AddReset(tokensPath string, reset Reset, secret string) error {
	unlock, err := lockRows(tokensPath)
	if err != nil {
		return err
	}
	defer unlock()
	accounts, err := LoadAccounts(tokensPath)
	if err != nil {
		return err
	}
	if _, ok := accounts.Find(reset.Handle); !ok {
		return ErrNoAccount
	}
	sum := sha256.Sum256([]byte(secret))
	reset.SecretHash = hex.EncodeToString(sum[:])
	kept := accounts.Resets[:0:0]
	for _, old := range accounts.Resets {
		// Ended links are dropped after a day; a used or superseded one only
		// has to answer "not valid", which an absent one also does.
		if old.Handle == reset.Handle && old.Used.IsZero() {
			old.Used = reset.Created
		}
		if reset.Created.Sub(old.Expires) < 24*time.Hour {
			kept = append(kept, old)
		}
	}
	accounts.Resets = append(kept, reset)
	return saveAccounts(tokensPath, accounts)
}

// FindReset is the reset link secret opens, open or not.
func (a Accounts) FindReset(secret string) (Reset, bool) {
	for _, r := range a.Resets {
		if r.Matches(secret) {
			return r, true
		}
	}
	return Reset{}, false
}

// UseReset spends the reset link secret opens and sets its person's new hash,
// in one write. It answers whose password it was.
func UseReset(tokensPath, secret, hash string, now time.Time) (string, error) {
	unlock, err := lockRows(tokensPath)
	if err != nil {
		return "", err
	}
	defer unlock()
	accounts, err := LoadAccounts(tokensPath)
	if err != nil {
		return "", err
	}
	for i := range accounts.Resets {
		r := &accounts.Resets[i]
		if !r.Matches(secret) {
			continue
		}
		if !r.Open(now) {
			return "", ErrResetInvalid
		}
		for k := range accounts.Accounts {
			if accounts.Accounts[k].Handle == r.Handle {
				r.Used = now
				accounts.Accounts[k].Hash, accounts.Accounts[k].Changed = hash, now
				return r.Handle, saveAccounts(tokensPath, accounts)
			}
		}
		return "", ErrResetInvalid
	}
	return "", ErrResetInvalid
}

// RemoveAccount deletes handle's account and its reset links: the operator
// removed the person, so the password no longer signs anyone in. A handle
// with no account is not an error.
func RemoveAccount(tokensPath, handle string) error {
	if tokensPath == "" {
		return nil
	}
	unlock, err := lockRows(tokensPath)
	if err != nil {
		return err
	}
	defer unlock()
	accounts, err := LoadAccounts(tokensPath)
	if err != nil {
		return err
	}
	kept := accounts.Accounts[:0:0]
	for _, a := range accounts.Accounts {
		if a.Handle != handle {
			kept = append(kept, a)
		}
	}
	if len(kept) == len(accounts.Accounts) {
		return nil
	}
	resets := accounts.Resets[:0:0]
	for _, r := range accounts.Resets {
		if r.Handle != handle {
			resets = append(resets, r)
		}
	}
	accounts.Accounts, accounts.Resets = kept, resets
	return saveAccounts(tokensPath, accounts)
}
