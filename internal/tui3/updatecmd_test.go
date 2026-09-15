package tui3

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

func updateNotes(a *app) string {
	var lines []string
	for _, entry := range a.entries {
		if entry.kind == entryNote {
			lines = append(lines, entry.text)
		}
	}
	return strings.Join(lines, "\n")
}

// TestC1LaunchAvailabilityBecomesOneTranscriptNote proves C1.
func TestC1LaunchAvailabilityBecomesOneTranscriptNote(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "test/model"})
	available := codeupdate.Available{Latest: "v0.2.0", Running: "v0.1.1"}
	drive(t, a, updateCheckMsg{available: available, show: true})
	line := "codeaf v0.2.0 is out · you have v0.1.1 · /update installs it and restarts · or: " + codeupdate.CurlCommand
	if got := strings.Count(updateNotes(a), line); got != 1 {
		t.Fatalf("launch note count = %d, want 1:\n%s", got, updateNotes(a))
	}
}

// TestOneUpdateOwnsTheSurfaceUntilItFinishes proves that a second installer
// and a home-page turn cannot begin while release selection is off-frame.
func TestOneUpdateOwnsTheSurfaceUntilItFinishes(t *testing.T) {
	agent := &fakeAgent{model: "test/model"}
	a := newApp(context.Background(), Options{
		Agent: agent, Workspace: "/tmp/lab", UpdateRunning: "v0.1.1", Restart: &codeupdate.Plan{},
		ResolveUpdate: func(context.Context, codeupdate.Choice) (codeupdate.Release, error) {
			return codeupdate.Release{Tag: "v0.2.0"}, nil
		},
		InstallUpdate: func(context.Context, codeupdate.Release) (codeupdate.InstallResult, error) {
			return codeupdate.InstallResult{}, nil
		},
	})
	first := a.slash("/update")
	if first == nil || !a.updateActive {
		t.Fatalf("first command = %v, active = %t", first, a.updateActive)
	}
	if second := a.slash("/upgrade"); second != nil {
		t.Fatal("the overlapping update returned a command")
	}
	started := 0
	a.start = func(string) (Conversation, error) {
		started++
		return Conversation{Agent: &fakeAgent{model: "test/model"}, SessionFile: "/tmp/next/transcript.jsonl"}, nil
	}
	a.openHome()
	a.home.box.setText("start another turn")
	a.home.build()
	drive(t, a, key("enter"))
	if started != 0 {
		t.Fatalf("home enter started %d conversations while the update was running", started)
	}
	if len(agent.sent) != 0 || a.home.box.String() != "start another turn" {
		t.Fatalf("sent = %q home draft = %q", agent.sent, a.home.box.String())
	}
	for _, want := range []string{"an update is already running", "finish the update before starting a turn"} {
		if strings.Count(updateNotes(a), want) != 1 {
			t.Fatalf("notes do not contain %q once:\n%s", want, updateNotes(a))
		}
	}
}

// TestAnUpdateOwnsHomesAskHereRoad proves that the home-page chord cannot
// start an errand beneath the restart that an update is preparing.
func TestAnUpdateOwnsHomesAskHereRoad(t *testing.T) {
	called := 0
	a := newTestApp(&fakeAgent{model: "test/model"})
	a.updateActive = true
	a.standingRoot = t.TempDir()
	a.errand = func(ErrandOrders) (Agent, error) {
		called++
		return &errandAgent{fakeAgent: fakeAgent{model: "test/model"}}, nil
	}
	a.openHome()
	a.home.box.setText("ask from home")
	a.home.build()
	drive(t, a, key("alt+enter"))
	if !a.composerShowing() {
		t.Fatal("the first ask-here chord did not open its composer")
	}
	drive(t, a, key("alt+enter"))

	if called != 0 || len(a.exchanges) != 0 {
		t.Fatalf("errand calls = %d exchanges = %d", called, len(a.exchanges))
	}
	if got := strings.Count(updateNotes(a), "finish the update before starting a turn"); got != 1 {
		t.Fatalf("update refusal count = %d, want 1:\n%s", got, updateNotes(a))
	}
}

// TestUpdateCheckCommandDefersTheReleaseDoor proves the command itself does no
// work until Bubble Tea runs it. The shared door test proves the first frame.
func TestUpdateCheckCommandDefersTheReleaseDoor(t *testing.T) {
	called := false
	a := newTestApp(&fakeAgent{model: "test/model"})
	a.updateCheck = func(context.Context) (codeupdate.Available, bool) {
		called = true
		return codeupdate.Available{}, false
	}
	command := a.checkForUpdate()
	if called || command == nil {
		t.Fatalf("building the first frame called = %t, command nil = %t", called, command == nil)
	}
	_ = command()
	if !called {
		t.Fatal("the command did not make the deferred check")
	}
}

// TestC6UpdateOnTheNewestBuildDoesNotInstallOrQuit proves C6.
func TestC6UpdateOnTheNewestBuildDoesNotInstallOrQuit(t *testing.T) {
	installed := false
	restart := &codeupdate.Plan{}
	a := newApp(context.Background(), Options{
		Agent: &fakeAgent{model: "test/model"}, Workspace: "/tmp/lab",
		UpdateRunning: "v0.2.0", Restart: restart,
		ResolveUpdate: func(context.Context, codeupdate.Choice) (codeupdate.Release, error) {
			return codeupdate.Release{Tag: "v0.2.0"}, nil
		},
		InstallUpdate: func(context.Context, codeupdate.Release) (codeupdate.InstallResult, error) {
			installed = true
			return codeupdate.InstallResult{}, nil
		},
	})
	command := a.slash("/update")
	drive(t, a, command())
	if installed || a.updateActive || restart.Path != "" || !strings.Contains(updateNotes(a), "you are on the newest codeaf, v0.2.0") {
		t.Fatalf("installed = %t active = %t restart = %+v notes:\n%s", installed, a.updateActive, restart, updateNotes(a))
	}
}

// TestC7UpdateInstallReturnsTheRestartPlanForThisTranscript proves C7.
func TestC7UpdateInstallReturnsTheRestartPlanForThisTranscript(t *testing.T) {
	restart := &codeupdate.Plan{}
	a := newApp(context.Background(), Options{
		Agent: &fakeAgent{model: "test/model"}, Workspace: "/tmp/lab", SessionFile: "/tmp/this.jsonl",
		UpdateRunning: "v0.1.1", UpdateArgs: []string{"chat", "--model", "x"}, Restart: restart,
		ResolveUpdate: func(context.Context, codeupdate.Choice) (codeupdate.Release, error) {
			return codeupdate.Release{Tag: "v0.2.0"}, nil
		},
		InstallUpdate: func(context.Context, codeupdate.Release) (codeupdate.InstallResult, error) {
			return codeupdate.InstallResult{Release: codeupdate.Release{Tag: "v0.2.0"}, Path: "/tmp/codeaf"}, nil
		},
	})
	command := a.slash("/update")
	drive(t, a, command())
	if restart.Path != "/tmp/codeaf" || strings.Join(restart.Args, " ") != "chat --model x --session /tmp/this.jsonl" {
		t.Fatalf("restart = %+v", restart)
	}
	for _, want := range []string{"downloading codeaf v0.2.0", "checksum matched · installed at /tmp/codeaf", "restarting on v0.2.0"} {
		if !strings.Contains(updateNotes(a), want) {
			t.Fatalf("notes do not contain %q:\n%s", want, updateNotes(a))
		}
	}
}

// TestC7UpdateFailureNamesTheCauseAndLeavesNoRestart proves C7.
func TestC7UpdateFailureNamesTheCauseAndLeavesNoRestart(t *testing.T) {
	restart := &codeupdate.Plan{}
	a := newApp(context.Background(), Options{
		Agent: &fakeAgent{model: "test/model"}, Workspace: "/tmp/lab", UpdateRunning: "v0.1.1", Restart: restart,
		ResolveUpdate: func(context.Context, codeupdate.Choice) (codeupdate.Release, error) {
			return codeupdate.Release{Tag: "v0.2.0"}, nil
		},
		InstallUpdate: func(context.Context, codeupdate.Release) (codeupdate.InstallResult, error) {
			return codeupdate.InstallResult{}, errors.New("the checksum did not match")
		},
	})
	drive(t, a, a.slash("/update")())
	if a.updateActive || restart.Path != "" || !strings.Contains(updateNotes(a), "the checksum did not match") || !strings.Contains(updateNotes(a), codeupdate.CurlCommand) {
		t.Fatalf("active = %t restart = %+v notes:\n%s", a.updateActive, restart, updateNotes(a))
	}
}

// TestC8UpdateRefusesWhileATurnOrTaskIsRunning proves C8.
func TestC8UpdateRefusesWhileATurnOrTaskIsRunning(t *testing.T) {
	for _, kind := range []string{"turn", "task"} {
		t.Run(kind, func(t *testing.T) {
			resolved := false
			a := newTestApp(&fakeAgent{model: "test/model"})
			a.updateRunning = "v0.1.1"
			a.restart = &codeupdate.Plan{}
			a.resolveUpdate = func(context.Context, codeupdate.Choice) (codeupdate.Release, error) {
				resolved = true
				return codeupdate.Release{}, nil
			}
			a.installUpdate = func(context.Context, codeupdate.Release) (codeupdate.InstallResult, error) {
				return codeupdate.InstallResult{}, nil
			}
			if kind == "turn" {
				a.state = stateWorking
			} else {
				a.tasks = map[uint64]*taskNode{1: {id: 1, state: session.TaskRunning}}
				a.taskOrder = []uint64{1}
			}
			if command := a.slash("/update"); command != nil {
				t.Fatal("the refusal returned a command")
			}
			if resolved || strings.Count(updateNotes(a), "finish the running turn or task before updating codeaf") != 1 {
				t.Fatalf("resolved = %t notes:\n%s", resolved, updateNotes(a))
			}
		})
	}
}
