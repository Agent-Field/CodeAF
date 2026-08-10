package runner

// Go-level checks for the Cause trichotomy that the TS fixtures cannot reach:
// the error→Cause normalisation, errors.Is plumbing, and the boundary rule
// that decides whether an observed cancellation counts as an interrupt.

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCauseOfPlainErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")

	if IsInterruptOnly(nil) {
		t.Error("the empty cause is not interrupt-only (effect.js:114 requires length > 0)")
	}
	if HasFails(nil) || HasDefects(nil) || HasInterrupts(nil) {
		t.Error("the empty cause has no reasons")
	}
	if Squash(nil).Error() != "Empty cause" {
		t.Errorf("squash(empty) = %v", Squash(nil))
	}
	if Pretty(nil) != "" {
		t.Errorf("pretty(empty) = %q", Pretty(nil))
	}

	if !HasFails(boom) || HasDefects(boom) || HasInterrupts(boom) {
		t.Error("a plain error is exactly one Fail reason")
	}
	if !errors.Is(Squash(boom), boom) {
		t.Error("squash of a lone failure is the error itself")
	}

	// The interruption sentinel classifies as an interrupt wherever it shows up.
	if !IsInterruptOnly(ErrInterrupted) {
		t.Error("ErrInterrupted must be interrupt-only")
	}
	wrapped := &Cause{Reasons: []Reason{{Kind: KindInterrupt}}}
	if !errors.Is(wrapped, ErrInterrupted) {
		t.Error("errors.Is must see through a Cause to its interrupt reason")
	}
	if !errors.Is(Fail(boom), boom) {
		t.Error("errors.Is must see through a Cause to its failure")
	}
}

func TestSquashPrecedence(t *testing.T) {
	t.Parallel()
	fail := errors.New("f")
	die := errors.New("d")
	c := NewCause(
		Reason{Kind: KindInterrupt},
		Reason{Kind: KindDefect, Defect: die, Err: die},
		Reason{Kind: KindFailure, Err: fail},
	)
	// Fail wins over Die wins over Interrupt regardless of position.
	if !errors.Is(Squash(c), fail) {
		t.Errorf("squash = %v, want the failure", Squash(c))
	}
	if !errors.Is(Squash(NewCause(Reason{Kind: KindInterrupt}, Reason{Kind: KindDefect, Defect: die, Err: die})), die) {
		t.Error("squash must prefer a defect over an interrupt")
	}
	if got := Squash(Interrupt()).Error(); got != "All fibers interrupted without error" {
		t.Errorf("squash(interrupt) = %q", got)
	}
}

func TestDieRendersNonErrorDefects(t *testing.T) {
	t.Parallel()
	c := Die("kaboom")
	if !HasDefects(c) {
		t.Fatal("Die must produce a defect reason")
	}
	if got := Squash(c).Error(); got != "kaboom" {
		t.Errorf("squash = %q", got)
	}
	if got := Pretty(Die(42)); got != "Error: 42" {
		t.Errorf("pretty = %q", got)
	}
}

func TestNormalizeCancellation(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")

	// Cancellation inherited from an outer runner scope is an interrupt too
	// (ENGINE-DESIGN.md:1044-1058).
	plain, cancelPlain := context.WithCancel(context.Background())
	cancelPlain()
	if got := normalizeCancellation(plain, context.Canceled); !IsInterruptOnly(got) {
		t.Errorf("an outer cancellation must become an interrupt, got %#v", got)
	}

	// This runner cancelled it: the observed context error becomes an interrupt.
	ours, cancelOurs := context.WithCancelCause(context.Background())
	cancelOurs(ErrInterrupted)
	if got := normalizeCancellation(ours, context.Canceled); !IsInterruptOnly(got) {
		t.Errorf("want an interrupt, got %#v", got)
	}
	if got := normalizeCancellation(ours, ErrInterrupted); !IsInterruptOnly(got) {
		t.Errorf("want an interrupt, got %#v", got)
	}
	// …but a genuine failure that happened to race the cancel is untouched.
	if got := normalizeCancellation(ours, boom); !errors.Is(got, boom) || IsInterruptOnly(got) {
		t.Errorf("a real failure must survive, got %#v", got)
	}
	// An explicit Cause is authoritative and never rewritten.
	mixed := NewCause(Reason{Kind: KindFailure, Err: boom}, Reason{Kind: KindInterrupt})
	if got := normalizeCancellation(ours, mixed); got != error(mixed) {
		t.Errorf("an explicit cause must pass through, got %#v", got)
	}
	if normalizeCancellation(ours, nil) != nil {
		t.Error("nil stays nil")
	}
}

func TestRunWorkRecoversPanics(t *testing.T) {
	t.Parallel()
	v, err := runWork(context.Background(), func(context.Context) (string, error) {
		panic(errors.New("kaboom"))
	})
	if v != "" {
		t.Errorf("a panicking body must not yield a value, got %q", v)
	}
	if !HasDefects(err) {
		t.Fatalf("want a defect, got %#v", err)
	}
	if got := Squash(err).Error(); got != "kaboom" {
		t.Errorf("squash = %q", got)
	}
}

func TestErrorNameAndMessage(t *testing.T) {
	t.Parallel()
	plain := errors.New("boom")
	if ErrorName(plain) != "Error" || ErrorMessage(plain) != "boom" {
		t.Errorf("plain error = %q/%q", ErrorName(plain), ErrorMessage(plain))
	}
	// `class BusyError extends Error` does not set `name`, so JS reports Error.
	var busy error = &BusyError{SessionID: "s1"}
	if ErrorName(busy) != "Error" || ErrorMessage(busy) != "Session s1 is busy" {
		t.Errorf("BusyError = %q/%q", ErrorName(busy), ErrorMessage(busy))
	}
	// Cancelled is a tagged error: name RunnerCancelled, empty message.
	if ErrorName(ErrCancelled) != "RunnerCancelled" || ErrorMessage(ErrCancelled) != "" {
		t.Errorf("Cancelled = %q/%q", ErrorName(ErrCancelled), ErrorMessage(ErrCancelled))
	}
	if ErrorName(nil) != "Error" || ErrorMessage(nil) != "" {
		t.Error("nil renders as an empty Error")
	}
}

func TestPrettySliceBounds(t *testing.T) {
	t.Parallel()
	long := errors.New(strings.Repeat("x", 1000))
	if got := PrettySlice(long, 300); len(got) != 300 {
		t.Errorf("len = %d, want 300", len(got))
	}
	if got := PrettySlice(long, 0); got != "" {
		t.Errorf("n=0 must be empty, got %q", got)
	}
	if got := PrettySlice(long, -5); got != "" {
		t.Errorf("a negative n must be empty, got %q", got)
	}
	if got := PrettySlice(errors.New("boom"), 10_000); got != "Error: boom" {
		t.Errorf("an oversized n must return the whole string, got %q", got)
	}
	if got := PrettySlice(nil, 300); got != "" {
		t.Errorf("the empty cause slices to empty, got %q", got)
	}
}

func TestPrettyInterruptBlock(t *testing.T) {
	t.Parallel()
	want := "InterruptError: All fibers interrupted without error {\n" +
		"  [cause]: InterruptCause: The fiber was interrupted by:\n" +
		"      at fiber (#7)\n}"
	if got := Pretty(InterruptFrom(7)); got != want {
		t.Errorf("pretty:\n got %q\nwant %q", got, want)
	}
	if !strings.Contains(Pretty(Interrupt()), "at fiber (unknown)") {
		t.Errorf("an unknown interruptor renders as (unknown), got %q", Pretty(Interrupt()))
	}
	// Any non-interrupt reason suppresses the synthesised block entirely.
	c := NewCause(Reason{Kind: KindFailure, Err: errors.New("boom")}, Reason{Kind: KindInterrupt})
	if got := Pretty(c); got != "Error: boom" {
		t.Errorf("pretty = %q", got)
	}
}
