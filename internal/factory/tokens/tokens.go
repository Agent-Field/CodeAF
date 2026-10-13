// Package tokens keeps factory access-token rows on disk.
//
// The host config's own access token stays in config.yaml: rotating that
// secret is a different door and must not drop these rows. Every other access
// token, including one minted by `codeaf factory token add`, is a row in
// tokens.json beside that file. A row may name when it expires. The service
// reads the file on each request, so revoke applies without a restart.
package tokens

import (
	"crypto/rand"
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

// ErrExists is an id collision. The caller mints another id.
var ErrExists = errors.New("access token id already exists")

// ErrNotFound is an id that is not in the file.
var ErrNotFound = errors.New("no access token")

// Profile keeps only public GitHub facts; the GitHub token is never persisted.
type Profile struct {
	Handle    string   `json:"handle"`
	Name      string   `json:"name"`
	AvatarURL string   `json:"avatar_url"`
	Scopes    []string `json:"scopes"`
}

// Token is one access token row. Expires is set when the row should stop
// working, and left empty for a row that does not expire. InvitedBy is set
// when someone minted the row as an invite; a plain access token leaves it
// empty. Revoked keeps the row as a tombstone so a later presentation can
// say the link was revoked. The secret is cleared; TokenHash is what matches.
type Token struct {
	Operator     bool      `json:"operator,omitempty"`
	AccessLink   bool      `json:"access_link,omitempty"`
	ID           string    `json:"id"`
	Token        string    `json:"token,omitempty"`
	TokenHash    string    `json:"token_hash,omitempty"`
	InvitedBy    string    `json:"invited_by,omitempty"`
	InvitedLogin string    `json:"invited_login,omitempty"`
	InvitedName  string    `json:"invited_name,omitempty"`
	Expires      time.Time `json:"expires,omitempty"`
	Created      time.Time `json:"created,omitempty"`
	Revoked      bool      `json:"revoked,omitempty"`
	// Joined is when this row's holder first signed in with GitHub.
	Joined  time.Time `json:"joined,omitempty"`
	Profile *Profile  `json:"profile,omitempty"`
}

// Mint chooses a new id and secret. The caller sets Expires and Created
// before Add.
func Mint() (Token, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return Token{}, err
	}
	id := make([]byte, 4)
	if _, err := rand.Read(id); err != nil {
		return Token{}, err
	}
	return Token{ID: "tok_" + hex.EncodeToString(id), Token: hex.EncodeToString(secret)}, nil
}

// Load reads the rows. A missing file is no rows, not an error.
func Load(path string) ([]Token, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		Tokens []Token `json:"tokens"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("access tokens %s: %w", path, err)
	}
	for _, row := range doc.Tokens {
		if err := row.valid(); err != nil {
			return nil, fmt.Errorf("access tokens %s: %w", path, err)
		}
	}
	return doc.Tokens, nil
}

// Save replaces the file by rename so a reader never sees half a row.
func Save(path string, rows []Token) error {
	if rows == nil {
		rows = []Token{}
	}
	body, err := json.MarshalIndent(struct {
		Tokens []Token `json:"tokens"`
	}{rows}, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(path, append(body, '\n'))
}

// Add appends one row. The id must be new.
func Add(path string, row Token) error {
	if err := row.valid(); err != nil {
		return err
	}
	rows, err := Load(path)
	if err != nil {
		return err
	}
	for _, old := range rows {
		if old.ID == row.ID {
			return ErrExists
		}
	}
	return Save(path, append(rows, row))
}

// Remove retires the row. A second revoke is ErrNotFound. The secret is
// replaced by its hash so a later presentation can be told the link was
// revoked, and list no longer shows the row.
func Remove(path, id string) error {
	rows, err := Load(path)
	if err != nil {
		return err
	}
	found := false
	for i := range rows {
		if rows[i].ID != id {
			continue
		}
		if rows[i].Revoked {
			return ErrNotFound
		}
		rows[i].TokenHash = hashSecret(rows[i].Token)
		rows[i].Token = ""
		rows[i].Revoked = true
		found = true
		break
	}
	if !found {
		return ErrNotFound
	}
	return Save(path, rows)
}

// hashSecret is the hex sha256 of a token secret.
func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// Matches is true when secret is this row's token, including a revoked row
// that kept only the hash.
func (t Token) Matches(secret string) bool {
	if secret == "" {
		return false
	}
	got := sha256.Sum256([]byte(secret))
	if t.Token != "" {
		want := sha256.Sum256([]byte(t.Token))
		if subtle.ConstantTimeCompare(want[:], got[:]) == 1 {
			return true
		}
	}
	if t.TokenHash == "" {
		return false
	}
	raw, err := hex.DecodeString(t.TokenHash)
	if err != nil || len(raw) != sha256.Size {
		return false
	}
	return subtle.ConstantTimeCompare(raw, got[:]) == 1
}

func (t Token) valid() error {
	if t.ID == "" || strings.TrimSpace(t.ID) != t.ID {
		return errors.New("access token id is empty")
	}
	if t.Revoked {
		if t.TokenHash == "" {
			return errors.New("revoked access token has no hash")
		}
		return nil
	}
	if t.Token == "" || strings.TrimSpace(t.Token) != t.Token || strings.ContainsAny(t.Token, " \t\r\n") {
		return errors.New("access token must contain no whitespace")
	}
	return nil
}

// writePrivate publishes a complete private file by rename, the same way the
// host config is written, so this package can be imported by both the command
// and the service.
func writePrivate(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tokens-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(body); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Bind remembers who signed in with this access token, under the same lock as
// add and revoke. The first GitHub handle sticks; a later sign-in as someone
// else is refused. An operator claim records consent before publishing identity.
func Bind(path, id string, profile Profile, now time.Time, claimEvent ...func() error) error {
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
		if rows[i].ID != id {
			continue
		}
		if err := bindProfile(path, &rows[i], profile, now, claimEvent); err != nil {
			return err
		}
		return Save(path, rows)
	}
	return os.ErrPermission
}

func bindingAllowed(row Token, handle string, now time.Time) bool {
	if row.Revoked || (row.Operator && !row.Joined.IsZero()) {
		return false
	}
	if !row.Expires.IsZero() && !now.Before(row.Expires) {
		return false
	}
	if row.InvitedLogin != "" && row.InvitedLogin != handle {
		return false
	}
	return row.Profile == nil || row.Profile.Handle == handle
}

func bindProfile(path string, row *Token, profile Profile, now time.Time, claimEvent []func() error) error {
	if !bindingAllowed(*row, profile.Handle, now) {
		return os.ErrPermission
	}
	if row.Operator {
		if len(claimEvent) > 0 {
			if err := claimEvent[0](); err != nil {
				return err
			}
		}
		if err := saveOperator(path, OperatorClaim{Login: profile.Handle, ClaimedAt: now}); err != nil {
			return err
		}
	}
	row.Profile = &profile
	// The first sign-in is when they joined; returning cannot move that instant.
	if row.Joined.IsZero() {
		row.Joined = now
	}
	return nil
}
