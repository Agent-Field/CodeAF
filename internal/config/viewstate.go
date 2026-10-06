package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// ViewState remembers navigation, not running work or a temporary decision.
// Place is the registry's word rather than its numeric id, so adding a room
// cannot turn an older bookmark into a different room.
type ViewState struct {
	Version   int    `json:"version"`
	Session   string `json:"session"`
	Workspace string `json:"workspace"`
	Place     string `json:"place"`
}

// viewStatePath scopes a bookmark to the directory the terminal launched in.
// Switching projects inside the window must not change that bookmark's key.
func viewStatePath(profileDir, launchDir string) string {
	if resolved, err := filepath.EvalSymlinks(launchDir); err == nil {
		launchDir = resolved
	}
	key := sha256.Sum256([]byte(filepath.Clean(launchDir)))
	return ProfilePath(profileDir, filepath.Join("views", hex.EncodeToString(key[:])+".json"))
}

// ViewStateAt treats missing, damaged and future records as no saved view.
// A bookmark is optional navigation and must never prevent startup.
func ViewStateAt(profileDir, launchDir string) (ViewState, bool) {
	f, err := os.Open(viewStatePath(profileDir, launchDir))
	if err != nil {
		return ViewState{}, false
	}
	defer f.Close()
	var view ViewState
	err = json.NewDecoder(io.LimitReader(f, 16*1024)).Decode(&view)
	if err != nil || view.Version != 1 || !filepath.IsAbs(view.Session) || !filepath.IsAbs(view.Workspace) || view.Place == "" {
		return ViewState{}, false
	}
	return view, true
}

// WriteViewState replaces one small record atomically. Concurrent terminals
// keep whole records, with the last navigation or shutdown taking precedence.
func WriteViewState(profileDir, launchDir string, view ViewState) error {
	// An explicit --session may have been relative to the launch directory.
	// Its saved identity must still name the same file after a later startup.
	var err error
	if view.Session, err = filepath.Abs(view.Session); err != nil {
		return err
	}
	if view.Workspace, err = filepath.Abs(view.Workspace); err != nil {
		return err
	}
	view.Version = 1
	data, err := json.Marshal(view)
	if err != nil {
		return err
	}
	path := viewStatePath(profileDir, launchDir)
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".view-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
