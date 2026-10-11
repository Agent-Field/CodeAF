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
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
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
// working, and left empty for a row that does not expire.
type Token struct {
	ID      string    `json:"id"`
	Token   string    `json:"token"`
	Expires time.Time `json:"expires,omitempty"`
	Created time.Time `json:"created,omitempty"`
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

// Remove deletes the row with id.
func Remove(path, id string) error {
	rows, err := Load(path)
	if err != nil {
		return err
	}
	next := make([]Token, 0, len(rows))
	found := false
	for _, row := range rows {
		if row.ID == id {
			found = true
			continue
		}
		next = append(next, row)
	}
	if !found {
		return ErrNotFound
	}
	return Save(path, next)
}

func (t Token) valid() error {
	if t.ID == "" || strings.TrimSpace(t.ID) != t.ID {
		return errors.New("access token id is empty")
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
// else is refused.
func Bind(path, id string, profile Profile, now time.Time) error {
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer func() { _ = filelock.Unlock(lock) }()
	rows, err := Load(path)
	if err != nil {
		return err
	}
	for i := range rows {
		row := &rows[i]
		if row.ID != id {
			continue
		}
		if !row.Expires.IsZero() && !now.Before(row.Expires) {
			return os.ErrPermission
		}
		if row.Profile != nil && row.Profile.Handle != profile.Handle {
			return os.ErrPermission
		}
		row.Profile = &profile
		// The first sign-in is when they joined. A later sign-in must not
		// move that instant.
		if row.Joined.IsZero() {
			row.Joined = now
		}
		return Save(path, rows)
	}
	return os.ErrPermission
}
