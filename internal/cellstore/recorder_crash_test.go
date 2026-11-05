package cellstore

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// crashSeal stops the Recorder at the Store boundary without letting it remove
// the completion from its WAL. The underlying engine still writes real receipts.
type crashSeal struct {
	Store
	after bool
}

func (s crashSeal) Seal(ctx context.Context, c cell.Cell, info TurnInfo) (Sealed, error) {
	if s.after {
		if _, err := s.Store.Seal(ctx, c, info); err != nil {
			return Sealed{}, err
		}
	}
	panic("simulated process exit")
}

func TestCompletionBookedOnceAcrossSealCrash(t *testing.T) {
	for _, after := range []bool{false, true} {
		name := "before_seal"
		if after {
			name = "after_seal_before_WAL_removal"
		}
		t.Run(name, func(t *testing.T) {
			for _, reopen := range []string{"recorder", "materialize"} {
				t.Run(reopen, func(t *testing.T) {
					c := newCell(t)
					e := (&fakeEngine{}).engine(t)
					walPath := e.WALPath(c)
					r, err := NewRecorder(&stubExec{}, crashSeal{Store: e, after: after}, c, walPath, Options{})
					if err != nil {
						t.Fatal(err)
					}
					func() {
						defer func() {
							if got := recover(); got != "simulated process exit" {
								t.Fatalf("crash = %v", got)
							}
						}()
						_, _ = r.Exec(t.Context(), req(open, "charge-once"), nil)
					}()
					// A newer receipt must not hide the completion's earlier sealed receipt.
					if _, err := e.Seal(t.Context(), c, TurnInfo{}); err != nil {
						t.Fatal(err)
					}
					for range 2 {
						if reopen == "recorder" {
							r, err = NewRecorder(&stubExec{fn: func(executor.ExecRequest) (executor.ExecResult, error) {
								t.Fatal("recovery reran external call")
								return executor.ExecResult{}, nil
							}}, e, c, walPath, Options{})
							if err != nil {
								t.Fatal(err)
							}
							r.Settle(t.Context())
						} else if _, err := e.SealRecovered(t.Context(), c); err != nil {
							t.Fatal(err)
						}
					}
					entries, err := Log(c)
					if err != nil {
						t.Fatal(err)
					}
					booked := 0
					for _, entry := range entries {
						for _, call := range entry.Receipt.Calls {
							if call.SideEffect == "external" {
								booked++
							}
						}
					}
					if booked != 1 {
						t.Fatalf("external completion booked %d times, want exactly once", booked)
					}
					_, recovery, err := OpenWAL(walPath)
					if err != nil {
						t.Fatal(err)
					}
					if len(recovery.Completed) != 0 {
						t.Fatalf("WAL retained %d completed calls", len(recovery.Completed))
					}
				})
			}
		})
	}
}

func TestRecoveryKeepsUnsealedCompletionBesideSealedCompletion(t *testing.T) {
	c := newCell(t)
	e := (&fakeEngine{}).engine(t)
	wal, _, err := OpenWAL(e.WALPath(c))
	if err != nil {
		t.Fatal(err)
	}
	first := Intent{V: walV, Tool: "exec", ArgsHash: "first", Started: 1, SideEffect: "external"}
	second := Intent{V: walV, Tool: "exec", ArgsHash: "second", Started: 2, SideEffect: "external"}
	unfinished := Intent{V: walV, Tool: "exec", ArgsHash: "unfinished", Started: 3, SideEffect: "external"}
	for _, i := range []Intent{first, second} {
		if err := wal.Begin(i); err != nil {
			t.Fatal(err)
		}
		done := executed(i, executor.ExecResult{Stdout: []byte("charged")}, nil, i.Started+1)
		if err := wal.Finish(i, done); err != nil {
			t.Fatal(err)
		}
		if i.ArgsHash == first.ArgsHash {
			if _, err := e.Seal(t.Context(), c, TurnInfo{Calls: []Executed{done}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := wal.Begin(unfinished); err != nil {
		t.Fatal(err)
	}
	if _, err := e.SealRecovered(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	entries, err := Log(c)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, entry := range entries {
		for _, call := range entry.Receipt.Calls {
			counts[call.ArgsHash]++
		}
	}
	if counts["first"] != 1 || counts["second"] != 1 || len(counts) != 2 {
		t.Fatalf("completion counts = %v, want first and second once", counts)
	}
	_, recovery, err := OpenWAL(e.WALPath(c))
	if err != nil {
		t.Fatal(err)
	}
	if len(recovery.Completed) != 0 || len(recovery.Incomplete) != 1 || recovery.Incomplete[0].ArgsHash != "unfinished" {
		t.Fatalf("recovery lost unresolved intent or retained booked calls: %+v", recovery)
	}
}
