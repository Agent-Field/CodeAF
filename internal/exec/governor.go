package exec

import (
	"runtime"
	"strings"
	"sync"
	"time"
)

// The admission doctrine. It used to be one rule for every leaf, and the rule
// was this machine's load average — which was the wrong question for almost
// every leaf aforge runs. A generalist leaf is a goroutine parked on a socket
// waiting for a model to answer: it costs a goroutine and a file handle, and
// it contributes essentially nothing to load average. Gating it on load
// average meant the only leaves that could ever be admitted were the handful
// the starvation floor guaranteed, because nothing a socket-parked leaf does
// can bring somebody else's reading back down. The floor was the ceiling.
//
// That was measured, not theorised: peak concurrency was three leaves in seven
// of eight live runs, and one job held thirteen ready leaves behind three
// running ones for twenty minutes on a sixteen-core machine that was otherwise
// idle. The user's summary of the fix is the doctrine now: there is no local
// load to speak of, the only load is the API's.
//
// So there are two classes of leaf, and only one of them loads this host.
//
//   - API-bound leaves — the generalist and everything shaped like it — are
//     admitted on dependency structure alone: a node runs as soon as the nodes
//     it needs have landed. The only bound is a backstop against pathological
//     fan-out, and it counts handles and goroutines rather than cores. The real
//     back-pressure for this class lives where the real resource is: the
//     provider's own adaptive limiter, which reads 429s and the provider's
//     backoff signals and is the only thing here that knows what the account
//     will sustain. Admission must not pre-empt it — a leaf refused here never
//     reaches the limiter, so the limiter never learns the account had room.
//
//   - Local-work leaves — the worker that spawns real compilers and real test
//     runs on this machine. For those the old doctrine is exactly right and is
//     kept verbatim: a CPU-derived cap, and load average per core with
//     hysteresis above a small floor. That protection came from a real
//     incident, aforge pinning a laptop's fan, and the incident was local
//     processes — precisely this class and only this class.
//
// Nothing running is ever cancelled and nothing already claimed is delayed;
// back-pressure applies only to admitting the next leaf, and a refusal is a
// decision about one leaf, never about the pass it was refused in.
const (
	// GovernorInFlightCeiling is a backstop, not a scheduler. It exists so a
	// pathological fan-out — a graph that goes a thousand leaves wide, a
	// runaway splice — cannot exhaust file handles or goroutine budget. It is
	// deliberately far above any width a real plan produces, so that in
	// ordinary operation it decides nothing at all: the shape of the graph is
	// what is supposed to decide how much runs at once. If this number is ever
	// the thing a run is waiting on, the interesting bug is upstream of here.
	GovernorInFlightCeiling = 64

	// GovernorLoadCeiling pauses claiming LOCAL-WORK leaves. More than one and
	// a half runnable threads per core means the scheduler is handing out
	// slices rather than running work, and one more compile makes every
	// compile slower. It says nothing about a leaf waiting on a socket.
	GovernorLoadCeiling = 1.5
	// GovernorLoadResume is the lower edge of the hysteresis band. Load average
	// is an exponentially decayed figure that hovers; resuming at the value it
	// paused at would admit and pause on alternating samples.
	GovernorLoadResume = 1.2
	// governorSampleTTL bounds how often the host is asked. The one-minute
	// average cannot move faster than this, and the claim path ticks in
	// hundreds of milliseconds.
	governorSampleTTL = time.Second
	// GovernorLocalFloor is the starvation guard under the local-work gate:
	// however loaded the machine is, this many local-work leaves may always be
	// claimed. Someone else's compile must never leave aforge running nothing
	// at all. It is a floor and nothing else — it was never meant to be the
	// number of leaves aforge runs, and for one release it was exactly that.
	GovernorLocalFloor = 3
)

// LocalWorkSubharness reports whether leaves of this worker do real work on
// this machine — compilers, test binaries, shells — rather than parking on a
// socket. It is the only question the load governor is still allowed to ask,
// and it is asked here so that no dispatch path has to know a worker by name.
func LocalWorkSubharness(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), SWESubharness)
}

// LocalLeafCap is the CPU-derived ceiling on leaves that do local work. Genuine
// host work is worth counting against the host's own cores; it never applies to
// API-bound leaves, which are bounded by GovernorInFlightCeiling alone.
func LocalLeafCap() int {
	cores := runtime.NumCPU()
	if cores < GovernorLocalFloor {
		return GovernorLocalFloor
	}
	return cores
}

// Governor is the admission gate on new leaves. It never blocks: a refusal is
// a decision the caller's next tick re-asks, so there is no hold to bound and
// no way for the gate to wedge a runner that is otherwise ready to work.
type Governor struct {
	mu       sync.Mutex
	sample   func() (float64, bool)
	now      func() time.Time
	localCap int
	holding  bool
	load     float64
	loaded   bool
	at       time.Time
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
	return &Governor{sample: sample, now: now, localCap: LocalLeafCap()}
}

// WithLocalCap replaces the CPU-derived cap on local-work leaves. Production
// uses the host's own core count; a caller that knows better — and a test that
// must assert the clamp without owning the machine's core count — says so here.
func (g *Governor) WithLocalCap(leaves int) *Governor {
	if g != nil && leaves > 0 {
		g.localCap = leaves
	}
	return g
}

// hostGovernor is process-global for the same reason the provider's limiter
// is: chat's runner, a headless runner, and anything else claiming leaves in
// this process are all loading one machine.
var hostGovernor = NewGovernor()

// HostGovernor is the shared gate every claim path consults.
func HostGovernor() *Governor { return hostGovernor }

// Admit reports whether one more API-bound leaf may be claimed. inFlight is how
// many leaves the caller is already running.
//
// Host load is deliberately absent from this decision. A leaf of this class is
// a socket and a goroutine; the resource it consumes belongs to the provider,
// not to this machine, and the provider's limiter is what adapts to it. The
// only thing asked here is whether the process is about to run out of the
// cheap local resources a socket does cost.
func (g *Governor) Admit(inFlight int) bool {
	return inFlight < GovernorInFlightCeiling
}

// AdmitLocal reports whether one more leaf that spawns real local processes may
// be claimed. inFlight is every leaf the caller is running; localInFlight is
// the share of them in this class, which is what the host actually feels.
func (g *Governor) AdmitLocal(inFlight, localInFlight int) bool {
	// The backstop binds every class: it is about handles and goroutines, and
	// a compile needs both too.
	if inFlight >= GovernorInFlightCeiling {
		return false
	}
	if g == nil {
		return true
	}
	// Starvation guard: someone else's load must never leave aforge running
	// nothing at all. The user asked for work, so the floor always gets
	// through no matter how saturated the machine is — and the host is not
	// even asked, since no answer it could give would change this.
	if localInFlight < GovernorLocalFloor {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	// Genuine host work, counted against the host's own cores. A load average
	// is a minute old; eight compiles started in one pass are eight compiles
	// before the reading moves at all, so the cap is the part that binds first.
	if localInFlight >= g.localCap {
		return false
	}
	load, known := g.sampleLocked()
	if !known {
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
//
// It is read on one path only, AdmitLocal. Nothing about an API-bound leaf is
// decided by it, which is the whole of the fix above.
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
