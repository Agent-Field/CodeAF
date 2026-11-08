package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

// afRelayMore adds entries to an execution's relay after the ones it holds,
// numbered on from the last seq the test gave it.
func (f *afFakeCP) afRelayMore(exec string, seq int64, kind string, data string) {
	f.set(func() {
		f.held[exec] = append(f.held[exec], agentfield.RelayEntry{Seq: seq, Kind: kind, Data: json.RawMessage(data)})
	})
}

// afUntilRead waits until the control plane holds nothing for exec: every
// entry it relayed has been read, kept and acked.
func afUntilRead(t *testing.T, cp *afFakeCP, exec string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for cp.heldCount(exec) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%s's relay was never read to its end: %d held", exec, cp.heldCount(exec))
		}
		time.Sleep(10 * time.Millisecond)
	}
	// raiseAsks starts any question before the ack; give it a moment to call.
	time.Sleep(50 * time.Millisecond)
}

func afSteers(cp *afFakeCP) string {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	return strings.Join(cp.steers, "\n")
}

// AN ANSWERED QUESTION STAYS ANSWERED. The answer reaches the step over steer,
// and the step takes its question down ("ask_done") only once it reads that
// steer; every line it relays in between used to carry the question back to
// the job, which asked it again with the same words (live A+B run 2, job 78,
// E2). Nor does a factory that restarts in that window ask it again from the
// questions it keeps on disk.
func TestAnAnsweredHarnessQuestionIsNotAskedAgain(t *testing.T) {
	appDayProfile(t)
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })
	st, it, stage := afStepJob(t)
	cp := &afFakeCP{tree: afNode("exec-root", "build_issue", "running", "2026-11-07T12:00:00Z", "",
		afNode("e-code", "coder", "running", "2026-11-07T12:00:01Z", ""))}
	cp.relayLines("e-code", [][]byte{
		[]byte(`{"type":"session","version":1,"id":"nodecoder","cwd":"/node","model":"m","timestamp":"2026-11-07T12:00:01Z"}`),
		[]byte(`{"type":"ask","id":"ask-1","question":"Count a zero entry?","options":["Yes","No"],"timestamp":"2026-11-07T12:00:02Z"}`),
	}, false)
	workspace := t.TempDir()
	var asks atomic.Int32
	ask := func(context.Context, factory.Asked) (string, error) {
		asks.Add(1)
		return "the person answered: No", nil
	}
	start := func() (context.CancelFunc, chan struct{}) {
		r := &afRemote{items: st, floor: st, workspace: workspace, conn: func() (afClient, string, bool) { return cp, "workaf-node", true }}
		cur, _ := st.Get(it.ID)
		ctx, cancel := context.WithCancel(context.Background())
		stepped := make(chan struct{})
		go func() { defer close(stepped); _, _ = r.Step(ctx, factoryrun.Job{Item: cur, Stage: stage, Ask: ask}) }()
		return cancel, stepped
	}
	cancel, stepped := start()
	t.Cleanup(func() { cancel(); <-stepped })
	deadline := time.Now().Add(5 * time.Second)
	for afSteers(cp) != `e-code: {"answer":"ask-1","text":"No","by":"person"}` {
		if time.Now().After(deadline) {
			t.Fatalf("the answer never went down: steers %q", afSteers(cp))
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The step works on before it reads the steer: more lines, no ask_done.
	cp.afRelayMore("e-code", 10, "line", `{"type":"message","role":"assistant","content":"thinking","timestamp":"2026-11-07T12:00:03Z"}`)
	afUntilRead(t, cp, "e-code")
	cp.afRelayMore("e-code", 11, "line", `{"type":"message","role":"assistant","content":"still","timestamp":"2026-11-07T12:00:04Z"}`)
	afUntilRead(t, cp, "e-code")
	if n := asks.Load(); n != 1 {
		t.Fatalf("the answered question was put to the job %d times", n)
	}
	// The factory restarts (this runner lets go; a new one follows the run).
	cancel()
	<-stepped
	cancel, stepped = start()
	cp.afRelayMore("e-code", 12, "line", `{"type":"message","role":"assistant","content":"after restart","timestamp":"2026-11-07T12:00:05Z"}`)
	afUntilRead(t, cp, "e-code")
	if n := asks.Load(); n != 1 {
		t.Fatalf("after a restart the answered question was put to the job again (%d asks)", n)
	}
	if s := afSteers(cp); strings.Count(s, `"answer":"ask-1"`) != 1 {
		t.Fatalf("the answer went down more than once: %q", s)
	}
}

// A QUESTION THE STEP TOOK DOWN COMES OFF THE JOB, AND THE RUN'S END LANDS.
// A harness question withdrawn without an answer (the step's own default,
// the run ending first) leaves no job sitting in Needs you on a question
// nobody asks any more; and the run's end still ends the step, with the pull
// request it opened on the job.
func TestAWithdrawnHarnessQuestionLeavesTheJobAndTheRunsEndLands(t *testing.T) {
	appDayProfile(t)
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })
	st, it, stage := afStepJob(t)
	cp := &afFakeCP{tree: afNode("exec-root", "build_issue", "running", "2026-11-07T12:00:00Z", "",
		afNode("e-code", "coder", "running", "2026-11-07T12:00:01Z", "")),
		result: json.RawMessage(`{"success":true,"pr_url":"https://github.com/santoshkumarradha/factory-demo/pull/209"}`)}
	cp.relayLines("e-code", [][]byte{
		[]byte(`{"type":"session","version":1,"id":"nodecoder","cwd":"/node","model":"m","timestamp":"2026-11-07T12:00:01Z"}`),
		[]byte(`{"type":"ask","id":"ask-1","question":"Count a zero entry?","options":["Yes","No"],"timestamp":"2026-11-07T12:00:02Z"}`),
	}, false)
	r := &afRemote{items: st, floor: st, workspace: t.TempDir(), conn: func() (afClient, string, bool) { return cp, "workaf-node", true }}
	// The job's ask as the floor's inbox writes it: the person holds it, and
	// it waits until it is answered or withdrawn.
	ask := func(ctx context.Context, q factory.Asked) (string, error) {
		_ = st.Update(it.ID, func(cur *factory.Item) error {
			cur.State, cur.QKind, cur.Question = factory.StateNeedsYou, factory.QKindStep, "build asks: "+q.Question
			held := q
			held.Stage, held.With = "build", factory.AskedYou
			cur.Asking = &held
			return nil
		})
		<-ctx.Done()
		return "", ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type stepped struct {
		res factory.StageResult
		err error
	}
	done := make(chan stepped, 1)
	go func() {
		res, err := r.Step(ctx, factoryrun.Job{Item: it, Stage: stage, Ask: ask})
		done <- stepped{res, err}
	}()
	afUntil(t, st, it.ID, "asked", func(i factory.Item) bool { return i.Asking != nil })
	cp.afRelayMore("e-code", 10, "line", `{"type":"ask_done","id":"ask-1"}`)
	afUntil(t, st, it.ID, "off Needs you once the step took its question down", func(i factory.Item) bool {
		return i.Asking == nil && i.Question == "" && i.QKind == "" && i.State == factory.StateRunning
	})
	// The run ends: the step ends with it, and its pull request is the job's.
	end := "2026-11-07T12:02:37Z"
	cp.set(func() {
		cp.tree = afNode("exec-root", "build_issue", "succeeded", "2026-11-07T12:00:00Z", end,
			afNode("e-code", "coder", "succeeded", "2026-11-07T12:00:01Z", end))
		cp.held["e-code"] = append(cp.held["e-code"], agentfield.RelayEntry{Seq: 11, Kind: "ended", Data: json.RawMessage(`{}`)})
	})
	select {
	case got := <-done:
		if got.err != nil || !got.res.Done {
			t.Fatalf("the run's end did not end the step: %+v %v", got.res, got.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the step never saw its run end")
	}
	if cur, _ := st.Get(it.ID); cur.PullRequest == nil || cur.PullRequest.Number != 209 || cur.Asking != nil {
		t.Fatalf("after the run's end: pr %+v asking %+v", cur.PullRequest, cur.Asking)
	}
}
