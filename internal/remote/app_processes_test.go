package remote

import (
	"encoding/json"
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
