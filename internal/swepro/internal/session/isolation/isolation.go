// Package isolation ports src/session/isolation.ts:1-221 from swe-pro
// (commit 3b25a1a).
//
// The scheduler gives each dispatched leaf an isolated workspace. Historically
// that was always a git worktree, sharing the main repository's object store.
// Furrow can instead make a fast whole-workspace copy-on-write fork.
//
// The shipped default remains worktree because a Furrow fork also copies
// .git: commits made in it live in an independent object store, and its target
// ref is frozen at fork time. The existing merge path risk-orders leaves,
// rebases onto the live target, merges plandb/<id> from the shared object store,
// invokes an LLM merger when needed, and verifies the result. A Furrow fork
// violates those assumptions. Bridging it would require re-plumbing that stack
// or replacing it with Furrow's merge, so this is not yet a cost-neutral
// optimization. Auto is nevertheless fully wired; once merge-back is bridged,
// changing DefaultIsolation to auto is the only policy change required.
package isolation

import (
	"os"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// IsolationBackend is the CODEAF_ISOLATION backend union.
type IsolationBackend string

const (
	BackendAuto     IsolationBackend = "auto"
	BackendWorktree IsolationBackend = "worktree"
	BackendFurrow   IsolationBackend = "furrow"

	// DefaultIsolation is the effective backend when CODEAF_ISOLATION is
	// unset or empty. See the package comment for why it is not auto yet.
	DefaultIsolation IsolationBackend = BackendWorktree
)

// IsolationResolution is resolveIsolationBackend's return object.
type IsolationResolution struct {
	Backend    IsolationBackend `json:"backend"`
	Recognized bool             `json:"recognized"`
	Raw        string           `json:"raw"`
}

// ResolveIsolationBackend resolves CODEAF_ISOLATION from env. A nil map is the
// same as an environment without the key. Values are JS-trimmed and matched
// case-insensitively; an unrecognized value returns the default and is flagged
// so the caller can log it.
func ResolveIsolationBackend(env map[string]string) IsolationResolution {
	raw := jscompat.Trim(env["CODEAF_ISOLATION"])
	switch norm := strings.ToLower(raw); IsolationBackend(norm) {
	case BackendAuto, BackendWorktree, BackendFurrow:
		return IsolationResolution{Backend: IsolationBackend(norm), Recognized: true, Raw: raw}
	default:
		return IsolationResolution{
			Backend:    DefaultIsolation,
			Recognized: raw == "",
			Raw:        raw,
		}
	}
}

// ResolveIsolationBackendFromEnv is ResolveIsolationBackend with process.env
// as its source, matching the TS function's default argument.
func ResolveIsolationBackendFromEnv() IsolationResolution {
	raw, _ := os.LookupEnv("CODEAF_ISOLATION")
	return ResolveIsolationBackend(map[string]string{"CODEAF_ISOLATION": raw})
}

// IsolationOptions contains the pure policy signals supplied by the scheduler.
type IsolationOptions struct {
	Requested        IsolationBackend `json:"requested"`
	RequiresMerge    bool             `json:"requiresMerge"`
	AdapterAvailable bool             `json:"adapterAvailable"`
	CowSupported     bool             `json:"cowSupported"`
}

// IsolationDecision mirrors the TS discriminated union. Reason is absent for
// the Furrow variant, just as JSON.stringify omits that undeclared property.
type IsolationDecision struct {
	Backend IsolationBackend `json:"backend"`
	Reason  string           `json:"reason,omitempty"`
}

// DecideIsolation applies the source's load-bearing gate order: an explicitly
// forced worktree, merge correctness, adapter availability, then COW support.
func DecideIsolation(opts IsolationOptions) IsolationDecision {
	if opts.Requested == BackendWorktree {
		return IsolationDecision{Backend: BackendWorktree, Reason: "forced"}
	}
	if opts.RequiresMerge {
		return IsolationDecision{Backend: BackendWorktree, Reason: "merge-path-git-native"}
	}
	if !opts.AdapterAvailable {
		return IsolationDecision{Backend: BackendWorktree, Reason: "furrow-adapter-unavailable"}
	}
	if !opts.CowSupported {
		return IsolationDecision{Backend: BackendWorktree, Reason: "cow-unsupported"}
	}
	return IsolationDecision{Backend: BackendFurrow}
}

// CowProbe is the injectable filesystem capability check used by
// CowSupportProber. Returning false is the safe path.
type CowProbe func(baseDir string) bool

// CowSupportProber memoizes one COW result process-wide (or per injected test
// instance). Like the TS cache, the first baseDir wins.
type CowSupportProber struct {
	mu       sync.Mutex
	probe    CowProbe
	measured bool
	result   bool
}

// NewCowSupportProber constructs a memoized prober around an injectable probe.
// A nil probe uses the real filesystem FICLONE attempt.
func NewCowSupportProber(probe CowProbe) *CowSupportProber {
	if probe == nil {
		probe = runCowProbe
	}
	return &CowSupportProber{probe: probe}
}

// Probe returns the cached result or runs the injected probe once. A panic in
// a test/custom probe is treated like the TS probe's caught exception: false.
func (p *CowSupportProber) Probe(baseDir string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.measured {
		return p.result
	}
	p.result = safeCowProbe(p.probe, baseDir)
	p.measured = true
	return p.result
}

func safeCowProbe(probe CowProbe, baseDir string) (result bool) {
	defer func() {
		if recover() != nil {
			result = false
		}
	}()
	return probe(baseDir)
}

var (
	defaultCowProberMu sync.Mutex
	defaultCowProber   = NewCowSupportProber(nil)
)

// ProbeCowSupport performs the memoized real-filesystem probe. With no
// argument it uses os.TempDir, matching the TS default parameter; extra
// arguments are ignored just as extra JavaScript arguments are.
func ProbeCowSupport(baseDirs ...string) bool {
	baseDir := os.TempDir()
	if len(baseDirs) > 0 {
		baseDir = baseDirs[0]
	}
	defaultCowProberMu.Lock()
	prober := defaultCowProber
	defaultCowProberMu.Unlock()
	return prober.Probe(baseDir)
}

// ResetCowProbeCacheForTest clears the package-wide memoized answer.
func ResetCowProbeCacheForTest() {
	defaultCowProberMu.Lock()
	defer defaultCowProberMu.Unlock()
	defaultCowProber = NewCowSupportProber(nil)
}

// DispatchEnvelopeStatus is the resource degradation action.
type DispatchEnvelopeStatus string

const (
	StatusProceed   DispatchEnvelopeStatus = "proceed"
	StatusSerialize DispatchEnvelopeStatus = "serialize"
	StatusPause     DispatchEnvelopeStatus = "pause"
)

// EnvelopeReading is one normalized resource measurement. JSNumber preserves
// JSON.stringify's treatment and spelling of all JavaScript number values.
type EnvelopeReading struct {
	Resource   string            `json:"resource"`
	OK         bool              `json:"ok"`
	HeadroomGB jscompat.JSNumber `json:"headroomGB"`
	FloorGB    jscompat.JSNumber `json:"floorGB"`
}

// ReadingToStatus applies the policy for one reading.
func ReadingToStatus(r EnvelopeReading) DispatchEnvelopeStatus {
	floor := float64(r.FloorGB)
	headroom := float64(r.HeadroomGB)
	if floor == 0 {
		return StatusProceed
	}
	if headroom < 0 {
		return StatusProceed
	}
	if r.OK {
		return StatusProceed
	}
	if headroom < floor/2 {
		return StatusPause
	}
	return StatusSerialize
}

// DispatchEnvelopeDecision is decideDispatchEnvelope's return object. Reading
// is absent when every resource says proceed, matching the TS object shape.
type DispatchEnvelopeDecision struct {
	Status  DispatchEnvelopeStatus `json:"status"`
	Reading *EnvelopeReading       `json:"reading,omitempty"`
}

var statusRank = map[DispatchEnvelopeStatus]int{
	StatusProceed:   0,
	StatusSerialize: 1,
	StatusPause:     2,
}

// DecideDispatchEnvelope combines readings using "worst wins." Ties retain the
// first driver because the source replaces the result only on a strict rank
// increase.
func DecideDispatchEnvelope(readings []EnvelopeReading) DispatchEnvelopeDecision {
	worst := DispatchEnvelopeDecision{Status: StatusProceed}
	for i := range readings {
		status := ReadingToStatus(readings[i])
		if statusRank[status] > statusRank[worst.Status] {
			worst = DispatchEnvelopeDecision{Status: status, Reading: &readings[i]}
		}
	}
	return worst
}
