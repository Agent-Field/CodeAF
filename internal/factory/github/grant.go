package github

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
)

// ReadGrant is an app-provided credential kept separately from every write credential.
type ReadGrant struct {
	Token     string    `json:"token"`
	Login     string    `json:"login"`
	GrantedAt time.Time `json:"granted_at"`
}

// GrantPath places the private read credential beside the host's access configuration.
func GrantPath(tokensPath string) string {
	if tokensPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(tokensPath), "github-grant.json")
}

// LoadReadGrant treats a missing grant as disconnected and refuses malformed state.
func LoadReadGrant(path string) (ReadGrant, error) {
	var grant ReadGrant
	if path == "" {
		return grant, nil
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return grant, nil
	}
	if err != nil {
		return grant, err
	}
	err = json.Unmarshal(body, &grant)
	if err == nil && (grant.Token == "" || grant.Login == "" || grant.GrantedAt.IsZero()) {
		err = errors.New("invalid factory GitHub read grant")
	}
	return grant, err
}

// SaveReadGrant replaces a private file so concurrent readers see complete credentials.
func SaveReadGrant(path string, grant ReadGrant) error {
	body, err := json.Marshal(grant)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".github-grant-*")
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

// ReadTokenAt preserves explicit host overrides; write callers still use TokenAt.
func ReadTokenAt(ctx context.Context, profileDir, path string) (string, string, error) {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if token := strings.TrimSpace(env.Value(name)); token != "" {
			return token, "env", nil
		}
	}
	grant, err := LoadReadGrant(path)
	if err != nil {
		return "", "", err
	}
	if grant.Token != "" {
		return grant.Token, "grant", nil
	}
	token, via := TokenAt(ctx, profileDir)
	return token, via, nil
}
