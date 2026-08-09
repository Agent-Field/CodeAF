// Package capability is a bug-for-bug port of src/session/capability.ts — the
// T3 capability estimator: an online, per-(modelID, size-band) Beta-posterior
// success estimator that tells the cut policy how big a chunk each model can
// reliably finish.
//
// The TS core is pure: no clock, no RNG, no I/O (only the two persistence
// wrappers at the bottom touch the filesystem). There is therefore NO
// SetClockForTesting hook in this package — there is nothing to inject.
//
// Fidelity notes (deliberate, do NOT "fix"):
//
//   - Decay accumulates in the TS expression shape, literally:
//     `cell.a = decay*cell.a + successW*successMass`. Reassociating it changes
//     the last bits.
//
//   - Cross-band propagation is asymmetric BY DIRECTION, not by band: success
//     mass informs SMALLER bands with crossStrong and larger ones with
//     crossWeak; failure mass is the mirror image. Distance 0 uses
//     Math.pow(x, 0) == 1 for both, so the observed cell always takes the full
//     (c, 1-c) update.
//
//   - The reported curve is the raw posterior means projected onto
//     non-increasing by a RUNNING MINIMUM taken from the SMALL end (xs → xl).
//
//   - The observe() evidence guard uses optional chaining
//     (`outcome.evidence?.auditCommandsRun === 0`). An ABSENT evidence object
//     yields `undefined === 0` → false, so the record is NOT skipped. Evidence
//     is a pointer here and its numeric fields are pointers so nil behaves like
//     JS undefined rather than like a zero struct.
//
//   - Cells are keyed by modelID ALONE (capability.ts:166,:344) even though the
//     public API takes a ModelRef with a providerID. Two providers serving the
//     same modelID share one set of cells. Kept.
//
//   - The T6 streak bonus targets `bandFloor + 1` where bandFloor is the
//     MINIMUM (smallest) band index seen during the streak, although the
//     comment at capability.ts:239 calls it "the largest band the streak
//     actually proved". Suspected bug; kept verbatim.
//
//   - Every table in the TS module (BAND_INDEX, VERDICT_CREDIT, PRIOR_MEAN and
//     the caller-supplied tierMap) is a PLAIN OBJECT, so a lookup walks
//     Object.prototype. The twelve inherited keys are therefore NOT undefined
//     and slip past the `=== undefined` guards; see bandIndex/verdictCredit/
//     priorMeanFor and jsvalue.go. Reachable and fixtured.
//
//   - `PRIOR_MEAN[tier][band]` THROWS a TypeError when `tier` is neither
//     "high"/"low" nor an Object.prototype key — an unlisted tierMap value, an
//     exotic defaultTier, or a modelID that is itself an Object.prototype key
//     (the tierMap lookup then returns an inherited function). Go panics at the
//     same point; nothing in TS catches it either.
//
//   - `snapshot()` assigns into an object literal, so a model literally named
//     "__proto__" is swallowed by the prototype setter and never appears in the
//     snapshot; array-index-like modelIDs ("2", "10") are emitted FIRST in
//     ascending numeric order. See jsvalue.go.
//
// KNOWN UNPORTABLE DIVERGENCE (capability.ts:271-275): `pSuccess(model, band)`
// with a band that is an Object.prototype key evaluates `curve(...)[<function>]`
// and returns JS `undefined` from a function typed `-> number`. Go returns NaN.
// Both stringify to `null`, so the golden fixtures still gate byte-for-byte;
// only a direct Go caller can tell them apart.
//
// Not concurrency-safe, matching the TS class: one tracker belongs to one run's
// scheduler loop.
//
// See leafoutcome.go in this package for why LeafOutcome is a local, strictly
// more defensive struct than internal/session/leafoutcome's (and for the
// FromLeafOutcome seam between them).
package capability

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/adaptiveflag"
	"github.com/Agent-Field/swe-pro-go/internal/session/sizeband"
)

// ---------------------------------------------------------------------------
// BANDS, ordered smallest → largest. Index is the x-axis of the capability
// curve; monotonicity is expressed against this ordering.
//
// TS marks it `readonly`; Go has no such guarantee for a package-level slice —
// do not mutate it.
var BANDS = []sizeband.SizeBand{
	sizeband.BandXS,
	sizeband.BandS,
	sizeband.BandM,
	sizeband.BandL,
	sizeband.BandXL,
}

var bandIndexTable = map[sizeband.SizeBand]float64{
	sizeband.BandXS: 0,
	sizeband.BandS:  1,
	sizeband.BandM:  2,
	sizeband.BandL:  3,
	sizeband.BandXL: 4,
}

// bandIndex is `BAND_INDEX[band]` plus the `=== undefined` test the callers
// apply to it. An Object.prototype key resolves to an inherited function/object
// rather than undefined, so the guard PASSES; every arithmetic use of the value
// (`t - b0`, `Math.min(floor, b0)`, `b0 + 1`) then produces NaN, which is what
// the NaN sentinel reproduces exactly.
func bandIndex(band sizeband.SizeBand) (float64, bool) {
	if v, ok := bandIndexTable[band]; ok {
		return v, true
	}
	if isObjectPrototypeKey(string(band)) {
		return math.NaN(), true
	}
	return 0, false
}

// ModelRef mirrors the exported TS interface.
type ModelRef struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

// ModelTierName is TS `type ModelTierName = "high" | "low"`. Kept a plain
// string type: capability.ts accepts caller-supplied tier maps and its
// PRIOR_MEAN lookup has observable behavior for values outside the union.
type ModelTierName string

const (
	TierHigh ModelTierName = "high"
	TierLow  ModelTierName = "low"
)

// ---------------------------------------------------------------------------
// Credit mapping: a verified verdict → a success "credit" in [0,1].
var verdictCreditTable = map[LeafVerdict]float64{
	VerdictPass:            1.0,
	VerdictPassAfterRepair: 0.5,
	VerdictFail:            0.0,
	VerdictEscalated:       0.0,
}

// verdictCredit is `VERDICT_CREDIT[verdict]` plus its `=== undefined` guard;
// see bandIndex for why Object.prototype keys return a NaN sentinel.
func verdictCredit(v LeafVerdict) (float64, bool) {
	if c, ok := verdictCreditTable[v]; ok {
		return c, true
	}
	if isObjectPrototypeKey(string(v)) {
		return math.NaN(), true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// Prior success means, per tier, per band ("optimistic-by-tier").
var priorMeanTable = map[ModelTierName]map[sizeband.SizeBand]float64{
	TierHigh: {
		sizeband.BandXS: 0.93,
		sizeband.BandS:  0.88,
		sizeband.BandM:  0.82,
		sizeband.BandL:  0.75,
		sizeband.BandXL: 0.55,
	},
	TierLow: {
		sizeband.BandXS: 0.88,
		sizeband.BandS:  0.78,
		sizeband.BandM:  0.55,
		sizeband.BandL:  0.4,
		sizeband.BandXL: 0.22,
	},
}

type priorKind int

const (
	// priorNormal: tier is "high" or "low".
	priorNormal priorKind = iota
	// priorProtoObject: tier is an Object.prototype key, so PRIOR_MEAN[tier] is
	// an inherited function/object; indexing it by a band yields undefined and
	// every downstream arithmetic collapses to NaN.
	priorProtoObject
	// priorMissing: PRIOR_MEAN[tier] is undefined → `undefined[band]` throws.
	priorMissing
)

func priorMeanFor(tier ModelTierName, fromPrototype bool) (map[sizeband.SizeBand]float64, priorKind) {
	if fromPrototype {
		// tierMap[modelID] resolved to an inherited function (or
		// Object.prototype itself); PRIOR_MEAN has no such key.
		return nil, priorMissing
	}
	if m, ok := priorMeanTable[tier]; ok {
		return m, priorNormal
	}
	if isObjectPrototypeKey(string(tier)) {
		return nil, priorProtoObject
	}
	return nil, priorMissing
}

const (
	defaultPriorStrength       = 2.0
	defaultCrossStrong         = 0.5
	defaultCrossWeak           = 0.25
	defaultPromotionStreakK    = 3.0
	defaultPromotionStreakBonu = 1.0
	defaultDecay               = 0.85
	defaultThreshold           = 0.7
)

// CapabilityOptions mirrors the TS interface; every field is a pointer because
// TS distinguishes "absent" (apply the default via `??`) from an explicit
// value — notably `adaptiveCutsEnabled: false`, which must NOT fall back to the
// env predicate.
type CapabilityOptions struct {
	AdaptiveCutsEnabled  *bool                    `json:"adaptiveCutsEnabled"`
	TierMap              map[string]ModelTierName `json:"tierMap"`
	DefaultTier          *ModelTierName           `json:"defaultTier"`
	Decay                *float64                 `json:"decay"`
	PriorStrength        *float64                 `json:"priorStrength"`
	CrossBandStrong      *float64                 `json:"crossBandStrong"`
	CrossBandWeak        *float64                 `json:"crossBandWeak"`
	PromotionStreakK     *float64                 `json:"promotionStreakK"`
	PromotionStreakBonus *float64                 `json:"promotionStreakBonus"`
}

// cell is the per-(modelID, band) accumulated, decayed evidence: `a` is success
// mass (Beta α evidence), `b` failure mass. Both start at 0; the prior is added
// only when a probability is read.
type cell struct {
	a float64
	b float64
}

type streakState struct {
	count     float64
	bandFloor float64
}

// ---------------------------------------------------------------------------

// SnapshotCells is the `Record<string, [number,number][]>` at
// CapabilitySnapshot.cells. It is a JS object, not a Go map: key order is
// array-index-keys-ascending then insertion order, and `Set` reproduces the
// object-literal assignment that swallows "__proto__". A nil *SnapshotCells is
// JS null/undefined.
type SnapshotCells struct {
	o *jsObject
}

func NewSnapshotCells() *SnapshotCells { return &SnapshotCells{o: newJSObject()} }

// Set is `cells[key] = value` on an object literal.
func (c *SnapshotCells) Set(key string, value any) { c.o.Set(key, value) }

func (c *SnapshotCells) Get(key string) any {
	if c == nil {
		return nil
	}
	return c.o.Get(key)
}

// Keys returns the own enumerable keys in JS property order — what both
// JSON.stringify and Object.entries iterate.
func (c *SnapshotCells) Keys() []string {
	if c == nil {
		return nil
	}
	return c.o.Keys()
}

func (c *SnapshotCells) Len() int {
	if c == nil {
		return 0
	}
	return c.o.Len()
}

func (c *SnapshotCells) MarshalJSON() ([]byte, error) {
	if c == nil {
		return []byte("null"), nil
	}
	return c.o.MarshalJSON()
}

func (c *SnapshotCells) UnmarshalJSON(b []byte) error {
	o := newJSObject()
	if err := o.UnmarshalJSON(b); err != nil {
		return err
	}
	c.o = o
	return nil
}

// CapabilitySnapshot mirrors the exported TS interface (declaration order:
// version, then cells).
//
// Version is a number rather than the TS literal type `1`: `restore` tests it
// with `!== 1`, and any non-1 number (including NaN, which a non-numeric JSON
// version decodes to) short-circuits exactly as TS does.
type CapabilitySnapshot struct {
	Version jscompat.JSNumber `json:"version"`
	Cells   *SnapshotCells    `json:"cells"`
}

// UnmarshalJSON is tolerant on purpose: `restore` is documented as never
// throwing, and TS hands it whatever JSON.parse produced.
func (s *CapabilitySnapshot) UnmarshalJSON(b []byte) error {
	v, err := parseJSValue(b)
	if err != nil {
		return err
	}
	*s = CapabilitySnapshot{Version: jscompat.JSNumber(math.NaN())}
	obj, ok := v.(*jsObject)
	if !ok {
		return nil
	}
	if f, ok := obj.Get("version").(float64); ok {
		s.Version = jscompat.JSNumber(f)
	}
	if cells, ok := obj.Get("cells").(*jsObject); ok {
		s.Cells = &SnapshotCells{o: cells}
	}
	return nil
}

// ---------------------------------------------------------------------------

// CapabilityTracker ports the TS class of the same name.
type CapabilityTracker struct {
	tierMap       map[string]ModelTierName
	defaultTier   ModelTierName
	decay         float64
	priorStrength float64
	crossStrong   float64
	crossWeak     float64
	streakK       float64
	streakBonus   float64
	// modelID → 5 cells (one per band, indexed by BAND_INDEX). Insertion-ordered
	// because snapshot() iterates it.
	cells *jscompat.OrderedMap[string, []cell]
	// modelID → clean-pass streak state. Never iterated, so a bare map is safe.
	streaks             map[string]streakState
	adaptiveCutsEnabled bool
}

// NewCapabilityTracker is `new CapabilityTracker(opts = {})`; a nil opts is the
// `{}` default.
//
// (TS `new CapabilityTracker(null)` throws — a default parameter only fires for
// `undefined`. Go cannot express that distinction and treats nil as `{}`.)
func NewCapabilityTracker(opts *CapabilityOptions) *CapabilityTracker {
	if opts == nil {
		opts = &CapabilityOptions{}
	}
	t := &CapabilityTracker{
		cells:   jscompat.NewOrderedMap[string, []cell](),
		streaks: make(map[string]streakState),
	}
	// `opts.adaptiveCutsEnabled ?? adaptiveCutsEnabled()` — the env predicate is
	// only read when the option is absent (?? short-circuits).
	if opts.AdaptiveCutsEnabled != nil {
		t.adaptiveCutsEnabled = *opts.AdaptiveCutsEnabled
	} else {
		t.adaptiveCutsEnabled = adaptiveflag.AdaptiveCutsEnabled()
	}
	t.tierMap = opts.TierMap
	t.defaultTier = TierLow
	if opts.DefaultTier != nil {
		t.defaultTier = *opts.DefaultTier
	}
	t.decay = clamp(orDefault(opts.Decay, defaultDecay), 0, 1)
	t.priorStrength = jsMax(0, orDefault(opts.PriorStrength, defaultPriorStrength))
	t.crossStrong = clamp(orDefault(opts.CrossBandStrong, defaultCrossStrong), 0, 1)
	t.crossWeak = clamp(orDefault(opts.CrossBandWeak, defaultCrossWeak), 0, 1)
	t.streakK = jsMax(1, math.Floor(orDefault(opts.PromotionStreakK, defaultPromotionStreakK)))
	t.streakBonus = jsMax(0, orDefault(opts.PromotionStreakBonus, defaultPromotionStreakBonu))
	return t
}

func orDefault(p *float64, def float64) float64 {
	if p == nil {
		return def
	}
	return *p
}

// Observe folds one verified LeafOutcome into the model's cells.
//
// Cross-band scheme: an outcome carries success mass c (the verdict credit) and
// failure mass (1-c). Each mass propagates to EVERY band with a
// distance-discounted weight whose direction encodes the monotonicity of
// capability — success informs smaller bands strongly and larger bands weakly,
// failure the mirror. Because every band is touched on every observation, the
// EWMA decay is applied uniformly to all of a model's cells once per
// observation.
func (t *CapabilityTracker) Observe(outcome LeafOutcome) {
	if t.adaptiveCutsEnabled &&
		outcome.Verdict == VerdictFail &&
		outcome.Evidence.commandsRunIsZero() &&
		outcome.Evidence.blockersIsZero() {
		return
	}
	modelID := ""
	if outcome.Model != nil {
		modelID = outcome.Model.ModelID
	}
	if modelID == "" {
		return
	}
	band := outcome.SizeBand
	b0, ok := bandIndex(band)
	if !ok {
		return
	}
	credit, ok := verdictCredit(outcome.Verdict)
	if !ok {
		return
	}
	successMass := credit
	failMass := 1 - credit

	cells := t.cellsFor(modelID)
	for i := 0; i < len(BANDS); i++ {
		k := float64(i) - b0 // signed distance: <0 smaller band, >0 larger band
		// success informs smaller (k<=0) strongly, larger (k>0) weakly.
		successBase := t.crossWeak
		if k <= 0 {
			successBase = t.crossStrong
		}
		successW := jsPow(successBase, math.Abs(k))
		// failure informs larger (k>=0) strongly, smaller (k<0) weakly.
		failBase := t.crossWeak
		if k >= 0 {
			failBase = t.crossStrong
		}
		failW := jsPow(failBase, math.Abs(k))
		c := &cells[i]
		c.a = t.decay*c.a + successW*successMass
		c.b = t.decay*c.b + failW*failMass
	}

	// T6: streak-accelerated promotion. A "clean" pass is a one-shot pass with
	// no repair rounds. K of them in a row fold one extra pass-credit
	// observation into band bandFloor+1. Any non-clean outcome resets the
	// streak. The bonus lands AFTER this observation's decay pass so it enters
	// as fresh evidence.
	if t.streakBonus > 0 {
		clean := outcome.Verdict == VerdictPass && float64(outcome.RepairRounds) == 0
		if clean {
			prev, hasPrev := t.streaks[modelID]
			prevCount := 0.0
			if hasPrev {
				prevCount = prev.count
			}
			count := prevCount + 1
			bandFloor := b0
			if hasPrev && prev.count > 0 {
				bandFloor = jsMin(prev.bandFloor, b0)
			}
			if count >= t.streakK {
				targetIdx := bandFloor + 1
				if targetIdx < float64(len(BANDS)) {
					cells[int(targetIdx)].a += t.streakBonus
				}
				// Reset after firing: one bonus per K consecutive clean passes.
				t.streaks[modelID] = streakState{count: 0, bandFloor: b0}
			} else {
				t.streaks[modelID] = streakState{count: count, bandFloor: bandFloor}
			}
		} else {
			// Non-clean (repair, fail, escalated) breaks the streak.
			t.streaks[modelID] = streakState{count: 0, bandFloor: b0}
		}
	}
}

// PSuccess is the posterior-mean success probability for (model, band), prior
// included, read off the MONOTONE curve rather than the raw cell.
//
// Returns NaN where TS returns `undefined` — see the package doc.
func (t *CapabilityTracker) PSuccess(model ModelRef, band sizeband.SizeBand) float64 {
	idx, ok := bandIndex(band)
	if !ok {
		return 0
	}
	curve := t.curve(model.ModelID)
	if math.IsNaN(idx) {
		return math.NaN()
	}
	return curve[int(idx)]
}

// MaxReliableBand is the largest band whose (monotone) pSuccess ≥ threshold.
// `threshold` is variadic to model the TS default parameter of 0.7; pass at
// most one value.
func (t *CapabilityTracker) MaxReliableBand(model ModelRef, threshold ...float64) sizeband.SizeBand {
	th := defaultThreshold
	if len(threshold) > 0 {
		th = threshold[0]
	}
	curve := t.curve(model.ModelID)
	best := 0
	for i := 0; i < len(BANDS); i++ {
		if curve[i] >= th {
			best = i
		} else {
			break // monotone: once below threshold, all larger bands are too
		}
	}
	return BANDS[best]
}

// HasObservations reports whether this model has accumulated ANY real evidence
// yet (as opposed to sitting on priors alone).
func (t *CapabilityTracker) HasObservations(model ModelRef) bool {
	cells, ok := t.cells.Get(model.ModelID)
	if !ok {
		return false
	}
	for _, c := range cells {
		if c.a > 0 || c.b > 0 {
			return true
		}
	}
	return false
}

// Snapshot round-trips the accumulated cell state for a cross-run warm start.
func (t *CapabilityTracker) Snapshot() CapabilitySnapshot {
	cells := NewSnapshotCells()
	for _, e := range t.cells.Entries() {
		pairs := make([][]jscompat.JSNumber, len(e.Val))
		for i, c := range e.Val {
			pairs[i] = []jscompat.JSNumber{jscompat.JSNumber(c.a), jscompat.JSNumber(c.b)}
		}
		cells.Set(e.Key, pairs)
	}
	return CapabilitySnapshot{Version: 1, Cells: cells}
}

// Restore is the tolerant inverse of Snapshot; a nil snapshot, a version other
// than 1 or absent cells are all no-ops.
//
// Note that a well-formed-but-all-skipped entry still CREATES the model's cell
// row (cellsFor runs before the per-pair guards), so it shows up in the next
// snapshot with zeros.
func (t *CapabilityTracker) Restore(snap *CapabilitySnapshot) {
	if snap == nil || float64(snap.Version) != 1 || snap.Cells == nil {
		return
	}
	for _, modelID := range snap.Cells.Keys() {
		arr, ok := jsIsArray(snap.Cells.Get(modelID))
		if !ok || len(arr) != len(BANDS) {
			continue
		}
		cs := t.cellsFor(modelID)
		for i := 0; i < len(BANDS); i++ {
			pairVal, _ := jsIndex(arr, i)
			pair, ok := jsIsArray(pairVal)
			if !ok {
				continue
			}
			av, aDef := jsIndex(pair, 0)
			bv, bDef := jsIndex(pair, 1)
			a := jsNumber(av, aDef)
			b := jsNumber(bv, bDef)
			nc := cell{}
			if !math.IsNaN(a) && !math.IsInf(a, 0) {
				nc.a = jsMax(0, a)
			}
			if !math.IsNaN(b) && !math.IsInf(b, 0) {
				nc.b = jsMax(0, b)
			}
			cs[i] = nc
		}
	}
}

// --- internals ---

// lookupTier is `this.tierMap[modelID] ?? this.defaultTier`. The plain-object
// lookup walks Object.prototype, so a modelID that is an inherited key yields a
// function/object rather than undefined and the `??` keeps it — which makes the
// subsequent PRIOR_MEAN lookup throw. The bool reports that case.
func (t *CapabilityTracker) lookupTier(modelID string) (ModelTierName, bool) {
	if v, ok := t.tierMap[modelID]; ok {
		return v, false
	}
	if isObjectPrototypeKey(modelID) {
		return "", true
	}
	return t.defaultTier, false
}

// curve is the monotone posterior curve for a model: raw posterior mean per
// band (prior + decayed evidence), projected onto the non-increasing constraint
// by a running minimum from the SMALL end.
func (t *CapabilityTracker) curve(modelID string) []float64 {
	tier, fromPrototype := t.lookupTier(modelID)
	prior, kind := priorMeanFor(tier, fromPrototype)
	cells, hasCells := t.cells.Get(modelID)
	out := make([]float64, len(BANDS))
	running := math.Inf(1)
	for i := 0; i < len(BANDS); i++ {
		band := BANDS[i]
		var p0 float64
		switch kind {
		case priorNormal:
			p0 = prior[band]
		case priorProtoObject:
			// PRIOR_MEAN[tier] is an inherited function/object; [band] is
			// undefined, and `priorStrength * undefined` is NaN.
			p0 = math.NaN()
		default:
			// TS: "TypeError: undefined is not an object (evaluating
			// 'PRIOR_MEAN[tier][band]')". Uncaught there, uncaught here.
			panic(fmt.Sprintf("capability: PRIOR_MEAN[%q] is undefined (evaluating PRIOR_MEAN[tier][band])", string(tier)))
		}
		a0 := t.priorStrength * p0
		b0 := t.priorStrength * (1 - p0)
		cellA, cellB := 0.0, 0.0
		if hasCells {
			cellA = cells[i].a
			cellB = cells[i].b
		}
		a := a0 + cellA
		b := b0 + cellB
		denom := a + b
		raw := p0
		if denom > 0 {
			raw = a / denom
		}
		running = jsMin(running, raw)
		out[i] = running
	}
	return out
}

func (t *CapabilityTracker) cellsFor(modelID string) []cell {
	cs, ok := t.cells.Get(modelID)
	if !ok {
		cs = make([]cell, len(BANDS))
		t.cells.Set(modelID, cs)
	}
	return cs
}

func clamp(x, lo, hi float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return lo
	}
	return jsMax(lo, jsMin(hi, x))
}

// ===========================================================================
// Persistence wrappers — thin, tolerant, separate from the pure core above.
// ===========================================================================

// isUsableOutcome is the structural guard: enough of a LeafOutcome to feed
// Observe. Only model.modelID, sizeBand and verdict are checked.
//
// The TS predicate additionally rejects non-string modelID/sizeBand/verdict;
// the tolerant decoder in leafoutcome.go leaves those fields at "" for
// non-strings, and "" is rejected by every one of the three tests below, so the
// two agree.
func isUsableOutcome(o *LeafOutcome) bool {
	if o.Model == nil || o.Model.ModelID == "" {
		return false
	}
	if _, ok := bandIndex(o.SizeBand); !ok {
		return false
	}
	if _, ok := verdictCredit(o.Verdict); !ok {
		return false
	}
	return true
}

// LoadOutcomes is the tolerant reader for the append-only JSONL sink written by
// emitLeafOutcome (T1). Skips blank/malformed lines and records that don't
// carry the fields the estimator needs. NEVER fails — a missing file or a
// half-written trailing line yields as many good records as could be parsed.
func LoadOutcomes(jsonlPath string) []LeafOutcome {
	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		return []LeafOutcome{} // missing/unreadable file → no outcomes, not an error.
	}
	out := []LeafOutcome{}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := jscompat.Trim(line)
		if trimmed == "" {
			continue
		}
		var parsed LeafOutcome
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			continue // malformed line — skip.
		}
		if isUsableOutcome(&parsed) {
			out = append(out, parsed)
		}
	}
	return out
}

// CapabilityFromRun builds a tracker from a run's outcomes.jsonl and folds
// every outcome in. `opts` forwards the same knobs as the constructor.
func CapabilityFromRun(jsonlPath string, opts *CapabilityOptions) *CapabilityTracker {
	tracker := NewCapabilityTracker(opts)
	for _, outcome := range LoadOutcomes(jsonlPath) {
		tracker.Observe(outcome)
	}
	return tracker
}

// compile-time guards
var (
	_ json.Marshaler   = (*SnapshotCells)(nil)
	_ json.Unmarshaler = (*SnapshotCells)(nil)
	_ json.Unmarshaler = (*CapabilitySnapshot)(nil)
)
