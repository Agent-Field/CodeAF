package lane

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ── WHAT THE ACCOUNT ITSELF WILL NOT REACH ──────────────────────────────────
//
// The serving set's negative half (sheet.go's [RefuseServing]) is a fact about
// ONE MODEL: this router will not serve that model from that machine, for now.
// There is a second kind of refusal that is not about a model at all, and it
// was filed as though it were.
//
// ── THE MEASURED FAILURE (2026-09-09 → 2026-09-10) ──────────────────────────
//
// The owner's account has the router's "paid model training" privacy switch
// off, which removes every machine that trains on paid inputs from EVERY
// request the account makes, whatever the model. The router says so when a
// request's whole set was such a machine:
//
//	0 endpoints out of 1 requested are available matching your guardrail
//	restrictions and data policy. … Paid model training violation (account
//	settings): 1 endpoint excluded; configurable at …/settings/privacy
//
// and its metadata carries the same fact as structure —
// `ineligibility_reasons: [{reason: "paid-model-training-violation-by-account",
// configure_url: …}]`. This process filed it as a thirty-minute refusal of ONE
// lane for ONE model, in memory: so the next model demanded the same machine and
// paid the same 404, and a fresh process sixteen hours later paid it again for
// the same machine (Wafer, twice; Fireworks and DeepSeek inside one race). And
// it was worse than a wasted round trip: a race that walks from one of these
// refusals to the next spends its arms on machines that were never going to be
// asked.
//
// ── THE LAW ─────────────────────────────────────────────────────────────────
//
// A MACHINE THE ACCOUNT'S OWN SETTINGS EXCLUDE IS EXCLUDED FOR EVERY MODEL, AND
// THE NEXT PROCESS KNOWS IT. [Serves] reads this set for every model, so the
// frontier never ranks the machine, the walk never demands it and the refusal
// door never predicts a walk onto it. It is written to the state root beside the
// lane ledger, because the router's own sentence says the fact lives in a
// SETTING and a setting outlives a process.
//
// IT IS NEVER A BAN. Two things take a machine back: [accountExclusionHold]
// passing, and — sooner — any answer that machine SERVES for this process
// ([ServedForAccount]), which is the router telling us the setting has changed.
// Neither costs a request: a request that does not demand anything can still
// be served from an excluded machine by the router's own free choice (the
// endpoint ladder's first rung is exactly such a request), so the set can only
// ever keep a machine from being DEMANDED, never from answering.

// accountExclusionHold is how long an account's exclusion of a machine is
// believed without being heard again.
//
// A DAY, because what it records is a SETTING. Half an hour — the serving set's
// hold — is right for a router's view of one model, which really does move, and
// wrong for a switch on a settings page: it bought the same 404 back at the top
// of every conversation. A day is long enough that no working day pays it twice
// and short enough that a person who flips the switch without this process
// seeing an answer from that machine has it back by tomorrow.
const accountExclusionHold = 24 * time.Hour

// accountExclusionsFile is where the set sleeps, under the state root.
const accountExclusionsFile = "account-exclusions.json"

// accountExcluded is the set: lane (lowercase) → the moment its exclusion stops
// counting. It is package state for the reason [refusedLanes] is — a fact
// about the router and the account, not about whichever sheet a build installed.
var accountExcluded struct {
	sync.RWMutex
	until map[string]time.Time
}

// accountWire is the file's shape: lane → until. The reason word travels with
// it so a person opening the file can tell why a machine is in it.
type accountWire struct {
	Lanes map[string]accountEntry `json:"lanes"`
}

type accountEntry struct {
	Until  time.Time `json:"until"`
	Reason string    `json:"reason,omitempty"`
}

// ExcludeForAccount writes one machine OUT of every model's serving set,
// because the router said the account's own settings exclude it, and saves the
// set so the next process starts knowing.
//
// It is called by the layer that classified the refusal (internal/provider's
// refusal door) and by nothing else. An unnamed lane writes nothing: an
// exclusion credited to nobody would take a machine away from nobody, and a
// blank key in the file would be a row nobody can read.
//
// THE WRITE IS SYNCHRONOUS AND THAT IS DELIBERATE. It happens at most once per
// machine per [accountExclusionHold] — the whole point of the set is that the
// refusal which teaches it is not paid twice — on a request that has already
// spent a round trip being refused, and a scheduled write would leave a test's
// home directory with a writer still in it.
func ExcludeForAccount(lane, reason string) {
	name := strings.ToLower(strings.TrimSpace(lane))
	if name == "" {
		return
	}
	accountExcluded.Lock()
	if accountExcluded.until == nil {
		accountExcluded.until = map[string]time.Time{}
	}
	accountExcluded.until[name] = time.Now().Add(accountExclusionHold)
	snapshot := accountSnapshotLocked(reason, name)
	accountExcluded.Unlock()
	saveAccountExclusions(snapshot, "")
}

// ServedForAccount takes a machine back the moment it answers for this
// process: the router serving from it is proof the account can reach it, which
// is what a person flipping the setting back looks like from here. A machine
// that was never excluded costs one read lock.
func ServedForAccount(lane string) {
	name := strings.ToLower(strings.TrimSpace(lane))
	if name == "" {
		return
	}
	accountExcluded.RLock()
	_, held := accountExcluded.until[name]
	accountExcluded.RUnlock()
	if !held {
		return
	}
	accountExcluded.Lock()
	delete(accountExcluded.until, name)
	snapshot := accountSnapshotLocked("", "")
	accountExcluded.Unlock()
	saveAccountExclusions(snapshot, name)
}

// AccountExcludes reports whether the account's own settings are believed to
// keep this machine from every model. [Serves] is its one reader that matters.
func AccountExcludes(lane string) bool {
	name := strings.ToLower(strings.TrimSpace(lane))
	if name == "" {
		return false
	}
	accountExcluded.RLock()
	until, held := accountExcluded.until[name]
	accountExcluded.RUnlock()
	return held && time.Now().Before(until)
}

// LoadAccountExclusions folds the saved set into this process's. It is called
// when a sheet is wired — the moment a process first says which router it is
// talking to — and calling it again is harmless: a read only ADDS, and keeps
// the later of two moments for a machine both sides know, so nothing this
// process learned is ever dropped by a read.
func LoadAccountExclusions() {
	path := accountExclusionsPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var saved accountWire
	if json.Unmarshal(data, &saved) != nil {
		return
	}
	now := time.Now()
	accountExcluded.Lock()
	defer accountExcluded.Unlock()
	if accountExcluded.until == nil {
		accountExcluded.until = map[string]time.Time{}
	}
	for lane, entry := range saved.Lanes {
		name := strings.ToLower(strings.TrimSpace(lane))
		if name == "" || !now.Before(entry.Until) {
			continue
		}
		if known, ok := accountExcluded.until[name]; !ok || entry.Until.After(known) {
			accountExcluded.until[name] = entry.Until
		}
	}
}

// ForgetAccountExclusionsInMemory empties the set this process holds and leaves
// the file alone. It is for tests, and it is the one way a test can stage "the
// next process on the same home".
func ForgetAccountExclusionsInMemory() {
	accountExcluded.Lock()
	defer accountExcluded.Unlock()
	accountExcluded.until = nil
}

// accountExclusionsPath is the file under the state root, and "" for a test
// binary that merely inherited a root (undertest.go): a suite must never
// write a person's account facts, nor read them into its own choices.
func accountExclusionsPath() string {
	return stateFile("v3", accountExclusionsFile)
}

// accountSnapshotLocked copies the live set, dropping what has expired, with
// the reason word attached to the machine that was just written.
func accountSnapshotLocked(reason, named string) accountWire {
	now := time.Now()
	wire := accountWire{Lanes: map[string]accountEntry{}}
	for lane, until := range accountExcluded.until {
		if !now.Before(until) {
			continue
		}
		entry := accountEntry{Until: until}
		if lane == named {
			entry.Reason = strings.TrimSpace(reason)
		}
		wire.Lanes[lane] = entry
	}
	return wire
}

// saveAccountExclusions writes the set, atomically, and `cleared` is a machine
// this write is taking OUT. A failure is silent: the set is a cache of what the
// router will say again, and a set that could not be saved costs the next
// process one refusal, which is what it cost before the file existed.
//
// IT MERGES WITH WHAT IS ALREADY ON DISK, because two processes share one home
// and each loaded the file when it started: a write of this process's set alone
// would drop a machine the other one learned since. Everything unexpired on disk
// is kept unless it is the machine being cleared, and the later moment wins for
// a machine both sides know.
func saveAccountExclusions(wire accountWire, cleared string) {
	path := accountExclusionsPath()
	if path == "" {
		return
	}
	now := time.Now()
	if data, err := os.ReadFile(path); err == nil {
		var saved accountWire
		if json.Unmarshal(data, &saved) == nil {
			for lane, entry := range saved.Lanes {
				lane = strings.ToLower(strings.TrimSpace(lane))
				if lane == "" || lane == cleared || !now.Before(entry.Until) {
					continue
				}
				mine, known := wire.Lanes[lane]
				switch {
				case !known:
					wire.Lanes[lane] = entry
				case mine.Reason == "" || entry.Until.After(mine.Until):
					if entry.Until.After(mine.Until) {
						mine.Until = entry.Until
					}
					if mine.Reason == "" {
						mine.Reason = entry.Reason
					}
					wire.Lanes[lane] = mine
				}
			}
		}
	}
	data, err := json.MarshalIndent(wire, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = writeAtomic(path, data)
}
