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
	// MaxUses is how many people a human access link admits. A row written
	// before the field existed has none, and means one person. Plain access
	// tokens and operator claims never read it.
	MaxUses int `json:"max_uses,omitempty"`
	// Joiners are the people who consumed a use, first to last. Profile and
	// Joined keep describing the first of them so a build that predates this
	// field still reads the row as spent rather than as open.
	Joiners []Joiner `json:"joiners,omitempty"`
	// RevokedAt is when the operator ended the link. Rows revoked before the
	// field existed leave it empty.
	RevokedAt time.Time `json:"revoked_at,omitempty"`
}

// Joiner is one person who used a link: public GitHub facts and the instant.
type Joiner struct {
	Profile Profile   `json:"profile"`
	Joined  time.Time `json:"joined"`
}

// MaxLinkPeople bounds an up-to-N human link.
const MaxLinkPeople = 50

// HumanLinkTTL is how long a new human access link lasts when made.
const HumanLinkTTL = 7 * 24 * time.Hour

// Human is a person's invitation: an access link that is not the operator's
// claim. Its secret may start a GitHub sign-in and nothing else.
func (t Token) Human() bool { return t.AccessLink && !t.Operator }

// Counted is a human link that carries its own use limit, written by a build
// that has the limit. Only these keep their members' sessions past the
// link's revoke or expiry; an older row keeps the rules it was written under.
func (t Token) Counted() bool { return t.Human() && t.MaxUses > 0 }

// Limit is how many people the link admits; a missing limit means one.
func (t Token) Limit() int {
	if t.MaxUses > 0 {
		return t.MaxUses
	}
	return 1
}

// Members lists who consumed a use. A row from before Joiners existed names
// its one person through Profile.
func (t Token) Members() []Joiner {
	out := append([]Joiner(nil), t.Joiners...)
	if t.Profile != nil && t.Profile.Handle != "" {
		for _, j := range out {
			if j.Profile.Handle == t.Profile.Handle {
				return out
			}
		}
		out = append([]Joiner{{Profile: *t.Profile, Joined: t.Joined}}, out...)
	}
	return out
}

// Has is whether handle already consumed a use of this link.
func (t Token) Has(handle string) bool {
	for _, j := range t.Members() {
		if j.Profile.Handle == handle {
			return true
		}
	}
	return false
}

// Used is how many uses have been consumed.
func (t Token) Used() int { return len(t.Members()) }

// Remaining is how many people may still join; revoked and expired links
// are judged by the caller, not here.
func (t Token) Remaining() int {
	if left := t.Limit() - t.Used(); left > 0 {
		return left
	}
	return 0
}

// Reason is why the row cannot admit a new person at now, in the order the
// invitation card checks: revoked, expired, used up. Empty means open.
func (t Token) Reason(now time.Time) string {
	switch {
	case t.Revoked:
		return "revoked"
	case !t.Expires.IsZero() && !now.Before(t.Expires):
		return "expired"
	case t.Human() && t.Remaining() == 0:
		return "used_up"
	}
	return ""
}

// EndedAt is when a human link stopped admitting people, or zero while it
// still does. Rows from before the fields existed fall back to what they hold.
func (t Token) EndedAt(now time.Time) time.Time {
	switch t.Reason(now) {
	case "revoked":
		if !t.RevokedAt.IsZero() {
			return t.RevokedAt
		}
		return t.Created
	case "expired":
		return t.Expires
	case "used_up":
		var last time.Time
		for _, j := range t.Members() {
			if j.Joined.After(last) {
				last = j.Joined
			}
		}
		return last
	}
	return time.Time{}
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
func Remove(path, id string) error { return RemoveAt(path, id, time.Now()) }

// RemoveAt is Remove with the instant the link ended, for a caller that has
// its own clock.
func RemoveAt(path, id string, now time.Time) error {
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
		rows[i].RevokedAt = now.UTC()
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

// Typed refusals from Join. Each is also os.ErrPermission, which is what Bind
// always returned, so a caller that only asks "was I refused" keeps working.
var (
	ErrRevoked = refusal("this access link was revoked")
	ErrExpired = refusal("this access link has expired")
	ErrUsedUp  = refusal("this access link has no uses left")
	// ErrWrongPerson is a link made for one GitHub login, or a token already
	// bound to someone else.
	ErrWrongPerson = refusal("this access link is for someone else")
)

type refusal string

func (r refusal) Error() string        { return string(r) }
func (r refusal) Is(target error) bool { return target == os.ErrPermission }

// Joined says what Join did to the row's use count.
type Joined struct {
	// Consumed is true only when this call took a use. A person who was
	// already in, or who came back, consumes nothing.
	Consumed bool
}

// Bind remembers who signed in with this access token, under the same lock as
// add and revoke. The first GitHub handle sticks; a later sign-in as someone
// else is refused. An operator claim records consent before publishing identity.
func Bind(path, id string, profile Profile, now time.Time, claimEvent ...func() error) error {
	_, err := Join(path, id, profile, now, claimEvent...)
	return err
}

// Join is Bind that also says whether a use was consumed. Call it only after
// GitHub has proved who the person is and they can see a repository: it is
// the one place a use is spent, under the cross-process row lock, so two
// people racing for the last place cannot both get it.
func Join(path, id string, profile Profile, now time.Time, claimEvent ...func() error) (Joined, error) {
	unlock, err := lockRows(path)
	if err != nil {
		return Joined{}, err
	}
	defer unlock()
	rows, err := Load(path)
	if err != nil {
		return Joined{}, err
	}
	for i := range rows {
		if rows[i].ID != id {
			continue
		}
		var out Joined
		if rows[i].Human() {
			out, err = joinHuman(&rows[i], profile, now)
		} else {
			err = bindProfile(path, &rows[i], profile, now, claimEvent)
		}
		if err != nil {
			return Joined{}, err
		}
		return out, Save(path, rows)
	}
	return Joined{}, os.ErrPermission
}

// joinHuman spends one use of a human link. Someone already counted is
// admitted again without spending another, and their public facts refresh.
func joinHuman(row *Token, profile Profile, now time.Time) (Joined, error) {
	if row.InvitedLogin != "" && row.InvitedLogin != profile.Handle {
		return Joined{}, ErrWrongPerson
	}
	// Revoked and expired end the invitation for everyone, including someone
	// who is already counted; only the use limit spares a person who is in.
	switch row.Reason(now) {
	case "revoked":
		return Joined{}, ErrRevoked
	case "expired":
		return Joined{}, ErrExpired
	}
	members := row.Members()
	for _, j := range members {
		if j.Profile.Handle != profile.Handle {
			continue
		}
		// A person already counted is admitted again without a second use.
		if row.Profile != nil && row.Profile.Handle == profile.Handle {
			row.Profile = &profile
		}
		for k := range row.Joiners {
			if row.Joiners[k].Profile.Handle == profile.Handle {
				row.Joiners[k].Profile = profile
			}
		}
		return Joined{}, nil
	}
	if row.Remaining() == 0 {
		return Joined{}, ErrUsedUp
	}
	if row.Profile == nil {
		row.Profile, row.Joined = &profile, now
	}
	// Rows that predate Joiners name their one person only through Profile;
	// write that person down before adding the next so none is lost.
	row.Joiners = append(members, Joiner{Profile: profile, Joined: now})
	return Joined{Consumed: true}, nil
}

// Unjoin gives back the use Join just took for handle, when the sign-in could
// not be completed after the use was spent. It does nothing for a person who
// was not consumed by this link, and never touches the first-person fields.
func Unjoin(path, id, handle string) error {
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
		if rows[i].ID != id || !rows[i].Human() {
			continue
		}
		kept := rows[i].Joiners[:0:0]
		for _, j := range rows[i].Members() {
			if j.Profile.Handle != handle {
				kept = append(kept, j)
			}
		}
		rows[i].Joiners = kept
		if rows[i].Profile != nil && rows[i].Profile.Handle == handle {
			rows[i].Profile, rows[i].Joined = nil, time.Time{}
			if len(kept) > 0 {
				first := kept[0]
				rows[i].Profile, rows[i].Joined = &first.Profile, first.Joined
			}
		}
		return Save(path, rows)
	}
	return nil
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
