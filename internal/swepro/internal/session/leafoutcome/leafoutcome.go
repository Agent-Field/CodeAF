// Package leafoutcome is the Go port of src/session/leaf-outcome.ts — the T1
// LeafOutcome telemetry record emitted at every leaf resolution (post
// review-gate, post merge attempt).
//
// The TS module is deliberately split into a pure core (verdict
// classification, size-band lookup delegated to ./size-band.ts, record
// assembly) and two best-effort I/O sinks that NEVER throw, because recording
// must not perturb the scheduler's control flow. The port keeps that shape:
// the pure functions return values, the two I/O functions return nothing and
// swallow every failure into the (discarded) log shim.
//
// Fidelity notes (deliberate, do not "fix"):
//
//   - SizeBand is an ALIAS for sizeband.SizeBand. TS declares the union here
//     and size-band.ts re-exports it while this module imports estimateSizeBand
//     back — a type-only cycle Go does not allow, so the port homes the type in
//     the sizeband package and re-exports it with a Go type alias. `SizeBand`
//     used from this package is the identical type, matching TS.
//
//   - Every JS `number` field is jscompat.JSNumber (float64) rather than int.
//     The TS types say `number`, so a caller can pass 0.5, NaN or 1e21 for
//     repairRounds/turns/costUsd/wallMs/timestamp and JSON.stringify has to
//     reproduce V8's exact text (NaN → null, 1e21 → "1e+21"). Int fields would
//     silently normalize all three.
//
//   - LeafOutcome.evidence carries `,omitempty` — the one deliberate exception
//     to the repo-wide no-omitempty rule. buildLeafOutcome only assigns the
//     key when input.evidence !== undefined, and JSON.stringify DROPS an
//     absent key rather than emitting null, so a plain pointer without
//     omitempty would add `"evidence":null` to every record.
//     KNOWN RESIDUAL GAP: TS `outcome.evidence = null` (reachable at runtime
//     since the check is `!== undefined`) stringifies to `"evidence":null`;
//     the Go nil pointer omits the key instead. Not modelled — a RawMessage
//     field would poison the ergonomics of the whole record for one
//     type-violating call.
//
//   - classifyVerdict compares `repairRounds > 0`, so NaN falls through to
//     "pass" in both languages. Preserved by keeping the field a float.
//
//   - auditEvidenceFromVerdict takes `unknown` and does JS property access on
//     it. The port models the value as a decoded-JSON `any` and reads
//     properties through jsProp, which returns nil for every non-object —
//     exactly what `v?.foo` yields for a string/number/bool/array/null.
//
//   - The command-mention regex is expanded to explicit ASCII case classes
//     instead of Go's `(?i)`. Go's `(?i)` applies UNICODE simple folding, so
//     `(?i)jest` also matches "jeſt" (long s) and `(?i)k` matches U+212A;
//     JS `/i` WITHOUT the /u flag canonicalizes via toUpperCase and rejects a
//     non-ASCII character whose uppercase is ASCII, so it does not. `\b` is
//     ASCII-only in both engines, so it ports unchanged.
//
//   - preserveRejectedWork shells out to `git diff` through @/util/process's
//     run(..., {nothrow: true}). Under nothrow a non-zero exit is not an error,
//     and an ASYNC spawn failure resolves with an EMPTY stdout buffer. A
//     SYNCHRONOUS spawn failure (Bun raises ENOTDIR from posix_spawn when cwd
//     is a regular file) escapes Process.spawn before the nothrow guard and
//     lands in preserveRejectedWork's own catch. Either way nothing is written,
//     so the Go port ignores exec errors entirely: an unusable git leaves
//     patch == "" and the function returns before touching the filesystem.
//     Verified by the `worktree pointing at a file` / `missing worktree`
//     fixtures, which record {"entries":null} for both. The only `catch` that
//     can change the FILESYSTEM outcome is an fs failure.
//
//   - Buffer.toString("utf8") is a LOSSY WHATWG decode, so invalid bytes in a
//     diff become U+FFFD before being written back out. decodeUTF8Lossy
//     reproduces it including the maximal-subpart rule (a truncated 3-byte
//     sequence costs ONE replacement character, not one per byte, which is
//     what Go's utf8.DecodeRune would give).
//
//   - No map or object is iterated anywhere in this module, so there is no
//     insertion-order or integer-like-key hazard to reproduce.
package leafoutcome

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/logshim"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/adaptiveflag"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sizeband"
)

var log = logshim.Create(map[string]any{"service": "session.leaf-outcome"})

// nowMillis stands in for Date.now(). Package-global, like the TS module's
// implicit dependency on the ambient clock.
var nowMillis = func() int64 { return time.Now().UnixMilli() }

// SetClockForTesting pins the millisecond clock. Returns a restore func.
func SetClockForTesting(f func() int64) func() {
	prev := nowMillis
	nowMillis = f
	return func() { nowMillis = prev }
}

// ---------------------------------------------------------------------------
// Types

// SizeBand mirrors `export type SizeBand = "xs" | "s" | "m" | "l" | "xl"`.
// It is a Go type ALIAS for sizeband.SizeBand: TS declares the union in this
// module and size-band.ts re-exports it, so the two names denote one type.
type SizeBand = sizeband.SizeBand

// LeafVerdict mirrors the LeafVerdict union.
type LeafVerdict string

const (
	VerdictPass            LeafVerdict = "pass"
	VerdictPassAfterRepair LeafVerdict = "pass_after_repair"
	VerdictFail            LeafVerdict = "fail"
	VerdictEscalated       LeafVerdict = "escalated"
)

// GateStatus mirrors the review-gate's terminal status union (GateResult.status
// in review-gate.ts). Values outside the union are NOT rejected — TS does no
// runtime validation either, and classifyVerdict simply falls through to the
// merge check for them.
type GateStatus string

const (
	GateStatusPass        GateStatus = "pass"
	GateStatusSkipped     GateStatus = "skipped"
	GateStatusDonePartial GateStatus = "done_partial"
	GateStatusEscalated   GateStatus = "escalated"
	GateStatusFail        GateStatus = "fail"
)

// LeafOutcomeModel is the inline `model` object of LeafOutcome.
type LeafOutcomeModel struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

// LeafOutcomeEvidence is the inline `evidence` object of LeafOutcome — also
// the return type of AuditEvidenceFromVerdict (TS `LeafOutcome["evidence"]`).
type LeafOutcomeEvidence struct {
	AuditCommandsRun jscompat.JSNumber `json:"auditCommandsRun"`
	AuditBlockers    jscompat.JSNumber `json:"auditBlockers"`
	InRunTestsPassed bool              `json:"inRunTestsPassed"`
}

// LeafOutcome mirrors the LeafOutcome interface. Field ORDER is the TS
// declaration order, which is also the assignment order in buildLeafOutcome —
// JSON.stringify follows insertion order, so the JSONL line's key order is a
// wire contract.
type LeafOutcome struct {
	TaskID        string               `json:"taskID"`
	Model         LeafOutcomeModel     `json:"model"`
	SizeBand      SizeBand             `json:"sizeBand"`
	Verdict       LeafVerdict          `json:"verdict"`
	RepairRounds  jscompat.JSNumber    `json:"repairRounds"`
	Turns         jscompat.JSNumber    `json:"turns"`
	ToolErrors    jscompat.JSNumber    `json:"toolErrors"`
	CostUsd       jscompat.JSNumber    `json:"costUsd"`
	WallMs        jscompat.JSNumber    `json:"wallMs"`
	MergeConflict bool                 `json:"mergeConflict"`
	Timestamp     jscompat.JSNumber    `json:"timestamp"`
	Evidence      *LeafOutcomeEvidence `json:"evidence,omitempty"`
}

// ---------------------------------------------------------------------------
// Size-band (T1 seam, T2 estimator).

// SizeBandFromTags mirrors sizeBandFromTags (leaf-outcome.ts:60). Kept for
// compat with existing callers/tests; it delegates to EstimateSizeBand with an
// empty description and no fan-in, reproducing T1's exact prior behavior.
//
// A nil slice is TS `undefined`. Both `undefined` and `[]` end up as an empty
// array because `[]` is truthy in JS, so the branch is unobservable.
func SizeBandFromTags(tags []string) SizeBand {
	return sizeband.EstimateSizeBand(sizeband.EstimateSizeBandInput{
		Description: "",
		Tags:        arrayFrom(tags),
	})
}

// arrayFrom is `tags ? Array.from(tags) : []` — a defensive copy, never nil.
func arrayFrom(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	out := make([]string, len(tags))
	copy(out, tags)
	return out
}

// ---------------------------------------------------------------------------
// Verdict classification.

// ClassifyVerdictInput is the inline parameter object of classifyVerdict.
type ClassifyVerdictInput struct {
	GateStatus    GateStatus        `json:"gateStatus"`
	RepairRounds  jscompat.JSNumber `json:"repairRounds"`
	MergeConflict bool              `json:"mergeConflict"`
}

// ClassifyVerdict mirrors classifyVerdict (leaf-outcome.ts:72). Pure mapping
// from what the gate + merge actually did to the four calibration buckets.
func ClassifyVerdict(input ClassifyVerdictInput) LeafVerdict {
	gateStatus, repairRounds, mergeConflict := input.GateStatus, input.RepairRounds, input.MergeConflict
	// Advisor/replanner involvement dominates.
	if gateStatus == GateStatusEscalated || gateStatus == GateStatusDonePartial {
		return VerdictEscalated
	}
	if gateStatus == GateStatusFail {
		return VerdictFail
	}
	// gateStatus is pass|skipped (or anything unrecognized) — the merge is the
	// last verification step.
	if mergeConflict {
		return VerdictFail
	}
	// NaN > 0 is false in both languages, so a NaN repair count reads "pass".
	if repairRounds > 0 {
		return VerdictPassAfterRepair
	}
	return VerdictPass
}

// ---------------------------------------------------------------------------
// Session-record aggregation.

// LeafSessionStats mirrors the LeafSessionStats interface.
type LeafSessionStats struct {
	Turns      jscompat.JSNumber `json:"turns"`
	ToolErrors jscompat.JSNumber `json:"toolErrors"`
	CostUsd    jscompat.JSNumber `json:"costUsd"`
}

// SessionMessagePartState is the inline `state` object of a message part.
type SessionMessagePartState struct {
	Status *string `json:"status"`
}

// SessionMessagePart is one element of the inline `parts` array. A nil element
// models TS `null`, which the `p?.` guard tolerates.
type SessionMessagePart struct {
	Type  *string                  `json:"type"`
	State *SessionMessagePartState `json:"state"`
}

// SessionMessageInfo is the inline `info` object of a message.
//
// Cost is a *JSNumber, i.e. TS `number | undefined`. The TS guard is
// `typeof c === "number" && Number.isFinite(c)`; a JSON `null` decodes to a nil
// pointer here and to `null` (typeof "object") there, so both skip it, and a
// non-finite number is skipped by the explicit finiteness check below.
type SessionMessageInfo struct {
	Role *string            `json:"role"`
	Cost *jscompat.JSNumber `json:"cost"`
}

// SessionMessage is the minimal structural message shape aggregateSessionStats
// takes (not the full MessageV2.WithParts) so it stays pure and fixture-driven.
type SessionMessage struct {
	Info  *SessionMessageInfo   `json:"info"`
	Parts []*SessionMessagePart `json:"parts"`
}

// AggregateSessionStats mirrors aggregateSessionStats (leaf-outcome.ts:98).
//
// Accumulation ORDER is message order and the `+=` shape is preserved
// literally, so float rounding matches TS exactly (0.01 + 0.02 stays
// 0.030000000000000002).
//
// Note the parts loop runs for EVERY message, not just assistant ones — that
// is what the TS does, so a tool error on a user message still counts.
func AggregateSessionStats(messages []*SessionMessage) LeafSessionStats {
	turns := 0.0
	toolErrors := 0.0
	costUsd := 0.0
	for _, m := range messages {
		if m != nil && m.Info != nil && m.Info.Role != nil && *m.Info.Role == "assistant" {
			turns += 1
			c := m.Info.Cost
			if c != nil && !math.IsNaN(float64(*c)) && !math.IsInf(float64(*c), 0) {
				costUsd += float64(*c)
			}
		}
		var parts []*SessionMessagePart
		if m != nil {
			// `m?.parts ?? []` — a nil slice iterates zero times, same as [].
			parts = m.Parts
		}
		for _, p := range parts {
			if p != nil && p.Type != nil && *p.Type == "tool" &&
				p.State != nil && p.State.Status != nil && *p.State.Status == "error" {
				toolErrors += 1
			}
		}
	}
	return LeafSessionStats{
		Turns:      jscompat.JSNumber(turns),
		ToolErrors: jscompat.JSNumber(toolErrors),
		CostUsd:    jscompat.JSNumber(costUsd),
	}
}

// ---------------------------------------------------------------------------
// Record assembly.

// BuildLeafOutcomeInput is the inline parameter object of buildLeafOutcome.
// Field order matches the TS declaration order.
type BuildLeafOutcomeInput struct {
	TaskID     string   `json:"taskID"`
	ProviderID string   `json:"providerID"`
	ModelID    string   `json:"modelID"`
	Tags       []string `json:"tags"`
	// Description and DependencyFanIn are the T2 static signals; nil is TS
	// `undefined`.
	Description     *string              `json:"description"`
	DependencyFanIn *float64             `json:"dependencyFanIn"`
	GateStatus      GateStatus           `json:"gateStatus"`
	RepairRounds    jscompat.JSNumber    `json:"repairRounds"`
	Turns           jscompat.JSNumber    `json:"turns"`
	ToolErrors      jscompat.JSNumber    `json:"toolErrors"`
	CostUsd         jscompat.JSNumber    `json:"costUsd"`
	WallMs          jscompat.JSNumber    `json:"wallMs"`
	MergeConflict   bool                 `json:"mergeConflict"`
	Evidence        *LeafOutcomeEvidence `json:"evidence"`
	// Now is `now?: number`, consumed with `??` — so an explicit 0 or NaN is
	// USED and only an absent value falls back to the clock.
	Now *jscompat.JSNumber `json:"now"`
}

// BuildLeafOutcome mirrors buildLeafOutcome (leaf-outcome.ts:123). Pure except
// for the Date.now() fallback, which is behind nowMillis.
func BuildLeafOutcome(input BuildLeafOutcomeInput) LeafOutcome {
	// `input.description ?? ""`
	description := ""
	if input.Description != nil {
		description = *input.Description
	}
	// `input.now ?? Date.now()`
	timestamp := jscompat.JSNumber(nowMillis())
	if input.Now != nil {
		timestamp = *input.Now
	}

	outcome := LeafOutcome{
		TaskID: input.TaskID,
		Model:  LeafOutcomeModel{ProviderID: input.ProviderID, ModelID: input.ModelID},
		SizeBand: sizeband.EstimateSizeBand(sizeband.EstimateSizeBandInput{
			Description:     description,
			Tags:            arrayFrom(input.Tags),
			DependencyFanIn: input.DependencyFanIn,
		}),
		Verdict: ClassifyVerdict(ClassifyVerdictInput{
			GateStatus:    input.GateStatus,
			RepairRounds:  input.RepairRounds,
			MergeConflict: input.MergeConflict,
		}),
		RepairRounds:  input.RepairRounds,
		Turns:         input.Turns,
		ToolErrors:    input.ToolErrors,
		CostUsd:       input.CostUsd,
		WallMs:        input.WallMs,
		MergeConflict: input.MergeConflict,
		Timestamp:     timestamp,
	}
	if input.Evidence != nil {
		outcome.Evidence = input.Evidence
	}
	return outcome
}

// ---------------------------------------------------------------------------
// Audit provenance.

// auditCommandRe is /\b(?:vitest|jest|bun test|pytest|cargo test|go test)\b/gi
// with the /i expanded to explicit ASCII case classes. See the package doc:
// Go's (?i) folds Unicode (U+017F → s, U+212A → k) where JS's non-/u /i does
// not, so `(?i)` would over-match.
var auditCommandRe = regexp.MustCompile(
	`\b(?:` +
		`[vV][iI][tT][eE][sS][tT]` +
		`|[jJ][eE][sS][tT]` +
		`|[bB][uU][nN] [tT][eE][sS][tT]` +
		`|[pP][yY][tT][eE][sS][tT]` +
		`|[cC][aA][rR][gG][oO] [tT][eE][sS][tT]` +
		`|[gG][oO] [tT][eE][sS][tT]` +
		`)\b`)

// jsProp is `v?.key`: property access on anything that is not a JS object
// yields undefined for these keys (none of them live on String/Array/Number
// prototypes), and optional chaining short-circuits on null/undefined.
func jsProp(v any, key string) any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return m[key]
}

// jsArray is Array.isArray(v).
func jsArray(v any) ([]any, bool) {
	a, ok := v.([]any)
	return a, ok
}

// AuditEvidenceFromVerdict mirrors auditEvidenceFromVerdict
// (leaf-outcome.ts:170): the small provenance payload shared by adaptive audit
// consumers.
//
// `verdict` is TS `unknown`; pass a decoded-JSON value (map[string]any,
// []any, string, float64, bool or nil).
//
// TS gives inRunTestsPassed a default of `false`. Go has no default
// parameters, so callers must pass it explicitly; `false` is the TS default.
func AuditEvidenceFromVerdict(verdict any, inRunTestsPassed bool) *LeafOutcomeEvidence {
	v := verdict
	var commands []any
	if arr, ok := jsArray(jsProp(jsProp(v, "step2_signal"), "commands")); ok {
		commands = arr
	} else if arr, ok := jsArray(jsProp(v, "commands")); ok {
		commands = arr
	} else {
		commands = []any{}
	}
	auditCommandsRun := float64(len(commands))
	if auditCommandsRun == 0 {
		if ev, ok := jsProp(v, "evidence").(string); ok {
			// Review-gate evidence is prose; count only explicit test/build
			// command mentions. `?? []` makes a null match count as 0.
			auditCommandsRun = float64(len(auditCommandRe.FindAllString(ev, -1)))
		}
	}
	auditBlockers := 0.0
	if arr, ok := jsArray(jsProp(v, "blockers")); ok {
		auditBlockers = float64(len(arr))
	} else if bugs, ok := jsArray(jsProp(v, "bugs")); ok {
		n := 0
		for _, bug := range bugs {
			if sev, ok := jsProp(bug, "severity").(string); ok && sev == "blocker" {
				n++
			}
		}
		auditBlockers = float64(n)
	}
	return &LeafOutcomeEvidence{
		AuditCommandsRun: jscompat.JSNumber(auditCommandsRun),
		AuditBlockers:    jscompat.JSNumber(auditBlockers),
		InRunTestsPassed: inRunTestsPassed,
	}
}

// ---------------------------------------------------------------------------
// Rejected-work sink.

// unsafeTaskIDRe is /[^a-zA-Z0-9._-]+/g.
var unsafeTaskIDRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// PreserveRejectedWorkArgs is the inline parameter object of
// preserveRejectedWork.
type PreserveRejectedWorkArgs struct {
	Workspace string  `json:"workspace"`
	Worktree  string  `json:"worktree"`
	TaskID    string  `json:"taskID"`
	BaseSha   *string `json:"baseSha"`
}

// PreserveRejectedWork mirrors preserveRejectedWork (leaf-outcome.ts:195).
//
// Best-effort rejected-work sink: it compares the leaf worktree to its base
// before any reset/discard and never throws or changes scheduler control flow.
// The TS returns Promise<void> that never rejects, so the Go port returns
// nothing.
func PreserveRejectedWork(args PreserveRejectedWorkArgs) {
	if !adaptiveflag.AdaptiveCutsEnabled() {
		return
	}
	if err := preserveRejectedWorkBody(args); err != nil {
		log.Warn("rejected-work preservation failed", map[string]any{
			"taskID": args.TaskID,
			"error":  sliceTo200(errString(err)),
		})
	}
}

// preserveRejectedWorkBody is the TS `try` block. Only the fs calls can fail:
// under Process.run's nothrow, a spawn failure resolves with an empty stdout
// buffer rather than throwing.
func preserveRejectedWorkBody(args PreserveRejectedWorkArgs) error {
	// `args.baseSha?.trim() ? [args.baseSha.trim()] : ["HEAD"]` — JS truthiness
	// of a string is "non-empty", and String.prototype.trim uses the JS
	// whitespace class (U+FEFF included, which Go's strings.TrimSpace also
	// handles but unicode.IsSpace does not).
	rng := []string{"HEAD"}
	if args.BaseSha != nil {
		if t := jscompat.Trim(*args.BaseSha); t != "" {
			rng = []string{t}
		}
	}

	cmd := exec.Command("git", append([]string{"diff", "--no-color"}, rng...)...)
	cmd.Dir = args.Worktree
	stdout, _ := cmd.Output() // nothrow: exit code and spawn errors are ignored
	patch := decodeUTF8Lossy(stdout)
	if jscompat.Trim(patch) == "" {
		return nil
	}

	safeTaskID := unsafeTaskIDRe.ReplaceAllString(args.TaskID, "_")
	dir := filepath.Join(args.Workspace, ".codeaf", "rejected-work")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, safeTaskID+".patch"), []byte(patch), 0o666)
}

// ---------------------------------------------------------------------------
// Persistence — both sinks.

// EmitLeafOutcomeArgs is the inline parameter object of emitLeafOutcome.
// WriteContext is the optional plandb-context callback; nil is TS `undefined`.
type EmitLeafOutcomeArgs struct {
	Workspace    string
	Outcome      LeafOutcome
	WriteContext func(body string) error
}

// EmitLeafOutcome mirrors emitLeafOutcome (leaf-outcome.ts:233).
//
// Never throws: each sink is independently wrapped so a failure in one (or
// both) only logs and the caller's control flow is untouched.
//  1. append-only JSONL at <workspace>/.codeaf/outcomes.jsonl
//  2. plandb context entry via the WriteContext callback
func EmitLeafOutcome(args EmitLeafOutcomeArgs) {
	line := StringifyOutcome(args.Outcome)

	// Sink 1: JSONL.
	if err := appendOutcomeLine(args.Workspace, line); err != nil {
		log.Error("leaf-outcome jsonl append failed", map[string]any{
			"taskID": args.Outcome.TaskID,
			"error":  sliceTo200(errString(err)),
		})
	}

	// Sink 2: plandb context entry.
	if args.WriteContext != nil {
		if err := args.WriteContext(line); err != nil {
			log.Error("leaf-outcome plandb context failed", map[string]any{
				"taskID": args.Outcome.TaskID,
				"error":  sliceTo200(errString(err)),
			})
		}
	}
}

// StringifyOutcome is `JSON.stringify(outcome)` for a LeafOutcome — the exact
// JSONL line body and the plandb context payload. Exported because both sinks
// share the one string and callers reuse it.
//
// jscompat.Stringify cannot fail for this shape (only strings, bools and
// JSNumbers), so the error is dropped rather than invented into a behavior TS
// does not have.
func StringifyOutcome(outcome LeafOutcome) string {
	b, err := jscompat.Stringify(outcome)
	if err != nil {
		return ""
	}
	return string(b)
}

func appendOutcomeLine(workspace, line string) error {
	dir := filepath.Join(workspace, ".codeaf")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "outcomes.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o666)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(line + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ---------------------------------------------------------------------------
// small JS-semantics helpers

// errString is `String(err)`. Node prints an Error as "<name>: <message>";
// the value only ever reaches the (discarded) log shim.
func errString(err error) string {
	if err == nil {
		return "undefined"
	}
	return "Error: " + err.Error()
}

// sliceTo200 is `.slice(0, 200)` on a JS string: 200 UTF-16 CODE UNITS, which
// can cut an astral character in half. Go strings cannot hold a lone
// surrogate, so a cut through a surrogate pair drops the character entirely
// here — accepted, the result is log-only.
func sliceTo200(s string) string {
	u := utf16.Encode([]rune(s))
	if len(u) <= 200 {
		return s
	}
	return string(utf16.Decode(u[:200]))
}

// decodeUTF8Lossy reproduces Buffer.toString("utf8") / the WHATWG UTF-8
// decoder: invalid sequences become U+FFFD, one per MAXIMAL SUBPART (so the
// two-byte prefix of a truncated three-byte sequence costs a single
// replacement character, unlike Go's byte-at-a-time utf8.DecodeRune).
func decodeUTF8Lossy(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			sb.WriteByte(c)
			i++
			continue
		}
		var need int
		var lo, hi byte
		switch {
		case c >= 0xC2 && c <= 0xDF:
			need, lo, hi = 1, 0x80, 0xBF
		case c == 0xE0:
			need, lo, hi = 2, 0xA0, 0xBF
		case c >= 0xE1 && c <= 0xEC:
			need, lo, hi = 2, 0x80, 0xBF
		case c == 0xED:
			need, lo, hi = 2, 0x80, 0x9F
		case c >= 0xEE && c <= 0xEF:
			need, lo, hi = 2, 0x80, 0xBF
		case c == 0xF0:
			need, lo, hi = 3, 0x90, 0xBF
		case c >= 0xF1 && c <= 0xF3:
			need, lo, hi = 3, 0x80, 0xBF
		case c == 0xF4:
			need, lo, hi = 3, 0x80, 0x8F
		default:
			// 0x80-0xC1 and 0xF5-0xFF can never start a sequence.
			sb.WriteRune(utf8.RuneError)
			i++
			continue
		}
		var cp rune
		switch need {
		case 1:
			cp = rune(c & 0x1F)
		case 2:
			cp = rune(c & 0x0F)
		default:
			cp = rune(c & 0x07)
		}
		j := 1
		ok := true
		for ; j <= need; j++ {
			l, h := byte(0x80), byte(0xBF)
			if j == 1 {
				l, h = lo, hi
			}
			if i+j >= len(b) || b[i+j] < l || b[i+j] > h {
				ok = false
				break
			}
			cp = cp<<6 | rune(b[i+j]&0x3F)
		}
		if !ok {
			// j bytes were consumed as the maximal subpart; the offending byte
			// (if any) is re-examined as a fresh sequence start.
			sb.WriteRune(utf8.RuneError)
			i += j
			continue
		}
		sb.WriteRune(cp)
		i += need + 1
	}
	return sb.String()
}
