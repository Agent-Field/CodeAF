package agentfield

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// ── HOW A FACTORY IS CONNECTED TO A CONTROL PLANE ───────────────────────────
//
// A connection is settings and two secrets, kept in two places. The control
// plane's address, the node id, the address the control plane reaches the
// factory on, the address the node listens on and the trust ceiling are the
// factory's settings, in agentfield.json beside the floor's other settings.
// The control plane's API key and the node's ADDRESS SECRET are kept THE WAY
// THE GITHUB TOKEN IS KEPT: by the desktop app when the app started this
// process, otherwise in the profile, never in the factory's folder and never
// in an answer.
//
// THE ADDRESS SECRET IS HOW THE NODE KNOWS A CALL IS THE CONTROL PLANE'S. The
// control plane dispatches a trigger to `{base_url}/reasoners/{name}` with no
// credential of its own (its trigger dispatcher sets none; only a direct
// execute carries the optional internal token), so a node that demanded a
// bearer would refuse every trigger. What the control plane DOES keep is the
// path of the base_url it was given, on every call it makes to the node. So
// the node registers `<base_url>/n/<secret>` — 32 random bytes — and answers
// nothing outside that prefix. Anyone who can read the control plane's node
// list with its API key can see it; anyone who can do that can also add a
// trigger, so the secret asks nothing of them they do not already have.

// settingsFile lives in the floor's folder. It is not a number, so the store
// never reads it as a job.
const settingsFile = "agentfield.json"

// DefaultNodeID is the node a factory registers as unless told otherwise.
const DefaultNodeID = "codeaf-factory"

// ErrNotKept is what a connect or disconnect is (errors.Is) when the control
// plane said yes but this machine could not keep or forget the answer.
var ErrNotKept = errors.New("codeaf could not keep the AgentField connection")

// Settings are the factory's own record of its connection. They hold no
// secret.
type Settings struct {
	// URL is the control plane, such as http://agentfield.local:8080.
	URL string `json:"url"`
	// NodeID is the node the factory registers as.
	NodeID string `json:"node_id"`
	// BaseURL is where the control plane reaches the factory, such as
	// http://10.0.0.5:8765. It is registered with the secret after it.
	BaseURL string `json:"base_url"`
	// Listen is the address the node's listener binds, such as 0.0.0.0:8765.
	Listen string `json:"listen"`
	// Trust is the most an entry from this control plane is trusted.
	Trust factory.Tier `json:"trust"`
}

// Status is a connection as a person reads it. It never carries a secret.
type Status struct {
	Connected bool
	Settings
	// KeyKept says an API key is kept for the control plane.
	KeyKept bool
}

// Connection is one factory's connection: Store is the floor (its folder
// keeps the settings), Profile the profile the secrets are kept in, and HTTP
// the transport every client it makes uses (nil is http.DefaultClient).
type Connection struct {
	Store   *store.Store
	Profile string
	HTTP    *http.Client

	mu sync.Mutex
}

// reNodeID is a node id the control plane and a URL path both take.
var reNodeID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// Clean checks s and fills its defaults: a node id, a listen address taken
// from the base_url's port, and the collaborator ceiling. It asks nobody.
func Clean(s Settings) (Settings, error) {
	var err error
	if s.URL, err = cleanURL("the AgentField address", s.URL); err != nil {
		return s, err
	}
	if s.BaseURL, err = cleanURL("the address AgentField reaches this factory on", s.BaseURL); err != nil {
		return s, err
	}
	if s.NodeID = strings.ToLower(strings.TrimSpace(s.NodeID)); s.NodeID == "" {
		s.NodeID = DefaultNodeID
	}
	if !reNodeID.MatchString(s.NodeID) {
		return s, errors.New("the node id is a lowercase word, such as codeaf-factory")
	}
	if s.Listen = strings.TrimSpace(s.Listen); s.Listen == "" {
		s.Listen, err = listenFor(s.BaseURL)
		if err != nil {
			return s, err
		}
	}
	if _, port, err := net.SplitHostPort(s.Listen); err != nil || port == "" {
		return s, errors.New("the listen address is host:port, such as 0.0.0.0:8765")
	}
	switch s.Trust = factory.Tier(strings.ToLower(strings.TrimSpace(string(s.Trust)))); s.Trust {
	case "":
		s.Trust = factory.TierCollab
	case factory.TierOwner, factory.TierCollab, factory.TierStranger:
	default:
		return s, errors.New("trust must be owner, collaborator or stranger")
	}
	return s, nil
}

// cleanURL is an http or https address, scheme and host and any path, with
// no query, no user and no trailing slash.
func cleanURL(what, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%s is required, such as http://10.0.0.5:8080", what)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%s is an http or https address, such as http://10.0.0.5:8080", what)
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+u.EscapedPath(), "/"), nil
}

// listenFor is the listen address a base_url implies: its port, on loopback
// when the base_url is loopback, else on every interface.
func listenFor(base string) (string, error) {
	u, _ := url.Parse(base)
	port := u.Port()
	if port == "" {
		return "", errors.New("the address AgentField reaches this factory on needs a port, such as http://10.0.0.5:8765, or say which address to listen on")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
		return net.JoinHostPort("127.0.0.1", port), nil
	}
	return net.JoinHostPort("0.0.0.0", port), nil
}

// Keep writes the settings and the secrets: key (empty for a control plane
// that takes none) and a fresh address secret. A keep that fails keeps
// nothing.
func (c *Connection) Keep(s Settings, key string) error {
	secret, err := newSecret()
	if err != nil {
		return ErrNotKept
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := keepSecrets(c.Profile, strings.TrimSpace(key), secret); err != nil {
		return fmt.Errorf("%w: the key was not kept", ErrNotKept)
	}
	if err := writePrivate(c.path(), s); err != nil {
		_ = forgetSecrets(c.Profile)
		return ErrNotKept
	}
	return nil
}

// Disconnect forgets the settings and the secrets. Forgetting a connection
// that is not there is not an error.
func (c *Connection) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	secretsErr := forgetSecrets(c.Profile)
	if err := os.Remove(c.path()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: its settings were not forgotten", ErrNotKept)
	}
	if secretsErr != nil {
		return fmt.Errorf("%w: the key was not forgotten", ErrNotKept)
	}
	return nil
}

// Status is the connection as it is kept now.
func (c *Connection) Status() Status {
	s, key, secret, ok := c.Kept()
	if !ok && s.URL == "" {
		return Status{}
	}
	return Status{Connected: ok && secret != "", Settings: s, KeyKept: key != ""}
}

// Kept is the kept settings, key and address secret, and whether there is a
// connection at all (settings and a secret).
func (c *Connection) Kept() (Settings, string, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var s Settings
	if c.Store == nil || readJSON(c.path(), &s) != nil || s.URL == "" || s.BaseURL == "" {
		return Settings{}, "", "", false
	}
	key := config.AgentFieldKeyAt(c.Profile)
	secret := config.AgentFieldNodeSecretAt(c.Profile)
	redact.Register(key)
	redact.Register(secret)
	return s, key, secret, secret != ""
}

// Client is a control plane client over the kept connection, or false.
func (c *Connection) Client() (*Client, Settings, bool) {
	s, key, _, ok := c.Kept()
	if !ok {
		return nil, Settings{}, false
	}
	return &Client{URL: s.URL, Key: key, HTTP: c.HTTP}, s, true
}

// Check asks the control plane at url, with key, for its source catalog: the
// one call that proves both the address and the key before anything is kept.
func Check(ctx context.Context, httpc *http.Client, url, key string) ([]Source, error) {
	return (&Client{URL: url, Key: key, HTTP: httpc}).Sources(ctx)
}

func (c *Connection) path() string { return filepath.Join(c.Store.Root(), settingsFile) }

func newSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// keepSecrets keeps both where the GitHub token is kept: with the app's keys
// in app mode, otherwise in the profile.
func keepSecrets(profile, key, secret string) error {
	redact.Register(key)
	redact.Register(secret)
	if keys := config.AppCredentialSource(); keys != nil {
		if key != "" {
			keys.Set(config.AppAgentFieldProvider, key)
		} else {
			keys.Remove(config.AppAgentFieldProvider)
		}
		keys.Set(config.AppAgentFieldNodeProvider, secret)
		return nil
	}
	return config.WriteAgentFieldSecrets(profile, key, secret)
}

func forgetSecrets(profile string) error {
	if keys := config.AppCredentialSource(); keys != nil {
		keys.Remove(config.AppAgentFieldProvider)
		keys.Remove(config.AppAgentFieldNodeProvider)
		return nil
	}
	if config.AgentFieldKeyAt(profile) == "" && config.AgentFieldNodeSecretAt(profile) == "" {
		return nil
	}
	return config.ForgetAgentFieldSecrets(profile)
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// writePrivate writes v as JSON, owner-readable only, replacing path whole.
func writePrivate(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
