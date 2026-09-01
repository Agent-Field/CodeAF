package exec

// The admission doctrine. It used to be one rule for every leaf, and the rule
// was this machine's load average — which was the wrong question for every leaf
// aforge runs. A leaf is a goroutine parked on a socket waiting for a model to
// answer: it costs a goroutine and a file handle, and it contributes essentially
// nothing to load average. Gating it on load average meant the only leaves that
// could ever be admitted were the handful a starvation floor guaranteed, because
// nothing a socket-parked leaf does can bring somebody else's reading back down.
// The floor was the ceiling.
//
// That was measured, not theorised: peak concurrency was three leaves in seven
// of eight live runs, and one job held thirteen ready leaves behind three
// running ones for twenty minutes on a sixteen-core machine that was otherwise
// idle. The user's summary of the fix is the doctrine now: there is no local
// load to speak of, the only load is the API's.
//
// So a leaf is admitted on dependency structure alone: a node runs as soon as
// the nodes it needs have landed. The only bound is a backstop against
// pathological fan-out, and it counts handles and goroutines rather than cores.
// The real back-pressure lives where the real resource is: the provider's own
// adaptive limiter, which reads 429s and the provider's backoff signals and is
// the only thing here that knows what the account will sustain. Admission must
// not pre-empt it — a leaf refused here never reaches the limiter, so the
// limiter never learns the account had room.
//
// Nothing running is ever cancelled and nothing already claimed is delayed;
// back-pressure applies only to admitting the next leaf, and a refusal is a
// decision about one leaf, never about the pass it was refused in.

// GovernorInFlightCeiling is a backstop, not a scheduler. It exists so a
// pathological fan-out — a graph that goes a thousand leaves wide, a runaway
// splice — cannot exhaust file handles or goroutine budget. It is deliberately
// far above any width a real plan produces, so that in ordinary operation it
// decides nothing at all: the shape of the graph is what is supposed to decide
// how much runs at once. If this number is ever the thing a run is waiting on,
// the interesting bug is upstream of here.
const GovernorInFlightCeiling = 64

// Governor is the admission gate on new leaves. It never blocks: a refusal is
// a decision the caller's next tick re-asks, so there is no hold to bound and
// no way for the gate to wedge a runner that is otherwise ready to work.
type Governor struct{}

// NewGovernor builds the gate. Callers share one: what is being bounded is a
// process-wide resource, and a per-runner gate would each count only its own
// share of it.
func NewGovernor() *Governor { return &Governor{} }

// hostGovernor is process-global for the same reason the provider's limiter
// is: chat's runner, a headless runner, and anything else claiming leaves in
// this process are all drawing on one account and one process.
var hostGovernor = NewGovernor()

// HostGovernor is the shared gate every claim path consults.
func HostGovernor() *Governor { return hostGovernor }

// Admit reports whether one more leaf may be claimed. inFlight is how many
// leaves the caller is already running.
//
// Host load is deliberately absent from this decision. A leaf is a socket and a
// goroutine; the resource it consumes belongs to the provider, not to this
// machine, and the provider's limiter is what adapts to it. The only thing
// asked here is whether the process is about to run out of the cheap local
// resources a socket does cost.
func (g *Governor) Admit(inFlight int) bool {
	return inFlight < GovernorInFlightCeiling
}
