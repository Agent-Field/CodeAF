package config

// Rule metadata lives beside settings so older readers keep their text rows.
// Usage is merged as deltas under a file lock: concurrent terminals must not
// overwrite one another's counts. A short batch avoids a write on each call.
import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// RuleOrigin records the answer, rather than inferring provenance from use.
type RuleOrigin struct {
	Agent string    `json:"agent,omitempty"`
	Place string    `json:"place,omitempty"`
	At    time.Time `json:"at,omitzero"`
}

// RuleMetadata adds history without changing the settings row older builds read.
type RuleMetadata struct {
	Origin   RuleOrigin `json:"origin"`
	Uses     uint64     `json:"uses"`
	LastUsed time.Time  `json:"last_used,omitzero"`
}

const ruleUsageDelay = time.Second

var ruleUsage = struct {
	sync.Mutex
	pending map[string]map[string]RuleMetadata
	timers  map[string]*time.Timer
}{pending: map[string]map[string]RuleMetadata{}, timers: map[string]*time.Timer{}}

func ruleProfile(profile string) string {
	if profile == "" {
		profile = ProfileDir()
	}
	return filepath.Dir(ProfilePath(profile, "rule-metadata.json"))
}

func readRuleMetadata(profile string) (map[string]RuleMetadata, error) {
	raw, err := os.ReadFile(filepath.Join(profile, "rule-metadata.json"))
	rows := map[string]RuleMetadata{}
	if errors.Is(err, os.ErrNotExist) {
		return rows, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = map[string]RuleMetadata{}
	}
	return rows, nil
}

// editRuleMetadata serializes sidecar writers across processes and replaces the
// file atomically so a stopped writer never leaves half a JSON document.
func editRuleMetadata(profile string, edit func(map[string]RuleMetadata)) error {
	if err := os.MkdirAll(profile, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(profile, "rule-metadata.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer filelock.Unlock(lock)
	rows, err := readRuleMetadata(profile)
	if err != nil {
		return err
	}
	edit(rows)
	raw, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(profile, ".rule-metadata-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(raw); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), filepath.Join(profile, "rule-metadata.json"))
}

func saveRuleOrigin(profile, id string, saveErr error, origins []RuleOrigin) error {
	if saveErr != nil || len(origins) == 0 {
		return saveErr
	}
	return editRuleMetadata(ruleProfile(profile), func(rows map[string]RuleMetadata) {
		row := rows[id]
		row.Origin = origins[0]
		rows[id] = row
	})
}

// RuleMetadataAt includes the batch not yet on disk, for immediate receipts.
func RuleMetadataAt(profile, id string) (RuleMetadata, error) {
	rows, err := RuleMetadataAllAt(profile)
	return rows[id], err
}

// RuleMetadataAllAt takes one snapshot for a settings list, so its rows do not
// disagree about a batch that happened to flush halfway through the read.
func RuleMetadataAllAt(profile string) (map[string]RuleMetadata, error) {
	profile = ruleProfile(profile)
	ruleUsage.Lock()
	defer ruleUsage.Unlock()
	rows, err := readRuleMetadata(profile)
	if err != nil {
		return nil, err
	}
	for id, delta := range ruleUsage.pending[profile] {
		row := rows[id]
		row.Uses += delta.Uses
		if delta.LastUsed.After(row.LastUsed) {
			row.LastUsed = delta.LastUsed
		}
		rows[id] = row
	}
	return rows, nil
}

// UseApprovalRule counts only automatic allows; the answer creating a rule is
// not itself an automatic use. Callers flush at turn settlement and shutdown.
func UseApprovalRule(profile, id string, at time.Time) (RuleMetadata, error) {
	profile = ruleProfile(profile)
	ruleUsage.Lock()
	pending := ruleUsage.pending[profile]
	if pending == nil {
		pending = map[string]RuleMetadata{}
		ruleUsage.pending[profile] = pending
	}
	row := pending[id]
	row.Uses++
	if at.After(row.LastUsed) {
		row.LastUsed = at
	}
	pending[id] = row
	if ruleUsage.timers[profile] == nil {
		ruleUsage.timers[profile] = time.AfterFunc(ruleUsageDelay, func() { _ = FlushRuleUsage(profile) })
	}
	ruleUsage.Unlock()
	return RuleMetadataAt(profile, id)
}

// FlushRuleUsage keeps a failed batch pending so the next flush can retry it.
func FlushRuleUsage(profile string) error {
	profile = ruleProfile(profile)
	ruleUsage.Lock()
	defer ruleUsage.Unlock()
	if timer := ruleUsage.timers[profile]; timer != nil {
		timer.Stop()
		delete(ruleUsage.timers, profile)
	}
	pending := ruleUsage.pending[profile]
	if len(pending) == 0 {
		return nil
	}
	err := editRuleMetadata(profile, func(rows map[string]RuleMetadata) {
		for id, delta := range pending {
			row := rows[id]
			row.Uses += delta.Uses
			if delta.LastUsed.After(row.LastUsed) {
				row.LastUsed = delta.LastUsed
			}
			rows[id] = row
		}
	})
	if err == nil {
		delete(ruleUsage.pending, profile)
	}
	return err
}

// RuleReceipt is the saved rule behind one automatic allow, carried as data so
// surfaces can show its explanation and address removal by its stable id.
type RuleReceipt struct {
	ID     string     `json:"id"`
	Text   string     `json:"text"`
	Origin RuleOrigin `json:"origin"`
	Uses   uint64     `json:"uses"`
	Scope  string     `json:"scope"`
}

// ApprovalRuleText uses the stored action and scope, never a model summary.
func ApprovalRuleText(scope, action string) string {
	prefix := "Always allow "
	switch action {
	case "prompt":
		prefix = "Ask before "
	case "deny":
		prefix = "Never allow "
	}
	return prefix + scope
}
