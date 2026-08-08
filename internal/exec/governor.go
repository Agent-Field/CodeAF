package exec

import (
	"runtime"
	"sync"
	"time"
)

// The admission doctrine, one layer below the provider's rate limiter: that
// one adapts to what the account will sustain, this one to what the machine
// will. Worker count is network-bound and limits itself — the goroutines are
// parked on sockets — but a leaf's shell can spawn anything, and no fixed
// worker ceiling can see a recursive scan pinning a core. So the next claim
// asks the host's own answer instead of a constant: load average per core,
// which counts everything running on this machine including work aforge did
// not start. Nothing running is ever cancelled and nothing already claimed is
// delayed; back-pressure applies only to admitting the next leaf, and it is
// universal — the governor knows nothing about which command is expensive.
const (
	// GovernorLoadCeiling pauses claiming. More than one and a half runnable
	// threads per core means the scheduler is handing out slices rather than
	// running work, and one more leaf makes every leaf slower.
	GovernorLoadCeiling = 1.5
	// GovernorLoadResume is the lower edge of the hysteresis band. Load average
	// is an exponentially decayed figure that hovers; resuming at the value it
	// paused at would admit and pause on alternating samples.
	GovernorLoadResume = 1.2
	// governorSampleTTL bounds how often the host is asked. The one-minute
	// average cannot move faster than this, and the claim path ticks in
	// hundreds of milliseconds.
	governorSampleTTL = time.Second
	// GovernorMinInFlight is the floor under back-pressure: however loaded the
	// machine is, this many leaves may always be claimed.
	//
	// The floor used to be one, and one turned the product's central promise
	// into a lie on any busy machine. "Say five things and five jobs run" is
	// the journey; what actually happened, measured, was three independent
	// single-leaf jobs executing strictly one after another with a claim gap
	// between each — because a leaf is a goroutine parked on a socket, its own
	// load contribution is nil, and yet the *first* one raised inFlight to 1
	// and every sibling after it met a gate reading somebody else's compile.
	//
	// The ceiling is still what it was and still does what it was built for:
	// it stops the tenth leaf joining a shell that is already pinning cores.
	// This only says that the difference between a resident and a queue is
	// worth three sockets, and that host pressure is a reason to stop growing
	// the fan-out — never a reason to abolish it.
	GovernorMinInFlight = 3
)

// Governor is the admission gate on new leaves. It never blocks: a refusal is
// a decision the caller's next tick re-asks, so there is no hold to bound and
// no way for the gate to wedge a runner that is otherwise ready to work.
type Governor struct {
	mu      sync.Mutex
	sample  func() (float64, bool)
	now     func() time.Time
	holding bool
	load    float64
	loaded  bool
	at      time.Time
}

// NewGovernor reads real host pressure. Callers share one: the pressure being
// measured is the machine's, and a per-runner governor would each rediscover
// the same number.
func NewGovernor() *Governor {
	return newGovernor(hostLoadPerCore, time.Now)
}

// NewGovernorFrom builds a gate over an injected reading of load per core.
// The bool is the host's honesty: false means "this platform cannot say", and
// the governor never gates on silence.
func NewGovernorFrom(sample func() (loadPerCore float64, known bool)) *Governor {
	return newGovernor(sample, time.Now)
}

func newGovernor(sample func() (float64, bool), now func() time.Time) *Governor {
	return &Governor{sample: sample, now: now}
}

// hostGovernor is process-global for the same reason the provider's limiter
// is: chat's runner, a headless runner, and anything else claiming leaves in
// this process are all loading one machine.
var hostGovernor = NewGovernor()

// HostGovernor is the shared gate every claim path consults.
func HostGovernor() *Governor { return hostGovernor }

// Admit reports whether one more leaf may be claimed. inFlight is how many the
// caller is already running.
func (g *Governor) Admit(inFlight int) bool {
	// Starvation guard: someone else's load must never leave aforge running
	// nothing at all, and it must never collapse a fan-out into a queue. The
	// user asked for work, so the floor always gets through no matter how
	// saturated the machine is.
	if inFlight < GovernorMinInFlight {
		return true
	}
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	load, ok := g.sampleLocked()
	if !ok {
		// A platform that cannot report pressure is not evidence of pressure.
		return true
	}
	if g.holding {
		if load < GovernorLoadResume {
			g.holding = false
		}
	} else if load > GovernorLoadCeiling {
		g.holding = true
	}
	return !g.holding
}

func (g *Governor) sampleLocked() (float64, bool) {
	now := g.now()
	if g.loaded && now.Sub(g.at) < governorSampleTTL {
		return g.load, true
	}
	load, ok := g.sample()
	if !ok {
		return 0, false
	}
	g.load, g.loaded, g.at = load, true, now
	return load, true
}

// hostLoadPerCore is the machine's own answer to "am I oversubscribed": the
// one-minute load average over the cores that can serve it. Free memory is
// deliberately not part of the reading — every cheap route to it on darwin
// goes through cgo, and a runaway shell shows up as CPU pressure first. A
// platform that cannot answer reports false and never gates anything.
func hostLoadPerCore() (float64, bool) {
	one, ok := loadAverageOne()
	if !ok {
		return 0, false
	}
	cores := runtime.NumCPU()
	if cores <= 0 {
		return 0, false
	}
	return one / float64(cores), true
}
