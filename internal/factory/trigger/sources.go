// Package trigger is the factory's Trigger API: the credentials and the
// webhooks that let another tool file work, say more about it, read how far
// it is and hear back, without a connector written for it.
//
//   - A SOURCE is a name registered by the operator (`codeaf factory source
//     add NAME`). Registering mints a token that can file, read and message
//     ONLY work whose source is NAME, and a signing secret for its webhooks.
//   - Work it files goes through inbox.Add, THE ONE DOOR, at no more than the
//     trust it was registered with.
//   - Every source with a callback has an inbox.Replier here that turns the
//     job's moments into signed deliveries, kept in a durable [Outbox] until
//     the callback answers 2xx.
//
// docs/design/factory-general/DESIGN.md, "Wave 2 — the Trigger API", is the
// contract this package fills.
package trigger

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

// SourcesFile is where a factory keeps its registered sources, in the
// floor's folder. It is not a number, so the store never reads it as a job.
//
// THE TOKEN IS KEPT ONLY AS ITS SHA-256, as factory access tokens are
// (tokens.Token.TokenHash): it is printed once, at add, and never again. The
// SIGNING SECRET IS THE ONE VALUE KEPT AS IT IS, because signing a delivery
// needs it; it lives in this private (0600) file and never in config.yaml or
// an answer after the add.
const SourcesFile = "trigger-sources.json"

// The refusals a person acts on.
var (
	ErrExists   = errors.New("a source with that name is already registered; revoke it first to mint a new token")
	ErrNotFound = errors.New("no source with that name is registered")
)

// MaxCallback is the longest callback address kept, in bytes.
const MaxCallback = 2048

// sourceName is a source as inbox.Entry.Source spells one: a lowercase word,
// optionally a second after one colon (`inbox:ops`).
var sourceName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}(:[a-z0-9][a-z0-9._/-]{0,63})?$`)

// reserved are names a registered source may not take: the floor's own
// origins, and the in-process connector whose answerer is already its own.
var reserved = map[string]bool{
	string(factory.OriginForge): true, string(factory.OriginTerminal): true, string(factory.OriginChat): true,
	"jira": true,
}

// Source is one registered source as the file keeps it.
type Source struct {
	Name      string       `json:"name"`
	TokenHash string       `json:"token_hash"`
	Secret    string       `json:"webhook_secret"`
	Callback  string       `json:"callback,omitempty"`
	Trust     factory.Tier `json:"trust"`
	Created   time.Time    `json:"created"`
}

// Public is a source as a person reads it: never the token, never the secret.
type Public struct {
	Name     string       `json:"name"`
	Callback string       `json:"callback,omitempty"`
	Trust    factory.Tier `json:"trust"`
	Created  time.Time    `json:"created"`
}

// Public is s without its secrets.
func (s Source) Public() Public {
	return Public{Name: s.Name, Callback: s.Callback, Trust: s.Trust, Created: s.Created}
}

// Minted is an add's answer: the source, and the two secrets, shown this once.
type Minted struct {
	Public
	Token  string `json:"token"`
	Secret string `json:"webhook_secret"`
}

// Matches says whether token is this source's, in constant time.
func (s Source) Matches(token string) bool {
	if token == "" || s.TokenHash == "" {
		return false
	}
	got := sha256.Sum256([]byte(token))
	want, err := hex.DecodeString(s.TokenHash)
	return err == nil && subtle.ConstantTimeCompare(want, got[:]) == 1
}

// Registry is the sources file of one floor.
type Registry struct {
	Path string
}

// RegistryAt is the registry in the floor folder root.
func RegistryAt(root string) Registry { return Registry{Path: filepath.Join(root, SourcesFile)} }

// CheckName refuses a name a source may not be registered under, in words a
// person acts on.
func CheckName(name string) error {
	switch {
	case name == "":
		return errors.New("a source needs a name, such as ops or inbox:ops")
	case reserved[name]:
		return fmt.Errorf("%q is a name the factory keeps for itself; pick another", name)
	case !sourceName.MatchString(name):
		return errors.New("a source name is lowercase letters, digits, . _ - and at most one colon, such as inbox:ops")
	}
	return nil
}

// CheckTrust is a source's trust: collaborator or stranger, empty being
// stranger. THE OPERATOR'S OWN STANDING IS NEVER A SOURCE'S: work that comes
// through a token handed to another tool is at most a collaborator's.
func CheckTrust(t factory.Tier) (factory.Tier, error) {
	switch t = factory.Tier(strings.ToLower(strings.TrimSpace(string(t)))); t {
	case "":
		return factory.TierStranger, nil
	case factory.TierCollab, factory.TierStranger:
		return t, nil
	}
	return "", errors.New("trust must be collaborator or stranger")
}

// CheckCallback is an http or https address, or empty for none.
func CheckCallback(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(raw) > MaxCallback || u.User != nil {
		return "", fmt.Errorf("the callback must be an http or https address of at most %d characters, with no password in it", MaxCallback)
	}
	return raw, nil
}

// Load is every registered source. A missing file is none.
func (r Registry) Load() ([]Source, error) {
	raw, err := os.ReadFile(r.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		Sources []Source `json:"sources"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("the sources file %s cannot be read: %w", r.Path, err)
	}
	return doc.Sources, nil
}

// Get is the source named name.
func (r Registry) Get(name string) (Source, error) {
	all, err := r.Load()
	if err != nil {
		return Source{}, err
	}
	for _, s := range all {
		if s.Name == name {
			return s, nil
		}
	}
	return Source{}, ErrNotFound
}

// Match is the source whose token is token. A read failure matches nothing:
// the file failing closed refuses every source token and leaves the
// factory's own access untouched.
func (r Registry) Match(token string) (Source, bool) {
	if token == "" {
		return Source{}, false
	}
	all, err := r.Load()
	if err != nil {
		return Source{}, false
	}
	for _, s := range all {
		if s.Matches(token) {
			return s, true
		}
	}
	return Source{}, false
}

// Add registers name and mints its token and signing secret.
func (r Registry) Add(name, callback string, trust factory.Tier, now time.Time) (Minted, error) {
	name = strings.TrimSpace(name)
	if err := CheckName(name); err != nil {
		return Minted{}, err
	}
	trust, err := CheckTrust(trust)
	if err != nil {
		return Minted{}, err
	}
	if callback, err = CheckCallback(callback); err != nil {
		return Minted{}, err
	}
	token, err := secret("cft_")
	if err != nil {
		return Minted{}, err
	}
	sign, err := secret("whsec_")
	if err != nil {
		return Minted{}, err
	}
	sum := sha256.Sum256([]byte(token))
	row := Source{Name: name, TokenHash: hex.EncodeToString(sum[:]), Secret: sign, Callback: callback, Trust: trust, Created: now.UTC()}
	err = r.change(func(all []Source) ([]Source, error) {
		if slices.ContainsFunc(all, func(s Source) bool { return s.Name == name }) {
			return nil, ErrExists
		}
		return append(all, row), nil
	})
	if err != nil {
		return Minted{}, err
	}
	return Minted{Public: row.Public(), Token: token, Secret: sign}, nil
}

// Revoke forgets name: its token stops working at once (the file is read on
// every request) and its webhooks still waiting are dropped.
func (r Registry) Revoke(name string) error {
	return r.change(func(all []Source) ([]Source, error) {
		i := slices.IndexFunc(all, func(s Source) bool { return s.Name == name })
		if i < 0 {
			return nil, ErrNotFound
		}
		return slices.Delete(all, i, i+1), nil
	})
}

// change rewrites the file under its lock, atomically and privately.
func (r Registry) change(edit func([]Source) ([]Source, error)) error {
	if err := os.MkdirAll(filepath.Dir(r.Path), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(r.Path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return fmt.Errorf("the sources file is busy: %w", err)
	}
	defer func() { _ = filelock.Unlock(lock) }()
	all, err := r.Load()
	if err != nil {
		return err
	}
	all, err = edit(all)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(struct {
		Sources []Source `json:"sources"`
	}{all}, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(r.Path, body)
}

// writePrivate writes body to path through a temporary file in the same
// folder, so a reader sees the old file or the new one and never half.
func writePrivate(path string, body []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".trigger-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func secret(prefix string) (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("could not mint a secret: %w", err)
	}
	return prefix + hex.EncodeToString(b[:]), nil
}

// Cap is the trust an entry from s is filed with: what it asked for, never
// above the source's own. An entry that asks for nothing takes the source's.
func (s Source) Cap(asked factory.Tier) factory.Tier {
	if asked == "" || rank[asked] > rank[s.Trust] {
		return s.Trust
	}
	return asked
}

var rank = map[factory.Tier]int{factory.TierStranger: 0, factory.TierCollab: 1, factory.TierOwner: 2}
