package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// THE LEDGER: what this profile has already been told.
//
// One small JSON file beside config.json — `notices.json` — holding, per notice
// id, how many sessions it has been shown in and when it retired, and the build
// the news channel last saw. It is the whole of the notices' memory across
// restarts, and it is written the way the profile's other files are written
// (internal/config's budget.go): whole, to a temporary name in the same
// directory, then renamed over the old one, so a crash mid-write leaves the
// previous ledger and never half of a new one.
//
// A MISSING OR UNREADABLE LEDGER IS AN EMPTY ONE. The worst thing an empty
// ledger does is show a tip a person has seen before, and no version of that
// is worth a line in the transcript about a file they did not know existed.

// noticeLedgerName is the file, beside config.json in the profile directory.
const noticeLedgerName = "notices.json"

// noticeLedgerPath is where the ledger lives, resolved the way config.json
// beside it is resolved ([config.ProfilePath]).
//
// AN EMPTY PROFILE DIRECTORY IS THE NORMAL CASE, NOT THE ABSENT CASE, AND
// ABSENCE IS A HOSTED WINDOW. This function used to answer "" for an empty
// directory and the board then held its memory in RAM for the session — which,
// because AFORGE_PROFILE_DIR is set on almost no launch, is what happened on
// almost every launch: a hint that is meant to age out after three sessions was
// on its first session every time, a hint retired by the gesture it teaches came
// back at the next start, and the news channel never had an older build to
// compare against, so a shipped feature could not announce itself. The ledger is
// four small keys beside the settings they are about, and it belongs wherever
// those settings are.
//
// A CONNECTION CHANGES NOTHING HERE, which is what makes this different from the
// crew (crew.go). What the ledger remembers is which KEYS the person at this
// keyboard has already been taught, and the keyboard is this machine's however
// far away the engine is — so a hosted window keeps its hints in this laptop's
// own state root, like every other thing this surface remembers about its own
// chrome.
func noticeLedgerPath(profileDir string) string {
	return config.ProfilePath(profileDir, noticeLedgerName)
}

// noticeLedger is the file's shape. Every field is omitted when empty, so a
// profile that has never been told anything has a ledger of `{}`.
type noticeLedger struct {
	// Build is the build the news channel last ran under.
	Build string `json:"build,omitempty"`
	// Seen is one mark per notice id that has ever been shown or retired.
	Seen map[string]noticeMark `json:"seen,omitempty"`
}

// noticeMark is the ledger's word on one notice.
type noticeMark struct {
	// Shown counts the SESSIONS the notice was shown in, not the frames.
	Shown int `json:"shown,omitempty"`
	// Retired is when it was retired, RFC 3339, or "" while it is still live.
	Retired string `json:"retired,omitempty"`
}

// loadNoticeLedger reads the file, and answers an empty ledger for a path that
// is empty, missing, or holds something that is not a ledger.
func loadNoticeLedger(path string) noticeLedger {
	var ledger noticeLedger
	if path == "" {
		return ledger
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ledger
	}
	if err := json.Unmarshal(raw, &ledger); err != nil {
		return noticeLedger{}
	}
	return ledger
}

// write puts the ledger on disk whole: a temporary file in the same directory,
// closed, then renamed over the old one. The directory is made if the profile
// has never written anything, with the mode the profile's other files use.
func (l noticeLedger) write(path string) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, ".notices-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	keep = true
	return nil
}

// retired says whether the notice is closed for good.
func (l noticeLedger) retired(id string) bool {
	return l.Seen[id].Retired != ""
}

// shown is how many sessions the notice has been shown in.
func (l noticeLedger) shown(id string) int {
	return l.Seen[id].Shown
}

// show counts one more session's showing and returns the new count.
func (l *noticeLedger) show(id string) int {
	if l.Seen == nil {
		l.Seen = map[string]noticeMark{}
	}
	mark := l.Seen[id]
	mark.Shown++
	l.Seen[id] = mark
	return mark.Shown
}

// retire closes the notice, stamped with the moment. A notice retired twice
// keeps its first stamp: the first time is the fact.
func (l *noticeLedger) retire(id string) {
	if l.Seen == nil {
		l.Seen = map[string]noticeMark{}
	}
	mark := l.Seen[id]
	if mark.Retired == "" {
		mark.Retired = time.Now().UTC().Format(time.RFC3339)
	}
	l.Seen[id] = mark
}
