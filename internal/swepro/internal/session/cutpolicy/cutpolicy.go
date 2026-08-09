// T4 cut policy — port of src/session/cut-policy.ts.
//
// The module is pure: no I/O, no clock, no RNG, no LLM. Every dynamic input
// (the task, the model, the tracker, the alternatives) is a parameter, so there
// is nothing to inject and no SetClockForTesting hook here.
//
// Fidelity notes (deliberate, do not "fix"):
//   - BAND_INDEX is a plain JS object literal, so `BAND_INDEX[band]` for a band
//     outside the five-value union is `undefined`, and EVERY numeric comparison
//     against `undefined` is false. bandIndexOf returns NaN for exactly that
//     reason — NaN has the identical comparison behavior. The one place the two
//     differ is BandToIndex's RETURN value (TS `undefined` vs Go NaN); both
//     JSON.stringify to null inside an array, which is how the fixtures encode
//     it. `BAND_INDEX["constructor"]` / `["__proto__"]` resolve through
//     Object.prototype to a function / an object instead of undefined; their
//     comparisons are still all false, so only a direct BandToIndex call on
//     those two strings is unrepresentable in a Go func returning float64. Not
//     fixtured, documented here (same call the sizeband port made).
//   - selectCoalesceGroup's doc comment claims the returned group is "in the
//     caller's original ordering"; the code returns it in the BAND-SORTED
//     ordering (the group is grown from `ordered`, not from `bucket`). The TS
//     test hides this by sorting ids before asserting. Ported as written — see
//     the fixture "selectCoalesceGroup/group-is-in-sorted-not-input-order".
//   - The bucket key is `${parent}\0${modelID}`, so a parent ending in "\0m"
//     with model "x" collides with parent "p" + model "m\0x" and the two
//     candidates coalesce across DIFFERENT parents. Kept; fixtured.
//   - splitForConcurrency does NOT forward the task's dependencyFanIn to
//     estimateSizeBand even though ConcurrencyCutTask carries one (cut-policy.ts
//     :179-182). decideCut does. Kept.
//   - descriptionForPart substitutes the file_scope line with
//     String.prototype.replace and a replacement STRING, so a `$&`, a `$` + backtick, a
//     `$'` or a `$$` inside a file path is expanded by GetSubstitution rather than
//     inserted literally. exactPath() rejects `*!?{}[]` but not `$`, so this is
//     reachable. Reproduced in expandReplacement.
package cutpolicy

import (
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/capability"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sizeband"
)

// bandIndexTable is TS BAND_INDEX (cut-policy.ts:25).
var bandIndexTable = map[sizeband.SizeBand]float64{
	sizeband.BandXS: 0,
	sizeband.BandS:  1,
	sizeband.BandM:  2,
	sizeband.BandL:  3,
	sizeband.BandXL: 4,
}

// bandIndexL / bandIndexM are the literal `BAND_INDEX.l` / `BAND_INDEX.m`
// property reads in shouldRootCut.
const (
	bandIndexM = 2
	bandIndexL = 3
)

// bandIndexOf is `BAND_INDEX[band]`. A key outside the union yields JS
// `undefined`; NaN reproduces its comparison behavior exactly (every <, <=, >,
// >= against it is false).
func bandIndexOf(band sizeband.SizeBand) float64 {
	if v, ok := bandIndexTable[band]; ok {
		return v
	}
	return math.NaN()
}

// ---------------------------------------------------------------------------
// CutDecision — the TS discriminated union.

// CutDecisionKind is the `kind` discriminant.
type CutDecisionKind string

const (
	DispatchAsLeaf CutDecisionKind = "dispatch-as-leaf"
	SplitFirst     CutDecisionKind = "split-first"
	EscalateModel  CutDecisionKind = "escalate-model"
)

// CutDecision is
//
//	| { kind: "dispatch-as-leaf" }
//	| { kind: "split-first"; reason: string }
//	| { kind: "escalate-model"; toModelID: string; reason: string }
//
// flattened into one struct. Only the fields the active variant declares are
// marshaled, in the TS object-literal key order (see MarshalJSON).
type CutDecision struct {
	Kind      CutDecisionKind
	ToModelID string
	Reason    string
}

// MarshalJSON emits exactly the keys the corresponding TS object literal has,
// in literal order. A struct with `omitempty` cannot express this: an
// escalate-model decision with an empty reason still carries the key, and a
// dispatch-as-leaf decision never does.
func (d CutDecision) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteString(`{"kind":`)
	b.WriteString(jsonString(string(d.Kind)))
	switch d.Kind {
	case DispatchAsLeaf:
	case SplitFirst:
		b.WriteString(`,"reason":`)
		b.WriteString(jsonString(d.Reason))
	case EscalateModel:
		b.WriteString(`,"toModelID":`)
		b.WriteString(jsonString(d.ToModelID))
		b.WriteString(`,"reason":`)
		b.WriteString(jsonString(d.Reason))
	}
	b.WriteString(`}`)
	return []byte(b.String()), nil
}

func jsonString(s string) string {
	b, err := jscompat.Stringify(s)
	if err != nil {
		// unreachable: a Go string always marshals.
		panic(err)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// Inputs.

// CutTask is the TS interface of the same name.
type CutTask struct {
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	// DependencyFanIn is TS `dependencyFanIn?: number`; nil is absent.
	DependencyFanIn *float64 `json:"dependencyFanIn"`
}

// DecideCutInput is the TS interface of the same name.
type DecideCutInput struct {
	Task CutTask `json:"task"`
	// Model is the model the scheduler's tier-resolution would assign to this
	// leaf.
	Model   capability.ModelRef `json:"model"`
	Tracker *capability.CapabilityTracker
	// Alternatives are other pool members (e.g. the HIGH pool) that could take
	// the leaf, in preference order — the first one that covers the band wins.
	Alternatives []capability.ModelRef `json:"alternatives"`
	// Threshold is the maxReliableBand threshold; nil defaults to 0.7 (matches
	// the estimator).
	Threshold *float64 `json:"threshold"`
}

// ConcurrencyCutTask is TS `interface ConcurrencyCutTask extends CutTask`.
type ConcurrencyCutTask struct {
	CutTask
	// FileScope holds the parsed file_scope entries. Unknown scopes are not
	// mechanically splittable.
	FileScope []string `json:"fileScope"`
}

// SplitForConcurrencyInput is the TS interface of the same name.
type SplitForConcurrencyInput struct {
	Task ConcurrencyCutTask `json:"task"`
	// IdleSlots is the number of dispatch slots not occupied by in-flight
	// leaves. TS `number`, so float64 (a fractional or NaN slot count reaches
	// the `< 2` test unchanged).
	IdleSlots    float64           `json:"idleSlots"`
	ReliableBand sizeband.SizeBand `json:"reliableBand"`
	// ActiveProbeWave is true while the T5 wave-0 probe is being held/dispatched.
	ActiveProbeWave bool `json:"activeProbeWave"`
}

const defaultThreshold = 0.7

// covers reports whether a model's (monotone) reliable band is at least as
// large as the task's band. Because maxReliableBand is monotone, this is the
// whole feasibility test — no need to inspect intermediate bands.
func covers(tracker *capability.CapabilityTracker, model capability.ModelRef, bandIdx float64, threshold float64) bool {
	reliable := tracker.MaxReliableBand(model, threshold)
	return bandIndexOf(reliable) >= bandIdx
}

// DecideCut ports decideCut (cut-policy.ts:74).
func DecideCut(input DecideCutInput) CutDecision {
	threshold := float64(defaultThreshold)
	if input.Threshold != nil {
		threshold = *input.Threshold
	}
	// `input.task.description ?? ""` / `input.task.tags ?? []` are no-ops for a
	// Go string and a nil slice respectively.
	band := sizeband.EstimateSizeBand(sizeband.EstimateSizeBandInput{
		Description:     input.Task.Description,
		Tags:            input.Task.Tags,
		DependencyFanIn: input.Task.DependencyFanIn,
	})
	bandIdx := bandIndexOf(band)

	assignedReliable := input.Tracker.MaxReliableBand(input.Model, threshold)
	assignedReliableIdx := bandIndexOf(assignedReliable)

	// xs-floor + the common case: the assigned model already covers the band.
	if bandIdx <= assignedReliableIdx {
		return CutDecision{Kind: DispatchAsLeaf}
	}

	// `alternatives` is in preference order; take the first that covers.
	for _, alt := range input.Alternatives {
		// Don't "escalate" to the same model — that's a no-op that would loop.
		// The test is on modelID ALONE, so an alternative that differs only in
		// providerID is skipped too.
		if alt.ModelID == input.Model.ModelID {
			continue
		}
		if covers(input.Tracker, alt, bandIdx, threshold) {
			altReliable := input.Tracker.MaxReliableBand(alt, threshold)
			return CutDecision{
				Kind:      EscalateModel,
				ToModelID: alt.ModelID,
				Reason: "task=" + string(band) + " > model-reliable=" + string(assignedReliable) +
					"; escalating to " + alt.ModelID + " (reliable=" + string(altReliable) + ")",
			}
		}
	}

	// Nothing covers the chunk (including the "xl task nobody covers" case).
	return CutDecision{
		Kind:   SplitFirst,
		Reason: "task=" + string(band) + " > model-reliable=" + string(assignedReliable) + "; no alternative covers, splitting",
	}
}

// ---------------------------------------------------------------------------
// W1 — parallelism-aware cuts.

// exactPath is `path.length > 0 && !/[*!?{}[\]]/.test(path)`. The character
// class is exactly the seven ASCII bytes * ! ? { } [ ], so a byte scan is
// equivalent to the UTF-16 one.
func exactPath(path string) bool {
	return len(path) > 0 && !strings.ContainsAny(path, "*!?{}[]")
}

func pathsOverlap(a, b string) bool {
	if a == b {
		return true
	}
	aa := a
	if strings.HasSuffix(a, "/") {
		aa = a[:len(a)-1] // slice(0,-1): the dropped unit is "/" (1 byte)
	}
	bb := b
	if strings.HasSuffix(b, "/") {
		bb = b[:len(b)-1]
	}
	return strings.HasPrefix(aa, bb+"/") || strings.HasPrefix(bb, aa+"/")
}

// PartitionFileScope returns exact, pairwise-disjoint file groups, or nil (TS
// `undefined`) when proof fails.
func PartitionFileScope(fileScope []string) [][]string {
	paths := make([]string, 0, len(fileScope))
	for _, p := range fileScope {
		// `.map(p => p.trim()).filter(Boolean)` — only "" is falsy for a string.
		if t := jscompat.Trim(p); t != "" {
			paths = append(paths, t)
		}
	}
	if len(paths) < 2 {
		return nil
	}
	for _, p := range paths {
		if !exactPath(p) {
			return nil
		}
	}
	// `[...new Set(paths)]` — insertion-ordered dedupe.
	unique := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		unique = append(unique, p)
	}
	if len(unique) < 2 {
		return nil
	}
	for i := 0; i < len(unique); i++ {
		for j := i + 1; j < len(unique); j++ {
			if pathsOverlap(unique[i], unique[j]) {
				return nil
			}
		}
	}
	out := make([][]string, 0, len(unique))
	for _, path := range unique {
		out = append(out, []string{path})
	}
	return out
}

func descriptionForPart(description string, group []string) string {
	line := "file_scope: " + strings.Join(group, ", ")
	// `.test()` then `.replace()`: the regex has no /g flag, so lastIndex is
	// never consulted and the two scans agree.
	if start, end, ok := findFileScopeLine(description); ok {
		return description[:start] + expandReplacement(line, description, start, end) + description[end:]
	}
	return jscompat.Trim(line + "\n" + description)
}

// SplitForConcurrency decides whether an otherwise leaf-ready task should be
// split to use idle concurrency. Returns the exact file groups to send to
// PlanDB, or nil (TS `undefined`).
func SplitForConcurrency(input SplitForConcurrencyInput) [][]string {
	if input.IdleSlots < 2 || input.ActiveProbeWave {
		return nil
	}
	groups := PartitionFileScope(input.Task.FileScope)
	// `!groups` is true only for undefined — an empty array is truthy, and
	// partitionFileScope never returns one.
	if groups == nil || len(groups) < 2 {
		return nil
	}

	reliableIndex := bandIndexOf(input.ReliableBand)
	partTags := make([]string, 0, len(input.Task.Tags))
	for _, tag := range input.Task.Tags {
		if !strings.HasPrefix(jsToLowerCase(tag), "scope:") {
			partTags = append(partTags, tag)
		}
	}
	for _, group := range groups {
		// NOTE: dependencyFanIn is deliberately NOT forwarded here — the TS
		// object literal only sets description and tags.
		band := sizeband.EstimateSizeBand(sizeband.EstimateSizeBandInput{
			Description: descriptionForPart(input.Task.Description, group),
			Tags:        partTags,
		})
		if bandIndexOf(band) > reliableIndex {
			return nil
		}
	}
	return groups
}

// BANDS re-exports the capability band ordering so callers/tests don't
// re-derive it (TS `export { BANDS }` — the same array reference).
var BANDS = capability.BANDS

// ---------------------------------------------------------------------------
// T5 — root-cut fast path + probe wave.

// BandToIndex is the band's position on the capability x-axis (xs=0 … xl=4).
// Returns NaN where TS returns `undefined` (see the package doc).
func BandToIndex(band sizeband.SizeBand) float64 {
	return bandIndexOf(band)
}

// BandWithinReliable is the root-cut feasibility test: is the WHOLE task's band
// within (≤) the chosen model's reliable band?
func BandWithinReliable(band sizeband.SizeBand, reliable sizeband.SizeBand) bool {
	return bandIndexOf(band) <= bandIndexOf(reliable)
}

// ShouldRootCutInput is the inline TS parameter object of shouldRootCut.
type ShouldRootCutInput struct {
	Band     sizeband.SizeBand `json:"band"`
	Reliable sizeband.SizeBand `json:"reliable"`
	HardMode bool              `json:"hardMode"`
}

// ShouldRootCut is the P4 root-cut GATE decision (the cost-staircase).
func ShouldRootCut(input ShouldRootCutInput) bool {
	idx := bandIndexOf(input.Band)
	if idx >= bandIndexL {
		return false
	}
	if input.HardMode && idx >= bandIndexM {
		return false
	}
	return BandWithinReliable(input.Band, input.Reliable)
}

// PickProbeLeaf returns the SMALLEST real-implementation leaf in the given
// (already-prioritized) order — lowest band index wins, ties broken by keeping
// the earlier item (strict `<`). The bool is false where TS returns
// `undefined`; note that a candidate whose bandIndex is +Infinity or NaN never
// beats the initial `Infinity`, so it is never selected either.
func PickProbeLeaf[T any](tasks []T, bandIndexOf func(t T) float64, isRealImpl func(t T) bool) (T, bool) {
	var best T
	found := false
	bestIdx := math.Inf(1)
	for _, t := range tasks {
		if !isRealImpl(t) {
			continue
		}
		idx := bandIndexOf(t)
		if idx < bestIdx {
			bestIdx = idx
			best = t
			found = true
		}
	}
	return best, found
}

// ---------------------------------------------------------------------------
// T6 — coalesce-group selection.

// CoalesceCandidate is the TS interface of the same name. Field order is the
// TS declaration order, which is also the object-literal order every producer
// uses.
type CoalesceCandidate struct {
	ID string `json:"id"`
	// Parent is TS `string | null`.
	Parent  *string `json:"parent"`
	ModelID string  `json:"modelID"`
	// BandIndex is a JS number: NaN/±Infinity marshal as null.
	BandIndex jscompat.JSNumber `json:"bandIndex"`
	// FileScope holds the parsed file_scope paths ([] = unknown/absent).
	FileScope []string `json:"fileScope"`
	// Description is the narrative description — fed to the injected
	// combinedBandIndex estimator.
	Description string `json:"description"`
}

// SelectCoalesceGroupOptions is the inline `opts` parameter object.
type SelectCoalesceGroupOptions struct {
	// CombinedBandIndex estimates the combined band index for a set of
	// candidates. Injected so the size-band estimator stays out of this pure
	// module. nil selects the default: the max member band index (a floor —
	// real coalescing only grows the band).
	CombinedBandIndex func(group []CoalesceCandidate) float64
	// MinGroup is TS `minGroup?: number`; nil defaults to 2.
	MinGroup *float64
}

// scopesCompatible: two scopes are compatible when they overlap (share a path)
// OR either is empty. An empty scope is a wildcard.
func scopesCompatible(a []string, accumulated []string) bool {
	if len(a) == 0 || len(accumulated) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(accumulated))
	for _, f := range accumulated {
		set[f] = struct{}{}
	}
	for _, f := range a {
		if _, ok := set[f]; ok {
			return true
		}
	}
	return false
}

func defaultCombinedBandIndex(g []CoalesceCandidate) float64 {
	// `g.reduce((mx, c) => Math.max(mx, c.bandIndex), 0)` — Math.max(NaN, …) is
	// NaN, and so is Go's math.Max.
	mx := 0.0
	for _, c := range g {
		mx = math.Max(mx, float64(c.BandIndex))
	}
	return mx
}

// SelectCoalesceGroup selects at most ONE coalesce group. Returns nil where TS
// returns `undefined`.
func SelectCoalesceGroup(
	candidates []CoalesceCandidate,
	reliableBandIndexOf func(modelID string) float64,
	opts *SelectCoalesceGroupOptions,
) []CoalesceCandidate {
	if opts == nil {
		opts = &SelectCoalesceGroupOptions{}
	}
	minGroup := 2.0
	if opts.MinGroup != nil {
		minGroup = *opts.MinGroup
	}
	combinedBandIndex := opts.CombinedBandIndex
	if combinedBandIndex == nil {
		combinedBandIndex = defaultCombinedBandIndex
	}

	// Bucket by (parent, model), preserving first-seen order of buckets and of
	// members within a bucket.
	buckets := jscompat.NewOrderedMap[string, []CoalesceCandidate]()
	for _, c := range candidates {
		if c.Parent == nil {
			continue // top-level tasks have no shared parent to coalesce under
		}
		reliable := reliableBandIndexOf(c.ModelID)
		if float64(c.BandIndex) >= reliable {
			continue // not "too small" — leave it alone
		}
		key := *c.Parent + "\x00" + c.ModelID
		cur, _ := buckets.Get(key)
		buckets.Set(key, append(cur, c))
	}

	for _, bucket := range buckets.Values() {
		if float64(len(bucket)) < minGroup {
			continue
		}
		reliable := reliableBandIndexOf(bucket[0].ModelID)
		// Seed from the smallest band; add compatible members while the
		// combined estimate stays within the reliable band. JS sort is stable,
		// so ties keep input order.
		ordered := make([]CoalesceCandidate, len(bucket))
		copy(ordered, bucket)
		sort.SliceStable(ordered, func(i, j int) bool {
			return float64(ordered[i].BandIndex)-float64(ordered[j].BandIndex) < 0
		})
		group := []CoalesceCandidate{ordered[0]}
		unionScope := append([]string(nil), ordered[0].FileScope...)
		for _, cand := range ordered[1:] {
			if !scopesCompatible(cand.FileScope, unionScope) {
				continue
			}
			trial := append(append([]CoalesceCandidate(nil), group...), cand)
			if combinedBandIndex(trial) > reliable {
				continue
			}
			group = append(group, cand)
			// `[...new Set([...unionScope, ...cand.fileScope])]`
			merged := make([]string, 0, len(unionScope)+len(cand.FileScope))
			seen := make(map[string]struct{}, len(unionScope)+len(cand.FileScope))
			for _, f := range unionScope {
				if _, ok := seen[f]; ok {
					continue
				}
				seen[f] = struct{}{}
				merged = append(merged, f)
			}
			for _, f := range cand.FileScope {
				if _, ok := seen[f]; ok {
					continue
				}
				seen[f] = struct{}{}
				merged = append(merged, f)
			}
			unionScope = merged
		}
		if float64(len(group)) >= minGroup {
			return group
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Hand-rolled /im scanner for descriptionForPart.
//
// RE2 has no way to express JS's `^`/`$` under /m (which break on \r, U+2028
// and U+2029 as well as \n) and its \s is not the JS class, so
// `/^file_scope:\s*.+$/im` is scanned by hand.
//
// Backtracking analysis (the pattern needs none beyond one point): `\s*` is
// greedy; if the maximal whitespace run ends at a position holding a
// non-line-terminator, `.+` matches at least that character and then runs to
// the next line terminator or the end, where `$` always succeeds. The only
// failure is a maximal run that ends at EOF or at a line terminator — then
// `\s*` gives units back one at a time and `.` must match the given-back unit,
// which succeeds for every whitespace unit except a line terminator.
//
// Match boundaries always fall on rune boundaries (the literal, the whitespace
// units and the line terminators are all single UTF-16 units, and `.+` consumes
// whole runes), so the scan works in bytes without a UTF-16 round trip.

func isJSWhitespaceRune(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// isLineTerminator is the JS LineTerminator set — what `^`/`$` break on under
// /m and what `.` refuses to match.
func isLineTerminator(r rune) bool {
	return r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029
}

const fileScopeLiteral = "file_scope:"

// asciiFoldHasPrefix is the /i literal compare. JS Canonicalize for a
// non-unicode regex never folds a code point ≥ 128 to one < 128 (the
// "cu < 128 but ch ≥ 128" guard), so case-insensitivity here is ASCII-only.
func asciiFoldHasPrefix(s string, at int, lit string) bool {
	if len(s)-at < len(lit) {
		return false
	}
	for i := 0; i < len(lit); i++ {
		c := s[at+i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != lit[i] {
			return false
		}
	}
	return true
}

// findFileScopeLine returns the byte span of the first match of
// /^file_scope:\s*.+$/im.
func findFileScopeLine(s string) (start, end int, ok bool) {
	lineStart := true
	for i := 0; i < len(s); {
		if lineStart {
			if e, matched := matchFileScopeAt(s, i); matched {
				return i, e, true
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		lineStart = isLineTerminator(r)
		i += size
	}
	// `^` also matches at len(s), but the literal cannot fit there.
	return 0, 0, false
}

func matchFileScopeAt(s string, i int) (int, bool) {
	if !asciiFoldHasPrefix(s, i, fileScopeLiteral) {
		return 0, false
	}
	p := i + len(fileScopeLiteral)

	// `\s*`, greedy — record every position it could give back to.
	stops := []int{p}
	for j := p; j < len(s); {
		r, size := utf8.DecodeRuneInString(s[j:])
		if !isJSWhitespaceRune(r) {
			break
		}
		j += size
		stops = append(stops, j)
	}

	for k := len(stops) - 1; k >= 0; k-- {
		at := stops[k]
		if at >= len(s) {
			continue // `.+` needs at least one character
		}
		r, size := utf8.DecodeRuneInString(s[at:])
		if isLineTerminator(r) {
			continue // `.` never matches a line terminator
		}
		// `.+` greedy: run to the next line terminator or the end. `$` holds at
		// both, so no further backtracking is possible.
		j := at + size
		for j < len(s) {
			r2, size2 := utf8.DecodeRuneInString(s[j:])
			if isLineTerminator(r2) {
				break
			}
			j += size2
		}
		return j, true
	}
	return 0, false
}

// expandReplacement reproduces GetSubstitution for a replacement STRING against
// a match with NO capture groups and NO named groups: `$$` → "$", `$&` → the
// match, `$` + backtick → the prefix, `$'` → the suffix. `$1`…`$99` exceed the group
// count and `$<` has no namedCaptures, so both stay literal.
func expandReplacement(replacement, s string, start, end int) string {
	if !strings.Contains(replacement, "$") {
		return replacement
	}
	var b strings.Builder
	for i := 0; i < len(replacement); i++ {
		c := replacement[i]
		if c != '$' || i+1 >= len(replacement) {
			b.WriteByte(c)
			continue
		}
		switch replacement[i+1] {
		case '$':
			b.WriteByte('$')
			i++
		case '&':
			b.WriteString(s[start:end])
			i++
		case '`':
			b.WriteString(s[:start])
			i++
		case '\'':
			b.WriteString(s[end:])
			i++
		default:
			b.WriteByte('$')
		}
	}
	return b.String()
}

// jsToLowerCase is String.prototype.toLowerCase. Go's unicode.ToLower is the
// *simple* mapping; the two disagree on U+0130 LATIN CAPITAL LETTER I WITH DOT
// ABOVE (JS: "i"+U+0307, Go: "i"), the only unconditional multi-character
// lowercase mapping in SpecialCasing.txt. Kept identical to the sizeband port's
// helper so a "scope:" prefix test agrees across modules.
func jsToLowerCase(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == 0x0130 {
			b.WriteString("i̇")
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
