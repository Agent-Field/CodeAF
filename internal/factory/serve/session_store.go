package serve

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// sessionRow keeps public session facts beside an authenticated encryption of
// GitHub's credentials. The hash is also the encryption's associated data, so
// moving a ciphertext to another credential cannot grant that caller access.
type sessionRow struct {
	Expires       time.Time    `json:"expires"`
	Created       time.Time    `json:"created"`
	LastSeen      time.Time    `json:"last_seen"`
	TokenID       string       `json:"token_id,omitempty"`
	ID            string       `json:"id"`
	Device        string       `json:"device"`
	Profile       *api.Profile `json:"profile,omitempty"`
	GitHub        []byte       `json:"github,omitempty"`
	GitHubExpired bool         `json:"github_expired,omitempty"`
}

type githubCredentials struct {
	Access         string    `json:"access_token"`
	Refresh        string    `json:"refresh_token,omitempty"`
	Expires        time.Time `json:"expires"`
	RefreshExpires time.Time `json:"refresh_expires"`
}

type sessionDocument struct {
	Version  int                   `json:"version"`
	Sessions map[string]sessionRow `json:"sessions"`
}

func sessionHash(credential string) string {
	sum := sha256.Sum256([]byte(credential))
	return hex.EncodeToString(sum[:])
}

func profileHandle(s accessSession) string {
	if s.profile == nil {
		return ""
	}
	return s.profile.Handle
}

// loadSessions reads the host's private file before accepting credentials.
// A corrupt or unreadable file fails session access closed, without replacing
// it with an empty file. A host without a home keeps ephemeral test sessions.
func (h *Handler) loadSessions() error {
	root := h.config.Root
	if h.config.TokensPath != "" {
		root = filepath.Dir(h.config.TokensPath)
	} else if root == "" && h.config.Store != nil {
		root = h.config.Store.Root()
	}
	if root == "" {
		return nil
	}
	h.sessions.path = filepath.Join(root, "sessions.json")
	body, err := os.ReadFile(h.sessions.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(h.sessions.path, 0o600); err != nil {
		return err
	}
	var doc sessionDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	if doc.Version != 1 {
		return errors.New("unsupported factory session file")
	}
	loaded := make(map[string]accessSession)
	pruned := false
	for hash, row := range doc.Sessions {
		raw, err := hex.DecodeString(hash)
		if err != nil || len(raw) != sha256.Size || row.ID == "" || row.Expires.IsZero() {
			return errors.New("invalid factory session file")
		}
		s := accessSession{expires: row.Expires, created: row.Created, lastSeen: row.LastSeen,
			tokenID: row.TokenID, id: row.ID, device: row.Device, profile: row.Profile, githubExpired: row.GitHubExpired}
		if !h.now().Before(s.expires) || (!s.githubExpired && !h.sessionUsable(s)) {
			pruned = true
			continue
		}
		if len(row.GitHub) > 0 {
			aead, err := h.sessionCipher(false)
			if err != nil {
				return err
			}
			if len(row.GitHub) < aead.NonceSize() {
				return errors.New("invalid encrypted factory session")
			}
			plain, err := aead.Open(nil, row.GitHub[:aead.NonceSize()], row.GitHub[aead.NonceSize():], []byte(hash))
			if err != nil {
				return errors.New("could not decrypt factory session")
			}
			var credentials githubCredentials
			if err := json.Unmarshal(plain, &credentials); err != nil {
				return err
			}
			s.githubToken, s.githubRefresh = credentials.Access, credentials.Refresh
			s.githubExpires, s.refreshExpires = credentials.Expires, credentials.RefreshExpires
		}
		loaded[hash] = s
	}
	h.sessions.ids = loaded
	for _, s := range loaded {
		if s.profile != nil && !s.githubExpired {
			h.viewer.see(*s.profile, s.created, s.lastSeen, s.githubToken)
		}
	}
	if pruned {
		return h.saveSessions()
	}
	return nil
}

// sessionCipher uses a private random key beside tokens.json. File permissions
// match the host config; AES-GCM keeps access and refresh tokens out of JSON.
// A missing key for an existing ciphertext is an error, never a new identity.
func (h *Handler) sessionCipher(create bool) (cipher.AEAD, error) {
	if h.sessions.key == nil {
		path := filepath.Join(filepath.Dir(h.sessions.path), "sessions.key")
		key, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) && create {
			key = make([]byte, 32)
			if _, err = rand.Read(key); err != nil {
				return nil, err
			}
			// The factory owns one serving process. Exclusive creation still
			// protects the key if two handlers initialize at the same instant.
			f, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if errors.Is(openErr, os.ErrExist) {
				key, err = os.ReadFile(path)
			} else if openErr != nil {
				return nil, openErr
			} else {
				_, err = f.Write(key)
				if err == nil {
					err = f.Sync()
				}
				closeErr := f.Close()
				if err == nil {
					err = closeErr
				}
			}
		}
		if err != nil {
			return nil, err
		}
		if len(key) != 32 {
			return nil, errors.New("invalid factory session key")
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, err
		}
		h.sessions.key = key
	}
	block, err := aes.NewCipher(h.sessions.key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// saveSessions is called with the session lock held. Sign-in, refresh and
// sign-out are acknowledged only after the private snapshot has been replaced.
func (h *Handler) saveSessions() error {
	if h.sessions.err != nil {
		return h.sessions.err
	}
	if h.sessions.path == "" {
		return nil
	}
	root := filepath.Dir(h.sessions.path)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	doc := sessionDocument{Version: 1, Sessions: make(map[string]sessionRow)}
	for hash, s := range h.sessions.ids {
		row := sessionRow{Expires: s.expires, Created: s.created, LastSeen: s.lastSeen,
			TokenID: s.tokenID, ID: s.id, Device: s.device, Profile: s.profile, GitHubExpired: s.githubExpired}
		if s.githubToken != "" || s.githubRefresh != "" {
			aead, err := h.sessionCipher(true)
			if err != nil {
				return err
			}
			plain, err := json.Marshal(githubCredentials{s.githubToken, s.githubRefresh, s.githubExpires, s.refreshExpires})
			if err != nil {
				return err
			}
			nonce := make([]byte, aead.NonceSize())
			if _, err := rand.Read(nonce); err != nil {
				return err
			}
			row.GitHub = aead.Seal(nonce, nonce, plain, []byte(hash))
		}
		doc.Sessions[hash] = row
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(root, ".sessions-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(append(body, '\n')); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), h.sessions.path); err != nil {
		return err
	}
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync factory sessions: %w", err)
	}
	return nil
}
