package runner

// The ENGINE-DESIGN.md:1160-1182 truth table. Timeout cases block on
// ctx.Done(), so tests never sleep to guess whether a deadline or transition
// has happened.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const tick = 5 * time.Millisecond

func TestDesignL1163To1182DegradeTruthTable(t *testing.T) {
	t.Parallel()
	const fallback = "@fallback"

	cases := []struct {
		name string
		d    time.Duration
		ctx  func() context.Context
		fn   func(context.Context) (string, error)
		want string
	}{
		{
			name: "success passes through",
			fn:   func(context.Context) (string, error) { return "V", nil },
			want: "V",
		},
		{
			name: "failure collapses",
			fn:   func(context.Context) (string, error) { return "", errors.New("boom") },
			want: fallback,
		},
		{
			name: "partial value plus failure collapses",
			fn:   func(context.Context) (string, error) { return "partial", errors.New("boom") },
			want: fallback,
		},
		{
			name: "panic error collapses",
			fn:   func(context.Context) (string, error) { panic(errors.New("kaboom")) },
			want: fallback,
		},
		{
			name: "panic value collapses",
			fn:   func(context.Context) (string, error) { panic("kaboom") },
			want: fallback,
		},
		{
			name: "defect cause collapses",
			fn:   func(context.Context) (string, error) { return "", Die(errors.New("defect")) },
			want: fallback,
		},
		{
			name: "interrupt cause collapses",
			fn:   func(context.Context) (string, error) { return "", Interrupt() },
			want: fallback,
		},
		{
			name: "mixed failure and interrupt collapses once",
			fn: func(context.Context) (string, error) {
				return "", NewCause(
					Reason{Kind: KindFailure, Err: errors.New("boom")},
					Reason{Kind: KindInterrupt},
				)
			},
			want: fallback,
		},
		{
			name: "timeout collapses",
			d:    tick,
			fn: func(ctx context.Context) (string, error) {
				<-ctx.Done()
				return "", context.Cause(ctx)
			},
			want: fallback,
		},
		{
			name: "timeout beats late success",
			d:    tick,
			fn: func(ctx context.Context) (string, error) {
				<-ctx.Done()
				return "late", nil
			},
			want: fallback,
		},
		{
			name: "outer cancellation also collapses",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			fn:   func(ctx context.Context) (string, error) { return "", context.Cause(ctx) },
			want: fallback,
		},
		{
			name: "outer cancellation through a deadline also collapses",
			d:    time.Minute,
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			fn:   func(ctx context.Context) (string, error) { return "", ctx.Err() },
			want: fallback,
		},
		{
			name: "context canceled value without cancellation is still collapsed",
			fn:   func(context.Context) (string, error) { return "", context.Canceled },
			want: fallback,
		},
		{
			name: "deadline with room passes through",
			d:    time.Minute,
			fn:   func(context.Context) (string, error) { return "V", nil },
			want: "V",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if tc.ctx != nil {
				ctx = tc.ctx()
			}
			if got := Value(ctx, tc.d, fallback, tc.fn); got != tc.want {
				t.Errorf("Value() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDesignL1163TimeoutAwaitsWorkFinalization(t *testing.T) {
	t.Parallel()
	sawDeadline := make(chan struct{})
	release := make(chan struct{})
	done := make(chan string, 1)

	go func() {
		done <- Value(context.Background(), tick, "@fallback", func(ctx context.Context) (string, error) {
			<-ctx.Done()
			close(sawDeadline)
			<-release
			return "", ctx.Err()
		})
	}()

	<-sawDeadline
	select {
	case v := <-done:
		t.Fatalf("Value returned %q before protected work finalized", v)
	default:
	}
	close(release)
	if got := <-done; got != "@fallback" {
		t.Errorf("Value() = %q, want fallback", got)
	}
}

func TestDesignL1164NonPositiveDurationDisablesDeadline(t *testing.T) {
	t.Parallel()
	for _, d := range []time.Duration{0, -time.Second} {
		release := make(chan struct{})
		done := make(chan string, 1)
		go func() {
			done <- Value(context.Background(), d, "@fallback", func(context.Context) (string, error) {
				<-release
				return "V", nil
			})
		}()
		close(release)
		if got := <-done; got != "V" {
			t.Errorf("d=%v: Value() = %q, want V", d, got)
		}
	}
}

func TestDesignL1167VoidSpecialization(t *testing.T) {
	t.Parallel()
	Void(context.Background(), 0, func(context.Context) error {
		return errors.New("boom")
	})
	Void(context.Background(), 0, func(context.Context) error {
		panic("boom")
	})
	Void(context.Background(), tick, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	Void(ctx, 0, func(c context.Context) error { return context.Cause(c) })
}

func TestDesignL1170LogUsesTheSameCollapse(t *testing.T) {
	t.Parallel()
	if got := Log(context.Background(), 0, "@fallback", "gate failed",
		func(context.Context) (string, error) {
			return "", errors.New(strings.Repeat("x", 1000))
		}); got != "@fallback" {
		t.Errorf("Log failure = %q", got)
	}
	if got := Log(context.Background(), time.Minute, "@fallback", "gate ok",
		func(context.Context) (string, error) { return "V", nil }); got != "V" {
		t.Errorf("Log success = %q", got)
	}
	if got := Log(context.Background(), tick, "@fallback", "gate timeout",
		func(ctx context.Context) (string, error) {
			<-ctx.Done()
			return "", context.Cause(ctx)
		}); got != "@fallback" {
		t.Errorf("Log timeout = %q", got)
	}
}

func TestValueNilContext(t *testing.T) {
	t.Parallel()
	//nolint:staticcheck // the nil-context defense is deliberate.
	if got := Value(nil, 0, "@fallback", func(context.Context) (string, error) {
		return "V", nil
	}); got != "V" {
		t.Errorf("Value(nil) = %q, want V", got)
	}
}
