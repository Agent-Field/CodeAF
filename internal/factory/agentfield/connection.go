package agentfield

import (
	"context"
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
// A connection is settings and secrets, kept in two places. The control
// plane's address, the node id, the address the control plane reaches the
// factory on (optional), the address the node listens on and the trust
// ceiling are the factory's settings, in agentfield.json beside the floor's
// other settings. The control plane's API key and its INTERNAL TOKEN are
// kept THE WAY THE GITHUB TOKEN IS KEPT: by the desktop app when the app
// started this process, otherwise in the profile, never in the factory's
// folder and never in an answer.
//
// TWO CARRIERS, ONE NODE. The node always dials the control plane's link
// (link.go): one WebSocket it opens and keeps, so dispatches and cancels
// reach a factory that has NO address the control plane can dial — a laptop
// behind NAT. With no base_url the node binds nothing at all. With a
// base_url it also listens, for a control plane that offers no link, and
// THE INTERNAL TOKEN IS HOW IT KNOWS AN HTTP CALL IS THE CONTROL PLANE'S:
// the control plane sends it as Authorization: Bearer on every dispatch and
// cancel, and the node refuses a reasoner call without it.
//
// THE OLD ADDRESS SECRET (`/n/<secret>`, from before the trigger dispatcher
// sent the bearer) is still answered for one release when it is kept, so a
// control plane that has the old base_url stored still reaches a node that
// restarted on this build. A fresh connect forgets it.

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
	// BaseURL is where the control plane reaches the factory over HTTP, such
	// as http://10.0.0.5:8765. EMPTY MEANS LINK ONLY: the node binds no port
	// and is reached only down the link it opens.
	BaseURL string `json:"base_url,omitempty"`
	// Listen is the address the node's listener binds, such as 0.0.0.0:8765;
	// empty with no base_url.
	Listen string `json:"listen,omitempty"`
	// Trust is the most an entry from this control plane is trusted.
	Trust factory.Tier `json:"trust"`
}

// Status is a connection as a person reads it. It never carries a secret.
type Status struct {
	Connected bool
	Settings
	// KeyKept says an API key is kept for the control plane.
	KeyKept bool
	// TokenKept says the control plane's internal token is kept, without
	// which the node refuses every HTTP reasoner call.
	TokenKept bool
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
	if strings.TrimSpace(s.BaseURL) != "" {
		if s.BaseURL, err = cleanURL("the address AgentField reaches this factory on", s.BaseURL); err != nil {
			return s, err
		}
	} else {
		s.BaseURL = ""
	}
	if s.NodeID = strings.ToLower(strings.TrimSpace(s.NodeID)); s.NodeID == "" {
		s.NodeID = DefaultNodeID
	}
	if !reNodeID.MatchString(s.NodeID) {
		return s, errors.New("the node id is a lowercase word, such as codeaf-factory")
	}
	if s.BaseURL == "" {
		// LINK ONLY: nothing listens.
		s.Listen = ""
	} else {
		if s.Listen = strings.TrimSpace(s.Listen); s.Listen == "" {
			s.Listen, err = listenFor(s.BaseURL)
			if err != nil {
				return s, err
			}
		}
		if _, port, err := net.SplitHostPort(s.Listen); err != nil || port == "" {
			return s, errors.New("the listen address is host:port, such as 0.0.0.0:8765")
		}
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

// Secrets are what a connection keeps besides its settings.
type Secrets struct {
	// Key is the control plane's API key; empty for one that takes none.
	Key string
	// Token is the control plane's internal token, the bearer an HTTP
	// dispatch must carry. Empty refuses every HTTP reasoner call.
	Token string
	// Legacy is a node secret kept from before the bearer (read only).
	Legacy string
}

// Keep writes the settings and the secrets: key and internal token (either
// empty for a control plane that has none). A keep that fails keeps nothing.
func (c *Connection) Keep(s Settings, key, token string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := keepSecrets(c.Profile, strings.TrimSpace(key), strings.TrimSpace(token)); err != nil {
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
	s, sec, ok := c.Kept()
	if !ok {
		return Status{}
	}
	return Status{Connected: true, Settings: s, KeyKept: sec.Key != "", TokenKept: sec.Token != ""}
}

// Kept is the kept settings and secrets, and whether there is a connection
// at all (settings with a control plane address).
func (c *Connection) Kept() (Settings, Secrets, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var s Settings
	if c.Store == nil || readJSON(c.path(), &s) != nil || s.URL == "" {
		return Settings{}, Secrets{}, false
	}
	sec := Secrets{Key: config.AgentFieldKeyAt(c.Profile), Token: config.AgentFieldInternalTokenAt(c.Profile), Legacy: config.AgentFieldNodeSecretAt(c.Profile)}
	redact.Register(sec.Key)
	redact.Register(sec.Token)
	redact.Register(sec.Legacy)
	return s, sec, true
}

// Client is a control plane client over the kept connection, or false.
func (c *Connection) Client() (*Client, Settings, bool) {
	s, sec, ok := c.Kept()
	if !ok {
		return nil, Settings{}, false
	}
	return &Client{URL: s.URL, Key: sec.Key, HTTP: c.HTTP, Owner: s.NodeID}, s, true
}

// Check asks the control plane at url, with key, for its source catalog: the
// one call that proves both the address and the key before anything is kept.
func Check(ctx context.Context, httpc *http.Client, url, key string) ([]Source, error) {
	return (&Client{URL: url, Key: key, HTTP: httpc}).Sources(ctx)
}

func (c *Connection) path() string { return filepath.Join(c.Store.Root(), settingsFile) }

// keepSecrets keeps both where the GitHub token is kept: handed to the app to
// keep in app mode (config.AppCredentials.Keep), otherwise in the profile. Any
// legacy node secret is forgotten.
func keepSecrets(profile, key, token string) error {
	redact.Register(key)
	redact.Register(token)
	if keys := config.AppCredentialSource(); keys != nil {
		for provider, v := range map[string]string{config.AppAgentFieldProvider: key, config.AppAgentFieldTokenProvider: token} {
			if v != "" {
				keys.Keep(provider, v)
			} else {
				keys.Forget(provider)
			}
		}
		keys.Forget(config.AppAgentFieldNodeProvider)
		return nil
	}
	return config.WriteAgentFieldSecrets(profile, key, token)
}

func forgetSecrets(profile string) error {
	if keys := config.AppCredentialSource(); keys != nil {
		keys.Forget(config.AppAgentFieldProvider)
		keys.Forget(config.AppAgentFieldNodeProvider)
		keys.Forget(config.AppAgentFieldTokenProvider)
		return nil
	}
	if config.AgentFieldKeyAt(profile) == "" && config.AgentFieldNodeSecretAt(profile) == "" && config.AgentFieldInternalTokenAt(profile) == "" {
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
