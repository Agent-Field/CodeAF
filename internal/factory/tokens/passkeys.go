package tokens

// ── PASSKEYS ON A PERSON'S ACCOUNT ──────────────────────────────────────────
//
// A passkey is a WebAuthn credential: a key pair kept by the person's own
// authenticator (iCloud Keychain, a password manager, a security key). The
// server keeps only its public half and the facts the WebAuthn rules ask a
// relying party to keep: the credential id, the COSE public key, the signature
// counter, the transports, the authenticator's AAGUID and its backup flags,
// plus the name the person gave it and when it was made and last used.
// NOTHING SECRET IS KEPT: a passkey's private key never leaves the person's
// authenticator, so this file holds nothing that signs anyone in.
//
// A person's passkeys live on their Account in accounts.json and are written
// under the same row lock as every other account write, so making an account
// with its first passkey through a link is the one critical section SignUp
// already is, and a credential id can never belong to two people: every
// write that adds one checks the whole file inside the lock first.
//
// THE LAST WAY IN CANNOT BE REMOVED. A person with no password keeps at least
// one passkey; RemovePasskey refuses the last one.

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"
)

// Refusals from the passkey writes.
var (
	ErrPasskeyTaken = errors.New("this passkey is already registered here")
	ErrNoPasskey    = errors.New("no such passkey")
	ErrLastWayIn    = errors.New("this is the only way this person signs in")
)

// PasskeyMost bounds how many passkeys one person may keep, so an account
// file cannot be grown without end by a signed-in session.
const PasskeyMost = 20

// Passkey is one WebAuthn credential's public record. ID and PublicKey are
// base64url without padding, as WebAuthn spells them on the wire.
type Passkey struct {
	ID             string    `json:"id"`
	PublicKey      string    `json:"public_key"`
	SignCount      uint32    `json:"sign_count"`
	Transports     []string  `json:"transports,omitempty"`
	AAGUID         string    `json:"aaguid,omitempty"`
	BackupEligible bool      `json:"backup_eligible"`
	BackupState    bool      `json:"backup_state"`
	Attestation    string    `json:"attestation,omitempty"`
	Name           string    `json:"name"`
	Created        time.Time `json:"created"`
	LastUsed       time.Time `json:"last_used,omitzero"`
}

// NewUserID is a fresh WebAuthn user handle: 32 random bytes, base64url. It
// is opaque on purpose: the spec asks that it carry nothing that identifies
// the person, so it is never the username.
func NewUserID() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// FindPasskey is the account holding credential id, and that passkey.
func (a Accounts) FindPasskey(id string) (Account, Passkey, bool) {
	if id == "" {
		return Account{}, Passkey{}, false
	}
	for _, acct := range a.Accounts {
		for _, pk := range acct.Passkeys {
			if pk.ID == id {
				return acct, pk, true
			}
		}
	}
	return Account{}, Passkey{}, false
}

// FindUserID is the account whose WebAuthn user handle is userID.
func (a Accounts) FindUserID(userID string) (Account, bool) {
	if userID == "" {
		return Account{}, false
	}
	for _, acct := range a.Accounts {
		if acct.UserID == userID {
			return acct, true
		}
	}
	return Account{}, false
}

// passkeysFree refuses any passkey in add whose credential id is already on
// an account. Called with the row lock held.
func passkeysFree(a Accounts, add []Passkey) error {
	for _, pk := range add {
		if _, _, held := a.FindPasskey(pk.ID); held || pk.ID == "" {
			return ErrPasskeyTaken
		}
	}
	return nil
}

// AddPasskey puts pk on handle's account. A credential id already held by
// anyone, this person included, is refused.
func AddPasskey(tokensPath, handle string, pk Passkey) error {
	unlock, err := lockRows(tokensPath)
	if err != nil {
		return err
	}
	defer unlock()
	accounts, err := LoadAccounts(tokensPath)
	if err != nil {
		return err
	}
	if err := passkeysFree(accounts, []Passkey{pk}); err != nil {
		return err
	}
	for i := range accounts.Accounts {
		acct := &accounts.Accounts[i]
		if acct.Handle != handle {
			continue
		}
		if len(acct.Passkeys) >= PasskeyMost {
			return errors.New("this account already keeps as many passkeys as it can")
		}
		if acct.UserID == "" {
			// An account made with a password gets its user handle with its
			// first passkey.
			if acct.UserID, err = NewUserID(); err != nil {
				return err
			}
		}
		acct.Passkeys = append(append([]Passkey(nil), acct.Passkeys...), pk)
		return saveAccounts(tokensPath, accounts)
	}
	return ErrNoAccount
}

// EnsureUserID answers handle's WebAuthn user handle, making one if the
// account has none yet, so a registration ceremony can name it.
func EnsureUserID(tokensPath, handle string) (string, error) {
	unlock, err := lockRows(tokensPath)
	if err != nil {
		return "", err
	}
	defer unlock()
	accounts, err := LoadAccounts(tokensPath)
	if err != nil {
		return "", err
	}
	for i := range accounts.Accounts {
		acct := &accounts.Accounts[i]
		if acct.Handle != handle {
			continue
		}
		if acct.UserID != "" {
			return acct.UserID, nil
		}
		if acct.UserID, err = NewUserID(); err != nil {
			return "", err
		}
		return acct.UserID, saveAccounts(tokensPath, accounts)
	}
	return "", ErrNoAccount
}

// UsedPasskey records a sign-in with credential id: its new counter and
// backup state, and when. A passkey removed meanwhile is ErrNoPasskey, so a
// ceremony that raced a removal does not sign anyone in.
func UsedPasskey(tokensPath, handle, id string, count uint32, backupState bool, now time.Time) error {
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
		acct := &accounts.Accounts[i]
		if acct.Handle != handle {
			continue
		}
		for k := range acct.Passkeys {
			if acct.Passkeys[k].ID == id {
				acct.Passkeys[k].SignCount, acct.Passkeys[k].BackupState, acct.Passkeys[k].LastUsed = count, backupState, now
				return saveAccounts(tokensPath, accounts)
			}
		}
	}
	return ErrNoPasskey
}

// RemovePasskey takes credential id off handle's account and moves its
// generation, which ends every session made before; it answers the new
// generation. The last way in is refused.
func RemovePasskey(tokensPath, handle, id string, now time.Time) (int64, error) {
	unlock, err := lockRows(tokensPath)
	if err != nil {
		return 0, err
	}
	defer unlock()
	accounts, err := LoadAccounts(tokensPath)
	if err != nil {
		return 0, err
	}
	for i := range accounts.Accounts {
		acct := &accounts.Accounts[i]
		if acct.Handle != handle {
			continue
		}
		kept := acct.Passkeys[:0:0]
		for _, pk := range acct.Passkeys {
			if pk.ID != id {
				kept = append(kept, pk)
			}
		}
		if len(kept) == len(acct.Passkeys) {
			return 0, ErrNoPasskey
		}
		if len(kept) == 0 && acct.Hash == "" {
			return 0, ErrLastWayIn
		}
		acct.Passkeys, acct.Changed, acct.Gen = kept, now, acct.Gen+1
		accounts.Devices = keptDevices(accounts, now)
		return acct.Gen, saveAccounts(tokensPath, accounts)
	}
	return 0, ErrNoAccount
}

// UseResetPasskey spends the reset link secret opens and makes pk its
// person's only way in, in one write: like a password reset, a reset by
// passkey replaces every way in, so the old password and passkeys end. It
// answers the person and the account's new generation.
func UseResetPasskey(tokensPath, secret string, pk Passkey, now time.Time) (string, int64, error) {
	unlock, err := lockRows(tokensPath)
	if err != nil {
		return "", 0, err
	}
	defer unlock()
	accounts, err := LoadAccounts(tokensPath)
	if err != nil {
		return "", 0, err
	}
	for i := range accounts.Resets {
		r := &accounts.Resets[i]
		if !r.Matches(secret) {
			continue
		}
		if !r.Open(now) {
			return "", 0, ErrResetInvalid
		}
		for k := range accounts.Accounts {
			acct := &accounts.Accounts[k]
			if acct.Handle != r.Handle {
				continue
			}
			// The person's own old passkeys are about to go, so only someone
			// else's holding this id is a refusal.
			others := accounts
			others.Accounts = append(append([]Account(nil), accounts.Accounts[:k]...), accounts.Accounts[k+1:]...)
			if err := passkeysFree(others, []Passkey{pk}); err != nil {
				return "", 0, err
			}
			if acct.UserID == "" {
				if acct.UserID, err = NewUserID(); err != nil {
					return "", 0, err
				}
			}
			r.Used = now
			acct.Hash, acct.Passkeys, acct.Changed, acct.Gen = "", []Passkey{pk}, now, acct.Gen+1
			accounts.Devices = keptDevices(accounts, now)
			return r.Handle, acct.Gen, saveAccounts(tokensPath, accounts)
		}
		return "", 0, ErrResetInvalid
	}
	return "", 0, ErrResetInvalid
}

// HandleTaken is whether handle is already a person here, read under the row
// lock. A passkey join asks it before the browser makes the passkey, so a
// person is not left with a passkey in their keychain for an account that was
// never made; SignUp asks again inside its own critical section, which is the
// answer that counts.
func HandleTaken(tokensPath, handle string, taken TakenFunc) (bool, error) {
	unlock, err := lockRows(tokensPath)
	if err != nil {
		return false, err
	}
	defer unlock()
	rows, err := Load(tokensPath)
	if err != nil {
		return false, err
	}
	accounts, err := LoadAccounts(tokensPath)
	if err != nil {
		return false, err
	}
	return handleTaken(tokensPath, accounts, rows, handle, taken)
}
