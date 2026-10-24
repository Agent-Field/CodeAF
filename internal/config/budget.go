package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/filememo"
	"github.com/Agent-Field/codeaf/internal/home"
)

// DailyBudgetUSDAt, DailyBudgetBaseUSDAt and WriteDailyBudgetUSD live in
// dailylimit.go, which owns the daily limit's persisted authority.

// profileConfigMemo is the profile's config.json, PARSED AT MOST ONCE PER
// CHANGE.
//
// THE DEFECT IT CLOSES, measured on 2026-09-11: this file answers questions that
// are asked on a keystroke and on a turn — [APIKeyConfigured] runs on EVERY
// Enter the person presses (internal/tui3's input.go) and [FirstPrompt] on EVERY
// turn (internal/session's loop.go) — and every one of those calls was a fresh
// os.ReadFile and a fresh encoding/json pass over a file that changes about once
// a week. Neither call site knew it was paying for a read; each of the eight
// readers below was written separately and each looked free on its own.
//
// THE MEMO IS KEYED ON THE FILE AND ON THIS PROCESS'S OWN WRITES.
// internal/filememo spends one stat to decide whether it may answer, and
// [SettingsGeneration] is folded in beside it so a value this process persisted
// can never be served back stale, whatever a filesystem's timestamp resolution
// is. What the memo does not see is what the counter below already documents it
// cannot: a same-inode write that restores both size and modification time.
// Ordinary external edits and atomic replacements are seen on the next read.
var profileConfigMemo = filememo.Stamped(SettingsGeneration,
	func(_ string, data []byte, missing bool) (map[string]json.RawMessage, error) {
		// A file that is not there yet is an empty object, not an error.
		if missing {
			return map[string]json.RawMessage{}, nil
		}
		values := make(map[string]json.RawMessage)
		if err := json.Unmarshal(data, &values); err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		if values == nil {
			return nil, fmt.Errorf("read config: expected a JSON object")
		}
		return values, nil
	})

// readProfileConfig is the single reader of the profile's config.json.
//
// EVERY CALLER GETS THE SAME MAP AND NOBODY MAY WRITE IT. A memo hands back the
// value it holds rather than a copy, so the eight readers in this package treat
// what comes out of here as read-only — which every one of them already did,
// because the only writer is [writeProfileValues] and it builds its own map.
//
// IT IS A LAW AND NOT A HOPE: TestAWriteDoesNotRewriteWhatTheMemoIsHolding reads
// a value, writes a different one through the writer, and fails if the map the
// reader was handed moved. Cloning on the way out would have made that
// impossible too, and it would also have put an allocation back on the path
// [APIKeyConfigured] runs on for every Enter — which is the cost this whole memo
// exists to remove. The law is free; the clone is not.
func readProfileConfig(profileDir string) (map[string]json.RawMessage, error) {
	return profileConfigMemo.Read(BudgetConfigPath(profileDir))
}

// writeProfileValue is the single writer every persisted setting goes through:
// read, replace one key, write a temporary file, rename. Unrelated keys — the
// other settings and anything a later version adds — survive untouched.
func writeProfileValue(profileDir, key string, value any) error {
	return writeProfileValues(profileDir, map[string]any{key: value})
}

// writeProfileValues is the same write for SEVERAL KEYS AT ONCE, and the reason
// it exists is that some settings are one decision spelled in more than one row.
// The crew is four tier rows written from one word (crew.go): four separate
// writes would leave a window in which two classes belong to the old crew and
// two to the new, and the derived crew row would read "custom" about a state
// nobody chose. One read, one temporary file, one rename — the keys land
// together or not at all.
//
// The single-key writer above is this function with a map of one, so there is
// still exactly one place that knows how a setting reaches the disk.
var profileWriteMu sync.Mutex

// removeProfileKey, as a value in [writeProfileValues], takes the key OUT of
// the file rather than writing it. It exists for the rows whose absence is an
// answer of its own — an unpinned crew seat is a row that is not there, and
// writing an empty string would be the different answer "cleared" — so the
// same one transaction can remove one row while it writes another.
var removeProfileKey = profileKeyRemoval{}

// profileKeyRemoval is [removeProfileKey]'s type, unexported so no caller can
// spell a removal any other way.
type profileKeyRemoval struct{}

// profileChange is what one write does to the file: the rows it sets, already
// encoded, and the rows it takes out.
type profileChange struct {
	set     map[string]json.RawMessage
	removed []string
}

// encodeChange turns the caller's updates into a profileChange. A value that
// cannot be encoded is refused here, before any lock is taken, because
// user-defined marshalers do not belong inside the profile critical section.
func encodeChange(updates map[string]any) (profileChange, error) {
	change := profileChange{set: make(map[string]json.RawMessage, len(updates))}
	for name, value := range updates {
		if _, remove := value.(profileKeyRemoval); remove {
			change.removed = append(change.removed, name)
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return profileChange{}, fmt.Errorf("write config %s: %w", name, err)
		}
		change.set[name] = encoded
	}
	return change, nil
}

// empty reports a change that sets and removes nothing, which is not written.
func (c profileChange) empty() bool { return len(c.set) == 0 && len(c.removed) == 0 }

// applyTo is held with the change made. THE WRITER OWNS ITS OWN MAP:
// [readProfileConfig] hands back the map its memo is holding, and adding a row
// to that map in place would rewrite what every reader in this package is about
// to be told the file says, including on the path where the write then fails.
func (c profileChange) applyTo(held map[string]json.RawMessage) map[string]json.RawMessage {
	values := make(map[string]json.RawMessage, len(held)+len(c.set))
	maps.Copy(values, held)
	maps.Copy(values, c.set)
	for _, name := range c.removed {
		delete(values, name)
	}
	return values
}

func writeProfileValues(profileDir string, updates map[string]any) error {
	return writeProfileValuesExpecting(profileDir, nil, updates)
}

// errProfileMoved is a write refused because a row it was decided on changed
// between the read and the write ([writeProfileValuesExpecting]).
var errProfileMoved = errors.New("these settings changed while the change was being made")

// writeProfileValuesExpecting is [writeProfileValues] that first checks, UNDER
// THE SAME LOCK AS THE WRITE, that every row in expect still holds exactly the
// bytes it was read with (a nil value: still absent), and refuses with
// [errProfileMoved] otherwise. It is how a change computed from several rows —
// a rule edited a tick at a time, a pin judged against that rule — lands on the
// rows it was computed from, or not at all, rather than over a write that came
// in between.
func writeProfileValuesExpecting(profileDir string, expect map[string]json.RawMessage, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	change, err := encodeChange(updates)
	if err != nil {
		return err
	}
	// The key in the error messages is a DETERMINISTIC one, since a map has no
	// order and a failure that named a different row on every attempt would be a
	// failure nobody could search for.
	return editProfile(profileDir, errorKey(updates), func(held map[string]json.RawMessage) (profileChange, error) {
		for name, want := range expect {
			got, present := held[name]
			if present != (want != nil) || (present && !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want))) {
				return profileChange{}, errProfileMoved
			}
		}
		return change, nil
	})
}

// editProfile is THE ONE WRITER of config.json and the seam every write passes:
// under the process mutex and the cross-process file lock it reads the file as
// it is NOW, asks decide what to change given that reading, and renames the
// result into place. A caller that must compute its change from what the file
// holds (a service row's key, the list of connected services, a rule added to a
// list) does that inside decide, so it can never write back a copy it loaded
// before another writer, or another machine's restore, put something new in the
// file. Whatever decide does not name is carried over untouched.
func editProfile(profileDir, key string, decide func(held map[string]json.RawMessage) (profileChange, error)) error {
	// A settings write is one read-copy-rename transaction. Two surface actions
	// may reach it together; serializing the whole transaction keeps the second
	// read behind the first rename instead of letting either rename discard the
	// other action.
	profileWriteMu.Lock()
	defer profileWriteMu.Unlock()

	path := BudgetConfigPath(profileDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("write config %s: %w", key, err)
	}
	profileLock, err := lockProfileConfig(path)
	if err != nil {
		return fmt.Errorf("write config %s: %w", key, err)
	}
	defer profileLock.Close()

	held, err := readProfileConfig(profileDir)
	if err != nil {
		return fmt.Errorf("write config: preserve existing file: %w", err)
	}
	change, err := decide(held)
	if err != nil {
		return err
	}
	if change.empty() {
		return nil
	}
	// Computed changes pass the same credential guard as direct writes, against the locked reading.
	if err := refuseAppCredentialUpdates(held, change); err != nil {
		return err
	}
	stampPreferences(profileDir, held, &change)
	encoded, err := json.MarshalIndent(change.applyTo(held), "", "  ")
	if err != nil {
		return fmt.Errorf("write config %s: %w", key, err)
	}
	if err := replaceProfileFile(path, append(encoded, '\n')); err != nil {
		return fmt.Errorf("write config %s: %w", key, err)
	}
	// EVERY PERSISTED WRITE PASSES HERE, which is what makes one counter enough
	// for a live reader to know its snapshot is stale (see [SettingsGeneration]).
	bumpSettingsGeneration()
	return nil
}

// replaceProfileFile writes body to a temporary file beside path and renames it
// over path, so a reader sees the old file or the new one and never half of
// either. The file is owner-only from its first byte.
func replaceProfileFile(path string, body []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name()) // a no-op once the rename has moved it
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}

// errorKey is the key a multi-key write blames, chosen deterministically: the
// first in sorted order. It is a name for a failure message and nothing else.
func errorKey(updates map[string]any) string {
	names := make([]string, 0, len(updates))
	for name := range updates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names[0]
}

// ── the generation counter ──────────────────────────────────────────────────
//
// A live reader — the v3 door's crew source (cmd/codeaf's v3RolesSource) — needs
// to know when what it read has changed, and it needs to know cheaply: an
// auxiliary model is resolved on the path of a turn, and a file read there would
// be a syscall per call for a file that changes once a week.
//
// So every write bumps a counter, and a reader compares one integer. It is the
// SIGNAL a cache somewhere else invalidates on — the v3 door's crew source, and
// since 2026-09-11 this package's OWN parsed view of config.json
// ([profileConfigMemo]), which folds this counter into its freshness key so that
// the one thing a file's timestamp cannot be trusted to report — a write this
// process just made — is reported by the process that made it.
//
// (It said here, until that memo existed, that this package deliberately held no
// cache because a stale settings value is worse than a slow one. Both halves of
// that stayed true and the conclusion did not: the memo is not allowed to be
// stale, because it re-stats the file on every read and reads this counter
// beside it.)
//
// The counter alone does not see another process's writes. Live snapshot
// readers must pair it with file metadata: the v3 crew source does this for
// both profile and project configuration, and profileConfigMemo does it for
// parsed profile values. No watcher or repeated JSON decode is required.

var settingsGeneration atomic.Uint64

// SettingsGeneration is the number of persisted settings writes this process has
// made. A reader that holds a snapshot keeps the value it read at, and rebuilds
// when the two differ.
func SettingsGeneration() uint64 { return settingsGeneration.Load() }

func bumpSettingsGeneration() { settingsGeneration.Add(1) }

// ProfilePath names a file this profile keeps, and is THE ONE PLACE THAT KNOWS
// WHAT AN EMPTY PROFILE DIRECTORY MEANS.
//
// AN EMPTY PROFILE DIRECTORY IS THE NORMAL CASE, NOT THE ABSENT CASE, AND
// ABSENCE IS A HOSTED WINDOW. [ProfileDir] carries CODEAF_PROFILE_DIR, which
// almost nobody exports, so the empty string is what very nearly every launch
// passes down here — and it has always meant "the profile where it always is",
// codeaf's own state root, which internal/home owns and CODEAF_HOME moves. A
// caller that reads emptiness as "there is no profile" and goes quiet is
// therefore silent on the ordinary launch and loud only on the rare one, which
// is the exact inversion this function exists to stop being retyped: it has
// cost the model picker's lane pin, the settings pair on the belt, the status
// line's crew segment and the notices' own memory, each found separately.
func ProfilePath(profileDir, name string) string {
	if profileDir = strings.TrimSpace(profileDir); profileDir != "" {
		return filepath.Join(profileDir, name)
	}
	return home.Join(name)
}

// BudgetConfigPath is config.json in codeaf's state root unless
// CODEAF_PROFILE_DIR supplies the same alternate root used by measured
// profiles.
func BudgetConfigPath(profileDir string) string { return ProfilePath(profileDir, "config.json") }

func parseDailyBudget(raw, source string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("%s: want a non-negative dollar amount, got %q", source, raw)
	}
	return validateDailyBudget(value, source)
}

func validateDailyBudget(value float64, source string) (float64, error) {
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%s: want a non-negative dollar amount, got %v", source, value)
	}
	return value, nil
}
