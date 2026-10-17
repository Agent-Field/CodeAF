package remote

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

type processWireFake struct {
	*fakeAgent
	rerun int
}

func (f *processWireFake) JobNotices() ([]session.JobNotice, error) {
	return []session.JobNotice{{ID: 7, Command: "printf ok", Kind: session.JobKindCommand, State: session.JobDone, OutputTruncated: true}}, nil
}
func (f *processWireFake) RerunProcess(id int) (session.JobNotice, error) {
	f.rerun = id
	return session.JobNotice{ID: 8, Command: "printf ok", Kind: session.JobKindCommand, State: session.JobRunning}, nil
}
func TestAppProcessWireClientAndServer(t *testing.T) {
	f := &processWireFake{fakeAgent: &fakeAgent{}}
	sess := newsSession(t, f)
	pipe, engine := Pipe()
	go func() {
		_ = ServeAttach(engine, engine, AttachOptions{Open: func(Hello) (*Session, error) { return sess, nil }})
		_ = engine.Close()
	}()
	client, err := Dial(pipe, "app", Hello{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	rows, err := client.Agent().JobNotices()
	if err != nil || len(rows) != 1 || rows[0].Command != "printf ok" || !rows[0].OutputTruncated {
		t.Fatalf("list: %+v %v", rows, err)
	}
	row, err := client.Agent().RerunProcess(7)
	if err != nil || row.ID != 8 || f.rerun != 7 {
		t.Fatalf("rerun: %+v %v", row, err)
	}
}
func TestAppProcessWireRefusesAbsentEngineAndBadArguments(t *testing.T) {
	for _, call := range []Frame{{Method: MethodProcessList}, {Method: MethodProcessRerun, Payload: json.RawMessage(`{"id":7}`)}} {
		if _, err := serveAppProcesses(&fakeAgent{}, call); err == nil || !strings.Contains(err.Error(), "cannot") {
			t.Fatalf("absent process door: %v", err)
		}
	}
	f := &processWireFake{fakeAgent: &fakeAgent{}}
	if _, err := serveAppProcesses(f, Frame{Method: MethodProcessRerun, Payload: json.RawMessage(`{"id":"x"}`)}); err == nil {
		t.Fatal("invalid process number accepted")
	}
}

type processPageFake struct {
	*processWireFake
	asked session.ProcessPageAsk
	stale bool
}

func (f *processPageFake) ProcessPage(_ context.Context, ask session.ProcessPageAsk) (session.ProcessPage, error) {
	f.asked = ask
	if f.stale {
		return session.ProcessPage{}, session.ErrProcessStale
	}
	return session.ProcessPage{Rows: []session.ProcessPageRow{{JobNotice: session.JobNotice{ID: 3, Dir: "/w", StartedBy: session.JobStartedByTask, Task: 2}, Unknown: session.ProcessUnknownOwnerLost}}, Next: "n", Incomplete: true, Revision: "r"}, nil
}
func (f *processPageFake) ProcessRecord(id int) (session.JobNotice, bool, error) {
	return session.JobNotice{ID: id, LogPath: "/logs/3.log"}, id == 3, nil
}

// THE PAGE AND THE RECORD ARE ANSWERED BY THE ENGINE THAT HOLDS THE
// CONVERSATION, whole, and an engine without them is told apart from a failure.
func TestAppProcessPageAndRecordCrossTheWire(t *testing.T) {
	f := &processPageFake{processWireFake: &processWireFake{fakeAgent: &fakeAgent{}}}
	sess := newsSession(t, f)
	pipe, engine := Pipe()
	go func() {
		_ = ServeAttach(engine, engine, AttachOptions{Open: func(Hello) (*Session, error) { return sess, nil }})
		_ = engine.Close()
	}()
	client, err := Dial(pipe, "app", Hello{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	page, err := client.Agent().ProcessPage(context.Background(), session.ProcessPageAsk{Limit: 7, Cursor: "c"})
	if err != nil || f.asked.Limit != 7 || f.asked.Cursor != "c" || page.Next != "n" || !page.Incomplete || len(page.Rows) != 1 {
		t.Fatalf("page: %+v %v", page, err)
	}
	row := page.Rows[0]
	if row.Unknown != session.ProcessUnknownOwnerLost || row.Dir != "/w" || row.Task != 2 || row.StartedBy != session.JobStartedByTask {
		t.Fatalf("row: %+v", row)
	}
	record, found, err := client.Agent().ProcessRecord(context.Background(), 3)
	if err != nil || !found || record.LogPath != "/logs/3.log" {
		t.Fatalf("record: %+v %v %v", record, found, err)
	}
	if _, found, _ := client.Agent().ProcessRecord(context.Background(), 4); found {
		t.Fatal("a missing record was found")
	}
	f.stale = true
	if _, err := client.Agent().ProcessPage(context.Background(), session.ProcessPageAsk{Limit: 1}); !errors.Is(err, session.ErrProcessStale) {
		t.Fatalf("stale: %v", err)
	}
	for _, call := range []Frame{{Method: MethodProcessPage, Payload: json.RawMessage(`{"limit":1}`)}, {Method: MethodProcessRecord, Payload: json.RawMessage(`{"id":1}`)}} {
		if _, err := serveAppProcesses(&fakeAgent{}, call); err == nil || !strings.Contains(err.Error(), "no such process action") {
			t.Fatalf("absent page door: %v", err)
		}
	}
}

type slowPageFake struct {
	*processWireFake
	release chan struct{}
}

func (f *slowPageFake) ProcessPage(context.Context, session.ProcessPageAsk) (session.ProcessPage, error) {
	<-f.release
	return session.ProcessPage{}, nil
}
func (f *slowPageFake) ProcessRecord(int) (session.JobNotice, bool, error) {
	<-f.release
	return session.JobNotice{}, false, nil
}

// THE CALLER'S DEADLINE IS THE CALL'S: an engine that does not answer leaves
// the caller at its own deadline, not at the wire's longer ceiling.
func TestAppProcessPageAndRecordKeepTheCallersDeadline(t *testing.T) {
	f := &slowPageFake{processWireFake: &processWireFake{fakeAgent: &fakeAgent{}}, release: make(chan struct{})}
	defer close(f.release)
	sess := newsSession(t, f)
	pipe, engine := Pipe()
	go func() {
		_ = ServeAttach(engine, engine, AttachOptions{Open: func(Hello) (*Session, error) { return sess, nil }})
		_ = engine.Close()
	}()
	client, err := Dial(pipe, "app", Hello{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for name, call := range map[string]func(context.Context) error{
		"page": func(ctx context.Context) error {
			_, err := client.Agent().ProcessPage(ctx, session.ProcessPageAsk{Limit: 1})
			return err
		},
		"record": func(ctx context.Context) error { _, _, err := client.Agent().ProcessRecord(ctx, 1); return err },
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		start := time.Now()
		err := call(ctx)
		cancel()
		if err == nil || time.Since(start) > 2*time.Second {
			t.Fatalf("%s outlived its deadline: %v after %v", name, err, time.Since(start))
		}
	}
}

type relayRecordFake struct{ *fakeAgent }

func (relayRecordFake) ProcessRecord(_ context.Context, id int) (session.JobNotice, bool, error) {
	return session.JobNotice{ID: id, Command: "relayed"}, true, nil
}

// A REMOTE AGENT SERVED ONWARD ANSWERS THE RECORD DOOR TOO: its ProcessRecord
// takes a context, and the server accepts either shape.
func TestAppProcessRecordDoorAcceptsTheContextShape(t *testing.T) {
	raw, err := serveAppProcesses(relayRecordFake{&fakeAgent{}}, Frame{Method: MethodProcessRecord, Payload: json.RawMessage(`{"id":5}`)})
	if err != nil || !strings.Contains(string(raw), "relayed") || !strings.Contains(string(raw), `"found":true`) {
		t.Fatalf("relayed record: %s %v", raw, err)
	}
}

type processActFake struct {
	*fakeAgent
	asked ProcessActArgs
}

func (f *processActFake) ProcessActions(_ context.Context, id int, ms int64) (session.ProcessActions, error) {
	f.asked = ProcessActArgs{ID: id, StartedMS: ms}
	if id == 404 {
		return session.ProcessActions{}, session.ErrNoSuchProcess
	}
	return session.ProcessActions{Record: session.JobNotice{ID: id}, Rerun: session.ProcessAction{Reason: session.ProcessRerunUnknown}}, nil
}
func (f *processActFake) RerunProcessAt(_ context.Context, id int, ms int64) (session.JobNotice, error) {
	f.asked = ProcessActArgs{ID: id, StartedMS: ms}
	return session.JobNotice{}, &session.ProcessActionRefused{Verb: "rerun", Reason: session.ProcessRerunUnknown}
}
func (f *processActFake) StopProcessAt(_ context.Context, id int, ms int64) (session.ProcessStopOutcome, error) {
	f.asked = ProcessActArgs{ID: id, StartedMS: ms}
	return session.ProcessStopOutcome{Line: "stopped", Unproven: true}, nil
}
func (f *processActFake) StopConversationProcesses(ctx context.Context) (session.ProcessStopReceipt, error) {
	if _, ok := ctx.Deadline(); !ok {
		return session.ProcessStopReceipt{}, errors.New("no deadline travelled to the engine's stop")
	}
	return session.ProcessStopReceipt{Eligible: 2, Stopped: 1, NotStopped: []session.JobNotice{{ID: 3}}}, nil
}

// THE STAMPED DOORS CARRY THE START, AND A REFUSAL CROSSES AS ITS WORD.
func TestProcessActWireCarriesStampsAndRefusalWords(t *testing.T) {
	f := &processActFake{fakeAgent: &fakeAgent{}}
	sess := newsSession(t, f)
	pipe, engine := Pipe()
	go func() {
		_ = ServeAttach(engine, engine, AttachOptions{Open: func(Hello) (*Session, error) { return sess, nil }})
		_ = engine.Close()
	}()
	client, err := Dial(pipe, "app", Hello{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	facts, err := client.Agent().ProcessActions(ctx, 7, 1234)
	if err != nil || facts.Record.ID != 7 || facts.Rerun.Reason != session.ProcessRerunUnknown || f.asked.StartedMS != 1234 {
		t.Fatalf("actions: %+v %v %+v", facts, err, f.asked)
	}
	if _, err := client.Agent().ProcessActions(ctx, 404, 1); !errors.Is(err, session.ErrNoSuchProcess) {
		t.Fatalf("not found: %v", err)
	}
	var refused *session.ProcessActionRefused
	if _, err := client.Agent().RerunProcessAt(ctx, 7, 99); !errors.As(err, &refused) || refused.Reason != session.ProcessRerunUnknown || f.asked.StartedMS != 99 {
		t.Fatalf("refused rerun: %v", err)
	}
	stop, err := client.Agent().StopProcessAt(ctx, 7, 5)
	if err != nil || !stop.Unproven {
		t.Fatalf("stop: %+v %v", stop, err)
	}
	receipt, err := client.Agent().StopConversationProcesses(ctx)
	if err != nil || receipt.Eligible != 2 || len(receipt.NotStopped) != 1 {
		t.Fatalf("stop all: %+v %v", receipt, err)
	}
}

// AN ENGINE WITHOUT THE DOORS IS UNSERVED, never a refusal or an empty answer.
func TestProcessActWireOnAnEngineWithoutTheDoors(t *testing.T) {
	sess := newsSession(t, &fakeAgent{})
	pipe, engine := Pipe()
	go func() {
		_ = ServeAttach(engine, engine, AttachOptions{Open: func(Hello) (*Session, error) { return sess, nil }})
		_ = engine.Close()
	}()
	client, err := Dial(pipe, "app", Hello{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Agent().StopConversationProcesses(context.Background()); !errors.Is(err, ErrProcessActionsUnserved) {
		t.Fatalf("absent doors: %v", err)
	}
}

func TestProcessActAlreadyCancelledWritesNothing(t *testing.T) {
	sess := newsSession(t, &processActFake{fakeAgent: &fakeAgent{}})
	pipe, engine := Pipe()
	go func() {
		_ = ServeAttach(engine, engine, AttachOptions{Open: func(Hello) (*Session, error) { return sess, nil }})
		_ = engine.Close()
	}()
	client, err := Dial(pipe, "app", Hello{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	before := client.made.Load()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Agent().RerunProcessAt(ctx, 7, 99)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled rerun: %v", err)
	}
	if got := client.made.Load(); got != before {
		t.Fatalf("cancelled rerun wrote %d calls", got-before)
	}
}

func TestProcessActExpiredForwardedDeadlineCallsNoDoor(t *testing.T) {
	for _, method := range []string{MethodProcessRerunAt, MethodProcessStopAll} {
		f := &processActFake{fakeAgent: &fakeAgent{}}
		payload, _ := json.Marshal(map[string]any{"id": 7, "started_ms": 99, "deadline_unix_nano": time.Now().Add(-time.Second).UnixNano()})
		_, err := serveProcessAct(f, Frame{Method: method, Payload: payload})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s expired deadline still reached a door: err=%v asked=%+v", method, err, f.asked)
		}
		if f.asked.ID != 0 {
			t.Fatalf("expired request reached a door: %+v", f.asked)
		}
	}
}

type deadlineProcessActFake struct {
	*processActFake
	deadline time.Time
}

func (f *deadlineProcessActFake) StopConversationProcesses(ctx context.Context) (session.ProcessStopReceipt, error) {
	f.deadline, _ = ctx.Deadline()
	return session.ProcessStopReceipt{}, nil
}
func TestProcessActWirePreservesCallersEarlierDeadline(t *testing.T) {
	f := &deadlineProcessActFake{processActFake: &processActFake{fakeAgent: &fakeAgent{}}}
	sess := newsSession(t, f)
	pipe, engine := Pipe()
	go func() {
		_ = ServeAttach(engine, engine, AttachOptions{Open: func(Hello) (*Session, error) { return sess, nil }})
		_ = engine.Close()
	}()
	client, err := Dial(pipe, "app", Hello{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	deadline := time.Now().Add(time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	if _, err := client.Agent().StopConversationProcesses(ctx); err != nil {
		t.Fatal(err)
	}
	if !f.deadline.Equal(deadline) {
		t.Fatalf("engine reset caller deadline: got=%s want=%s", f.deadline, deadline)
	}
}
