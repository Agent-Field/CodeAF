package cellstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
)

func newRecorder(t *testing.T, c cell.Cell, inner executor.Executor, walPath string) (*Recorder, *fakeEngine) {
	t.Helper()
	fake := &fakeEngine{}
	r, err := NewRecorder(inner, fake.engine(t), c, walPath, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return r, fake
}

func req(net executor.NetPolicy, argv ...string) executor.ExecRequest {
	return executor.ExecRequest{Argv: argv, Net: net}
}

var open = executor.NetPolicy{Open: true}

func TestEachCallSealsOneTurnWithItsReceiptRow(t *testing.T) {
	c := newCell(t)
	r, fake := newRecorder(t, c, &stubExec{}, filepath.Join(t.TempDir(), "wal"))
	for _, argv := range [][]string{{"a"}, {"b"}} {
		if _, err := r.Exec(context.Background(), req(executor.NetPolicy{}, argv...), nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := fake.count("turn-end"); got != 2 {
		t.Fatalf("%d seals for two calls", got)
	}
	head, _ := Head(c)
	if len(head.Receipt.Calls) != 1 || head.Receipt.Calls[0].SideEffect != "local" || head.Turn.Parent == "" {
		t.Fatalf("head %+v", head)
	}
}

func TestSealFailureDoesNotFailTheCallAndRidesTheNextSeal(t *testing.T) {
	c := newCell(t)
	fail := true
	fake := &fakeEngine{}
	e := fake.engine(t)
	good := e.Run
	e.Run = func(ctx context.Context, d string, env []string, argv ...string) ([]byte, error) {
		if fail && len(argv) > 2 && argv[2] == "hook" {
			return nil, errors.New("disk full")
		}
		return good(ctx, d, env, argv...)
	}
	var reported []error
	r, err := NewRecorder(&stubExec{}, e, c, filepath.Join(t.TempDir(), "wal"), Options{Report: func(err error) {
		if err != nil {
			reported = append(reported, err)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Exec(context.Background(), req(executor.NetPolicy{}, "one"), nil); err != nil {
		t.Fatalf("call failed with the seal: %v", err)
	}
	fail = false
	if _, err := r.Exec(context.Background(), req(executor.NetPolicy{}, "two"), nil); err != nil {
		t.Fatal(err)
	}
	head, _ := Head(c)
	if len(reported) != 1 || len(head.Receipt.Calls) != 2 {
		t.Fatalf("reported %v, head calls %d; want one report and both calls in the turn", reported, len(head.Receipt.Calls))
	}
}

// A harness that dies after logging intent and before the result leaves an
// intent with no completion. (L9)
func crashMidCall(t *testing.T, c cell.Cell, wal string, net executor.NetPolicy) {
	t.Helper()
	inner := &stubExec{fn: func(executor.ExecRequest) (executor.ExecResult, error) { panic("harness died") }}
	r, _ := newRecorder(t, c, inner, wal)
	func() {
		defer func() { _ = recover() }()
		_, _ = r.Exec(context.Background(), req(net, "curl", "example.com"), nil)
	}()
}

func TestIncompleteCallIsSurfacedAndNeverRerun(t *testing.T) {
	for name, tc := range map[string]struct {
		net   executor.NetPolicy
		rerun bool
	}{"external": {open, false}, "local": {executor.NetPolicy{}, true}} {
		t.Run(name, func(t *testing.T) {
			c := newCell(t)
			wal := filepath.Join(t.TempDir(), "wal")
			crashMidCall(t, c, wal, tc.net)

			inner := &stubExec{}
			r, fake := newRecorder(t, c, inner, wal)
			got := r.Incomplete()
			if len(got) != 1 || got[0].SideEffect != name || got[0].MayRerun() != tc.rerun {
				t.Fatalf("incomplete = %+v", got)
			}
			if inner.runs != 0 || fake.count("turn-end") != 0 {
				t.Fatalf("reopen ran %d calls and %d seals; it must run none", inner.runs, fake.count("turn-end"))
			}
			if err := r.Resolve(got[0]); err != nil || len(r.Incomplete()) != 0 {
				t.Fatalf("resolve: %v %v", err, r.Incomplete())
			}
			again, _ := newRecorder(t, c, inner, wal)
			if len(again.Incomplete()) != 0 {
				t.Fatal("a resolved intent came back")
			}
		})
	}
}

func TestIncompleteSurvivesLaterSeals(t *testing.T) {
	c := newCell(t)
	wal := filepath.Join(t.TempDir(), "wal")
	crashMidCall(t, c, wal, open)
	r, _ := newRecorder(t, c, &stubExec{}, wal)
	if _, err := r.Exec(context.Background(), req(executor.NetPolicy{}, "next"), nil); err != nil {
		t.Fatal(err)
	}
	if len(r.Incomplete()) != 1 {
		t.Fatal("sealing another call dropped the incomplete one")
	}
	reopened, _ := newRecorder(t, c, &stubExec{}, wal)
	if len(reopened.Incomplete()) != 1 {
		t.Fatal("compaction dropped the incomplete intent")
	}
}

func TestCompletedButUnsealedCallRidesTheNextSeal(t *testing.T) {
	c := newCell(t)
	wal := filepath.Join(t.TempDir(), "wal")
	w, _, _ := OpenWAL(wal)
	i := Intent{V: 1, Tool: "exec", ArgsHash: "aa", Started: 5, SideEffect: "external"}
	_ = w.Begin(i)
	_ = w.Finish(i, Executed{Call: Call{Tool: "exec", ArgsHash: "aa", Started: 5, SideEffect: "external"}, Stdout: []byte("kept")})

	r, _ := newRecorder(t, c, &stubExec{}, wal)
	if _, err := r.Exec(context.Background(), req(executor.NetPolicy{}, "x"), nil); err != nil {
		t.Fatal(err)
	}
	head, _ := Head(c)
	if len(head.Receipt.Calls) != 2 || head.Receipt.Calls[0].ArgsHash != "aa" {
		t.Fatalf("calls %+v", head.Receipt.Calls)
	}
	if got, _ := os.ReadFile(rel(c, BlobsDir+"/"+hashHex([]byte("kept")))); string(got) != "kept" {
		t.Fatal("external output lost across the crash")
	}
}

func TestExternalCallKeepsOutputInTheWAL(t *testing.T) {
	c := newCell(t)
	wal := filepath.Join(t.TempDir(), "wal")
	inner := &stubExec{fn: func(executor.ExecRequest) (executor.ExecResult, error) {
		return executor.ExecResult{Stdout: []byte("resp"), SideEffect: executor.EffectExternal}, nil
	}}
	fake := &fakeEngine{}
	e := fake.engine(t)
	e.Run = func(context.Context, string, []string, ...string) ([]byte, error) { return nil, errors.New("down") }
	r, _ := NewRecorder(inner, e, c, wal, Options{})
	_, _ = r.Exec(context.Background(), req(open, "curl"), nil)

	_, rec, _ := OpenWAL(wal)
	if len(rec.Completed) != 1 || string(rec.Completed[0].Stdout) != "resp" {
		t.Fatalf("wal completed %+v", rec.Completed)
	}
}

func TestFlagOffWrapIsTheExecutorItself(t *testing.T) {
	t.Setenv(cell.EnvVar, "")
	c := newCell(t)
	inner := &stubExec{}
	got, err := Wrap(inner, c)
	if err != nil || got != executor.Executor(inner) {
		t.Fatalf("Wrap with the flag off = %#v, %v; want the same executor", got, err)
	}
	if _, err := got.Exec(context.Background(), req(executor.NetPolicy{}, "x"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rel(c, TurnsPath)); !os.IsNotExist(err) {
		t.Fatalf("flag off wrote %s", TurnsPath)
	}
}

func TestFlagOnWrapRecords(t *testing.T) {
	t.Setenv(cell.EnvVar, "1")
	t.Setenv("CODEAF_HOME", t.TempDir())
	got, err := Wrap(&stubExec{}, newCell(t))
	if _, ok := got.(*Recorder); err != nil || !ok {
		t.Fatalf("Wrap with the flag on = %T, %v", got, err)
	}
}
