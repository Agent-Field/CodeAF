package vaultsync

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/keys"
)

// DeviceWindows carries replaceable history windows. Only the named device
// writes its values; receiving a window never makes that device its author.
// The source bounds the encoded windows before capture, across collections.
type DeviceWindows struct {
	scope, device, ledger string
	set                   Keyset
	every                 time.Duration
	now                   func() time.Time
}

// Windows keeps the rate clock on disk because flush and idle rounds construct
// fresh carriers. Team records use Keyed and never pass through this clock.
func Windows(kind, device string, set Keyset, ledger string, every time.Duration) DeviceWindows {
	return DeviceWindows{scope: "home:windows:" + kind, device: device, set: set, ledger: ledger, every: every, now: time.Now}
}

func (w DeviceWindows) captureEdits(v VaultFile) error { return w.capture(v) }
func (w DeviceWindows) capture(v VaultFile) error {
	local, err := w.set.Values()
	if errors.Is(err, ErrDamaged) {
		return nil
	}
	if err != nil {
		return err
	}
	entries, err := v.Entries(w.scope)
	if err != nil {
		return err
	}
	var last time.Time
	raw, err := os.ReadFile(w.ledger)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = json.Unmarshal(raw, &last)
	now := w.now()
	due := last.IsZero() || now.Sub(last) >= w.every
	own := map[string]string{}
	for _, e := range entries {
		team, device, ok := strings.Cut(e.Name, "/")
		if !ok || device != w.device {
			continue
		}
		own[team] = e.Value
		// Deletion cannot wait for the publication clock. No other device's
		// values are touched, even when that device has been revoked.
		if !w.set.Holds(team) || (due && local[team] == "") {
			if err := v.Delete(w.scope + "/" + e.Name); err != nil {
				return err
			}
		}
	}
	if !due {
		return w.restore(v)
	}
	changed := false
	names := make([]string, 0, len(local))
	for name := range local {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, team := range names {
		value := local[team]
		if own[team] == value {
			continue
		}
		name := team + "/" + w.device
		if err := v.PutAt(w.scope+"/"+name, keys.Entry{Name: name, Value: value, Scope: w.scope}, now); err != nil {
			return err
		}
		changed = true
	}
	// A quiet round writes neither a value nor the local rate clock.
	if changed {
		raw, _ := json.Marshal(now)
		if err := writeAtomic(w.ledger, raw, 0600); err != nil {
			return err
		}
	}
	// A revoked writer changes the directory without changing the sealed
	// object. Refresh display filtering even when no pull will be needed.
	return w.restore(v)
}

func (w DeviceWindows) restore(v VaultFile) error {
	entries, err := v.Entries(w.scope)
	if err != nil {
		return err
	}
	remote := map[string]string{}
	for _, e := range entries {
		team, device, ok := strings.Cut(e.Name, "/")
		if ok && device != w.device && w.set.Holds(team) {
			remote[e.Name] = e.Value
		}
	}
	// Apply receives the complete current set, including its absence, so an
	// expired or revoked window disappears from the display mirror atomically.
	return ignoreDamaged(w.set.Apply(remote))
}
