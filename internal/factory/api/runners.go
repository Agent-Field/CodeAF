package api

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// These are the S1/S2 factory and runner words, shared by reads and writes.
const (
	FactorySolo        = "solo"
	FactoryTeam        = "team"
	DefaultRunnerSlots = 2
	// MaxRunnerSlots is the most jobs one runner may be told to run at once,
	// and the one number the server, the worker and the local runner share.
	// Eight is a DELIBERATE PRODUCT AND SAFETY POLICY, not a measurement: each
	// slot is a credential, a git worktree and a stage conversation inside one
	// process, and a bounded number keeps one owner's click from fanning a
	// machine out without limit. It happens to equal surfaceMaxProcs in
	// cmd/codeaf/main.go, but that is a CPU scheduler width and is NOT evidence
	// that more jobs would run badly; no benchmark here says either way.
	// Raising it is a decision about that policy, made in this one constant.
	//
	// COMPATIBILITY: before this ceiling existed a runner could save any
	// number. A saved row above the ceiling is read as the ceiling (the
	// registry load and every attach), so the roster, GET /v1/runner/self and
	// the worker all report the capacity that is actually granted. Leases
	// already held above it are never cancelled; they finish and the extra
	// slots do not refill.
	MaxRunnerSlots  = 8
	RunnerThisMac   = "this_mac"
	RunnerMac       = "mac"
	RunnerCI        = "ci"
	RunnerIdle      = "idle"
	RunnerBusy      = "busy"
	RunnerOffline   = "offline"
	RunnerAsleep    = "asleep"
	PersonActive    = "active"
	PersonAway      = "away"
	PersonInvited   = "invited"
	PersonIdleAfter = 15 * time.Minute
)

// Checkout is a repository already present on this runner, never a server path.
type Checkout struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
}

// Runner is one computer and its owner's capacity, folded across execution slots.
type Runner struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Owner     string     `json:"owner"`
	Kind      string     `json:"kind"`
	Slots     int        `json:"slots"`
	State     string     `json:"state"`
	Checkouts []Checkout `json:"checkouts"`
	Jobs      []int      `json:"jobs"`
	LastSeen  time.Time  `json:"last_seen"`
	// LiveCapacity is true only when this runner's worker attached saying it
	// follows a saved capacity change while running. Absent (false) is an
	// older worker, or one that cannot ask. For such a runner a raise is NOT
	// promised by a restart alone: an older binary stays at its fixed 2 however
	// often it restarts, so the screen may say only that it needs a compatible
	// update or configuration.
	LiveCapacity bool `json:"live_capacity,omitempty"`
}

// Runners is the factory directory; it contains no execution credentials.
type Runners struct {
	Factory Factory  `json:"factory"`
	Runners []Runner `json:"runners"`
	// People holds the signed-in GitHub name and photo of each owner listed in
	// Runners. A handle with no verified profile is absent, never blank.
	People map[string]factory.PersonSummary `json:"people,omitempty"`
}

// RunnerPresence is sent before macOS sleep and after wake by the native app.
type RunnerPresence struct {
	Asleep bool `json:"asleep"`
}

// RunnerSettings changes this owner's runner capacity and local checkout facts.
// Slots is required and runs 1..MaxRunnerSlots. Checkouts absent (null or not
// sent) leaves the saved list alone; an empty list clears it.
type RunnerSettings struct {
	Slots     int        `json:"slots"`
	Checkouts []Checkout `json:"checkouts"`
}

// RunnerSelf is what a runner's own credential may read about itself: the
// capacity its owner saved. It names no other runner and no other owner.
type RunnerSelf struct {
	Slots int `json:"slots"`
}

// People is absent as a section in solo mode and populated only in a team.
type People struct {
	Factory Factory  `json:"factory"`
	Mode    string   `json:"mode"`
	People  []Person `json:"people"`
}

// Runner execution shares the lease protocol while older callers retain their Go names.
type RunnerAttach = WorkerAttach
type RunnerAttached = WorkerAttached
type RunnerLease = WorkerLease
type RunnerPull = WorkerPull
type RunnerHeartbeat = WorkerHeartbeat
type RunnerReport = WorkerReport
type RunnerDone = WorkerDone

// RunnerEvent is a small presence notification, without local paths or execution secrets.
type RunnerEvent struct {
	Runner   string    `json:"runner"`
	Owner    string    `json:"owner"`
	State    string    `json:"state"`
	LastSeen time.Time `json:"last_seen"`
}
