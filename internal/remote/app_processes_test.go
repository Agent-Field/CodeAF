package remote

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

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
	record, found, err := client.Agent().ProcessRecord(3)
	if err != nil || !found || record.LogPath != "/logs/3.log" {
		t.Fatalf("record: %+v %v %v", record, found, err)
	}
	if _, found, _ := client.Agent().ProcessRecord(4); found {
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
