package api

import "time"

// These are the S1/S2 factory and runner words, shared by reads and writes.
const (
	FactorySolo        = "solo"
	FactoryTeam        = "team"
	DefaultRunnerSlots = 2
	RunnerThisMac      = "this_mac"
	RunnerMac          = "mac"
	RunnerCI           = "ci"
	RunnerIdle         = "idle"
	RunnerBusy         = "busy"
	RunnerOffline      = "offline"
	RunnerAsleep       = "asleep"
	PersonActive       = "active"
	PersonAway         = "away"
	PersonInvited      = "invited"
	PersonIdleAfter    = 15 * time.Minute
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
}

// Runners is the factory directory; it contains no execution credentials.
type Runners struct {
	Factory Factory  `json:"factory"`
	Runners []Runner `json:"runners"`
}

// RunnerPresence is sent before macOS sleep and after wake by the native app.
type RunnerPresence struct {
	Asleep bool `json:"asleep"`
}

// RunnerSettings changes this owner's runner capacity and local checkout facts.
type RunnerSettings struct {
	Slots     int        `json:"slots"`
	Checkouts []Checkout `json:"checkouts"`
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
