// Knob registry — port of src/session/knobs.ts.
//
// W2: every tunable the T1–T7 (+ frontier) system introduced, declared with a
// default identical to today's hardcoded value, a bounded [min, max] range,
// and a one-line rationale. Resolution order:
//
//	<project>/.codeaf/knobs.json override → env CODEAF_KNOB_<NAME> → default
//
// Values are clamped to [min, max]; a clamp emits a warning log.
//
// Fidelity notes (deliberate, do not "fix"):
//   - formatNumber's `if (Number.isInteger(n)) return String(n)` branch is dead
//     — both arms return String(n). Kept as-is; the comment above it in the TS
//     source describes a toFixed-trimming implementation that was never
//     written. See formatNumber below.
//   - knobsSnapshotHash sorts with Array.prototype.sort's default comparator,
//     which is UTF-16-code-unit order, NOT UTF-8 byte order. lessUTF16
//     reproduces it (matters only for astral-plane keys; every registry name
//     is ASCII).
//   - The Record<string, …> results are insertion-ordered JS objects, so
//     `values` / `sources` iterate and stringify in KNOB_REGISTRY order.
//     LoadKnobFile additionally inherits V8's array-index-first key order.
//   - resolveKnobs' `raw ?? def.default` keeps a resolved 0, and clamp() lets
//     -0 through unclamped (-0 < 0 is false), so `bounded !== chosen` never
//     fires for it. Both behaviours are reproduced literally.
//   - The module reads neither Date.now() nor Math.random(), so there is no
//     injectable clock here.
package knobs

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/logshim"
)

var log = logshim.Create(map[string]any{"service": "session.knobs"})

// KnobDef mirrors the TS interface; field order is the JSON key order.
type KnobDef struct {
	Name      string  `json:"name"`
	Default   float64 `json:"default"`
	Min       float64 `json:"min"`
	Max       float64 `json:"max"`
	Rationale string  `json:"rationale"`
}

// MarshalJSON routes the three numbers through V8's formatter so a -0 or a
// sub-1e-6 bound would stringify exactly as JSON.stringify does.
func (k KnobDef) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`{"name":`)
	b.Write(jsonString(k.Name))
	b.WriteString(`,"default":`)
	b.Write(mustNumber(k.Default))
	b.WriteString(`,"min":`)
	b.Write(mustNumber(k.Min))
	b.WriteString(`,"max":`)
	b.Write(mustNumber(k.Max))
	b.WriteString(`,"rationale":`)
	b.Write(jsonString(k.Rationale))
	b.WriteByte('}')
	return b.Bytes(), nil
}

func mustNumber(f float64) []byte {
	out, _ := jscompat.JSNumber(f).MarshalJSON()
	return out
}

// LEAF_CONTEXT_TRIGGER_TOKENS_DEFAULT is W6c's absolute input-token compaction
// trigger for coder leaf sessions. Named so the registry entry below and the
// consumer in overflow.ts share one source of truth — no magic-number drift
// between declaration and use.
const LEAF_CONTEXT_TRIGGER_TOKENS_DEFAULT float64 = 60_000

// ---------------------------------------------------------------------------
// Registry. Defaults are byte-identical to the hardcoded constants in
// capability.ts, size-band.ts, cut-policy.ts, auditor-gate.ts, and the
// frontier tick cap declared in the W1 plan (FRONTIER_MAX_TICKS = 5).
// Do not "improve" a default here — the integration pass swaps consumers to
// read these values, and byte-identical defaults are the proof of no-op.
//
// The TS declaration is `readonly KnobDef[] … as const`; Go has no immutable
// slice, so treat this as read-only by convention.
var KNOB_REGISTRY = []KnobDef{
	// capability.ts — streak / EWMA / threshold / prior
	{
		Name:      "PROMOTION_STREAK_K",
		Default:   3,
		Min:       1,
		Max:       20,
		Rationale: "Clean passes in a row that trigger one promotion bonus (capability DEFAULT_PROMOTION_STREAK_K).",
	},
	{
		Name:      "PROMOTION_STREAK_BONUS",
		Default:   1.0,
		Min:       0,
		Max:       5,
		Rationale: "Success mass folded into band b+1 when the streak fires (DEFAULT_PROMOTION_STREAK_BONUS).",
	},
	{
		Name:      "EWMA_DECAY",
		Default:   0.85,
		Min:       0,
		Max:       1,
		Rationale: "Per-observation EWMA decay on accumulated evidence (capability constructor default 0.85).",
	},
	{
		Name:      "RELIABILITY_THRESHOLD",
		Default:   0.7,
		Min:       0,
		Max:       1,
		Rationale: "pSuccess floor for maxReliableBand / decideCut (DEFAULT_THRESHOLD).",
	},
	{
		Name:      "PRIOR_STRENGTH",
		Default:   2,
		Min:       0,
		Max:       50,
		Rationale: "Prior pseudo-count strength α₀+β₀ (DEFAULT_PRIOR_STRENGTH).",
	},
	{
		Name:      "PRIOR_MEAN_HIGH_XS",
		Default:   0.93,
		Min:       0,
		Max:       1,
		Rationale: "HIGH-tier prior mean at band xs (PRIOR_MEAN.high.xs).",
	},
	{
		Name:      "PRIOR_MEAN_HIGH_S",
		Default:   0.88,
		Min:       0,
		Max:       1,
		Rationale: "HIGH-tier prior mean at band s (PRIOR_MEAN.high.s).",
	},
	{
		Name:      "PRIOR_MEAN_HIGH_M",
		Default:   0.82,
		Min:       0,
		Max:       1,
		Rationale: "HIGH-tier prior mean at band m (PRIOR_MEAN.high.m).",
	},
	{
		Name:      "PRIOR_MEAN_HIGH_L",
		Default:   0.75,
		Min:       0,
		Max:       1,
		Rationale: "HIGH-tier prior mean at band l (PRIOR_MEAN.high.l).",
	},
	{
		Name:      "PRIOR_MEAN_HIGH_XL",
		Default:   0.55,
		Min:       0,
		Max:       1,
		Rationale: "HIGH-tier prior mean at band xl (PRIOR_MEAN.high.xl).",
	},
	{
		Name:      "PRIOR_MEAN_LOW_XS",
		Default:   0.88,
		Min:       0,
		Max:       1,
		Rationale: "LOW-tier prior mean at band xs (PRIOR_MEAN.low.xs).",
	},
	{
		Name:      "PRIOR_MEAN_LOW_S",
		Default:   0.78,
		Min:       0,
		Max:       1,
		Rationale: "LOW-tier prior mean at band s (PRIOR_MEAN.low.s).",
	},
	{
		Name:      "PRIOR_MEAN_LOW_M",
		Default:   0.55,
		Min:       0,
		Max:       1,
		Rationale: "LOW-tier prior mean at band m (PRIOR_MEAN.low.m).",
	},
	{
		Name:      "PRIOR_MEAN_LOW_L",
		Default:   0.4,
		Min:       0,
		Max:       1,
		Rationale: "LOW-tier prior mean at band l (PRIOR_MEAN.low.l).",
	},
	{
		Name:      "PRIOR_MEAN_LOW_XL",
		Default:   0.22,
		Min:       0,
		Max:       1,
		Rationale: "LOW-tier prior mean at band xl (PRIOR_MEAN.low.xl).",
	},

	// size-band.ts — char / line / criteria / file / fan-in thresholds
	{
		Name:      "DESC_CHARS_S",
		Default:   120,
		Min:       1,
		Max:       10_000,
		Rationale: "Description-char ceiling for size-band S bucket (DESC_CHARS_S).",
	},
	{
		Name:      "DESC_CHARS_M",
		Default:   400,
		Min:       1,
		Max:       50_000,
		Rationale: "Description-char ceiling for size-band M bucket (DESC_CHARS_M).",
	},
	{
		Name:      "DESC_CHARS_L",
		Default:   1200,
		Min:       1,
		Max:       100_000,
		Rationale: "Description-char ceiling for size-band L bucket (DESC_CHARS_L).",
	},
	{
		Name:      "DESC_LINES_S",
		Default:   4,
		Min:       1,
		Max:       1_000,
		Rationale: "Description-line ceiling for size-band S bucket (DESC_LINES_S).",
	},
	{
		Name:      "DESC_LINES_M",
		Default:   12,
		Min:       1,
		Max:       1_000,
		Rationale: "Description-line ceiling for size-band M bucket (DESC_LINES_M).",
	},
	{
		Name:      "DESC_LINES_L",
		Default:   30,
		Min:       1,
		Max:       1_000,
		Rationale: "Description-line ceiling for size-band L bucket (DESC_LINES_L).",
	},
	{
		Name:      "ACCEPTANCE_FEW",
		Default:   2,
		Min:       1,
		Max:       100,
		Rationale: "Acceptance-criteria count ceiling for 'few' points (ACCEPTANCE_FEW).",
	},
	{
		Name:      "ACCEPTANCE_SEVERAL",
		Default:   5,
		Min:       1,
		Max:       100,
		Rationale: "Acceptance-criteria count ceiling for 'several' points (ACCEPTANCE_SEVERAL).",
	},
	{
		Name:      "FILES_FEW",
		Default:   3,
		Min:       1,
		Max:       100,
		Rationale: "file_scope width ceiling for 'few' points (FILES_FEW).",
	},
	{
		Name:      "FILES_SEVERAL",
		Default:   7,
		Min:       1,
		Max:       500,
		Rationale: "file_scope width ceiling for 'several' points (FILES_SEVERAL).",
	},
	{
		Name:      "FANIN_FEW",
		Default:   1,
		Min:       0,
		Max:       50,
		Rationale: "Dependency fan-in ceiling for zero fan-in points (FANIN_FEW).",
	},
	{
		Name:      "FANIN_SEVERAL",
		Default:   4,
		Min:       0,
		Max:       100,
		Rationale: "Dependency fan-in ceiling for 'several' points (FANIN_SEVERAL).",
	},
	{
		Name:      "FANIN_MANY",
		Default:   9,
		Min:       0,
		Max:       200,
		Rationale: "Dependency fan-in ceiling for 'many' points (FANIN_MANY).",
	},

	// cut-policy.ts / plandb-scheduler.ts — coalesce + probe
	{
		Name:      "COALESCE_MIN_GROUP",
		Default:   2,
		Min:       2,
		Max:       20,
		Rationale: "Minimum siblings required to form a coalesce group (selectCoalesceGroup minGroup).",
	},
	{
		Name:      "COALESCE_MAX_PER_CYCLE",
		Default:   1,
		Min:       1,
		Max:       10,
		Rationale: "At most one coalesce per scheduler cycle (plandb-scheduler T6 policy).",
	},
	{
		Name:      "PROBE_WAVE_CAPACITY",
		Default:   1,
		Min:       1,
		Max:       5,
		Rationale: "Dispatch capacity while the wave-0 calibration probe is in flight (probeCap=1).",
	},

	// auditor-gate.ts — light audit turn cap
	{
		Name:      "LIGHT_AUDIT_TURN_CAP",
		Default:   7,
		Min:       1,
		Max:       50,
		Rationale: "Hard turn budget for the root-cut light auditor (LIGHT_AUDIT_TURN_CAP).",
	},

	// W7a audit-convergence — cleanup/accept diminishing-returns cap
	{
		Name:      "AUDIT_CLEANUP_MAX_CYCLES",
		Default:   1,
		Min:       0,
		Max:       5,
		Rationale: "Max cheap batched cleanup passes for a 0-correctness (hygiene/polish-only) audit before the work is accepted-with-notes (audit-convergence.ts AUDIT_CLEANUP_MAX_CYCLES_DEFAULT). min 0 = kill switch: 0 disables the cleanup/accept arms entirely, restoring byte-identical legacy audit-fix behavior.",
	},

	// W1 frontier — declared default; consumer lands in a parallel wave
	{
		Name:      "FRONTIER_MAX_TICKS",
		Default:   5,
		Min:       1,
		Max:       50,
		Rationale: "Max micro-replan ticks per run when CODEAF_FRONTIER=1 (W1 plan default).",
	},

	// W6c overflow.ts — absolute leaf-session compaction trigger
	{
		Name:      "LEAF_CONTEXT_TRIGGER_TOKENS",
		Default:   LEAF_CONTEXT_TRIGGER_TOKENS_DEFAULT,
		Min:       20_000,
		Max:       160_000,
		Rationale: "Absolute input-token compaction trigger for coder leaf sessions; fires compaction earlier than the global window fraction to cap the quadratic transcript tax (0 disables via env kill-switch, see overflow.ts).",
	},
}

// KnobName mirrors `export type KnobName = (typeof KNOB_REGISTRY)[number]["name"]`.
// Go has no string-literal union, so this is an alias and callers get no
// compile-time membership check.
type KnobName = string

// registryByName mirrors REGISTRY_BY_NAME: a Map, so lookups are immune to
// prototype keys ("__proto__", "constructor") that a plain object would leak.
var registryByName = func() *jscompat.OrderedMap[string, KnobDef] {
	m := jscompat.NewOrderedMap[string, KnobDef]()
	for _, k := range KNOB_REGISTRY {
		m.Set(k.Name, k)
	}
	return m
}()

// KnobDefFor is the TS `knobDef(name)`. It is spelled *For because Go has one
// namespace for types and functions and `KnobDef` is already the struct; TS
// keeps both because interfaces live in the type namespace.
// A nil result stands in for `undefined`.
//
// The TS version hands back the live registry object, so a caller could mutate
// the registry through it; this returns a copy. No caller in the tree mutates,
// so the observable contract is identical.
func KnobDefFor(name string) *KnobDef {
	def, ok := registryByName.Get(name)
	if !ok {
		return nil
	}
	out := def
	return &out
}

// DefaultKnobValues mirrors defaultKnobValues(): a fresh object whose keys are
// inserted in KNOB_REGISTRY order.
func DefaultKnobValues() *Record[float64] {
	out := NewRecord[float64]()
	for _, k := range KNOB_REGISTRY {
		out.Set(k.Name, k.Default)
	}
	return out
}

type ClampEvent struct {
	Name    string  `json:"name"`
	Raw     float64 `json:"raw"`
	Clamped float64 `json:"clamped"`
}

// MarshalJSON keeps the TS object-literal key order and V8 number formatting.
func (c ClampEvent) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`{"name":`)
	b.Write(jsonString(c.Name))
	b.WriteString(`,"raw":`)
	b.Write(mustNumber(c.Raw))
	b.WriteString(`,"clamped":`)
	b.Write(mustNumber(c.Clamped))
	b.WriteByte('}')
	return b.Bytes(), nil
}

// KnobSource is the union `"file" | "env" | "default"`.
type KnobSource string

const (
	SourceFile    KnobSource = "file"
	SourceEnv     KnobSource = "env"
	SourceDefault KnobSource = "default"
)

type ResolvedKnobs struct {
	Values  *Record[float64] `json:"values"`
	Clamped []ClampEvent     `json:"clamped"`
	// Sources records where each value came from after resolution (pre-clamp
	// source).
	Sources *Record[KnobSource] `json:"sources"`
}

// ---------------------------------------------------------------------------
// Pure resolution. `fileOverrides` / `env` are injected so tests never touch
// the filesystem or process.env. Unknown keys in either source are ignored.
// Priority: file → env CODEAF_KNOB_<NAME> → default. Then clamp to [min, max].
//
// A nil fileOverrides is the TS `= {}` default. A nil env behaves the same as
// an empty one: TS reads `env[k]` and a missing key is `undefined`, which
// parseNumeric rejects exactly like the empty string a Go map yields.
func ResolveKnobs(fileOverrides *Record[any], env map[string]string) ResolvedKnobs {
	values := NewRecord[float64]()
	sources := NewRecord[KnobSource]()
	clamped := []ClampEvent{}

	for _, def := range KNOB_REGISTRY {
		var raw float64
		hasRaw := false
		source := SourceDefault

		fileVal, _ := fileOverrides.Get(def.Name)
		if fromFile, ok := parseNumeric(fileVal); ok {
			raw = fromFile
			hasRaw = true
			source = SourceFile
		} else {
			if fromEnv, ok := parseNumeric(env["CODEAF_KNOB_"+def.Name]); ok {
				raw = fromEnv
				hasRaw = true
				source = SourceEnv
			}
		}

		chosen := def.Default
		if hasRaw {
			chosen = raw
		}
		bounded := clamp(chosen, def.Min, def.Max)
		if bounded != chosen {
			clamped = append(clamped, ClampEvent{Name: def.Name, Raw: chosen, Clamped: bounded})
		}
		values.Set(def.Name, bounded)
		sources.Set(def.Name, source)
	}

	return ResolvedKnobs{Values: values, Clamped: clamped, Sources: sources}
}

// parseNumeric mirrors the TS helper over the JSON.parse value model: a finite
// number passes through, a non-blank string goes through Number() and passes if
// finite, everything else (booleans, null, arrays, objects, missing) is
// rejected. Numbers must be float64 — that is what parseJSON produces; a
// hand-built Record[any] holding an int would be rejected here where JS would
// have accepted it.
func parseNumeric(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		if !math.IsNaN(t) && !math.IsInf(t, 0) {
			return t, true
		}
	case string:
		if jscompat.Trim(t) != "" {
			n := jscompat.ToNumber(t)
			if !math.IsNaN(n) && !math.IsInf(n, 0) {
				return n, true
			}
		}
	}
	return 0, false
}

func clamp(n, min, max float64) float64 {
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

// ---------------------------------------------------------------------------
// KnobsSnapshotHash is a stable hash of a resolved values map. Keys sorted;
// encoding is `NAME=value\n` so the digest is independent of object key
// insertion order and of JSON float formatting quirks across runtimes.
//
// A nil argument is the TS `= defaultKnobValues()` default parameter; an empty
// (non-nil) Record hashes the empty payload, exactly as `knobsSnapshotHash({})`
// does.
func KnobsSnapshotHash(values *Record[float64]) string {
	if values == nil {
		values = DefaultKnobValues()
	}
	keys := values.Keys()
	sort.SliceStable(keys, func(i, j int) bool { return lessUTF16(keys[i], keys[j]) })
	var payload strings.Builder
	for _, k := range keys {
		v, _ := values.Get(k)
		// The TS guard is `typeof v !== "number" || !Number.isFinite(v)`; the
		// typeof half is unreachable from a *Record[float64].
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		payload.WriteString(k)
		payload.WriteByte('=')
		payload.WriteString(formatNumber(v))
		payload.WriteByte('\n')
	}
	sum := sha1.Sum([]byte(payload.String()))
	return hex.EncodeToString(sum[:])
}

// formatNumber is the TS helper verbatim. Its comment promises trailing-zero
// trimming and its `Number.isInteger` branch suggests two code paths, but both
// arms are `String(n)` — the integer test is dead. Kept for shape.
func formatNumber(n float64) string {
	if n == math.Trunc(n) && !math.IsInf(n, 0) { // Number.isInteger(n)
		return jscompat.FormatNumber(n)
	}
	return jscompat.FormatNumber(n)
}

// ---------------------------------------------------------------------------
// Thin loaders — never throw. Missing/malformed knobs.json → {}.
func LoadKnobFile(projectRoot string) *Record[any] {
	file := filepath.Join(projectRoot, ".codeaf", "knobs.json")
	text, err := os.ReadFile(file)
	if err != nil {
		return NewRecord[any]()
	}
	parsed, ok := parseJSON(string(text))
	if !ok {
		return NewRecord[any]()
	}
	// `!parsed || typeof parsed !== "object" || Array.isArray(parsed)`: only a
	// non-null, non-array object survives, i.e. only a *Record[any].
	obj, ok := parsed.(*Record[any])
	if !ok {
		return NewRecord[any]()
	}
	return obj
}

// ResolveProjectKnobs resolves knobs for a project: file → env → default,
// logging any clamps. A nil env is the TS `= process.env` default.
func ResolveProjectKnobs(projectRoot string, env map[string]string) ResolvedKnobs {
	if env == nil {
		env = processEnv()
	}
	resolved := ResolveKnobs(LoadKnobFile(projectRoot), env)
	for _, c := range resolved.Clamped {
		log.Warn("knob clamped to declared range", map[string]any{
			"name":    c.Name,
			"raw":     c.Raw,
			"clamped": c.Clamped,
		})
	}
	return resolved
}

// processEnv is the Go image of process.env. Split on the first '=' so a value
// containing '=' survives, matching Node.
func processEnv() map[string]string {
	out := make(map[string]string)
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			out[kv[:i]] = kv[i+1:]
		}
	}
	return out
}
