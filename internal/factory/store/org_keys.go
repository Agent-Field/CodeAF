package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ── THE ORGANIZATION'S MODEL KEYS ──────────────────────────────────────────
//
// When the operator runs an organization's workstream agents on its server
// (factory.AgentRunnerServer, WAVE2.md §4.3), the server calls models with
// keys the operator hands it, never with the key of whoever happens to sign
// in to the machine it runs on. They are kept here, in one file only this
// server's user may read (0600, written atomically under a lock), by provider.
//
// A KEY IS WRITTEN AND NEVER READ BACK OVER THE WIRE. [Store.OrgKeys] answers
// what a screen may show — the provider, the last four characters, who set it
// and when — and only [Store.OrgKey], which no route calls, answers the key
// itself, for the server's own runner.

// OrgKeyProviders are the providers a key may be kept for. The server's
// runner calls models through the default model source, which is OpenRouter.
var OrgKeyProviders = []string{"openrouter"}

// OrgKey is one key as a screen may see it.
type OrgKey struct {
	Provider string `json:"provider"`
	Last4    string `json:"last4"`
	SetAt    string `json:"set_at"`
	SetBy    string `json:"set_by"`
}

type orgKeyRow struct {
	Key   string `json:"key"`
	SetAt string `json:"set_at"`
	SetBy string `json:"set_by"`
}

const orgKeysFile = "org-keys.json"

// ErrOrgKeyProvider is a provider no key may be kept for.
var ErrOrgKeyProvider = errors.New(`provider must be "openrouter"`)

// KnownOrgKeyProvider says p is a provider a key may be kept for.
func KnownOrgKeyProvider(p string) bool {
	for _, known := range OrgKeyProviders {
		if p == known {
			return true
		}
	}
	return false
}

func (st *Store) readOrgKeys() (map[string]orgKeyRow, error) {
	raw, err := os.ReadFile(filepath.Join(st.root, orgKeysFile))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]orgKeyRow{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]orgKeyRow{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.New("the organization's key file cannot be read")
	}
	return out, nil
}

// OrgKeys is every key kept, as a screen may see it, by provider.
func (st *Store) OrgKeys() ([]OrgKey, error) {
	rows, err := st.readOrgKeys()
	if err != nil {
		return nil, err
	}
	out := make([]OrgKey, 0, len(rows))
	for provider, row := range rows {
		out = append(out, OrgKey{Provider: provider, Last4: lastFour(row.Key), SetAt: row.SetAt, SetBy: row.SetBy})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out, nil
}

// OrgKey is the key kept for provider, for the server's own runner. NO ROUTE
// ANSWERS IT.
func (st *Store) OrgKey(provider string) (string, bool) {
	rows, err := st.readOrgKeys()
	if err != nil {
		return "", false
	}
	row, ok := rows[provider]
	return row.Key, ok && row.Key != ""
}

// SetOrgKey keeps key for provider, replacing the one before, and answers it
// as a screen may see it.
func (st *Store) SetOrgKey(provider, key, by, at string) (OrgKey, error) {
	if !KnownOrgKeyProvider(provider) {
		return OrgKey{}, ErrOrgKeyProvider
	}
	key = strings.TrimSpace(key)
	err := st.underLock(filepath.Join(st.root, "org-keys.lock"), func() error {
		rows, err := st.readOrgKeys()
		if err != nil {
			return err
		}
		rows[provider] = orgKeyRow{Key: key, SetAt: at, SetBy: by}
		return st.writeOrgKeys(rows)
	})
	return OrgKey{Provider: provider, Last4: lastFour(key), SetAt: at, SetBy: by}, err
}

// DeleteOrgKey forgets provider's key, and says whether one was kept.
func (st *Store) DeleteOrgKey(provider string) (bool, error) {
	var had bool
	err := st.underLock(filepath.Join(st.root, "org-keys.lock"), func() error {
		rows, err := st.readOrgKeys()
		if err != nil {
			return err
		}
		_, had = rows[provider]
		if !had {
			return nil
		}
		delete(rows, provider)
		return st.writeOrgKeys(rows)
	})
	return had, err
}

func (st *Store) writeOrgKeys(rows map[string]orgKeyRow) error {
	raw, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	// writeAtomic creates the file 0600 before a byte is written.
	return writeAtomic(filepath.Join(st.root, orgKeysFile), append(raw, '\n'))
}

// lastFour is what a screen shows of a key: its last four characters, and
// nothing for a key too short to hide anything behind them.
func lastFour(key string) string {
	if len(key) < 12 {
		return ""
	}
	return key[len(key)-4:]
}
