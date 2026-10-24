package clientrun

import (
	"encoding/json"
	"errors"
	"io"
	"maps"
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
	Transcript     string `json:"transcript,omitempty"`
	ClaimToken     string `json:"claim_token,omitempty"`
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
	if r.lock == nil {
		return errors.New("client runner is closed")
	}
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

// load streams old receipt maps one record at a time. Chat logging precedes
// compaction, so moving history out of the replay file cannot lose chat lines.
func (r *Runner) load() error {
	f, err := os.Open(StatePath(r.cfg.Home))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	token, err := dec.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return errors.New("invalid client runner state")
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return err
		}
		switch key {
		case "secret":
			err = dec.Decode(&r.state.Secret)
		case "clients":
			err = dec.Decode(&r.state.Clients)
		case "receipts":
			err = r.loadReceipts(dec)
		default:
			var ignored json.RawMessage
			err = dec.Decode(&ignored)
		}
		if err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing client runner state")
	}
	if r.state.Clients == nil {
		r.state.Clients = map[string]Client{}
	}
	for key, receipt := range r.state.Receipts {
		if _, ok := r.state.Clients[receipt.Client]; !ok {
			delete(r.state.Receipts, key)
		}
	}
	// Only a logger may compact an installation's old on-disk history. Read-
	// only callers keep its file intact until a logger can archive the lines.
	if r.cfg.Log != nil {
		return r.save()
	}
	return nil
}

func (r *Runner) loadReceipts(dec *json.Decoder) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return nil
	}
	if token != json.Delim('{') {
		return errors.New("invalid client receipts")
	}
	for dec.More() {
		if _, err := dec.Token(); err != nil {
			return err
		}
		var receipt Receipt
		if err := dec.Decode(&receipt); err != nil {
			return err
		}
		if r.cfg.Log != nil && receipt.Status != "claiming" {
			if err := r.log(receipt); err != nil {
				return err
			}
		}
		// Keep only the highest number: all older numbers are rejected by
		// previous's contiguous-sequence check even without their full proof.
		key := receipt.Client
		previous, ok := r.state.Receipts[key]
		if !ok || previous.Number < receipt.Number {
			r.state.Receipts[key] = receipt
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	kept := r.state.Receipts
	r.state.Receipts = map[string]Receipt{}
	for _, receipt := range kept {
		r.state.Receipts[receiptKey(receipt.Client, receipt.Number)] = receipt
	}
	return nil
}

// trim keeps the current proof and sequence watermark even when the relay's
// acknowledgement response was lost. Older effects already reached chat logs.
func (r *Runner) trim(receipt Receipt) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old := maps.Clone(r.state.Receipts)
	for key, previous := range r.state.Receipts {
		if previous.Client == receipt.Client && previous.Number < receipt.Number {
			delete(r.state.Receipts, key)
		}
	}
	if len(old) == len(r.state.Receipts) {
		return nil
	}
	if err := r.save(); err != nil {
		r.state.Receipts = old
		return err
	}
	return nil
}
