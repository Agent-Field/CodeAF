package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/home"
)

type taskStorageUncertainty struct {
	Version   int    `json:"version"`
	Workspace string `json:"workspace"`
}

func taskStorageUncertaintyRoot() string {
	if env.Get("CODEAF_PROTOTYPE_TASK_STORAGE") != "1" || strings.TrimSpace(env.Get(home.EnvVar)) == "" {
		return ""
	}
	return home.Join("task-storage", "uncertain-writers")
}

// markTaskStorageUncertain is written before launching an uncontained shell or
// custom tool. Process-group disappearance cannot prove that a daemon did not
// escape, so this local receipt intentionally never expires or clears on retry.
func markTaskStorageUncertain(workspace string) error {
	root := taskStorageUncertaintyRoot()
	if root == "" {
		return nil
	}
	workspace = canonicalPath(workspace)
	if workspace == "" || !filepath.IsAbs(workspace) {
		return errors.New("cannot record the tool's workspace for safe compact storage")
	}
	if e := os.MkdirAll(root, 0700); e != nil {
		return e
	}
	// A host-bound process can name absolute paths outside its launch folder.
	// Without containment, one launch makes this entire test profile uncertain.
	name := filepath.Join(root, "uncontained.json")
	f, e := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(e, os.ErrExist) {
		return syncTaskStorageUncertainty(name)
	}
	if e != nil {
		return e
	}
	raw, e := json.Marshal(taskStorageUncertainty{Version: 1, Workspace: workspace})
	if e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	closed := f.Close()
	if e == nil {
		e = closed
	}
	if e != nil {
		return e
	}
	return syncTaskStorageUncertainty(name)
}

// Every launcher makes the presence durable itself, including a launcher that
// races the first creator before its write or directory sync has finished.
func syncTaskStorageUncertainty(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	err = f.Sync()
	closed := f.Close()
	if err != nil {
		return err
	}
	if closed != nil {
		return closed
	}
	for dir := filepath.Dir(name); ; dir = filepath.Dir(dir) {
		if err := syncTaskStorageDirectory(dir); err != nil {
			return err
		}
		if filepath.Dir(dir) == dir {
			return nil
		}
	}
}

func syncTaskStorageDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func taskStorageHasUncertainWriter() bool {
	root := taskStorageUncertaintyRoot()
	if root == "" {
		return false
	}
	entries, e := os.ReadDir(root)
	if errors.Is(e, os.ErrNotExist) {
		return false
	}
	if e != nil {
		return true
	}
	// Presence is the proof: malformed or unfamiliar entries must also retain
	// copies, and reading their contents cannot make retirement safer.
	return len(entries) != 0
}
