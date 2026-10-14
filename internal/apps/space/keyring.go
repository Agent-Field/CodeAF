package space

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// savedSpace is one workspace this desktop has joined. The token is the
// member session and never goes in the desk's own files.
type savedSpace struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Address string    `json:"address"`
	Origin  string    `json:"origin"`
	Role    string    `json:"role"`
	Handle  string    `json:"handle"`
	Token   string    `json:"token"`
	Expires time.Time `json:"expires"`
	Order   int       `json:"order"`
}

// Keyring stores joined spaces. Production uses the OS keychain. NewFileKeyring
// is for tests; the server does not fall back to a file.
type Keyring interface {
	Load() ([]savedSpace, error)
	Save([]savedSpace) error
}

var errKeyringUnavailable = errors.New("the keychain is not available")

type fileKeyring struct{ path string }

// NewFileKeyring keeps joined spaces in a file. Tests use it. The running
// binary does not.
func NewFileKeyring(path string) Keyring { return fileKeyring{path: path} }

func (f fileKeyring) Load() ([]savedSpace, error) {
	body, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}
	var rows []savedSpace
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (f fileKeyring) Save(rows []savedSpace) error {
	if rows == nil {
		rows = []savedSpace{}
	}
	body, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".spaces-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, f.path)
}

type osKeyring struct{}

func (osKeyring) Load() ([]savedSpace, error) {
	body, err := osKeyring{}.read()
	if err != nil || len(bytes.TrimSpace(body)) == 0 {
		return nil, err
	}
	var rows []savedSpace
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (k osKeyring) Save(rows []savedSpace) error {
	if rows == nil {
		rows = []savedSpace{}
	}
	body, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	return k.write(body)
}

func (osKeyring) read() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "security", "find-generic-password", "-s", "codeaf", "-a", "spaces", "-w")
	case "linux":
		cmd = exec.CommandContext(ctx, "secret-tool", "lookup", "service", "codeaf", "account", "spaces")
	default:
		return nil, errKeyringUnavailable
	}
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errKeyringUnavailable
		}
		if missingItem(err) {
			return nil, nil
		}
		return nil, err
	}
	return bytes.TrimSpace(out), nil
}

func (osKeyring) write(body []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "security", "add-generic-password", "-U", "-s", "codeaf", "-a", "spaces", "-w", string(body))
	case "linux":
		cmd = exec.CommandContext(ctx, "secret-tool", "store", "--label", "codeaf spaces", "service", "codeaf", "account", "spaces")
		cmd.Stdin = bytes.NewReader(body)
	default:
		return errKeyringUnavailable
	}
	out, err := cmd.CombinedOutput()
	if errors.Is(err, exec.ErrNotFound) {
		return errKeyringUnavailable
	}
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text != "" {
			return errors.New(text)
		}
		return err
	}
	return nil
}

func missingItem(err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	text := strings.ToLower(string(exit.Stderr))
	return strings.Contains(text, "could not be found") || strings.Contains(text, "not found") || strings.Contains(text, "no such")
}
