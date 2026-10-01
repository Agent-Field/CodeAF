package preflight

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// stashName is the file, beside the chat's own folder and never inside what a
// seal captures, that holds what a takeover found until the agent has been told.
// It is this machine's alone: another machine's takeover finds its own.
const stashName = "resume.pending"

func (m *Machine) stashPath() string { return filepath.Join(m.root, stashName) }

// stash keeps a resume for the chat's next step. A resume with nothing to say
// keeps nothing, and clears what an earlier takeover left.
func (m *Machine) stash(r Resume) error {
	if r.Empty() {
		return m.unstashErr()
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return os.WriteFile(m.stashPath(), raw, 0o600)
}

// stashed is the resume a takeover left, and false where there is none.
func (m *Machine) stashed() (Resume, bool) {
	raw, err := os.ReadFile(m.stashPath())
	if err != nil {
		return Resume{}, false
	}
	var r Resume
	return r, json.Unmarshal(raw, &r) == nil
}

// unstash forgets what a takeover left: the agent has been told.
func (m *Machine) unstash() { _ = m.unstashErr() }

func (m *Machine) unstashErr() error {
	if err := os.Remove(m.stashPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
