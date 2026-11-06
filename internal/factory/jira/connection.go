package jira

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
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
)

// ── HOW A FACTORY IS CONNECTED TO JIRA ──────────────────────────────────────
//
// A connection is three things kept in two places. The site, the account's
// email, the JQL and the account's time zone are the factory's settings, in
// jira.json beside the floor's other settings. The API token is a secret and
// is kept THE WAY THE GITHUB TOKEN IS KEPT: by the desktop app when the app
// started this process, otherwise in the profile (config.KeyJiraToken), never
// in the factory's folder and never in an answer. A factory is connected only
// while both halves are there, and NOTHING READS JIRA WHILE IT IS NOT: no
// feed, no loop, no comment.

// settingsFile and healthFile live in the floor's folder. Neither is a
// number, so the store never reads either as a job.
const (
	settingsFile = "jira.json"
	healthFile   = "jira-status.json"
)

// ErrNotKept is what a connect or disconnect is (errors.Is) when Jira said
// yes but this machine could not keep or forget the answer.
var ErrNotKept = errors.New("codeaf could not keep the Jira connection")

// MaxJQL is the longest query a person may connect with, in bytes.
const MaxJQL = 2000

// Settings are the factory's own record of its Jira connection. They hold no
// secret.
type Settings struct {
	Site  string `json:"site"`
	Email string `json:"email"`
	JQL   string `json:"jql"`
	// Zone is the account's time zone as /myself answered it.
	Zone string `json:"time_zone,omitempty"`
	// Account is the connected account's display name.
	Account string `json:"account,omitempty"`
}

// Health is what the last reads said.
type Health struct {
	LastRead  time.Time `json:"last_read,omitzero"`
	LastError string    `json:"last_error,omitempty"`
}

// Status is a connection as a person reads it. It never carries the token.
type Status struct {
	Connected bool
	Settings
	Health
}

// Connection is one factory's Jira connection: Root is the floor's folder,
// Profile the profile the token is kept in. HTTP is the transport every
// client it makes uses; nil is http.DefaultClient.
type Connection struct {
	Root    string
	Profile string
	HTTP    *http.Client

	mu sync.Mutex
}

// Connect checks the credentials against /myself and the JQL with one
// search, then keeps the token and the settings. A check that fails keeps
// nothing, so a connection is never half made.
func (c *Connection) Connect(ctx context.Context, site, email, token, jql string) (Status, error) {
	site, err := NormalizeSite(site)
	if err != nil {
		return Status{}, err
	}
	email, token, jql = strings.TrimSpace(email), strings.TrimSpace(token), strings.TrimSpace(jql)
	switch {
	case email == "" || !strings.Contains(email, "@"):
		return Status{}, errors.New("the email is the Jira account's sign-in email")
	case token == "":
		return Status{}, errors.New("an API token is needed: make one at id.atlassian.com, under Security, API tokens")
	case strings.ContainsAny(token, " \t\r\n"):
		return Status{}, errors.New("an API token is one word")
	case len(jql) > MaxJQL:
		return Status{}, errors.New("the JQL is longer than codeaf keeps")
	}
	first, err := Query(jql, "")
	if err != nil {
		return Status{}, err
	}
	client := &Client{Site: site, Email: email, Token: token, HTTP: c.HTTP}
	me, err := client.Myself(ctx)
	if err != nil {
		return Status{}, err
	}
	if _, err := client.Search(ctx, first, ""); err != nil {
		return Status{}, err
	}
	kept := Settings{Site: site, Email: email, JQL: jql, Zone: me.TimeZone, Account: me.DisplayName}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := keepToken(c.Profile, token); err != nil {
		return Status{}, fmt.Errorf("%w: the token was not kept", ErrNotKept)
	}
	if err := writePrivate(filepath.Join(c.Root, settingsFile), kept); err != nil {
		_ = forgetToken(c.Profile)
		return Status{}, ErrNotKept
	}
	_ = os.Remove(filepath.Join(c.Root, healthFile))
	return Status{Connected: true, Settings: kept}, nil
}

// Disconnect forgets the token and the settings. Disconnecting a factory
// that is not connected is not an error.
func (c *Connection) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	tokenErr := forgetToken(c.Profile)
	for _, name := range []string{settingsFile, healthFile} {
		if err := os.Remove(filepath.Join(c.Root, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: its settings were not forgotten", ErrNotKept)
		}
	}
	if tokenErr != nil {
		return fmt.Errorf("%w: the token was not forgotten", ErrNotKept)
	}
	return nil
}

// Status is the connection as it is kept now.
func (c *Connection) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	settings, ok := c.settings()
	if !ok {
		return Status{}
	}
	var health Health
	_ = readJSON(filepath.Join(c.Root, healthFile), &health)
	return Status{Connected: config.JiraTokenAt(c.Profile) != "", Settings: settings, Health: health}
}

// Feed is the feed over the kept connection, and false when the factory is
// not connected: then there is nothing to read and no loop may start.
func (c *Connection) Feed() (*Feed, bool) {
	client, settings, ok := c.client()
	if !ok {
		return nil, false
	}
	return &Feed{Client: client, JQL: settings.JQL, Zone: zoneOf(settings.Zone), Observe: c.observe}, true
}

// Replier answers through whatever connection is kept when a job's moment
// arrives, so it is registered once and a disconnect silences it: a factory
// that is no longer connected comments nothing.
func (c *Connection) Replier() inbox.Replier { return liveReplier{c} }

type liveReplier struct{ c *Connection }

func (r liveReplier) Reply(ctx context.Context, key string, ev inbox.Event) error {
	client, _, ok := r.c.client()
	if !ok {
		return errors.New("Jira is not connected")
	}
	return Replier{Client: client}.Reply(ctx, key, ev)
}

// client is a client over the kept connection, or false.
func (c *Connection) client() (*Client, Settings, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	settings, ok := c.settings()
	if !ok {
		return nil, Settings{}, false
	}
	token := config.JiraTokenAt(c.Profile)
	if token == "" {
		return nil, Settings{}, false
	}
	return &Client{Site: settings.Site, Email: settings.Email, Token: token, HTTP: c.HTTP}, settings, true
}

// settings reads jira.json; the caller holds mu.
func (c *Connection) settings() (Settings, bool) {
	var s Settings
	if strings.TrimSpace(c.Root) == "" || readJSON(filepath.Join(c.Root, settingsFile), &s) != nil {
		return Settings{}, false
	}
	return s, s.Site != "" && s.Email != "" && s.JQL != ""
}

// observe records one read's outcome: a good read moves last_read and clears
// the error; a failed one keeps last_read and says why.
func (c *Connection) observe(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.settings(); !ok {
		return // disconnected while the read was out: record nothing
	}
	path := filepath.Join(c.Root, healthFile)
	var h Health
	_ = readJSON(path, &h)
	if err == nil {
		h = Health{LastRead: time.Now().UTC()}
	} else {
		h.LastError = err.Error()
	}
	_ = writePrivate(path, h)
}

// NormalizeSite is a site's base URL, scheme and host only: acme.atlassian.net
// and https://acme.atlassian.net/jira/your-work both read as
// https://acme.atlassian.net. Plain http is accepted only for this machine
// (a stub in a test), because basic auth over http sends the token in the
// clear.
func NormalizeSite(site string) (string, error) {
	site = strings.TrimSpace(site)
	if site == "" {
		return "", errors.New("the Jira site is its web address, such as https://acme.atlassian.net")
	}
	if !strings.Contains(site, "://") {
		site = "https://" + site
	}
	u, err := url.Parse(site)
	if err != nil || u.Host == "" || u.User != nil {
		return "", errors.New("the Jira site is its web address, such as https://acme.atlassian.net")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !loopback(u.Hostname()) {
			return "", errors.New("the Jira site must use https")
		}
	default:
		return "", errors.New("the Jira site must use https")
	}
	return u.Scheme + "://" + u.Host, nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// zoneOf is the named zone, or nil when it is not known on this machine
// (see [Feed.Zone]).
func zoneOf(name string) *time.Location {
	if name == "" {
		return nil
	}
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return nil
}

// keepToken keeps the token where the GitHub token is kept: with the app's
// keys in app mode, otherwise in the profile.
func keepToken(profile, token string) error {
	if keys := config.AppCredentialSource(); keys != nil {
		keys.Set(config.AppJiraProvider, token)
		return nil
	}
	return config.WriteJiraToken(profile, token)
}

func forgetToken(profile string) error {
	if keys := config.AppCredentialSource(); keys != nil {
		keys.Remove(config.AppJiraProvider)
		return nil
	}
	if config.JiraTokenAt(profile) == "" {
		return nil
	}
	return config.ForgetJiraToken(profile)
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
