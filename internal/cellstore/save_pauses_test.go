package cellstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
)

type countingCapture struct {
	mu          sync.Mutex
	turns       []TurnInfo
	transcripts []string
}

func (s *countingCapture) Seal(_ context.Context, c cell.Cell, in TurnInfo) (Sealed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(rel(c, cell.TranscriptPath))
	if err != nil && !os.IsNotExist(err) {
		return Sealed{}, err
	}
	s.turns = append(s.turns, in)
	s.transcripts = append(s.transcripts, string(raw))
	return Sealed{}, nil
}

func (s *countingCapture) captured() []TurnInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]TurnInfo(nil), s.turns...)
}

func (s *countingCapture) contents() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.transcripts...)
}

func pauseResult(t *testing.T, c cell.Cell, line string) {
	t.Helper()
	file, err := os.OpenFile(rel(c, cell.TranscriptPath), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString(line + "\n")
	closed := file.Close()
	if err != nil || closed != nil {
		t.Fatalf("write tool result: %v %v", err, closed)
	}
}

func pauseRecorder(t *testing.T, store Store, opts Options) (*Recorder, cell.Cell, string) {
	t.Helper()
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Unix(100, 0) }
	}
	c := newCell(t)
	wal := filepath.Join(t.TempDir(), "wal")
	r, err := NewRecorder(&stubExec{}, store, c, wal, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r, c, wal
}

func pauseCall(t *testing.T, r *Recorder, call executor.Call, trigger Trigger) {
	t.Helper()
	if err := r.Around(t.Context(), call, executor.EffectExternal, trigger, func() ([]byte, bool) {
		return call.Args, false
	}); err != nil {
		t.Fatal(err)
	}
}

func waitPauseCapture(t *testing.T, r *Recorder) {
	t.Helper()
	if err := r.wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSavePausesFiveShellStepsSealOnceAtTurnEnd(t *testing.T) {
	s := &countingCapture{}
	now := time.Unix(100, 0)
	r, c, wal := pauseRecorder(t, s, Options{Async: true, Now: func() time.Time { return now }})
	var hashes []string
	var transcript string
	for n := range 5 {
		args := []byte(fmt.Sprintf("shell %d", n))
		pauseCall(t, r, executor.Call{Tool: "bash", Args: args, Deferred: true}, AgentRun)
		line := fmt.Sprintf("result shell %d", n)
		pauseResult(t, c, line)
		transcript += line + "\n"
		executor.SettleStep(t.Context(), sealed{rec: r})
		waitPauseCapture(t, r)
		if got := len(s.captured()); got != 0 {
			t.Fatalf("call %d started %d captures before the pause", n, got)
		}
		_, recovery, err := OpenWAL(wal)
		if err != nil || len(recovery.Completed) != n+1 || len(recovery.Incomplete) != 0 {
			t.Fatalf("call %d was not completed in the WAL: %+v %v", n, recovery, err)
		}
		hashes = append(hashes, hashHex(args))
		now = now.Add(time.Second)
	}
	r.Settle(t.Context())
	waitPauseCapture(t, r)
	turns := s.captured()
	if len(turns) != 1 || len(turns[0].Calls) != 5 || turns[0].Changed != nil {
		t.Fatalf("want one full capture of five calls at Settle: %+v", turns)
	}
	for n, call := range turns[0].Calls {
		if call.Call.ArgsHash != hashes[n] || string(call.Stdout) != fmt.Sprintf("shell %d", n) {
			t.Fatalf("batch lost call %d: %+v", n, call)
		}
	}
	if got := s.contents(); !reflect.DeepEqual(got, []string{transcript}) {
		t.Fatalf("turn-end seal lost step result lines: %q", got)
	}
	_, recovery, err := OpenWAL(wal)
	if err != nil || len(recovery.Completed) != 0 {
		t.Fatalf("sealed calls stayed in the WAL: %+v %v", recovery, err)
	}
}

func TestSavePausesExactPathsCaptureUntilUnknownCallIsPending(t *testing.T) {
	s := &countingCapture{}
	now := time.Unix(100, 0)
	r, _, _ := pauseRecorder(t, s, Options{Async: true, Now: func() time.Time { return now }})
	pauseCall(t, r, executor.Call{Tool: "write", Changed: []string{"first.txt"}}, AgentRun)
	waitPauseCapture(t, r)
	turns := s.captured()
	if len(turns) != 1 || !reflect.DeepEqual(turns[0].Changed, []string{"first.txt"}) {
		t.Fatalf("exact paths did not capture immediately: %+v", turns)
	}
	pauseCall(t, r, executor.Call{Tool: "bash", Args: []byte("unknown")}, AgentRun)
	pauseCall(t, r, executor.Call{Tool: "write", Changed: []string{"second.txt"}}, AgentRun)
	waitPauseCapture(t, r)
	if got := len(s.captured()); got != 1 {
		t.Fatalf("exact call started a full capture over an unsaved shell call: %d", got)
	}
	r.Settle(t.Context())
	waitPauseCapture(t, r)
	turns = s.captured()
	if len(turns) != 2 || len(turns[1].Calls) != 2 || turns[1].Changed != nil {
		t.Fatalf("held exact call was not in the full batch: %+v", turns)
	}
}

func TestSavePausesExactAndTranscriptStepsCaptureImmediately(t *testing.T) {
	s := &countingCapture{}
	r, c, _ := pauseRecorder(t, s, Options{Async: true})
	pauseCall(t, r, executor.Call{Tool: "write", Changed: []string{"first.txt"}, Deferred: true}, AgentRun)
	pauseResult(t, c, "first write result")
	executor.SettleStep(t.Context(), sealed{rec: r})
	waitPauseCapture(t, r)
	if turns := s.captured(); len(turns) != 1 || !reflect.DeepEqual(turns[0].Changed, []string{"first.txt"}) {
		t.Fatalf("exact step did not capture immediately: %+v", turns)
	}
	pauseCall(t, r, executor.Call{Tool: "bash", Args: []byte("unknown"), Deferred: true}, AgentRun)
	pauseResult(t, c, "shell result")
	executor.SettleStep(t.Context(), sealed{rec: r})
	pauseCall(t, r, executor.Call{Tool: "write", Changed: []string{"second.txt"}, Deferred: true}, AgentRun)
	pauseResult(t, c, "second write result")
	executor.SettleStep(t.Context(), sealed{rec: r})
	waitPauseCapture(t, r)
	if len(s.captured()) != 1 {
		t.Fatal("exact step forced a full save while an unknown call was held")
	}
	r.Settle(t.Context())
	waitPauseCapture(t, r)
	if turns := s.captured(); len(turns) != 2 || len(turns[1].Calls) != 2 || turns[1].Changed != nil {
		t.Fatalf("forced settle lost the mixed step batch: %+v", turns)
	}

	transcriptStore := &countingCapture{}
	transcriptRecorder, transcriptCell, _ := pauseRecorder(t, transcriptStore, Options{Async: true})
	pauseResult(t, transcriptCell, "transcript-only answer")
	executor.SettleStep(t.Context(), sealed{rec: transcriptRecorder})
	waitPauseCapture(t, transcriptRecorder)
	if turns := transcriptStore.captured(); len(turns) != 1 || len(turns[0].Calls) != 0 || turns[0].Changed == nil {
		t.Fatalf("transcript-only step did not retain its cheap capture: %+v", turns)
	}
}

func TestSavePausesStepIntervalStartsAtOldestCompletionAndResetsAfterSeal(t *testing.T) {
	s := &countingCapture{}
	now := time.Unix(100, 0)
	r, c, _ := pauseRecorder(t, s, Options{Async: true, Now: func() time.Time { return now }})
	// A long first command has no unsaved completion until it returns.
	if err := r.Around(t.Context(), executor.Call{Tool: "bash", Args: []byte("long"), Deferred: true}, executor.EffectExternal, AgentRun, func() ([]byte, bool) {
		now = now.Add(time.Minute)
		if len(s.captured()) != 0 {
			t.Fatal("capture started during a command")
		}
		return nil, false
	}); err != nil {
		t.Fatal(err)
	}
	pauseResult(t, c, "long command result")
	executor.SettleStep(t.Context(), sealed{rec: r})
	waitPauseCapture(t, r)
	if len(s.captured()) != 0 {
		t.Fatal("the first completion was already treated as overdue")
	}
	now = now.Add(30*time.Second - time.Millisecond)
	pauseCall(t, r, executor.Call{Tool: "bash", Args: []byte("before interval"), Deferred: true}, AgentRun)
	pauseResult(t, c, "second command result")
	executor.SettleStep(t.Context(), sealed{rec: r})
	waitPauseCapture(t, r)
	if len(s.captured()) != 0 {
		t.Fatal("capture started before 30 seconds")
	}
	now = now.Add(time.Millisecond)
	if err := r.Around(t.Context(), executor.Call{Tool: "read", Changed: []string{}, Deferred: true}, executor.EffectLocal, AgentRun, func() ([]byte, bool) {
		if len(s.captured()) != 0 {
			t.Fatal("capture started while the next command was running")
		}
		return nil, false
	}); err != nil {
		t.Fatal(err)
	}
	if len(s.captured()) != 0 {
		t.Fatal("deferred completion captured before its step settle")
	}
	pauseResult(t, c, "read result")
	executor.SettleStep(t.Context(), sealed{rec: r})
	waitPauseCapture(t, r)
	turns := s.captured()
	if len(turns) != 1 || len(turns[0].Calls) != 3 || turns[0].Changed != nil {
		t.Fatalf("oldest completion did not bring the full batch at 30 seconds: %+v", turns)
	}
	now = now.Add(time.Second)
	pauseCall(t, r, executor.Call{Tool: "bash", Args: []byte("new interval"), Deferred: true}, AgentRun)
	pauseResult(t, c, "new interval result")
	executor.SettleStep(t.Context(), sealed{rec: r})
	waitPauseCapture(t, r)
	if len(s.captured()) != 1 {
		t.Fatal("a sealed batch's age leaked into the next interval")
	}
	r.Settle(t.Context())
	waitPauseCapture(t, r)
}

func TestSavePausesExecUsesTheSameInterval(t *testing.T) {
	s := &countingCapture{}
	now := time.Unix(100, 0)
	r, _, _ := pauseRecorder(t, s, Options{Async: true, Now: func() time.Time { return now }})
	for n := range 2 {
		if _, err := r.Exec(t.Context(), req(open, fmt.Sprintf("command-%d", n)), nil); err != nil {
			t.Fatal(err)
		}
		waitPauseCapture(t, r)
		if len(s.captured()) != n {
			t.Fatalf("Exec capture count after call %d: %d", n, len(s.captured()))
		}
		now = now.Add(30 * time.Second)
	}
	if got := s.captured(); len(got) != 1 || len(got[0].Calls) != 2 {
		t.Fatalf("Exec did not batch its completions: %+v", got)
	}
}

func TestSavePausesSetupAndSyncStillCaptureImmediately(t *testing.T) {
	for _, tc := range []struct {
		name    string
		async   bool
		trigger Trigger
	}{
		{"setup", true, Setup},
		{"sync", false, AgentRun},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &countingCapture{}
			r, _, _ := pauseRecorder(t, s, Options{Async: tc.async})
			for n := range 2 {
				pauseCall(t, r, executor.Call{Tool: "bash", Args: []byte(fmt.Sprint(n))}, tc.trigger)
				waitPauseCapture(t, r)
				if turns := s.captured(); len(turns) != n+1 || turns[n].Trigger != tc.trigger {
					t.Fatalf("immediate capture changed: %+v", turns)
				}
			}
			if !tc.async {
				pauseCall(t, r, executor.Call{Tool: "bash", Args: []byte("deferred sync step"), Deferred: true}, tc.trigger)
				executor.SettleStep(t.Context(), sealed{rec: r})
				if turns := s.captured(); len(turns) != 3 || len(turns[2].Calls) != 1 {
					t.Fatalf("synchronous step settle was held: %+v", turns)
				}
			}
		})
	}
}

func TestSavePausesHeldCallIsRecoveredByTheNextRecorder(t *testing.T) {
	s := &countingCapture{}
	r, c, wal := pauseRecorder(t, s, Options{Async: true})
	pauseCall(t, r, executor.Call{Tool: "bash", Args: []byte("external output"), Deferred: true}, AgentRun)
	pauseResult(t, c, "external output result")
	executor.SettleStep(t.Context(), sealed{rec: r})
	waitPauseCapture(t, r)
	_, recovery, err := OpenWAL(wal)
	if err != nil || len(recovery.Completed) != 1 || string(recovery.Completed[0].Stdout) != "external output" || len(s.captured()) != 0 {
		t.Fatalf("held call was not durable before restart: %+v %v", recovery, err)
	}
	inner := &stubExec{}
	next, err := NewRecorder(inner, s, c, wal, Options{Async: true})
	if err != nil {
		t.Fatal(err)
	}
	next.Settle(t.Context())
	waitPauseCapture(t, next)
	turns := s.captured()
	if inner.runs != 0 || len(turns) != 1 || len(turns[0].Calls) != 1 || string(turns[0].Calls[0].Stdout) != "external output" {
		t.Fatalf("restart lost or reran the held call: runs=%d turns=%+v", inner.runs, turns)
	}
	_, recovery, err = OpenWAL(wal)
	if err != nil || len(recovery.Completed) != 0 {
		t.Fatalf("recovered seal did not compact the WAL: %+v %v", recovery, err)
	}
}

func TestSavePausesHeadBoundariesSettleHeldCalls(t *testing.T) {
	for _, boundary := range []string{"await", "captured", "drain", "exit"} {
		t.Run(boundary, func(t *testing.T) {
			s := &countingCapture{}
			r, c, wal := pauseRecorder(t, s, Options{Async: true})
			pauseCall(t, r, executor.Call{Tool: "bash", Args: []byte(boundary), Deferred: true}, AgentRun)
			pauseResult(t, c, boundary+" result")
			if boundary != "drain" {
				executor.SettleStep(t.Context(), sealed{rec: r})
			}
			waitPauseCapture(t, r)
			if len(s.captured()) != 0 {
				t.Fatal("call was captured before the boundary")
			}
			var err error
			switch boundary {
			case "await":
				err = Await(t.Context(), c)
			case "captured":
				err = Captured(t.Context(), c)
			case "drain":
				err = r.Drain(t.Context())
			case "exit":
				err = Flush(t.Context())
			}
			if err != nil {
				t.Fatal(err)
			}
			if turns := s.captured(); len(turns) != 1 || len(turns[0].Calls) != 1 || string(turns[0].Calls[0].Stdout) != boundary {
				t.Fatalf("%s read a stale head: %+v", boundary, turns)
			}
			_, recovery, err := OpenWAL(wal)
			if err != nil || len(recovery.Completed) != 0 {
				t.Fatalf("%s left a completed call unsealed: %+v %v", boundary, recovery, err)
			}
		})
	}
}

func TestSavePausesCommandWaitLeavesHeldCallsPending(t *testing.T) {
	s := &countingCapture{}
	r, c, _ := pauseRecorder(t, s, Options{Async: true})
	pauseCall(t, r, executor.Call{Tool: "bash"}, AgentRun)
	executor.SettleStep(t.Context(), sealed{rec: r})
	if err := WaitCapture(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if len(s.captured()) != 0 {
		t.Fatal("ordinary ownership check started a capture")
	}
	r.Settle(t.Context())
	waitPauseCapture(t, r)
}

func TestSavePausesCapturedLeavesAnOpenStepInTheWAL(t *testing.T) {
	s := &countingCapture{}
	r, c, wal := pauseRecorder(t, s, Options{Async: true})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := r.Around(t.Context(), executor.Call{Tool: "bash", Deferred: true}, executor.EffectExternal, AgentRun, func() ([]byte, bool) {
		if err := Captured(ctx, c); err != nil {
			t.Fatalf("boundary waited for the running tool: %v", err)
		}
		if len(s.captured()) != 0 {
			t.Fatal("boundary started a capture during a tool call")
		}
		return []byte("finished after boundary request"), false
	}); err != nil {
		t.Fatal(err)
	}
	if err := Captured(ctx, c); err != nil {
		t.Fatalf("boundary waited for an unwritten step result: %v", err)
	}
	waitPauseCapture(t, r)
	_, recovery, err := OpenWAL(wal)
	if err != nil || len(recovery.Completed) != 1 || len(s.captured()) != 0 {
		t.Fatalf("open step did not remain in the WAL: %+v %v", recovery, err)
	}
	pauseResult(t, c, "tool result written after the boundary")
	executor.SettleStep(t.Context(), sealed{rec: r})
	waitPauseCapture(t, r)
	if len(s.captured()) != 0 {
		t.Fatal("young step settle unexpectedly captured")
	}
	if err := Captured(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if turns := s.captured(); len(turns) != 1 || len(turns[0].Calls) != 1 || string(turns[0].Calls[0].Stdout) != "finished after boundary request" || !reflect.DeepEqual(s.contents(), []string{"tool result written after the boundary\n"}) {
		t.Fatalf("closed-step boundary lost the completed call or its result: %+v", turns)
	}
}

func TestSavePausesClosedStepBoundaryBlocksTheNextEnter(t *testing.T) {
	s := blockingCapture{make(chan struct{}, 1), make(chan struct{}), make(chan TurnInfo, 1)}
	r, c, _ := pauseRecorder(t, s, Options{Async: true})
	var release sync.Once
	t.Cleanup(func() {
		release.Do(func() { close(s.release) })
		if err := r.wait(context.Background()); err != nil {
			t.Error(err)
		}
	})
	pauseCall(t, r, executor.Call{Tool: "bash", Args: []byte("held"), Deferred: true}, AgentRun)
	pauseResult(t, c, "held result")
	executor.SettleStep(t.Context(), sealed{rec: r})
	boundary := make(chan error, 1)
	go func() { boundary <- Captured(t.Context(), c) }()
	<-s.entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ran := false
	err := r.Around(ctx, executor.Call{Tool: "bash"}, executor.EffectExternal, AgentRun, func() ([]byte, bool) {
		ran = true
		return nil, false
	})
	if !errors.Is(err, context.Canceled) || ran {
		t.Fatalf("tool entered while the closed-step boundary sealed: ran=%v err=%v", ran, err)
	}
	release.Do(func() { close(s.release) })
	if err := <-boundary; err != nil {
		t.Fatal(err)
	}
	if turn := <-s.calls; len(turn.Calls) != 1 || string(turn.Calls[0].Stdout) != "held" {
		t.Fatalf("boundary lost the held step: %+v", turn)
	}
}

func TestSavePausesOpenStepBoundaryJoinsOnlyTheExistingCapture(t *testing.T) {
	s := blockingCapture{make(chan struct{}, 4), make(chan struct{}), make(chan TurnInfo, 4)}
	r, c, _ := pauseRecorder(t, s, Options{Async: true})
	var release sync.Once
	t.Cleanup(func() {
		release.Do(func() { close(s.release) })
		if err := r.wait(context.Background()); err != nil {
			t.Error(err)
		}
	})
	pauseCall(t, r, executor.Call{Tool: "write", Changed: []string{"first.txt"}}, AgentRun)
	<-s.entered
	pauseResult(t, c, "result written during the first capture")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Captured(ctx, c); !errors.Is(err, context.Canceled) {
		t.Fatalf("open-step boundary did not join the existing capture: %v", err)
	}
	release.Do(func() { close(s.release) })
	if err := Captured(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	waitPauseCapture(t, r)
	if len(s.calls) != 1 {
		t.Fatalf("open-step boundary scheduled another capture: %d", len(s.calls))
	}
}

func TestSavePausesReadOnlyBoundaryKeepsTheWALAndFails(t *testing.T) {
	r, c, wal := pauseRecorder(t, failedCapture{ErrReadOnly}, Options{Async: true})
	pauseCall(t, r, executor.Call{Tool: "bash"}, AgentRun)
	executor.SettleStep(t.Context(), sealed{rec: r})
	if err := Await(t.Context(), c); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("boundary acknowledged a refused capture: %v", err)
	}
	_, recovery, err := OpenWAL(wal)
	if err != nil || len(recovery.Completed) != 1 {
		t.Fatalf("refused boundary lost its held call: %+v %v", recovery, err)
	}
}

func TestSavePausesDirtyChecksSettleAndRememberExternalOutput(t *testing.T) {
	for _, boundary := range []string{"dirty", "local work"} {
		t.Run(boundary, func(t *testing.T) {
			s := &countingCapture{}
			c := newCell(t)
			e := Engine{DataRoot: t.TempDir()}
			r, err := NewRecorder(&stubExec{}, s, c, e.WALPath(c), Options{Async: true, Now: func() time.Time { return time.Unix(100, 0) }})
			if err != nil {
				t.Fatal(err)
			}
			pauseCall(t, r, executor.Call{Tool: "bash", Args: []byte("external output only")}, AgentRun)
			var work bool
			if boundary == "dirty" {
				work, err = e.Dirty(t.Context(), c)
			} else {
				work, err = e.LocalWork(t.Context(), c)
			}
			if err != nil || !work || len(s.captured()) != 0 {
				t.Fatalf("%s did not preserve the open step in the WAL: work=%v captures=%d err=%v", boundary, work, len(s.captured()), err)
			}
			executor.SettleStep(t.Context(), sealed{rec: r})
			if boundary == "dirty" {
				work, err = e.Dirty(t.Context(), c)
			} else {
				work, err = e.LocalWork(t.Context(), c)
			}
			if err != nil || !work || len(s.captured()) != 1 {
				t.Fatalf("%s lost the held external effect: work=%v captures=%d err=%v", boundary, work, len(s.captured()), err)
			}
		})
	}
}

func TestSavePausesRewindKeepsThePreviousTurnFiles(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		e := realEngine(t)
		c := newCell(t)
		first := sealStep(t, e, c, map[string]string{"work.txt": "before this turn"}, `{"turn":1}`)
		r, err := NewRecorder(&stubExec{}, e, c, e.WALPath(c), Options{Async: true, Now: func() time.Time { return time.Unix(100, 0) }})
		if err != nil {
			t.Fatal(err)
		}
		for n := range 2 {
			if err := r.Around(t.Context(), executor.Call{Tool: "bash", Args: []byte(fmt.Sprint(n)), Deferred: true}, executor.EffectLocal, AgentRun, func() ([]byte, bool) {
				if err := os.WriteFile(filepath.Join(c.Root, "work.txt"), []byte(fmt.Sprintf("during this turn %d", n)), 0600); err != nil {
					t.Fatal(err)
				}
				return nil, false
			}); err != nil {
				t.Fatal(err)
			}
			pauseResult(t, c, fmt.Sprintf(`{"turn":2,"result":%d}`, n))
		}
		executor.SettleStep(t.Context(), sealed{rec: r})
		// Rewind must keep the work being undone in history before restoring.
		rewound, err := e.Rewind(t.Context(), c, first.Turn.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(filepath.Join(c.Root, "work.txt")); err != nil || string(got) != "before this turn" {
			t.Fatalf("rewind did not restore the turn's starting files: %q %v", got, err)
		}
		turns, err := Log(c)
		if err != nil || len(turns) != 3 || rewound.Turn.Parent != turns[1].Turn.ID || len(turns[1].Receipt.Calls) != 2 {
			t.Fatalf("rewind did not settle both held calls before restoring: %+v %v", turns, err)
		}
	})
}

func TestSavePausesRewindRefusesAnOpenStep(t *testing.T) {
	s := &countingCapture{}
	c := newCell(t)
	e := Engine{DataRoot: t.TempDir()}
	r, err := NewRecorder(&stubExec{}, s, c, e.WALPath(c), Options{Async: true})
	if err != nil {
		t.Fatal(err)
	}
	pauseCall(t, r, executor.Call{Tool: "bash", Deferred: true}, AgentRun)
	if _, err := e.Rewind(t.Context(), c, "older"); !errors.Is(err, ErrStepOpen) {
		t.Fatalf("rewind accepted an open step: %v", err)
	}
	_, recovery, err := OpenWAL(e.WALPath(c))
	if err != nil || len(recovery.Completed) != 1 || len(s.captured()) != 0 {
		t.Fatalf("refused rewind changed the held call: %+v %v", recovery, err)
	}
	r.Settle(t.Context())
	waitPauseCapture(t, r)
}
