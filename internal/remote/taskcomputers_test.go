package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// S9 keeps the watcher's welcome bytes unchanged when task controls are absent.
func TestAWatcherWelcomeIsUnchanged(t *testing.T) {
	taskComputerWireHome(t)
	file := t.TempDir() + "/chat.jsonl"
	welcome := func(paired bool) []byte {
		far := &pairedTaskWireAgent{fakeAgent: &fakeAgent{}, available: paired}
		loop, err := Loopback(Hello{Version: Version, Watch: true}, Options{Boot: func(Hello) (*Engine, error) {
			return &Engine{Agent: far, SessionFile: file}, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		defer loop.Close()
		welcome := loop.Client.Welcome()
		// Each live owner mints its own cursor scope, independently of capabilities.
		welcome.SessionInstance = ""
		raw, err := json.Marshal(welcome)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	original, paired := welcome(false), welcome(true)
	if !bytes.Equal(original, paired) || bytes.Contains(paired, []byte(`"taskComputers"`)) {
		t.Fatalf("a watcher's welcome changed:\n%s\n%s", original, paired)
	}
}

type pairedTaskWireAgent struct {
	*fakeAgent
	computerMu              sync.Mutex
	computer, effort, brief string
	solo                    bool
	here                    uint64
	answer                  session.TaskAnswer
	available               bool
}

func taskComputerWireHome(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("CODEAF_HOME", root)
}

func (a *pairedTaskWireAgent) TaskComputersSupported() bool { return a.available }
func (a *pairedTaskWireAgent) StartTaskOn(_ context.Context, brief string, solo bool, computer string, effort ...string) (uint64, string, string, error) {
	a.computerMu.Lock()
	defer a.computerMu.Unlock()
	a.brief, a.solo, a.computer = brief, solo, computer
	if len(effort) > 0 {
		a.effort = effort[0]
	}
	return 7, "Repair export", "the task works in its own copy", nil
}
func (a *pairedTaskWireAgent) RunTaskHere(_ context.Context, id uint64) (string, error) {
	a.computerMu.Lock()
	defer a.computerMu.Unlock()
	a.here = id
	return "running task 7 Repair export here", nil
}
func (a *pairedTaskWireAgent) ResolveTask(_ uint64, answer session.TaskAnswer) {
	a.computerMu.Lock()
	defer a.computerMu.Unlock()
	a.answer = answer
}

// DESIGN contracts 1, 6 and 11 require a connected window's controls to reach
// its actual engine agent with the computer, effort and recovery task intact.
func TestConnectedTaskComputerControlsReachTheEngineAgent(t *testing.T) {
	taskComputerWireHome(t)
	far := &pairedTaskWireAgent{fakeAgent: &fakeAgent{}, available: true}
	file := t.TempDir() + "/chat.jsonl"
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: file}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	agent := loop.Client.Agent()
	controls, ok := any(agent).(interface {
		TaskComputersSupported() bool
		StartTaskOn(context.Context, string, bool, string, ...string) (uint64, string, string, error)
		RunTaskHere(context.Context, uint64) (string, error)
	})
	if !ok || !controls.TaskComputersSupported() {
		t.Fatal("the connection omits paired task controls")
	}
	id, title, note, err := controls.StartTaskOn(context.Background(), "repair export", true, "Abir's Mac", "cheap")
	if err != nil || id != 7 || title != "Repair export" || note != "the task works in its own copy" {
		t.Fatalf("named start = %d %q %q %v", id, title, note, err)
	}
	far.computerMu.Lock()
	if far.computer != "Abir's Mac" || far.effort != "cheap" || far.brief != "repair export" || !far.solo {
		t.Errorf("the start lost its chosen controls")
	}
	far.computerMu.Unlock()
	if line, err := controls.RunTaskHere(context.Background(), 7); err != nil || line != "running task 7 Repair export here" {
		t.Fatalf("run here = %q, %v", line, err)
	}
	far.computerMu.Lock()
	if far.here != 7 {
		t.Errorf("recovery reached task %d", far.here)
	}
	far.computerMu.Unlock()
	agent.ResolveTask(7, session.TaskAnswer{Approved: true, Computer: "this computer"})
	waitFor(t, "the proposal's computer to cross", func() bool {
		far.computerMu.Lock()
		defer far.computerMu.Unlock()
		return far.answer.Approved && far.answer.Computer == "this computer"
	})
}

// The compatibility law keeps old requests byte-for-byte unchanged when no
// new optional choice is supplied; the original method names keep their work.
func TestExistingTaskRequestsKeepTheirMethodAndBytes(t *testing.T) {
	taskComputerWireHome(t)
	for _, tc := range []struct {
		value any
		want  string
	}{
		{TaskStartArgs{Brief: "repair export", Solo: true, Effort: "best"}, `{"brief":"repair export","solo":true,"effort":"best"}`},
		{TaskResolveArgs{ID: 7, Approved: true, Redirect: "also test it", Model: "test/model"}, `{"id":7,"approved":true,"redirect":"also test it","model":"test/model"}`},
		{session.TaskAnswer{Approved: true, Redirect: "also test it", Model: "test/model"}, `{"Approved":true,"Redirect":"also test it","Model":"test/model"}`},
	} {
		raw, err := json.Marshal(tc.value)
		if err != nil || string(raw) != tc.want {
			t.Fatalf("old request changed: %s, %v; want %s", raw, err, tc.want)
		}
	}
	if MethodTaskStart != "Task.Start" || MethodTaskResolve != "Task.Resolve" {
		t.Fatal("an existing method was renamed")
	}
	far := &fakeAgent{}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: t.TempDir() + "/chat.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if _, _, _, err := loop.Client.Agent().StartTask(context.Background(), "repair export", true); err != nil {
		t.Fatal(err)
	}
	far.mu.Lock()
	defer far.mu.Unlock()
	if len(far.tasks) != 1 || far.tasks[0] != "solo:repair export" {
		t.Fatalf("the original start no longer reaches its original operation: %v", far.tasks)
	}
}

// The absence and compatibility laws require an omitted welcome capability
// to keep the new methods unavailable without probing an older engine.
func TestAnEngineWithoutPairedTaskMethodsAdvertisesNoComputerControls(t *testing.T) {
	taskComputerWireHome(t)
	client, old := newEngine(t)
	controls, ok := any(client.Agent()).(interface{ TaskComputersSupported() bool })
	if !ok {
		t.Fatal("the client cannot read the task-computer capability")
	}
	if controls.TaskComputersSupported() {
		t.Fatal("an older welcome enabled paired task controls")
	}
	if _, err := client.Ping(); err != nil {
		t.Fatal(err)
	}
	old.mu.Lock()
	defer old.mu.Unlock()
	for _, call := range old.seen {
		if call.Method == "Task.StartOn" || call.Method == "Task.RunHere" {
			t.Fatalf("an older engine was probed with %s", call.Method)
		}
	}
}

// DESIGN contract 11 addresses the task in its own chat. Neither a foreign
// conversation claim nor a read-only connection may restart another task.
func TestConnectedTaskComputerControlsKeepTheirConversationAndRefuseWatchers(t *testing.T) {
	for _, watch := range []bool{false, true} {
		t.Run(map[bool]string{false: "owner", true: "watcher"}[watch], func(t *testing.T) {
			taskComputerWireHome(t)
			far := &pairedTaskWireAgent{fakeAgent: &fakeAgent{}, available: true}
			file := t.TempDir() + "/chat.jsonl"
			loop, err := Loopback(Hello{Version: Version, Watch: watch}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far, SessionFile: file}, nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer loop.Close()
			claimed := t.TempDir() + "/another-chat.jsonl"
			if watch {
				claimed = file
			}
			if _, err := loop.Client.call(nil, MethodTaskStartOn, TaskStartOnArgs{Brief: "repair export", Computer: "work computer", Session: claimed}); err == nil {
				t.Fatal("a foreign or read-only named start was accepted")
			}
			if _, err := loop.Client.call(nil, MethodTaskRunHere, TaskSetupArgs{ID: 7, Session: claimed}); err == nil {
				t.Fatal("a foreign or read-only local restart was accepted")
			}
			far.computerMu.Lock()
			defer far.computerMu.Unlock()
			if far.computer != "" || far.here != 0 {
				t.Fatal("a refused control changed work")
			}
			if watch && loop.Client.Agent().TaskComputersSupported() {
				t.Fatal("a read-only window advertises paired task controls")
			}
		})
	}
}

// The absence law applies to a new engine too: method shapes alone do not
// make an unpaired or ownerless conversation able to run tasks elsewhere.
func TestAnUnpairedOrOwnerlessEngineOffersNoTaskComputerControls(t *testing.T) {
	for _, available := range []bool{false, true} {
		t.Run(map[bool]string{false: "unpaired", true: "ownerless"}[available], func(t *testing.T) {
			taskComputerWireHome(t)
			far := &pairedTaskWireAgent{fakeAgent: &fakeAgent{}, available: available}
			file := t.TempDir() + "/chat.jsonl"
			if available {
				file = ""
			}
			loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far, SessionFile: file}, nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer loop.Close()
			if loop.Client.Agent().TaskComputersSupported() {
				t.Fatal("unusable paired task controls were advertised")
			}
			raw, err := json.Marshal(loop.Client.Welcome())
			if err != nil || strings.Contains(string(raw), `"taskComputers"`) {
				t.Fatalf("the absent optional capability changed welcome bytes: %s %v", raw, err)
			}
		})
	}
}
