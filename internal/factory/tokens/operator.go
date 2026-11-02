package tokens

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// OperatorLinkTTL bounds the claim invitation, not the operator's later sessions.
const OperatorLinkTTL = 24 * time.Hour

// OperatorClaim survives credential rotation and records identity proved by GitHub.
type OperatorClaim struct {
	Login     string    `json:"login"`
	ClaimedAt time.Time `json:"claimed_at"`
}

// LoadOperator reads the claim beside the access links without consulting host credentials.
func LoadOperator(tokensPath string) (OperatorClaim, error) {
	var claim OperatorClaim
	if tokensPath == "" {
		return claim, nil
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(tokensPath), "operator.json"))
	if errors.Is(err, os.ErrNotExist) {
		return claim, nil
	}
	if err != nil {
		return claim, err
	}
	err = json.Unmarshal(body, &claim)
	if err == nil && (claim.Login == "" || claim.ClaimedAt.IsZero()) {
		err = errors.New("invalid factory operator claim")
	}
	return claim, err
}

func saveOperator(path string, claim OperatorClaim) error {
	body, err := json.Marshal(claim)
	if err != nil {
		return err
	}
	return writePrivate(filepath.Join(filepath.Dir(path), "operator.json"), body)
}

// RotateOperator revokes older claim links under the same lock used by sign-in.
// A claimed operator remains the bound login during recovery, regardless of host tokens.
func RotateOperator(path, hostLogin string, now time.Time, recovery bool) (Token, error) {
	unlock, err := lockRows(path)
	if err != nil {
		return Token{}, err
	}
	defer unlock()
	return rotateOperatorLocked(path, hostLogin, now, recovery)
}

// InviteOperator names the GitHub login an operator link minted without one
// is for. Serve mints the row before it says ready and names the login once
// GitHub answers, before the link is printed, so the secret is never shown
// unbound. A row already named, revoked or gone is left as it is.
func InviteOperator(path, id, login string) error {
	unlock, err := lockRows(path)
	if err != nil {
		return err
	}
	defer unlock()
	rows, err := Load(path)
	if err != nil {
		return err
	}
	for i := range rows {
		if row := &rows[i]; row.ID == id && row.Operator && !row.Revoked && row.InvitedLogin == "" {
			row.InvitedLogin = login
			return Save(path, rows)
		}
	}
	return nil
}

// lockRows is shared by link rotation and GitHub binding so neither can race a claim.
func lockRows(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(lock, true, false); err != nil {
		lock.Close()
		return nil, err
	}
	return func() { _ = filelock.Unlock(lock); _ = lock.Close() }, nil
}

func rotateOperatorLocked(path, hostLogin string, now time.Time, recovery bool) (Token, error) {
	claim, err := LoadOperator(path)
	if err != nil {
		return Token{}, err
	}
	if claim.Login != "" && !recovery {
		return Token{}, nil
	}
	if claim.Login != "" {
		hostLogin = claim.Login
	}
	rows, err := Load(path)
	if err != nil {
		return Token{}, err
	}
	retireOperatorLinks(rows)
	row, err := Mint()
	if err != nil {
		return Token{}, err
	}
	row.Operator, row.AccessLink = true, true
	row.Created, row.Expires = now, now.Add(OperatorLinkTTL)
	row.InvitedLogin = hostLogin
	return row, Save(path, append(rows, row))
}

// retireOperatorLinks leaves claimed sessions intact; their invitation secret is already spent.
func retireOperatorLinks(rows []Token) {
	for i := range rows {
		row := &rows[i]
		if row.Operator && !row.Revoked && row.Joined.IsZero() {
			row.TokenHash, row.Token, row.Revoked = hashSecret(row.Token), "", true
		}
	}
}
