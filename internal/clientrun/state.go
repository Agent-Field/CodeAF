package clientrun

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

type state struct {
	Secret   []byte             `json:"secret,omitempty"`
	Clients  map[string]Client  `json:"clients"`
	Receipts map[string]Receipt `json:"receipts"`
}

// Receipt is durable before dispatch and finished afterward. A begun call is
// never dispatched again after a crash: its effect may already have happened.
type Receipt struct {
	SkippedFrom    int64  `json:"skipped_from,omitempty"`
	SkippedThrough int64  `json:"skipped_through,omitempty"`
	Digest         string `json:"digest"`
	Client         string `json:"client"`
	Number         int64  `json:"number"`
	Agent          string `json:"agent"`
	Method         string `json:"method"`
	At             string `json:"at"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
}

// save replaces the state durably. The caller holds mu, so settings and
// receipts cannot overwrite each other's update.
func (r *Runner) save() error {
	path := StatePath(r.cfg.Home)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(r.state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".runner-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = f.Chmod(0600)
	if err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err != nil {
		return err
	}
	if closed != nil {
		return closed
	}
	if err := fsreplace.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
