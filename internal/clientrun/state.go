package clientrun

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

type state struct {
	Secret   []byte               `json:"secret,omitempty"`
	LastAt   map[string]int64     `json:"last_at,omitempty"`
	Paused   map[string]bool      `json:"paused,omitempty"`
	Outcomes map[string][]Outcome `json:"outcomes,omitempty"`
	Receipts map[string]Receipt   `json:"receipts,omitempty"`
}

type pinsState struct {
	Enrollments map[string]enrollmentPin `json:"enrollments,omitempty"`
	Directories map[string]uint64        `json:"directories,omitempty"`
}

type enrollmentPin struct {
	Enroll   clientproto.Enrollment `json:"enroll"`
	Conflict bool                   `json:"conflict,omitempty"`
}

// Tombstone records a revoked phone and the directory version at which it was
// observed revoked. A tombstoned client is never re-admitted, including after
// AcceptOlderList (contract V1, V2, V5).
type Tombstone struct {
	Client           string `json:"client"`
	DirectoryVersion uint64 `json:"directory_version"`
}

// Outcome is one entry in the outcomes.<writer> slot (design 3.6).
type Outcome struct {
	Number int64  `json:"number"`
	Status string `json:"status"`
	Agent  string `json:"agent,omitempty"`
	Reason string `json:"reason,omitempty"`
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

// writeAtomic replaces path atomically with fsync of both the temporary file
// and the parent directory so a crash never leaves a half-written state file.
func writeAtomic(path, prefix string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), prefix)
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
	return fsreplace.SyncDir(filepath.Dir(path))
}

// save replaces runner.json durably. The caller holds mu, so settings,
// monotonic timestamps, and receipts cannot overwrite each other's update.
func (r *Runner) save() error {
	if r.lock == nil {
		return errors.New("client runner is closed")
	}
	return writeAtomic(StatePath(r.cfg.Home), ".runner-*", r.state)
}

func (r *Runner) savePinsLocked() error {
	if r.lock == nil {
		return errors.New("client runner is closed")
	}
	return writeAtomic(PinsPath(r.cfg.Home), ".pins-*", r.pins)
}

func (r *Runner) saveTombstonesLocked() error {
	if r.lock == nil {
		return errors.New("client runner is closed")
	}
	list := make([]Tombstone, 0, len(r.tombs))
	for _, t := range r.tombs {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Client < list[j].Client })
	return writeAtomic(TombstonesPath(r.cfg.Home), ".tombstones-*", list)
}

// nextAt returns max(wall_clock_ms, last_at+1) for (writer, slotName) and
// persists runner.json before the caller uploads the slot (design 3.3, contract L6).
func (r *Runner) nextAt(writer, slotName string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.LastAt == nil {
		r.state.LastAt = map[string]int64{}
	}
	key := writer + "/" + slotName
	prev, had := r.state.LastAt[key]
	at := max(r.cfg.Now().UnixMilli(), prev+1)
	r.state.LastAt[key] = at
	if err := r.save(); err != nil {
		if had {
			r.state.LastAt[key] = prev
		} else {
			delete(r.state.LastAt, key)
		}
		return 0, err
	}
	return at, nil
}

func (r *Runner) isTombstonedLocked(id string) bool {
	_, ok := r.tombs[id]
	return ok
}

func (r *Runner) hasClientStateLocked(id string) bool {
	if _, ok := r.enrolled[id]; ok {
		return true
	}
	if _, ok := r.pins.Enrollments[id]; ok {
		return true
	}
	if _, ok := r.state.Paused[id]; ok {
		return true
	}
	if _, ok := r.state.Outcomes[id]; ok {
		return true
	}
	for _, rec := range r.state.Receipts {
		if rec.Client == id {
			return true
		}
	}
	return false
}

// tombstoneClientLocked writes the tombstone for id, deletes that client's
// keys, receipts, outcomes, pause flag, and publication hashes, and persists
// both tombstones.json and runner.json (contract V1, V2, V5).
func (r *Runner) tombstoneClientLocked(id string, dirVersion uint64) error {
	if id == "" {
		return nil
	}
	existing, already := r.tombs[id]
	if !already || (existing.DirectoryVersion == 0 && dirVersion > 0) {
		r.tombs[id] = Tombstone{Client: id, DirectoryVersion: max(existing.DirectoryVersion, dirVersion)}
		if err := r.saveTombstonesLocked(); err != nil {
			if already {
				r.tombs[id] = existing
			} else {
				delete(r.tombs, id)
			}
			return err
		}
	}
	delete(r.enrolled, id)
	delete(r.watching, id)
	for key := range r.hashes {
		if strings.HasPrefix(key, id+"/") {
			delete(r.hashes, key)
		}
	}
	for key, cancel := range r.coalesce {
		if strings.HasPrefix(key, id+"/") {
			delete(r.coalesce, key)
			cancel()
		}
	}
	for key := range r.slotBackoffs {
		if strings.HasPrefix(key, id+"/") {
			delete(r.slotBackoffs, key)
		}
	}
	stateChanged := false
	if _, ok := r.state.Paused[id]; ok {
		delete(r.state.Paused, id)
		stateChanged = true
	}
	if _, ok := r.state.Outcomes[id]; ok {
		delete(r.state.Outcomes, id)
		stateChanged = true
	}
	for key, receipt := range r.state.Receipts {
		if receipt.Client == id {
			delete(r.state.Receipts, key)
			stateChanged = true
		}
	}
	if _, ok := r.pins.Enrollments[id]; ok {
		delete(r.pins.Enrollments, id)
		if err := r.savePinsLocked(); err != nil {
			return err
		}
	}
	if stateChanged {
		return r.save()
	}
	return nil
}

// load reads runner.json, pins.json, and tombstones.json under the process lock.
func (r *Runner) load() error {
	if err := r.loadTombstones(); err != nil {
		return err
	}
	if err := r.loadPins(); err != nil {
		return err
	}
	if err := r.loadRunnerState(); err != nil {
		return err
	}
	// Seed the derived enrolled view from pinned non-conflicting statements that
	// are not tombstoned so Clients/Pause/Unpair work immediately after restart.
	for id, pin := range r.pins.Enrollments {
		if r.isTombstonedLocked(id) {
			delete(r.pins.Enrollments, id)
			continue
		}
		if !pin.Conflict && pin.Enroll.Device == id {
			r.enrolled[id] = pin.Enroll
		}
	}
	return nil
}

func (r *Runner) loadPins() error {
	raw, err := os.ReadFile(PinsPath(r.cfg.Home))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var loaded pinsState
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return err
	}
	if loaded.Enrollments != nil {
		r.pins.Enrollments = loaded.Enrollments
	}
	if loaded.Directories != nil {
		r.pins.Directories = loaded.Directories
	}
	return nil
}

func (r *Runner) loadTombstones() error {
	raw, err := os.ReadFile(TombstonesPath(r.cfg.Home))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] == '[' {
		var list []Tombstone
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return err
		}
		for _, t := range list {
			if t.Client != "" {
				r.tombs[t.Client] = t
			}
		}
		return nil
	}
	var byID map[string]Tombstone
	if err := json.Unmarshal(trimmed, &byID); err != nil {
		return err
	}
	for id, t := range byID {
		if t.Client == "" {
			t.Client = id
		}
		r.tombs[t.Client] = t
	}
	return nil
}

// loadRunnerState streams runner.json, including legacy receipt maps, one
// record at a time. Chat logging precedes compaction, so moving history out of
// the replay file cannot lose chat lines.
func (r *Runner) loadRunnerState() error {
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
		case "last_at":
			err = dec.Decode(&r.state.LastAt)
		case "paused":
			err = dec.Decode(&r.state.Paused)
		case "outcomes":
			err = dec.Decode(&r.state.Outcomes)
		case "clients":
			var legacy map[string]struct {
				Paused bool `json:"paused"`
			}
			if err = dec.Decode(&legacy); err == nil {
				for id, c := range legacy {
					if c.Paused {
						r.state.Paused[id] = true
					}
				}
			}
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
	if r.state.LastAt == nil {
		r.state.LastAt = map[string]int64{}
	}
	if r.state.Paused == nil {
		r.state.Paused = map[string]bool{}
	}
	if r.state.Outcomes == nil {
		r.state.Outcomes = map[string][]Outcome{}
	}
	for id := range r.tombs {
		delete(r.state.Paused, id)
		delete(r.state.Outcomes, id)
	}
	for key, receipt := range r.state.Receipts {
		if r.isTombstonedLocked(receipt.Client) {
			delete(r.state.Receipts, key)
		}
	}
	for id, list := range r.state.Outcomes {
		if len(list) > clientproto.MaxOutcomes {
			r.state.Outcomes[id] = slices.Clone(list[len(list)-clientproto.MaxOutcomes:])
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
		// Keep only the highest number per client as the replay watermark.
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

// receiptKey and log are ported unchanged from origin/phone/live's queue.go so
// loadReceipts in state.go compiles and archives legacy receipts cleanly.
func receiptKey(client string, number int64) string {
	return client + "/" + strconv.FormatInt(number, 10)
}

func (r *Runner) log(receipt Receipt) error {
	if r.cfg.Log == nil {
		return errors.New("queued calls need a chat logger")
	}
	return r.cfg.Log(receipt)
}
