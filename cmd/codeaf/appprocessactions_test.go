package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/service"
	"github.com/Agent-Field/codeaf/internal/session"
)

// actorEngine is an owner engine with the stamped doors. Each door records
// what it was asked; StopConversationProcesses can be made to wait for its
// context, the way an engine that does not answer in time does.
type actorEngine struct {
	*fakeConversation
	mu       sync.Mutex
	facts    session.ProcessActions
	factsErr error
	record   session.JobNotice
	asked    []string
	stop     session.ProcessStopOutcome
	receipt  session.ProcessStopReceipt
	block    bool
	returned bool
}

func (e *actorEngine) note(door string, id int, ms int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.asked = append(e.asked, door+":"+strconv.Itoa(id)+"@"+strconv.FormatInt(ms, 10))
}

func (e *actorEngine) ProcessRecord(_ context.Context, id int) (session.JobNotice, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.record, e.record.ID == id, nil
}
func (e *actorEngine) ProcessActions(_ context.Context, id int, ms int64) (session.ProcessActions, error) {
	e.note("actions", id, ms)
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.facts, e.factsErr
}
func (e *actorEngine) RerunProcessAt(_ context.Context, id int, ms int64) (session.JobNotice, error) {
	e.note("rerun", id, ms)
	return session.JobNotice{}, &session.ProcessActionRefused{Verb: "rerun", Reason: session.ProcessRerunUnknown}
}
func (e *actorEngine) StopProcessAt(_ context.Context, id int, ms int64) (session.ProcessStopOutcome, error) {
	e.note("stop", id, ms)
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stop, nil
}
func (e *actorEngine) StopConversationProcesses(ctx context.Context) (session.ProcessStopReceipt, error) {
	e.mu.Lock()
	block := e.block
	e.mu.Unlock()
	defer func() { e.mu.Lock(); e.returned = true; e.mu.Unlock() }()
	if block {
		<-ctx.Done()
		return session.ProcessStopReceipt{}, ctx.Err()
	}
	return e.receipt, nil
}

func actorBackend(t *testing.T, engine *actorEngine) (*appBackend, appwire.Agent) {
	t.Helper()
	backend := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: engine, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	t.Cleanup(func() { backend.Close() })
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := backend.heldLive(agent.ID); err != nil {
		t.Fatal(err)
	}
	return backend, agent
}

// THE ENGINE HOLDING THE CONVERSATION SAYS WHAT MAY BE DONE, by number and
// start; an unknown row carries its reason, and nothing else does.
func TestProcessActionsAreTheHoldingEnginesAnswerForThatRun(t *testing.T) {
	started := time.UnixMilli(1_700_000_000_000)
	ended := session.JobNotice{ID: 41, Kind: session.JobKindCommand, State: session.JobDone, Started: started}
	engine := &actorEngine{fakeConversation: &fakeConversation{}, record: ended, facts: session.ProcessActions{
		Record: ended, Stop: session.ProcessAction{Reason: session.ProcessStopEnded}, Rerun: session.ProcessAction{Available: true},
	}}
	backend, agent := actorBackend(t, engine)
	id := appProcessID(agent.ID, ended)
	got, err := backend.ProcessActions(context.Background(), id)
	if err != nil || got.Kind != appwire.ProcessKindJob || got.State != appwire.ProcessEnded || got.UnknownReason != "" ||
		!got.Rerun.Available || got.Stop.Available || got.Stop.Reason != session.ProcessStopEnded || got.Log.Reason != appwire.ProcessLogNoLog {
		t.Fatalf("ended: %+v %v", got, err)
	}
	if engine.asked[0] != "actions:41@1700000000000" {
		t.Fatalf("the engine was asked %q", engine.asked)
	}
	unknown := ended
	unknown.State = session.JobRunning
	engine.mu.Lock()
	engine.facts = session.ProcessActions{Record: unknown, Unknown: session.ProcessUnknownOwnerLost,
		Stop: session.ProcessAction{Reason: session.ProcessStopUnknown}, Rerun: session.ProcessAction{Reason: session.ProcessRerunUnknown}}
	engine.mu.Unlock()
	got, err = backend.ProcessActions(context.Background(), id)
	if err != nil || got.State != appwire.ProcessUnknown || got.UnknownReason != appwire.ProcessUnknownOwnerLost || got.Rerun.Available || got.Stop.Available {
		t.Fatalf("unknown: %+v %v", got, err)
	}
	engine.mu.Lock()
	engine.factsErr = session.ErrNoSuchProcess
	engine.mu.Unlock()
	if _, err := backend.ProcessActions(context.Background(), id); !isCode(err, appwire.CodeNotFound) {
		t.Fatalf("another run: %v", err)
	}
	if _, err := backend.ProcessActions(context.Background(), agent.ID+":p41.!!"); !isCode(err, appwire.CodeBadParams) {
		t.Fatalf("a malformed stamp: %v", err)
	}
}

// RUN AGAIN AND STOP ACT THROUGH THE STAMPED DOOR AND REPORT WHAT IT SAID: a
// refusal is a conflict in the engine's words, a stop it could not prove or
// that a saving render refused is never a success, and an engine too old to
// check is never asked to run anything again.
func TestRerunAndStopActThroughTheStampedDoor(t *testing.T) {
	started := time.UnixMilli(1_700_000_000_000)
	running := session.JobNotice{ID: 42, Kind: session.JobKindCommand, State: session.JobRunning, Started: started}
	engine := &actorEngine{fakeConversation: &fakeConversation{}, record: running}
	backend, agent := actorBackend(t, engine)
	id := appProcessID(agent.ID, running)
	if _, err := backend.RerunProcess(id); !isCode(err, appwire.CodeConflict) {
		t.Fatalf("refused rerun: %v", err)
	}
	for _, outcome := range []session.ProcessStopOutcome{{Line: "not ended in time", Unproven: true}, {Line: "being saved", Refused: true}} {
		engine.mu.Lock()
		engine.stop = outcome
		engine.mu.Unlock()
		if err := backend.StopProcess(id); err == nil {
			t.Fatalf("a stop that did not happen answered success: %+v", outcome)
		}
	}
	engine.mu.Lock()
	engine.stop = session.ProcessStopOutcome{Line: "stopped"}
	engine.mu.Unlock()
	if err := backend.StopProcess(id); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	asked := append([]string(nil), engine.asked...)
	engine.mu.Unlock()
	if len(asked) != 4 || asked[0] != "rerun:42@1700000000000" || asked[3] != "stop:42@1700000000000" {
		t.Fatalf("doors asked: %q", asked)
	}
	if len(engine.cancels) != 0 {
		t.Fatalf("the unstamped door was used beside the stamped one: %q", engine.cancels)
	}

	old := &fakeConversation{jobs: []session.JobNotice{{ID: 3, Kind: session.JobKindCommand, State: session.JobDone, Started: started}}}
	legacy, _, oldAgent := workBackend(t, old)
	oldID := appProcessID(oldAgent.ID, old.jobs[0])
	if _, err := legacy.RerunProcess(oldID); !isCode(err, appwire.CodeUnknownMethod) {
		t.Fatalf("an old engine was asked to run again: %v", err)
	}
}

// STOP-ALL HAS ONE DEADLINE FROM THE MOMENT IT IS ASKED: an engine that does
// not answer in time is joined at the deadline and the receipt says what is
// unknown, never "all stopped"; a second stop-all for the agent is busy; a
// crew is refused; an agent that does not exist is not found.
func TestStopAllHasOneDeadlineAndNeverClaimsWhatItCouldNotSee(t *testing.T) {
	engine := &actorEngine{fakeConversation: &fakeConversation{}, block: true}
	backend, agent := actorBackend(t, engine)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	second := make(chan error, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		_, err := backend.StopAllProcesses(context.Background(), appwire.ProcessStopAllAsk{Agent: agent.ID})
		second <- err
	}()
	began := time.Now()
	res, err := backend.StopAllProcesses(ctx, appwire.ProcessStopAllAsk{Agent: agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(began) > 2*time.Second {
		t.Fatalf("stop-all outlived its deadline: %v", time.Since(began))
	}
	engine.mu.Lock()
	returned := engine.returned
	engine.mu.Unlock()
	if !returned {
		t.Fatal("stop-all answered before the engine's stop was joined")
	}
	if !res.Partial || !res.RemainingUnknown || res.Eligible != 0 || res.NotStopped == nil || res.ServicesNotStopped == nil || res.Unsupported == nil {
		t.Fatalf("an unanswered stop-all receipt: %+v", res)
	}
	if err := <-second; !isCode(err, appwire.CodeBusy) {
		t.Fatalf("a second stop-all for the agent: %v", err)
	}
	if _, err := backend.StopAllProcesses(ctx, appwire.ProcessStopAllAsk{Agent: agent.ID, Crew: "c1"}); !isCode(err, appwire.CodeBadParams) {
		t.Fatalf("crew: %v", err)
	}
	if _, err := backend.StopAllProcesses(ctx, appwire.ProcessStopAllAsk{Agent: "nobody"}); err == nil {
		t.Fatal("an agent that does not exist was stopped")
	}
}

// THE RECEIPT IS THE ENGINE'S, IN THE APP'S IDS, BOUNDED: not-stopped jobs keep
// their stamps, a list over its bound is cut and says so, and a part this app
// cannot reach is unsupported rather than zero.
func TestStopAllReceiptIsTheEnginesBoundedAndInTheAppsIDs(t *testing.T) {
	started := time.UnixMilli(1_700_000_000_000)
	notStopped := make([]session.JobNotice, appwire.ProcessStopAllIDs+3)
	for i := range notStopped {
		notStopped[i] = session.JobNotice{ID: i + 1, Started: started}
	}
	engine := &actorEngine{fakeConversation: &fakeConversation{}, receipt: session.ProcessStopReceipt{
		Eligible: len(notStopped) + 2, Stopped: 2, NotStopped: notStopped,
	}}
	backend, agent := actorBackend(t, engine)
	res, err := backend.StopAllProcesses(context.Background(), appwire.ProcessStopAllAsk{Agent: agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if res.Eligible != len(notStopped)+2 || res.Stopped != 2 || len(res.NotStopped) != appwire.ProcessStopAllIDs ||
		res.NotStopped[0] != appProcessID(agent.ID, notStopped[0]) || !res.RemainingUnknown || !res.Partial {
		t.Fatalf("receipt: eligible=%d stopped=%d listed=%d first=%q partial=%v", res.Eligible, res.Stopped, len(res.NotStopped), res.NotStopped[0], res.Partial)
	}
	if len(res.Unsupported) != 1 || res.Unsupported[0] != appwire.ProcessStopAllServices {
		t.Fatalf("services with no supervisor: %q", res.Unsupported)
	}
	if res.JobsDeadlineMS != int(session.ProcessStopAllWait/time.Millisecond) {
		t.Fatalf("jobs deadline: %d", res.JobsDeadlineMS)
	}

	old := &fakeConversation{}
	legacy, _, oldAgent := workBackend(t, old)
	res, err = legacy.StopAllProcesses(context.Background(), appwire.ProcessStopAllAsk{Agent: oldAgent.ID})
	if err != nil || !res.Partial || len(res.Unsupported) != 2 || res.Unsupported[0] != appwire.ProcessStopAllJobs {
		t.Fatalf("an engine without the door: %+v %v", res, err)
	}
	if len(old.cancels) != 0 {
		t.Fatalf("an old engine was stopped job by job: %q", old.cancels)
	}
}

// A CONVERSATION NOBODY HERE HOLDS IS ANSWERED FROM ITS STORE: a record saved
// running is unknown and offers nothing, an ended one offers no run again
// without an owner, and its log target is the managed file, identified, with
// no grant — a link in its place is refused.
func TestProcessActionsAndLogTargetOfANotHeldConversation(t *testing.T) {
	conv := &fakeConversation{}
	backend, _, agent := workBackend(t, conv)
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	logs := filepath.Join(dir, "logs", "jobs")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	started := time.UnixMilli(1_700_000_000_000)
	ended := session.JobNotice{ID: 7, Kind: session.JobKindCommand, State: session.JobDone, Started: started, LogPath: filepath.Join(logs, "7.log")}
	running := session.JobNotice{ID: 8, Kind: session.JobKindCommand, State: session.JobRunning, Started: started}
	linked := session.JobNotice{ID: 9, Kind: session.JobKindCommand, State: session.JobDone, Started: started, LogPath: filepath.Join(logs, "9.log")}
	seedRecords(t, dir, ended, running, linked)
	if err := os.WriteFile(ended.LogPath, []byte("built\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "secret")
	_ = os.WriteFile(secret, []byte("secret"), 0o600)
	if err := os.Symlink(secret, linked.LogPath); err != nil {
		t.Fatal(err)
	}
	letGo(t, backend, agent.ID, transcript)
	ctx := context.Background()

	got, err := backend.ProcessActions(ctx, appProcessID(agent.ID, running))
	if err != nil || got.State != appwire.ProcessUnknown || got.UnknownReason != appwire.ProcessUnknownNotHeld ||
		got.Stop.Reason != appwire.ProcessActionNotHeld || got.Rerun.Reason != appwire.ProcessActionNotHeld {
		t.Fatalf("saved running: %+v %v", got, err)
	}
	got, err = backend.ProcessActions(ctx, appProcessID(agent.ID, ended))
	if err != nil || got.State != appwire.ProcessEnded || got.UnknownReason != "" || got.Stop.Reason != session.ProcessStopEnded ||
		got.Rerun.Available || got.Rerun.Reason != appwire.ProcessActionNotHeld || !got.Log.Available {
		t.Fatalf("saved ended: %+v %v", got, err)
	}
	target, err := backend.ProcessLogTarget(ctx, appProcessID(agent.ID, ended))
	if err != nil || !target.Local || target.Path != ended.LogPath || target.Size != 6 || target.Revision == "" || target.Gone || target.CapBytes != session.AppProcessOutputBytes {
		t.Fatalf("log target: %+v %v", target, err)
	}
	read, err := backend.ReadProcessLog(ctx, appwire.ProcessLogAsk{ID: appProcessID(agent.ID, ended)})
	if err != nil || read.Revision != target.Revision {
		t.Fatalf("the target's revision is not the log read's: %+v %v", read, err)
	}
	if _, err := backend.ProcessLogTarget(ctx, appProcessID(agent.ID, linked)); !isCode(err, appwire.CodeEngine) {
		t.Fatalf("a linked log: %v", err)
	}
	if got, err := backend.ProcessActions(ctx, appProcessID(agent.ID, linked)); err != nil || got.Log.Reason != appwire.ProcessLogUnmanaged {
		t.Fatalf("a linked log's action: %+v %v", got, err)
	}
	if err := os.Remove(ended.LogPath); err != nil {
		t.Fatal(err)
	}
	target, err = backend.ProcessLogTarget(ctx, appProcessID(agent.ID, ended))
	if err != nil || !target.Gone || target.Path != "" || target.Revision != "" {
		t.Fatalf("an expired log: %+v %v", target, err)
	}
	if _, err := backend.ProcessLogTarget(ctx, appProcessID(agent.ID, running)); !isCode(err, appwire.CodeNotFound) {
		t.Fatalf("a record with no log: %v", err)
	}
}

// A SERVICE'S ACTIONS AND LOG ARE THE SUPERVISOR'S, AND STOP-ALL STOPS ONLY
// THE AGENT'S OWN: another agent's service keeps running.
func TestServiceActionsLogTargetAndOwnerScopedStopAll(t *testing.T) {
	w := newSvcWorld(t)
	row := w.keep(t, w.agent.ID, service.PolicyOnFailure)
	other := w.keep(t, "someone-else", service.PolicyOnFailure)
	id := w.agent.ID + ":" + row.ID
	if err := os.WriteFile(row.LogPath, []byte("ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	got, err := w.backend.ProcessActions(ctx, id)
	if err != nil || got.Kind != appwire.ProcessKindService || got.State != appwire.ProcessRunning || !got.Stop.Available ||
		got.Rerun.Reason != session.ProcessRerunRunning || !got.Log.Available || got.UnknownReason != "" {
		t.Fatalf("running service: %+v %v", got, err)
	}
	target, err := w.backend.ProcessLogTarget(ctx, id)
	if err != nil || target.Kind != appwire.ProcessKindService || target.Path != row.LogPath || target.Size != 6 || target.CapBytes != service.LogCapBytes {
		t.Fatalf("service log target: %+v %v", target, err)
	}
	res, err := w.backend.StopAllProcesses(ctx, appwire.ProcessStopAllAsk{Agent: w.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if res.ServicesEligible != 1 || res.ServicesStopped != 1 || len(res.ServicesNotStopped) != 0 {
		t.Fatalf("services receipt: %+v", res)
	}
	if theirs, err := w.sup.Get("someone-else", other.ID); err != nil || !theirs.Running {
		t.Fatalf("another agent's service was stopped: %+v %v", theirs, err)
	}
	got, err = w.backend.ProcessActions(ctx, id)
	if err != nil || got.Stop.Available || !got.Rerun.Available {
		t.Fatalf("stopped service: %+v %v", got, err)
	}
}

func TestOldEngineStopDoesNotFallBackToAnUnverifiableCancel(t *testing.T) {
	job := session.JobNotice{ID: 7, Kind: session.JobKindRender, State: session.JobRunning, Started: time.Now()}
	old := &fakeConversation{jobs: []session.JobNotice{job}}
	backend, _, agent := workBackend(t, old)
	err := backend.StopProcess(appProcessID(agent.ID, job))
	if !isCode(err, appwire.CodeUnknownMethod) {
		t.Fatalf("old engine stop pretended to prove a stop: %v", err)
	}
	old.mu.Lock()
	defer old.mu.Unlock()
	if len(old.cancels) != 0 {
		t.Fatalf("unverifiable old Cancel was called: %q", old.cancels)
	}
}
