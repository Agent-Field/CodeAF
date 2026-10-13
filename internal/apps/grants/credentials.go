package grants

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/config"
	accounts "github.com/Agent-Field/codeaf/internal/connect"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"golang.org/x/oauth2"
)

// Connections is the narrow, replaceable boundary around internal/connect.
// SignIn receives a fresh workspace id so it cannot overwrite a personal account.
type Connections interface {
	Personal(context.Context, string) ([]bridge.Connection, error)
	PersonalCredential(context.Context, string, string) (bridge.GrantCredential, error)
	SignIn(context.Context, string, string) (bridge.Connection, error)
	WorkspaceCredential(context.Context, string, string) (bridge.GrantCredential, error)
	SignOut(context.Context, string, string) error
}

type localConnections struct{ home string }

// The existing connect store keeps these fields private. This read adapter is
// confined here until internal/connect exports a credential transfer seam.
type connectEntry struct {
	Account string        `json:"account"`
	Auth    string        `json:"auth"`
	Key     string        `json:"key"`
	KeyEnv  string        `json:"keyEnv"`
	Keys    *oauth2.Token `json:"keys"`
	Since   string        `json:"since"`
}

func serviceID(service string) string {
	if service == "gmail" || service == "calendar" {
		return "google"
	}
	return service
}
func personalEntries(dir string) (map[string]connectEntry, error) {
	if source := config.AppCredentialSource(); source != nil {
		entries := map[string]connectEntry{}
		for _, plug := range accounts.Registered() {
			service := plug.Service()
			raw, _ := source.Key(service.ID)
			if raw == "" {
				continue
			}
			entry := connectEntry{}
			if strings.HasPrefix(raw, "{") {
				if json.Unmarshal([]byte(raw), &entry) != nil {
					return nil, errors.New("Cannot read connections")
				}
			} else if service.Auth == accounts.AuthKey {
				entry.Auth = accounts.AuthKey
				entry.Key = raw
			} else {
				entry.Keys = &oauth2.Token{AccessToken: raw}
			}
			entries[service.ID] = entry
		}
		return entries, nil
	}
	if config.AppMode() {
		return map[string]connectEntry{}, nil
	}
	return readConnections(dir)
}
func readConnections(dir string) (map[string]connectEntry, error) {
	raw, err := os.ReadFile(filepath.Join(dir, accounts.StoreFileName))
	if os.IsNotExist(err) {
		return map[string]connectEntry{}, nil
	}
	if err != nil {
		return nil, errors.New("Cannot read connections")
	}
	entries := map[string]connectEntry{}
	if json.Unmarshal(raw, &entries) != nil {
		return nil, errors.New("Cannot read connections")
	}
	return entries, nil
}
func (e connectEntry) credential() bridge.GrantCredential {
	if e.Auth == accounts.AuthKey {
		key := e.Key
		if e.KeyEnv != "" {
			key = os.Getenv(e.KeyEnv)
		}
		return bridge.GrantCredential{Kind: "grant", Token: key}
	}
	if e.Keys == nil {
		return bridge.GrantCredential{}
	}
	if e.Keys.RefreshToken != "" {
		return bridge.GrantCredential{Kind: "refresh", Token: e.Keys.RefreshToken}
	}
	c := bridge.GrantCredential{Kind: "grant", Token: e.Keys.AccessToken}
	if !e.Keys.Expiry.IsZero() {
		c.ExpiresAt = e.Keys.Expiry.UTC().Format(time.RFC3339)
	}
	return c
}
func (l *localConnections) Personal(_ context.Context, member string) ([]bridge.Connection, error) {
	if !ownDesktop(svc.Caller{Member: member}) {
		return nil, bridge.Errorf(bridge.CodeDenied, "These accounts belong to another person")
	}
	entries, err := personalEntries(l.home)
	if err != nil {
		return nil, err
	}
	return entriesList(entries, "personal"), nil
}
func entriesList(entries map[string]connectEntry, level string) []bridge.Connection {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	slices.Sort(names)
	out := []bridge.Connection{}
	for _, name := range names {
		e := entries[name]
		c := e.credential()
		if c.Token == "" {
			continue
		}
		services := []string{name}
		if name == "google" {
			services = []string{"gmail", "calendar"}
		}
		status := "connected"
		if expired(c, time.Now()) {
			status = "expired"
		}
		for _, service := range services {
			out = append(out, bridge.Connection{ID: name, Service: service, Account: e.Account, Level: level, Status: status, Since: e.Since})
		}
	}
	return out
}
func credentialAt(dir, service string) (bridge.GrantCredential, error) {
	entries, err := readConnections(dir)
	if err != nil {
		return bridge.GrantCredential{}, err
	}
	c := entries[serviceID(service)].credential()
	if !validCredential(&c) {
		return c, bridge.Errorf(bridge.CodeNotFound, "Connect this account first")
	}
	return c, nil
}
func (l *localConnections) PersonalCredential(_ context.Context, member, service string) (bridge.GrantCredential, error) {
	if !ownDesktop(svc.Caller{Member: member}) {
		return bridge.GrantCredential{}, bridge.Errorf(bridge.CodeDenied, "These accounts belong to another person")
	}
	entries, err := personalEntries(l.home)
	if err != nil {
		return bridge.GrantCredential{}, err
	}
	c := entries[serviceID(service)].credential()
	if !validCredential(&c) {
		return c, bridge.Errorf(bridge.CodeNotFound, "Connect this account first")
	}
	return c, nil
}
func (l *localConnections) workspaceDir(id string) string {
	return filepath.Join(l.home, "workspace-connect", id)
}
func (l *localConnections) WorkspaceCredential(_ context.Context, id, service string) (bridge.GrantCredential, error) {
	return credentialAt(l.workspaceDir(id), service)
}
func (l *localConnections) manager(dir string) (*accounts.Manager, error) {
	creds := map[string]accounts.ClientCredential{}
	if id, secret := config.GoogleOAuthClientAt(l.home); strings.TrimSpace(id) != "" && strings.TrimSpace(secret) != "" {
		creds["google"] = accounts.ClientCredential{ID: id, Secret: secret}
	}
	if id := config.SlackOAuthClientAt(l.home); id != "" {
		creds["slack"] = accounts.ClientCredential{ID: id, Public: true}
	}
	return accounts.NewManager(dir, creds)
}
func (l *localConnections) SignIn(ctx context.Context, id, service string) (bridge.Connection, error) {
	out := bridge.Connection{}
	manager, err := l.manager(l.workspaceDir(id))
	if err != nil {
		return out, errors.New("Cannot open workspace sign-in")
	}
	flow, err := manager.BeginAuth(ctx, serviceID(service), "")
	if err != nil {
		return out, errors.New("Cannot start workspace sign-in")
	}
	defer flow.Cancel()
	// The desktop receives the loopback URL as an event before this method waits.
	if publish, ok := ctx.Value(authPublisherKey{}).(func(string)); ok {
		publish(flow.URL())
	} else {
		return out, bridge.Errorf(bridge.CodeInvalid, "A desktop sign-in listener is required")
	}
	status, err := flow.Wait(ctx)
	if err != nil {
		return out, errors.New("Workspace sign-in did not finish")
	}
	return bridge.Connection{ID: id, Service: service, Account: status.Account, Level: "workspace", Status: "connected", Since: time.Now().UTC().Format(time.RFC3339)}, nil
}
func (l *localConnections) SignOut(_ context.Context, id, service string) error {
	manager, err := l.manager(l.workspaceDir(id))
	if err != nil {
		return errors.New("Cannot open workspace connection")
	}
	return manager.Disconnect(serviceID(service))
}

type authPublisherKey struct{}

// seal binds the encrypted bytes to the member, app, space and service so copying
// ciphertext into another grant never gives that grant access to the account.
func (s *Service) seal(c bridge.GrantCredential, binding []byte) ([]byte, error) {
	aead, err := s.cipher()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, errors.New("Cannot encrypt connection")
	}
	return aead.Seal(nonce, nonce, encodeCredential(c), binding), nil
}
func (s *Service) unseal(raw, binding []byte) (bridge.GrantCredential, error) {
	c := bridge.GrantCredential{}
	aead, err := s.cipher()
	if err != nil {
		return c, err
	}
	n := aead.NonceSize()
	if len(raw) < n {
		return c, errors.New("Cannot decrypt connection")
	}
	plain, err := aead.Open(nil, raw[:n], raw[n:], binding)
	if err != nil || json.Unmarshal(plain, &c) != nil {
		return c, errors.New("Cannot decrypt connection")
	}
	return c, nil
}
func grantBinding(member, app, space, service string) []byte {
	raw, _ := json.Marshal([]string{member, app, space, service})
	return raw
}
func (s *Service) cipher() (cipher.AEAD, error) {
	key, err := s.encryptionKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("Cannot open connection encryption")
	}
	return cipher.NewGCM(block)
}
func (s *Service) encryptionKey() ([]byte, error) {
	lock, err := os.OpenFile(filepath.Join(s.home, ".connect-apps-key.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, errors.New("Cannot open connection encryption")
	}
	defer lock.Close()
	if filelock.Lock(lock, true, false) != nil {
		return nil, errors.New("Cannot open connection encryption")
	}
	defer func() { _ = filelock.Unlock(lock) }()
	path := filepath.Join(s.home, ".connect-apps-key")
	key, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, errors.New("Cannot create connection encryption")
		}
		// Atomic replacement prevents a killed writer from leaving a partial key.
		f, createErr := os.CreateTemp(s.home, ".connect-key-*")
		if createErr != nil {
			return nil, errors.New("Cannot create connection encryption")
		}
		defer os.Remove(f.Name())
		if err = f.Chmod(0600); err == nil {
			_, err = f.Write(key)
		}
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(f.Name(), path)
		}
	}
	if err != nil || len(key) != 32 {
		return nil, errors.New("Cannot open connection encryption")
	}
	return key, nil
}
