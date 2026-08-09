package runner

// The Effect `Cause` trichotomy (ENGINE-DESIGN §4.3). Effect distinguishes
// three reason kinds inside one failure; Go has one `error`. `Cause` carries
// the reason list explicitly so the four discrimination sites in runner.ts
// (complete, awaitDone, the shell await path, processor.ts:713) can ask the
// same three questions Effect asks.
//
// Ported from effect/dist/internal/effect.js:
//   hasFails            :76-88   some Fail
//   hasDies             :76      some Die
//   hasInterrupts       :88      some Interrupt
//   hasInterruptsOnly   :114     length > 0 && every Interrupt  ← note the > 0
//   causeSquash         :164     first Fail, else first Die, else the two
//                                canned Errors
//   causePrettyErrors   :175     the InterruptError/InterruptCause synthesis
//   causePretty         :299     join("\n") with the nested-cause block

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// ReasonKind is Effect's Reason._tag.
type ReasonKind uint8

const (
	// KindFailure is an ordinary typed error (Effect `Fail`).
	KindFailure ReasonKind = iota
	// KindDefect is a recovered panic (Effect `Die`).
	KindDefect
	// KindInterrupt is cancellation (Effect `Interrupt`).
	KindInterrupt
)

// Reason mirrors one entry of CauseImpl.reasons.
type Reason struct {
	Kind ReasonKind
	// Err is the typed error for KindFailure and the defect rendered as an
	// error for KindDefect. It is nil for KindInterrupt.
	Err error
	// Defect is the raw recovered value for KindDefect (Effect's `defect` is
	// `unknown`, not necessarily an Error).
	Defect any
	// FiberID is Interrupt.fiberId — a JS number, so *float64, and nil when
	// the interruptor is unknown.
	FiberID *float64
}

// Cause is Effect's Cause<E>. It is an error so it can travel through ordinary
// Go error returns, and it Unwraps to its reasons so errors.Is sees through it.
type Cause struct {
	Reasons []Reason
}

func (c *Cause) Error() string { return squashCause(c).Error() }

// Unwrap exposes every reason's error to errors.Is / errors.As.
func (c *Cause) Unwrap() []error {
	if c == nil {
		return nil
	}
	out := make([]error, 0, len(c.Reasons))
	for _, r := range c.Reasons {
		switch r.Kind {
		case KindInterrupt:
			out = append(out, ErrInterrupted)
		default:
			if r.Err != nil {
				out = append(out, r.Err)
			}
		}
	}
	return out
}

// ErrInterrupted is the cancellation sentinel handed to
// context.WithCancelCause. Work bodies should surface it by returning
// context.Cause(ctx).
var ErrInterrupted = errors.New("interrupted")

// errAllInterrupted / errEmptyCause are the two canned squash results
// (effect.js:164-172). Effect allocates a fresh Error each time; nothing reads
// identity, so package-level values are equivalent.
var (
	errAllInterrupted = errors.New("All fibers interrupted without error")
	errEmptyCause     = errors.New("Empty cause")
)

// ── constructors ──────────────────────────────────────────────────────────

// NewCause is Cause.fromReasons.
func NewCause(reasons ...Reason) *Cause { return &Cause{Reasons: reasons} }

// Fail is Cause.fail.
func Fail(err error) *Cause { return &Cause{Reasons: []Reason{{Kind: KindFailure, Err: err}}} }

// Die is Cause.die. `v` is the recovered panic value; when it is not already an
// error it is rendered the way Effect renders a non-Error defect.
func Die(v any) *Cause {
	return &Cause{Reasons: []Reason{{Kind: KindDefect, Defect: v, Err: defectError(v)}}}
}

// Interrupt is Cause.interrupt() with an unknown interruptor.
func Interrupt() *Cause { return &Cause{Reasons: []Reason{{Kind: KindInterrupt}}} }

// InterruptFrom is Cause.interrupt(fiberId).
func InterruptFrom(fiberID float64) *Cause {
	return &Cause{Reasons: []Reason{{Kind: KindInterrupt, FiberID: &fiberID}}}
}

// defectError renders a panic value as an error. `Defect` keeps the original
// value so Squash can hand it back unchanged.
type defectValue struct{ Value any }

func (d *defectValue) Error() string { return fmt.Sprint(d.Value) }

func defectError(v any) error {
	if err, ok := v.(error); ok {
		return err
	}
	return &defectValue{Value: v}
}

// ── classification ────────────────────────────────────────────────────────

// causeOf normalises any error into a Cause. A plain Go error is one Fail
// reason; the interruption sentinel is one Interrupt reason; nil is the empty
// cause (which — deliberately — is NOT interrupt-only, effect.js:114).
func causeOf(err error) *Cause {
	if err == nil {
		return &Cause{}
	}
	if c, ok := err.(*Cause); ok {
		return c
	}
	if errors.Is(err, ErrInterrupted) {
		return Interrupt()
	}
	return Fail(err)
}

// HasFails is Cause.hasFails.
func HasFails(err error) bool { return anyReason(err, KindFailure) }

// HasDefects is Cause.hasDies.
func HasDefects(err error) bool { return anyReason(err, KindDefect) }

// HasInterrupts is Cause.hasInterrupts.
func HasInterrupts(err error) bool { return anyReason(err, KindInterrupt) }

func anyReason(err error, kind ReasonKind) bool {
	for _, r := range causeOf(err).Reasons {
		if r.Kind == kind {
			return true
		}
	}
	return false
}

// IsInterruptOnly is Cause.hasInterruptsOnly: at least one reason, and every
// reason an interrupt. The empty cause is false.
func IsInterruptOnly(err error) bool {
	c := causeOf(err)
	if len(c.Reasons) == 0 {
		return false
	}
	for _, r := range c.Reasons {
		if r.Kind != KindInterrupt {
			return false
		}
	}
	return true
}

// Squash is Cause.squash: the first failure's error, else the first defect,
// else one of the two canned errors.
func Squash(err error) error { return squashCause(causeOf(err)) }

func squashCause(c *Cause) error {
	if c == nil {
		return errEmptyCause
	}
	for _, r := range c.Reasons {
		if r.Kind == KindFailure {
			return r.Err
		}
	}
	for _, r := range c.Reasons {
		if r.Kind == KindDefect {
			return r.Err
		}
	}
	for _, r := range c.Reasons {
		if r.Kind == KindInterrupt {
			return errAllInterrupted
		}
	}
	return errEmptyCause
}

// ── JS error shape ────────────────────────────────────────────────────────

// jsError lets a ported error type declare the `name` / `message` pair a JS
// Error would have. Without it a Go error is `name: "Error"`, `message:
// err.Error()` — which is what `class X extends Error {}` produces too, since
// a subclass that does not set `name` inherits "Error".
type jsError interface {
	ErrorName() string
	ErrorMessage() string
}

// ErrorName is JS `err.name`.
func ErrorName(err error) string {
	if err == nil {
		return "Error"
	}
	if j, ok := err.(jsError); ok {
		return j.ErrorName()
	}
	return "Error"
}

// ErrorMessage is JS `err.message`.
func ErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	if j, ok := err.(jsError); ok {
		return j.ErrorMessage()
	}
	return err.Error()
}

// ── pretty ────────────────────────────────────────────────────────────────

type prettyErr struct {
	stack string
	cause *prettyErr
}

// Pretty is Cause.pretty. Go errors carry no `.stack`, so causePrettyError
// takes its else-branch and every rendered frame is exactly
// `${name}: ${message}`; an interrupt-only cause takes the synthesised
// InterruptError path, nested InterruptCause and fiber ids included.
func Pretty(err error) string {
	c := causeOf(err)
	if len(c.Reasons) == 0 {
		return ""
	}
	var out []prettyErr
	var interrupts []Reason
	for _, r := range c.Reasons {
		if r.Kind == KindInterrupt {
			interrupts = append(interrupts, r)
			continue
		}
		out = append(out, prettyErr{stack: ErrorName(r.Err) + ": " + ErrorMessage(r.Err)})
	}
	if len(out) == 0 {
		lines := []string{"InterruptCause: The fiber was interrupted by:"}
		for _, r := range interrupts {
			id := "unknown"
			if r.FiberID != nil {
				id = "#" + jscompat.FormatNumber(*r.FiberID)
			}
			lines = append(lines, "    at fiber ("+id+")")
		}
		out = append(out, prettyErr{
			stack: "InterruptError: All fibers interrupted without error",
			cause: &prettyErr{stack: strings.Join(lines, "\n")},
		})
	}
	rendered := make([]string, 0, len(out))
	for _, e := range out {
		if e.cause != nil {
			rendered = append(rendered, e.stack+" {\n"+renderErrorCause(e.cause, "  ")+"\n}")
			continue
		}
		rendered = append(rendered, e.stack)
	}
	return strings.Join(rendered, "\n")
}

func renderErrorCause(c *prettyErr, prefix string) string {
	lines := strings.Split(c.stack, "\n")
	stack := prefix + "[cause]: " + lines[0]
	for _, l := range lines[1:] {
		stack += "\n" + prefix + l
	}
	if c.cause != nil {
		stack += " {\n" + renderErrorCause(c.cause, prefix+"  ") + "\n" + prefix + "}"
	}
	return stack
}

// PrettySlice is `Cause.pretty(cause).slice(0, n)` — the model-visible
// truncation at prompt.ts:1528, agent-json.ts:531 and review-gate.ts:808/:1093.
// The slice counts UTF-16 code units, not runes, because that is what
// String.prototype.slice counts.
//
// Divergence: a cut that lands between the halves of a surrogate pair leaves a
// lone surrogate in JS; Go cannot hold one in a string, so it becomes U+FFFD.
// Only reachable when an error message puts an astral character exactly on the
// boundary.
func PrettySlice(err error, n int) string { return sliceUTF16(Pretty(err), n) }

func sliceUTF16(s string, n int) string {
	if n <= 0 {
		return ""
	}
	u := utf16.Encode([]rune(s))
	if n >= len(u) {
		return s
	}
	return string(utf16.Decode(u[:n]))
}

// ── boundary normalisation ────────────────────────────────────────────────

// normalizeCancellation is the §4.3 boundary rule: an error that merely
// OBSERVED a cancelled context is an interrupt, but only when the work's
// context actually was cancelled. That includes Runner.Cancel and cancellation
// inherited from the runner scope / shell caller. A real failure that happens
// to mention context.Canceled while nobody asked for a stop stays a failure.
func normalizeCancellation(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*Cause); ok {
		return err
	}
	if ctx == nil || ctx.Err() == nil {
		return err
	}
	cause := context.Cause(ctx)
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrInterrupted) ||
		(cause != nil && errors.Is(err, cause)) {
		return Interrupt()
	}
	return err
}

// runWork runs a work body, turning a panic into a Defect cause (the Go
// analogue of an Effect fiber dying) and normalising observed cancellation.
func runWork[A any](ctx context.Context, fn func(context.Context) (A, error)) (v A, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			var zero A
			v, err = zero, Die(rec)
		}
	}()
	v, err = fn(ctx)
	err = normalizeCancellation(ctx, err)
	return v, err
}
